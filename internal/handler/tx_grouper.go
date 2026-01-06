package handler

import (
	"sort"
	"sync"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

// TransactionGrouper groups multiple swaps from the same transaction
type TransactionGrouper struct {
	pendingSwaps map[string]*pendingTransaction
	mu           sync.RWMutex
	log          logger.Logger

	// Configuration
	groupWindow time.Duration // Time window to wait for related swaps
	maxSwaps    int           // Maximum swaps to group in one transaction
}

type pendingTransaction struct {
	txHash     string
	swaps      []*models.W3WTransactionMessage
	firstSeen  time.Time
	lastUpdate time.Time
	timer      *time.Timer
}

// NewTransactionGrouper creates a new transaction grouper
func NewTransactionGrouper(log logger.Logger, groupWindow time.Duration) *TransactionGrouper {
	if groupWindow == 0 {
		groupWindow = 500 * time.Millisecond // Default 500ms window
	}

	return &TransactionGrouper{
		pendingSwaps: make(map[string]*pendingTransaction),
		log:          log.With(logger.F("component", "tx_grouper")),
		groupWindow:  groupWindow,
		maxSwaps:     10, // Reasonable limit for multi-hop swaps
	}
}

// AddSwap adds a swap to the grouper and returns complete multi-hop swap if ready
// Returns nil if still waiting for more swaps
func (g *TransactionGrouper) AddSwap(swap *models.W3WTransactionMessage) *models.MultiHopSwap {
	g.mu.Lock()
	defer g.mu.Unlock()

	txHash := swap.D.TxHash

	// Get or create pending transaction
	pending, exists := g.pendingSwaps[txHash]
	if !exists {
		pending = &pendingTransaction{
			txHash:     txHash,
			swaps:      make([]*models.W3WTransactionMessage, 0, 4),
			firstSeen:  time.Now(),
			lastUpdate: time.Now(),
		}
		g.pendingSwaps[txHash] = pending

		g.log.Debug("new transaction group started",
			logger.F("tx_hash", txHash),
		)
	}

	// Add swap to pending list
	pending.swaps = append(pending.swaps, swap)
	pending.lastUpdate = time.Now()

	g.log.Debug("swap added to group",
		logger.F("tx_hash", txHash),
		logger.F("swap_count", len(pending.swaps)),
		logger.F("swap_path", getSwapPathString(pending.swaps)),
	)

	// Cancel existing timer if any
	if pending.timer != nil {
		pending.timer.Stop()
	}

	// Set new timer to finalize after groupWindow
	pending.timer = time.AfterFunc(g.groupWindow, func() {
		g.finalizeTransaction(txHash)
	})

	// Check if we hit max swaps limit (force finalize)
	if len(pending.swaps) >= g.maxSwaps {
		g.log.Warn("max swaps limit reached, finalizing immediately",
			logger.F("tx_hash", txHash),
			logger.F("swap_count", len(pending.swaps)),
		)
		if pending.timer != nil {
			pending.timer.Stop()
		}
		return g.buildMultiHopSwap(pending)
	}

	return nil
}

// finalizeTransaction is called when timer expires
func (g *TransactionGrouper) finalizeTransaction(txHash string) {
	g.mu.Lock()
	pending, exists := g.pendingSwaps[txHash]
	if !exists {
		g.mu.Unlock()
		return
	}

	multiHop := g.buildMultiHopSwap(pending)
	g.mu.Unlock()

	if multiHop != nil {
		g.log.Info("transaction group finalized",
			logger.F("tx_hash", txHash),
			logger.F("swap_count", len(multiHop.Swaps)),
			logger.F("is_multi_hop", multiHop.IsMultiHop()),
			logger.F("swap_path", getSwapPathString(multiHop.Swaps)),
			logger.F("total_value_usd", multiHop.TotalValueUSD),
		)
	}
}

// buildMultiHopSwap creates a MultiHopSwap from pending transaction
func (g *TransactionGrouper) buildMultiHopSwap(pending *pendingTransaction) *models.MultiHopSwap {
	if len(pending.swaps) == 0 {
		return nil
	}

	// Sort swaps by quote index to ensure correct order
	sort.Slice(pending.swaps, func(i, j int) bool {
		return pending.swaps[i].D.QuoteIndex < pending.swaps[j].D.QuoteIndex
	})

	// Calculate total value USD (sum of all swaps)
	totalValueUSD := 0.0
	for _, swap := range pending.swaps {
		totalValueUSD += swap.D.ValueUSD
	}

	multiHop := &models.MultiHopSwap{
		TxHash:        pending.txHash,
		Swaps:         pending.swaps,
		FirstSwap:     pending.swaps[0],
		LastSwap:      pending.swaps[len(pending.swaps)-1],
		TotalValueUSD: totalValueUSD,
		PlatformID:    pending.swaps[0].D.PlatformID,
		MakerAddress:  pending.swaps[0].D.MakerAddress,
		Timestamp:     pending.swaps[0].D.Timestamp,
	}

	// Remove from pending
	delete(g.pendingSwaps, pending.txHash)

	return multiHop
}

// GetPendingCount returns the number of pending transactions
func (g *TransactionGrouper) GetPendingCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.pendingSwaps)
}

// Cleanup removes old pending transactions that exceeded timeout
func (g *TransactionGrouper) Cleanup(maxAge time.Duration) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	removed := 0

	for txHash, pending := range g.pendingSwaps {
		if now.Sub(pending.firstSeen) > maxAge {
			g.log.Warn("removing stale pending transaction",
				logger.F("tx_hash", txHash),
				logger.F("age_seconds", now.Sub(pending.firstSeen).Seconds()),
				logger.F("swap_count", len(pending.swaps)),
			)
			if pending.timer != nil {
				pending.timer.Stop()
			}
			delete(g.pendingSwaps, txHash)
			removed++
		}
	}

	return removed
}

// getSwapPathString returns a string representation of swap path
func getSwapPathString(swaps []*models.W3WTransactionMessage) string {
	if len(swaps) == 0 {
		return ""
	}

	// Sort by quote index first
	sortedSwaps := make([]*models.W3WTransactionMessage, len(swaps))
	copy(sortedSwaps, swaps)
	sort.Slice(sortedSwaps, func(i, j int) bool {
		return sortedSwaps[i].D.QuoteIndex < sortedSwaps[j].D.QuoteIndex
	})

	path := sortedSwaps[0].D.Token0Symbol
	for _, swap := range sortedSwaps {
		path += " → " + swap.D.Token1Symbol
	}
	return path
}
