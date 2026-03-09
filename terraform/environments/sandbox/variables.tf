variable "project_id" {
  description = "The GCP project ID"
  type        = string
  default     = "workflow-scanner"
}

variable "region" {
  description = "The GCP region for resources"
  type        = string
  default     = "europe-north1"
}

# Secrets as input variables
variable "base_url" {
  description = "Webpage URL"
  type        = string
  sensitive   = true
}

variable "gh_client_id" {
  description = "GitHub App Client ID"
  type        = string
  sensitive   = true
}

variable "gh_client_secret" {
  description = "GitHub App Client Secret"
  type        = string
  sensitive   = true
}


variable "stripe_key" {
  description = "Stripe Secret Key"
  type        = string
  sensitive   = true
}

variable "stripe_publishable_key" {
  description = "Stripe Publishable Key"
  type        = string
  sensitive   = true
}

variable "stripe_webhook_secret" {
  description = "Stripe Webhook Secret"
  type        = string
  sensitive   = true
}

variable "openai_api_key" {
  description = "OpenAI API Key"
  type        = string
  sensitive   = true
}

variable "resend_api_key" {
  description = "Resend API Key for sending emails"
  type        = string
  sensitive   = true
}

variable "feedback_to_email" {
  description = "Email address to receive feedback"
  type        = string
}


variable "sandbox_allowed_users" {
  description = "Comma-separated list of email addresses allowed to access sandbox environment"
  type        = string
  sensitive   = true
  default     = ""
}


variable "service_name" {
  description = "Name of the Cloud Run service"
  type        = string
  default     = "workflow-scanner"
}

variable "container_image" {
  description = "Container image for Cloud Run service"
  type        = string
  default     = ""
}

variable "environment" {
  description = "Environment (dev, staging, prod)"
  type        = string
  default     = "dev"
}

variable "max_instances" {
  description = "Maximum number of Cloud Run instances"
  type        = number
  default     = 10
}

variable "cpu_limit" {
  description = "CPU limit for Cloud Run service"
  type        = string
  default     = "2000m"
}

variable "memory_limit" {
  description = "Memory limit for Cloud Run service"
  type        = string
  default     = "2Gi"
}