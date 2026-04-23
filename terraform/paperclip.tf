terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

variable "project_id" {
  description = "GCP Project ID"
  type        = string
  default     = "workflow-scanner"
}

variable "region" {
  description = "GCP Region"
  type        = string
  default     = "europe-north1"
}

variable "zone" {
  description = "GCP Zone"
  type        = string
  default     = "europe-north1-a"
}

resource "google_compute_firewall" "allow_ssh_paperclip" {
  name    = "allow-ssh-paperclip"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["paperclip-ai"]
  description   = "Allow SSH access to Paperclip VM"
}

resource "google_compute_firewall" "allow_paperclip_web" {
  name    = "allow-paperclip-web"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["3100"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["paperclip-ai"]
  description   = "Allow Paperclip web interface access"
}

resource "google_compute_instance" "paperclip_vm" {
  name         = "paperclip-ai-vm"
  machine_type = "e2-standard-4"
  zone         = var.zone

  tags = ["paperclip-ai"]

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 200
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"

    access_config {
      # Ephemeral public IP
    }
  }

  metadata_startup_script = file("${path.module}/../scripts/paperclip-startup.sh")

  lifecycle {
    create_before_destroy = true
  }
}

output "paperclip_vm_ip" {
  description = "External IP of the Paperclip VM"
  value       = google_compute_instance.paperclip_vm.network_interface[0].access_config[0].nat_ip
}

output "paperclip_url" {
  description = "Paperclip web interface URL"
  value       = "http://${google_compute_instance.paperclip_vm.network_interface[0].access_config[0].nat_ip}:3100"
}