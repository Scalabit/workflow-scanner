terraform {
  required_version = ">= 1.0"
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
    "iam.googleapis.com"
  ])
  
  project = var.project_id
  service = each.value
  
  disable_on_destroy = false
}

# Reference existing Workload Identity Pool
data "google_iam_workload_identity_pool" "github_pool" {
  workload_identity_pool_id = "github-actions-pool"
  location                  = "global"
}

# Reference existing Workload Identity Provider
data "google_iam_workload_identity_pool_provider" "github_provider" {
  workload_identity_pool_id          = data.google_iam_workload_identity_pool.github_pool.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-provider"
  location                           = "global"
}

# Allow GitHub Actions to impersonate the service account
resource "google_service_account_iam_member" "workload_identity_user" {
  service_account_id = "projects/${var.project_id}/serviceAccounts/${data.google_service_account.github_actions_sa.email}"
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/projects/${data.google_project.project.number}/locations/global/workloadIdentityPools/${data.google_iam_workload_identity_pool.github_pool.workload_identity_pool_id}/attribute.repository/Scalabit/workflow-scanner"
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

# IAM bindings for the service account
resource "google_project_iam_member" "cloud_run_sa_bindings" {
  for_each = toset([
    "roles/secretmanager.secretAccessor",
    "roles/artifactregistry.reader"
  ])
  
  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.cloud_run_sa.email}"
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
  for_each = google_secret_manager_secret.secrets
  
  secret_id = each.value.secret_id
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
      image = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.workflow_scanner.repository_id}/workflow-scanner:latest"
      
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
        name  = "PORT"
        value = "8080"
      }
      
      env {
        name  = "TOKEN_VALIDATION_URL"
        value = "https://${var.service_name}-${substr(sha256(var.project_id), 0, 8)}-${substr(var.region, 0, 2)}.a.run.app"
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

# Cloud Storage bucket for Terraform state (must be created manually first)
# This is commented out because it should be created before running terraform
# resource "google_storage_bucket" "terraform_state" {
#   name     = "workflow-scanner-terraform-state"
#   location = var.region
# }