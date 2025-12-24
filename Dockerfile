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

# Build the scanner binary for GitHub Actions
RUN CGO_ENABLED=0 GOOS=linux go build -o workflow-scanner ./cmd/scanner

# Final stage
FROM alpine:3.18

# Install ca-certificates and git (needed for git operations)
RUN apk --no-cache add ca-certificates git

# Create app directory
WORKDIR /root/

# Copy the scanner binary from builder stage
COPY --from=builder /app/workflow-scanner /usr/local/bin/workflow-scanner

# Run the scanner
ENTRYPOINT ["workflow-scanner"]