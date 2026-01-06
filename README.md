# Go Alert Web3 BNB

A Golang application that monitors blockchain events via WebSocket and sends alerts to Telegram with **Redis-based deduplication** for distributed deployments.

## Features

-   WebSocket client with auto-reconnection
-   Telegram notifications with rate limiting
-   **Web UI for Stream Configuration**:
    -   Visual modal interface for per-stream settings
    -   Click stream badges to configure
    -   No JSON editing required
-   **Per-stream configuration**:
    -   Custom Telegram bots for each stream
    -   Multiple Telegram bots per stream
    -   TX stream filters (min value, buy/sell/both)
    -   Stream-specific rate limiting
-   Structured logging (JSON/Text format)
-   Event filtering and processing
-   Redis pub/sub for cluster coordination
-   Distributed locking for event deduplication
-   Prevents duplicate notifications when autoscaling
-   Docker & Docker Compose support
-   Settings Web UI for configuration
-   Graceful shutdown

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

-   Go 1.23+
-   Redis 6+
-   Docker (optional)

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

![Home](./images/home.png)
![Settings](./images/setting.png)

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

| Variable             | Description            | Default                                 |
| -------------------- | ---------------------- | --------------------------------------- |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token     | **Required**                            |
| `TELEGRAM_CHAT_ID`   | Telegram chat ID       | **Required**                            |
| `WS_URL`             | WebSocket URL          | `wss://nbstream.binance.com/w3w/stream` |
| `REDIS_HOST`         | Redis host             | `localhost`                             |
| `REDIS_PORT`         | Redis port             | `6379`                                  |
| `REDIS_PASSWORD`     | Redis password         | ``                                      |
| `LOG_LEVEL`          | Log level              | `info`                                  |
| `LOG_FORMAT`         | Log format (json/text) | `json`                                  |

### YAML Configuration

See `configs/config.yaml` for full configuration options including:

-   WebSocket settings (reconnect, ping/pong)
-   Telegram rate limiting
-   Redis connection pooling
-   Token subscriptions

### Stream-Specific Configuration

Each stream can be configured independently with custom settings via the **Web UI** or Redis.

#### Using the Web UI (Recommended)

1. Navigate to http://localhost:8080/tokens
2. Find your token in the list
3. Click on any stream badge (e.g., `tx`, `ticker24h`)
4. Configure in the modal:
   - Enable/disable notifications
   - Add multiple Telegram bots
   - Set TX filters (min value, buy/sell)
   - Configure rate limiting
5. Click "Save Configuration"

**Visual Guide:**

```
Tokens Page → Stream Badge → Configuration Modal
                  ↓
         ┌──────────────────┐
         │ [tx] [ticker24h] │ ← Click to configure
         └──────────────────┘
                  ↓
    ┌─────────────────────────────┐
    │ Stream Configuration Modal  │
    │                             │
    │ ☑ Enable Notifications      │
    │                             │
    │ Telegram Bots  [+ Add Bot]  │
    │ TX Filters (min value, etc) │
    │ Rate Limiting               │
    │                             │
    │     [Cancel] [Save]         │
    └─────────────────────────────┘
```

See [Web UI Guide](docs/STREAM_CONFIG_UI.md) for detailed instructions.

#### Using Redis (Advanced)

Store configuration as JSON in Redis:

```bash
redis-cli SET "settings:token:{token-id}" '{
  "stream_notify": {
    "tx": {
      "enabled": true,
      "telegram_bots": [...],
      "tx_min_value_usd": 1000,
      "tx_filter_type": "buy",
      "rate_limit": 10,
      "rate_limit_window": 60000000000
    }
  }
}'
```

See [API Documentation](docs/STREAM_CONFIG.md) for JSON format and `configs/stream-config-example.json` for examples.

#### Available Options

| Option                | Type            | Description                                    | Default       |
| --------------------- | --------------- | ---------------------------------------------- | ------------- |
| `enabled`             | boolean         | Enable/disable notifications for this stream   | `true`        |
| `telegram_bots`       | array           | List of Telegram bots to send notifications to | Global config |
| `tx_min_value_usd`    | number          | Minimum transaction value in USD (TX only)     | `0` (no min)  |
| `tx_filter_type`      | string          | Filter: `"both"`, `"buy"`, or `"sell"` (TX)    | `"both"`      |
| `rate_limit`          | number          | Messages per window                            | Global limit  |
| `rate_limit_window`   | duration string | Rate limit window (e.g., `"1m"`, `"5m"`)       | Global window |

#### Example: Configure TX Stream

```json
{
  "enabled": true,
  "telegram_bots": [
    {
      "bot_token": "123456:ABC-DEF...",
      "chat_id": "-1001234567890",
      "enabled": true,
      "name": "High Value Alerts"
    },
    {
      "bot_token": "789012:GHI-JKL...",
      "chat_id": "-1009876543210",
      "enabled": true,
      "name": "Backup Bot"
    }
  ],
  "tx_min_value_usd": 1000,
  "tx_filter_type": "buy",
  "rate_limit": 10,
  "rate_limit_window": "1m"
}
```

This configuration will:
- Only notify for BUY transactions (ignore sells)
- Filter transactions below $1000 USD
- Send to 2 different Telegram bots simultaneously
- Limit to 10 messages per minute for this stream

#### Use Cases

**1. High-Value Transaction Alerts**
```json
{
  "tx_min_value_usd": 10000,
  "tx_filter_type": "both",
  "telegram_bots": [{"bot_token": "...", "chat_id": "...", "enabled": true}]
}
```

**2. Buy-Only Monitoring**
```json
{
  "tx_filter_type": "buy",
  "rate_limit": 5,
  "rate_limit_window": "1m"
}
```

**3. Multiple Notification Channels**
```json
{
  "telegram_bots": [
    {"bot_token": "BOT1", "chat_id": "TEAM_CHAT", "name": "Team"},
    {"bot_token": "BOT2", "chat_id": "VIP_CHAT", "name": "VIP"},
    {"bot_token": "BOT3", "chat_id": "PUBLIC_CHAT", "name": "Public"}
  ]
}
```

## W3W Stream Channels

| Stream Type  | Format                                | Example                         |
| ------------ | ------------------------------------- | ------------------------------- |
| Ticker 24h   | `w3w@<contract>@<chain>@ticker24h`    | `w3w@So111...@CT_501@ticker24h` |
| Holders      | `w3w@<contract>@<chain>@holders`      | `w3w@pump...@CT_501@holders`    |
| Transactions | `tx@<chain_id>_<contract>`            | `tx@16_pump...`                 |
| Kline        | `kl@<chain_id>@<contract>@<interval>` | `kl@16@pump...@5m`              |

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

| Pattern                  | Purpose                 | TTL  |
| ------------------------ | ----------------------- | ---- |
| `event:lock:{hash}`      | Distributed lock        | 30s  |
| `event:processed:{hash}` | Processed marker        | 5m   |
| `ratelimit:{key}`        | Rate limit data         | 2min |
| `settings:*`             | Token/Telegram settings | -    |

## Dependencies

| Package                        | Description        |
| ------------------------------ | ------------------ |
| `github.com/gorilla/websocket` | WebSocket client   |
| `github.com/redis/go-redis/v9` | Redis client       |
| `github.com/charmbracelet/log` | Structured logging |
| `github.com/google/uuid`       | UUID generation    |
| `gopkg.in/yaml.v3`             | YAML configuration |

## License

MIT
