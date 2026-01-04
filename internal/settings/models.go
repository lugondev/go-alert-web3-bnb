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

// TokenSettings holds configuration for a single token
type TokenSettings struct {
	ID            string       `json:"id"`
	Address       string       `json:"address"`
	ChainType     ChainType    `json:"chain_type"`
	Name          string       `json:"name"`
	Symbol        string       `json:"symbol"`
	Streams       []StreamType `json:"streams"`
	NotifyEnabled bool         `json:"notify_enabled"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
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

// TokenListItem represents a token in list view
type TokenListItem struct {
	ID            string   `json:"id"`
	Address       string   `json:"address"`
	ChainType     string   `json:"chain_type"`
	ChainName     string   `json:"chain_name"`
	Name          string   `json:"name"`
	Symbol        string   `json:"symbol"`
	Streams       []string `json:"streams"`
	NotifyEnabled bool     `json:"notify_enabled"`
}

// ToListItem converts TokenSettings to TokenListItem
func (t *TokenSettings) ToListItem() TokenListItem {
	streams := make([]string, len(t.Streams))
	for i, s := range t.Streams {
		streams[i] = string(s)
	}
	return TokenListItem{
		ID:            t.ID,
		Address:       t.Address,
		ChainType:     string(t.ChainType),
		ChainName:     ChainName(t.ChainType),
		Name:          t.Name,
		Symbol:        t.Symbol,
		Streams:       streams,
		NotifyEnabled: t.NotifyEnabled,
	}
}
