package mapping

import (
	"testing"

	"github.com/fabidick22/flux2-ecr-webhook/internal/discovery"
)

func TestBuild(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		result := Build(nil)
		if len(result) != 0 {
			t.Errorf("Build(nil) returned %d entries, want 0", len(result))
		}
	})

	t.Run("single repo single receiver", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/abc"},
				Token:        "secret-token",
				Regex:        "prod-.*",
				ReceiverName: "receiver-prod",
			},
		}
		result := Build(infos)

		if len(result) != 1 {
			t.Fatalf("expected 1 ECR repo, got %d", len(result))
		}
		entry, ok := result["my-repo"]
		if !ok {
			t.Fatal("missing 'my-repo' key")
		}
		if len(entry) != 1 {
			t.Fatalf("expected 1 receiver entry, got %d", len(entry))
		}
		webhook, ok := entry["receiver-prod"]
		if !ok {
			t.Fatal("missing 'receiver-prod' key")
		}
		if len(webhook.Webhook) != 1 || webhook.Webhook[0] != "https://flux.example.com/hook/abc" {
			t.Errorf("unexpected webhook URLs: %v", webhook.Webhook)
		}
		if webhook.Token != "secret-token" {
			t.Errorf("token = %q, want %q", webhook.Token, "secret-token")
		}
		if webhook.Regex != "prod-.*" {
			t.Errorf("regex = %q, want %q", webhook.Regex, "prod-.*")
		}
	})

	t.Run("single repo multiple receivers", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/abc"},
				Token:        "token-1",
				Regex:        "prod-.*",
				ReceiverName: "receiver-prod",
			},
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/xyz"},
				Token:        "token-2",
				Regex:        "stg-.*",
				ReceiverName: "receiver-stg",
			},
		}
		result := Build(infos)

		if len(result) != 1 {
			t.Fatalf("expected 1 ECR repo, got %d", len(result))
		}
		entry := result["my-repo"]
		if len(entry) != 2 {
			t.Fatalf("expected 2 receiver entries, got %d", len(entry))
		}
		if _, ok := entry["receiver-prod"]; !ok {
			t.Error("missing 'receiver-prod'")
		}
		if _, ok := entry["receiver-stg"]; !ok {
			t.Error("missing 'receiver-stg'")
		}
	})

	t.Run("multiple repos", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "repo-a",
				WebhookURLs:  []string{"https://flux.example.com/hook/1"},
				ReceiverName: "recv-a",
			},
			{
				ECRRepoName:  "repo-b",
				WebhookURLs:  []string{"https://flux.example.com/hook/2"},
				ReceiverName: "recv-b",
			},
		}
		result := Build(infos)

		if len(result) != 2 {
			t.Fatalf("expected 2 ECR repos, got %d", len(result))
		}
	})

	t.Run("omits empty token and regex", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/abc"},
				Token:        "",
				Regex:        "",
				ReceiverName: "receiver-prod",
			},
		}
		result := Build(infos)

		webhook := result["my-repo"]["receiver-prod"]
		if webhook.Token != "" {
			t.Errorf("token should be empty, got %q", webhook.Token)
		}
		if webhook.Regex != "" {
			t.Errorf("regex should be empty, got %q", webhook.Regex)
		}
	})
}

func TestBuildWithClusterID(t *testing.T) {
	t.Run("keys have cluster prefix", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/abc"},
				Token:        "token",
				ReceiverName: "receiver-prod",
			},
		}
		result := BuildWithClusterID(infos, "gitops.stg.example.com")
		entry := result["my-repo"]
		if _, ok := entry["gitops.stg.example.com::receiver-prod"]; !ok {
			t.Error("expected prefixed key 'gitops.stg.example.com::receiver-prod'")
		}
		if _, ok := entry["receiver-prod"]; ok {
			t.Error("unprefixed key should not exist")
		}
	})

	t.Run("empty clusterID has no prefix", func(t *testing.T) {
		infos := []discovery.ImageInfo{
			{
				ECRRepoName:  "my-repo",
				WebhookURLs:  []string{"https://flux.example.com/hook/abc"},
				ReceiverName: "receiver-prod",
			},
		}
		result := BuildWithClusterID(infos, "")
		entry := result["my-repo"]
		if _, ok := entry["receiver-prod"]; !ok {
			t.Error("expected unprefixed key 'receiver-prod'")
		}
	})
}
