package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

const (
	// Channel names
	channelEvents    = "events:broadcast"
	channelHeartbeat = "cluster:heartbeat"
	channelLeader    = "cluster:leader"
)

// PubSubMessage represents a message sent via pub/sub
type PubSubMessage struct {
	Type       string          `json:"type"`
	InstanceID string          `json:"instance_id"`
	Timestamp  int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// MessageHandler is a function that handles incoming pub/sub messages
type MessageHandler func(msg *PubSubMessage)

// PubSub handles Redis pub/sub operations for cluster coordination
type PubSub struct {
	client     *Client
	log        logger.Logger
	instanceID string

	pubsub   *redis.PubSub
	handlers map[string][]MessageHandler
	mu       sync.RWMutex

	done chan struct{}
}

// NewPubSub creates a new pub/sub handler
func NewPubSub(client *Client, instanceID string, log logger.Logger) *PubSub {
	return &PubSub{
		client:     client,
		log:        log.With(logger.F("component", "pubsub")),
		instanceID: instanceID,
		handlers:   make(map[string][]MessageHandler),
		done:       make(chan struct{}),
	}
}

// Subscribe subscribes to one or more channels
func (p *PubSub) Subscribe(ctx context.Context, channels ...string) error {
	p.pubsub = p.client.rdb.Subscribe(ctx, channels...)

	// Wait for subscription confirmation
	_, err := p.pubsub.Receive(ctx)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	p.log.Info("subscribed to channels", logger.F("channels", channels))
	return nil
}

// Start begins listening for messages
func (p *PubSub) Start(ctx context.Context) {
	go p.listen(ctx)
}

// listen processes incoming pub/sub messages
func (p *PubSub) listen(ctx context.Context) {
	if p.pubsub == nil {
		p.log.Error("pubsub not initialized, call Subscribe first")
		return
	}

	ch := p.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("stopping pubsub listener due to context cancellation")
			return
		case <-p.done:
			p.log.Info("stopping pubsub listener")
			return
		case msg, ok := <-ch:
			if !ok {
				p.log.Warn("pubsub channel closed")
				return
			}
			p.handleMessage(msg)
		}
	}
}

// handleMessage processes a single pub/sub message
func (p *PubSub) handleMessage(msg *redis.Message) {
	var pubsubMsg PubSubMessage
	if err := json.Unmarshal([]byte(msg.Payload), &pubsubMsg); err != nil {
		p.log.Warn("failed to unmarshal pubsub message",
			logger.F("error", err),
			logger.F("channel", msg.Channel),
		)
		return
	}

	// Skip messages from self
	if pubsubMsg.InstanceID == p.instanceID {
		return
	}

	p.log.Debug("received pubsub message",
		logger.F("channel", msg.Channel),
		logger.F("type", pubsubMsg.Type),
		logger.F("from_instance", pubsubMsg.InstanceID),
	)

	// Call registered handlers
	p.mu.RLock()
	handlers := p.handlers[msg.Channel]
	p.mu.RUnlock()

	for _, handler := range handlers {
		handler(&pubsubMsg)
	}
}

// RegisterHandler registers a handler for a specific channel
func (p *PubSub) RegisterHandler(channel string, handler MessageHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handlers[channel] = append(p.handlers[channel], handler)
}

// Publish publishes a message to a channel
func (p *PubSub) Publish(ctx context.Context, channel string, msgType string, payload interface{}) error {
	var payloadJSON json.RawMessage
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		payloadJSON = data
	}

	msg := PubSubMessage{
		Type:       msgType,
		InstanceID: p.instanceID,
		Timestamp:  time.Now().Unix(),
		Payload:    payloadJSON,
	}

	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := p.client.rdb.Publish(ctx, channel, msgJSON).Err(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	p.log.Debug("published message",
		logger.F("channel", channel),
		logger.F("type", msgType),
	)

	return nil
}

// PublishEvent publishes an event notification to other instances
func (p *PubSub) PublishEvent(ctx context.Context, event *models.Event) error {
	return p.Publish(ctx, channelEvents, string(event.Type), event)
}

// PublishHeartbeat publishes a heartbeat message
func (p *PubSub) PublishHeartbeat(ctx context.Context) error {
	return p.Publish(ctx, channelHeartbeat, "heartbeat", map[string]string{
		"instance_id": p.instanceID,
		"status":      "alive",
	})
}

// Close closes the pub/sub connection
func (p *PubSub) Close() error {
	close(p.done)

	if p.pubsub != nil {
		return p.pubsub.Close()
	}
	return nil
}

// GetEventChannel returns the events broadcast channel name
func GetEventChannel() string {
	return channelEvents
}

// GetHeartbeatChannel returns the heartbeat channel name
func GetHeartbeatChannel() string {
	return channelHeartbeat
}
