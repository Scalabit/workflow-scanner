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

variable "zone" {
  description = "The GCP zone for the VM"
  type        = string
  default     = "europe-north1-a"
}

variable "vm_name" {
  description = "Name of the Wazuh manager VM"
  type        = string
  default     = "wazuh-manager"
}

variable "machine_type" {
  description = "Machine type for the VM - meets minimum Wazuh requirements"
  type        = string
  default     = "e2-small"  # 2vCPU, 2GB RAM - meets minimum requirements
}

variable "disk_size_gb" {
  description = "Size of the boot disk in GB"
  type        = number
  default     = 50
}

variable "wazuh_admin_password" {
  description = "Admin password for Wazuh dashboard"
  type        = string
  sensitive   = true
}