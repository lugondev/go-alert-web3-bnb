package websocket

import (
	"context"
	"fmt"
	"sync"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
)

// SubscriptionManager manages WebSocket subscriptions
// It provides a way to subscribe/unsubscribe channels dynamically
type SubscriptionManager struct {
	client      *Client
	settingsSvc *settings.Service
	log         logger.Logger

	mu sync.RWMutex
}

// NewSubscriptionManager creates a new subscription manager
func NewSubscriptionManager(client *Client, settingsSvc *settings.Service, log logger.Logger) *SubscriptionManager {
	return &SubscriptionManager{
		client:      client,
		settingsSvc: settingsSvc,
		log:         log.With(logger.F("component", "subscription-manager")),
	}
}

// SyncSubscriptions synchronizes subscriptions with current settings
// It compares current subscribed channels with active subscriptions from settings
// and subscribes/unsubscribes as needed
func (m *SubscriptionManager) SyncSubscriptions(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil || !m.client.IsConnected() {
		m.log.Warn("cannot sync subscriptions: client not connected")
		return ErrNotConnected
	}

	// Get active subscriptions from settings
	activeChannels, err := m.settingsSvc.GetActiveSubscriptions(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active subscriptions: %w", err)
	}

	// Get currently subscribed channels
	currentChannels := m.client.GetSubscribedChannels()

	// Find channels to unsubscribe (in current but not in active)
	toUnsubscribe := difference(currentChannels, activeChannels)

	// Find channels to subscribe (in active but not in current)
	toSubscribe := difference(activeChannels, currentChannels)

	m.log.Info("syncing subscriptions",
		logger.F("current", len(currentChannels)),
		logger.F("active", len(activeChannels)),
		logger.F("to_subscribe", len(toSubscribe)),
		logger.F("to_unsubscribe", len(toUnsubscribe)),
	)

	// Unsubscribe from channels no longer needed
	if len(toUnsubscribe) > 0 {
		m.log.Info("unsubscribing from channels",
			logger.F("channels", toUnsubscribe),
		)
		if err := m.client.Unsubscribe(ctx, toUnsubscribe); err != nil {
			m.log.Error("failed to unsubscribe", logger.F("error", err))
			// Continue with subscribe even if unsubscribe fails
		}
	}

	// Subscribe to new channels
	if len(toSubscribe) > 0 {
		m.log.Info("subscribing to channels",
			logger.F("channels", toSubscribe),
		)
		if err := m.client.Subscribe(ctx, toSubscribe); err != nil {
			return fmt.Errorf("failed to subscribe: %w", err)
		}
	}

	return nil
}

// UnsubscribeToken unsubscribes all channels for a specific token
func (m *SubscriptionManager) UnsubscribeToken(ctx context.Context, tokenAddress string, chainType settings.ChainType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil || !m.client.IsConnected() {
		return ErrNotConnected
	}

	// Build all possible channel patterns for this token
	channels := buildTokenChannels(tokenAddress, chainType)

	// Filter to only subscribed channels
	currentChannels := m.client.GetSubscribedChannels()
	toUnsubscribe := intersect(channels, currentChannels)

	if len(toUnsubscribe) == 0 {
		m.log.Debug("no channels to unsubscribe for token",
			logger.F("token", tokenAddress),
		)
		return nil
	}

	m.log.Info("unsubscribing token channels",
		logger.F("token", tokenAddress),
		logger.F("channels", toUnsubscribe),
	)

	return m.client.Unsubscribe(ctx, toUnsubscribe)
}

// SubscribeToken subscribes to all enabled channels for a specific token
func (m *SubscriptionManager) SubscribeToken(ctx context.Context, token *settings.TokenSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil || !m.client.IsConnected() {
		return ErrNotConnected
	}

	channels := buildTokenActiveChannels(token)
	if len(channels) == 0 {
		m.log.Debug("no channels to subscribe for token",
			logger.F("token", token.Address),
		)
		return nil
	}

	// Filter out already subscribed channels
	currentChannels := m.client.GetSubscribedChannels()
	toSubscribe := difference(channels, currentChannels)

	if len(toSubscribe) == 0 {
		m.log.Debug("all channels already subscribed",
			logger.F("token", token.Address),
		)
		return nil
	}

	m.log.Info("subscribing token channels",
		logger.F("token", token.Address),
		logger.F("channels", toSubscribe),
	)

	return m.client.Subscribe(ctx, toSubscribe)
}

// UnsubscribeStream unsubscribes a specific stream for a token
func (m *SubscriptionManager) UnsubscribeStream(ctx context.Context, tokenAddress string, chainType settings.ChainType, streamType settings.StreamType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil || !m.client.IsConnected() {
		return ErrNotConnected
	}

	channel := buildStreamChannel(tokenAddress, chainType, streamType)
	if channel == "" {
		return nil
	}

	// Check if channel is subscribed
	currentChannels := m.client.GetSubscribedChannels()
	found := false
	for _, ch := range currentChannels {
		if ch == channel {
			found = true
			break
		}
	}

	if !found {
		m.log.Debug("channel not subscribed, skipping unsubscribe",
			logger.F("channel", channel),
		)
		return nil
	}

	m.log.Info("unsubscribing stream",
		logger.F("token", tokenAddress),
		logger.F("stream", streamType),
		logger.F("channel", channel),
	)

	return m.client.Unsubscribe(ctx, []string{channel})
}

// SubscribeStream subscribes to a specific stream for a token
func (m *SubscriptionManager) SubscribeStream(ctx context.Context, tokenAddress string, chainType settings.ChainType, streamType settings.StreamType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client == nil || !m.client.IsConnected() {
		return ErrNotConnected
	}

	channel := buildStreamChannel(tokenAddress, chainType, streamType)
	if channel == "" {
		return nil
	}

	// Check if channel is already subscribed
	currentChannels := m.client.GetSubscribedChannels()
	for _, ch := range currentChannels {
		if ch == channel {
			m.log.Debug("channel already subscribed",
				logger.F("channel", channel),
			)
			return nil
		}
	}

	m.log.Info("subscribing stream",
		logger.F("token", tokenAddress),
		logger.F("stream", streamType),
		logger.F("channel", channel),
	)

	return m.client.Subscribe(ctx, []string{channel})
}

// GetSubscribedChannels returns currently subscribed channels
func (m *SubscriptionManager) GetSubscribedChannels() []string {
	if m.client == nil {
		return nil
	}
	return m.client.GetSubscribedChannels()
}

// Helper functions

// difference returns elements in a that are not in b
func difference(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, item := range b {
		bSet[item] = true
	}

	var result []string
	for _, item := range a {
		if !bSet[item] {
			result = append(result, item)
		}
	}
	return result
}

// intersect returns elements that are in both a and b
func intersect(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, item := range b {
		bSet[item] = true
	}

	var result []string
	for _, item := range a {
		if bSet[item] {
			result = append(result, item)
		}
	}
	return result
}

// buildTokenChannels builds all possible channel names for a token
func buildTokenChannels(tokenAddress string, chainType settings.ChainType) []string {
	chainID := getChainID(chainType)
	return []string{
		fmt.Sprintf("w3w@%s@%s@ticker24h", tokenAddress, chainType),
		fmt.Sprintf("w3w@%s@%s@holders", tokenAddress, chainType),
		fmt.Sprintf("tx@%d_%s", chainID, tokenAddress),
		fmt.Sprintf("kl@%d@%s@1m", chainID, tokenAddress),
	}
}

// buildTokenActiveChannels builds channel names for active streams of a token
func buildTokenActiveChannels(token *settings.TokenSettings) []string {
	var channels []string
	chainID := getChainID(token.ChainType)

	for _, stream := range token.Streams {
		switch stream {
		case settings.StreamTicker24h, settings.StreamHolders:
			channel := fmt.Sprintf("w3w@%s@%s@%s", token.Address, token.ChainType, stream)
			channels = append(channels, channel)
		case settings.StreamTx:
			channel := fmt.Sprintf("tx@%d_%s", chainID, token.Address)
			channels = append(channels, channel)
		case settings.StreamKline:
			channel := fmt.Sprintf("kl@%d@%s@1m", chainID, token.Address)
			channels = append(channels, channel)
		}
	}

	return channels
}

// buildStreamChannel builds channel name for a specific stream
func buildStreamChannel(tokenAddress string, chainType settings.ChainType, streamType settings.StreamType) string {
	chainID := getChainID(chainType)

	switch streamType {
	case settings.StreamTicker24h, settings.StreamHolders:
		return fmt.Sprintf("w3w@%s@%s@%s", tokenAddress, chainType, streamType)
	case settings.StreamTx:
		return fmt.Sprintf("tx@%d_%s", chainID, tokenAddress)
	case settings.StreamKline:
		return fmt.Sprintf("kl@%d@%s@1m", chainID, tokenAddress)
	default:
		return ""
	}
}

// getChainID returns the chain ID for a chain type
func getChainID(ct settings.ChainType) int {
	switch ct {
	case settings.ChainSolana:
		return 16
	case settings.ChainBSC:
		return 14
	case settings.ChainBase:
		return 199
	default:
		return 16
	}
}
