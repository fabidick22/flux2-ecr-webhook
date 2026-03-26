package aws

import (
	"testing"

	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
)

func TestBuildLambdaZip(t *testing.T) {
	data, err := buildLambdaZip()
	if err != nil {
		t.Fatalf("buildLambdaZip() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("buildLambdaZip() returned empty data")
	}
	// ZIP magic number: PK\x03\x04
	if data[0] != 0x50 || data[1] != 0x4b {
		t.Errorf("expected ZIP magic number, got %x %x", data[0], data[1])
	}
}

func TestBuildEventPattern(t *testing.T) {
	tests := []struct {
		name      string
		repos     []string
		wantRepos bool
	}{
		{
			name:      "no repos",
			repos:     nil,
			wantRepos: false,
		},
		{
			name:      "with repos",
			repos:     []string{"my-repo", "other-repo"},
			wantRepos: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := buildEventPattern(tt.repos)
			if pattern == "" {
				t.Fatal("empty pattern")
			}
			if tt.wantRepos {
				if !contains(pattern, "my-repo") {
					t.Errorf("pattern should contain 'my-repo': %s", pattern)
				}
			} else {
				if contains(pattern, "repository-name") {
					t.Errorf("pattern should not contain 'repository-name' filter: %s", pattern)
				}
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"ResourceNotFoundException: secret not found", true},
		{"NoSuchEntity: role does not exist", true},
		{"AccessDenied: not allowed", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isNotFound(errorWithMsg(tt.msg)); got != tt.want {
			t.Errorf("isNotFound(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestAlreadyExists(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"ResourceExistsException: already exists", true},
		{"EntityAlreadyExists: role exists", true},
		{"QueueAlreadyExists: queue exists", true},
		{"ResourceNotFoundException: not found", false},
	}
	for _, tt := range tests {
		if got := alreadyExists(errorWithMsg(tt.msg)); got != tt.want {
			t.Errorf("alreadyExists(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestNamingHelpers(t *testing.T) {
	p := &Provider{cfg: Config{AppName: "my-app", LambdaName: "my-fn", SQSName: "my-q"}}

	if got := p.roleName(); got != "my-app-lambda-role" {
		t.Errorf("roleName() = %q", got)
	}
	if got := p.eventRuleName(); got != "my-app-ecr-push" {
		t.Errorf("eventRuleName() = %q", got)
	}
	if got := p.repoMappingSecretName(); got != "my-app-repo-mapping" {
		t.Errorf("repoMappingSecretName() = %q", got)
	}
	if got := p.tokenSecretName(); got != "my-app-token" {
		t.Errorf("tokenSecretName() = %q", got)
	}
}

func TestClusterID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"with scheme and host", "https://gitops.stg.wokiapp.com", "gitops.stg.wokiapp.com"},
		{"with path", "https://gitops.prod.wokiapp.com/some/path", "gitops.prod.wokiapp.com"},
		{"with port", "https://flux.example.com:8443", "flux.example.com"},
		{"empty url", "", ""},
		{"http scheme", "http://flux.local", "flux.local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Provider{cfg: Config{WebhookBaseURL: tt.url}}
			if got := p.clusterID(); got != tt.want {
				t.Errorf("clusterID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrefixMapping(t *testing.T) {
	t.Run("with cluster ID", func(t *testing.T) {
		m := mapping.RepoMapping{
			"repo-a": mapping.RepoEntry{
				"recv-a": mapping.WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
			},
		}
		result := prefixMapping(m, "stg.example.com")
		if _, ok := result["repo-a"]["stg.example.com::recv-a"]; !ok {
			t.Error("expected prefixed key")
		}
		if _, ok := result["repo-a"]["recv-a"]; ok {
			t.Error("unprefixed key should not exist")
		}
	})

	t.Run("empty cluster ID returns original", func(t *testing.T) {
		m := mapping.RepoMapping{
			"repo-a": mapping.RepoEntry{
				"recv-a": mapping.WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
			},
		}
		result := prefixMapping(m, "")
		if _, ok := result["repo-a"]["recv-a"]; !ok {
			t.Error("expected unprefixed key")
		}
	})
}

func TestCountEntries(t *testing.T) {
	m := mapping.RepoMapping{
		"repo-a": mapping.RepoEntry{
			"recv-1": mapping.WebhookEntry{},
			"recv-2": mapping.WebhookEntry{},
		},
		"repo-b": mapping.RepoEntry{
			"recv-3": mapping.WebhookEntry{},
		},
	}
	if got := countEntries(m); got != 3 {
		t.Errorf("countEntries() = %d, want 3", got)
	}
}

// errorWithMsg implements error for testing.
type errMsg string

func errorWithMsg(msg string) error {
	if msg == "" {
		return nil
	}
	return errMsg(msg)
}

func (e errMsg) Error() string { return string(e) }
