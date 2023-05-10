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
  #description = "Object with repository mapping, if this variable is set `repo_mapping_file` will be ignored."
  description = <<EOT
Object with repository mapping, if this variable is set `repo_mapping_file` will be ignored.
**Example:**

```
{
  ecr-repo-name = {
    webhook = "https://gitops.domain.com/hook/111111 "
  }
  test/ecr-repo-name = {
    webhook = "https://gitops.domain.com/hook/111111 "
    token = "webhook-token "
  }
}
```

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
