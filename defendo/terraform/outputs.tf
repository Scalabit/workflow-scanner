output "topic_name" {
  description = "Pub/Sub topic name — use as the --pubsub-topic flag value."
  value       = google_pubsub_topic.security_reports.name
}

output "topic_id" {
  description = "Full Pub/Sub topic resource ID."
  value       = google_pubsub_topic.security_reports.id
}

output "subscription_id" {
  description = "Full Pub/Sub subscription resource ID."
  value       = google_pubsub_subscription.security_reports.id
}

output "agent_service_account_email" {
  description = "Agent SA email — visible in Pub/Sub message attributes."
  value       = google_service_account.agent.email
}

output "agent_service_account_key_base64" {
  description = <<-EOT
    Base64-encoded JSON key for the agent SA.
    Decode and write to a file on each managed device, then set:
      GOOGLE_APPLICATION_CREDENTIALS=C:\path\to\key.json
    Or pass inline:
      [System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String("<value>")) | Set-Content key.json
  EOT
  value     = google_service_account_key.agent.private_key
  sensitive = true
}

output "wif_provider" {
  description = "Workload Identity Provider resource name — store as GitHub secret WIF_PROVIDER."
  value       = google_iam_workload_identity_pool_provider.github_actions.name
}

output "terraform_ci_service_account" {
  description = "Terraform CI SA email — store as GitHub secret WIF_SERVICE_ACCOUNT."
  value       = google_service_account.terraform_ci.email
}
