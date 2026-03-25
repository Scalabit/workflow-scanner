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
  default     = "europe-north1-b"
}

variable "vm_name" {
  description = "Name of the Windows VM"
  type        = string
  default     = "wazuh-windows-vm"
}

variable "machine_type" {
  description = "Machine type for the VM (cheap option)"
  type        = string
  default     = "e2-micro"
}

variable "disk_size_gb" {
  description = "Size of the boot disk in GB"
  type        = number
  default     = 50
}


variable "wazuh_registration_password" {
  description = "Password for Wazuh agent registration"
  type        = string
  sensitive   = true
}