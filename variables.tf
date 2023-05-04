variable "app_name" {
  description = "Name used for resources to create."
  type        = string
  default     = "flux2-ecr-webhook"
}

variable "repo_mapping_file" {
  description = "YAML file path with repository mapping."
  type        = string
}

variable "webhook_token" {
  description = "Webhook token used to call the Flux receiver."
  type        = string
  sensitive   = true
  default     = null
}

variable "cw_logs_retention" {
  description = "Specifies the number of days you want to retain log events in the specified log group."
  type        = number
  default     = 14
}
