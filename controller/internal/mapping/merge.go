package mapping

import (
	"sort"
	"strings"
)

// MergeMapping merges local entries into an existing mapping.
// It removes all entries belonging to clusterID from existing,
// then adds all entries from local. This ensures that:
//   - Other clusters' entries are preserved
//   - This cluster's stale entries are cleaned up
//   - Current entries from this cluster are added
func MergeMapping(existing, local RepoMapping, clusterID string) RepoMapping {
	prefix := clusterID + KeySeparator

	// Deep copy existing.
	merged := make(RepoMapping, len(existing))
	for repo, entries := range existing {
		newEntries := make(RepoEntry, len(entries))
		for k, v := range entries {
			newEntries[k] = v
		}
		merged[repo] = newEntries
	}

	// Remove this cluster's old entries.
	for repo, entries := range merged {
		for key := range entries {
			if strings.HasPrefix(key, prefix) {
				delete(entries, key)
			}
		}
		if len(entries) == 0 {
			delete(merged, repo)
		}
	}

	// Add current local entries.
	for repo, entries := range local {
		if _, ok := merged[repo]; !ok {
			merged[repo] = make(RepoEntry, len(entries))
		}
		for k, v := range entries {
			merged[repo][k] = v
		}
	}

	return merged
}

// ExtractRepoNames returns all ECR repository names from the mapping, sorted.
func ExtractRepoNames(m RepoMapping) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ClusterIDFromKey extracts the cluster ID from a prefixed receiver key.
// Returns empty string for keys without a separator.
func ClusterIDFromKey(key string) string {
	idx := strings.Index(key, KeySeparator)
	if idx < 0 {
		return ""
	}
	return key[:idx]
}
