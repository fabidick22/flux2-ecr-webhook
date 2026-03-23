package mapping

import "testing"

func TestMergeMapping_empty_existing(t *testing.T) {
	local := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::recv-a": WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
		},
	}
	merged := MergeMapping(RepoMapping{}, local, "stg.example.com")
	if len(merged) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(merged))
	}
	if _, ok := merged["repo-a"]["stg.example.com::recv-a"]; !ok {
		t.Error("expected local entry to be present")
	}
}

func TestMergeMapping_single_cluster_replace(t *testing.T) {
	existing := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::old-recv": WebhookEntry{Webhook: []string{"https://stg/hook/old"}},
		},
	}
	local := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::new-recv": WebhookEntry{Webhook: []string{"https://stg/hook/new"}},
		},
	}
	merged := MergeMapping(existing, local, "stg.example.com")
	if _, ok := merged["repo-a"]["stg.example.com::old-recv"]; ok {
		t.Error("old entry should be removed")
	}
	if _, ok := merged["repo-a"]["stg.example.com::new-recv"]; !ok {
		t.Error("new entry should be present")
	}
}

func TestMergeMapping_multi_cluster_preserved(t *testing.T) {
	existing := RepoMapping{
		"repo-a": RepoEntry{
			"prod.example.com::recv-prod": WebhookEntry{Webhook: []string{"https://prod/hook/a"}},
		},
	}
	local := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::recv-stg": WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
		},
	}
	merged := MergeMapping(existing, local, "stg.example.com")
	if _, ok := merged["repo-a"]["prod.example.com::recv-prod"]; !ok {
		t.Error("prod entry should be preserved")
	}
	if _, ok := merged["repo-a"]["stg.example.com::recv-stg"]; !ok {
		t.Error("stg entry should be added")
	}
}

func TestMergeMapping_repo_shared(t *testing.T) {
	existing := RepoMapping{
		"shared-repo": RepoEntry{
			"prod.example.com::api-receiver": WebhookEntry{Webhook: []string{"https://prod/hook/api"}},
		},
	}
	local := RepoMapping{
		"shared-repo": RepoEntry{
			"stg.example.com::api-receiver": WebhookEntry{Webhook: []string{"https://stg/hook/api"}},
		},
	}
	merged := MergeMapping(existing, local, "stg.example.com")
	if len(merged["shared-repo"]) != 2 {
		t.Fatalf("expected 2 entries for shared-repo, got %d", len(merged["shared-repo"]))
	}
}

func TestMergeMapping_cleanup(t *testing.T) {
	existing := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::recv-stg":   WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
			"prod.example.com::recv-prod": WebhookEntry{Webhook: []string{"https://prod/hook/a"}},
		},
	}
	local := RepoMapping{} // stg has no entries anymore
	merged := MergeMapping(existing, local, "stg.example.com")
	if _, ok := merged["repo-a"]["stg.example.com::recv-stg"]; ok {
		t.Error("stg entry should be removed")
	}
	if _, ok := merged["repo-a"]["prod.example.com::recv-prod"]; !ok {
		t.Error("prod entry should be preserved")
	}
}

func TestMergeMapping_empty_repo_removed(t *testing.T) {
	existing := RepoMapping{
		"stg-only-repo": RepoEntry{
			"stg.example.com::recv": WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
		},
	}
	local := RepoMapping{}
	merged := MergeMapping(existing, local, "stg.example.com")
	if _, ok := merged["stg-only-repo"]; ok {
		t.Error("repo with no remaining entries should be removed")
	}
}

func TestMergeMapping_does_not_mutate_existing(t *testing.T) {
	existing := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::recv": WebhookEntry{Webhook: []string{"https://stg/hook/a"}},
		},
	}
	local := RepoMapping{
		"repo-a": RepoEntry{
			"stg.example.com::new-recv": WebhookEntry{Webhook: []string{"https://stg/hook/new"}},
		},
	}
	MergeMapping(existing, local, "stg.example.com")
	// Original existing should not be mutated.
	if _, ok := existing["repo-a"]["stg.example.com::recv"]; !ok {
		t.Error("original existing should not be mutated")
	}
}

func TestExtractRepoNames(t *testing.T) {
	m := RepoMapping{
		"repo-b": RepoEntry{},
		"repo-a": RepoEntry{},
		"repo-c": RepoEntry{},
	}
	names := ExtractRepoNames(m)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "repo-a" || names[1] != "repo-b" || names[2] != "repo-c" {
		t.Errorf("expected sorted names, got %v", names)
	}
}

func TestExtractRepoNames_empty(t *testing.T) {
	names := ExtractRepoNames(RepoMapping{})
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestClusterIDFromKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"stg.example.com::receiver-prod", "stg.example.com"},
		{"receiver-prod", ""},
		{"a::b::c", "a"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ClusterIDFromKey(tt.key); got != tt.want {
			t.Errorf("ClusterIDFromKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
