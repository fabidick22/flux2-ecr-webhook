package controller

import (
	"context"
	"time"

	imagev1beta2 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ImageRepositorySyncReconciler watches Flux ImageRepository resources and
// automatically keeps AWS resources (SecretsManager repo_mapping and
// EventBridge rules) in sync — no manual Terraform intervention needed.
type ImageRepositorySyncReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	ResyncInterval time.Duration
}

// RBAC required by this controller:
//
//+kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagerepositories,verbs=get;list;watch
//+kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagepolicies,verbs=get;list;watch
//+kubebuilder:rbac:groups=notification.toolkit.fluxcd.io,resources=receivers,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ImageRepositorySyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ImageRepository that triggered this reconciliation.
	repo := &imagev1beta2.ImageRepository{}
	if err := r.Get(ctx, req.NamespacedName, repo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip resources annotated with the exclusion annotation.
	// TODO (Phase 2): read annotation key from controller config.
	const skipAnnotation = "ecr-webhook.io/skip"
	if val, ok := repo.Annotations[skipAnnotation]; ok && val == "true" {
		logger.Info("skipping ImageRepository (exclude annotation set)", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, nil
	}

	logger.Info("reconciling ImageRepository", "name", req.Name, "namespace", req.Namespace, "image", repo.Spec.Image)

	// TODO (Phase 2): discover associated ImagePolicy and Receiver,
	//   build repo_mapping, and call aws.UpdateRepoMapping().
	// TODO (Phase 3): provision / update AWS resources.

	return ctrl.Result{RequeueAfter: r.ResyncInterval}, nil
}

func (r *ImageRepositorySyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&imagev1beta2.ImageRepository{}).
		Complete(r)
}
