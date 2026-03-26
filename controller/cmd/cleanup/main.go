package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud"
	"github.com/fabidick22/flux2-ecr-webhook/internal/cloud/aws"
	"github.com/fabidick22/flux2-ecr-webhook/internal/config"
)

func main() {
	cfg := config.FromEnv()

	ctx := context.Background()
	provider, err := newCloudProvider(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating cloud provider: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("starting cloud resource cleanup...")
	if err := provider.Cleanup(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("cleanup complete")
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
