#!/bin/bash
# Final, Bulletproof Wazuh Configuration Fix

OSSEC_CONF="/var/ossec/etc/ossec.conf"
TMP_FINAL="/tmp/ossec.final"

echo "Applying final, bulletproof Wazuh configuration..."

# build a known-good configuration from scratch to avoid all previous corruption
cat > "$TMP_FINAL" << 'EOF'
<ossec_config>
  <global>
    <jsonout_output>yes</jsonout_output>
    <alerts_log>yes</alerts_log>
    <logall>no</logall>
    <update_check>yes</update_check>
  </global>

  <alerts>
    <log_alert_level>3</log_alert_level>
    <email_alert_level>12</email_alert_level>
  </alerts>

  <logging>
    <log_format>plain</log_format>
  </logging>

  <remote>
    <connection>secure</connection>
    <port>1514</port>
    <protocol>tcp</protocol>
    <queue_size>131072</queue_size>
  </remote>

  <rootcheck>
    <disabled>no</disabled>
    <frequency>43200</frequency>
  </rootcheck>

  <syscheck>
    <disabled>no</disabled>
    <frequency>43200</frequency>
    <scan_on_start>yes</scan_on_start>
    <directories>/etc,/usr/bin,/usr/sbin</directories>
    <directories>/bin,/sbin,/boot</directories>
  </syscheck>

  <ruleset>
    <decoder_dir>ruleset/decoders</decoder_dir>
    <rule_dir>ruleset/rules</rule_dir>
    <decoder_dir>etc/decoders</decoder_dir>
    <rule_dir>etc/rules</rule_dir>
  </ruleset>

  <auth>
    <disabled>no</disabled>
    <port>1515</port>
    <use_password>yes</use_password>
  </auth>

  <!-- OpenClaw Autonomous SOC Integration -->
  <integration>
    <name>custom-openclaw</name>
    <level>12</level>
    <alert_format>json</alert_format>
  </integration>

  <localfile>
    <log_format>syslog</log_format>
    <location>/var/log/syslog</location>
  </localfile>
  <localfile>
    <log_format>syslog</log_format>
    <location>/var/log/auth.log</location>
  </localfile>
</ossec_config>
EOF

# 2. Verify and Apply
if /var/ossec/bin/wazuh-analysisd -t -c "$TMP_FINAL" > /dev/null 2>&1; then
    echo "Configuration validated. Applying..."
    sudo cp "$TMP_FINAL" "$OSSEC_CONF"
    sudo systemctl restart wazuh-manager
    echo "Wazuh manager successfully restored."
else
    echo "CRITICAL ERROR: Fresh configuration failed validation."
    /var/ossec/bin/wazuh-analysisd -t -c "$TMP_FINAL"
    exit 1
fi
