terraform {
  required_version = ">= 1.0"
  
  backend "gcs" {
    bucket = "workflow-scanner-terraform-state-34659588692"
    prefix = "terraform/state"
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
}

data "google_project" "project" {
  project_id = var.project_id
}

# Enable required APIs
resource "google_project_service" "apis" {
  for_each = toset([
    "run.googleapis.com",
    "artifactregistry.googleapis.com",
    "secretmanager.googleapis.com",
    "cloudbuild.googleapis.com",
    "iam.googleapis.com",
    "batch.googleapis.com",
    "compute.googleapis.com",
    "storage.googleapis.com"
  ])
  
  project = var.project_id
  service = each.value
  
  disable_on_destroy = false
}


# Reference existing GitHub Actions service account
data "google_service_account" "github_actions_sa" {
  account_id = "github-actions-sa"
}

# Grant permissions to GitHub Actions service account
resource "google_project_iam_member" "github_actions_permissions" {
  for_each = toset([
    "roles/run.admin",
    "roles/artifactregistry.admin", 
    "roles/secretmanager.admin",
    "roles/serviceusage.serviceUsageAdmin",
    "roles/iam.serviceAccountAdmin",
    "roles/resourcemanager.projectIamAdmin"
  ])
  
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${data.google_service_account.github_actions_sa.email}"
}

# Create Artifact Registry repository for container images
resource "google_artifact_registry_repository" "workflow_scanner" {
  location      = var.region
  repository_id = "workflow-scanner"
  description   = "Container images for workflow scanner"
  format        = "DOCKER"
  
  depends_on = [google_project_service.apis]
}

# Service account for Cloud Run
resource "google_service_account" "cloud_run_sa" {
  account_id   = "workflow-scanner-sa"
  display_name = "Workflow Scanner Service Account"
  description  = "Service account for workflow scanner Cloud Run service"
}

# IAM bindings for Cloud Run service account (following GCP Batch requirements)
resource "google_project_iam_member" "cloud_run_sa_bindings" {
  for_each = toset([
    "roles/secretmanager.secretAccessor",
    "roles/artifactregistry.reader",
    "roles/compute.instanceAdmin.v1",   # Required to create Compute instances
    "roles/storage.objectAdmin",
    "roles/iam.serviceAccountUser"      # Required to use scanner service account
  ])
  
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}

# Grant Cloud Run service account permission to use scanner service account
resource "google_service_account_iam_member" "cloud_run_use_scanner_sa" {
  service_account_id = google_service_account.scanner_sa.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}

# Secrets in Secret Manager
locals {
  secrets = {
    "gh-app-id"              = var.gh_app_id
    "gh-app-secret"          = var.gh_app_secret
    "stripe-key"             = var.stripe_key
    "stripe-publishable-key" = var.stripe_publishable_key
    "stripe-webhook-secret"  = var.stripe_webhook_secret
    "openai-api-key"         = var.openai_api_key
  }
}

resource "google_secret_manager_secret" "secrets" {
  for_each = local.secrets
  
  secret_id = each.key
  
  replication {
    auto {}
  }
  
  depends_on = [google_project_service.apis]
}

# Create secret versions with actual values
resource "google_secret_manager_secret_version" "secret_versions" {
  for_each = local.secrets
  
  secret      = google_secret_manager_secret.secrets[each.key].id
  secret_data = each.value
}

# Grant Cloud Run service account access to secrets
resource "google_secret_manager_secret_iam_member" "secret_access" {
  for_each = local.secrets
  
  secret_id = each.key
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}

# Cloud Run service
resource "google_cloud_run_v2_service" "workflow_scanner" {
  name     = "workflow-scanner"
  location = var.region
  
  template {
    service_account = google_service_account.cloud_run_sa.email
    
    scaling {
      min_instance_count = 0
      max_instance_count = 10
    }
    
    containers {
      image = var.container_image != "" ? var.container_image : "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.workflow_scanner.repository_id}/workflow-scanner:latest"
      
      ports {
        container_port = 8080
      }
      
      resources {
        limits = {
          cpu    = "2000m"
          memory = "2Gi"
        }
      }
      
      env {
        name  = "BASE_URL"
        value = "https://workflow-scanner-36bg3tpnra-lz.a.run.app"
      }
      
      # Compute Engine configuration
      env {
        name  = "COMPUTE_PROJECT_ID"
        value = var.project_id
      }
      
      env {
        name  = "COMPUTE_REGION"
        value = var.region
      }
      
      env {
        name  = "COMPUTE_BUCKET"
        value = google_storage_bucket.scanner_jobs.name
      }
      
      env {
        name  = "COMPUTE_SERVICE_ACCOUNT"
        value = google_service_account.scanner_sa.email
      }
      
      env {
        name  = "SCANNER_IMAGE"
        value = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.workflow_scanner.repository_id}/batch-scanner:latest"
      }
      
      # GitHub OAuth secrets
      env {
        name = "GH_APP_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["gh-app-id"].secret_id
            version = "latest"
          }
        }
      }
      
      env {
        name = "GH_APP_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["gh-app-secret"].secret_id
            version = "latest"
          }
        }
      }
      
      # Stripe secrets
      env {
        name = "TEST_STRIPE"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["stripe-key"].secret_id
            version = "latest"
          }
        }
      }
      
      env {
        name = "TEST_STRIPE_PK"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["stripe-publishable-key"].secret_id
            version = "latest"
          }
        }
      }
      
      env {
        name = "TEST_STRIPE_WEBHOOK_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["stripe-webhook-secret"].secret_id
            version = "latest"
          }
        }
      }
      
      # LLM API secrets
      env {
        name = "OPENAI_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["openai-api-key"].secret_id
            version = "latest"
          }
        }
      }
    }
  }
  
  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }
  
  depends_on = [
    google_project_service.apis,
    google_artifact_registry_repository.workflow_scanner
  ]
}


# Allow unauthenticated access to Cloud Run (needed for GitHub Actions and webhooks)
resource "google_cloud_run_service_iam_member" "public_access" {
  service  = google_cloud_run_v2_service.workflow_scanner.name
  location = google_cloud_run_v2_service.workflow_scanner.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Cloud Storage bucket for workflow data
resource "google_storage_bucket" "workflow_data" {
  name     = "workflow-scanner-data-${random_id.bucket_suffix.hex}"
  location = var.region
  
  uniform_bucket_level_access = true
  
  lifecycle_rule {
    condition {
      age = 30
    }
    action {
      type = "Delete"
    }
  }
  
  depends_on = [google_project_service.apis]
}

# Random suffix for bucket name uniqueness
resource "random_id" "bucket_suffix" {
  byte_length = 8
}

# Service account for Compute Engine scanner instances
resource "google_service_account" "scanner_sa" {
  account_id   = "workflow-scanner-compute"
  display_name = "Workflow Scanner Compute Service Account"
  description  = "Service account for Compute Engine workflow scanning instances"
}

# IAM bindings for scanner service account (following GCP Compute requirements)
resource "google_project_iam_member" "scanner_sa_bindings" {
  for_each = toset([
    "roles/secretmanager.secretAccessor",
    "roles/artifactregistry.reader", 
    "roles/storage.objectAdmin",
    "roles/compute.serviceAgent",     # Required for Compute instances
    "roles/logging.logWriter"        # Required for job logs
  ])
  
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.scanner_sa.email}"
}

# Grant scanner service account access to storage bucket
resource "google_storage_bucket_iam_member" "scanner_bucket_access" {
  bucket = google_storage_bucket.workflow_data.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.scanner_sa.email}"
}

# Cloud Storage bucket for Terraform state (must be created manually first)
# This is commented out because it should be created before running terraform
# resource "google_storage_bucket" "terraform_state" {
#   name     = "workflow-scanner-terraform-state"
#   location = var.region
# }

# Cloud Storage bucket for scanner job data exchange
resource "google_storage_bucket" "scanner_jobs" {
  name     = "workflow-scanner-compute-${random_id.bucket_suffix.hex}"
  location = var.region
  
  uniform_bucket_level_access = true
  
  # Auto-delete job data after 7 days
  lifecycle_rule {
    condition {
      age = 7
    }
    action {
      type = "Delete"
    }
  }
  
  depends_on = [google_project_service.apis]
}

# Grant Cloud Run service account access to scanner bucket
resource "google_storage_bucket_iam_member" "cloud_run_scanner_access" {
  bucket = google_storage_bucket.scanner_jobs.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}

# Grant scanner service account access to scanner bucket
resource "google_storage_bucket_iam_member" "scanner_scanner_access" {
  bucket = google_storage_bucket.scanner_jobs.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.scanner_sa.email}"
}