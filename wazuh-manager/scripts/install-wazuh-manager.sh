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

# Install Node.js 24.x (required for OpenClaw)
log "Installing Node.js 24.x for OpenClaw compatibility..."
curl -fsSL https://deb.nodesource.com/setup_24.x | bash -
DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a apt-get install -y nodejs
log "Node.js version: $(node -v)"

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

# Configure OpenClaw webhook integration
log "Configuring OpenClaw webhook integration..."

# Create the custom integration script
cat > /var/ossec/integrations/custom-openclaw << 'EOF'
#!/bin/bash
# OpenClaw Webhook Integration for Wazuh (Middleware Version)
ALERT_FILE=$1
ALERT_OUTPUT=`cat $ALERT_FILE`

# Get webhook token from environment or metadata
WEBHOOK_TOKEN=""
if [ -f /opt/openclaw-autopilot/.env ]; then
    WEBHOOK_TOKEN=$(grep "OPENCLAW_WEBHOOK_TOKEN=" /opt/openclaw-autopilot/.env | cut -d'=' -f2)
fi

if [ -z "$WEBHOOK_TOKEN" ] && command -v curl >/dev/null 2>&1; then
    WEBHOOK_TOKEN=$(curl -s -H "Metadata-Flavor: Google" "http://metadata.google.internal/computeMetadata/v1/instance/attributes/openclaw-webhook-token" 2>/dev/null || echo "")
fi

# Default fallback (will be replaced by GitHub Actions)
if [ -z "$WEBHOOK_TOKEN" ]; then
    WEBHOOK_TOKEN="__OPENCLAW_WEBHOOK_TOKEN__"
fi

# Send alert to MCP Server Middleware (port 3001) which translates it for OpenClaw Runtime API
curl -X POST \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $WEBHOOK_TOKEN" \
  --data "$ALERT_OUTPUT" \
  "http://localhost:3001/webhook/wazuh-alert" \
  --silent --show-error --max-time 10 || \
  echo "$(date) - Failed to send alert to OpenClaw Middleware: $ALERT_OUTPUT" >> /var/log/wazuh-openclaw-integration.log
EOF

# Set correct permissions for Wazuh integration script
chown root:wazuh /var/ossec/integrations/custom-openclaw
chmod 750 /var/ossec/integrations/custom-openclaw

# Add webhook configuration to ossec.conf
if [ -f /var/ossec/etc/ossec.conf ]; then
    # Backup original configuration
    cp /var/ossec/etc/ossec.conf /var/ossec/etc/ossec.conf.webhook_backup
    
    # Enable integratord if not already present
    if ! grep -q "<integratord>" /var/ossec/etc/ossec.conf; then
        sed -i '/<\/ossec_config>/i \
  <integratord> \
    <enabled>yes</enabled> \
  </integratord>' /var/ossec/etc/ossec.conf
    fi

    # Add webhook integration before closing tag
    sed -i '/<\/ossec_config>/i \
\
  <!-- OpenClaw Autonomous SOC Integration --> \
  <integration> \
    <name>custom-openclaw</name> \
    <level>12</level> <!-- Forward high severity alerts to AI agents (Changed from 10 to 12+) --> \
    <alert_format>json</alert_format> \
    <max_log>50</max_log> \
  </integration>' /var/ossec/etc/ossec.conf
    
    # Add monitoring for local logs (syslog and auth.log)
    if ! grep -q "/var/log/syslog" /var/ossec/etc/ossec.conf; then
        sed -i '/<\/ossec_config>/i \
  <localfile> \
    <log_format>syslog</log_format> \
    <location>/var/log/syslog</location> \
  </localfile> \
  <localfile> \
    <log_format>syslog</log_format> \
    <location>/var/log/auth.log</location> \
  </localfile>' /var/ossec/etc/ossec.conf
    fi
    
    log "OpenClaw webhook integration (Level 12) and local log monitoring added to ossec.conf"
else
    log "Warning: Wazuh configuration file not found"
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

# Configure OpenClaw for proper webhook handling
log "Setting up OpenClaw configuration for webhook integration..."

# Ensure OpenClaw config directory exists
mkdir -p ~/.openclaw/wazuh-autopilot

# Copy OpenClaw autopilot configuration if it exists
if [ -d /opt/openclaw-autopilot/openclaw ]; then
    log "Copying OpenClaw autopilot configuration..."
    cp /opt/openclaw-autopilot/openclaw/openclaw.json ~/.openclaw/ 2>/dev/null || log "Warning: Could not copy OpenClaw config"
    cp -r /opt/openclaw-autopilot/openclaw/agents ~/.openclaw/wazuh-autopilot/ 2>/dev/null || log "Warning: Could not copy OpenClaw agents"
fi

# Display installation summary
log "=== Wazuh Installation Summary ==="
log "Wazuh Manager: Installed"
log "Wazuh Indexer: Installed" 
log "Wazuh Dashboard: Installed"
log "OpenClaw Integration: Configured"
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