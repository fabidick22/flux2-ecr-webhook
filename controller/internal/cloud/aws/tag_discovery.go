package aws

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	tagtypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
)

// discoverByTags searches for AWS resources tagged with managed-by=flux2-webhook
// and returns the resolved names for the 3 resources SyncMapping needs.
// Returns nil if no tagged resources are found.
func (p *Provider) discoverByTags(ctx context.Context) (*resolvedNames, error) {
	out, err := p.tag.GetResources(ctx, &resourcegroupstaggingapi.GetResourcesInput{
		TagFilters: []tagtypes.TagFilter{
			{Key: ptr(managedByTag), Values: []string{managedByValue}},
		},
		ResourceTypeFilters: []string{
			"secretsmanager:secret",
			"sqs:queue",
			"events:rule",
		},
	})
	if err != nil {
		return nil, err
	}

	if len(out.ResourceTagMappingList) == 0 {
		return nil, nil
	}

	names := &resolvedNames{}
	for _, r := range out.ResourceTagMappingList {
		arn := *r.ResourceARN
		typeTag := tagValue(r.Tags, typeTagKey)

		switch typeTag {
		case "repo-mapping":
			names.repoMappingSecret = nameFromSecretARN(arn)
		case "queue":
			names.sqsQueue = nameFromSQSARN(arn)
		case "event-rule":
			names.eventRule = nameFromEventRuleARN(arn)
		}
	}

	return names, nil
}

// tagValue returns the value of a tag with the given key, or empty string.
func tagValue(tags []tagtypes.Tag, key string) string {
	for _, t := range tags {
		if t.Key != nil && *t.Key == key && t.Value != nil {
			return *t.Value
		}
	}
	return ""
}

// nameFromSecretARN extracts the secret name from an ARN like:
// arn:aws:secretsmanager:us-east-1:123456:secret:my-secret-AbCdEf
func nameFromSecretARN(arn string) string {
	// Format: arn:aws:secretsmanager:REGION:ACCOUNT:secret:NAME-RANDOM
	parts := strings.SplitN(arn, ":", 7)
	if len(parts) < 7 {
		return ""
	}
	name := parts[6]
	// SecretsManager appends a 6-char random suffix after a hyphen.
	if idx := strings.LastIndex(name, "-"); idx > 0 {
		return name[:idx]
	}
	return name
}

// nameFromSQSARN extracts the queue name from an ARN like:
// arn:aws:sqs:us-east-1:123456:my-queue
func nameFromSQSARN(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return parts[5]
}

// nameFromEventRuleARN extracts the rule name from an ARN like:
// arn:aws:events:us-east-1:123456:rule/my-rule
func nameFromEventRuleARN(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	// Format: rule/rule-name
	rulePart := parts[5]
	if strings.HasPrefix(rulePart, "rule/") {
		return rulePart[5:]
	}
	return rulePart
}
