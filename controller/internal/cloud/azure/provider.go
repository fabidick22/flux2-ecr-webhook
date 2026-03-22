// Package azure will implement the CloudProvider interface for Microsoft Azure.
// This is a placeholder — contributions welcome.
package azure

import (
	"context"

	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud"
	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
)

// Provider is a stub for Azure support.
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
