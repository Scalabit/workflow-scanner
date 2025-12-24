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

ENV GOGARBLE="workflow-scanner/*/cmd/scanner/main.go, workflow-scanner/*/pkg/*"
# Build the workflow scanner binary
RUN go install mvdan.cc/garble@latest

RUN CGO_ENABLED=0 GOOS=linux garble -literals -tiny -seed=random build -o workflow-scanner cmd/scanner/main.go 

# Runtime stage - minimal Alpine with just the binary
FROM alpine:3.19

# Install ca-certificates and git (needed for git operations)
RUN apk add --no-cache ca-certificates git

# Copy the binary from builder
COPY --from=builder /app/workflow-scanner /usr/local/bin/workflow-scanner

ENTRYPOINT ["workflow-scanner"]