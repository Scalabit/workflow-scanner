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

# Clone Paperclip repository
sudo -u paperclip git clone https://github.com/paperclipai/paperclip.git
cd paperclip

# Install dependencies
sudo -u paperclip pnpm install

# Create systemd service for Paperclip
cat > /etc/systemd/system/paperclip.service << 'EOF'
[Unit]
Description=Paperclip AI Service
After=network.target

[Service]
Type=simple
User=paperclip
WorkingDirectory=/home/paperclip/paperclip
Environment=HOST=0.0.0.0
Environment=PORT=3100
Environment=PAPERCLIP_HOME=/home/paperclip/.paperclip
ExecStart=/usr/bin/pnpm dev
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