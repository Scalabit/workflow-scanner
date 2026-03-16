# Wazuh OpenClaw Autopilot - Secret Manager resources

resource "google_secret_manager_secret" "mcp_api_key" {
  secret_id = "wazuh-mcp-api-key"
  
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "openclaw_api_key" {
  secret_id = "openclaw-api-key"
  
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret" "openclaw_webhook_token" {
  secret_id = "openclaw-webhook-token"
  
  replication {
    auto {}
  }
}