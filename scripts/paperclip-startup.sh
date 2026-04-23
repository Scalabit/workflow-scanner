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

# Install Paperclip CLI globally
npm install -g paperclipai@latest

# Create paperclip user
useradd -m -s /bin/bash paperclip
usermod -aG sudo paperclip

# Switch to paperclip user home directory
cd /home/paperclip

# Run onboarding as paperclip user
sudo -u paperclip paperclipai onboard

# Create systemd service for Paperclip
cat > /etc/systemd/system/paperclip.service << 'EOF'
[Unit]
Description=Paperclip AI Service
After=network.target

[Service]
Type=simple
User=paperclip
WorkingDirectory=/home/paperclip
Environment=HOST=0.0.0.0
ExecStart=/usr/bin/paperclipai run
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