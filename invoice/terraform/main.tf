terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "7.6.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

resource "google_compute_instance" "phi_instance" {
  name         = "phi35-invoice-processor"
  machine_type = "e2-standard-4"
  zone         = "${var.region}-a"
  
  lifecycle {
    replace_triggered_by = [
      terraform_data.force_recreate.output
    ]
  }

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2404-lts-amd64"
      size  = 50
      type  = "pd-standard"
    }
  }

  network_interface {
    network = "default"
    access_config {}
  }

  metadata = {
    startup-script = file("${path.module}/../scripts/startup-script.sh")
    email-username = var.email_username
    email-password = var.email_password
    imap-server    = var.imap_server
    smtp-server    = var.smtp_server
    # Force recreation when startup script changes
    script-hash    = filesha256("${path.module}/../scripts/startup-script.sh")
    force-recreate = timestamp()
  }

  tags = ["phi-instance"]
}

resource "google_compute_firewall" "phi_firewall" {
  name    = "allow-phi-api"
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["8000", "22"]
  }

  source_ranges = ["0.0.0.0/0"]
  target_tags   = ["phi-instance"]
}

resource "terraform_data" "force_recreate" {
  input = "${timestamp()}-${filesha256("${path.module}/../scripts/startup-script.sh")}"
}