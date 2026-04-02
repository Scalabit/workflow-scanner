#!/bin/bash
# Ultra-Robust Wazuh Configuration Cleaner and Updater

OSSEC_CONF="/var/ossec/etc/ossec.conf"
TMP_BASE="/tmp/ossec.base"
TMP_FINAL="/tmp/ossec.final"

echo "Starting robust Wazuh configuration update..."

if [ -f "$OSSEC_CONF" ]; then
    # 1. Select source (prefer backup for a cleaner start)
    SOURCE="$OSSEC_CONF"
    if [ -f "${OSSEC_CONF}.backup" ]; then SOURCE="${OSSEC_CONF}.backup"; fi
    
    # 2. Clean everything: Remove ALL config tags and previous additions
    # This prevents the "End of file and some elements were not closed" error
    grep -vE "ossec_config|custom-openclaw|/var/log/syslog|/var/log/auth.log|<integration>|<\/integration>|<localfile>|<\/localfile>|<log_format>|<location>" "$SOURCE" > "$TMP_BASE"
    
    # 3. Build the new configuration from scratch
    {
      echo "<ossec_config>"
      cat "$TMP_BASE"
      echo ""
      echo "  <!-- OpenClaw Autonomous SOC Integration -->"
      echo "  <integration>"
      echo "    <name>custom-openclaw</name>"
      echo "    <level>12</level>"
      echo "    <alert_format>json</alert_format>"
      echo "  </integration>"
      echo ""
      echo "  <localfile>"
      echo "    <log_format>syslog</log_format>"
      echo "    <location>/var/log/syslog</location>"
      echo "  </localfile>"
      echo "  <localfile>"
      echo "    <log_format>syslog</log_format>"
      echo "    <location>/var/log/auth.log</location>"
      echo "  </localfile>"
      echo "</ossec_config>"
    } > "$TMP_FINAL"

    # 4. Verify before applying
    if /var/ossec/bin/wazuh-analysisd -t -c "$TMP_FINAL" > /dev/null 2>&1; then
        echo "Configuration validated successfully. Applying clean config..."
        sudo cp "$TMP_FINAL" "$OSSEC_CONF"
        sudo systemctl restart wazuh-manager
        echo "Wazuh manager restarted successfully."
    else
        echo "ERROR: Generated configuration is still invalid. Diagnostics follow:"
        /var/ossec/bin/wazuh-analysisd -t -c "$TMP_FINAL"
        exit 1
    fi
else
    echo "ERROR: ossec.conf not found at $OSSEC_CONF"
    exit 1
fi
