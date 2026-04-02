#!/bin/bash
# Safer Wazuh Webhook Configuration Script

OSSEC_CONF="/var/ossec/etc/ossec.conf"
TMP_CONF="/tmp/ossec.conf.tmp"

echo "Updating Wazuh configuration..."

if [ -f "$OSSEC_CONF" ]; then
    # 1. Create a clean base from the backup if possible, otherwise use current
    if [ -f "${OSSEC_CONF}.backup" ]; then
        cp "${OSSEC_CONF}.backup" "$TMP_CONF"
    else
        cp "$OSSEC_CONF" "$TMP_CONF"
    fi
    
    # 2. Remove any existing custom-openclaw sections to prevent duplicates
    # This is a robust way to strip the specific integration block
    sed -i '/<integration>/,/<\/integration>/ { /custom-openclaw/d; }' "$TMP_CONF"
    # Clean up empty integration tags left behind
    sed -i '/<integration>/{N;/<\/integration>/d;}' "$TMP_CONF"

    # 3. Insert the new clean integration block before the closing tag
    # Using Level 12 as requested
    sed -i '/<\/ossec_config>/i \
  <integration> \
    <name>custom-openclaw</name> \
    <level>12</level> \
    <alert_format>json</alert_format> \
  </integration>' "$TMP_CONF"

    # 4. Verify the config before applying
    if /var/ossec/bin/wazuh-analysisd -t -c "$TMP_CONF" > /dev/null 2>&1; then
        echo "Configuration validated successfully. Applying..."
        sudo cp "$TMP_CONF" "$OSSEC_CONF"
        sudo systemctl restart wazuh-manager
        echo "Wazuh manager restarted successfully."
    else
        echo "ERROR: New configuration is invalid. Check for XML syntax errors."
        /var/ossec/bin/wazuh-analysisd -t -c "$TMP_CONF"
        exit 1
    fi
else
    echo "ERROR: ossec.conf not found at $OSSEC_CONF"
    exit 1
fi
