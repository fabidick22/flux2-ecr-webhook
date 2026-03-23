// Package cloud defines the CloudProvider interface used by the reconciler
// to manage cloud infrastructure without coupling to a specific vendor.
package cloud

import (
	"context"
	"fmt"

	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
)

// CloudProvider abstracts the cloud operations needed by the controller.
// Implementations exist for AWS; GCP and Azure are planned.
type CloudProvider interface {
	// Validate checks the cloud identity and, when infrastructure is managed
	// externally, verifies that the required resources exist and are accessible.
	// Should be called at startup before the reconciler starts.
	Validate(ctx context.Context) error

	// EnsureInfrastructure creates all cloud resources if they don't already
	// exist (Lambda, SQS, IAM, EventBridge rule, SecretsManager secret).
	// Must be idempotent.
	EnsureInfrastructure(ctx context.Context) error

	// SyncMapping persists the repo_mapping to the cloud secret store and
	// updates the event rule filter so only relevant ECR repos trigger events.
	SyncMapping(ctx context.Context, repoMapping mapping.RepoMapping) error

	// Cleanup removes all cloud resources created by the controller.
	// Ignores resources that no longer exist.
	Cleanup(ctx context.Context) error
}

// ErrProviderNotSupported is returned for unimplemented cloud providers.
var ErrProviderNotSupported = fmt.Errorf("cloud provider not supported")
