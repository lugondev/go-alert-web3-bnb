package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

const (
	// tickerCacheKeyPrefix is the prefix for ticker cache keys
	tickerCacheKeyPrefix = "alert:ticker:cache:"
	// tickerCacheTTL is the TTL for ticker cache entries
	tickerCacheTTL = 5 * time.Minute
	// tickerCacheMaxAge is the maximum age for cached data to be considered valid for display
	tickerCacheMaxAge = 3 * time.Minute
)

// TickerCacheData holds cached ticker data for a token
type TickerCacheData struct {
	// Token address (contract address)
	TokenAddress string `json:"token_address"`
	// Price in USD
	Price float64 `json:"price"`
	// Volume 24h buy (v24b)
	Volume24hBuy float64 `json:"volume_24h_buy"`
	// Volume 24h sell (v24s)
	Volume24hSell float64 `json:"volume_24h_sell"`
	// Total Volume 24h
	Volume24h float64 `json:"volume_24h"`
	// Market cap (mc)
	MarketCap float64 `json:"market_cap"`
	// Liquidity
	Liquidity float64 `json:"liquidity"`
	// Price change 24h percentage
	PriceChange24h float64 `json:"price_change_24h"`
	// Last updated timestamp
	UpdatedAt time.Time `json:"updated_at"`
}

// Implement TickerData interface for telegram formatter

// GetPrice returns the price
func (d *TickerCacheData) GetPrice() float64 {
	return d.Price
}

// GetMarketCap returns the market cap
func (d *TickerCacheData) GetMarketCap() float64 {
	return d.MarketCap
}

// GetVolume24h returns the 24h volume
func (d *TickerCacheData) GetVolume24h() float64 {
	return d.Volume24h
}

// GetPriceChange24h returns the 24h price change percentage
func (d *TickerCacheData) GetPriceChange24h() float64 {
	return d.PriceChange24h
}

// GetLiquidity returns the liquidity
func (d *TickerCacheData) GetLiquidity() float64 {
	return d.Liquidity
}

// IsValid checks if the cached data is still valid for display (less than maxAge old)
func (d *TickerCacheData) IsValid(maxAge time.Duration) bool {
	return time.Since(d.UpdatedAt) < maxAge
}

// Age returns the age of the cached data
func (d *TickerCacheData) Age() time.Duration {
	return time.Since(d.UpdatedAt)
}

// TickerCache provides caching for ticker24h data
type TickerCache struct {
	rdb    *redis.Client
	log    logger.Logger
	maxAge time.Duration
}

// NewTickerCache creates a new ticker cache
func NewTickerCache(rdb *redis.Client, log logger.Logger) *TickerCache {
	return &TickerCache{
		rdb:    rdb,
		log:    log.With(logger.F("component", "ticker-cache")),
		maxAge: tickerCacheMaxAge,
	}
}

// SetMaxAge sets the maximum age for cached data to be considered valid
func (c *TickerCache) SetMaxAge(maxAge time.Duration) {
	c.maxAge = maxAge
}

// cacheKey generates the cache key for a token address
func (c *TickerCache) cacheKey(tokenAddress string) string {
	return tickerCacheKeyPrefix + tokenAddress
}

// Set caches ticker data for a token
func (c *TickerCache) Set(ctx context.Context, data *TickerCacheData) error {
	if data == nil {
		return fmt.Errorf("ticker data is nil")
	}

	data.UpdatedAt = time.Now()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal ticker data: %w", err)
	}

	key := c.cacheKey(data.TokenAddress)
	if err := c.rdb.Set(ctx, key, jsonData, tickerCacheTTL).Err(); err != nil {
		return fmt.Errorf("failed to cache ticker data: %w", err)
	}

	c.log.Debug("ticker data cached",
		logger.F("token", data.TokenAddress),
		logger.F("price", data.Price),
		logger.F("market_cap", data.MarketCap),
		logger.F("volume_24h", data.Volume24h),
	)

	return nil
}

// Get retrieves cached ticker data for a token
// Returns nil if not found or expired
func (c *TickerCache) Get(ctx context.Context, tokenAddress string) (*TickerCacheData, error) {
	key := c.cacheKey(tokenAddress)

	jsonData, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to get ticker data: %w", err)
	}

	var data TickerCacheData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker data: %w", err)
	}

	return &data, nil
}

// GetValid retrieves cached ticker data only if it's still valid (within maxAge)
// Returns nil if not found, expired, or too old
func (c *TickerCache) GetValid(ctx context.Context, tokenAddress string) (*TickerCacheData, error) {
	data, err := c.Get(ctx, tokenAddress)
	if err != nil {
		return nil, err
	}

	if data == nil {
		return nil, nil
	}

	if !data.IsValid(c.maxAge) {
		c.log.Debug("ticker cache data too old",
			logger.F("token", tokenAddress),
			logger.F("age_seconds", data.Age().Seconds()),
			logger.F("max_age_seconds", c.maxAge.Seconds()),
		)
		return nil, nil
	}

	return data, nil
}

// Delete removes cached ticker data for a token
func (c *TickerCache) Delete(ctx context.Context, tokenAddress string) error {
	key := c.cacheKey(tokenAddress)
	return c.rdb.Del(ctx, key).Err()
}

// Clear removes all cached ticker data
func (c *TickerCache) Clear(ctx context.Context) error {
	pattern := tickerCacheKeyPrefix + "*"
	keys, err := c.rdb.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to list ticker cache keys: %w", err)
	}

	if len(keys) > 0 {
		if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete ticker cache keys: %w", err)
		}
		c.log.Info("cleared ticker cache", logger.F("keys_count", len(keys)))
	}

	return nil
}
