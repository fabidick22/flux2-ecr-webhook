// Package mapping converts discovered Flux resource data into the repo_mapping
// structure consumed by the Lambda function stored in AWS SecretsManager.
package mapping

import "github.com/fabidick22/flux2-ecr-webhook/internal/discovery"

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
// Matches the inner object of the Lambda's repo_mapping:
//
//	{ "<receiver-name>": WebhookEntry }
type RepoEntry map[string]WebhookEntry

// RepoMapping is the top-level structure persisted in AWS SecretsManager
// and read by the Lambda function on every invocation.
//
//	{ "<ecr-repo-name>": RepoEntry }
type RepoMapping map[string]RepoEntry

// Build converts a flat slice of ImageInfo values (produced by the discovery
// package) into a RepoMapping. Multiple ImageInfo entries for the same ECR
// repo are merged under separate receiver-name keys.
func Build(infos []discovery.ImageInfo) RepoMapping {
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

		result[info.ECRRepoName][info.ReceiverName] = entry
	}

	return result
}
