// Package gcp will implement the CloudProvider interface for Google Cloud Platform.
// This is a placeholder — contributions welcome.
package gcp

import (
	"context"

	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud"
	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
)

// Provider is a stub for GCP support.
type Provider struct{}

func (p *Provider) EnsureInfrastructure(ctx context.Context) error {
	return cloud.ErrProviderNotSupported
}

func (p *Provider) SyncMapping(ctx context.Context, repoMapping mapping.RepoMapping) error {
	return cloud.ErrProviderNotSupported
}

func (p *Provider) Cleanup(ctx context.Context) error {
	return cloud.ErrProviderNotSupported
}
