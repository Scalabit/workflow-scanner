#!/bin/bash
set -e

echo "Starting fresh OpenClaw Autopilot deployment..."

# 1. Clean up any previous installations
echo "Cleaning up previous installations..."
sudo systemctl stop wazuh-mcp-server wazuh-autopilot openclaw-gateway 2>/dev/null || true
sudo systemctl disable wazuh-mcp-server wazuh-autopilot openclaw-gateway 2>/dev/null || true
sudo rm -f /etc/systemd/system/wazuh-mcp-server.service /etc/systemd/system/wazuh-autopilot.service /etc/systemd/system/openclaw-gateway.service
sudo systemctl daemon-reload

# 2. Install Ollama
echo "Installing Ollama..."
if ! command -v ollama &> /dev/null; then
  curl -fsSL https://ollama.com/install.sh | sh
  sudo mkdir -p /etc/systemd/system/ollama.service.d/
  sudo tee /etc/systemd/system/ollama.service.d/override.conf > /dev/null << 'OLLAMA_CONF'
[Service]
Environment="OLLAMA_NUM_CTX=32768"
Environment="OLLAMA_HOST=0.0.0.0"
Environment="OLLAMA_KEEP_ALIVE=24h"
OLLAMA_CONF
  sudo systemctl daemon-reload
  sudo systemctl enable ollama
  sudo systemctl start ollama
  sleep 10
  ollama pull qwen2.5:0.5b
fi

# 3. System dependencies
echo "Installing system dependencies..."
sudo apt-get update
sudo DEBIAN_FRONTEND=noninteractive apt-get install -yq python3 python3-pip nodejs npm

# 4. MCP Server
echo "Installing MCP Server..."
sudo rm -rf /opt/wazuh-mcp-server
sudo git clone https://github.com/gensecaihq/Wazuh-MCP-Server /opt/wazuh-mcp-server
cd /opt/wazuh-mcp-server
sudo pip3 install -r requirements.txt

sudo tee /etc/systemd/system/wazuh-mcp-server.service > /dev/null << MCP_SERVICE
[Unit]
Description=Wazuh MCP Server
After=network.target wazuh-manager.service ollama.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/wazuh-mcp-server
Environment=WAZUH_HOST=localhost
Environment=WAZUH_PORT=55000
Environment=WAZUH_USER=wazuh
Environment=WAZUH_VERIFY_SSL=false
Environment=MCP_PORT=3001
Environment=MCP_HOST=0.0.0.0
Environment=AUTH_MODE=bearer
Environment=MCP_API_KEY=${MCP_API_KEY}
ExecStart=/usr/bin/python3 -m src.wazuh_mcp_server
Restart=always

[Install]
WantedBy=multi-user.target
MCP_SERVICE

sudo systemctl daemon-reload
sudo systemctl enable wazuh-mcp-server
sudo systemctl start wazuh-mcp-server

# 5. OpenClaw Autopilot
echo "Installing OpenClaw Autopilot..."
sudo rm -rf /opt/openclaw-autopilot
sudo git clone https://github.com/gensecaihq/Wazuh-Openclaw-Autopilot /opt/openclaw-autopilot
cd /opt/openclaw-autopilot/runtime/autopilot-service
sudo npm install

# Create .env
sudo tee /opt/openclaw-autopilot/.env << ENV_EOF
AUTOPILOT_MODE=bootstrap
MCP_URL=http://localhost:3001
AUTOPILOT_MCP_AUTH=${MCP_API_KEY}
OPENCLAW_LLM_MODE=local
AUTOPILOT_REQUIRE_TAILSCALE=false
AUTOPILOT_INSTALL_MODE=bootstrap
WAZUH_MANAGER_IP=localhost
WAZUH_MANAGER_PORT=1514
OLLAMA_URL=http://localhost:11434
ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
AUTOPILOT_RESPONDER_ENABLED=true
CORS_ORIGIN=*
OPENCLAW_GATEWAY_URL=http://$(curl -s ifconfig.me):18789
OPENCLAW_TOKEN=${OPENCLAW_TOKEN}
OPENCLAW_WEBHOOK_TOKEN=${OPENCLAW_WEBHOOK_TOKEN}
OPENCLAW_GATEWAY_TOKEN=${OPENCLAW_GATEWAY_TOKEN}
AUTOPILOT_API_KEY=${MCP_API_KEY}
OPENCLAW_HOST=0.0.0.0
OPENCLAW_PORT=18789
RUNTIME_PORT=9090
AUTOPILOT_DATA_DIR=/var/lib/wazuh-autopilot
AUTOPILOT_CONFIG_DIR=/etc/wazuh-autopilot
APPROVAL_TOKEN_TTL_MINUTES=60
ENV_EOF

# 6. OpenClaw CLI & Gateway
echo "Installing OpenClaw CLI..."
sudo npm install -g openclaw@latest

# OpenClaw Config
sudo mkdir -p /root/.openclaw /root/.openclaw/workspace /root/.openclaw/agents/main/sessions /var/lib/wazuh-autopilot
sudo cp /tmp/wazuh-autopilot/config/openclaw.json /root/.openclaw/openclaw.json

# Auth profiles
sudo mkdir -p /root/.openclaw/agents/main/agent
sudo tee /root/.openclaw/agents/main/agent/auth-profiles.json > /dev/null << 'AUTH_EOF'
{
  "ollama": {
    "baseUrl": "http://localhost:11434",
    "apiKey": "ollama-local"
  }
}
AUTH_EOF

# Replace placeholders
HOOKS_TOKEN=$(openssl rand -hex 24)
sudo sed -i "s/HOOKS_TOKEN_PLACEHOLDER/${HOOKS_TOKEN}/g" /root/.openclaw/openclaw.json
sudo sed -i "s/\${OPENCLAW_GATEWAY_TOKEN}/${OPENCLAW_GATEWAY_TOKEN}/g" /root/.openclaw/openclaw.json
sudo sed -i "s/\${ANTHROPIC_API_KEY}/${ANTHROPIC_API_KEY}/g" /root/.openclaw/openclaw.json

# Services
sudo tee /etc/systemd/system/wazuh-autopilot.service > /dev/null << 'AUTOPILOT_EOF'
[Unit]
Description=Wazuh OpenClaw Autopilot
After=network.target wazuh-manager.service wazuh-mcp-server.service ollama.service

[Service]
Type=simple
User=root
WorkingDirectory=/opt/openclaw-autopilot/runtime/autopilot-service
EnvironmentFile=/opt/openclaw-autopilot/.env
ExecStart=/usr/bin/node index.js
Restart=always

[Install]
WantedBy=multi-user.target
AUTOPILOT_EOF

OPENCLAW_PATH=$(which openclaw)
sudo tee /etc/systemd/system/openclaw-gateway.service << GATEWAY_EOF
[Unit]
Description=OpenClaw Gateway
After=network.target ollama.service

[Service]
Type=simple
User=root
WorkingDirectory=/root
EnvironmentFile=/opt/openclaw-autopilot/.env
ExecStart=$OPENCLAW_PATH gateway run --bind lan --port 18789 --force --token ${OPENCLAW_GATEWAY_TOKEN} --auth none
Restart=always

[Install]
WantedBy=multi-user.target
GATEWAY_EOF

sudo systemctl daemon-reload
sudo systemctl enable wazuh-autopilot openclaw-gateway
sudo systemctl start wazuh-autopilot openclaw-gateway

# 7. Wazuh Integration
echo "Configuring Wazuh integration..."
sudo cp /tmp/wazuh-autopilot/scripts/update-wazuh-webhooks.sh /usr/local/bin/update-wazuh-webhooks.sh
sudo chmod +x /usr/local/bin/update-wazuh-webhooks.sh

sudo tee /var/ossec/integrations/custom-openclaw > /dev/null << 'INTEGRATION_SCRIPT'
#!/bin/bash
WEBHOOK_TOKEN=""
if [ -f /opt/openclaw-autopilot/.env ]; then
    WEBHOOK_TOKEN=$(grep "OPENCLAW_WEBHOOK_TOKEN=" /opt/openclaw-autopilot/.env | cut -d'=' -f2)
fi
if [ -z "$WEBHOOK_TOKEN" ]; then
    WEBHOOK_TOKEN="HOOKS_TOKEN_PLACEHOLDER"
fi

while read ALERT; do
    curl -X POST -H "Content-Type: application/json" -H "Authorization: Bearer $WEBHOOK_TOKEN" \
        --data "$ALERT" "http://localhost:18789/hooks/wazuh-alert" \
        --silent --show-error --max-time 10 || \
        echo "$(date) - Failed to send alert to OpenClaw" >> /var/log/wazuh-openclaw-integration.log
done
INTEGRATION_SCRIPT

sudo sed -i "s/HOOKS_TOKEN_PLACEHOLDER/${HOOKS_TOKEN}/g" /var/ossec/integrations/custom-openclaw
sudo chmod 750 /var/ossec/integrations/custom-openclaw
sudo chown root:wazuh /var/ossec/integrations/custom-openclaw

sudo /usr/local/bin/update-wazuh-webhooks.sh

echo "Deployment complete!"
