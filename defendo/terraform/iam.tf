# ── Agent service account ─────────────────────────────────────────────────────
# One shared publisher identity for all managed Windows devices.
# Each device authenticates using the SA key exported from outputs.tf.

resource "google_service_account" "agent" {
  account_id   = "defendo-agent"
  display_name = "Defendo Agent"
  description  = "Publishes security check results to Pub/Sub from managed Windows devices."
  project      = var.project_id
}

# Grant publish-only access — the agent cannot read or manage the topic.
resource "google_pubsub_topic_iam_member" "agent_publisher" {
  project = var.project_id
  topic   = google_pubsub_topic.security_reports.name
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:${google_service_account.agent.email}"
}

# Long-lived JSON key for Windows devices that cannot use Workload Identity.
# The base64-encoded key is exposed in outputs.tf (sensitive).
# Rotate with: terraform taint google_service_account_key.agent && terraform apply
resource "google_service_account_key" "agent" {
  service_account_id = google_service_account.agent.name
}

# ── Workload Identity Federation for GitHub Actions ───────────────────────────
# Keyless authentication: GitHub Actions exchanges a short-lived OIDC token for
# GCP credentials — no service account keys stored in GitHub Secrets.
#
# Bootstrap (one-time, before CI is set up):
#   gcloud auth application-default login
#   terraform init -backend-config="bucket=<bucket>"
#   terraform apply -var="project_id=..." -var="github_repository=owner/repo"
# After apply, copy the wif_provider and terraform_ci_service_account outputs
# into GitHub → Settings → Secrets as WIF_PROVIDER and WIF_SERVICE_ACCOUNT.

resource "google_iam_workload_identity_pool" "github_actions" {
  workload_identity_pool_id = "github-actions"
  display_name              = "GitHub Actions"
  description               = "Keyless authentication pool for GitHub Actions workflows."
  project                   = var.project_id
}

resource "google_iam_workload_identity_pool_provider" "github_actions" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github_actions.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-provider"
  display_name                       = "GitHub OIDC"
  project                            = var.project_id

  # Restrict tokens to the configured repository only.
  attribute_condition = "assertion.repository == \"${var.github_repository}\""

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# ── Terraform CI service account ──────────────────────────────────────────────

resource "google_service_account" "terraform_ci" {
  account_id   = "terraform-ci"
  display_name = "Terraform CI/CD"
  description  = "Impersonated by GitHub Actions to plan and apply Terraform."
  project      = var.project_id
}

# Allow any workflow from the configured repository to impersonate this SA.
resource "google_service_account_iam_member" "terraform_ci_wif" {
  service_account_id = google_service_account.terraform_ci.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github_actions.name}/attribute.repository/${var.github_repository}"
}

# Minimum roles for Terraform to manage the resources in this module.
locals {
  terraform_ci_roles = toset([
    "roles/pubsub.admin",                  # Create / update topics and subscriptions
    "roles/iam.serviceAccountAdmin",        # Create / update service accounts
    "roles/iam.serviceAccountKeyAdmin",     # Create SA keys for the agent
    "roles/iam.workloadIdentityPoolAdmin",  # Manage WIF pool and provider
    "roles/storage.objectAdmin",            # Read / write Terraform state in GCS
  ])
}

resource "google_project_iam_member" "terraform_ci" {
  for_each = local.terraform_ci_roles
  project  = var.project_id
  role     = each.value
  member   = "serviceAccount:${google_service_account.terraform_ci.email}"
}
