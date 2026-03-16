terraform {
  required_version = ">= 1.5"
  
  backend "gcs" {
    bucket = "workflow-scanner-terraform-state-34659588692"
    prefix = "terraform/defendo-windows-vm/state"
  }
  
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

# Get the latest Windows Server 2022 image
data "google_compute_image" "windows_2022" {
  family  = "windows-2022"
  project = "windows-cloud"
}

resource "google_compute_instance" "defendo_vm" {
  name         = var.vm_name
  machine_type = var.machine_type
  zone         = var.zone
  
  tags = ["defendo-agent", "windows-vm", "security"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.windows_2022.self_link
      size  = var.disk_size_gb
      type  = "pd-ssd"
    }
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
  account_id   = "defendo-windows-agent-sa"
  display_name = "Defendo Windows Agent Service Account"
  description  = "Service account for Defendo Windows security agent"
}

resource "google_project_iam_member" "defendo_pubsub_publisher" {
  project = var.project_id
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.defendo_agent.email}"
}

resource "google_project_iam_member" "defendo_storage_viewer" {
  project = var.project_id
  role    = "roles/storage.objectViewer"
  member  = "serviceAccount:${google_service_account.defendo_agent.email}"
}

resource "google_pubsub_topic" "defendo_alerts" {
  name = "defendo-windows-alerts"

  labels = {
    environment = "security"
    purpose     = "defendo-windows-agent"
  }
}

resource "google_pubsub_topic" "defendo_formatted_reports" {
  name = "defendo-formatted-reports"

  labels = {
    environment = "security"
    purpose     = "defendo-formatted-reports"
  }
}

resource "google_compute_firewall" "defendo_rdp" {
  name    = "allow-defendo-windows-rdp"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["3389"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["defendo-agent"]
}