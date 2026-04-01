#!/bin/bash

# OpenClaw Configuration Setup Script
# Configures OpenClaw for Wazuh Autopilot integration

set -e

# Logging function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a /var/log/openclaw-config.log
}

log "Starting OpenClaw configuration for Wazuh Autopilot..."

# Create OpenClaw directory structure
log "Creating OpenClaw directory structure..."
mkdir -p ~/.openclaw/wazuh-autopilot

# Copy autopilot configuration if available
if [ -d /opt/openclaw-autopilot/openclaw ]; then
    log "Copying OpenClaw autopilot configuration..."
    
    # Copy main config with webhook mappings
    cp /opt/openclaw-autopilot/openclaw/openclaw.json ~/.openclaw/
    
    # Copy agent instruction files
    cp -r /opt/openclaw-autopilot/openclaw/agents ~/.openclaw/wazuh-autopilot/
    
    log "OpenClaw autopilot configuration copied successfully"
else
    log "Warning: OpenClaw autopilot directory not found at /opt/openclaw-autopilot/"
fi

# Verify webhook configuration
log "Verifying webhook configuration..."
if grep -q "wazuh-alert" ~/.openclaw/openclaw.json 2>/dev/null; then
    log "Webhook mappings found in OpenClaw configuration"
else
    log "Warning: No webhook mappings found in OpenClaw configuration"
fi

# Set proper permissions
chmod 644 ~/.openclaw/openclaw.json 2>/dev/null || log "Warning: Could not set config permissions"

# Set up webhook token from GitHub secrets
WEBHOOK_TOKEN_FILE="/opt/openclaw-autopilot/.env"
if [ -f "$WEBHOOK_TOKEN_FILE" ]; then
    # Check if webhook token is already configured
    if ! grep -q "OPENCLAW_WEBHOOK_TOKEN" "$WEBHOOK_TOKEN_FILE"; then
        log "Setting up webhook token from environment..."
        # This will be replaced by GitHub Actions with the actual secret
        echo "OPENCLAW_WEBHOOK_TOKEN=__OPENCLAW_WEBHOOK_TOKEN__" >> "$WEBHOOK_TOKEN_FILE"
        log "Webhook token placeholder added (will be replaced by GitHub Actions)"
    else
        log "Webhook token already configured"
    fi
else
    log "Warning: OpenClaw environment file not found at $WEBHOOK_TOKEN_FILE"
fi

log "OpenClaw configuration setup completed successfully!"