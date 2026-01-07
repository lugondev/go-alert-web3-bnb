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

	// Transaction grouper for multi-hop swaps
	txGrouper *TransactionGrouper

	// Multi-hop swap notifications channel
	multiHopQueue chan *models.MultiHopSwap

	// Configuration
	rateLimitConfig redis.RateLimitConfig
	enableDedup     bool
	enableMultiHop  bool

	// Debug stream types lookup map for fast check
	debugStreamSet map[string]bool
}

// EventFilter is a function that determines if an event should be processed
type EventFilter func(event *models.Event) bool

// Config holds event handler configuration
type Config struct {
	QueueSize       int
	WorkerCount     int
	EnableDedup     bool
	EnableMultiHop  bool          // Enable multi-hop swap grouping
	MultiHopWindow  time.Duration // Time window to group swaps (default 500ms)
	RateLimitKey    string
	RateLimitMax    int
	RateLimitWindow time.Duration
	DebugStreams    []string // Stream types to enable debug logging
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
	if cfg.MultiHopWindow == 0 {
		cfg.MultiHopWindow = 500 * time.Millisecond
	}

	// Build debug stream set for fast lookup
	debugStreamSet := make(map[string]bool, len(cfg.DebugStreams))
	for _, s := range cfg.DebugStreams {
		debugStreamSet[s] = true
	}

	h := &EventHandler{
		notifier:       notifier,
		formatter:      telegram.NewFormatter(),
		deduplicator:   deduplicator,
		rateLimiter:    rateLimiter,
		pubsub:         pubsub,
		tickerCache:    tickerCache,
		settingsSvc:    settingsSvc,
		log:            log.With(logger.F("component", "handler")),
		filters:        make([]EventFilter, 0),
		eventQueue:     make(chan *models.Event, cfg.QueueSize),
		workerDone:     make(chan struct{}),
		multiHopQueue:  make(chan *models.MultiHopSwap, cfg.QueueSize),
		enableDedup:    cfg.EnableDedup,
		enableMultiHop: cfg.EnableMultiHop,
		debugStreamSet: debugStreamSet,
		rateLimitConfig: redis.RateLimitConfig{
			Key:    cfg.RateLimitKey,
			Limit:  cfg.RateLimitMax,
			Window: cfg.RateLimitWindow,
		},
	}

	// Initialize transaction grouper if multi-hop is enabled
	if cfg.EnableMultiHop {
		h.txGrouper = NewTransactionGrouper(log, cfg.MultiHopWindow, func(multiHop *models.MultiHopSwap) {
			select {
			case h.multiHopQueue <- multiHop:
				h.log.Debug("multi-hop swap queued from timer callback",
					logger.F("tx_hash", multiHop.TxHash),
					logger.F("swap_count", len(multiHop.Swaps)),
				)
			default:
				h.log.Warn("multi-hop queue full, dropping swap from timer",
					logger.F("tx_hash", multiHop.TxHash),
				)
			}
		})
		h.log.Info("multi-hop swap grouping enabled",
			logger.F("group_window", cfg.MultiHopWindow.String()),
		)
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

// isStreamDebugEnabled checks if debug logging is enabled for a specific stream
func (h *EventHandler) isStreamDebugEnabled(stream string) bool {
	if len(h.debugStreamSet) == 0 {
		return false // No debug streams configured, disable all stream debug
	}

	streamType := extractStreamType(stream)
	return h.debugStreamSet[streamType]
}

// Start starts the event processing workers
func (h *EventHandler) Start(ctx context.Context, workerCount int) {
	var wg sync.WaitGroup

	// Start event processing workers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			h.worker(ctx, workerID)
		}(i)
	}

	// Start multi-hop notification worker if enabled
	if h.enableMultiHop {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.multiHopWorker(ctx)
		}()

		// Start cleanup goroutine for stale pending transactions
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.cleanupWorker(ctx)
		}()
	}

	go func() {
		wg.Wait()
		close(h.workerDone)
	}()
}

// Stop stops the event handler and waits for workers to finish with timeout
func (h *EventHandler) Stop() {
	close(h.eventQueue)

	if h.enableMultiHop {
		close(h.multiHopQueue)
	}

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
		if h.isStreamDebugEnabled(event.Stream) {
			h.log.Debug("event filtered out",
				logger.F("event_type", string(event.Type)),
				logger.F("stream", event.Stream),
			)
		}
		return
	}

	// Try to queue the event
	select {
	case h.eventQueue <- event:
		if h.isStreamDebugEnabled(event.Stream) {
			h.log.Debug("event queued",
				logger.F("event_type", string(event.Type)),
				logger.F("stream", event.Stream),
			)
		}
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
	debugEnabled := h.isStreamDebugEnabled(event.Stream)

	if debugEnabled {
		h.log.Debug("processing event",
			logger.F("event_type", string(event.Type)),
			logger.F("stream", event.Stream),
		)
	}

	// Use deduplication if enabled
	if h.enableDedup && h.deduplicator != nil {
		err := h.deduplicator.ProcessWithDedup(ctx, event, func() error {
			return h.doProcessEvent(ctx, event)
		})

		if err != nil {
			if errors.Is(err, redis.ErrEventAlreadyProcessed) {
				if debugEnabled {
					h.log.Debug("event already processed by another instance",
						logger.F("event_type", string(event.Type)),
						logger.F("stream", event.Stream),
					)
				}
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
		if h.isStreamDebugEnabled(event.Stream) {
			h.log.Debug("unknown event type, skipping",
				logger.F("event_type", string(event.Type)),
				logger.F("stream", event.Stream),
			)
		}
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

	return h.sendNotification(ctx, event, message)
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
	debugEnabled := h.isStreamDebugEnabled(event.Stream)

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
		} else if debugEnabled {
			h.log.Debug("ticker data cached successfully",
				logger.F("token", tokenAddress),
				logger.F("price", price),
				logger.F("market_cap", marketCap),
			)
		}
	} else if debugEnabled {
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
		if h.isStreamDebugEnabled(event.Stream) {
			h.log.Debug("failed to parse kline data", logger.F("raw_data", string(event.Data)), logger.F("error", err))
		}
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
	debugEnabled := h.isStreamDebugEnabled(event.Stream)

	// Log raw data for debugging
	if debugEnabled {
		h.log.Debug("parsing transaction data",
			logger.F("raw_data", string(event.Data)),
			logger.F("stream", event.Stream),
		)
	}

	// Try to parse as W3W transaction format first
	w3wData, err := event.ParseW3WTransactionData()
	if err == nil && w3wData.D.TxHash != "" {
		// Successfully parsed as W3W format
		if debugEnabled {
			h.log.Debug("W3W transaction data parsed",
				logger.F("tx_hash", w3wData.D.TxHash),
				logger.F("type", w3wData.D.TxType),
				logger.F("token0", w3wData.D.Token0Symbol),
				logger.F("token1", w3wData.D.Token1Symbol),
				logger.F("amount0", w3wData.D.Amount0),
				logger.F("amount1", w3wData.D.Amount1),
				logger.F("value_usd", w3wData.D.ValueUSD),
				logger.F("quote_index", w3wData.D.QuoteIndex),
			)
		}

		// Smart multi-hop grouping for token-specific subscriptions
		// We want to show the FULL context of how our subscribed token is used:
		// - If ORE is intermediate: "USDC → ORE → SOL" (routing token)
		// - If ORE is source: "ORE → SOL" (sell)
		// - If ORE is destination: "USDC → ORE" (buy)
		//
		// Enable grouping to detect multi-hop and show full context
		shouldGroupMultiHop := true

		if h.enableMultiHop && h.txGrouper != nil && shouldGroupMultiHop {
			multiHop := h.txGrouper.AddSwap(w3wData)

			// If multiHop is returned, it means grouping is complete
			if multiHop != nil {
				// Queue for notification
				select {
				case h.multiHopQueue <- multiHop:
					if debugEnabled {
						h.log.Debug("multi-hop swap queued for notification",
							logger.F("tx_hash", multiHop.TxHash),
							logger.F("swap_count", len(multiHop.Swaps)),
						)
					}
				default:
					h.log.Warn("multi-hop queue full, dropping swap",
						logger.F("tx_hash", multiHop.TxHash),
					)
				}
			}

			// Return empty string to skip individual swap notification
			// The grouped notification will be sent by multiHopWorker
			return "", nil
		}

		// Multi-hop disabled or not applicable, process as individual swap
		// Extract subscribed token from stream
		subscribedToken := extractTokenAddress(event.Stream)

		// Try to get cached ticker data for the subscribed token
		var tickerData telegram.TickerData
		if h.tickerCache != nil {
			// Determine which token to get ticker for
			tickerTokenAddr := w3wData.D.Token0Address
			if subscribedToken != "" {
				// Use subscribed token for ticker lookup
				tickerTokenAddr = subscribedToken
			}

			if debugEnabled {
				h.log.Debug("looking up ticker cache",
					logger.F("token_address", tickerTokenAddr),
					logger.F("subscribed_token", subscribedToken),
				)
			}

			cachedData, err := h.tickerCache.GetValid(ctx, tickerTokenAddr)
			if err != nil {
				h.log.Warn("failed to get cached ticker data",
					logger.F("error", err),
					logger.F("token", tickerTokenAddr),
				)
			} else if cachedData != nil {
				tickerData = cachedData
				if debugEnabled {
					h.log.Debug("using cached ticker data for transaction",
						logger.F("token", tickerTokenAddr),
						logger.F("price", cachedData.Price),
						logger.F("market_cap", cachedData.MarketCap),
						logger.F("volume_24h", cachedData.Volume24h),
						logger.F("age_seconds", cachedData.Age().Seconds()),
					)
				}
			} else if debugEnabled {
				h.log.Debug("no cached ticker data found",
					logger.F("token", tickerTokenAddr),
				)
			}
		} else if debugEnabled {
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
	if debugEnabled {
		h.log.Debug("legacy transaction data parsed",
			logger.F("hash", data.Hash),
			logger.F("from", data.From),
			logger.F("to", data.To),
			logger.F("value", data.Value),
			logger.F("token_symbol", data.TokenSymbol),
			logger.F("amount", data.Amount),
		)
	}

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

// multiHopWorker processes multi-hop swap notifications
func (h *EventHandler) multiHopWorker(ctx context.Context) {
	h.log.Debug("multi-hop worker started")

	for {
		select {
		case <-ctx.Done():
			h.log.Debug("multi-hop worker stopped due to context cancellation")
			return
		case multiHop, ok := <-h.multiHopQueue:
			if !ok {
				h.log.Debug("multi-hop worker stopped, queue closed")
				return
			}
			h.processMultiHopSwap(ctx, multiHop)
		}
	}
}

// cleanupWorker periodically cleans up stale pending transactions
func (h *EventHandler) cleanupWorker(ctx context.Context) {
	h.log.Debug("cleanup worker started")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			h.log.Debug("cleanup worker stopped due to context cancellation")
			return
		case <-ticker.C:
			if h.txGrouper != nil {
				removed := h.txGrouper.Cleanup(10 * time.Second)
				if removed > 0 {
					h.log.Info("cleaned up stale pending transactions",
						logger.F("removed_count", removed),
					)
				}
			}
		}
	}
}

// processMultiHopSwap processes a completed multi-hop swap
func (h *EventHandler) processMultiHopSwap(ctx context.Context, multiHop *models.MultiHopSwap) {
	debugEnabled := h.isStreamDebugEnabled("tx")

	if debugEnabled || multiHop.IsMultiHop() {
		h.log.Info("processing multi-hop swap",
			logger.F("tx_hash", multiHop.TxHash),
			logger.F("is_multi_hop", multiHop.IsMultiHop()),
			logger.F("swap_count", len(multiHop.Swaps)),
			logger.F("swap_path", getSwapPathString(multiHop.Swaps)),
			logger.F("total_value_usd", multiHop.TotalValueUSD),
		)
	}

	// Determine subscribed token by checking which token appears in all swaps
	// For token-specific streams like tx@16_oreoU2P8..., we want to highlight
	// the role of that token in the multi-hop swap

	// Extract subscribed token address from the stream (if available)
	// We need to reconstruct the stream to extract token address
	tokenAddress := multiHop.FirstSwap.D.Token0Address
	chainID := multiHop.PlatformID
	stream := fmt.Sprintf("tx@%d_%s", chainID, tokenAddress)
	subscribedTokenAddr := extractTokenAddress(stream)

	// Detect subscribed token info with role and amounts
	tokenInfo := h.detectSubscribedTokenInfo(multiHop, subscribedTokenAddr)

	if debugEnabled {
		h.log.Debug("subscribed token info detected",
			logger.F("address", tokenInfo.Address),
			logger.F("symbol", tokenInfo.Symbol),
			logger.F("role", tokenInfo.Role),
			logger.F("position", tokenInfo.Position),
			logger.F("amount_in", tokenInfo.AmountIn),
			logger.F("amount_out", tokenInfo.AmountOut),
		)
	}

	// Get ticker data for the subscribed token
	var tickerData telegram.TickerData
	if h.tickerCache != nil && tokenInfo.Address != "" {
		cachedData, err := h.tickerCache.GetValid(ctx, tokenInfo.Address)
		if err != nil {
			h.log.Warn("failed to get cached ticker data for multi-hop",
				logger.F("error", err),
				logger.F("token", tokenInfo.Address),
			)
		} else if cachedData != nil {
			tickerData = cachedData
			if debugEnabled {
				h.log.Debug("using cached ticker data for multi-hop swap",
					logger.F("token", tokenInfo.Address),
					logger.F("symbol", tokenInfo.Symbol),
					logger.F("price", cachedData.Price),
				)
			}
		}
	}

	// Format message with subscribed token context
	var message string
	if multiHop.IsMultiHop() {
		// Convert internal TokenInfo to telegram.TokenInfo
		telegramTokenInfo := &telegram.TokenInfo{
			Address:    tokenInfo.Address,
			Symbol:     tokenInfo.Symbol,
			Role:       tokenInfo.Role,
			Position:   tokenInfo.Position,
			AmountIn:   tokenInfo.AmountIn,
			AmountOut:  tokenInfo.AmountOut,
			PriceUSD:   tokenInfo.PriceUSD,
			FromToken:  tokenInfo.FromToken,
			ToToken:    tokenInfo.ToToken,
			BridgeFrom: tokenInfo.BridgeFrom,
			BridgeTo:   tokenInfo.BridgeTo,
		}
		message = h.formatter.FormatMultiHopSwapWithTokenInfo(multiHop, telegramTokenInfo, tickerData)
	} else {
		// Single swap, use regular format
		swap := multiHop.FirstSwap
		message = h.formatter.FormatW3WTransactionWithTicker(
			swap.D.TxHash,
			swap.D.TxType,
			swap.D.Token0Address,
			swap.D.Token1Address,
			swap.D.Token0Symbol,
			swap.D.Token1Symbol,
			swap.D.Amount0,
			swap.D.Amount1,
			swap.D.ValueUSD,
			swap.D.Token0PriceUSD,
			swap.D.Token1PriceUSD,
			swap.D.PlatformID,
			swap.D.MakerAddress,
			tickerData,
		)
	}

	if tokenInfo.Address != "" {
		tokenAddress = tokenInfo.Address
	}
	chainID = multiHop.PlatformID
	stream = fmt.Sprintf("tx@%d_%s", chainID, tokenAddress)

	fakeEvent := &models.Event{
		Type:   models.EventTypeTransaction,
		Stream: stream,
		Data:   nil,
	}

	if err := h.sendNotificationForMultiHop(ctx, fakeEvent, message, multiHop.TxHash, multiHop.FirstSwap.D.TxType, multiHop.TotalValueUSD); err != nil {
		h.log.Error("failed to send multi-hop notification",
			logger.F("error", err),
			logger.F("tx_hash", multiHop.TxHash),
		)
		return
	}

	h.log.Info("multi-hop notification sent",
		logger.F("tx_hash", multiHop.TxHash),
		logger.F("is_multi_hop", multiHop.IsMultiHop()),
		logger.F("swap_count", len(multiHop.Swaps)),
		logger.F("subscribed_token", tokenInfo.Symbol),
		logger.F("token_role", tokenInfo.Role),
	)
}

// SubscribedTokenInfo contains information about the subscribed token in a swap
type SubscribedTokenInfo struct {
	Address    string  // Token contract address
	Symbol     string  // Token symbol (e.g., "ORE")
	Role       string  // "source", "destination", "bridge"
	Position   int     // Position in swap path (0-based)
	AmountIn   float64 // Amount received (for bridge tokens)
	AmountOut  float64 // Amount sent (for bridge tokens)
	PriceUSD   float64 // Token price in USD
	FromToken  string  // Token swapped FROM (for destination role)
	ToToken    string  // Token swapped TO (for source role)
	BridgeFrom string  // Starting token (for bridge role)
	BridgeTo   string  // Ending token (for bridge role)
}

// detectSubscribedTokenInfo finds and analyzes the subscribed token in the swap
// Returns detailed information about the token's role and amounts
func (h *EventHandler) detectSubscribedTokenInfo(multiHop *models.MultiHopSwap, subscribedTokenAddr string) *SubscribedTokenInfo {
	debugEnabled := h.isStreamDebugEnabled("tx")

	if debugEnabled {
		h.log.Debug("detecting subscribed token info",
			logger.F("tx_hash", multiHop.TxHash),
			logger.F("subscribed_addr", subscribedTokenAddr),
			logger.F("swap_count", len(multiHop.Swaps)),
		)
	}

	// If no subscribed token provided, auto-detect routing token
	if subscribedTokenAddr == "" {
		return h.detectRoutingToken(multiHop)
	}

	// Normalize address for comparison (case-insensitive)
	subscribedTokenAddr = strings.ToLower(subscribedTokenAddr)

	// Search through all swaps to find the subscribed token
	for i, swap := range multiHop.Swaps {
		token0Addr := strings.ToLower(swap.D.Token0Address)
		token1Addr := strings.ToLower(swap.D.Token1Address)

		// Check if token0 (input) matches subscribed token
		if token0Addr == subscribedTokenAddr {
			info := &SubscribedTokenInfo{
				Address:   swap.D.Token0Address,
				Symbol:    swap.D.Token0Symbol,
				Position:  i,
				AmountOut: swap.D.Amount0,
				PriceUSD:  swap.D.Token0PriceUSD,
			}

			// Determine role based on position
			if i == 0 && len(multiHop.Swaps) == 1 {
				// Single swap: token0 is being sold
				info.Role = "source"
				info.ToToken = swap.D.Token1Symbol
			} else if i == 0 {
				// First swap in multi-hop: token0 is starting point
				info.Role = "source"
				info.ToToken = multiHop.LastSwap.D.Token1Symbol
			} else {
				// Middle or end position: token0 is being used as bridge or destination
				// If this token appears again as output in next swap, it's a bridge
				if i < len(multiHop.Swaps)-1 && strings.ToLower(multiHop.Swaps[i+1].D.Token0Address) == subscribedTokenAddr {
					info.Role = "bridge"
					info.BridgeFrom = multiHop.FirstSwap.D.Token0Symbol
					info.BridgeTo = multiHop.LastSwap.D.Token1Symbol
					// For bridge, also track amount in from previous swap
					if i > 0 {
						info.AmountIn = multiHop.Swaps[i-1].D.Amount1
					}
				} else {
					info.Role = "bridge"
					info.BridgeFrom = multiHop.FirstSwap.D.Token0Symbol
					info.BridgeTo = swap.D.Token1Symbol
				}
			}

			if debugEnabled {
				h.log.Debug("found subscribed token as input",
					logger.F("symbol", info.Symbol),
					logger.F("role", info.Role),
					logger.F("position", info.Position),
					logger.F("amount_out", info.AmountOut),
				)
			}

			return info
		}

		// Check if token1 (output) matches subscribed token
		if token1Addr == subscribedTokenAddr {
			info := &SubscribedTokenInfo{
				Address:  swap.D.Token1Address,
				Symbol:   swap.D.Token1Symbol,
				Position: i,
				AmountIn: swap.D.Amount1,
				PriceUSD: swap.D.Token1PriceUSD,
			}

			// Determine role based on position and future swaps
			if i == len(multiHop.Swaps)-1 && len(multiHop.Swaps) == 1 {
				// Single swap: token1 is being bought
				info.Role = "destination"
				info.FromToken = swap.D.Token0Symbol
			} else if i == len(multiHop.Swaps)-1 {
				// Last swap in multi-hop: token1 is final destination
				info.Role = "destination"
				info.FromToken = multiHop.FirstSwap.D.Token0Symbol
			} else {
				// Token1 in middle: likely a bridge token
				info.Role = "bridge"
				info.BridgeFrom = multiHop.FirstSwap.D.Token0Symbol
				info.BridgeTo = multiHop.LastSwap.D.Token1Symbol
				// For bridge, also track amount out in next swap
				if i < len(multiHop.Swaps)-1 {
					info.AmountOut = multiHop.Swaps[i+1].D.Amount0
				}
			}

			if debugEnabled {
				h.log.Debug("found subscribed token as output",
					logger.F("symbol", info.Symbol),
					logger.F("role", info.Role),
					logger.F("position", info.Position),
					logger.F("amount_in", info.AmountIn),
				)
			}

			return info
		}
	}

	// Token not found in swaps
	if debugEnabled {
		h.log.Warn("subscribed token not found in swaps",
			logger.F("subscribed_addr", subscribedTokenAddr),
			logger.F("tx_hash", multiHop.TxHash),
		)
	}

	// Fallback: return first token
	return &SubscribedTokenInfo{
		Address:  multiHop.FirstSwap.D.Token0Address,
		Symbol:   multiHop.FirstSwap.D.Token0Symbol,
		Role:     "unknown",
		Position: 0,
		PriceUSD: multiHop.FirstSwap.D.Token0PriceUSD,
	}
}

// detectRoutingToken finds the routing/bridge token in a multi-hop swap
// (token that appears as output of one swap and input of next)
func (h *EventHandler) detectRoutingToken(multiHop *models.MultiHopSwap) *SubscribedTokenInfo {
	if !multiHop.IsMultiHop() {
		return &SubscribedTokenInfo{
			Address:  multiHop.FirstSwap.D.Token0Address,
			Symbol:   multiHop.FirstSwap.D.Token0Symbol,
			Role:     "source",
			Position: 0,
			ToToken:  multiHop.FirstSwap.D.Token1Symbol,
			PriceUSD: multiHop.FirstSwap.D.Token0PriceUSD,
		}
	}

	for i := 0; i < len(multiHop.Swaps)-1; i++ {
		currentSwap := multiHop.Swaps[i]
		nextSwap := multiHop.Swaps[i+1]

		if strings.EqualFold(currentSwap.D.Token1Address, nextSwap.D.Token0Address) {
			return &SubscribedTokenInfo{
				Address:    currentSwap.D.Token1Address,
				Symbol:     currentSwap.D.Token1Symbol,
				Role:       "bridge",
				Position:   i,
				AmountIn:   currentSwap.D.Amount1,
				AmountOut:  nextSwap.D.Amount0,
				PriceUSD:   currentSwap.D.Token1PriceUSD,
				BridgeFrom: multiHop.FirstSwap.D.Token0Symbol,
				BridgeTo:   multiHop.LastSwap.D.Token1Symbol,
			}
		}
	}

	return &SubscribedTokenInfo{
		Address:  multiHop.FirstSwap.D.Token0Address,
		Symbol:   multiHop.FirstSwap.D.Token0Symbol,
		Role:     "source",
		Position: 0,
		ToToken:  multiHop.LastSwap.D.Token1Symbol,
		PriceUSD: multiHop.FirstSwap.D.Token0PriceUSD,
	}
}

func (h *EventHandler) sendNotification(ctx context.Context, event *models.Event, message string) error {
	tokenAddress := extractTokenAddress(event.Stream)
	streamType := extractStreamType(event.Stream)

	if tokenAddress == "" || streamType == "" {
		if h.notifier == nil || !h.notifier.IsEnabled() {
			if h.isStreamDebugEnabled(event.Stream) {
				h.log.Debug("telegram notification skipped: notifier not enabled and no stream info",
					logger.F("event_type", string(event.Type)),
				)
			}
			return nil
		}
		h.log.Warn("could not extract token/stream info, using global config",
			logger.F("stream", event.Stream),
		)
		return h.sendWithGlobalConfig(ctx, event, message)
	}

	tokens, err := h.settingsSvc.GetTokens(ctx)
	if err != nil {
		if h.notifier == nil || !h.notifier.IsEnabled() {
			h.log.Warn("failed to get tokens and global notifier disabled",
				logger.F("error", err),
			)
			return nil
		}
		h.log.Warn("failed to get tokens, using global config",
			logger.F("error", err),
		)
		return h.sendWithGlobalConfig(ctx, event, message)
	}

	for _, token := range tokens {
		if !strings.EqualFold(token.Address, tokenAddress) {
			continue
		}

		if !token.IsStreamNotifyEnabled(settings.StreamType(streamType)) {
			h.log.Info("stream notification disabled",
				logger.F("token", tokenAddress),
				logger.F("stream", streamType),
				logger.F("token_notify_enabled", token.NotifyEnabled),
			)
			return nil
		}

		streamConfig, hasStreamConfig := token.GetStreamConfig(settings.StreamType(streamType))

		if streamType == "tx" && hasStreamConfig {
			txType := ""
			valueUSD := 0.0

			w3wData, err := event.ParseW3WTransactionData()
			if err == nil {
				txType = w3wData.D.TxType
				valueUSD = w3wData.D.ValueUSD
			} else {
				h.log.Warn("failed to parse W3W transaction data for filter check",
					logger.F("error", err),
					logger.F("stream", event.Stream),
				)
			}

			if !streamConfig.ShouldNotifyTx(txType, valueUSD) {
				h.log.Info("tx filtered by stream config",
					logger.F("tx_type", txType),
					logger.F("value_usd", valueUSD),
					logger.F("min_value", streamConfig.TxMinValueUSD),
					logger.F("filter_type", streamConfig.TxFilterType),
				)
				return nil
			}
		}

		telegramSettings, _ := h.settingsSvc.GetTelegram(ctx)
		defaultBot := settings.TelegramSettings{}
		if telegramSettings != nil {
			defaultBot = *telegramSettings
		}

		var bots []settings.TelegramBot
		if hasStreamConfig {
			bots = streamConfig.GetTelegramBots(defaultBot)
		} else if defaultBot.BotToken != "" && defaultBot.ChatID != "" {
			bots = []settings.TelegramBot{{
				BotToken: defaultBot.BotToken,
				ChatID:   defaultBot.ChatID,
				Enabled:  defaultBot.Enabled,
				Name:     "default",
			}}
		}

		if len(bots) == 0 {
			h.log.Warn("no telegram bots configured for stream",
				logger.F("token", tokenAddress),
				logger.F("stream", streamType),
				logger.F("has_stream_config", hasStreamConfig),
			)
			return nil
		}

		rateLimitKey := fmt.Sprintf("stream:%s:%s", tokenAddress, streamType)
		rateLimitConfig := redis.RateLimitConfig{
			Key:    rateLimitKey,
			Limit:  h.rateLimitConfig.Limit,
			Window: h.rateLimitConfig.Window,
		}
		if hasStreamConfig {
			rateLimitConfig.Limit = streamConfig.GetRateLimit(h.rateLimitConfig.Limit)
			rateLimitConfig.Window = streamConfig.GetRateLimitWindow(h.rateLimitConfig.Window)
		}

		if h.rateLimiter != nil {
			allowed, err := h.rateLimiter.Allow(ctx, rateLimitConfig)
			if err != nil {
				h.log.Error("rate limit check failed",
					logger.F("error", err),
				)
			} else if !allowed {
				h.log.Warn("stream rate limit exceeded",
					logger.F("stream", event.Stream),
				)
				return nil
			}
		}

		return h.sendToMultipleBots(ctx, bots, message, event.Type)
	}

	h.log.Warn("token not found, using global config",
		logger.F("token", tokenAddress),
	)
	return h.sendWithGlobalConfig(ctx, event, message)
}

func (h *EventHandler) sendWithGlobalConfig(ctx context.Context, event *models.Event, message string) error {
	if h.rateLimiter != nil {
		allowed, err := h.rateLimiter.Allow(ctx, h.rateLimitConfig)
		if err != nil {
			h.log.Error("rate limit check failed",
				logger.F("error", err),
			)
		} else if !allowed {
			h.log.Warn("global rate limit exceeded",
				logger.F("event_type", string(event.Type)),
			)
			return nil
		}
	}

	if err := h.notifier.SendMarkdown(ctx, message); err != nil {
		h.log.Error("failed to send telegram notification",
			logger.F("error", err),
			logger.F("event_type", string(event.Type)),
		)
		return err
	}

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

func (h *EventHandler) sendToMultipleBots(ctx context.Context, bots []settings.TelegramBot, message string, eventType models.EventType) error {
	if len(bots) == 0 {
		return fmt.Errorf("no bots configured")
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(bots))

	for i, bot := range bots {
		if !bot.Enabled {
			continue
		}

		wg.Add(1)
		go func(idx int, b settings.TelegramBot) {
			defer wg.Done()

			notifier := telegram.NewNotifier(telegram.Config{
				BotToken:   b.BotToken,
				ChatID:     b.ChatID,
				RateLimit:  30,
				Timeout:    30 * time.Second,
				RetryCount: 3,
			}, h.log)

			if err := notifier.SendMarkdown(ctx, message); err != nil {
				h.log.Error("failed to send to bot",
					logger.F("bot_index", idx),
					logger.F("bot_name", b.Name),
					logger.F("error", err),
				)
				errChan <- err
			}
		}(i, bot)
	}

	wg.Wait()
	close(errChan)

	var firstErr error
	sentCount := 0
	for err := range errChan {
		if firstErr == nil {
			firstErr = err
		}
	}

	sentCount = len(bots) - len(errChan)

	h.log.Info("notifications sent to multiple bots",
		logger.F("event_type", string(eventType)),
		logger.F("total_bots", len(bots)),
		logger.F("sent_count", sentCount),
		logger.F("failed_count", len(errChan)),
	)

	return firstErr
}

func (h *EventHandler) sendNotificationForMultiHop(ctx context.Context, event *models.Event, message, txHash, txType string, valueUSD float64) error {
	tokenAddress := extractTokenAddress(event.Stream)
	streamType := "tx"

	if tokenAddress == "" {
		if h.notifier == nil || !h.notifier.IsEnabled() {
			if h.isStreamDebugEnabled(event.Stream) {
				h.log.Debug("telegram notification skipped: notifier not enabled and no token address",
					logger.F("tx_hash", txHash),
				)
			}
			return nil
		}
		h.log.Warn("could not extract token address, using global config",
			logger.F("stream", event.Stream),
		)
		return h.sendWithGlobalConfigForMultiHop(ctx, message, txHash)
	}

	tokens, err := h.settingsSvc.GetTokens(ctx)
	if err != nil {
		if h.notifier == nil || !h.notifier.IsEnabled() {
			h.log.Warn("failed to get tokens and global notifier disabled",
				logger.F("error", err),
			)
			return nil
		}
		h.log.Warn("failed to get tokens, using global config",
			logger.F("error", err),
		)
		return h.sendWithGlobalConfigForMultiHop(ctx, message, txHash)
	}

	for _, token := range tokens {
		if !strings.EqualFold(token.Address, tokenAddress) {
			continue
		}

		if !token.IsStreamNotifyEnabled(settings.StreamType(streamType)) {
			h.log.Info("stream notification disabled for multi-hop",
				logger.F("token", tokenAddress),
				logger.F("tx_hash", txHash),
				logger.F("token_notify_enabled", token.NotifyEnabled),
			)
			return nil
		}

		streamConfig, hasStreamConfig := token.GetStreamConfig(settings.StreamType(streamType))

		if hasStreamConfig && !streamConfig.ShouldNotifyTx(txType, valueUSD) {
			h.log.Info("multi-hop tx filtered by stream config",
				logger.F("tx_hash", txHash),
				logger.F("tx_type", txType),
				logger.F("value_usd", valueUSD),
				logger.F("min_value", streamConfig.TxMinValueUSD),
				logger.F("filter_type", streamConfig.TxFilterType),
			)
			return nil
		}

		telegramSettings, _ := h.settingsSvc.GetTelegram(ctx)
		defaultBot := settings.TelegramSettings{}
		if telegramSettings != nil {
			defaultBot = *telegramSettings
		}

		var bots []settings.TelegramBot
		if hasStreamConfig {
			bots = streamConfig.GetTelegramBots(defaultBot)
		} else if defaultBot.BotToken != "" && defaultBot.ChatID != "" {
			bots = []settings.TelegramBot{{
				BotToken: defaultBot.BotToken,
				ChatID:   defaultBot.ChatID,
				Enabled:  defaultBot.Enabled,
				Name:     "default",
			}}
		}

		if len(bots) == 0 {
			h.log.Warn("no telegram bots configured for multi-hop",
				logger.F("token", tokenAddress),
				logger.F("tx_hash", txHash),
				logger.F("has_stream_config", hasStreamConfig),
			)
			return nil
		}

		rateLimitKey := fmt.Sprintf("stream:%s:%s", tokenAddress, streamType)
		rateLimitConfig := redis.RateLimitConfig{
			Key:    rateLimitKey,
			Limit:  h.rateLimitConfig.Limit,
			Window: h.rateLimitConfig.Window,
		}
		if hasStreamConfig {
			rateLimitConfig.Limit = streamConfig.GetRateLimit(h.rateLimitConfig.Limit)
			rateLimitConfig.Window = streamConfig.GetRateLimitWindow(h.rateLimitConfig.Window)
		}

		if h.rateLimiter != nil {
			allowed, err := h.rateLimiter.Allow(ctx, rateLimitConfig)
			if err != nil {
				h.log.Error("rate limit check failed",
					logger.F("error", err),
				)
			} else if !allowed {
				h.log.Warn("stream rate limit exceeded for multi-hop",
					logger.F("tx_hash", txHash),
				)
				return nil
			}
		}

		return h.sendToMultipleBots(ctx, bots, message, models.EventTypeTransaction)
	}

	h.log.Warn("token not found for multi-hop, using global config",
		logger.F("token", tokenAddress),
		logger.F("tx_hash", txHash),
	)
	return h.sendWithGlobalConfigForMultiHop(ctx, message, txHash)
}

func (h *EventHandler) sendWithGlobalConfigForMultiHop(ctx context.Context, message, txHash string) error {
	if h.rateLimiter != nil {
		allowed, err := h.rateLimiter.Allow(ctx, h.rateLimitConfig)
		if err != nil {
			h.log.Error("rate limit check failed",
				logger.F("error", err),
			)
		} else if !allowed {
			h.log.Warn("global rate limit exceeded for multi-hop",
				logger.F("tx_hash", txHash),
			)
			return nil
		}
	}

	if err := h.notifier.SendMarkdown(ctx, message); err != nil {
		h.log.Error("failed to send multi-hop notification",
			logger.F("error", err),
			logger.F("tx_hash", txHash),
		)
		return err
	}

	h.log.Info("multi-hop notification sent",
		logger.F("tx_hash", txHash),
	)

	return nil
}
