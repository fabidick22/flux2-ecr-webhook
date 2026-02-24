package controller

import (
	"context"
	"time"

	imagev1beta2 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	notificationv1 "github.com/fluxcd/notification-controller/api/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/fabidick22/flux2-ecr-webhook/internal/config"
	"github.com/fabidick22/flux2-ecr-webhook/internal/discovery"
	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
)

// ImageRepositorySyncReconciler watches Flux ImageRepository resources and
// automatically keeps AWS resources (SecretsManager repo_mapping and
// EventBridge rules) in sync — no manual Terraform intervention required.
type ImageRepositorySyncReconciler struct {
	client.Client
	Config         config.Config
	ResyncInterval time.Duration
}

// RBAC markers — used by controller-gen to generate ClusterRole manifests.
//
//+kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagerepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagepolicies,verbs=get;list;watch
//+kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ImageRepositorySyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("trigger", req.NamespacedName)

	// 1. List all ImageRepositories that the controller manages.
	//    We always rebuild the complete mapping, regardless of which specific
	//    resource triggered this reconciliation.
	repos, err := r.listManagedRepos(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.Info("reconciling repo mapping", "managedRepos", len(repos))

	// 2. For each repository, discover its associated Flux resources.
	disc := &discovery.FluxDiscovery{
		Client:         r.Client,
		FluxNamespace:  r.Config.FluxNamespace,
		WebhookBaseURL: r.Config.WebhookBaseURL,
	}

	var allInfos []discovery.ImageInfo
	for i := range repos {
		infos, err := disc.DiscoverForImageRepository(ctx, &repos[i])
		if err != nil {
			// Log and continue — a single bad repo should not block all others.
			logger.Error(err, "skipping ImageRepository due to discovery error",
				"name", repos[i].Name, "namespace", repos[i].Namespace)
			continue
		}
		allInfos = append(allInfos, infos...)
	}

	// 3. Build the complete repo_mapping from all discovered data.
	repoMapping := mapping.Build(allInfos)
	logger.Info("repo mapping built", "ecrRepos", len(repoMapping))

	// TODO (Phase 3): persist repoMapping to AWS SecretsManager and
	//   sync the EventBridge rule's ECR repository list.

	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}

// listManagedRepos lists all ImageRepository resources that pass the configured
// namespace filters and do not carry the exclusion annotation.
func (r *ImageRepositorySyncReconciler) listManagedRepos(ctx context.Context) ([]imagev1beta2.ImageRepository, error) {
	list := &imagev1beta2.ImageRepositoryList{}

	var opts []client.ListOption
	if !r.Config.ScanAllNamespaces && len(r.Config.IncludeNamespaces) > 0 {
		// When not scanning all namespaces, query each included namespace
		// individually and merge the results.
		return r.listFromNamespaces(ctx, r.Config.IncludeNamespaces)
	}

	if err := r.List(ctx, list, opts...); err != nil {
		return nil, err
	}

	return r.filterRepos(list.Items), nil
}

// listFromNamespaces lists ImageRepositories from specific namespaces only.
func (r *ImageRepositorySyncReconciler) listFromNamespaces(ctx context.Context, namespaces []string) ([]imagev1beta2.ImageRepository, error) {
	var all []imagev1beta2.ImageRepository
	for _, ns := range namespaces {
		list := &imagev1beta2.ImageRepositoryList{}
		if err := r.List(ctx, list, client.InNamespace(ns)); err != nil {
			return nil, err
		}
		all = append(all, list.Items...)
	}
	return r.filterRepos(all), nil
}

// filterRepos removes repositories in excluded namespaces or annotated to skip.
func (r *ImageRepositorySyncReconciler) filterRepos(repos []imagev1beta2.ImageRepository) []imagev1beta2.ImageRepository {
	excluded := make(map[string]bool, len(r.Config.ExcludeNamespaces))
	for _, ns := range r.Config.ExcludeNamespaces {
		excluded[ns] = true
	}

	var result []imagev1beta2.ImageRepository
	for _, repo := range repos {
		if excluded[repo.Namespace] {
			continue
		}
		if repo.Annotations[r.Config.ExcludeAnnotation] == "true" {
			continue
		}
		result = append(result, repo)
	}
	return result
}

// SetupWithManager registers the reconciler and adds secondary watches so that
// changes to Receiver or ImagePolicy also trigger reconciliation — not just
// the periodic resync. This is the event-driven part of the controller.
func (r *ImageRepositorySyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Primary watch: an ImageRepository change triggers reconciliation directly.
		For(&imagev1beta2.ImageRepository{}).
		// Secondary watch: when a Receiver changes, re-reconcile all ImageRepositories
		// it references (webhook URL or token may have changed).
		Watches(
			&notificationv1.Receiver{},
			handler.EnqueueRequestsFromMapFunc(r.receiverToImageRepos),
		).
		// Secondary watch: when an ImagePolicy changes, re-reconcile the
		// referenced ImageRepository (regex/tag filter may have changed).
		Watches(
			&imagev1beta2.ImagePolicy{},
			handler.EnqueueRequestsFromMapFunc(r.policyToImageRepo),
		).
		Complete(r)
}

// receiverToImageRepos maps a Receiver change to the ImageRepository reconcile
// requests it should trigger.
func (r *ImageRepositorySyncReconciler) receiverToImageRepos(_ context.Context, obj client.Object) []reconcile.Request {
	receiver, ok := obj.(*notificationv1.Receiver)
	if !ok {
		return nil
	}

	var requests []reconcile.Request
	for _, res := range receiver.Spec.Resources {
		if res.Kind != "ImageRepository" {
			continue
		}
		ns := res.Namespace
		if ns == "" {
			ns = receiver.Namespace
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: res.Name, Namespace: ns},
		})
	}
	return requests
}

// policyToImageRepo maps an ImagePolicy change to the ImageRepository it
// references, so that a changed tag filter is picked up immediately.
func (r *ImageRepositorySyncReconciler) policyToImageRepo(_ context.Context, obj client.Object) []reconcile.Request {
	policy, ok := obj.(*imagev1beta2.ImagePolicy)
	if !ok {
		return nil
	}

	ref := policy.Spec.ImageRepositoryRef
	ns := ref.Namespace
	if ns == "" {
		ns = policy.Namespace
	}
	return []reconcile.Request{
		{NamespacedName: types.NamespacedName{Name: ref.Name, Namespace: ns}},
	}
}
