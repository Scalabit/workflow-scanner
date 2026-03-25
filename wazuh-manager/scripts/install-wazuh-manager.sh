#!/bin/bash

# Wazuh Manager Installation Script for Ubuntu 22.04
# This script installs Wazuh manager, indexer, and dashboard using the all-in-one installer

set -e

# Logging function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a /var/log/wazuh-install.log
}

log "Starting Wazuh all-in-one installation..."

# Update system packages
log "Updating system packages..."
apt-get update -y
apt-get upgrade -y

# Install required dependencies
log "Installing required dependencies..."
apt-get install -y curl wget gnupg lsb-release

# Download and install Wazuh all-in-one
log "Downloading Wazuh installation script..."
cd /tmp
curl -sO https://packages.wazuh.com/4.14/wazuh-install.sh

# Make the script executable
chmod +x wazuh-install.sh

# Run the all-in-one installation
log "Starting Wazuh all-in-one installation (this may take several minutes)..."
./wazuh-install.sh -a 2>&1 | tee -a /var/log/wazuh-install.log

# The installation script generates random passwords, let's save them
log "Saving installation output and credentials..."

# Extract the admin credentials from the installation log
if grep -q "User: admin" /var/log/wazuh-install.log; then
    log "Installation completed successfully!"
    
    # Extract and save credentials
    grep -A 2 "User: admin" /var/log/wazuh-install.log > /tmp/wazuh-credentials.txt
    
    # Make credentials readable by root only
    chmod 600 /tmp/wazuh-credentials.txt
    
    log "Credentials saved to /tmp/wazuh-credentials.txt"
else
    log "Warning: Could not find admin credentials in installation log"
fi

# Check service status
log "Checking Wazuh services status..."

# Check if services are running
if systemctl is-active --quiet wazuh-manager; then
    log "Wazuh Manager service is running"
else
    log "Warning: Wazuh Manager service is not running"
fi

if systemctl is-active --quiet wazuh-indexer; then
    log "Wazuh Indexer service is running"
else
    log "Warning: Wazuh Indexer service is not running"
fi

if systemctl is-active --quiet wazuh-dashboard; then
    log "Wazuh Dashboard service is running"
else
    log "Warning: Wazuh Dashboard service is not running"
fi

# Configure agent enrollment settings
log "Configuring agent enrollment..."

# Enable agent registration
if [ -f /var/ossec/etc/ossec.conf ]; then
    # Backup original configuration
    cp /var/ossec/etc/ossec.conf /var/ossec/etc/ossec.conf.backup
    
    # Enable auto-enrollment
    sed -i 's/<use_password>no<\/use_password>/<use_password>yes<\/use_password>/' /var/ossec/etc/ossec.conf
    log "Agent registration enabled in ossec.conf"
else
    log "Warning: Wazuh configuration file not found"
fi

# Set up agent password
log "Setting up agent registration password..."
REG_PASSWORD=$(curl -s -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/wazuh-registration-password")
echo "$REG_PASSWORD" > /var/ossec/etc/authd.pass
chown ossec:ossec /var/ossec/etc/authd.pass
chmod 640 /var/ossec/etc/authd.pass
log "Agent registration password configured"

# Restart wazuh-manager to apply BOTH the conf and password changes
log "Restarting wazuh-manager service..."
systemctl stop wazuh-manager
sleep 5
systemctl start wazuh-manager
log "Wazuh manager restarted successfully"

# Configure Windows Event Log monitoring in shared agent configuration
log "Configuring Windows Event Log monitoring..."
if [ -f /var/ossec/etc/shared/default/agent.conf ]; then
    # Backup existing agent.conf
    cp /var/ossec/etc/shared/default/agent.conf /var/ossec/etc/shared/default/agent.conf.backup
else
    # Create directory if it doesn't exist
    mkdir -p /var/ossec/etc/shared/default
fi

# Create agent.conf with Windows Event Log monitoring
cat > /var/ossec/etc/shared/default/agent.conf << 'EOF'
<agent_config os="windows">
  <localfile>
    <location>Application</location>
    <log_format>eventchannel</log_format>
  </localfile>
  <localfile>
    <location>System</location>
    <log_format>eventchannel</log_format>
  </localfile>
  <localfile>
    <location>Security</location>
    <log_format>eventchannel</log_format>
  </localfile>
  <localfile>
    <location>Microsoft-Windows-Sysmon/Operational</location>
    <log_format>eventchannel</log_format>
  </localfile>
  
  <active-response>
    <disabled>no</disabled>
  </active-response>
</agent_config>
EOF

chown ossec:ossec /var/ossec/etc/shared/default/agent.conf
chmod 640 /var/ossec/etc/shared/default/agent.conf
log "Windows Event Log monitoring configured in shared agent.conf"

# Clean up old disconnected agents
log "Cleaning up old disconnected agents..."
/var/ossec/bin/agent_control -l | grep "Disconnected" | while read line; do
  agent_id=$(echo "$line" | awk -F',' '{print $1}' | awk '{print $2}')
  if [ ! -z "$agent_id" ] && [ "$agent_id" != "000" ]; then
    log "Removing disconnected agent ID: $agent_id"
    echo "y" | /var/ossec/bin/manage_agents -r "$agent_id" >/dev/null 2>&1 || true
  fi
done

# Display installation summary
log "=== Wazuh Installation Summary ==="
log "Wazuh Manager: Installed"
log "Wazuh Indexer: Installed" 
log "Wazuh Dashboard: Installed"
log "External IP: $(curl -s ifconfig.me 2>/dev/null || echo 'Unable to determine')"
log "Internal IP: $(hostname -I | awk '{print $1}')"
log ""
log "Dashboard URL: https://$(curl -s ifconfig.me 2>/dev/null || hostname -I | awk '{print $1}')"
log ""
log "Agent enrollment command template:"
log "For internal communication: Use IP $(hostname -I | awk '{print $1}')"
log ""
log "Installation logs: /var/log/wazuh-install.log"
log "Credentials: /tmp/wazuh-credentials.txt"
log ""
log "=== Installation Complete ==="

# Create completion marker
touch /var/log/wazuh-install-complete
echo "$(date): Wazuh installation completed successfully" > /var/log/wazuh-install-complete

log "Wazuh installation script finished successfully!"