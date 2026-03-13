output "manager_external_ip" {
  description = "External IP address of the Wazuh manager"
  value       = google_compute_instance.wazuh_manager.network_interface[0].access_config[0].nat_ip
}

output "manager_internal_ip" {
  description = "Internal IP address of the Wazuh manager"
  value       = google_compute_instance.wazuh_manager.network_interface[0].network_ip
}

output "wazuh_dashboard_url" {
  description = "URL to access Wazuh dashboard"
  value       = "https://${google_compute_instance.wazuh_manager.network_interface[0].access_config[0].nat_ip}"
}

output "ssh_connection_command" {
  description = "Command to SSH to the Wazuh manager"
  value       = "gcloud compute ssh ${google_compute_instance.wazuh_manager.name} --zone=${var.zone}"
}

output "agent_enrollment_command_template" {
  description = "Template command for enrolling agents"
  value       = "Use internal IP ${google_compute_instance.wazuh_manager.network_interface[0].network_ip} for agent enrollment"
}

output "manager_ip_secret_name" {
  description = "Secret Manager secret containing the manager IP"
  value       = google_secret_manager_secret.wazuh_manager_ip.secret_id
}