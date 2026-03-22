package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	imagev1beta2 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	notificationv1 "github.com/fluxcd/notification-controller/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud"
	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud/aws"
	"github.com/fabidick22/flux2-ecr-webhook/internal/config"
	"github.com/fabidick22/flux2-ecr-webhook/internal/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(imagev1beta2.AddToScheme(scheme))
	utilruntime.Must(notificationv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var leaderElect bool
	var resyncInterval time.Duration

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Address the metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Address the health probe endpoint binds to.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election (recommended for multi-replica setups).")
	flag.DurationVar(&resyncInterval, "resync-interval", 0, "Periodic resync interval (overrides RESYNC_INTERVAL env var when set).")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{})))

	// Load all other configuration from env vars injected by the Helm ConfigMap.
	cfg := config.FromEnv()

	// --resync-interval flag overrides the env var when explicitly provided.
	effectiveResync := cfg.ResyncDuration()
	if resyncInterval > 0 {
		effectiveResync = resyncInterval
	}

	setupLog.Info("starting flux2-ecr-webhook controller",
		"fluxNamespace", cfg.FluxNamespace,
		"webhookBaseURL", cfg.WebhookBaseURL,
		"cloudProvider", cfg.CloudProvider,
		"scanAllNamespaces", cfg.ScanAllNamespaces,
		"excludeNamespaces", cfg.ExcludeNamespaces,
		"resyncInterval", effectiveResync,
	)

	// Initialize cloud provider.
	provider, err := newCloudProvider(context.Background(), cfg)
	if err != nil {
		setupLog.Error(err, "unable to create cloud provider")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "flux2-ecr-webhook.fabidick22.github.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.ImageRepositorySyncReconciler{
		Client:         mgr.GetClient(),
		Config:         cfg,
		CloudProvider:  provider,
		ResyncInterval: effectiveResync,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ImageRepositorySync")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func newCloudProvider(ctx context.Context, cfg config.Config) (cloud.CloudProvider, error) {
	switch cfg.CloudProvider {
	case "aws":
		return aws.NewProvider(ctx, aws.Config{
			Region:        cfg.AWSRegion,
			AppName:       cfg.AWSAppName,
			LambdaName:    cfg.AWSLambdaName,
			LambdaRuntime: cfg.AWSLambdaRuntime,
			LambdaTimeout: cfg.AWSLambdaTimeout,
			SQSName:       cfg.AWSSQSName,
		})
	default:
		return nil, fmt.Errorf("unsupported cloud provider: %s", cfg.CloudProvider)
	}
}
