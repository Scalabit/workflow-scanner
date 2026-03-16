output "vm_name" {
  description = "Name of the created Windows VM"
  value       = google_compute_instance.defendo_vm.name
}

output "vm_external_ip" {
  description = "External IP address of the Windows VM"
  value       = google_compute_instance.defendo_vm.network_interface[0].access_config[0].nat_ip
}

output "vm_internal_ip" {
  description = "Internal IP address of the Windows VM"
  value       = google_compute_instance.defendo_vm.network_interface[0].network_ip
}

output "service_account_email" {
  description = "Email of the service account used by the VM"
  value       = google_service_account.defendo_agent.email
}

output "pubsub_topic" {
  description = "Name of the Pub/Sub topic for security alerts"
  value       = google_pubsub_topic.defendo_alerts.name
}

output "rdp_connection" {
  description = "RDP connection command"
  value       = "mstsc /v:${google_compute_instance.defendo_vm.network_interface[0].access_config[0].nat_ip}"
}