package handlers

import (
	"github.com/gofiber/fiber/v2"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
)

// SettingsHandler handles HTTP requests for settings
type SettingsHandler struct {
	service *settings.Service
	log     logger.Logger
}

// NewSettingsHandler creates a new settings handler
func NewSettingsHandler(service *settings.Service, log logger.Logger) *SettingsHandler {
	return &SettingsHandler{
		service: service,
		log:     log.With(logger.F("component", "settings-handler")),
	}
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

// TestTelegram sends a test message to Telegram
func (h *SettingsHandler) TestTelegram(c *fiber.Ctx) error {
	// TODO: Implement test message sending
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Test message sent",
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

	if err := h.service.DeleteToken(ctx, tokenID); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
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

	if err := h.service.ToggleTokenNotify(ctx, tokenID, body.Enabled); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
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
