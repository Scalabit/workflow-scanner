#!/bin/bash
set -e

# Update package lists
apt-get update

# Install basic packages and newer Go
apt-get install -y --no-install-recommends python3-pip git wget

# Install Go 1.21+
wget https://go.dev/dl/go1.21.6.linux-amd64.tar.gz
tar -C /usr/local -xzf go1.21.6.linux-amd64.tar.gz
export PATH=/usr/local/go/bin:$PATH
echo 'export PATH=/usr/local/go/bin:$PATH' >> /etc/profile
which go && go version

# Install Python packages (with retry and system packages flag)
for i in {1..3}; do
    pip3 install --break-system-packages torch transformers accelerate huggingface_hub && break
    echo "Retry $i/3 for pip install"
    sleep 5
done

# Create working directory
mkdir -p /opt/phi-invoice
cd /opt/phi-invoice

# Clone repo
git clone https://github.com/Scalabit/workflow-scanner.git . || exit 1
cd invoice || exit 1

# Verify files exist
ls -la main.go email.go || exit 1

# Set Go environment
export GOMODCACHE=/tmp/gomodcache
export GOCACHE=/tmp/gocache
mkdir -p $GOMODCACHE $GOCACHE

# Build the Go app
go mod download || exit 1
go build -o invoice-processor main.go email.go || exit 1

# Verify binary was created
ls -la invoice-processor || exit 1

# Get environment variables from metadata
export EMAIL_USERNAME=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-username" -H "Metadata-Flavor: Google")
export EMAIL_PASSWORD=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-password" -H "Metadata-Flavor: Google")
export IMAP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/imap-server" -H "Metadata-Flavor: Google")
export SMTP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/smtp-server" -H "Metadata-Flavor: Google")

# Start the service
echo "Starting invoice processor..."
nohup ./invoice-processor > invoice.log 2>&1 &
sleep 2

# Verify service is running
if pgrep invoice-processor; then
    echo "Invoice processor started successfully"
else
    echo "Failed to start invoice processor"
    cat invoice.log
    exit 1
fi