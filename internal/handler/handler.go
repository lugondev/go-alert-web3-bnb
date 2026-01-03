package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/redis"
	"github.com/lugondev/go-alert-web3-bnb/internal/telegram"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

// EventHandler processes WebSocket events and sends notifications
type EventHandler struct {
	notifier     *telegram.Notifier
	formatter    *telegram.Formatter
	deduplicator *redis.Deduplicator
	rateLimiter  *redis.RateLimiter
	pubsub       *redis.PubSub
	log          logger.Logger

	// Event filtering and processing options
	filters   []EventFilter
	filtersMu sync.RWMutex

	// Event queue for async processing
	eventQueue chan *models.Event
	workerDone chan struct{}

	// Configuration
	rateLimitConfig redis.RateLimitConfig
	enableDedup     bool
}

// EventFilter is a function that determines if an event should be processed
type EventFilter func(event *models.Event) bool

// Config holds event handler configuration
type Config struct {
	QueueSize       int
	WorkerCount     int
	EnableDedup     bool
	RateLimitKey    string
	RateLimitMax    int
	RateLimitWindow time.Duration
}

// NewEventHandler creates a new event handler
func NewEventHandler(
	notifier *telegram.Notifier,
	deduplicator *redis.Deduplicator,
	rateLimiter *redis.RateLimiter,
	pubsub *redis.PubSub,
	log logger.Logger,
	cfg Config,
) *EventHandler {
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 100
	}
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = 3
	}
	if cfg.RateLimitKey == "" {
		cfg.RateLimitKey = "telegram:notify"
	}
	if cfg.RateLimitMax == 0 {
		cfg.RateLimitMax = 30
	}
	if cfg.RateLimitWindow == 0 {
		cfg.RateLimitWindow = time.Minute
	}

	h := &EventHandler{
		notifier:     notifier,
		formatter:    telegram.NewFormatter(),
		deduplicator: deduplicator,
		rateLimiter:  rateLimiter,
		pubsub:       pubsub,
		log:          log.With(logger.F("component", "handler")),
		filters:      make([]EventFilter, 0),
		eventQueue:   make(chan *models.Event, cfg.QueueSize),
		workerDone:   make(chan struct{}),
		enableDedup:  cfg.EnableDedup,
		rateLimitConfig: redis.RateLimitConfig{
			Key:    cfg.RateLimitKey,
			Limit:  cfg.RateLimitMax,
			Window: cfg.RateLimitWindow,
		},
	}

	return h
}

// Start starts the event processing workers
func (h *EventHandler) Start(ctx context.Context, workerCount int) {
	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			h.worker(ctx, workerID)
		}(i)
	}

	go func() {
		wg.Wait()
		close(h.workerDone)
	}()
}

// Stop stops the event handler and waits for workers to finish
func (h *EventHandler) Stop() {
	close(h.eventQueue)
	<-h.workerDone
}

// worker processes events from the queue
func (h *EventHandler) worker(ctx context.Context, id int) {
	h.log.Debug("worker started", logger.F("worker_id", id))

	for {
		select {
		case <-ctx.Done():
			h.log.Debug("worker stopped due to context cancellation", logger.F("worker_id", id))
			return
		case event, ok := <-h.eventQueue:
			if !ok {
				h.log.Debug("worker stopped, queue closed", logger.F("worker_id", id))
				return
			}
			h.processEvent(ctx, event)
		}
	}
}

// Handle queues an event for processing (implements websocket.EventHandler)
func (h *EventHandler) Handle(event *models.Event) {
	// Apply filters
	if !h.shouldProcess(event) {
		h.log.Debug("event filtered out",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
		)
		return
	}

	// Try to queue the event
	select {
	case h.eventQueue <- event:
		h.log.Debug("event queued",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
		)
	default:
		h.log.Warn("event queue full, dropping event",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
		)
	}
}

// AddFilter adds an event filter
func (h *EventHandler) AddFilter(filter EventFilter) {
	h.filtersMu.Lock()
	defer h.filtersMu.Unlock()
	h.filters = append(h.filters, filter)
}

// shouldProcess checks if an event passes all filters
func (h *EventHandler) shouldProcess(event *models.Event) bool {
	h.filtersMu.RLock()
	defer h.filtersMu.RUnlock()

	for _, filter := range h.filters {
		if !filter(event) {
			return false
		}
	}
	return true
}

// processEvent processes a single event with deduplication
func (h *EventHandler) processEvent(ctx context.Context, event *models.Event) {
	h.log.Info("processing event",
		logger.F("event_type", string(event.Type)),
		logger.F("stream", event.Stream),
	)

	// Use deduplication if enabled
	if h.enableDedup && h.deduplicator != nil {
		err := h.deduplicator.ProcessWithDedup(ctx, event, func() error {
			return h.doProcessEvent(ctx, event)
		})

		if err != nil {
			if errors.Is(err, redis.ErrEventAlreadyProcessed) {
				h.log.Debug("event already processed by another instance",
					logger.F("event_type", string(event.Type)),
					logger.F("stream", event.Stream),
				)
				return
			}
			h.log.Error("failed to process event with dedup",
				logger.F("error", err),
				logger.F("event_type", string(event.Type)),
			)
		}
		return
	}

	// Process without deduplication
	if err := h.doProcessEvent(ctx, event); err != nil {
		h.log.Error("failed to process event",
			logger.F("error", err),
			logger.F("event_type", string(event.Type)),
		)
	}
}

// doProcessEvent performs the actual event processing and notification
func (h *EventHandler) doProcessEvent(ctx context.Context, event *models.Event) error {
	var message string
	var err error

	switch event.Type {
	case models.EventTypeTicker24h:
		message, err = h.handleTicker24h(event)
	case models.EventTypeKline:
		message, err = h.handleKline(event)
	case models.EventTypeHolders:
		message, err = h.handleHolders(event)
	case models.EventTypeTransaction:
		message, err = h.handleTransaction(event)
	case models.EventTypeBlock:
		message, err = h.handleBlock(event)
	case models.EventTypePrice:
		message, err = h.handlePrice(event)
	case models.EventTypeAlert:
		message, err = h.handleAlert(event)
	default:
		h.log.Debug("unknown event type, skipping",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
		)
		return nil
	}

	if err != nil {
		h.log.Error("failed to process event",
			logger.F("error", err),
			logger.F("event_type", string(event.Type)),
		)
		return err
	}

	if message == "" {
		return nil
	}

	// Skip if notifier is nil or disabled (check before rate limiting)
	if h.notifier == nil || !h.notifier.IsEnabled() {
		h.log.Debug("telegram notification skipped: notifier not enabled",
			logger.F("event_type", string(event.Type)),
		)
		return nil
	}

	// Check rate limit before sending
	if h.rateLimiter != nil {
		allowed, err := h.rateLimiter.Allow(ctx, h.rateLimitConfig)
		if err != nil {
			h.log.Error("rate limit check failed",
				logger.F("error", err),
			)
			// Continue anyway on rate limit check failure
		} else if !allowed {
			h.log.Warn("rate limit exceeded, skipping notification",
				logger.F("event_type", string(event.Type)),
			)
			return nil
		}
	}

	// Send to Telegram
	if err := h.notifier.SendMarkdown(ctx, message); err != nil {
		h.log.Error("failed to send telegram notification",
			logger.F("error", err),
			logger.F("event_type", string(event.Type)),
		)
		return err
	}

	// Publish event to other instances (for monitoring/logging)
	if h.pubsub != nil {
		if err := h.pubsub.PublishEvent(ctx, event); err != nil {
			h.log.Warn("failed to publish event to pubsub",
				logger.F("error", err),
			)
		}
	}

	h.log.Info("notification sent",
		logger.F("event_type", string(event.Type)),
		logger.F("stream", event.Stream),
	)

	return nil
}

// handleTicker24h processes ticker 24h events from W3W stream
func (h *EventHandler) handleTicker24h(event *models.Event) (string, error) {
	data, err := event.ParseTicker24hData()
	if err != nil {
		return "", fmt.Errorf("failed to parse ticker24h data: %w", err)
	}

	// Parse price and change values
	price, _ := strconv.ParseFloat(data.Price, 64)
	priceChange24h, _ := strconv.ParseFloat(data.PriceChange24h, 64)
	volume24h, _ := strconv.ParseFloat(data.Volume24h, 64)
	marketCap, _ := strconv.ParseFloat(data.MarketCap, 64)
	liquidity, _ := strconv.ParseFloat(data.Liquidity, 64)

	return h.formatter.FormatTicker24h(
		data.ContractAddress,
		price,
		priceChange24h,
		volume24h,
		marketCap,
		liquidity,
	), nil
}

// handleKline processes kline/candlestick events
func (h *EventHandler) handleKline(event *models.Event) (string, error) {
	data, err := event.ParseKlineData()
	if err != nil {
		h.log.Debug("failed to parse kline data", logger.F("raw_data", string(event.Data)), logger.F("error", err))
		return "", fmt.Errorf("failed to parse kline data: %w", err)
	}

	open, _ := strconv.ParseFloat(data.Open, 64)
	high, _ := strconv.ParseFloat(data.High, 64)
	low, _ := strconv.ParseFloat(data.Low, 64)
	closePrice, _ := strconv.ParseFloat(data.Close, 64)
	volume, _ := strconv.ParseFloat(data.Volume, 64)

	return h.formatter.FormatKline(
		data.ContractAddress,
		data.Interval,
		open,
		high,
		low,
		closePrice,
		volume,
		data.Closed,
	), nil
}

// handleHolders processes holder count events
func (h *EventHandler) handleHolders(event *models.Event) (string, error) {
	data, err := event.ParseHoldersData()
	if err != nil {
		return "", fmt.Errorf("failed to parse holders data: %w", err)
	}

	holderCount, _ := strconv.ParseInt(data.HolderCount, 10, 64)
	kycHolderCount, _ := strconv.ParseInt(data.KYCHolderCount, 10, 64)

	return h.formatter.FormatHolders(
		data.ContractAddress,
		holderCount,
		kycHolderCount,
	), nil
}

// handleTransaction processes transaction events
func (h *EventHandler) handleTransaction(event *models.Event) (string, error) {
	data, err := event.ParseTransactionData()
	if err != nil {
		return "", err
	}

	return h.formatter.FormatTransaction(
		data.Hash,
		data.From,
		data.To,
		data.Value,
		data.TokenSymbol,
		data.Amount,
	), nil
}

// handleBlock processes block events
func (h *EventHandler) handleBlock(event *models.Event) (string, error) {
	data, err := event.ParseBlockData()
	if err != nil {
		return "", err
	}

	return h.formatter.FormatBlock(
		data.Number,
		data.Hash,
		data.Transactions,
		data.GasUsed,
	), nil
}

// handlePrice processes price events
func (h *EventHandler) handlePrice(event *models.Event) (string, error) {
	data, err := event.ParsePriceData()
	if err != nil {
		return "", err
	}

	return h.formatter.FormatPrice(
		data.Symbol,
		data.Price,
		data.Change24h,
		data.ChangePercent,
	), nil
}

// handleAlert processes alert events
func (h *EventHandler) handleAlert(event *models.Event) (string, error) {
	data, err := event.ParseAlertData()
	if err != nil {
		return "", err
	}

	return h.formatter.FormatAlert(
		data.Level,
		data.Title,
		data.Message,
	), nil
}

// Common filters

// FilterByEventType creates a filter that only allows specific event types
func FilterByEventType(types ...models.EventType) EventFilter {
	typeSet := make(map[models.EventType]bool)
	for _, t := range types {
		typeSet[t] = true
	}

	return func(event *models.Event) bool {
		return typeSet[event.Type]
	}
}

// FilterByStream creates a filter that only allows specific streams
func FilterByStream(streams ...string) EventFilter {
	streamSet := make(map[string]bool)
	for _, s := range streams {
		streamSet[s] = true
	}

	return func(event *models.Event) bool {
		return streamSet[event.Stream]
	}
}

// FilterByMinValue creates a filter for transaction events with minimum value
func FilterByMinValue(minValue float64) EventFilter {
	return func(event *models.Event) bool {
		if event.Type != models.EventTypeTransaction {
			return true // Pass through non-transaction events
		}

		data, err := event.ParseTransactionData()
		if err != nil {
			return false
		}

		return data.Amount >= minValue
	}
}

// FilterByPriceChange creates a filter for ticker events with minimum change percentage
func FilterByPriceChange(minChangePercent float64) EventFilter {
	return func(event *models.Event) bool {
		if event.Type != models.EventTypeTicker24h {
			return true // Pass through non-ticker events
		}

		data, err := event.ParseTicker24hData()
		if err != nil {
			return false
		}

		// Check absolute value of 24h change
		change, _ := strconv.ParseFloat(data.PriceChange24h, 64)
		if change < 0 {
			change = -change
		}

		return change >= minChangePercent
	}
}

// FilterByMinVolume creates a filter for ticker events with minimum 24h volume
func FilterByMinVolume(minVolume float64) EventFilter {
	return func(event *models.Event) bool {
		if event.Type != models.EventTypeTicker24h {
			return true // Pass through non-ticker events
		}

		data, err := event.ParseTicker24hData()
		if err != nil {
			return false
		}

		volume, _ := strconv.ParseFloat(data.Volume24h, 64)
		return volume >= minVolume
	}
}
