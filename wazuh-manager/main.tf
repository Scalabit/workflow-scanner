terraform {
  required_version = ">= 1.0"

  backend "gcs" {
    bucket = "workflow-scanner-terraform-state-34659588692"
    prefix = "terraform/wazuh-manager/state"
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

# Create a VPC network for the Wazuh manager
resource "google_compute_network" "wazuh_manager_vpc" {
  name                    = "wazuh-manager-network"
  auto_create_subnetworks = false
  
  depends_on = [google_project_service.apis]
}

# Create a subnet
resource "google_compute_subnetwork" "wazuh_manager_subnet" {
  name          = "wazuh-manager-subnet"
  network       = google_compute_network.wazuh_manager_vpc.id
  ip_cidr_range = "10.1.0.0/24"
  region        = var.region
}

# Firewall rule to allow SSH
resource "google_compute_firewall" "allow_ssh" {
  name    = "allow-ssh-wazuh-manager"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Firewall rule for Wazuh Dashboard (HTTPS)
resource "google_compute_firewall" "allow_wazuh_dashboard" {
  name    = "allow-wazuh-dashboard"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Firewall rule for Wazuh agent communication
resource "google_compute_firewall" "allow_wazuh_agents" {
  name    = "allow-wazuh-agents"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["1514", "1515", "55000"]
  }

  allow {
    protocol = "udp"
    ports    = ["1514"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Firewall rule for OpenClaw Autopilot dashboard
resource "google_compute_firewall" "allow_openclaw_dashboard" {
  name    = "allow-openclaw-dashboard"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["18789", "9090"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Firewall rule for MCP server API
resource "google_compute_firewall" "allow_mcp_server" {
  name    = "allow-mcp-server"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["3000"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Firewall rule for Ollama API
resource "google_compute_firewall" "allow_ollama_api" {
  name    = "allow-ollama-api"
  network = google_compute_network.wazuh_manager_vpc.name

  allow {
    protocol = "tcp"
    ports    = ["11434"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["wazuh-manager"]
}

# Get the latest Ubuntu 22.04 LTS image
data "google_compute_image" "ubuntu" {
  family  = "ubuntu-2204-lts"
  project = "ubuntu-os-cloud"
}

# Create Wazuh manager VM instance
resource "google_compute_instance" "wazuh_manager" {
  name         = var.vm_name
  machine_type = var.machine_type
  zone         = var.zone
  allow_stopping_for_update = true

  tags = ["wazuh-manager"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = var.disk_size_gb
      type  = "pd-standard"
    }
  }

  network_interface {
    network    = google_compute_network.wazuh_manager_vpc.id
    subnetwork = google_compute_subnetwork.wazuh_manager_subnet.id
    
    access_config {
      # Ephemeral external IP
    }
  }

  # Metadata for init
  metadata = {
    enable-oslogin = "TRUE"
    wazuh-registration-password = var.wazuh_registration_password
    startup-script = file("${path.module}/scripts/install-wazuh-manager.sh")
  }

  # Service account for monitoring and logging
  service_account {
    email = google_service_account.wazuh_manager_sa.email
    scopes = [
      "https://www.googleapis.com/auth/cloud-platform",
      "https://www.googleapis.com/auth/logging.write",
      "https://www.googleapis.com/auth/monitoring.write"
    ]
  }

  depends_on = [
    google_project_service.apis,
    google_compute_network.wazuh_manager_vpc,
    google_compute_subnetwork.wazuh_manager_subnet
  ]
}

# Service account for the VM
resource "google_service_account" "wazuh_manager_sa" {
  account_id   = "wazuh-manager-sa"
  display_name = "Wazuh Manager Service Account"
  description  = "Service account for Wazuh manager VM"
}

# IAM bindings for VM service account
resource "google_project_iam_member" "wazuh_manager_sa_bindings" {
  for_each = toset([
    "roles/logging.logWriter",
    "roles/monitoring.metricWriter"
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.wazuh_manager_sa.email}"
}

# Store the manager IP in Secret Manager for the Windows VM to use
resource "google_secret_manager_secret" "wazuh_manager_ip" {
  secret_id = "wazuh-manager-ip"

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis]
}

resource "google_secret_manager_secret_version" "wazuh_manager_ip" {
  secret      = google_secret_manager_secret.wazuh_manager_ip.id
  secret_data = google_compute_instance.wazuh_manager.network_interface[0].network_ip
}