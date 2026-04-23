#!/bin/bash

# Paperclip AI VM Setup Script
# This script installs Node.js 20+, pnpm, and Paperclip AI

set -e

# Update system
apt-get update && apt-get upgrade -y

# Install required packages
apt-get install -y curl git build-essential

# Install Node.js 20 LTS using NodeSource repository
curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
apt-get install -y nodejs

# Verify Node.js installation
node --version
npm --version

# Install pnpm globally
npm install -g pnpm@latest

# Verify pnpm installation
pnpm --version

# Create paperclip user
useradd -m -s /bin/bash paperclip
usermod -aG sudo paperclip

# Switch to paperclip user home directory
cd /home/paperclip

# Create Paperclip configuration directory and config file
sudo -u paperclip mkdir -p /home/paperclip/.paperclip

# Create config to bind to all interfaces
sudo -u paperclip cat > /home/paperclip/.paperclip/config.toml << 'EOF'
[server]
bind = "0.0.0.0"
port = 3100

[auth]
mode = "disabled"
EOF

# Install Paperclip AI using npx and run onboarding
sudo -u paperclip npx paperclipai onboard --yes

# Create systemd service for Paperclip
cat > /etc/systemd/system/paperclip.service << 'EOF'
[Unit]
Description=Paperclip AI Service
After=network.target

[Service]
Type=simple
User=paperclip
WorkingDirectory=/home/paperclip
Environment=PAPERCLIP_HOME=/home/paperclip/.paperclip
ExecStart=/usr/bin/npx paperclipai run
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOF

# Enable and start the service
systemctl daemon-reload
systemctl enable paperclip
systemctl start paperclip

# Create firewall rule for internal access
ufw allow 3100

# Log completion
echo "Paperclip AI installation completed successfully!"
echo "Service status:"
systemctl status paperclip --no-pager
echo "Paperclip AI should be accessible on port 3100"