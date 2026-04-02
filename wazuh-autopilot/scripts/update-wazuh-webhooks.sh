#!/bin/bash
# Wait for OpenClaw to be ready
echo "Waiting for OpenClaw Gateway to be ready..."
while ! curl -s http://localhost:18789/ > /dev/null 2>&1; do
    echo "Waiting for OpenClaw Gateway..."
    sleep 5
done

echo "OpenClaw Gateway is ready, updating Wazuh webhooks..."

OSSEC_CONF="/var/ossec/etc/ossec.conf"
if [ -f "$OSSEC_CONF" ]; then
    cp "$OSSEC_CONF" "${OSSEC_CONF}.pre-webhook"
    
    # Clean up any existing custom-openclaw integration to ensure a clean state
    # (Matches various formats used in previous attempts)
    sudo sed -i '/<integration>/,/<\/integration>/ { /custom-openclaw/d }' "$OSSEC_CONF"
    sudo sed -i '/<name>custom-openclaw<\/name>/d' "$OSSEC_CONF"

    # Add new integration block before the closing tag
    # Using level 12 as requested
    sudo sed -i '/<\/ossec_config>/i \
\
  <!-- OpenClaw Autonomous SOC Integration --> \
  <integration> \
    <name>custom-openclaw</name> \
    <level>12</level> <!-- High severity alerts for AI analysis --> \
    <alert_format>json</alert_format> \
    <max_log>50</max_log> \
  </integration>' "$OSSEC_CONF"
    
    echo "Webhook configuration added to ossec.conf (Level 12)"
    sudo systemctl restart wazuh-manager
    echo "Wazuh manager restarted"
else
    echo "Error: Wazuh configuration file not found"
    exit 1
fi
