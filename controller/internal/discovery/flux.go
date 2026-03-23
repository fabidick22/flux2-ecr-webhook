// Package discovery provides functions to cross-reference Flux resources
// (ImageRepository, ImagePolicy, Receiver) and extract the data needed
// to build the AWS repo_mapping automatically.
package discovery

import (
	"context"
	"fmt"
	"strings"

	imagev1beta2 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	notificationv1 "github.com/fluxcd/notification-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ImageInfo holds all data extracted from Flux resources for one ECR repo entry.
// One ImageRepository may produce multiple ImageInfo values (one per Receiver).
type ImageInfo struct {
	// ECRRepoName is the repository name extracted from the ECR image URL.
	// e.g., "my-ecr-repo" from "123456.dkr.ecr.us-east-1.amazonaws.com/my-ecr-repo"
	ECRRepoName string

	// WebhookURLs is the list of full webhook URLs built from
	// WebhookBaseURL + Receiver.Status.WebhookPath.
	WebhookURLs []string

	// Token is the bearer token read from the secret referenced by the Receiver.
	Token string

	// Regex is the tag filter pattern taken from the associated ImagePolicy.
	// Empty string means match all tags.
	Regex string

	// ReceiverName is used as the unique ID key in the repo_mapping.
	ReceiverName string
}

// FluxDiscovery discovers and correlates Flux resources for the controller.
type FluxDiscovery struct {
	Client         client.Client
	FluxNamespace  string
	WebhookBaseURL string
}

// DiscoverForImageRepository returns all ImageInfo entries for the given
// ImageRepository by cross-referencing its associated Receivers and ImagePolicies.
func (d *FluxDiscovery) DiscoverForImageRepository(ctx context.Context, repo *imagev1beta2.ImageRepository) ([]ImageInfo, error) {
	logger := log.FromContext(ctx).WithValues("imageRepo", repo.Name, "namespace", repo.Namespace)

	ecrRepoName, err := extractECRRepoName(repo.Spec.Image)
	if err != nil {
		logger.V(1).Info("skipping non-ECR image", "image", repo.Spec.Image, "reason", err.Error())
		return nil, nil
	}
	logger = logger.WithValues("ecrRepo", ecrRepoName)

	receivers, err := d.findReceiversForRepo(ctx, repo.Name, repo.Namespace)
	if err != nil {
		return nil, fmt.Errorf("listing receivers for %s/%s: %w", repo.Namespace, repo.Name, err)
	}
	if len(receivers) == 0 {
		logger.V(1).Info("no Receiver found", "imageRepo", repo.Name)
		return nil, nil
	}
	logger.V(1).Info("found matching Receivers", "receivers", len(receivers))

	policies, err := d.findPoliciesForRepo(ctx, repo.Name, repo.Namespace)
	if err != nil {
		return nil, fmt.Errorf("listing image policies for %s: %w", repo.Name, err)
	}

	regex := extractRegex(policies)

	var results []ImageInfo
	for i := range receivers {
		r := &receivers[i]

		webhookURLs := d.buildWebhookURLs(r)
		if len(webhookURLs) == 0 {
			logger.Info("Receiver has no status.webhookPath yet (not reconciled by notification-controller?)", "receiver", r.Name)
			continue
		}

		token, err := d.readToken(ctx, r.Spec.SecretRef.Name, r.Namespace)
		if err != nil {
			return nil, fmt.Errorf("reading token for receiver %s/%s: %w", r.Namespace, r.Name, err)
		}

		logger.V(1).Info("discovered webhook mapping", "receiver", r.Name, "webhooks", webhookURLs, "hasToken", token != "", "regex", regex)
		results = append(results, ImageInfo{
			ECRRepoName:  ecrRepoName,
			WebhookURLs:  webhookURLs,
			Token:        token,
			Regex:        regex,
			ReceiverName: r.Name,
		})
	}

	return results, nil
}

// findReceiversForRepo returns all Receivers in the Flux namespace whose
// spec.resources list references the given ImageRepository by name and namespace.
func (d *FluxDiscovery) findReceiversForRepo(ctx context.Context, repoName, repoNamespace string) ([]notificationv1.Receiver, error) {
	list := &notificationv1.ReceiverList{}
	if err := d.Client.List(ctx, list, client.InNamespace(d.FluxNamespace)); err != nil {
		return nil, err
	}

	var matched []notificationv1.Receiver
	for _, r := range list.Items {
		for _, res := range r.Spec.Resources {
			if res.Kind != "ImageRepository" || res.Name != repoName {
				continue
			}
			// Namespace is optional in CrossNamespaceObjectReference;
			// empty means same namespace as the Receiver itself.
			resNS := res.Namespace
			if resNS == "" {
				resNS = r.Namespace
			}
			if resNS == repoNamespace {
				matched = append(matched, r)
				break
			}
		}
	}
	return matched, nil
}

// findPoliciesForRepo returns all ImagePolicies that reference the given
// ImageRepository via spec.imageRepositoryRef.
func (d *FluxDiscovery) findPoliciesForRepo(ctx context.Context, repoName, repoNamespace string) ([]imagev1beta2.ImagePolicy, error) {
	list := &imagev1beta2.ImagePolicyList{}
	// Policies are typically in the same namespace as the ImageRepository.
	if err := d.Client.List(ctx, list, client.InNamespace(repoNamespace)); err != nil {
		return nil, err
	}

	var matched []imagev1beta2.ImagePolicy
	for _, p := range list.Items {
		ref := p.Spec.ImageRepositoryRef
		// ref.Namespace is optional; empty means same namespace as policy.
		refNS := ref.Namespace
		if refNS == "" {
			refNS = p.Namespace
		}
		if ref.Name == repoName && refNS == repoNamespace {
			matched = append(matched, p)
		}
	}
	return matched, nil
}

// buildWebhookURLs constructs full webhook URLs from base URL + Receiver webhook path.
func (d *FluxDiscovery) buildWebhookURLs(r *notificationv1.Receiver) []string {
	if r.Status.WebhookPath == "" {
		return nil
	}
	base := strings.TrimRight(d.WebhookBaseURL, "/")
	path := r.Status.WebhookPath
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return []string{base + path}
}

// readToken fetches the bearer token from the Kubernetes secret referenced
// by the Receiver's secretRef. Flux stores the token under the "token" key.
func (d *FluxDiscovery) readToken(ctx context.Context, secretName, namespace string) (string, error) {
	secret := &corev1.Secret{}
	if err := d.Client.Get(ctx, client.ObjectKey{Name: secretName, Namespace: namespace}, secret); err != nil {
		return "", err
	}
	if val, ok := secret.Data["token"]; ok {
		return string(val), nil
	}
	return "", fmt.Errorf("secret %s/%s has no 'token' key", namespace, secretName)
}

// extractRegex returns the tag filter pattern from the first ImagePolicy
// that has one defined. Returns empty string when none is found.
func extractRegex(policies []imagev1beta2.ImagePolicy) string {
	for _, p := range policies {
		if p.Spec.FilterTags != nil && p.Spec.FilterTags.Pattern != "" {
			return p.Spec.FilterTags.Pattern
		}
	}
	return ""
}

// extractECRRepoName parses an ECR image reference and returns the repository
// path segment (everything after the registry host).
//
// Examples:
//
//	"123456.dkr.ecr.us-east-1.amazonaws.com/my-repo"        → "my-repo"
//	"123456.dkr.ecr.us-east-1.amazonaws.com/team/my-repo"   → "team/my-repo"
//	"123456.dkr.ecr.us-east-1.amazonaws.com/my-repo:latest" → "my-repo"
func extractECRRepoName(image string) (string, error) {
	// Strip tag or digest.
	image = strings.SplitN(image, ":", 2)[0]
	image = strings.SplitN(image, "@", 2)[0]

	if !strings.Contains(image, ".dkr.ecr.") || !strings.Contains(image, ".amazonaws.com") {
		return "", fmt.Errorf("image %q is not an ECR image (missing .dkr.ecr. or .amazonaws.com)", image)
	}

	// Split on the first "/" to separate host from repo path.
	idx := strings.Index(image, "/")
	if idx == -1 || idx == len(image)-1 {
		return "", fmt.Errorf("cannot extract repository name from ECR image %q", image)
	}
	return image[idx+1:], nil
}
