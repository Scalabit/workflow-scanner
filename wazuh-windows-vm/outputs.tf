output "vm_external_ip" {
  description = "External IP address of the Windows VM"
  value       = google_compute_instance.wazuh_windows_vm.network_interface[0].access_config[0].nat_ip
}

output "vm_internal_ip" {
  description = "Internal IP address of the Windows VM"
  value       = google_compute_instance.wazuh_windows_vm.network_interface[0].network_ip
}

output "vm_name" {
  description = "Name of the Windows VM"
  value       = google_compute_instance.wazuh_windows_vm.name
}

output "rdp_connection_command" {
  description = "Command to connect via RDP"
  value       = "Use RDP client to connect to ${google_compute_instance.wazuh_windows_vm.network_interface[0].access_config[0].nat_ip}:3389 with username Administrator"
}

output "admin_password_secret_name" {
  description = "Name of the secret containing the Administrator password"
  value       = google_secret_manager_secret.windows_admin_password.secret_id
}

output "machine_type" {
  description = "Machine type used for the VM"
  value       = google_compute_instance.wazuh_windows_vm.machine_type
}

output "zone" {
  description = "Zone where the VM is deployed"
  value       = google_compute_instance.wazuh_windows_vm.zone
}