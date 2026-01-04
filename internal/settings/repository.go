package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

const (
	// Redis keys
	keySettings     = "alert:settings"
	keyTelegram     = "alert:settings:telegram"
	keyTokens       = "alert:settings:tokens"
	keyTokenPrefix  = "alert:settings:token:"
	keyStreams      = "alert:settings:streams"
	keyStreamPrefix = "alert:settings:stream:"
)

// Repository handles settings persistence in Redis
type Repository struct {
	rdb *redis.Client
	log logger.Logger
}

// NewRepository creates a new settings repository
func NewRepository(rdb *redis.Client, log logger.Logger) *Repository {
	return &Repository{
		rdb: rdb,
		log: log.With(logger.F("component", "settings-repository")),
	}
}

// Initialize loads initial settings from config if not exists in Redis
func (r *Repository) Initialize(ctx context.Context, telegram TelegramSettings, tokens []TokenSettings) error {
	// Check if settings already exist
	exists, err := r.rdb.Exists(ctx, keySettings).Result()
	if err != nil {
		return fmt.Errorf("failed to check settings existence: %w", err)
	}

	if exists > 0 {
		r.log.Info("settings already exist in Redis, skipping initialization")
		return nil
	}

	// Initialize settings from config
	settings := &AppSettings{
		Telegram:  telegram,
		Tokens:    tokens,
		UpdatedAt: time.Now(),
	}

	// Generate IDs for tokens
	for i := range settings.Tokens {
		if settings.Tokens[i].ID == "" {
			settings.Tokens[i].ID = uuid.New().String()
		}
		if settings.Tokens[i].CreatedAt.IsZero() {
			settings.Tokens[i].CreatedAt = time.Now()
		}
		settings.Tokens[i].UpdatedAt = time.Now()
	}

	if err := r.SaveSettings(ctx, settings); err != nil {
		return fmt.Errorf("failed to save initial settings: %w", err)
	}

	r.log.Info("initialized settings from config",
		logger.F("telegram_enabled", telegram.Enabled),
		logger.F("tokens_count", len(tokens)),
	)

	return nil
}

// SaveSettings saves all settings to Redis
func (r *Repository) SaveSettings(ctx context.Context, settings *AppSettings) error {
	settings.UpdatedAt = time.Now()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	if err := r.rdb.Set(ctx, keySettings, data, 0).Err(); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	// Also save telegram and tokens separately for quick access
	if err := r.SaveTelegramSettings(ctx, settings.Telegram); err != nil {
		r.log.Warn("failed to save telegram settings separately", logger.F("error", err))
	}

	return nil
}

// GetSettings retrieves all settings from Redis
func (r *Repository) GetSettings(ctx context.Context) (*AppSettings, error) {
	data, err := r.rdb.Get(ctx, keySettings).Bytes()
	if err != nil {
		if err == redis.Nil {
			return NewDefaultSettings(), nil
		}
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	var settings AppSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return &settings, nil
}

// SaveTelegramSettings saves Telegram settings
func (r *Repository) SaveTelegramSettings(ctx context.Context, telegram TelegramSettings) error {
	data, err := json.Marshal(telegram)
	if err != nil {
		return fmt.Errorf("failed to marshal telegram settings: %w", err)
	}

	return r.rdb.Set(ctx, keyTelegram, data, 0).Err()
}

// GetTelegramSettings retrieves Telegram settings
func (r *Repository) GetTelegramSettings(ctx context.Context) (*TelegramSettings, error) {
	data, err := r.rdb.Get(ctx, keyTelegram).Bytes()
	if err != nil {
		if err == redis.Nil {
			return &TelegramSettings{RateLimit: 30}, nil
		}
		return nil, fmt.Errorf("failed to get telegram settings: %w", err)
	}

	var telegram TelegramSettings
	if err := json.Unmarshal(data, &telegram); err != nil {
		return nil, fmt.Errorf("failed to unmarshal telegram settings: %w", err)
	}

	return &telegram, nil
}

// AddToken adds a new token
func (r *Repository) AddToken(ctx context.Context, token TokenSettings) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	// Generate ID if not set
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	token.CreatedAt = time.Now()
	token.UpdatedAt = time.Now()

	// Check for duplicate address
	for _, t := range settings.Tokens {
		if t.Address == token.Address && t.ChainType == token.ChainType {
			return fmt.Errorf("token already exists: %s on %s", token.Address, ChainName(token.ChainType))
		}
	}

	settings.Tokens = append(settings.Tokens, token)

	return r.SaveSettings(ctx, settings)
}

// UpdateToken updates an existing token
func (r *Repository) UpdateToken(ctx context.Context, token TokenSettings) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	found := false
	for i, t := range settings.Tokens {
		if t.ID == token.ID {
			token.CreatedAt = t.CreatedAt
			token.UpdatedAt = time.Now()
			settings.Tokens[i] = token
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("token not found: %s", token.ID)
	}

	return r.SaveSettings(ctx, settings)
}

// DeleteToken removes a token
func (r *Repository) DeleteToken(ctx context.Context, tokenID string) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	found := false
	for i, t := range settings.Tokens {
		if t.ID == tokenID {
			settings.Tokens = append(settings.Tokens[:i], settings.Tokens[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("token not found: %s", tokenID)
	}

	return r.SaveSettings(ctx, settings)
}

// GetToken retrieves a single token by ID
func (r *Repository) GetToken(ctx context.Context, tokenID string) (*TokenSettings, error) {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range settings.Tokens {
		if t.ID == tokenID {
			return &t, nil
		}
	}

	return nil, fmt.Errorf("token not found: %s", tokenID)
}

// GetAllTokens retrieves all tokens
func (r *Repository) GetAllTokens(ctx context.Context) ([]TokenSettings, error) {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	return settings.Tokens, nil
}

// ToggleTokenNotify toggles notification for a token
func (r *Repository) ToggleTokenNotify(ctx context.Context, tokenID string, enabled bool) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	found := false
	for i, t := range settings.Tokens {
		if t.ID == tokenID {
			settings.Tokens[i].NotifyEnabled = enabled
			settings.Tokens[i].UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("token not found: %s", tokenID)
	}

	return r.SaveSettings(ctx, settings)
}

// UpdateTokenStreams updates streams for a token
func (r *Repository) UpdateTokenStreams(ctx context.Context, tokenID string, streams []StreamType) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	found := false
	for i, t := range settings.Tokens {
		if t.ID == tokenID {
			settings.Tokens[i].Streams = streams
			settings.Tokens[i].UpdatedAt = time.Now()
			// Initialize stream notify for new streams
			settings.Tokens[i].InitStreamNotify()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("token not found: %s", tokenID)
	}

	return r.SaveSettings(ctx, settings)
}

// ToggleStreamNotify toggles notification for a specific stream of a token
func (r *Repository) ToggleStreamNotify(ctx context.Context, tokenID string, streamType StreamType, enabled bool) error {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return err
	}

	found := false
	for i, t := range settings.Tokens {
		if t.ID == tokenID {
			// Check if stream exists in token's streams
			streamExists := false
			for _, s := range t.Streams {
				if s == streamType {
					streamExists = true
					break
				}
			}
			if !streamExists {
				return fmt.Errorf("stream %s not found in token %s", streamType, tokenID)
			}

			// Initialize StreamNotify if nil
			if settings.Tokens[i].StreamNotify == nil {
				settings.Tokens[i].StreamNotify = make(map[StreamType]StreamNotifyConfig)
			}

			settings.Tokens[i].StreamNotify[streamType] = StreamNotifyConfig{Enabled: enabled}
			settings.Tokens[i].UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("token not found: %s", tokenID)
	}

	return r.SaveSettings(ctx, settings)
}

// ClearAll removes all settings from Redis
func (r *Repository) ClearAll(ctx context.Context) error {
	keys := []string{keySettings, keyTelegram, keyTokens, keyStreams}

	// Find all token and stream keys
	tokenKeys, err := r.rdb.Keys(ctx, keyTokenPrefix+"*").Result()
	if err == nil {
		keys = append(keys, tokenKeys...)
	}

	streamKeys, err := r.rdb.Keys(ctx, keyStreamPrefix+"*").Result()
	if err == nil {
		keys = append(keys, streamKeys...)
	}

	// Also clear all alert-related keys
	alertKeys, err := r.rdb.Keys(ctx, "alert:*").Result()
	if err == nil {
		keys = append(keys, alertKeys...)
	}

	if len(keys) == 0 {
		r.log.Info("no settings to clear")
		return nil
	}

	if err := r.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("failed to clear settings: %w", err)
	}

	r.log.Info("cleared all settings", logger.F("keys_count", len(keys)))
	return nil
}

// GetActiveSubscriptions returns all active stream subscriptions
func (r *Repository) GetActiveSubscriptions(ctx context.Context) ([]string, error) {
	settings, err := r.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	var channels []string
	for _, token := range settings.Tokens {
		chainID := getChainID(token.ChainType)
		for _, stream := range token.Streams {
			switch stream {
			case StreamTicker24h, StreamHolders:
				channel := fmt.Sprintf("w3w@%s@%s@%s", token.Address, token.ChainType, stream)
				channels = append(channels, channel)
			case StreamTx:
				channel := fmt.Sprintf("tx@%d_%s", chainID, token.Address)
				channels = append(channels, channel)
			case StreamKline:
				channel := fmt.Sprintf("kl@%d@%s@1m", chainID, token.Address)
				channels = append(channels, channel)
			}
		}
	}

	return channels, nil
}

// getChainID returns the chain ID for a chain type
func getChainID(ct ChainType) int {
	switch ct {
	case ChainSolana:
		return 16
	case ChainBSC:
		return 14
	case ChainBase:
		return 199
	default:
		return 16
	}
}
