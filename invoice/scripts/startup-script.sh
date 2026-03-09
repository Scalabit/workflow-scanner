#!/bin/bash
set -e

# Update package lists
apt-get update

# Install packages without specific versions to avoid 404s
apt-get install -y --no-install-recommends python3-pip git golang-go

# Ensure go is in PATH
export PATH=$PATH:/usr/lib/go-1.18/bin:/usr/bin
which go || exit 1

# Install Python packages (with retry)
for i in {1..3}; do
    pip3 install torch transformers accelerate huggingface_hub && break
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