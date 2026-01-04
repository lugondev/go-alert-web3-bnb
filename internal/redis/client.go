package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

// Config holds Redis connection configuration
type Config struct {
	Host         string
	Port         int
	Password     string
	DB           int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client wraps the Redis client with additional functionality
type Client struct {
	rdb *redis.Client
	log logger.Logger
}

// buildRedisAddr constructs Redis address from host and port.
// If host already contains a port (e.g., "host:port"), use it as-is.
// Otherwise, append the configured port.
func buildRedisAddr(host string, port int) string {
	// Check if host already contains port (has colon and something after it)
	if strings.Contains(host, ":") {
		return host
	}
	return fmt.Sprintf("%s:%d", host, port)
}

// NewClient creates a new Redis client
func NewClient(cfg Config, log logger.Logger) (*Client, error) {
	// Build Redis address - handle case where host already contains port
	addr := buildRedisAddr(cfg.Host, cfg.Port)

	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	client := &Client{
		rdb: rdb,
		log: log.With(logger.F("component", "redis")),
	}

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	client.log.Info("redis connected successfully",
		logger.F("addr", addr),
		logger.F("db", cfg.DB),
	)

	return client, nil
}

// GetClient returns the underlying Redis client
func (c *Client) GetClient() *redis.Client {
	return c.rdb
}

// Close closes the Redis connection
func (c *Client) Close() error {
	c.log.Info("closing redis connection")
	return c.rdb.Close()
}

// Ping checks if Redis is available
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Set sets a key-value pair with optional expiration
func (c *Client) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return c.rdb.Set(ctx, key, value, expiration).Err()
}

// Get retrieves a value by key
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Del deletes one or more keys
func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// Exists checks if keys exist
func (c *Client) Exists(ctx context.Context, keys ...string) (int64, error) {
	return c.rdb.Exists(ctx, keys...).Result()
}

// Expire sets expiration on a key
func (c *Client) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return c.rdb.Expire(ctx, key, expiration).Err()
}

// Incr increments a key's integer value
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.rdb.Incr(ctx, key).Result()
}

// SetNX sets a key only if it doesn't exist (for distributed locks)
func (c *Client) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return c.rdb.SetNX(ctx, key, value, expiration).Result()
}

// Health returns the health status of Redis
func (c *Client) Health(ctx context.Context) map[string]interface{} {
	health := map[string]interface{}{
		"status": "up",
	}

	info, err := c.rdb.Info(ctx, "server", "clients", "memory").Result()
	if err != nil {
		health["status"] = "down"
		health["error"] = err.Error()
		return health
	}

	health["info"] = info
	return health
}
