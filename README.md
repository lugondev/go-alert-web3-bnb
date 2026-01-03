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
│   └── server/          # Application entry point
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

-   Go 1.21+
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
WS_URL=wss://stream.binance.com:9443/ws

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

| Variable                | Description                | Default                            |
| ----------------------- | -------------------------- | ---------------------------------- |
| `APP_NAME`              | Application name           | `go-alert-web3-bnb`                |
| `APP_ENV`               | Environment                | `development`                      |
| `WS_URL`                | WebSocket URL              | `wss://stream.binance.com:9443/ws` |
| `WS_RECONNECT_INTERVAL` | Reconnect interval         | `5s`                               |
| `WS_MAX_RETRIES`        | Max reconnection attempts  | `10`                               |
| `TELEGRAM_BOT_TOKEN`    | Telegram bot token         | **Required**                       |
| `TELEGRAM_CHAT_ID`      | Telegram chat ID           | **Required**                       |
| `TELEGRAM_RATE_LIMIT`   | Messages per minute        | `30`                               |
| `REDIS_HOST`            | Redis host                 | `localhost`                        |
| `REDIS_PORT`            | Redis port                 | `6379`                             |
| `REDIS_PASSWORD`        | Redis password             | ``                                 |
| `REDIS_DB`              | Redis database             | `0`                                |
| `REDIS_POOL_SIZE`       | Connection pool size       | `10`                               |
| `REDIS_LOCK_TTL`        | Distributed lock TTL       | `30s`                              |
| `REDIS_PROCESSED_TTL`   | Event processed marker TTL | `5m`                               |
| `LOG_LEVEL`             | Log level                  | `info`                             |
| `LOG_FORMAT`            | Log format (json/text)     | `json`                             |

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

## License

MIT
