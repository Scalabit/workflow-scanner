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

# Build the batch scanner binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o batch-scanner ./cmd/batch

# Final stage with Docker support for Dagger
FROM docker:dind

# Install bash, curl, git, and ca-certificates
RUN apk --no-cache add bash curl git ca-certificates

# Install Dagger CLI
RUN curl -L https://dl.dagger.io/dagger/install.sh | sh && \
    mv bin/dagger /usr/local/bin/

# Copy the batch scanner binary
COPY --from=builder /app/batch-scanner /usr/local/bin/

# Make sure the binary is executable
RUN chmod +x /usr/local/bin/batch-scanner

# Start Docker daemon and Dagger engine in background, then run scanner
CMD ["sh", "-c", "dockerd --host=unix:///var/run/docker.sock & \
    echo 'Waiting for Docker to start...' && \
    while ! docker info >/dev/null 2>&1; do sleep 1; done && \
    echo 'Docker is ready!' && \
    export DOCKER_HOST=unix:///var/run/docker.sock && \
    batch-scanner"]