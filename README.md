# Go Alert Web3 BNB

A Golang application that monitors blockchain events via WebSocket and sends alerts to Telegram with **Redis-based deduplication** for distributed deployments.

## Features

-   🔌 WebSocket client with auto-reconnection
-   📬 Telegram notifications with rate limiting
-   📝 Structured logging (JSON/Text format)
-   🎯 Event filtering and processing
-   🔄 **Redis pub/sub for cluster coordination**
-   🔒 **Distributed locking for event deduplication**
-   ⚡ **Prevents duplicate notifications when autoscaling**
-   🐳 Docker & Docker Compose support
-   ⚡ Graceful shutdown

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
                    │  ┌─────────────────┐  │
                    │  │ Distributed Lock │  │
                    │  │ Event Dedup     │  │
                    │  │ Pub/Sub         │  │
                    │  │ Rate Limiting   │  │
                    │  └─────────────────┘  │
                    └───────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │      Telegram         │
                    │   (Single Notify)     │
                    └───────────────────────┘
```

## Project Structure

```
.
├── cmd/
│   ├── server/          # Application entry point
│   └── wstest/          # WebSocket connection tester
├── internal/
│   ├── config/          # Configuration management
│   ├── handler/         # Event processing
│   ├── logger/          # Structured logging
│   ├── redis/           # Redis client, dedup, pubsub, rate limiter
│   ├── telegram/        # Telegram notifications
│   └── websocket/       # WebSocket client
├── pkg/
│   └── models/          # Shared data models
├── configs/             # Configuration files
├── Dockerfile
├── docker-compose.yaml
└── Makefile
```

## Quick Start

### Prerequisites

-   Go 1.23+
-   Redis 6+
-   Docker (optional)

### Installation

```bash
# Clone the repository
git clone https://github.com/lugondev/go-alert-web3-bnb.git
cd go-alert-web3-bnb

# Install dependencies
make deps

# Build
make build
```

### Configuration

1. Copy the example environment file:

```bash
cp .env.example .env
```

2. Edit `.env` with your settings:

```env
# Required
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id

# WebSocket
WS_URL=wss://nbstream.binance.com/w3w/stream

# Redis (for deduplication)
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
```

### Running

```bash
# Start Redis first (if not using Docker)
redis-server

# Run directly
make run

# Or with Docker Compose (includes Redis)
docker-compose up -d

# Scale to multiple instances (Redis handles deduplication)
docker-compose up -d --scale go-alert-web3-bnb=3
```

### WebSocket Test Tool

Test WebSocket connection and subscriptions:

```bash
# Run WebSocket tester
go run ./cmd/wstest/main.go
```

## W3W Stream Channels

The application supports various W3W (Web3 Watch) stream channels:

### Channel Formats

| Stream Type   | Format                                         | Example                                                    |
| ------------- | ---------------------------------------------- | ---------------------------------------------------------- |
| Ticker 24h    | `w3w@<contract>@<chain_type>@ticker24h`        | `w3w@So111...1111@CT_501@ticker24h`                        |
| Holders       | `w3w@<contract>@<chain_type>@holders`          | `w3w@pumpCmX...Dfn@CT_501@holders`                         |
| Transactions  | `tx@<chain_id>_<contract>`                     | `tx@16_pumpCmX...Dfn`                                      |
| Tagged Tx     | `tx@tag@<chain_id>_<contract>`                 | `tx@tag@16_pumpCmX...Dfn`                                  |
| Kline         | `kl@<chain_id>@<contract>@<interval>`          | `kl@16@pumpCmX...Dfn@5m`                                   |

### Chain Types

| Chain Type | Description |
| ---------- | ----------- |
| `CT_501`   | Solana      |

### Kline Intervals

Supported intervals: `1m`, `5m`, `15m`, `30m`, `1h`, `4h`, `1d`

### Subscribe Example

```go
subscribeMsg := map[string]interface{}{
    "id":     "3",
    "method": "SUBSCRIBE",
    "params": []string{
        "w3w@So11111111111111111111111111111111111111111@CT_501@ticker24h",
        "w3w@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@CT_501@holders",
        "tx@16_pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn",
        "kl@16@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@5m",
    },
}
```

## How Deduplication Works

When running multiple instances:

1. **Event Received**: Each instance receives the same WebSocket event
2. **Lock Acquisition**: Instances race to acquire a distributed lock in Redis
3. **Winner Processes**: Only the instance that acquires the lock processes the event
4. **Mark Processed**: Event is marked as processed with a TTL
5. **Notify Once**: Only one Telegram notification is sent

```go
// Automatic deduplication in handler
err := deduplicator.ProcessWithDedup(ctx, event, func() error {
    // This only runs on ONE instance
    return notifier.Send(ctx, message)
})

if errors.Is(err, redis.ErrEventAlreadyProcessed) {
    // Another instance already handled this event
    return
}
```

## Configuration Options

### Environment Variables

| Variable                | Description                | Default                                |
| ----------------------- | -------------------------- | -------------------------------------- |
| `APP_NAME`              | Application name           | `go-alert-web3-bnb`                    |
| `APP_ENV`               | Environment                | `development`                          |
| `WS_URL`                | WebSocket URL              | `wss://nbstream.binance.com/w3w/stream`|
| `WS_RECONNECT_INTERVAL` | Reconnect interval         | `5s`                                   |
| `WS_MAX_RETRIES`        | Max reconnection attempts  | `10`                                   |
| `TELEGRAM_BOT_TOKEN`    | Telegram bot token         | **Required**                           |
| `TELEGRAM_CHAT_ID`      | Telegram chat ID           | **Required**                           |
| `TELEGRAM_RATE_LIMIT`   | Messages per minute        | `30`                                   |
| `REDIS_HOST`            | Redis host                 | `localhost`                            |
| `REDIS_PORT`            | Redis port                 | `6379`                                 |
| `REDIS_PASSWORD`        | Redis password             | ``                                     |
| `REDIS_DB`              | Redis database             | `0`                                    |
| `REDIS_POOL_SIZE`       | Connection pool size       | `10`                                   |
| `REDIS_LOCK_TTL`        | Distributed lock TTL       | `30s`                                  |
| `REDIS_PROCESSED_TTL`   | Event processed marker TTL | `5m`                                   |
| `LOG_LEVEL`             | Log level                  | `info`                                 |
| `LOG_FORMAT`            | Log format (json/text)     | `json`                                 |
| `LOG_OUTPUT`            | Log output                 | `stdout`                               |

### YAML Configuration

The application also supports YAML configuration via `configs/config.yaml`:

```yaml
app:
  name: go-alert-web3-bnb
  environment: development

websocket:
  url: wss://nbstream.binance.com/w3w/stream
  reconnect_interval: 5s
  max_retries: 10
  ping_interval: 30s
  pong_timeout: 10s

telegram:
  bot_token: ${TELEGRAM_BOT_TOKEN}
  chat_id: ${TELEGRAM_CHAT_ID}
  rate_limit: 30

redis:
  host: localhost
  port: 6379
  lock_ttl: 30s
  processed_ttl: 5m

logger:
  level: debug
  format: text
```

## Event Types

The application supports the following event types:

-   `transaction` - Blockchain transactions
-   `block` - New blocks
-   `price` - Price updates
-   `alert` - General alerts

## Adding Event Filters

```go
// Filter by event type
eventHandler.AddFilter(handler.FilterByEventType(
    models.EventTypeTransaction,
    models.EventTypePrice,
))

// Filter by minimum transaction value
eventHandler.AddFilter(handler.FilterByMinValue(1000))

// Filter by price change percentage
eventHandler.AddFilter(handler.FilterByPriceChange(5.0))
```

## Redis Keys

The application uses the following Redis key patterns:

| Pattern                  | Purpose                               | TTL       |
| ------------------------ | ------------------------------------- | --------- |
| `event:lock:{hash}`      | Distributed lock for event processing | 30s       |
| `event:processed:{hash}` | Marker that event was processed       | 5m        |
| `ratelimit:{key}`        | Sliding window rate limit data        | 2x window |

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Format code
make fmt

# Run linter
make lint

# Tidy dependencies
make tidy

# Show all available commands
make help
```

## Production Deployment

### Docker Compose (Recommended)

```bash
# Start with 3 instances
docker-compose up -d --scale go-alert-web3-bnb=3

# View logs
docker-compose logs -f go-alert-web3-bnb

# Scale up/down dynamically
docker-compose up -d --scale go-alert-web3-bnb=5
```

### Kubernetes

For Kubernetes deployments, ensure:

1. Redis is deployed (or use managed Redis like AWS ElastiCache)
2. All pods point to the same Redis instance
3. Use `Deployment` with multiple replicas

## Monitoring

Each instance logs its unique Instance ID:

```json
{
	"timestamp": "2024-01-15T10:30:00Z",
	"level": "INFO",
	"message": "application started",
	"instance_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	"dedup_enabled": true
}
```

## Dependencies

| Package                         | Description              |
| ------------------------------- | ------------------------ |
| `github.com/gorilla/websocket`  | WebSocket client         |
| `github.com/redis/go-redis/v9`  | Redis client             |
| `github.com/charmbracelet/log`  | Structured logging       |
| `github.com/google/uuid`        | UUID generation          |
| `gopkg.in/yaml.v3`              | YAML configuration       |

## License

MIT
