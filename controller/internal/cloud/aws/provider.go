// Package aws implements the CloudProvider interface for Amazon Web Services.
package aws

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/fabidick22/flux2-ecr-webhook/internal/mapping"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:embed lambdafn/app.py
var lambdaSource []byte

// Config holds AWS-specific configuration.
type Config struct {
	Region        string
	AppName       string
	LambdaName    string
	LambdaRuntime string
	LambdaTimeout int32
	SQSName       string
}

// Provider implements cloud.CloudProvider for AWS.
type Provider struct {
	cfg Config
	sm  *secretsmanager.Client
	eb  *eventbridge.Client
	lm  *lambda.Client
	sq  *sqs.Client
	im  *iam.Client
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
	}, nil
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

	// 5. EventBridge rule + target (initially empty repo filter)
	if err := p.ensureEventRule(ctx, queueArn, nil); err != nil {
		return fmt.Errorf("ensuring EventBridge rule: %w", err)
	}
	logger.Info("EventBridge rule ready")

	return nil
}

// SyncMapping updates the repo_mapping secret and EventBridge rule filter.
func (p *Provider) SyncMapping(ctx context.Context, repoMapping mapping.RepoMapping) error {
	logger := log.FromContext(ctx).WithName("aws")

	// Persist mapping to SecretsManager.
	data, err := json.Marshal(repoMapping)
	if err != nil {
		return fmt.Errorf("marshalling repo mapping: %w", err)
	}
	if err := p.updateSecret(ctx, p.repoMappingSecretName(), string(data)); err != nil {
		return fmt.Errorf("updating repo mapping secret: %w", err)
	}
	logger.Info("repo mapping persisted", "ecrRepos", len(repoMapping))

	// Update EventBridge filter with discovered ECR repo names.
	repoNames := make([]string, 0, len(repoMapping))
	for name := range repoMapping {
		repoNames = append(repoNames, name)
	}

	// We need the queue ARN to set the target.
	_, queueArn, err := p.getQueueInfo(ctx)
	if err != nil {
		return fmt.Errorf("getting queue ARN for EventBridge: %w", err)
	}

	if err := p.ensureEventRule(ctx, queueArn, repoNames); err != nil {
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

	// SQS queue.
	if err := p.deleteQueue(ctx); err != nil {
		logger.Error(err, "failed to delete SQS queue (continuing)")
	}

	// IAM: detach inline policies, then role.
	if err := p.deleteRole(ctx); err != nil {
		logger.Error(err, "failed to delete IAM role (continuing)")
	}

	// SecretsManager: repo-mapping only (not global token).
	if err := p.deleteSecret(ctx, p.repoMappingSecretName()); err != nil {
		logger.Error(err, "failed to delete repo-mapping secret (continuing)")
	}

	logger.Info("cleanup complete")
	return nil
}

// -- naming helpers --

func (p *Provider) roleName() string      { return p.cfg.AppName + "-lambda-role" }
func (p *Provider) eventRuleName() string  { return p.cfg.AppName + "-ecr-push" }
func (p *Provider) eventTargetID() string  { return p.cfg.AppName + "-sqs-target" }
func (p *Provider) repoMappingSecretName() string { return p.cfg.AppName + "-repo-mapping" }
func (p *Provider) tokenSecretName() string       { return p.cfg.AppName + "-token" }

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
