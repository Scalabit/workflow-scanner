variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "email_username" {
  description = "Email username for IMAP/SMTP"
  type        = string
  sensitive   = true
}

variable "email_password" {
  description = "Email password for IMAP/SMTP"
  type        = string
  sensitive   = true
}

variable "imap_server" {
  description = "IMAP server address with port"
  type        = string
}

variable "smtp_server" {
  description = "SMTP server address with port"
  type        = string
}