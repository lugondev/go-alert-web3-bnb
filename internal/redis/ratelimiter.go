package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

const (
	prefixRateLimit = "ratelimit:"
)

// RateLimiter provides distributed rate limiting using Redis
type RateLimiter struct {
	client *Client
	log    logger.Logger
}

// RateLimitConfig holds rate limit configuration
type RateLimitConfig struct {
	Key    string        // Unique key for rate limiting (e.g., "telegram", "api")
	Limit  int           // Maximum number of requests
	Window time.Duration // Time window for the limit
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(client *Client, log logger.Logger) *RateLimiter {
	return &RateLimiter{
		client: client,
		log:    log.With(logger.F("component", "ratelimiter")),
	}
}

// Allow checks if an action is allowed under the rate limit
// Uses sliding window algorithm with Redis
func (r *RateLimiter) Allow(ctx context.Context, cfg RateLimitConfig) (bool, error) {
	key := prefixRateLimit + cfg.Key
	now := time.Now().UnixMilli()
	windowStart := now - cfg.Window.Milliseconds()

	// Use Lua script for atomic operations
	script := `
		-- Remove old entries outside the window
		redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
		
		-- Count current entries in the window
		local count = redis.call('ZCARD', KEYS[1])
		
		if count < tonumber(ARGV[2]) then
			-- Add new entry with current timestamp as score
			redis.call('ZADD', KEYS[1], ARGV[3], ARGV[3])
			-- Set expiration on the key
			redis.call('PEXPIRE', KEYS[1], ARGV[4])
			return 1
		else
			return 0
		end
	`

	result, err := r.client.rdb.Eval(ctx, script, []string{key},
		windowStart,                 // ARGV[1]: window start timestamp
		cfg.Limit,                   // ARGV[2]: max limit
		now,                         // ARGV[3]: current timestamp
		cfg.Window.Milliseconds()*2, // ARGV[4]: key expiration (2x window)
	).Int64()

	if err != nil {
		r.log.Error("rate limit check failed",
			logger.F("error", err),
			logger.F("key", cfg.Key),
		)
		return false, fmt.Errorf("rate limit check failed: %w", err)
	}

	allowed := result == 1

	if !allowed {
		r.log.Debug("rate limit exceeded",
			logger.F("key", cfg.Key),
			logger.F("limit", cfg.Limit),
			logger.F("window", cfg.Window),
		)
	}

	return allowed, nil
}

// GetCurrentCount returns the current count in the rate limit window
func (r *RateLimiter) GetCurrentCount(ctx context.Context, cfg RateLimitConfig) (int64, error) {
	key := prefixRateLimit + cfg.Key
	now := time.Now().UnixMilli()
	windowStart := now - cfg.Window.Milliseconds()

	// Remove old entries and count
	pipe := r.client.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", windowStart))
	countCmd := pipe.ZCard(ctx, key)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get rate limit count: %w", err)
	}

	return countCmd.Val(), nil
}

// Reset resets the rate limit for a key
func (r *RateLimiter) Reset(ctx context.Context, key string) error {
	return r.client.Del(ctx, prefixRateLimit+key)
}

// WaitForSlot waits until a slot becomes available or context is cancelled
func (r *RateLimiter) WaitForSlot(ctx context.Context, cfg RateLimitConfig) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			allowed, err := r.Allow(ctx, cfg)
			if err != nil {
				return err
			}
			if allowed {
				return nil
			}
		}
	}
}

// RemainingCapacity returns how many more requests are allowed in the current window
func (r *RateLimiter) RemainingCapacity(ctx context.Context, cfg RateLimitConfig) (int64, error) {
	count, err := r.GetCurrentCount(ctx, cfg)
	if err != nil {
		return 0, err
	}

	remaining := int64(cfg.Limit) - count
	if remaining < 0 {
		remaining = 0
	}

	return remaining, nil
}
