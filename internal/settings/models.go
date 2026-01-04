package settings

import "time"

// StreamType represents available stream types
type StreamType string

const (
	StreamTicker24h StreamType = "ticker24h"
	StreamHolders   StreamType = "holders"
	StreamTx        StreamType = "tx"
	StreamKline     StreamType = "kline"
)

// AllStreamTypes returns all available stream types
func AllStreamTypes() []StreamType {
	return []StreamType{StreamTicker24h, StreamHolders, StreamTx, StreamKline}
}

// ChainType represents supported blockchain chains
type ChainType string

const (
	ChainSolana ChainType = "CT_501" // Solana - Chain ID: 16
	ChainBSC    ChainType = "56"     // BNB Smart Chain - Chain ID: 14
	ChainBase   ChainType = "8453"   // Base - Chain ID: 199
)

// AllChainTypes returns all available chain types
func AllChainTypes() []ChainType {
	return []ChainType{ChainSolana, ChainBSC, ChainBase}
}

// ChainName returns human-readable name for chain type
func ChainName(ct ChainType) string {
	switch ct {
	case ChainSolana:
		return "Solana"
	case ChainBSC:
		return "BNB Smart Chain"
	case ChainBase:
		return "Base"
	default:
		return string(ct)
	}
}

// TelegramSettings holds Telegram bot configuration
type TelegramSettings struct {
	BotToken  string `json:"bot_token"`
	ChatID    string `json:"chat_id"`
	RateLimit int    `json:"rate_limit"`
	Enabled   bool   `json:"enabled"`
}

// StreamNotifyConfig holds notification configuration for a single stream
type StreamNotifyConfig struct {
	Enabled bool `json:"enabled"`
}

// TokenSettings holds configuration for a single token
type TokenSettings struct {
	ID            string       `json:"id"`
	Address       string       `json:"address"`
	ChainType     ChainType    `json:"chain_type"`
	Name          string       `json:"name"`
	Symbol        string       `json:"symbol"`
	Streams       []StreamType `json:"streams"`
	NotifyEnabled bool         `json:"notify_enabled"` // Global notify toggle for this token
	// StreamNotify holds per-stream notification settings
	// Key is the stream type (e.g., "ticker24h", "holders", "tx", "kline")
	StreamNotify map[StreamType]StreamNotifyConfig `json:"stream_notify,omitempty"`
	CreatedAt    time.Time                         `json:"created_at"`
	UpdatedAt    time.Time                         `json:"updated_at"`
}

// IsStreamNotifyEnabled checks if notification is enabled for a specific stream
// It returns true if the stream is in the Streams list and has notify enabled
// If StreamNotify is not set for the stream, it defaults to the global NotifyEnabled value
func (t *TokenSettings) IsStreamNotifyEnabled(streamType StreamType) bool {
	// First check if global notify is disabled
	if !t.NotifyEnabled {
		return false
	}

	// Check if stream exists in subscribed streams
	streamExists := false
	for _, s := range t.Streams {
		if s == streamType {
			streamExists = true
			break
		}
	}
	if !streamExists {
		return false
	}

	// Check per-stream setting if exists
	if t.StreamNotify != nil {
		if config, exists := t.StreamNotify[streamType]; exists {
			return config.Enabled
		}
	}

	// Default to global notify enabled
	return t.NotifyEnabled
}

// SetStreamNotify sets the notify status for a specific stream
func (t *TokenSettings) SetStreamNotify(streamType StreamType, enabled bool) {
	if t.StreamNotify == nil {
		t.StreamNotify = make(map[StreamType]StreamNotifyConfig)
	}
	t.StreamNotify[streamType] = StreamNotifyConfig{Enabled: enabled}
}

// InitStreamNotify initializes StreamNotify map with default values based on NotifyEnabled
func (t *TokenSettings) InitStreamNotify() {
	if t.StreamNotify == nil {
		t.StreamNotify = make(map[StreamType]StreamNotifyConfig)
	}
	for _, stream := range t.Streams {
		if _, exists := t.StreamNotify[stream]; !exists {
			t.StreamNotify[stream] = StreamNotifyConfig{Enabled: t.NotifyEnabled}
		}
	}
}

// StreamSettings holds configuration for stream subscriptions
type StreamSettings struct {
	TokenID       string     `json:"token_id"`
	StreamType    StreamType `json:"stream_type"`
	Enabled       bool       `json:"enabled"`
	NotifyEnabled bool       `json:"notify_enabled"`
}

// AppSettings holds all application settings
type AppSettings struct {
	Telegram  TelegramSettings `json:"telegram"`
	Tokens    []TokenSettings  `json:"tokens"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// NewDefaultSettings creates default settings
func NewDefaultSettings() *AppSettings {
	return &AppSettings{
		Telegram: TelegramSettings{
			RateLimit: 30,
			Enabled:   false,
		},
		Tokens:    []TokenSettings{},
		UpdatedAt: time.Now(),
	}
}

// StreamListItem represents a stream with its notify status for list view
type StreamListItem struct {
	Type          string `json:"type"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	NotifyEnabled bool   `json:"notify_enabled"`
}

// TokenListItem represents a token in list view
type TokenListItem struct {
	ID            string           `json:"id"`
	Address       string           `json:"address"`
	ChainType     string           `json:"chain_type"`
	ChainName     string           `json:"chain_name"`
	Name          string           `json:"name"`
	Symbol        string           `json:"symbol"`
	Streams       []string         `json:"streams"`
	StreamItems   []StreamListItem `json:"stream_items"` // Streams with notify status
	NotifyEnabled bool             `json:"notify_enabled"`
}

// ToListItem converts TokenSettings to TokenListItem
func (t *TokenSettings) ToListItem() TokenListItem {
	streams := make([]string, len(t.Streams))
	streamItems := make([]StreamListItem, len(t.Streams))

	for i, s := range t.Streams {
		streams[i] = string(s)
		streamItems[i] = StreamListItem{
			Type:          string(s),
			Label:         getStreamLabel(s),
			Description:   getStreamDescription(s),
			NotifyEnabled: t.IsStreamNotifyEnabled(s),
		}
	}
	return TokenListItem{
		ID:            t.ID,
		Address:       t.Address,
		ChainType:     string(t.ChainType),
		ChainName:     ChainName(t.ChainType),
		Name:          t.Name,
		Symbol:        t.Symbol,
		Streams:       streams,
		StreamItems:   streamItems,
		NotifyEnabled: t.NotifyEnabled,
	}
}

// getStreamLabel returns human-readable label for stream type
func getStreamLabel(st StreamType) string {
	switch st {
	case StreamTicker24h:
		return "24h Ticker"
	case StreamHolders:
		return "Holders"
	case StreamTx:
		return "Transactions"
	case StreamKline:
		return "Kline"
	default:
		return string(st)
	}
}

// getStreamDescription returns description for stream type
func getStreamDescription(st StreamType) string {
	switch st {
	case StreamTicker24h:
		return "Price and volume data"
	case StreamHolders:
		return "Token holder changes"
	case StreamTx:
		return "Real-time transactions"
	case StreamKline:
		return "Candlestick data"
	default:
		return ""
	}
}
