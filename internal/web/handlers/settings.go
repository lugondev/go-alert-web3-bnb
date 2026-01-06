package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
	"github.com/lugondev/go-alert-web3-bnb/internal/telegram"
)

// SubscriptionSyncer is an interface for syncing subscriptions
type SubscriptionSyncer interface {
	SyncSubscriptions(ctx context.Context) error
	UnsubscribeStream(ctx context.Context, tokenAddress string, chainType settings.ChainType, streamType settings.StreamType) error
	SubscribeStream(ctx context.Context, tokenAddress string, chainType settings.ChainType, streamType settings.StreamType) error
	UnsubscribeToken(ctx context.Context, tokenAddress string, chainType settings.ChainType) error
}

// SettingsHandler handles HTTP requests for settings
type SettingsHandler struct {
	service   *settings.Service
	log       logger.Logger
	subSyncer SubscriptionSyncer
}

// NewSettingsHandler creates a new settings handler
func NewSettingsHandler(service *settings.Service, log logger.Logger) *SettingsHandler {
	return &SettingsHandler{
		service:   service,
		log:       log.With(logger.F("component", "settings-handler")),
		subSyncer: nil, // Will be set later via SetSubscriptionSyncer
	}
}

// SetSubscriptionSyncer sets the subscription syncer for managing WebSocket subscriptions
func (h *SettingsHandler) SetSubscriptionSyncer(syncer SubscriptionSyncer) {
	h.subSyncer = syncer
	h.log.Info("subscription syncer set")
}

// RegisterRoutes registers all settings routes
func (h *SettingsHandler) RegisterRoutes(app *fiber.App) {
	// Pages
	app.Get("/", h.IndexPage)
	app.Get("/tokens", h.TokensPage)
	app.Get("/tokens/new", h.NewTokenPage)
	app.Get("/tokens/:id", h.EditTokenPage)
	app.Get("/telegram", h.TelegramPage)

	// API endpoints
	api := app.Group("/api")

	// Settings
	api.Get("/settings", h.GetSettings)

	// Telegram
	api.Get("/telegram", h.GetTelegram)
	api.Post("/telegram", h.SaveTelegram)
	api.Post("/telegram/test", h.TestTelegram)

	// Tokens
	api.Get("/tokens", h.GetTokens)
	api.Post("/tokens", h.CreateToken)
	api.Get("/tokens/:id", h.GetToken)
	api.Put("/tokens/:id", h.UpdateToken)
	api.Delete("/tokens/:id", h.DeleteToken)
	api.Post("/tokens/:id/notify", h.ToggleTokenNotify)
	api.Put("/tokens/:id/streams", h.UpdateTokenStreams)
	api.Post("/tokens/:id/streams/:stream/notify", h.ToggleStreamNotify)
	api.Get("/tokens/:id/streams/:stream/config", h.GetStreamConfig)
	api.Put("/tokens/:id/streams/:stream/config", h.UpdateStreamConfig)

	// Subscriptions
	api.Get("/subscriptions", h.GetSubscriptions)
}

// IndexPage renders the main settings page
func (h *SettingsHandler) IndexPage(c *fiber.Ctx) error {
	ctx := c.Context()

	telegramSettings, err := h.service.GetTelegram(ctx)
	if err != nil {
		h.log.Error("failed to get telegram settings", logger.F("error", err))
		telegramSettings = &settings.TelegramSettings{}
	}

	tokens, err := h.service.GetTokens(ctx)
	if err != nil {
		h.log.Error("failed to get tokens", logger.F("error", err))
		tokens = []settings.TokenSettings{}
	}

	// Convert to list items
	tokenList := make([]settings.TokenListItem, len(tokens))
	for i, t := range tokens {
		tokenList[i] = t.ToListItem()
	}

	return c.Render("index", fiber.Map{
		"Title":    "Web3 Alert Settings",
		"Telegram": telegramSettings,
		"Tokens":   tokenList,
	})
}

// TokensPage renders the tokens management page
func (h *SettingsHandler) TokensPage(c *fiber.Ctx) error {
	ctx := c.Context()

	tokens, err := h.service.GetTokens(ctx)
	if err != nil {
		h.log.Error("failed to get tokens", logger.F("error", err))
		tokens = []settings.TokenSettings{}
	}

	// Convert to list items
	tokenList := make([]settings.TokenListItem, len(tokens))
	for i, t := range tokens {
		tokenList[i] = t.ToListItem()
	}

	return c.Render("tokens", fiber.Map{
		"Title":  "Token Management",
		"Tokens": tokenList,
	})
}

// NewTokenPage renders the new token form
func (h *SettingsHandler) NewTokenPage(c *fiber.Ctx) error {
	// Use TokenListItem with string types for template compatibility
	emptyToken := settings.TokenListItem{
		ChainType: string(settings.ChainSolana), // Default chain
	}

	return c.Render("token_form", fiber.Map{
		"Title":           "Add New Token",
		"Token":           emptyToken,
		"ChainTypes":      getChainTypeOptions(),
		"StreamTypes":     getStreamTypeOptions(),
		"StreamMap":       make(map[string]bool),
		"StreamNotifyMap": make(map[string]bool),
		"IsNew":           true,
	})
}

// EditTokenPage renders the edit token form
func (h *SettingsHandler) EditTokenPage(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Error("failed to get token", logger.F("error", err), logger.F("id", tokenID))
		return c.Redirect("/tokens")
	}

	// Convert streams to map for template
	streamMap := make(map[string]bool)
	for _, s := range token.Streams {
		streamMap[string(s)] = true
	}

	// Convert stream notify to map for template
	streamNotifyMap := make(map[string]bool)
	for _, s := range token.Streams {
		streamNotifyMap[string(s)] = token.IsStreamNotifyEnabled(s)
	}

	// Convert to list item for template (ChainType as string)
	tokenData := token.ToListItem()

	return c.Render("token_form", fiber.Map{
		"Title":           "Edit Token",
		"Token":           tokenData,
		"ChainTypes":      getChainTypeOptions(),
		"StreamTypes":     getStreamTypeOptions(),
		"StreamMap":       streamMap,
		"StreamNotifyMap": streamNotifyMap,
		"IsNew":           false,
	})
}

// TelegramPage renders the Telegram settings page
func (h *SettingsHandler) TelegramPage(c *fiber.Ctx) error {
	ctx := c.Context()

	telegram, err := h.service.GetTelegram(ctx)
	if err != nil {
		h.log.Error("failed to get telegram settings", logger.F("error", err))
		telegram = &settings.TelegramSettings{RateLimit: 30}
	}

	return c.Render("telegram", fiber.Map{
		"Title":    "Telegram Settings",
		"Telegram": telegram,
	})
}

// GetSettings returns all settings as JSON
func (h *SettingsHandler) GetSettings(c *fiber.Ctx) error {
	ctx := c.Context()

	allSettings, err := h.service.GetSettings(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(allSettings)
}

// GetTelegram returns Telegram settings
func (h *SettingsHandler) GetTelegram(c *fiber.Ctx) error {
	ctx := c.Context()

	telegram, err := h.service.GetTelegram(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(telegram)
}

// SaveTelegram saves Telegram settings
func (h *SettingsHandler) SaveTelegram(c *fiber.Ctx) error {
	ctx := c.Context()

	var form settings.TelegramFormData
	if err := c.BodyParser(&form); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid form data",
		})
	}

	telegram := form.ToTelegramSettings()
	if err := h.service.SaveTelegram(ctx, telegram); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// Check if this is an HTMX request
	if c.Get("HX-Request") == "true" {
		return c.Render("partials/alert", fiber.Map{
			"Type":    "success",
			"Message": "Telegram settings saved successfully",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Telegram settings saved",
	})
}

// TestTelegramRequest represents the request body for testing Telegram
type TestTelegramRequest struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
}

// TestTelegram sends a test message to Telegram
func (h *SettingsHandler) TestTelegram(c *fiber.Ctx) error {
	var req TestTelegramRequest

	// Try to parse request body for custom credentials
	if err := c.BodyParser(&req); err != nil {
		// If no body provided, use saved settings
		req = TestTelegramRequest{}
	}

	// If no credentials in request, get from saved settings
	if req.BotToken == "" || req.ChatID == "" {
		ctx := c.Context()
		telegramSettings, err := h.service.GetTelegram(ctx)
		if err != nil {
			h.log.Error("failed to get telegram settings for test", logger.F("error", err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to get Telegram settings",
			})
		}

		if req.BotToken == "" {
			req.BotToken = telegramSettings.BotToken
		}
		if req.ChatID == "" {
			req.ChatID = telegramSettings.ChatID
		}
	}

	// Validate credentials
	if req.BotToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Bot token is required",
		})
	}
	if req.ChatID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Chat ID is required",
		})
	}

	// Create a temporary notifier for testing
	testNotifier := telegram.NewNotifier(telegram.Config{
		BotToken:   req.BotToken,
		ChatID:     req.ChatID,
		RateLimit:  60, // Allow more messages for testing
		Timeout:    10 * time.Second,
		RetryCount: 1,
	}, h.log)

	if !testNotifier.IsEnabled() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid Telegram credentials",
		})
	}

	// Send test message
	formatter := telegram.NewFormatter()
	testMessage := formatter.FormatAlert("success", "Test Message", "This is a test message from Web3 Alert. Your Telegram configuration is working correctly!")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := testNotifier.SendMarkdown(ctx, testMessage); err != nil {
		h.log.Error("failed to send test telegram message", logger.F("error", err))
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Failed to send test message: " + err.Error(),
		})
	}

	h.log.Info("test telegram message sent successfully")

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Test message sent successfully",
	})
}

// GetTokens returns all tokens
func (h *SettingsHandler) GetTokens(c *fiber.Ctx) error {
	ctx := c.Context()

	tokens, err := h.service.GetTokens(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(tokens)
}

// GetToken returns a single token
func (h *SettingsHandler) GetToken(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(token)
}

// CreateToken creates a new token
func (h *SettingsHandler) CreateToken(c *fiber.Ctx) error {
	ctx := c.Context()

	var form settings.TokenFormData
	if err := c.BodyParser(&form); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid form data",
		})
	}

	token := form.ToTokenSettings()
	if err := h.service.AddToken(ctx, token); err != nil {
		h.log.Error("failed to create token", logger.F("error", err))

		// Check if this is an HTMX request
		if c.Get("HX-Request") == "true" {
			return c.Render("partials/alert", fiber.Map{
				"Type":    "error",
				"Message": err.Error(),
			})
		}

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	h.log.Info("token created",
		logger.F("token_address", token.Address),
		logger.F("chain_type", token.ChainType),
		logger.F("streams", token.Streams),
	)

	// Subscribe to token streams if syncer is available and notify is enabled
	if h.subSyncer != nil && token.NotifyEnabled {
		syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.subSyncer.SyncSubscriptions(syncCtx); err != nil {
			h.log.Warn("failed to sync subscriptions after token create",
				logger.F("error", err),
			)
		}
	}

	// Check if this is an HTMX request
	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/tokens")
		return c.SendStatus(fiber.StatusOK)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Token created",
	})
}

// UpdateToken updates a token
func (h *SettingsHandler) UpdateToken(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	var form settings.TokenFormData
	if err := c.BodyParser(&form); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid form data",
		})
	}

	form.ID = tokenID
	token := form.ToTokenSettings()

	if err := h.service.UpdateToken(ctx, token); err != nil {
		h.log.Error("failed to update token", logger.F("error", err))

		if c.Get("HX-Request") == "true" {
			return c.Render("partials/alert", fiber.Map{
				"Type":    "error",
				"Message": err.Error(),
			})
		}

		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	h.log.Info("token updated",
		logger.F("token_id", tokenID),
		logger.F("token_address", token.Address),
		logger.F("streams", token.Streams),
	)

	// Sync subscriptions after update (streams may have changed)
	if h.subSyncer != nil {
		syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.subSyncer.SyncSubscriptions(syncCtx); err != nil {
			h.log.Warn("failed to sync subscriptions after token update",
				logger.F("error", err),
			)
		}
	}

	if c.Get("HX-Request") == "true" {
		c.Set("HX-Redirect", "/tokens")
		return c.SendStatus(fiber.StatusOK)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Token updated",
	})
}

// DeleteToken deletes a token
func (h *SettingsHandler) DeleteToken(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	// Get token info before delete for subscription management
	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Warn("failed to get token before delete",
			logger.F("error", err),
			logger.F("token_id", tokenID),
		)
		// Continue with delete even if we can't get token info
	}

	if err := h.service.DeleteToken(ctx, tokenID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	h.log.Info("token deleted",
		logger.F("token_id", tokenID),
	)

	// Unsubscribe from all token streams if syncer is available
	if h.subSyncer != nil && token != nil {
		syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := h.subSyncer.UnsubscribeToken(syncCtx, token.Address, token.ChainType); err != nil {
			h.log.Warn("failed to unsubscribe token streams after delete",
				logger.F("error", err),
				logger.F("token", token.Address),
			)
		}
	}

	if c.Get("HX-Request") == "true" {
		return c.SendStatus(fiber.StatusOK)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Token deleted",
	})
}

// ToggleTokenNotify toggles notification for a token
func (h *SettingsHandler) ToggleTokenNotify(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	var body struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Get token info before toggle for subscription management
	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Error("failed to get token for notify toggle",
			logger.F("error", err),
			logger.F("token_id", tokenID),
		)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if err := h.service.ToggleTokenNotify(ctx, tokenID, body.Enabled); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	h.log.Info("token notify toggled",
		logger.F("token_id", tokenID),
		logger.F("token_address", token.Address),
		logger.F("enabled", body.Enabled),
	)

	// Sync WebSocket subscriptions if syncer is available
	if h.subSyncer != nil {
		syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if !body.Enabled {
			// When disabling token notify, unsubscribe all its streams
			if err := h.subSyncer.UnsubscribeToken(syncCtx, token.Address, token.ChainType); err != nil {
				h.log.Warn("failed to unsubscribe token streams after disable",
					logger.F("error", err),
					logger.F("token", token.Address),
				)
			}
		} else {
			// When enabling, sync to subscribe active streams
			if err := h.subSyncer.SyncSubscriptions(syncCtx); err != nil {
				h.log.Warn("failed to sync subscriptions after enable",
					logger.F("error", err),
				)
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"enabled": body.Enabled,
	})
}

// UpdateTokenStreams updates streams for a token
func (h *SettingsHandler) UpdateTokenStreams(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")

	var body struct {
		Streams []string `json:"streams"`
	}

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	streams := make([]settings.StreamType, len(body.Streams))
	for i, s := range body.Streams {
		streams[i] = settings.StreamType(s)
	}

	if err := h.service.UpdateTokenStreams(ctx, tokenID, streams); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"streams": body.Streams,
	})
}

// GetSubscriptions returns all active subscriptions
func (h *SettingsHandler) GetSubscriptions(c *fiber.Ctx) error {
	ctx := c.Context()

	subscriptions, err := h.service.GetActiveSubscriptions(ctx)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"subscriptions": subscriptions,
		"count":         len(subscriptions),
	})
}

// ChainTypeOption represents a chain type option for forms
type ChainTypeOption struct {
	Value string
	Label string
}

// StreamTypeOption represents a stream type option for forms
type StreamTypeOption struct {
	Value       string
	Label       string
	Description string
}

// getChainTypeOptions returns chain type options for forms
func getChainTypeOptions() []ChainTypeOption {
	return []ChainTypeOption{
		{Value: string(settings.ChainSolana), Label: "Solana"},
		{Value: string(settings.ChainBSC), Label: "BNB Smart Chain"},
		{Value: string(settings.ChainBase), Label: "Base"},
	}
}

// getStreamTypeOptions returns stream type options for forms
func getStreamTypeOptions() []StreamTypeOption {
	return []StreamTypeOption{
		{Value: string(settings.StreamTicker24h), Label: "24h Ticker", Description: "Price and volume data"},
		{Value: string(settings.StreamHolders), Label: "Holders", Description: "Token holder changes"},
		{Value: string(settings.StreamTx), Label: "Transactions", Description: "Real-time transactions"},
		{Value: string(settings.StreamKline), Label: "Kline", Description: "Candlestick data"},
	}
}

// ToggleStreamNotify toggles notification for a specific stream of a token
func (h *SettingsHandler) ToggleStreamNotify(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")
	streamType := c.Params("stream")

	var body struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	// Get token info before toggle for subscription management
	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Error("failed to get token for stream toggle",
			logger.F("error", err),
			logger.F("token_id", tokenID),
		)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	if err := h.service.ToggleStreamNotify(ctx, tokenID, settings.StreamType(streamType), body.Enabled); err != nil {
		h.log.Error("failed to toggle stream notify",
			logger.F("error", err),
			logger.F("token_id", tokenID),
			logger.F("stream", streamType),
		)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	h.log.Info("stream notify toggled",
		logger.F("token_id", tokenID),
		logger.F("stream", streamType),
		logger.F("enabled", body.Enabled),
	)

	// Sync WebSocket subscriptions if syncer is available
	if h.subSyncer != nil {
		syncCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if body.Enabled {
			// Subscribe to the stream
			if err := h.subSyncer.SubscribeStream(syncCtx, token.Address, token.ChainType, settings.StreamType(streamType)); err != nil {
				h.log.Warn("failed to subscribe stream after enable",
					logger.F("error", err),
					logger.F("token", token.Address),
					logger.F("stream", streamType),
				)
			}
		} else {
			// Unsubscribe from the stream
			if err := h.subSyncer.UnsubscribeStream(syncCtx, token.Address, token.ChainType, settings.StreamType(streamType)); err != nil {
				h.log.Warn("failed to unsubscribe stream after disable",
					logger.F("error", err),
					logger.F("token", token.Address),
					logger.F("stream", streamType),
				)
			}
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"enabled": body.Enabled,
		"stream":  streamType,
	})
}

// GetStreamNotifyStatus returns the notification status for a specific stream
func (h *SettingsHandler) GetStreamNotifyStatus(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")
	streamType := c.Params("stream")

	enabled, err := h.service.GetStreamNotifyStatus(ctx, tokenID, settings.StreamType(streamType))
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"enabled": enabled,
		"stream":  streamType,
	})
}

func (h *SettingsHandler) GetStreamConfig(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")
	streamType := c.Params("stream")

	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Error("failed to get token for stream config",
			logger.F("error", err),
			logger.F("token_id", tokenID),
		)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Token not found",
		})
	}

	streamTypeEnum := settings.StreamType(streamType)
	validStream := false
	for _, s := range token.Streams {
		if s == streamTypeEnum {
			validStream = true
			break
		}
	}
	if !validStream {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Stream not configured for this token",
		})
	}

	config, exists := token.GetStreamConfig(streamTypeEnum)
	if !exists {
		return c.JSON(fiber.Map{
			"enabled":           token.IsStreamNotifyEnabled(streamTypeEnum),
			"telegram_bots":     []settings.TelegramBot{},
			"tx_min_value_usd":  0,
			"tx_filter_type":    settings.TxFilterBoth,
			"rate_limit":        0,
			"rate_limit_window": "0s",
		})
	}

	return c.JSON(config)
}

func (h *SettingsHandler) UpdateStreamConfig(c *fiber.Ctx) error {
	ctx := c.Context()
	tokenID := c.Params("id")
	streamType := c.Params("stream")

	var formData StreamConfigFormData
	if err := c.BodyParser(&formData); err != nil {
		h.log.Error("failed to parse stream config form data",
			logger.F("error", err),
			logger.F("token_id", tokenID),
			logger.F("stream", streamType),
		)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid form data",
		})
	}

	if err := formData.Validate(settings.StreamType(streamType)); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	token, err := h.service.GetToken(ctx, tokenID)
	if err != nil {
		h.log.Error("failed to get token for stream config update",
			logger.F("error", err),
			logger.F("token_id", tokenID),
		)
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Token not found",
		})
	}

	streamTypeEnum := settings.StreamType(streamType)
	validStream := false
	for _, s := range token.Streams {
		if s == streamTypeEnum {
			validStream = true
			break
		}
	}
	if !validStream {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Stream not configured for this token",
		})
	}

	config := formData.ToStreamNotifyConfig()

	if token.StreamNotify == nil {
		token.StreamNotify = make(map[settings.StreamType]settings.StreamNotifyConfig)
	}
	token.StreamNotify[streamTypeEnum] = config

	if err := h.service.UpdateToken(ctx, *token); err != nil {
		h.log.Error("failed to update token stream config",
			logger.F("error", err),
			logger.F("token_id", tokenID),
			logger.F("stream", streamType),
		)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update stream configuration",
		})
	}

	h.log.Info("stream config updated",
		logger.F("token_id", tokenID),
		logger.F("stream", streamType),
		logger.F("enabled", config.Enabled),
	)

	if c.Get("HX-Request") == "true" {
		return c.Render("partials/alert", fiber.Map{
			"Type":    "success",
			"Message": "Stream configuration updated successfully",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Stream configuration updated",
		"config":  config,
	})
}

type StreamConfigFormData struct {
	Enabled         bool                  `json:"enabled" form:"enabled"`
	TelegramBots    []TelegramBotFormData `json:"telegram_bots" form:"telegram_bots"`
	TxMinValueUSD   float64               `json:"tx_min_value_usd" form:"tx_min_value_usd"`
	TxFilterType    string                `json:"tx_filter_type" form:"tx_filter_type"`
	RateLimit       int                   `json:"rate_limit" form:"rate_limit"`
	RateLimitWindow string                `json:"rate_limit_window" form:"rate_limit_window"`
}

type TelegramBotFormData struct {
	BotToken string `json:"bot_token" form:"bot_token"`
	ChatID   string `json:"chat_id" form:"chat_id"`
	Enabled  bool   `json:"enabled" form:"enabled"`
	Name     string `json:"name" form:"name"`
}

func (f *StreamConfigFormData) Validate(streamType settings.StreamType) error {
	for i, bot := range f.TelegramBots {
		if bot.BotToken == "" {
			return fmt.Errorf("telegram bot #%d: bot token is required", i+1)
		}
		if bot.ChatID == "" {
			return fmt.Errorf("telegram bot #%d: chat ID is required", i+1)
		}
	}

	if streamType == settings.StreamTx {
		if f.TxMinValueUSD < 0 {
			return fmt.Errorf("tx_min_value_usd must be >= 0")
		}
		if f.TxFilterType != "" &&
			f.TxFilterType != string(settings.TxFilterBoth) &&
			f.TxFilterType != string(settings.TxFilterBuy) &&
			f.TxFilterType != string(settings.TxFilterSell) {
			return fmt.Errorf("tx_filter_type must be one of: both, buy, sell")
		}
	}

	if f.RateLimit < 0 {
		return fmt.Errorf("rate_limit must be >= 0")
	}

	if f.RateLimitWindow != "" && f.RateLimitWindow != "0s" {
		if _, err := time.ParseDuration(f.RateLimitWindow); err != nil {
			return fmt.Errorf("invalid rate_limit_window format: %w", err)
		}
	}

	return nil
}

func (f *StreamConfigFormData) ToStreamNotifyConfig() settings.StreamNotifyConfig {
	config := settings.StreamNotifyConfig{
		Enabled:       f.Enabled,
		TxMinValueUSD: f.TxMinValueUSD,
		RateLimit:     f.RateLimit,
	}

	if len(f.TelegramBots) > 0 {
		config.TelegramBots = make([]settings.TelegramBot, len(f.TelegramBots))
		for i, bot := range f.TelegramBots {
			config.TelegramBots[i] = settings.TelegramBot{
				BotToken: bot.BotToken,
				ChatID:   bot.ChatID,
				Enabled:  bot.Enabled,
				Name:     bot.Name,
			}
		}
	}

	if f.TxFilterType != "" {
		config.TxFilterType = settings.TxFilterType(f.TxFilterType)
	}

	if f.RateLimitWindow != "" && f.RateLimitWindow != "0s" {
		duration, _ := time.ParseDuration(f.RateLimitWindow)
		config.RateLimitWindow = duration
	}

	return config
}
