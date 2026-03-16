variable "project_id" {
  description = "Google Cloud project ID."
  type        = string
}

variable "region" {
  description = "Google Cloud region for resources."
  type        = string
  default     = "europe-west1"
}

variable "environment" {
  description = "Deployment environment label (e.g. prod, staging)."
  type        = string
  default     = "prod"
}

variable "topic_name" {
  description = "Pub/Sub topic name for Defendo agent security reports."
  type        = string
  default     = "defendo-security-reports"
}

variable "subscription_name" {
  description = "Pub/Sub pull subscription name for backend consumers."
  type        = string
  default     = "defendo-security-reports-sub"
}

variable "message_retention_days" {
  description = "Days to retain undelivered messages on the topic and subscription."
  type        = number
  default     = 7
}

variable "ack_deadline_seconds" {
  description = "Seconds Pub/Sub waits for a subscriber ack before redelivering the message."
  type        = number
  default     = 60
}

variable "github_repository" {
  description = "GitHub repository in owner/repo format (e.g. myorg/AngelGuard). Scopes Workload Identity Federation access to this repository only."
  type        = string
}
