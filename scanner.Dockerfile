# Build stage
FROM golang:1.25.3-alpine AS builder

# Install git and ca-certificates
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the workflow scanner binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o workflow-scanner ./cmd/scanner

# Final stage with Docker support for Dagger + optimized tools
FROM docker:dind

# Install all required tools in one layer for efficiency  
RUN apk --no-cache add \
    bash \
    curl \
    git \
    ca-certificates \
    jq \
    && rm -rf /var/cache/apk/*

# Install Dagger CLI
RUN curl -L https://dl.dagger.io/dagger/install.sh | sh && \
    mv bin/dagger /usr/local/bin/

# Copy the workflow scanner binary
COPY --from=builder /app/workflow-scanner /usr/local/bin/

# Make sure the binary is executable
RUN chmod +x /usr/local/bin/workflow-scanner

# Start Docker daemon and Dagger engine in background, then run scanner
CMD ["sh", "-c", "dockerd --host=unix:///var/run/docker.sock & \
    echo 'Waiting for Docker to start...' && \
    while ! docker info >/dev/null 2>&1; do sleep 1; done && \
    echo 'Docker is ready!' && \
    echo 'Pre-pulling Zizmor image...' && \
    docker pull ghcr.io/zizmorcore/zizmor:1.18.0 && \
    echo 'Zizmor image ready!' && \
    export DOCKER_HOST=unix:///var/run/docker.sock && \
    workflow-scanner"]