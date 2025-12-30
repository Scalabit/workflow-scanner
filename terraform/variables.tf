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

variable "gitlab_client_id" {
  description = "GitLab App Client ID"
  type        = string
  sensitive   = true
}

variable "gitlab_client_secret" {
  description = "GitLab App Client Secret"
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