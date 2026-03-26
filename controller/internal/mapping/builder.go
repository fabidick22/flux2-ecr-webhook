// Package mapping converts discovered Flux resource data into the repo_mapping
// structure consumed by the Lambda function stored in AWS SecretsManager.
package mapping

import "github.com/fabidick22/flux2-ecr-webhook/internal/discovery"

// KeySeparator separates the cluster ID from the receiver name in mapping keys.
// Example: "gitops.stg.example.com::api-admin-receiver"
const KeySeparator = "::"

// WebhookEntry mirrors the JSON structure expected by the Lambda function.
//
//	{
//	  "webhook": ["https://flux.example.com/hook/abc123"],
//	  "token":   "my-secret-token",   // omitted when empty
//	  "regex":   "prod-.*"            // omitted when empty (Lambda defaults to ".*")
//	}
type WebhookEntry struct {
	Webhook []string `json:"webhook"`
	Token   string   `json:"token,omitempty"`
	Regex   string   `json:"regex,omitempty"`
}

// RepoEntry maps a receiver name (ID) to its webhook configuration.
// In multi-cluster mode, keys are prefixed with the cluster ID:
//
//	{ "gitops.stg.example.com::receiver-name": WebhookEntry }
type RepoEntry map[string]WebhookEntry

// RepoMapping is the top-level structure persisted in the cloud secret store
// and read by the serverless function on every invocation.
//
//	{ "<ecr-repo-name>": RepoEntry }
type RepoMapping map[string]RepoEntry

// Build converts a flat slice of ImageInfo values (produced by the discovery
// package) into a RepoMapping without cluster ID prefixes.
func Build(infos []discovery.ImageInfo) RepoMapping {
	return BuildWithClusterID(infos, "")
}

// BuildWithClusterID converts ImageInfo values into a RepoMapping with receiver
// keys prefixed by clusterID. When clusterID is empty, keys are unprefixed
// (backward-compatible with single-cluster mode).
func BuildWithClusterID(infos []discovery.ImageInfo, clusterID string) RepoMapping {
	result := make(RepoMapping)

	for _, info := range infos {
		if _, ok := result[info.ECRRepoName]; !ok {
			result[info.ECRRepoName] = make(RepoEntry)
		}

		entry := WebhookEntry{
			Webhook: info.WebhookURLs,
		}
		if info.Token != "" {
			entry.Token = info.Token
		}
		if info.Regex != "" {
			entry.Regex = info.Regex
		}

		key := info.ReceiverName
		if clusterID != "" {
			key = clusterID + KeySeparator + info.ReceiverName
		}
		result[info.ECRRepoName][key] = entry
	}

	return result
}
