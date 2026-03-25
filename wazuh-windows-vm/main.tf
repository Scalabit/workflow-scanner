terraform {
  required_version = ">= 1.0"

  backend "gcs" {
    bucket = "workflow-scanner-terraform-state-34659588692"
    prefix = "terraform/wazuh-windows-vm/state"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.1"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
  zone    = var.zone
}

# Enable required APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "compute.googleapis.com",
    "oslogin.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "secretmanager.googleapis.com"
  ])

  project = var.project_id
  service = each.value

  disable_on_destroy = false
}

# Reference the existing Wazuh manager network
data "google_compute_network" "wazuh_manager_vpc" {
  name = "wazuh-manager-network"
}

# Reference the existing Wazuh manager subnet  
data "google_compute_subnetwork" "wazuh_manager_subnet" {
  name   = "wazuh-manager-subnet"
  region = var.region
}

# Get Wazuh manager IP from Secret Manager
data "google_secret_manager_secret_version" "wazuh_manager_ip" {
  secret = "wazuh-manager-ip"
}

# Firewall rule to allow RDP (add to existing network)
resource "google_compute_firewall" "allow_rdp" {
  name    = "allow-rdp-wazuh-windows-vm"
  network = data.google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["3389"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-windows-vm"]
}

# Random password for Windows Administrator
resource "random_password" "windows_admin_password" {
  length  = 16
  special = true
}

resource "random_id" "vm_deployment_id" {
  byte_length = 8
  
  keepers = {
    deployment_time = timestamp()
  }
}

# Cleanup old VMs with same base name before creating new one
resource "null_resource" "cleanup_old_vms" {
  triggers = {
    deployment_id = random_id.vm_deployment_id.hex
  }
  
  provisioner "local-exec" {
    command = <<-EOT
      # Find and delete old VMs with the same base name pattern
      OLD_VMS=$(gcloud compute instances list --filter="name~'^${var.vm_name}-[a-f0-9]+$' AND zone:(${var.zone})" --format="value(name)" || true)
      if [ ! -z "$OLD_VMS" ]; then
        echo "Cleaning up old VMs: $OLD_VMS"
        echo "$OLD_VMS" | xargs -r gcloud compute instances delete --zone=${var.zone} --quiet || true
      fi
    EOT
  }
}

# Store the password in Secret Manager
resource "google_secret_manager_secret" "windows_admin_password" {
  secret_id = "wazuh-windows-vm-admin-password"

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "windows_admin_password" {
  secret      = google_secret_manager_secret.windows_admin_password.id
  secret_data = random_password.windows_admin_password.result
}

# Get the latest Windows Server 2022 image
data "google_compute_image" "windows_2022" {
  family  = "windows-2022"
  project = "windows-cloud"
}

# Create Windows VM instance
resource "google_compute_instance" "wazuh_windows_vm" {
  name         = "${var.vm_name}-${random_id.vm_deployment_id.hex}"
  machine_type = var.machine_type
  zone         = var.zone

  tags = ["wazuh-windows-vm"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.windows_2022.self_link
      size  = var.disk_size_gb
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = data.google_compute_network.wazuh_manager_vpc.id
    subnetwork = data.google_compute_subnetwork.wazuh_manager_subnet.id
    
    access_config {
      # Ephemeral external IP
    }
  }

  # Enable OS Login for better security
  metadata = {
    enable-oslogin = "TRUE"
    deployment-id = random_id.vm_deployment_id.hex
    windows-startup-script-ps1 = templatefile("${path.module}/scripts/install-wazuh-agent.ps1", {
      wazuh_manager_ip = data.google_secret_manager_secret_version.wazuh_manager_ip.secret_data
      wazuh_agent_name = "${var.vm_name}-agent"
      wazuh_registration_password = var.wazuh_registration_password
    })
  }

  # Service account for monitoring and logging
  service_account {
    email = google_service_account.wazuh_vm_sa.email
    scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
      "https://www.googleapis.com/auth/logging.write",
      "https://www.googleapis.com/auth/monitoring.write"
    ]
  }

  # Set Windows password
  metadata_startup_script = "net user Administrator ${random_password.windows_admin_password.result}"

  depends_on = [
    google_project_service.apis,
    data.google_compute_network.wazuh_manager_vpc,
    data.google_compute_subnetwork.wazuh_manager_subnet,
    data.google_secret_manager_secret_version.wazuh_manager_ip,
    null_resource.cleanup_old_vms
  ]

  lifecycle {
    ignore_changes = [
      metadata_startup_script
    ]
  }
}

# Service account for the VM
resource "google_service_account" "wazuh_vm_sa" {
  account_id   = "wazuh-windows-vm-sa"
  display_name = "Wazuh Windows VM Service Account"
  description  = "Service account for Wazuh Windows monitoring VM"
}

# IAM bindings for VM service account
resource "google_project_iam_member" "wazuh_vm_sa_bindings" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter",
    "roles/secretmanager.secretAccessor"
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.wazuh_vm_sa.email}"
}