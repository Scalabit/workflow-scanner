output "cloud_run_url" {
  description = "URL of the deployed Cloud Run service"
  value       = google_cloud_run_v2_service.workflow_scanner.uri
}

output "service_account_email" {
  description = "Email of the Cloud Run service account"
  value       = google_service_account.cloud_run_sa.email
}

output "artifact_registry_repository" {
  description = "Artifact Registry repository for container images"
  value       = google_artifact_registry_repository.workflow_scanner.name
}

output "container_image_url" {
  description = "Full URL for container image in Artifact Registry"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.workflow_scanner.repository_id}/workflow-scanner"
}

output "secret_manager_secrets" {
  description = "List of Secret Manager secret names"
  value       = [for secret in google_secret_manager_secret.secrets : secret.secret_id]
}

output "project_id" {
  description = "GCP Project ID"
  value       = var.project_id
}

output "region" {
  description = "GCP Region"
  value       = var.region
}

# Workload Identity Federation outputs for GitHub secrets
output "wif_provider" {
  description = "Workload Identity Provider for GitHub Actions"
  value       = google_iam_workload_identity_pool_provider.github_provider.name
}

output "wif_service_account" {
  description = "Service account email for GitHub Actions"
  value       = google_service_account.github_actions_sa.email
}