terraform {
  required_version = ">= 1.5"
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
}

resource "google_compute_disk" "defendo_boot_disk" {
  name = "${var.vm_name}-boot-disk"
  type = "pd-ssd"
  zone = var.zone
  size = var.disk_size_gb
  
  image = "windows-server-2022-dc-v20241210"

  labels = {
    environment = "security"
    purpose     = "defendo-agent"
  }
}

resource "google_compute_instance" "defendo_vm" {
  name         = var.vm_name
  machine_type = var.machine_type
  zone         = var.zone
  
  tags = ["defendo-agent", "windows-vm", "security"]

  boot_disk {
    source      = google_compute_disk.defendo_boot_disk.id
    auto_delete = false
  }

  network_interface {
    network = "default"
    
    access_config {
      // External IP
    }
  }

  metadata = {
    windows-startup-script-ps1 = file("${path.module}/startup-script.ps1")
  }

  service_account {
    email = google_service_account.defendo_agent.email
    scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
      "https://www.googleapis.com/auth/pubsub"
    ]
  }

  labels = {
    environment = "security"
    purpose     = "defendo-agent"
    os          = "windows"
  }
}

resource "google_service_account" "defendo_agent" {
  account_id   = "defendo-agent-sa"
  display_name = "Defendo Agent Service Account"
  description  = "Service account for Defendo security agent"
}

resource "google_project_iam_member" "defendo_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.defendo_agent.email}"
}

resource "google_pubsub_topic" "defendo_alerts" {
  name = "defendo-security-alerts"

  labels = {
    environment = "security"
    purpose     = "defendo-agent"
  }
}

resource "google_compute_firewall" "defendo_rdp" {
  name    = "allow-defendo-rdp"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["3389"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["defendo-agent"]
}