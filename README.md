# Go Alert Web3 BNB

A Golang application that monitors blockchain events via WebSocket and sends alerts to Telegram with **Redis-based deduplication** for distributed deployments.

## Features

- WebSocket client with auto-reconnection
- Telegram notifications with rate limiting
- Structured logging (JSON/Text format)
- Event filtering and processing
- Redis pub/sub for cluster coordination
- Distributed locking for event deduplication
- Prevents duplicate notifications when autoscaling
- Docker & Docker Compose support
- Settings Web UI for configuration
- Graceful shutdown

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Instance 1    │    │   Instance 2    │    │   Instance N    │
│  (WebSocket)    │    │  (WebSocket)    │    │  (WebSocket)    │
└────────┬────────┘    └────────┬────────┘    └────────┬────────┘
         │                      │                      │
         └──────────────────────┼──────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │        Redis          │
                    │  - Distributed Lock   │
                    │  - Event Dedup        │
                    │  - Settings Storage   │
                    │  - Rate Limiting      │
                    └───────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │      Telegram         │
                    │   (Single Notify)     │
                    └───────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.23+
- Redis 6+
- Docker (optional)

### Installation

```bash
git clone https://github.com/lugondev/go-alert-web3-bnb.git
cd go-alert-web3-bnb

# Copy and configure environment
cp .env.example .env
# Edit .env with your TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID
```

### Running with Docker (Recommended)

```bash
# Start all services (Redis + Settings UI + Alert Bot)
./deploy.sh up

# View logs
./deploy.sh logs

# Scale alert bot instances
./deploy.sh scale 3

# Stop
./deploy.sh down
```

Settings UI available at: http://localhost:8080

### Running Locally

```bash
# Install dependencies
make deps

# Run tests
make test

# Build binaries
make build

# Run server (requires Redis running)
make run

# Run Settings UI
make run-ui
```

## Commands Reference

### Makefile (Development)

```bash
make help          # Show all commands

# Build
make build         # Build all binaries
make build-linux   # Cross-compile for Linux

# Run (local)
make run           # Run main server
make run-ui        # Run settings UI

# Quality
make test          # Run tests
make test-cover    # Run with coverage
make lint          # Run linter
make fmt           # Format code

# Dependencies
make deps          # Download deps
make tidy          # Tidy go.mod
```

### deploy.sh (Docker/Deployment)

```bash
./deploy.sh help   # Show all commands

# Docker
./deploy.sh build  # Build image
./deploy.sh up     # Start services (background)
./deploy.sh down   # Stop services
./deploy.sh logs   # View logs
./deploy.sh status # Service status

# Scaling
./deploy.sh scale 3  # Scale to 3 instances

# Deploy
./deploy.sh push     # Push to ghcr.io
./deploy.sh deploy   # Push + deploy to Coolify
```

## Configuration

### Environment Variables

| Variable              | Description              | Default                                   |
| --------------------- | ------------------------ | ----------------------------------------- |
| `TELEGRAM_BOT_TOKEN`  | Telegram bot token       | **Required**                              |
| `TELEGRAM_CHAT_ID`    | Telegram chat ID         | **Required**                              |
| `WS_URL`              | WebSocket URL            | `wss://nbstream.binance.com/w3w/stream`   |
| `REDIS_HOST`          | Redis host               | `localhost`                               |
| `REDIS_PORT`          | Redis port               | `6379`                                    |
| `REDIS_PASSWORD`      | Redis password           | ``                                        |
| `LOG_LEVEL`           | Log level                | `info`                                    |
| `LOG_FORMAT`          | Log format (json/text)   | `json`                                    |

### YAML Configuration

See `configs/config.yaml` for full configuration options including:
- WebSocket settings (reconnect, ping/pong)
- Telegram rate limiting
- Redis connection pooling
- Token subscriptions

## W3W Stream Channels

| Stream Type  | Format                                  | Example                               |
| ------------ | --------------------------------------- | ------------------------------------- |
| Ticker 24h   | `w3w@<contract>@<chain>@ticker24h`      | `w3w@So111...@CT_501@ticker24h`       |
| Holders      | `w3w@<contract>@<chain>@holders`        | `w3w@pump...@CT_501@holders`          |
| Transactions | `tx@<chain_id>_<contract>`              | `tx@16_pump...`                       |
| Kline        | `kl@<chain_id>@<contract>@<interval>`   | `kl@16@pump...@5m`                    |

**Chain Types:** `CT_501` (Solana), `56` (BSC), `8453` (Base)

**Kline Intervals:** `1m`, `5m`, `15m`, `30m`, `1h`, `4h`, `1d`

## How Deduplication Works

When running multiple instances:

1. **Event Received**: Each instance receives the same WebSocket event
2. **Lock Acquisition**: Instances race to acquire a distributed lock in Redis
3. **Winner Processes**: Only the instance that acquires the lock processes the event
4. **Mark Processed**: Event is marked as processed with a TTL
5. **Notify Once**: Only one Telegram notification is sent

```go
err := deduplicator.ProcessWithDedup(ctx, event, func() error {
    return notifier.Send(ctx, message)  // Only runs on ONE instance
})
```

## Project Structure

```
.
├── cmd/
│   ├── server/          # Main alert bot
│   ├── settings/        # Settings Web UI
│   └── wstest/          # WebSocket tester
├── internal/
│   ├── config/          # Configuration
│   ├── handler/         # Event processing
│   ├── logger/          # Logging
│   ├── redis/           # Redis (dedup, pubsub, rate limit)
│   ├── settings/        # Settings service
│   ├── telegram/        # Notifications
│   ├── web/             # Web UI (templates, static, handlers)
│   └── websocket/       # WebSocket client
├── pkg/models/          # Shared models
├── configs/             # Config files
├── Dockerfile
├── docker-compose.yaml
├── Makefile             # Dev commands
└── deploy.sh            # Docker/deploy commands
```

## Production Deployment

### Docker Compose

```bash
# Start with multiple instances
./deploy.sh up
./deploy.sh scale 3

# View logs
./deploy.sh logs go-alert-web3-bnb
```

### GitHub Container Registry

```bash
# Push to ghcr.io (requires: gh auth login)
./deploy.sh push v1.0.0

# Deploy to Coolify
./deploy.sh deploy v1.0.0
```

### Kubernetes

Ensure all pods point to the same Redis instance for deduplication to work.

## Redis Keys

| Pattern                  | Purpose                         | TTL  |
| ------------------------ | ------------------------------- | ---- |
| `event:lock:{hash}`      | Distributed lock                | 30s  |
| `event:processed:{hash}` | Processed marker                | 5m   |
| `ratelimit:{key}`        | Rate limit data                 | 2min |
| `settings:*`             | Token/Telegram settings         | -    |

## Dependencies

| Package                        | Description         |
| ------------------------------ | ------------------- |
| `github.com/gorilla/websocket` | WebSocket client    |
| `github.com/redis/go-redis/v9` | Redis client        |
| `github.com/charmbracelet/log` | Structured logging  |
| `github.com/google/uuid`       | UUID generation     |
| `gopkg.in/yaml.v3`             | YAML configuration  |

## License

MIT
