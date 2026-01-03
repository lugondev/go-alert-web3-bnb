package redis

import (
	"context"
	"testing"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

// mockLogger implements logger.Logger for testing
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, fields ...logger.Field) {}
func (m *mockLogger) Info(msg string, fields ...logger.Field)  {}
func (m *mockLogger) Warn(msg string, fields ...logger.Field)  {}
func (m *mockLogger) Error(msg string, fields ...logger.Field) {}
func (m *mockLogger) Fatal(msg string, fields ...logger.Field) {}
func (m *mockLogger) With(fields ...logger.Field) logger.Logger {
	return m
}
func (m *mockLogger) WithContext(ctx context.Context) logger.Logger {
	return m
}

func TestDeduplicator_GenerateEventKey(t *testing.T) {
	log := &mockLogger{}

	// Create a mock client (won't be used for key generation)
	dedup := &Deduplicator{
		log:        log,
		instanceID: "test-instance",
	}

	event1 := &models.Event{
		Type: models.EventTypeTransaction,
		Data: []byte(`{"hash":"0x123","from":"0xabc"}`),
	}

	event2 := &models.Event{
		Type: models.EventTypeTransaction,
		Data: []byte(`{"hash":"0x123","from":"0xabc"}`),
	}

	event3 := &models.Event{
		Type: models.EventTypeTransaction,
		Data: []byte(`{"hash":"0x456","from":"0xdef"}`),
	}

	key1 := dedup.GenerateEventKey(event1)
	key2 := dedup.GenerateEventKey(event2)
	key3 := dedup.GenerateEventKey(event3)

	// Same events should produce same key
	if key1 != key2 {
		t.Errorf("expected same key for identical events, got %s and %s", key1, key2)
	}

	// Different events should produce different keys
	if key1 == key3 {
		t.Errorf("expected different keys for different events, got same: %s", key1)
	}

	// Key should be 32 characters (16 bytes hex encoded)
	if len(key1) != 32 {
		t.Errorf("expected key length 32, got %d", len(key1))
	}
}

func TestDeduplicator_GetInstanceID(t *testing.T) {
	log := &mockLogger{}

	dedup := &Deduplicator{
		log:        log,
		instanceID: "test-instance-123",
	}

	id := dedup.GetInstanceID()
	if id != "test-instance-123" {
		t.Errorf("expected instance ID 'test-instance-123', got %s", id)
	}
}

func TestDeduplicatorConfig_Defaults(t *testing.T) {
	cfg := DeduplicatorConfig{}

	if cfg.LockTTL != 0 {
		t.Errorf("expected LockTTL 0, got %v", cfg.LockTTL)
	}
	if cfg.ProcessedTTL != 0 {
		t.Errorf("expected ProcessedTTL 0, got %v", cfg.ProcessedTTL)
	}

	// Test that NewDeduplicator sets defaults
	// Note: This would require a real Redis connection in integration tests
}

func TestRateLimitConfig(t *testing.T) {
	cfg := RateLimitConfig{
		Key:    "test:limit",
		Limit:  100,
		Window: time.Minute,
	}

	if cfg.Key != "test:limit" {
		t.Errorf("expected key 'test:limit', got %s", cfg.Key)
	}
	if cfg.Limit != 100 {
		t.Errorf("expected limit 100, got %d", cfg.Limit)
	}
	if cfg.Window != time.Minute {
		t.Errorf("expected window 1m, got %v", cfg.Window)
	}
}

func TestPubSubMessage(t *testing.T) {
	msg := PubSubMessage{
		Type:       "test",
		InstanceID: "instance-1",
		Timestamp:  time.Now().Unix(),
	}

	if msg.Type != "test" {
		t.Errorf("expected type 'test', got %s", msg.Type)
	}
	if msg.InstanceID != "instance-1" {
		t.Errorf("expected instance ID 'instance-1', got %s", msg.InstanceID)
	}
}

func TestGetChannelNames(t *testing.T) {
	eventChannel := GetEventChannel()
	heartbeatChannel := GetHeartbeatChannel()

	if eventChannel != "events:broadcast" {
		t.Errorf("expected event channel 'events:broadcast', got %s", eventChannel)
	}
	if heartbeatChannel != "cluster:heartbeat" {
		t.Errorf("expected heartbeat channel 'cluster:heartbeat', got %s", heartbeatChannel)
	}
}

func TestErrors(t *testing.T) {
	if ErrEventAlreadyProcessed.Error() != "event already processed" {
		t.Errorf("unexpected error message: %s", ErrEventAlreadyProcessed.Error())
	}
	if ErrLockNotAcquired.Error() != "failed to acquire lock" {
		t.Errorf("unexpected error message: %s", ErrLockNotAcquired.Error())
	}
}
