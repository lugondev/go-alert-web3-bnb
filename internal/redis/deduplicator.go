package redis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

var (
	ErrEventAlreadyProcessed = errors.New("event already processed")
	ErrLockNotAcquired       = errors.New("failed to acquire lock")
)

const (
	// Key prefixes
	prefixEventLock      = "event:lock:"
	prefixEventProcessed = "event:processed:"
	prefixInstanceLock   = "instance:lock:"

	// Default TTLs
	defaultLockTTL      = 30 * time.Second
	defaultProcessedTTL = 5 * time.Minute
)

// Deduplicator handles event deduplication across distributed instances
type Deduplicator struct {
	client     *Client
	log        logger.Logger
	instanceID string

	// Configuration
	lockTTL      time.Duration
	processedTTL time.Duration
}

// DeduplicatorConfig holds configuration for the deduplicator
type DeduplicatorConfig struct {
	LockTTL      time.Duration
	ProcessedTTL time.Duration
}

// NewDeduplicator creates a new event deduplicator
func NewDeduplicator(client *Client, log logger.Logger, cfg DeduplicatorConfig) *Deduplicator {
	if cfg.LockTTL == 0 {
		cfg.LockTTL = defaultLockTTL
	}
	if cfg.ProcessedTTL == 0 {
		cfg.ProcessedTTL = defaultProcessedTTL
	}

	return &Deduplicator{
		client:       client,
		log:          log.With(logger.F("component", "deduplicator")),
		instanceID:   uuid.New().String(),
		lockTTL:      cfg.LockTTL,
		processedTTL: cfg.ProcessedTTL,
	}
}

// GetInstanceID returns the unique instance identifier
func (d *Deduplicator) GetInstanceID() string {
	return d.instanceID
}

// GenerateEventKey generates a unique key for an event based on its content
func (d *Deduplicator) GenerateEventKey(event *models.Event) string {
	// Create a hash based on event type and data
	data := struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}{
		Type: string(event.Type),
		Data: event.Data,
	}

	jsonData, _ := json.Marshal(data)
	hash := sha256.Sum256(jsonData)
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes for shorter key
}

// TryAcquireLock attempts to acquire a distributed lock for processing an event
// Returns true if lock was acquired, false if another instance already has it
func (d *Deduplicator) TryAcquireLock(ctx context.Context, event *models.Event) (bool, error) {
	eventKey := d.GenerateEventKey(event)
	lockKey := prefixEventLock + eventKey

	// Try to set the lock with NX (only if not exists)
	acquired, err := d.client.SetNX(ctx, lockKey, d.instanceID, d.lockTTL)
	if err != nil {
		d.log.Error("failed to acquire lock",
			logger.F("error", err),
			logger.F("event_key", eventKey),
		)
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if acquired {
		d.log.Debug("lock acquired",
			logger.F("event_key", eventKey),
			logger.F("instance_id", d.instanceID),
		)
	} else {
		d.log.Debug("lock already held by another instance",
			logger.F("event_key", eventKey),
		)
	}

	return acquired, nil
}

// ReleaseLock releases the distributed lock for an event
func (d *Deduplicator) ReleaseLock(ctx context.Context, event *models.Event) error {
	eventKey := d.GenerateEventKey(event)
	lockKey := prefixEventLock + eventKey

	// Only release if we own the lock (using Lua script for atomicity)
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := d.client.rdb.Eval(ctx, script, []string{lockKey}, d.instanceID).Int64()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if result == 1 {
		d.log.Debug("lock released", logger.F("event_key", eventKey))
	}

	return nil
}

// MarkAsProcessed marks an event as processed to prevent reprocessing
func (d *Deduplicator) MarkAsProcessed(ctx context.Context, event *models.Event) error {
	eventKey := d.GenerateEventKey(event)
	processedKey := prefixEventProcessed + eventKey

	processedData := map[string]interface{}{
		"instance_id":  d.instanceID,
		"processed_at": time.Now().UTC().Format(time.RFC3339),
		"event_type":   string(event.Type),
	}

	jsonData, err := json.Marshal(processedData)
	if err != nil {
		return fmt.Errorf("failed to marshal processed data: %w", err)
	}

	if err := d.client.Set(ctx, processedKey, jsonData, d.processedTTL); err != nil {
		return fmt.Errorf("failed to mark as processed: %w", err)
	}

	d.log.Debug("event marked as processed",
		logger.F("event_key", eventKey),
		logger.F("ttl", d.processedTTL),
	)

	return nil
}

// IsProcessed checks if an event has already been processed
func (d *Deduplicator) IsProcessed(ctx context.Context, event *models.Event) (bool, error) {
	eventKey := d.GenerateEventKey(event)
	processedKey := prefixEventProcessed + eventKey

	exists, err := d.client.Exists(ctx, processedKey)
	if err != nil {
		return false, fmt.Errorf("failed to check if processed: %w", err)
	}

	return exists > 0, nil
}

// ShouldProcess is a convenience method that combines lock acquisition and processed check
// Returns true if this instance should process the event
func (d *Deduplicator) ShouldProcess(ctx context.Context, event *models.Event) (bool, error) {
	// First, check if already processed
	processed, err := d.IsProcessed(ctx, event)
	if err != nil {
		return false, err
	}
	if processed {
		d.log.Debug("event already processed, skipping",
			logger.F("event_type", string(event.Type)),
		)
		return false, nil
	}

	// Try to acquire lock
	acquired, err := d.TryAcquireLock(ctx, event)
	if err != nil {
		return false, err
	}
	if !acquired {
		d.log.Debug("another instance is processing this event",
			logger.F("event_type", string(event.Type)),
		)
		return false, nil
	}

	// Double-check if processed (in case another instance just finished)
	processed, err = d.IsProcessed(ctx, event)
	if err != nil {
		// Release lock on error
		_ = d.ReleaseLock(ctx, event)
		return false, err
	}
	if processed {
		_ = d.ReleaseLock(ctx, event)
		return false, nil
	}

	return true, nil
}

// ProcessWithDedup wraps event processing with deduplication logic
func (d *Deduplicator) ProcessWithDedup(ctx context.Context, event *models.Event, processor func() error) error {
	shouldProcess, err := d.ShouldProcess(ctx, event)
	if err != nil {
		return fmt.Errorf("dedup check failed: %w", err)
	}

	if !shouldProcess {
		return ErrEventAlreadyProcessed
	}

	// Process the event
	processErr := processor()

	// Mark as processed regardless of success (to prevent spam on errors)
	if markErr := d.MarkAsProcessed(ctx, event); markErr != nil {
		d.log.Error("failed to mark event as processed",
			logger.F("error", markErr),
		)
	}

	// Release the lock
	if releaseErr := d.ReleaseLock(ctx, event); releaseErr != nil {
		d.log.Error("failed to release lock",
			logger.F("error", releaseErr),
		)
	}

	return processErr
}
