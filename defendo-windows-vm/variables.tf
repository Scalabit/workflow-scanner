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
  description = "Name of the Windows VM for Defendo"
  type        = string
  default     = "defendo-windows-vm"
}

variable "machine_type" {
  description = "Machine type for the VM (16GB RAM for security agent)"
  type        = string
  default     = "e2-standard-4"
}

variable "disk_size_gb" {
  description = "Size of the boot disk in GB"
  type        = number
  default     = 100
}

