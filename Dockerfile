# Build stage
FROM golang:1.21-alpine AS builder

# Install necessary packages
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/bin/go-alert-web3-bnb \
    ./cmd/server

# Final stage
FROM alpine:3.19

# Install ca-certificates and timezone data
RUN apk --no-cache add ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/bin/go-alert-web3-bnb .

# Copy config files
COPY --from=builder /app/configs ./configs

# Set ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port (if needed for health checks)
EXPOSE 8080

# Set entrypoint
ENTRYPOINT ["./go-alert-web3-bnb"]

# Default command arguments
CMD ["-config", "configs/config.yaml"]
