terraform {
  required_version = ">= 1.0"

  backend "gcs" {
    bucket = "workflow-scanner-terraform-state-34659588692"
    prefix = "terraform/sandbox/state"
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
    time = {
      source  = "hashicorp/time"
      version = "~> 0.9"
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
    "sqladmin.googleapis.com",
    "storage.googleapis.com",
    "compute.googleapis.com"
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
    "roles/resourcemanager.projectIamAdmin",
    "roles/cloudsql.admin"
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${data.google_service_account.github_actions_sa.email}"
}

# Create Artifact Registry repository for container images
resource "google_artifact_registry_repository" "workflow_scanner" {
  location      = var.region
  repository_id = "workflow-scanner-sandbox"
  description   = "Container images for workflow scanner"
  format        = "DOCKER"

  depends_on = [google_project_service.apis]
}

# Service account for Cloud Run
resource "google_service_account" "cloud_run_sa" {
  account_id   = "workflow-scanner-sandbox-sa"
  display_name = "Workflow Scanner Sandbox Service Account"
  description  = "Service account for workflow scanner sandbox Cloud Run service"
}

# IAM bindings for Cloud Run service account
resource "google_project_iam_member" "cloud_run_sa_bindings" {
  for_each = toset([
    "roles/secretmanager.secretAccessor",
    "roles/artifactregistry.reader",
    "roles/cloudsql.client", # Required for CloudSQL access
    "roles/storage.objectAdmin"
  ])

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.cloud_run_sa.email}"
}


# Secrets in Secret Manager (sandbox-specific names)
locals {
  secrets = {
    "sandbox-base-url"               = var.base_url
    "sandbox-gh-client-id"           = var.gh_client_id
    "sandbox-gh-client-secret"       = var.gh_client_secret
    "sandbox-stripe-key"             = var.stripe_key
    "sandbox-stripe-publishable-key" = var.stripe_publishable_key
    "sandbox-stripe-webhook-secret"  = var.stripe_webhook_secret
    "sandbox-openai-api-key"         = var.openai_api_key
    "sandbox-resend-api-key"         = var.resend_api_key
    "sandbox-feedback-to-email"      = var.feedback_to_email
    "sandbox-allowed-users"          = var.sandbox_allowed_users
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
  name     = "workflow-scanner-sandbox"
  location = var.region

  template {
    service_account = google_service_account.cloud_run_sa.email

    # Add CloudSQL connection
    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.workflow_scanner_db.connection_name]
      }
    }

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

      # Mount CloudSQL volume
      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      env {
        name = "BASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-base-url"].secret_id
            version = "latest"
          }
        }
      }

      # Database configuration
      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.database_url.secret_id
            version = "latest"
          }
        }
      }

      # GitHub OAuth secrets
      env {
        name = "GH_CLIENT_ID"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-gh-client-id"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "GH_CLIENT_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-gh-client-secret"].secret_id
            version = "latest"
          }
        }
      }

      # GitLab OAuth secrets (using dummy values)
      env {
        name = "GITLAB_CLIENT_ID"
        value = "dummy"
      }

      env {
        name = "GITLAB_CLIENT_SECRET"
        value = "dummy"
      }

      # Stripe secrets
      env {
        name = "TEST_STRIPE"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-stripe-key"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "TEST_STRIPE_PK"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-stripe-publishable-key"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "TEST_STRIPE_WEBHOOK_SECRET"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-stripe-webhook-secret"].secret_id
            version = "latest"
          }
        }
      }

      # LLM API secrets
      env {
        name = "OPENAI_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-openai-api-key"].secret_id
            version = "latest"
          }
        }
      }

      # Feedback/Email configuration
      env {
        name = "RESEND_API_KEY"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-resend-api-key"].secret_id
            version = "latest"
          }
        }
      }

      env {
        name = "FEEDBACK_TO_EMAIL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-feedback-to-email"].secret_id
            version = "latest"
          }
        }
      }

      # Sandbox access control
      env {
        name  = "ENVIRONMENT"
        value = "sandbox"
      }

      env {
        name = "ALLOWED_USERS"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.secrets["sandbox-allowed-users"].secret_id
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
    google_artifact_registry_repository.workflow_scanner,
    google_sql_database_instance.workflow_scanner_db,
    google_secret_manager_secret_version.database_url,
    google_secret_manager_secret_version.secret_versions
  ]
}


# Allow unauthenticated access to Cloud Run (needed for GitHub Actions and webhooks)
resource "google_cloud_run_service_iam_member" "public_access" {
  service  = google_cloud_run_v2_service.workflow_scanner.name
  location = google_cloud_run_v2_service.workflow_scanner.location
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# Random suffix for bucket name uniqueness
resource "random_id" "bucket_suffix" {
  byte_length = 8
}