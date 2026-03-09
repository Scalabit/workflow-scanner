#!/bin/bash

apt update
apt install -y python3-pip git golang-go

pip3 install torch transformers accelerate huggingface_hub

mkdir -p /opt/phi-invoice
cd /opt/phi-invoice

git clone https://github.com/Scalabit/workflow-scanner.git .
cd invoice
go mod download
go build -o invoice-processor

export EMAIL_USERNAME=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-username" -H "Metadata-Flavor: Google")
export EMAIL_PASSWORD=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/email-password" -H "Metadata-Flavor: Google")
export IMAP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/imap-server" -H "Metadata-Flavor: Google")
export SMTP_SERVER=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/smtp-server" -H "Metadata-Flavor: Google")

./invoice-processor &