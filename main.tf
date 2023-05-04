
module "lambda_function" {
  source = "github.com/terraform-aws-modules/terraform-aws-lambda?ref=v4.16.0"

  function_name                     = var.app_name
  handler                           = "app.lambda_handler"
  runtime                           = "python3.10"
  source_path                       = "${path.module}/src/call_receiver"
  cloudwatch_logs_retention_in_days = var.cw_logs_retention
  environment_variables = {
    REPOS_MAPPING                   = aws_secretsmanager_secret.repo-mapping.name
    FLUX2_WEBHOOK_TOKEN_SECRET_NAME = aws_secretsmanager_secret.webhook-token.name
  }
}

module "sqs_queue" {
  source = "github.com/terraform-aws-modules/terraform-aws-sqs?ref=v4.0.1"
  name   = "${var.app_name}-push-events"
}

resource "aws_secretsmanager_secret" "repo-mapping" {
  name = "lambda/${var.app_name}/repo-mapping"
}

resource "aws_secretsmanager_secret_version" "repo-mapping" {
  secret_id     = aws_secretsmanager_secret.repo-mapping.id
  secret_string = jsonencode(local.repo_mapping)
}

resource "aws_secretsmanager_secret" "webhook-token" {
  name = "lambda/${var.app_name}/token"
}

resource "aws_secretsmanager_secret_version" "webhook-token" {
  secret_id     = aws_secretsmanager_secret.webhook-token.id
  secret_string = var.webhook_token
}

resource "aws_iam_policy" "lambda_sqs_policy" {
  name_prefix = "${var.app_name}-sqs-policy"
  description = "Policy to allow lambda to receive messages from SQS"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"]
        Resource = [module.sqs_queue.queue_arn]
      }
    ]
  })
}

resource "aws_iam_policy" "lambda_secrets_policy" {
  name        = "${var.app_name}-secrets-policy"
  description = "Policy to allow lambda to receive secrets from Secrets Manager"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["secretsmanager:GetSecretValue"]
        Resource = [aws_secretsmanager_secret.webhook-token.arn, aws_secretsmanager_secret.repo-mapping.arn]
      }
    ]
  })
}

resource "aws_sqs_queue_policy" "sqs_policy" {
  queue_url = module.sqs_queue.queue_id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = "*"
        Action    = "sqs:SendMessage"
        Resource  = module.sqs_queue.queue_arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_cloudwatch_event_rule.ecr_event.arn
          }
        }
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_sqs_attachment" {
  policy_arn = aws_iam_policy.lambda_sqs_policy.arn
  role       = module.lambda_function.lambda_role_name
}

resource "aws_iam_role_policy_attachment" "lambda_secrets_attachment" {
  policy_arn = aws_iam_policy.lambda_secrets_policy.arn
  role       = module.lambda_function.lambda_role_name
}

resource "aws_lambda_event_source_mapping" "sqs_mapping" {
  event_source_arn = module.sqs_queue.queue_arn
  function_name    = module.lambda_function.lambda_function_arn
}

resource "aws_cloudwatch_event_rule" "ecr_event" {
  name_prefix = "${var.app_name}-push-event"
  description = "ECR event rule"
  event_pattern = jsonencode({
    source      = ["aws.ecr"]
    detail-type = ["ECR Image Action"]
    detail = {
      action-type     = ["PUSH"]
      result          = ["SUCCESS"]
      repository-name = keys(local.repo_mapping)
    }
  })
}

resource "aws_cloudwatch_event_target" "sqs_target" {
  rule      = aws_cloudwatch_event_rule.ecr_event.name
  target_id = "sqs_target"
  arn       = module.sqs_queue.queue_arn
}
