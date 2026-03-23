// Package aws implements the CloudProvider interface for Amazon Web Services.
package aws

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed lambdafn/app.py
var lambdaSource []byte

// managedByTag is the tag key used to identify resources managed by this controller.
const managedByTag = "managed-by"

// managedByValue is the tag value for resources managed by this controller.
const managedByValue = "flux2-webhook"

// typeTagKey is the tag key used to distinguish resource types during tag discovery.
const typeTagKey = "flux2-webhook/type"

// Config holds AWS-specific configuration.
type Config struct {
	Region        string
	AppName       string
	LambdaName    string
	LambdaRuntime string
	LambdaTimeout int32
	SQSName       string

	// ManageInfra controls whether the controller creates/deletes AWS resources.
	// When false, only SyncMapping runs (resources must already exist).
	ManageInfra bool

	// Explicit resource references (override tag discovery and naming convention).
	ExistingRepoMappingSecret string
	ExistingSQSQueue          string
	ExistingEventRule         string
}

// resolvedNames holds the actual resource names used by SyncMapping,
// determined by: explicit config → tag discovery → naming convention.
type resolvedNames struct {
	repoMappingSecret string
	sqsQueue          string
	eventRule         string
}

// Provider implements cloud.CloudProvider for AWS.
type Provider struct {
	cfg Config
	sm  *secretsmanager.Client
	eb  *eventbridge.Client
	lm  *lambda.Client
	sq  *sqs.Client
	im  *iam.Client
	cw  *cloudwatchlogs.Client
	st  *sts.Client
	tag *resourcegroupstaggingapi.Client

	resolved   *resolvedNames
	resolveMu  sync.Mutex
}

// NewProvider creates an AWS CloudProvider from the given config.
func NewProvider(ctx context.Context, cfg Config) (*Provider, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewAdaptiveMode(func(o *retry.AdaptiveModeOptions) {
				o.StandardOptions = append(o.StandardOptions, func(so *retry.StandardOptions) {
					so.MaxAttempts = 5
				})
			})
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Provider{
		cfg: cfg,
		sm:  secretsmanager.NewFromConfig(awsCfg),
		eb:  eventbridge.NewFromConfig(awsCfg),
		lm:  lambda.NewFromConfig(awsCfg),
		sq:  sqs.NewFromConfig(awsCfg),
		im:  iam.NewFromConfig(awsCfg),
		cw:  cloudwatchlogs.NewFromConfig(awsCfg),
		st:  sts.NewFromConfig(awsCfg),
		tag: resourcegroupstaggingapi.NewFromConfig(awsCfg),
	}, nil
}

// Validate checks the AWS identity and, when ManageInfra is false,
// verifies that the required resources exist and are accessible.
func (p *Provider) Validate(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("aws")

	// Always log the current AWS identity for cross-account visibility.
	identity, err := p.st.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("verifying AWS identity: %w", err)
	}
	logger.Info("AWS identity verified",
		"account", *identity.Account,
		"arn", *identity.Arn,
		"manageInfrastructure", p.cfg.ManageInfra,
	)

	// Resolve resource names (explicit config → tag discovery → naming convention).
	if err := p.resolveResourceNames(ctx); err != nil {
		return fmt.Errorf("resolving resource names: %w", err)
	}

	// When infra is managed externally, verify resources are accessible.
	if !p.cfg.ManageInfra {
		if err := p.validateExternalResources(ctx); err != nil {
			return fmt.Errorf("validating external resources in account %s: %w", *identity.Account, err)
		}
		logger.Info("external resources validated",
			"secret", p.resolved.repoMappingSecret,
			"queue", p.resolved.sqsQueue,
			"rule", p.resolved.eventRule,
		)
	}

	return nil
}

// resolveResourceNames determines the actual names for the 3 resources SyncMapping needs.
// Priority: explicit config → tag discovery → naming convention.
func (p *Provider) resolveResourceNames(ctx context.Context) error {
	p.resolveMu.Lock()
	defer p.resolveMu.Unlock()
	if p.resolved != nil {
		return nil
	}

	names := &resolvedNames{
		repoMappingSecret: p.repoMappingSecretName(),
		sqsQueue:          p.cfg.SQSName,
		eventRule:         p.eventRuleName(),
	}

	// 1. Explicit config overrides everything.
	if p.cfg.ExistingRepoMappingSecret != "" {
		names.repoMappingSecret = p.cfg.ExistingRepoMappingSecret
	}
	if p.cfg.ExistingSQSQueue != "" {
		names.sqsQueue = p.cfg.ExistingSQSQueue
	}
	if p.cfg.ExistingEventRule != "" {
		names.eventRule = p.cfg.ExistingEventRule
	}

	// 2. Tag discovery for any names not explicitly set.
	if p.cfg.ExistingRepoMappingSecret == "" || p.cfg.ExistingSQSQueue == "" || p.cfg.ExistingEventRule == "" {
		discovered, err := p.discoverByTags(ctx)
		if err != nil {
			log.FromContext(ctx).WithName("aws").Info("tag discovery skipped", "reason", err.Error())
		} else if discovered != nil {
			if p.cfg.ExistingRepoMappingSecret == "" && discovered.repoMappingSecret != "" {
				names.repoMappingSecret = discovered.repoMappingSecret
			}
			if p.cfg.ExistingSQSQueue == "" && discovered.sqsQueue != "" {
				names.sqsQueue = discovered.sqsQueue
			}
			if p.cfg.ExistingEventRule == "" && discovered.eventRule != "" {
				names.eventRule = discovered.eventRule
			}
		}
	}

	logger := log.FromContext(ctx).WithName("aws")
	logger.Info("resource names resolved",
		"secret", names.repoMappingSecret,
		"queue", names.sqsQueue,
		"rule", names.eventRule,
	)
	p.resolved = names
	return nil
}

// validateExternalResources checks that the required resources exist.
func (p *Provider) validateExternalResources(ctx context.Context) error {
	// Check SecretsManager secret.
	if _, err := p.sm.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: ptr(p.resolved.repoMappingSecret),
	}); err != nil {
		return fmt.Errorf("secret %q not accessible — verify IRSA role targets the correct account: %w",
			p.resolved.repoMappingSecret, err)
	}

	// Check SQS queue.
	if _, err := p.sq.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: ptr(p.resolved.sqsQueue),
	}); err != nil {
		return fmt.Errorf("SQS queue %q not accessible — verify IRSA role targets the correct account: %w",
			p.resolved.sqsQueue, err)
	}

	// Check EventBridge rule.
	if _, err := p.eb.DescribeRule(ctx, &eventbridge.DescribeRuleInput{
		Name: ptr(p.resolved.eventRule),
	}); err != nil {
		return fmt.Errorf("EventBridge rule %q not accessible — verify IRSA role targets the correct account: %w",
			p.resolved.eventRule, err)
	}

	return nil
}

// EnsureInfrastructure creates all required AWS resources idempotently.
func (p *Provider) EnsureInfrastructure(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("aws")

	// 1. IAM role for Lambda
	roleArn, err := p.ensureRole(ctx)
	if err != nil {
		return fmt.Errorf("ensuring IAM role: %w", err)
	}
	logger.Info("IAM role ready", "arn", roleArn)

	// 2. SQS queue
	queueURL, queueArn, err := p.ensureQueue(ctx)
	if err != nil {
		return fmt.Errorf("ensuring SQS queue: %w", err)
	}
	logger.Info("SQS queue ready", "url", queueURL)

	// 3. SecretsManager secrets
	if err := p.ensureSecrets(ctx); err != nil {
		return fmt.Errorf("ensuring secrets: %w", err)
	}
	logger.Info("SecretsManager secrets ready")

	// 4. Lambda function
	if err := p.ensureLambda(ctx, roleArn, queueArn); err != nil {
		return fmt.Errorf("ensuring Lambda: %w", err)
	}
	logger.Info("Lambda function ready")

	// 5. CloudWatch Log Group for Lambda
	if err := p.ensureLogGroup(ctx); err != nil {
		return fmt.Errorf("ensuring CloudWatch log group: %w", err)
	}
	logger.Info("CloudWatch log group ready")

	// 6. EventBridge rule + target (initially empty repo filter)
	if err := p.ensureEventRule(ctx, queueArn, nil); err != nil {
		return fmt.Errorf("ensuring EventBridge rule: %w", err)
	}
	logger.Info("EventBridge rule ready")

	return nil
}

// SyncMapping updates the repo_mapping secret and EventBridge rule filter.
// Uses resolved resource names (from explicit config, tag discovery, or naming convention).
func (p *Provider) SyncMapping(ctx context.Context, repoMapping mapping.RepoMapping) error {
	logger := log.FromContext(ctx).WithName("aws")

	// Ensure names are resolved (idempotent, runs once).
	if err := p.resolveResourceNames(ctx); err != nil {
		return fmt.Errorf("resolving resource names: %w", err)
	}

	// Persist mapping to SecretsManager.
	data, err := json.Marshal(repoMapping)
	if err != nil {
		return fmt.Errorf("marshalling repo mapping: %w", err)
	}
	if err := p.updateSecret(ctx, p.resolved.repoMappingSecret, string(data)); err != nil {
		return fmt.Errorf("updating repo mapping secret: %w", err)
	}
	logger.Info("repo mapping persisted", "ecrRepos", len(repoMapping))

	// Update EventBridge filter with discovered ECR repo names.
	repoNames := make([]string, 0, len(repoMapping))
	for name := range repoMapping {
		repoNames = append(repoNames, name)
	}

	// We need the queue ARN to set the target.
	_, queueArn, err := p.getQueueInfoByName(ctx, p.resolved.sqsQueue)
	if err != nil {
		return fmt.Errorf("getting queue ARN for EventBridge: %w", err)
	}

	if err := p.ensureEventRuleByName(ctx, p.resolved.eventRule, queueArn, repoNames); err != nil {
		return fmt.Errorf("updating EventBridge rule: %w", err)
	}
	logger.Info("EventBridge rule updated", "repos", repoNames)

	return nil
}

// Cleanup removes all AWS resources created by the controller.
func (p *Provider) Cleanup(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("aws-cleanup")

	// EventBridge: remove target, then rule.
	if err := p.deleteEventRule(ctx); err != nil {
		logger.Error(err, "failed to delete EventBridge rule (continuing)")
	}

	// Lambda: remove event source mapping, then function.
	if err := p.deleteLambda(ctx); err != nil {
		logger.Error(err, "failed to delete Lambda (continuing)")
	}

	// CloudWatch Log Group.
	if err := p.deleteLogGroup(ctx); err != nil {
		logger.Error(err, "failed to delete CloudWatch log group (continuing)")
	}

	// SQS queue.
	if err := p.deleteQueue(ctx); err != nil {
		logger.Error(err, "failed to delete SQS queue (continuing)")
	}

	// IAM: detach inline policies, then role.
	if err := p.deleteRole(ctx); err != nil {
		logger.Error(err, "failed to delete IAM role (continuing)")
	}

	// SecretsManager: both repo-mapping and token secrets.
	for _, name := range []string{p.repoMappingSecretName(), p.tokenSecretName()} {
		if err := p.deleteSecret(ctx, name); err != nil {
			logger.Error(err, "failed to delete secret (continuing)", "name", name)
		}
	}

	logger.Info("cleanup complete")
	return nil
}

// -- naming helpers --

func (p *Provider) roleName() string              { return p.cfg.AppName + "-lambda-role" }
func (p *Provider) eventRuleName() string          { return p.cfg.AppName + "-ecr-push" }
func (p *Provider) eventTargetID() string          { return p.cfg.AppName + "-sqs-target" }
func (p *Provider) repoMappingSecretName() string  { return p.cfg.AppName + "-repo-mapping" }
func (p *Provider) tokenSecretName() string        { return p.cfg.AppName + "-token" }
func (p *Provider) logGroupName() string           { return "/aws/lambda/" + p.cfg.LambdaName }

// getQueueInfoByName is like getQueueInfo but accepts an explicit queue name.
func (p *Provider) getQueueInfoByName(ctx context.Context, queueName string) (string, string, error) {
	urlOut, err := p.sq.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: ptr(queueName)})
	if err != nil {
		return "", "", err
	}
	attrOut, err := p.sq.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       urlOut.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return *urlOut.QueueUrl, "", err
	}
	return *urlOut.QueueUrl, attrOut.Attributes["QueueArn"], nil
}

// ensureEventRuleByName is like ensureEventRule but accepts an explicit rule name.
func (p *Provider) ensureEventRuleByName(ctx context.Context, ruleName, queueArn string, repoNames []string) error {
	pattern := buildEventPattern(repoNames)

	putOut, err := p.eb.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         ptr(ruleName),
		EventPattern: ptr(pattern),
		State:        ebtypes.RuleStateEnabled,
	})
	if err != nil {
		return fmt.Errorf("putting EventBridge rule: %w", err)
	}
	ruleArn := *putOut.RuleArn

	if queueArn != "" {
		_, err = p.eb.PutTargets(ctx, &eventbridge.PutTargetsInput{
			Rule: ptr(ruleName),
			Targets: []ebtypes.Target{
				{
					Id:  ptr(p.eventTargetID()),
					Arn: ptr(queueArn),
				},
			},
		})
		if err != nil {
			return fmt.Errorf("putting EventBridge target: %w", err)
		}

		queueURL, _, _ := p.getQueueInfoByName(ctx, p.resolved.sqsQueue)
		if queueURL != "" {
			_ = p.setQueuePolicy(ctx, queueURL, queueArn, ruleArn)
		}
	}

	return nil
}

// buildLambdaZip creates an in-memory ZIP with the embedded Python source.
func buildLambdaZip() ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	f, err := w.Create("app.py")
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(lambdaSource); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// sqsPolicyForEventBridge returns an SQS policy that allows EventBridge to send messages.
func sqsPolicyForEventBridge(queueArn, ruleArn string) string {
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "events.amazonaws.com"},
    "Action": "sqs:SendMessage",
    "Resource": %q,
    "Condition": {"ArnEquals": {"aws:SourceArn": %q}}
  }]
}`, queueArn, ruleArn)
}

// lambdaTrustPolicy is the assume-role policy for Lambda.
const lambdaTrustPolicy = `{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"Service": "lambda.amazonaws.com"},
    "Action": "sts:AssumeRole"
  }]
}`

// lambdaSQSPolicy returns an inline policy granting SQS read access.
func lambdaSQSPolicy(queueArn string) string {
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"],
    "Resource": %q
  }]
}`, queueArn)
}

// lambdaSecretsPolicy returns an inline policy granting SecretsManager read access.
func lambdaSecretsPolicy(secretArns []string) string {
	arns, _ := json.Marshal(secretArns)
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": "secretsmanager:GetSecretValue",
    "Resource": %s
  }]
}`, string(arns))
}

// lambdaLogsPolicy returns an inline policy for CloudWatch Logs.
func lambdaLogsPolicy() string {
	return `{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"],
    "Resource": "arn:aws:logs:*:*:*"
  }]
}`
}

// isNotFound checks common AWS "not found" error codes.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	// All AWS SDK v2 errors embed a code in the message.
	msg := err.Error()
	for _, code := range []string{
		"ResourceNotFoundException",
		"NoSuchEntity",
		"NotFoundException",
		"ResourceNotFoundFault",
		"404",
	} {
		if contains(msg, code) {
			return true
		}
	}
	return false
}

// alreadyExists checks common AWS "already exists" error codes.
func alreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{
		"ResourceExistsException",
		"EntityAlreadyExists",
		"QueueAlreadyExists",
		"ResourceConflictException",
	} {
		if contains(msg, code) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// isScheduledForDeletion detects SecretsManager's error when a secret
// is still pending deletion after a recent ForceDeleteWithoutRecovery.
func isScheduledForDeletion(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "scheduled for deletion")
}

// isQueueDeletedRecently detects SQS's error when recreating a queue
// within 60 seconds of deletion.
func isQueueDeletedRecently(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "QueueDeletedRecently")
}

// isInvalidParameterValue detects Lambda's InvalidParameterValueException,
// commonly returned when the IAM role is not yet propagated.
func isInvalidParameterValue(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "InvalidParameterValueException")
}

// ptr returns a pointer to the given string.
func ptr(s string) *string { return aws.String(s) }
