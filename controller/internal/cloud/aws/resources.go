package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// ── IAM ─────────────────────────────────────────────────────────────────

func (p *Provider) ensureRole(ctx context.Context) (string, error) {
	out, err := p.im.GetRole(ctx, &iam.GetRoleInput{RoleName: ptr(p.roleName())})
	if err == nil {
		return *out.Role.Arn, nil
	}
	if !isNotFound(err) {
		return "", err
	}

	createOut, err := p.im.CreateRole(ctx, &iam.CreateRoleInput{
		RoleName:                 ptr(p.roleName()),
		AssumeRolePolicyDocument: ptr(lambdaTrustPolicy),
		Tags: []iamtypes.Tag{
			{Key: ptr("managed-by"), Value: ptr("flux2-ecr-webhook")},
		},
	})
	if err != nil {
		return "", err
	}

	// Wait briefly for IAM propagation before attaching policies.
	time.Sleep(5 * time.Second)

	return *createOut.Role.Arn, nil
}

func (p *Provider) attachRolePolicies(ctx context.Context, queueArn string, secretArns []string) error {
	policies := map[string]string{
		"sqs-access":     lambdaSQSPolicy(queueArn),
		"secrets-access": lambdaSecretsPolicy(secretArns),
		"logs-access":    lambdaLogsPolicy(),
	}
	for name, doc := range policies {
		_, err := p.im.PutRolePolicy(ctx, &iam.PutRolePolicyInput{
			RoleName:       ptr(p.roleName()),
			PolicyName:     ptr(p.cfg.AppName + "-" + name),
			PolicyDocument: ptr(doc),
		})
		if err != nil {
			return fmt.Errorf("attaching %s policy: %w", name, err)
		}
	}
	return nil
}

func (p *Provider) deleteRole(ctx context.Context) error {
	// List and delete all inline policies first.
	listOut, err := p.im.ListRolePolicies(ctx, &iam.ListRolePoliciesInput{
		RoleName: ptr(p.roleName()),
	})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, name := range listOut.PolicyNames {
		_, _ = p.im.DeleteRolePolicy(ctx, &iam.DeleteRolePolicyInput{
			RoleName:   ptr(p.roleName()),
			PolicyName: ptr(name),
		})
	}
	_, err = p.im.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: ptr(p.roleName())})
	if isNotFound(err) {
		return nil
	}
	return err
}

// ── SQS ─────────────────────────────────────────────────────────────────

func (p *Provider) ensureQueue(ctx context.Context) (string, string, error) {
	// Try to get existing queue first.
	url, arn, err := p.getQueueInfo(ctx)
	if err == nil {
		return url, arn, nil
	}

	// SQS requires a 60-second wait after deleting a queue before recreating
	// it with the same name. Retry with backoff if we hit this window.
	for attempt := 0; attempt < 4; attempt++ {
		_, err = p.sq.CreateQueue(ctx, &sqs.CreateQueueInput{
			QueueName: ptr(p.cfg.SQSName),
			Tags:      map[string]string{"managed-by": "flux2-ecr-webhook"},
		})
		if err == nil || alreadyExists(err) {
			break
		}
		if isQueueDeletedRecently(err) && attempt < 3 {
			time.Sleep(20 * time.Second)
			continue
		}
		return "", "", err
	}

	// Re-fetch to get both URL and ARN.
	return p.getQueueInfo(ctx)
}

func (p *Provider) getQueueInfo(ctx context.Context) (string, string, error) {
	urlOut, err := p.sq.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: ptr(p.cfg.SQSName)})
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

func (p *Provider) setQueuePolicy(ctx context.Context, queueURL, queueArn, ruleArn string) error {
	_, err := p.sq.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: ptr(queueURL),
		Attributes: map[string]string{
			"Policy": sqsPolicyForEventBridge(queueArn, ruleArn),
		},
	})
	return err
}

func (p *Provider) deleteQueue(ctx context.Context) error {
	urlOut, err := p.sq.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: ptr(p.cfg.SQSName)})
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = p.sq.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: urlOut.QueueUrl})
	if isNotFound(err) {
		return nil
	}
	return err
}

// ── SecretsManager ──────────────────────────────────────────────────────

func (p *Provider) ensureSecrets(ctx context.Context) error {
	for _, name := range []string{p.repoMappingSecretName(), p.tokenSecretName()} {
		_, err := p.sm.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
			Name:         ptr(name),
			SecretString: ptr("{}"),
			Tags: []smtypes.Tag{
				{Key: ptr("managed-by"), Value: ptr("flux2-ecr-webhook")},
			},
		})
		if err == nil || alreadyExists(err) {
			continue
		}
		// Secret may still be scheduled for deletion after a recent cleanup.
		// Restore it instead of failing.
		if isScheduledForDeletion(err) {
			if _, restoreErr := p.sm.RestoreSecret(ctx, &secretsmanager.RestoreSecretInput{
				SecretId: ptr(name),
			}); restoreErr != nil {
				return fmt.Errorf("restoring secret %s: %w", name, restoreErr)
			}
			continue
		}
		return fmt.Errorf("creating secret %s: %w", name, err)
	}
	return nil
}

func (p *Provider) updateSecret(ctx context.Context, name, value string) error {
	_, err := p.sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     ptr(name),
		SecretString: ptr(value),
	})
	return err
}

func (p *Provider) getSecretArn(ctx context.Context, name string) (string, error) {
	out, err := p.sm.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
		SecretId: ptr(name),
	})
	if err != nil {
		return "", err
	}
	return *out.ARN, nil
}

func (p *Provider) deleteSecret(ctx context.Context, name string) error {
	_, err := p.sm.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   ptr(name),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if isNotFound(err) {
		return nil
	}
	return err
}

// ── Lambda ──────────────────────────────────────────────────────────────

func (p *Provider) ensureLambda(ctx context.Context, roleArn, queueArn string) error {
	// Attach IAM policies before creating/updating the function.
	repoArn, _ := p.getSecretArn(ctx, p.repoMappingSecretName())
	tokenArn, _ := p.getSecretArn(ctx, p.tokenSecretName())
	secretArns := []string{}
	if repoArn != "" {
		secretArns = append(secretArns, repoArn)
	}
	if tokenArn != "" {
		secretArns = append(secretArns, tokenArn)
	}
	if err := p.attachRolePolicies(ctx, queueArn, secretArns); err != nil {
		return err
	}

	zipData, err := buildLambdaZip()
	if err != nil {
		return fmt.Errorf("building lambda zip: %w", err)
	}

	// Check if function exists.
	_, err = p.lm.GetFunction(ctx, &lambda.GetFunctionInput{
		FunctionName: ptr(p.cfg.LambdaName),
	})
	if err == nil {
		// Update existing function code.
		_, err = p.lm.UpdateFunctionCode(ctx, &lambda.UpdateFunctionCodeInput{
			FunctionName: ptr(p.cfg.LambdaName),
			ZipFile:      zipData,
		})
		return err
	}
	if !isNotFound(err) {
		return err
	}

	// Create new function. Retry because IAM role propagation is eventually
	// consistent and Lambda may not be able to assume it immediately.
	createInput := &lambda.CreateFunctionInput{
		FunctionName: ptr(p.cfg.LambdaName),
		Runtime:      lambdatypes.Runtime(p.cfg.LambdaRuntime),
		Handler:      ptr("app.lambda_handler"),
		Role:         ptr(roleArn),
		Code: &lambdatypes.FunctionCode{
			ZipFile: zipData,
		},
		Timeout:    aws.Int32(p.cfg.LambdaTimeout),
		MemorySize: aws.Int32(128),
		Environment: &lambdatypes.Environment{
			Variables: map[string]string{
				"REPOS_MAPPING":                   p.repoMappingSecretName(),
				"FLUX2_WEBHOOK_TOKEN_SECRET_NAME": p.tokenSecretName(),
			},
		},
		Tags: map[string]string{"managed-by": "flux2-ecr-webhook"},
	}
	for attempt := 0; attempt < 5; attempt++ {
		_, err = p.lm.CreateFunction(ctx, createInput)
		if err == nil {
			break
		}
		if isInvalidParameterValue(err) && attempt < 4 {
			time.Sleep(time.Duration(attempt+1) * 5 * time.Second)
			continue
		}
		return fmt.Errorf("creating lambda: %w", err)
	}

	// Create event source mapping (SQS → Lambda).
	if queueArn != "" {
		return p.ensureEventSourceMapping(ctx, queueArn)
	}
	return nil
}

func (p *Provider) ensureEventSourceMapping(ctx context.Context, queueArn string) error {
	// Check if mapping already exists.
	listOut, err := p.lm.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
		FunctionName:   ptr(p.cfg.LambdaName),
		EventSourceArn: ptr(queueArn),
	})
	if err == nil && len(listOut.EventSourceMappings) > 0 {
		return nil
	}

	_, err = p.lm.CreateEventSourceMapping(ctx, &lambda.CreateEventSourceMappingInput{
		FunctionName:   ptr(p.cfg.LambdaName),
		EventSourceArn: ptr(queueArn),
		BatchSize:      aws.Int32(1),
		Enabled:        aws.Bool(true),
	})
	if alreadyExists(err) {
		return nil
	}
	return err
}

func (p *Provider) deleteLambda(ctx context.Context) error {
	// Delete event source mappings first.
	listOut, err := p.lm.ListEventSourceMappings(ctx, &lambda.ListEventSourceMappingsInput{
		FunctionName: ptr(p.cfg.LambdaName),
	})
	if err == nil {
		for _, m := range listOut.EventSourceMappings {
			_, _ = p.lm.DeleteEventSourceMapping(ctx, &lambda.DeleteEventSourceMappingInput{
				UUID: m.UUID,
			})
		}
	}

	_, err = p.lm.DeleteFunction(ctx, &lambda.DeleteFunctionInput{
		FunctionName: ptr(p.cfg.LambdaName),
	})
	if isNotFound(err) {
		return nil
	}
	return err
}

// ── EventBridge ─────────────────────────────────────────────────────────

func (p *Provider) ensureEventRule(ctx context.Context, queueArn string, repoNames []string) error {
	pattern := buildEventPattern(repoNames)

	putOut, err := p.eb.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:         ptr(p.eventRuleName()),
		EventPattern: ptr(pattern),
		State:        ebtypes.RuleStateEnabled,
		Tags: []ebtypes.Tag{
			{Key: ptr("managed-by"), Value: ptr("flux2-ecr-webhook")},
		},
	})
	if err != nil {
		return fmt.Errorf("putting EventBridge rule: %w", err)
	}
	ruleArn := *putOut.RuleArn

	// Set target: SQS queue.
	if queueArn != "" {
		_, err = p.eb.PutTargets(ctx, &eventbridge.PutTargetsInput{
			Rule: ptr(p.eventRuleName()),
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

		// Allow EventBridge to write to SQS.
		queueURL, _, _ := p.getQueueInfo(ctx)
		if queueURL != "" {
			_ = p.setQueuePolicy(ctx, queueURL, queueArn, ruleArn)
		}
	}

	return nil
}

func (p *Provider) deleteEventRule(ctx context.Context) error {
	// Remove targets first.
	_, err := p.eb.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{
		Rule: ptr(p.eventRuleName()),
		Ids:  []string{p.eventTargetID()},
	})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("removing EventBridge targets: %w", err)
	}

	_, err = p.eb.DeleteRule(ctx, &eventbridge.DeleteRuleInput{
		Name: ptr(p.eventRuleName()),
	})
	if isNotFound(err) {
		return nil
	}
	return err
}

// buildEventPattern creates the EventBridge event pattern for ECR push events.
func buildEventPattern(repoNames []string) string {
	pattern := map[string]interface{}{
		"source":      []string{"aws.ecr"},
		"detail-type": []string{"ECR Image Action"},
		"detail": map[string]interface{}{
			"action-type": []string{"PUSH"},
			"result":      []string{"SUCCESS"},
		},
	}

	if len(repoNames) > 0 {
		detail := pattern["detail"].(map[string]interface{})
		detail["repository-name"] = repoNames
	}

	data, _ := json.Marshal(pattern)
	return string(data)
}
