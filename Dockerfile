# Build stage
FROM golang:1.23-alpine AS builder

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

# Build the unified application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /app/bin/go-alert \
    ./cmd/app

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
COPY --from=builder /app/bin/go-alert .

# Copy config files
COPY --from=builder /app/configs ./configs

# Copy web templates and static files
COPY --from=builder /app/internal/web/templates ./templates
COPY --from=builder /app/internal/web/static ./static

# Set ownership
RUN chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose port for web UI
EXPOSE 8080

# Environment variables
ENV WEB_PORT=8080
ENV TEMPLATES_DIR=/app/templates
ENV STATIC_DIR=/app/static
ENV ENABLE_UI=true
ENV ENABLE_SERVER=true

# Set entrypoint
ENTRYPOINT ["./go-alert"]

# Default command arguments (both UI and server enabled)
CMD ["-config", "configs/config.yaml"]
