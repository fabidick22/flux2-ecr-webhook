variable "app_name" {
  description = "Name used for resources to create."
  type        = string
  default     = "flux2-ecr-webhook"
}

variable "repo_mapping_file" {
  description = "YAML file path with repository mapping."
  type        = string
  default     = ""
}

variable "repo_mapping" {
  type        = any
  default     = null
  sensitive   = true
  description = <<EOT
Object with repository mapping, if this variable is set `repo_mapping_file` will be ignored.

**Available Attributes:**
- `<ECR>`: ECR resource name.
- `<ECR>.<ID>`: Unique name for webhooks.
- `<ECR>.<ID>.webhook`: Webhook list.
- `<ECR>.<ID>.token` (Optional): Token used for webhooks, if set, then "webhook_token" will be ignored.
- `<ECR>.<ID>.regex` (Optional): Regular expression that is applied to the image tag

EOT
}

variable "webhook_token" {
  description = "Webhook default token used to call the Flux receiver. If it doesn't find a `token` attribute in the repository mapping use this token for the webhooks"
  type        = string
  sensitive   = true
  default     = null
}

variable "cw_logs_retention" {
  description = "Specifies the number of days you want to retain log events in the specified log group."
  type        = number
  default     = 14
}
