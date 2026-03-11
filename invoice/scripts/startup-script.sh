#!/bin/bash
exec > /var/log/startup-script.log 2>&1
set -x

echo "=== Starting Invoice Processor Setup ==="

# Update and install packages
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y python3-pip git wget curl poppler-utils tesseract-ocr tesseract-ocr-eng

# Install Go 1.21+
echo "Installing Go..."
cd /tmp
wget -q https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
export PATH="/usr/local/go/bin:$PATH"
echo 'export PATH="/usr/local/go/bin:$PATH"' >> /etc/profile

# Verify Go installation
/usr/local/go/bin/go version

# Install Python packages (CPU optimized)
echo "Installing Python packages..."
pip3 install --break-system-packages --upgrade pip
pip3 install --break-system-packages torch --index-url https://download.pytorch.org/whl/cpu
pip3 install --break-system-packages transformers accelerate huggingface_hub pypdf

# Verify Python packages
python3 -c "import torch, transformers; print('Python packages installed successfully')"

# Clone and build application FIRST
echo "Setting up application..."
rm -rf /opt/phi-invoice
mkdir -p /opt/phi-invoice
cd /opt/phi-invoice

# Clone with retries
for i in {1..3}; do
    git clone https://github.com/Scalabit/workflow-scanner.git . && break
    echo "Retry $i/3 for git clone"
    sleep 5
done

cd invoice
ls -la main.go email.go

# Build application
export HOME=/root
export GOMODCACHE=/tmp/gomodcache
export GOCACHE=/tmp/gocache
export PATH="/usr/local/go/bin:$PATH"

/usr/local/go/bin/go mod download
/usr/local/go/bin/go build -o invoice-processor main.go email.go

# Verify binary
ls -la invoice-processor
chmod +x invoice-processor

# Pre-download Phi model AFTER app setup to avoid cache deletion
echo "Pre-downloading Phi model..."
export TRANSFORMERS_CACHE=/opt/phi-invoice/model_cache
mkdir -p $TRANSFORMERS_CACHE
chmod -R 777 $TRANSFORMERS_CACHE
export HF_HOME=$TRANSFORMERS_CACHE

python3 -c "
import os
os.environ['TRANSFORMERS_CACHE'] = '/opt/phi-invoice/model_cache'
os.environ['HF_HOME'] = '/opt/phi-invoice/model_cache'

from transformers import AutoTokenizer, AutoModelForCausalLM
import torch
print('Downloading Phi-3.5-mini tokenizer...')
tokenizer = AutoTokenizer.from_pretrained('microsoft/Phi-3.5-mini')
print('Downloading Phi-3.5-mini model...')
model = AutoModelForCausalLM.from_pretrained('microsoft/Phi-3.5-mini', torch_dtype=torch.float32, low_cpu_mem_usage=True, device_map='cpu')
print('Phi-3.5-mini download complete - cached at /opt/phi-invoice/model_cache')
"

# Get environment variables
echo "Getting environment variables..."
export EMAIL_USERNAME=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-username" -H "Metadata-Flavor: Google")
export EMAIL_PASSWORD=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-password" -H "Metadata-Flavor: Google") 
export IMAP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/imap-server" -H "Metadata-Flavor: Google")
export SMTP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/smtp-server" -H "Metadata-Flavor: Google")

echo "EMAIL_USERNAME: $EMAIL_USERNAME"
echo "IMAP_SERVER: $IMAP_SERVER"

# Create systemd service for proper daemon management
cat > /etc/systemd/system/invoice-processor.service << 'EOF'
[Unit]
Description=Invoice Processor Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/phi-invoice/invoice
ExecStart=/opt/phi-invoice/invoice/invoice-processor
Restart=always
Environment=EMAIL_USERNAME=%EMAIL_USERNAME%
Environment=EMAIL_PASSWORD=%EMAIL_PASSWORD%
Environment=IMAP_SERVER=%IMAP_SERVER%
Environment=SMTP_SERVER=%SMTP_SERVER%
Environment=PATH=%PATH%

[Install]
WantedBy=multi-user.target
EOF

# Replace environment variables in service file with proper escaping
sed -i "s|%EMAIL_USERNAME%|$EMAIL_USERNAME|g" /etc/systemd/system/invoice-processor.service
sed -i "s|%EMAIL_PASSWORD%|\"$EMAIL_PASSWORD\"|g" /etc/systemd/system/invoice-processor.service  
sed -i "s|%IMAP_SERVER%|$IMAP_SERVER|g" /etc/systemd/system/invoice-processor.service
sed -i "s|%SMTP_SERVER%|$SMTP_SERVER|g" /etc/systemd/system/invoice-processor.service
sed -i "s|%PATH%|/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/games:/usr/local/games:/snap/bin|g" /etc/systemd/system/invoice-processor.service

# Enable and start service
systemctl daemon-reload
systemctl enable invoice-processor
systemctl start invoice-processor

# Verify service is running
sleep 5
if systemctl is-active --quiet invoice-processor; then
    echo "Invoice processor service started successfully"
    systemctl status invoice-processor
else
    echo "Failed to start invoice processor service"
    systemctl status invoice-processor
    exit 1
fi

echo "=== Setup Complete ==="