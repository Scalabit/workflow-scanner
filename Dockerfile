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

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Build the web server
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server ./pkg/web/server.go

# Final stage
FROM alpine:3.18

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create app directory
WORKDIR /root/

# Copy the binaries from builder stage
COPY --from=builder /app/main .
COPY --from=builder /app/server .

# Copy frontend assets
COPY --from=builder /app/frontend ./frontend

# Expose port
EXPOSE 8080

# Run the web server
CMD ["./server"]