package settings

import (
	"context"
	"fmt"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

// Service handles settings business logic
type Service struct {
	repo *Repository
	log  logger.Logger
}

// NewService creates a new settings service
func NewService(repo *Repository, log logger.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log.With(logger.F("component", "settings-service")),
	}
}

// Initialize initializes settings from config
func (s *Service) Initialize(ctx context.Context, telegram TelegramSettings, tokens []TokenSettings) error {
	return s.repo.Initialize(ctx, telegram, tokens)
}

// GetSettings retrieves all settings
func (s *Service) GetSettings(ctx context.Context) (*AppSettings, error) {
	return s.repo.GetSettings(ctx)
}

// SaveSettings saves all settings
func (s *Service) SaveSettings(ctx context.Context, settings *AppSettings) error {
	return s.repo.SaveSettings(ctx, settings)
}

// GetTelegram retrieves Telegram settings
func (s *Service) GetTelegram(ctx context.Context) (*TelegramSettings, error) {
	return s.repo.GetTelegramSettings(ctx)
}

// SaveTelegram saves Telegram settings
func (s *Service) SaveTelegram(ctx context.Context, telegram TelegramSettings) error {
	if err := s.repo.SaveTelegramSettings(ctx, telegram); err != nil {
		return err
	}

	// Also update in main settings
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return err
	}
	settings.Telegram = telegram
	return s.repo.SaveSettings(ctx, settings)
}

// GetTokens retrieves all tokens
func (s *Service) GetTokens(ctx context.Context) ([]TokenSettings, error) {
	return s.repo.GetAllTokens(ctx)
}

// GetToken retrieves a single token
func (s *Service) GetToken(ctx context.Context, id string) (*TokenSettings, error) {
	return s.repo.GetToken(ctx, id)
}

// AddToken adds a new token
func (s *Service) AddToken(ctx context.Context, token TokenSettings) error {
	// Validate token
	if err := s.validateToken(token); err != nil {
		return err
	}

	return s.repo.AddToken(ctx, token)
}

// UpdateToken updates an existing token
func (s *Service) UpdateToken(ctx context.Context, token TokenSettings) error {
	// Validate token
	if err := s.validateToken(token); err != nil {
		return err
	}

	return s.repo.UpdateToken(ctx, token)
}

// DeleteToken deletes a token
func (s *Service) DeleteToken(ctx context.Context, id string) error {
	return s.repo.DeleteToken(ctx, id)
}

// ToggleTokenNotify toggles notification for a token
func (s *Service) ToggleTokenNotify(ctx context.Context, id string, enabled bool) error {
	return s.repo.ToggleTokenNotify(ctx, id, enabled)
}

// UpdateTokenStreams updates streams for a token
func (s *Service) UpdateTokenStreams(ctx context.Context, id string, streams []StreamType) error {
	// Validate streams
	for _, stream := range streams {
		if !isValidStream(stream) {
			return fmt.Errorf("invalid stream type: %s", stream)
		}
	}

	return s.repo.UpdateTokenStreams(ctx, id, streams)
}

// ToggleStreamNotify toggles notification for a specific stream of a token
func (s *Service) ToggleStreamNotify(ctx context.Context, tokenID string, streamType StreamType, enabled bool) error {
	// Validate stream type
	if !isValidStream(streamType) {
		return fmt.Errorf("invalid stream type: %s", streamType)
	}

	return s.repo.ToggleStreamNotify(ctx, tokenID, streamType, enabled)
}

// GetStreamNotifyStatus gets the notification status for a specific stream of a token
func (s *Service) GetStreamNotifyStatus(ctx context.Context, tokenID string, streamType StreamType) (bool, error) {
	token, err := s.repo.GetToken(ctx, tokenID)
	if err != nil {
		return false, err
	}

	return token.IsStreamNotifyEnabled(streamType), nil
}

// GetActiveSubscriptions returns all active subscriptions
func (s *Service) GetActiveSubscriptions(ctx context.Context) ([]string, error) {
	return s.repo.GetActiveSubscriptions(ctx)
}

// ClearAll clears all settings
func (s *Service) ClearAll(ctx context.Context) error {
	return s.repo.ClearAll(ctx)
}

// validateToken validates token settings
func (s *Service) validateToken(token TokenSettings) error {
	if token.Address == "" {
		return fmt.Errorf("token address is required")
	}

	if !isValidChainType(token.ChainType) {
		return fmt.Errorf("invalid chain type: %s", token.ChainType)
	}

	for _, stream := range token.Streams {
		if !isValidStream(stream) {
			return fmt.Errorf("invalid stream type: %s", stream)
		}
	}

	return nil
}

// isValidChainType checks if chain type is valid
func isValidChainType(ct ChainType) bool {
	switch ct {
	case ChainSolana, ChainBSC, ChainBase:
		return true
	default:
		return false
	}
}

// isValidStream checks if stream type is valid
func isValidStream(st StreamType) bool {
	switch st {
	case StreamTicker24h, StreamHolders, StreamTx, StreamKline:
		return true
	default:
		return false
	}
}

// TokenFormData represents form data for token creation/update
type TokenFormData struct {
	ID            string          `json:"id" form:"id"`
	Address       string          `json:"address" form:"address"`
	ChainType     string          `json:"chain_type" form:"chain_type"`
	Name          string          `json:"name" form:"name"`
	Symbol        string          `json:"symbol" form:"symbol"`
	Streams       []string        `json:"streams" form:"streams"`
	NotifyEnabled bool            `json:"notify_enabled" form:"notify_enabled"`
	StreamNotify  map[string]bool `json:"stream_notify,omitempty" form:"stream_notify"` // Per-stream notify settings
}

// ToTokenSettings converts form data to TokenSettings
func (f *TokenFormData) ToTokenSettings() TokenSettings {
	streams := make([]StreamType, 0, len(f.Streams))
	for _, s := range f.Streams {
		streams = append(streams, StreamType(s))
	}

	// Convert stream notify settings
	streamNotify := make(map[StreamType]StreamNotifyConfig)
	if f.StreamNotify != nil {
		for streamStr, enabled := range f.StreamNotify {
			streamNotify[StreamType(streamStr)] = StreamNotifyConfig{Enabled: enabled}
		}
	} else {
		// Default: use global NotifyEnabled for all streams
		for _, stream := range streams {
			streamNotify[stream] = StreamNotifyConfig{Enabled: f.NotifyEnabled}
		}
	}

	return TokenSettings{
		ID:            f.ID,
		Address:       f.Address,
		ChainType:     ChainType(f.ChainType),
		Name:          f.Name,
		Symbol:        f.Symbol,
		Streams:       streams,
		NotifyEnabled: f.NotifyEnabled,
		StreamNotify:  streamNotify,
	}
}

// TelegramFormData represents form data for Telegram settings
type TelegramFormData struct {
	BotToken  string `json:"bot_token" form:"bot_token"`
	ChatID    string `json:"chat_id" form:"chat_id"`
	RateLimit int    `json:"rate_limit" form:"rate_limit"`
	Enabled   bool   `json:"enabled" form:"enabled"`
}

// ToTelegramSettings converts form data to TelegramSettings
func (f *TelegramFormData) ToTelegramSettings() TelegramSettings {
	return TelegramSettings{
		BotToken:  f.BotToken,
		ChatID:    f.ChatID,
		RateLimit: f.RateLimit,
		Enabled:   f.Enabled,
	}
}
