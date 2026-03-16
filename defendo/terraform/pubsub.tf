locals {
  retention_duration = "${var.message_retention_days * 86400}s"

  common_labels = {
    app         = "defendo"
    environment = var.environment
    managed-by  = "terraform"
  }
}

# ── Topic ─────────────────────────────────────────────────────────────────────

resource "google_pubsub_topic" "security_reports" {
  name    = var.topic_name
  project = var.project_id

  # Retain messages for N days even when no subscription exists yet,
  # so reports are not lost during subscriber deployments or outages.
  message_retention_duration = local.retention_duration

  labels = local.common_labels
}

# ── Subscription ──────────────────────────────────────────────────────────────
# Pull subscription consumed by the backend (Cloud Function, Cloud Run, etc.).
# Additional push subscriptions or fan-out subscribers can be added here.

resource "google_pubsub_subscription" "security_reports" {
  name    = var.subscription_name
  topic   = google_pubsub_topic.security_reports.id
  project = var.project_id

  # Match topic retention so messages are not lost before consumption.
  message_retention_duration = local.retention_duration

  # Time the subscriber has to ack a delivered message before redelivery.
  ack_deadline_seconds = var.ack_deadline_seconds

  # Do not keep a copy of messages after they have been acknowledged.
  retain_acked_messages = false

  # Subscription never expires (empty ttl = no expiration).
  expiration_policy {
    ttl = ""
  }

  labels = local.common_labels
}
