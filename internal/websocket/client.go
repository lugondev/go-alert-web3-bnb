package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

var (
	ErrNotConnected   = errors.New("websocket not connected")
	ErrMaxRetries     = errors.New("max reconnect retries exceeded")
	ErrSendFailed     = errors.New("failed to send message")
	ErrInvalidMessage = errors.New("invalid message format")
)

// Config holds WebSocket client configuration
type Config struct {
	URL               string
	ReconnectInterval time.Duration
	MaxRetries        int
	PingInterval      time.Duration
	PongTimeout       time.Duration
	WriteTimeout      time.Duration
	ReadTimeout       time.Duration
}

// EventHandler is a function that handles incoming events
type EventHandler func(event *models.Event)

// Client represents a WebSocket client
type Client struct {
	config  Config
	conn    *websocket.Conn
	log     logger.Logger
	handler EventHandler

	mu           sync.RWMutex
	isConnected  bool
	retryCount   int
	done         chan struct{}
	reconnecting bool
	pingID       uint64 // Auto-incrementing ping ID

	// Track subscribed channels for unsubscribe on close
	subscribedChannels []string
	channelsMu         sync.RWMutex
}

// NewClient creates a new WebSocket client
func NewClient(cfg Config, log logger.Logger) *Client {
	return &Client{
		config: cfg,
		log:    log.With(logger.F("component", "websocket")),
		done:   make(chan struct{}),
	}
}

// SetHandler sets the event handler
func (c *Client) SetHandler(handler EventHandler) {
	c.handler = handler
}

// Connect establishes a WebSocket connection
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.isConnected {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	return c.connect(ctx)
}

// connect performs the actual connection
func (c *Client) connect(ctx context.Context) error {
	c.log.Info("connecting to websocket", logger.F("url", c.config.URL))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.config.URL, nil)
	if err != nil {
		c.log.Error("failed to connect to websocket",
			logger.F("error", err),
			logger.F("url", c.config.URL),
		)
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.isConnected = true
	c.retryCount = 0
	c.pingID = 0 // Reset ping ID on new connection
	c.mu.Unlock()

	c.log.Info("websocket connected successfully", logger.F("url", c.config.URL))

	// Setup ping/pong handlers
	c.setupPingPong()

	return nil
}

// setupPingPong configures ping/pong for connection keepalive
func (c *Client) setupPingPong() {
	c.conn.SetPongHandler(func(appData string) error {
		c.log.Debug("pong received")
		return c.conn.SetReadDeadline(time.Now().Add(c.config.PongTimeout))
	})
}

// Start begins reading messages from the WebSocket
func (c *Client) Start(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}

	go c.readLoop(ctx)
	go c.pingLoop(ctx)

	return nil
}

// readLoop continuously reads messages from the WebSocket
func (c *Client) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		c.isConnected = false
		c.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("context cancelled, stopping read loop")
			return
		case <-c.done:
			c.log.Info("done signal received, stopping read loop")
			return
		default:
		}

		if err := c.readMessage(ctx); err != nil {
			// Check if shutdown is in progress before attempting reconnect
			select {
			case <-ctx.Done():
				c.log.Info("context cancelled during read, stopping read loop")
				return
			case <-c.done:
				c.log.Info("done signal received during read, stopping read loop")
				return
			default:
			}

			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.log.Info("websocket closed normally")
				return
			}

			c.log.Error("read error", logger.F("error", err))

			// Attempt reconnection
			if err := c.reconnect(ctx); err != nil {
				c.log.Error("reconnection failed", logger.F("error", err))
				return
			}
		}
	}
}

// readMessage reads a single message from the WebSocket
func (c *Client) readMessage(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return ErrNotConnected
	}

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout)); err != nil {
		return err
	}

	_, message, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	c.log.Debug("raw message received",
		logger.F("size", len(message)),
		logger.F("raw", string(message)),
	)

	// Try to parse as W3W stream message first
	var streamMsg models.W3WStreamMessage
	if err := json.Unmarshal(message, &streamMsg); err == nil && streamMsg.Stream != "" {
		// This is a W3W stream message
		event := &models.Event{
			Stream:    streamMsg.Stream,
			Type:      models.ParseStreamType(streamMsg.Stream),
			Data:      streamMsg.Data,
			Timestamp: time.Now(),
		}

		c.log.Debug("parsed W3W stream message",
			logger.F("stream", streamMsg.Stream),
			logger.F("type", string(event.Type)),
		)

		if c.handler != nil {
			c.handler(event)
		}
		return nil
	}

	// Try to parse as subscribe response
	var subResp models.SubscribeResponse
	if err := json.Unmarshal(message, &subResp); err == nil && subResp.ID != "" {
		// Check if this is a ping response (GET_PROPERTY response)
		if subResp.Result != nil {
			c.log.Debug("pong received", logger.F("id", subResp.ID))
			// Extend read deadline on successful pong
			if err := conn.SetReadDeadline(time.Now().Add(c.config.PongTimeout)); err != nil {
				c.log.Warn("failed to extend read deadline", logger.F("error", err))
			}
		} else {
			c.log.Info("subscription response received",
				logger.F("id", subResp.ID),
				logger.F("result", subResp.Result),
			)
		}
		return nil
	}

	// Fallback: parse as generic event
	var event models.Event
	if err := json.Unmarshal(message, &event); err != nil {
		c.log.Warn("failed to parse message",
			logger.F("error", err),
			logger.F("message", string(message)),
		)
		return nil // Don't return error for parse failures
	}

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Call the handler if set
	if c.handler != nil {
		c.handler(&event)
	}

	return nil
}

// pingMessage represents the ping request message
type pingMessage struct {
	ID     string   `json:"id"`
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// pingLoop sends periodic ping messages using GET_PROPERTY method
func (c *Client) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			conn := c.conn
			isConnected := c.isConnected
			c.pingID++
			pingID := c.pingID
			c.mu.Unlock()

			if !isConnected || conn == nil {
				continue
			}

			// Create ping message with incrementing ID
			msg := pingMessage{
				ID:     fmt.Sprintf("%d", pingID),
				Method: "GET_PROPERTY",
				Params: []string{"combined"},
			}

			msgBytes, err := json.Marshal(msg)
			if err != nil {
				c.log.Error("failed to marshal ping message", logger.F("error", err))
				continue
			}

			if err := conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); err != nil {
				c.log.Error("failed to set write deadline", logger.F("error", err))
				continue
			}

			if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
				c.log.Error("failed to send ping", logger.F("error", err))
			} else {
				c.log.Debug("ping sent", logger.F("id", pingID))
			}
		}
	}
}

// reconnect attempts to reconnect to the WebSocket
func (c *Client) reconnect(ctx context.Context) error {
	// Check if shutdown is already in progress
	select {
	case <-ctx.Done():
		c.log.Info("reconnect skipped: context cancelled")
		return ctx.Err()
	case <-c.done:
		c.log.Info("reconnect skipped: done signal received")
		return nil
	default:
	}

	c.mu.Lock()
	if c.reconnecting {
		c.mu.Unlock()
		return nil
	}
	c.reconnecting = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.reconnecting = false
		c.mu.Unlock()
	}()

	for {
		c.mu.Lock()
		c.retryCount++
		retryCount := c.retryCount
		c.mu.Unlock()

		if retryCount > c.config.MaxRetries {
			return ErrMaxRetries
		}

		c.log.Info("attempting reconnection",
			logger.F("attempt", retryCount),
			logger.F("max_retries", c.config.MaxRetries),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		case <-time.After(c.config.ReconnectInterval):
			if err := c.connect(ctx); err != nil {
				c.log.Warn("reconnection attempt failed",
					logger.F("error", err),
					logger.F("attempt", retryCount),
				)
				continue
			}
			return nil
		}
	}
}

// Send sends a message through the WebSocket
func (c *Client) Send(ctx context.Context, data interface{}) error {
	c.mu.RLock()
	conn := c.conn
	isConnected := c.isConnected
	c.mu.RUnlock()

	if !isConnected || conn == nil {
		return ErrNotConnected
	}

	message, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if err := conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout)); err != nil {
		return err
	}

	if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
		c.log.Error("failed to send message", logger.F("error", err))
		return ErrSendFailed
	}

	c.log.Debug("message sent", logger.F("size", len(message)))
	return nil
}

// Subscribe sends a subscription message (common pattern for WebSocket APIs)
func (c *Client) Subscribe(ctx context.Context, channels []string) error {
	msg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": channels,
		"id":     time.Now().UnixNano(),
	}

	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	// Track subscribed channels
	c.channelsMu.Lock()
	c.subscribedChannels = append(c.subscribedChannels, channels...)
	c.channelsMu.Unlock()

	c.log.Debug("channels subscribed",
		logger.F("channels", channels),
		logger.F("count", len(channels)),
	)

	return nil
}

// Unsubscribe sends an unsubscription message
func (c *Client) Unsubscribe(ctx context.Context, channels []string) error {
	if len(channels) == 0 {
		return nil
	}

	msg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": channels,
		"id":     time.Now().UnixNano(),
	}

	if err := c.Send(ctx, msg); err != nil {
		return err
	}

	// Remove from tracked channels
	c.channelsMu.Lock()
	remaining := make([]string, 0, len(c.subscribedChannels))
	unsubSet := make(map[string]bool, len(channels))
	for _, ch := range channels {
		unsubSet[ch] = true
	}
	for _, ch := range c.subscribedChannels {
		if !unsubSet[ch] {
			remaining = append(remaining, ch)
		}
	}
	c.subscribedChannels = remaining
	c.channelsMu.Unlock()

	c.log.Debug("channels unsubscribed",
		logger.F("channels", channels),
		logger.F("count", len(channels)),
	)

	return nil
}

// UnsubscribeAll unsubscribes from all tracked channels
func (c *Client) UnsubscribeAll(ctx context.Context) error {
	c.channelsMu.RLock()
	channels := make([]string, len(c.subscribedChannels))
	copy(channels, c.subscribedChannels)
	c.channelsMu.RUnlock()

	if len(channels) == 0 {
		c.log.Debug("no channels to unsubscribe")
		return nil
	}

	c.log.Info("unsubscribing from all channels",
		logger.F("channels", channels),
		logger.F("count", len(channels)),
	)

	return c.Unsubscribe(ctx, channels)
}

// GetSubscribedChannels returns a copy of currently subscribed channels
func (c *Client) GetSubscribedChannels() []string {
	c.channelsMu.RLock()
	defer c.channelsMu.RUnlock()

	channels := make([]string, len(c.subscribedChannels))
	copy(channels, c.subscribedChannels)
	return channels
}

// Close closes the WebSocket connection gracefully
// It unsubscribes from all channels before closing
func (c *Client) Close() error {
	// Unsubscribe from all channels before closing
	c.channelsMu.RLock()
	channels := make([]string, len(c.subscribedChannels))
	copy(channels, c.subscribedChannels)
	c.channelsMu.RUnlock()

	if len(channels) > 0 {
		c.mu.RLock()
		conn := c.conn
		isConnected := c.isConnected
		c.mu.RUnlock()

		if isConnected && conn != nil {
			c.log.Info("unsubscribing from all channels before close",
				logger.F("channels", channels),
				logger.F("count", len(channels)),
			)

			// Create unsubscribe message
			msg := map[string]interface{}{
				"method": "UNSUBSCRIBE",
				"params": channels,
				"id":     time.Now().UnixNano(),
			}

			msgBytes, err := json.Marshal(msg)
			if err == nil {
				_ = conn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))
				if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
					c.log.Warn("failed to send unsubscribe message",
						logger.F("error", err),
					)
				} else {
					c.log.Info("unsubscribe message sent successfully")
				}
			}
		}

		// Clear tracked channels
		c.channelsMu.Lock()
		c.subscribedChannels = nil
		c.channelsMu.Unlock()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	close(c.done)

	if c.conn != nil {
		// Send close message
		_ = c.conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		err := c.conn.Close()
		c.conn = nil
		c.isConnected = false
		return err
	}

	return nil
}

// IsConnected returns the connection status
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isConnected
}
