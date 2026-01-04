package handler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/redis"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
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
	tickerCache  *redis.TickerCache
	settingsSvc  *settings.Service
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
	tickerCache *redis.TickerCache,
	settingsSvc *settings.Service,
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
		tickerCache:  tickerCache,
		settingsSvc:  settingsSvc,
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

	// Log settings service status
	if settingsSvc != nil {
		h.log.Info("event handler initialized with settings service")
	} else {
		h.log.Warn("event handler initialized WITHOUT settings service - notify check will be skipped")
	}

	// Log ticker cache status
	if tickerCache != nil {
		h.log.Info("event handler initialized with ticker cache")
	} else {
		h.log.Warn("event handler initialized WITHOUT ticker cache - transaction alerts won't include ticker data")
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

// Stop stops the event handler and waits for workers to finish with timeout
func (h *EventHandler) Stop() {
	close(h.eventQueue)

	// Wait for workers with timeout
	select {
	case <-h.workerDone:
		h.log.Debug("all workers stopped gracefully")
	case <-time.After(2 * time.Second):
		h.log.Warn("worker shutdown timeout, some workers may still be running")
	}
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
		message, err = h.handleTicker24h(ctx, event)
	case models.EventTypeKline:
		message, err = h.handleKline(event)
	case models.EventTypeHolders:
		message, err = h.handleHolders(event)
	case models.EventTypeTransaction:
		message, err = h.handleTransaction(ctx, event)
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

	// Check if token notification is enabled
	if !h.isTokenNotifyEnabled(ctx, event.Stream) {
		h.log.Warn("SKIPPING notification - token notify disabled",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
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

// isTokenNotifyEnabled checks if notification is enabled for the token and stream
// It checks global Telegram settings, per-token notification settings, and per-stream settings
// Stream format: w3w@<token_address>@<chain_type>@<stream_type>
// or: kl@<chain_id>@<token_address>@<interval>
// or: tx@<chain_id>_<token_address>
func (h *EventHandler) isTokenNotifyEnabled(ctx context.Context, stream string) bool {
	if h.settingsSvc == nil {
		h.log.Warn("settingsSvc is nil, allowing notification")
		return true // If no settings service, allow all
	}

	// First check global Telegram notification setting
	telegramSettings, err := h.settingsSvc.GetTelegram(ctx)
	if err != nil {
		h.log.Warn("failed to get telegram settings for notify check",
			logger.F("error", err),
		)
		// On error, continue to check token settings
	} else if telegramSettings != nil && !telegramSettings.Enabled {
		h.log.Info("telegram notifications globally disabled",
			logger.F("stream", stream),
		)
		return false
	}

	tokenAddress := extractTokenAddress(stream)
	if tokenAddress == "" {
		h.log.Warn("could not extract token address from stream",
			logger.F("stream", stream),
		)
		return true // If can't extract token address, allow
	}

	streamType := extractStreamType(stream)

	// Get all tokens and check if this token has notify enabled
	tokens, err := h.settingsSvc.GetTokens(ctx)
	if err != nil {
		h.log.Warn("failed to get tokens for notify check",
			logger.F("error", err),
		)
		return true // On error, allow notification
	}

	h.log.Info("checking notify enabled",
		logger.F("stream", stream),
		logger.F("extracted_address", tokenAddress),
		logger.F("extracted_stream_type", streamType),
		logger.F("tokens_count", len(tokens)),
	)

	for _, token := range tokens {
		h.log.Debug("comparing token",
			logger.F("token_address", token.Address),
			logger.F("extracted_address", tokenAddress),
			logger.F("notify_enabled", token.NotifyEnabled),
		)
		if strings.EqualFold(token.Address, tokenAddress) {
			// Check per-stream notification setting if stream type is known
			if streamType != "" {
				streamNotifyEnabled := token.IsStreamNotifyEnabled(settings.StreamType(streamType))
				h.log.Info("found matching token with stream check",
					logger.F("token_address", tokenAddress),
					logger.F("stream_type", streamType),
					logger.F("global_notify_enabled", token.NotifyEnabled),
					logger.F("stream_notify_enabled", streamNotifyEnabled),
				)
				return streamNotifyEnabled
			}

			// Fallback to global token notify setting
			h.log.Info("found matching token (no stream type)",
				logger.F("token_address", tokenAddress),
				logger.F("notify_enabled", token.NotifyEnabled),
			)
			return token.NotifyEnabled
		}
	}

	h.log.Warn("token not found in settings, allowing notification",
		logger.F("token_address", tokenAddress),
	)
	// Token not found in settings, allow notification
	return true
}

// extractTokenAddress extracts token address from stream name
// Supports formats:
// - w3w@<token_address>@<chain_type>@<stream_type>
// - kl@<chain_id>@<token_address>@<interval>
// - tx@<chain_id>_<token_address>
func extractTokenAddress(stream string) string {
	parts := strings.Split(stream, "@")
	if len(parts) < 2 {
		return ""
	}

	prefix := parts[0]
	switch prefix {
	case "w3w":
		// w3w@<token_address>@<chain_type>@<stream_type>
		if len(parts) >= 2 {
			return parts[1]
		}
	case "kl":
		// kl@<chain_id>@<token_address>@<interval>
		if len(parts) >= 3 {
			return parts[2]
		}
	case "tx":
		// tx@<chain_id>_<token_address>
		if len(parts) >= 2 {
			// Split by underscore to get token address
			txParts := strings.SplitN(parts[1], "_", 2)
			if len(txParts) >= 2 {
				return txParts[1]
			}
		}
	}

	return ""
}

// extractStreamType extracts stream type from stream name
// Supports formats:
// - w3w@<token_address>@<chain_type>@<stream_type> -> stream_type
// - kl@<chain_id>@<token_address>@<interval> -> kline
// - tx@<chain_id>_<token_address> -> tx
func extractStreamType(stream string) string {
	parts := strings.Split(stream, "@")
	if len(parts) < 2 {
		return ""
	}

	prefix := parts[0]
	switch prefix {
	case "w3w":
		// w3w@<token_address>@<chain_type>@<stream_type>
		if len(parts) >= 4 {
			return parts[3] // e.g., "ticker24h", "holders"
		}
	case "kl":
		// kl@<chain_id>@<token_address>@<interval> -> kline
		return "kline"
	case "tx":
		// tx@<chain_id>_<token_address> -> tx
		return "tx"
	}

	return ""
}

// handleTicker24h processes ticker 24h events from W3W stream
func (h *EventHandler) handleTicker24h(ctx context.Context, event *models.Event) (string, error) {
	data, err := event.ParseTicker24hData()
	if err != nil {
		return "", fmt.Errorf("failed to parse ticker24h data: %w", err)
	}

	// Extract token address without chain type suffix (e.g., "@CT_501")
	tokenAddress := data.ContractAddress
	if idx := strings.Index(tokenAddress, "@"); idx != -1 {
		tokenAddress = tokenAddress[:idx]
	}

	// Parse price and change values
	price, _ := strconv.ParseFloat(data.Price, 64)
	priceChange24h, _ := strconv.ParseFloat(data.PriceChange24h, 64)
	volume24h, _ := strconv.ParseFloat(data.Volume24h, 64)
	volume24hBuy, _ := strconv.ParseFloat(data.VolumeBuy24h, 64)
	volume24hSell, _ := strconv.ParseFloat(data.VolumeSell24h, 64)
	marketCap, _ := strconv.ParseFloat(data.MarketCap, 64)
	liquidity, _ := strconv.ParseFloat(data.Liquidity, 64)

	// Cache ticker data for use in transaction alerts
	if h.tickerCache != nil {
		cacheData := &redis.TickerCacheData{
			TokenAddress:   tokenAddress,
			Price:          price,
			Volume24hBuy:   volume24hBuy,
			Volume24hSell:  volume24hSell,
			Volume24h:      volume24h,
			MarketCap:      marketCap,
			Liquidity:      liquidity,
			PriceChange24h: priceChange24h,
		}

		if err := h.tickerCache.Set(ctx, cacheData); err != nil {
			h.log.Warn("failed to cache ticker data",
				logger.F("error", err),
				logger.F("token", tokenAddress),
			)
		} else {
			h.log.Debug("ticker data cached successfully",
				logger.F("token", tokenAddress),
				logger.F("price", price),
				logger.F("market_cap", marketCap),
			)
		}
	} else {
		h.log.Debug("ticker cache is nil, skipping cache")
	}

	return h.formatter.FormatTicker24h(
		tokenAddress,
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
func (h *EventHandler) handleTransaction(ctx context.Context, event *models.Event) (string, error) {
	// Log raw data for debugging
	h.log.Debug("parsing transaction data",
		logger.F("raw_data", string(event.Data)),
		logger.F("stream", event.Stream),
	)

	// Try to parse as W3W transaction format first
	w3wData, err := event.ParseW3WTransactionData()
	if err == nil && w3wData.D.TxHash != "" {
		// Successfully parsed as W3W format
		h.log.Debug("W3W transaction data parsed",
			logger.F("tx_hash", w3wData.D.TxHash),
			logger.F("type", w3wData.D.TxType),
			logger.F("token0", w3wData.D.Token0Symbol),
			logger.F("token1", w3wData.D.Token1Symbol),
			logger.F("amount0", w3wData.D.Amount0),
			logger.F("amount1", w3wData.D.Amount1),
			logger.F("value_usd", w3wData.D.ValueUSD),
		)

		// Try to get cached ticker data for token0 (the main token)
		var tickerData telegram.TickerData
		if h.tickerCache != nil {
			h.log.Debug("looking up ticker cache",
				logger.F("token0_address", w3wData.D.Token0Address),
			)
			cachedData, err := h.tickerCache.GetValid(ctx, w3wData.D.Token0Address)
			if err != nil {
				h.log.Warn("failed to get cached ticker data",
					logger.F("error", err),
					logger.F("token", w3wData.D.Token0Address),
				)
			} else if cachedData != nil {
				tickerData = cachedData
				h.log.Debug("using cached ticker data for transaction",
					logger.F("token", w3wData.D.Token0Address),
					logger.F("price", cachedData.Price),
					logger.F("market_cap", cachedData.MarketCap),
					logger.F("volume_24h", cachedData.Volume24h),
					logger.F("age_seconds", cachedData.Age().Seconds()),
				)
			} else {
				h.log.Debug("no cached ticker data found",
					logger.F("token", w3wData.D.Token0Address),
				)
			}
		} else {
			h.log.Debug("ticker cache is nil, skipping ticker lookup")
		}

		return h.formatter.FormatW3WTransactionWithTicker(
			w3wData.D.TxHash,
			w3wData.D.TxType,
			w3wData.D.Token0Address,
			w3wData.D.Token1Address,
			w3wData.D.Token0Symbol,
			w3wData.D.Token1Symbol,
			w3wData.D.Amount0,
			w3wData.D.Amount1,
			w3wData.D.ValueUSD,
			w3wData.D.Token0PriceUSD,
			w3wData.D.Token1PriceUSD,
			w3wData.D.PlatformID,
			w3wData.D.MakerAddress,
			tickerData,
		), nil
	}

	// Fallback to legacy format
	data, err := event.ParseTransactionData()
	if err != nil {
		h.log.Error("failed to parse transaction data",
			logger.F("error", err),
			logger.F("raw_data", string(event.Data)),
		)
		return "", err
	}

	// Log parsed values for debugging
	h.log.Debug("legacy transaction data parsed",
		logger.F("hash", data.Hash),
		logger.F("from", data.From),
		logger.F("to", data.To),
		logger.F("value", data.Value),
		logger.F("token_symbol", data.TokenSymbol),
		logger.F("amount", data.Amount),
	)

	// Warn if critical fields are empty
	if data.Hash == "" && data.From == "" && data.To == "" {
		h.log.Warn("transaction data has empty critical fields - possible JSON field name mismatch",
			logger.F("raw_data", string(event.Data)),
		)
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
