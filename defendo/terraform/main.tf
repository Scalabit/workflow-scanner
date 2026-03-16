terraform {
  required_version = ">= 1.6"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
  }

  # The GCS bucket for state is passed at init time to avoid hardcoding.
  # Create it once manually before first run:
  #   gcloud storage buckets create gs://<bucket> --project=<project> --location=europe-west1
  # Then init with:
  #   terraform init -backend-config="bucket=<bucket>"
  # In CI this is injected via the TF_STATE_BUCKET secret (see workflow).
  backend "gcs" {
    prefix = "defendo/pubsub"
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}
