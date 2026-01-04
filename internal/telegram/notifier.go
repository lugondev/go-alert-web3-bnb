package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
)

var (
	ErrRateLimited = errors.New("rate limit exceeded")
	ErrSendFailed  = errors.New("failed to send telegram message")
)

// TickerData interface for ticker cache data to avoid circular import
type TickerData interface {
	GetPrice() float64
	GetMarketCap() float64
	GetVolume24h() float64
	GetPriceChange24h() float64
	GetLiquidity() float64
}

const (
	telegramAPIURL = "https://api.telegram.org/bot%s/sendMessage"
)

// Config holds Telegram notifier configuration
type Config struct {
	BotToken   string
	ChatID     string
	RateLimit  int // Messages per minute
	Timeout    time.Duration
	RetryCount int
}

// Message represents a Telegram message
type Message struct {
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

// Response represents the Telegram API response
type Response struct {
	OK          bool   `json:"ok"`
	Description string `json:"description,omitempty"`
	ErrorCode   int    `json:"error_code,omitempty"`
}

// Notifier handles sending notifications to Telegram
type Notifier struct {
	config     Config
	httpClient *http.Client
	log        logger.Logger
	enabled    bool

	mu           sync.Mutex
	messageCount int
	lastReset    time.Time
}

// NewNotifier creates a new Telegram notifier
func NewNotifier(cfg Config, log logger.Logger) *Notifier {
	enabled := cfg.BotToken != "" && cfg.ChatID != ""

	n := &Notifier{
		config:    cfg,
		log:       log.With(logger.F("component", "telegram")),
		lastReset: time.Now(),
		enabled:   enabled,
	}

	if enabled {
		n.httpClient = &http.Client{
			Timeout: cfg.Timeout,
		}
		n.log.Info("telegram notifier enabled")
	} else {
		n.log.Warn("telegram notifier disabled: missing bot token or chat ID")
	}

	return n
}

// IsEnabled returns true if Telegram notifications are enabled
func (n *Notifier) IsEnabled() bool {
	return n.enabled
}

// Send sends a text message to the configured chat
func (n *Notifier) Send(ctx context.Context, text string) error {
	return n.SendWithParseMode(ctx, text, "")
}

// SendMarkdown sends a message with Markdown formatting
func (n *Notifier) SendMarkdown(ctx context.Context, text string) error {
	return n.SendWithParseMode(ctx, text, "Markdown")
}

// SendHTML sends a message with HTML formatting
func (n *Notifier) SendHTML(ctx context.Context, text string) error {
	return n.SendWithParseMode(ctx, text, "HTML")
}

// SendWithParseMode sends a message with specified parse mode
func (n *Notifier) SendWithParseMode(ctx context.Context, text string, parseMode string) error {
	// Skip if not enabled
	if !n.enabled {
		n.log.Debug("telegram notification skipped: notifier disabled")
		return nil
	}

	// Check rate limit
	if err := n.checkRateLimit(); err != nil {
		return err
	}

	msg := Message{
		ChatID:    n.config.ChatID,
		Text:      text,
		ParseMode: parseMode,
	}

	return n.sendWithRetry(ctx, msg)
}

// checkRateLimit checks if we're within rate limits
func (n *Notifier) checkRateLimit() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	now := time.Now()

	// Reset counter every minute
	if now.Sub(n.lastReset) >= time.Minute {
		n.messageCount = 0
		n.lastReset = now
	}

	if n.messageCount >= n.config.RateLimit {
		return ErrRateLimited
	}

	n.messageCount++
	return nil
}

// sendWithRetry sends a message with retry logic
func (n *Notifier) sendWithRetry(ctx context.Context, msg Message) error {
	var lastErr error

	for i := 0; i <= n.config.RetryCount; i++ {
		if i > 0 {
			// Exponential backoff
			backoff := time.Duration(i*i) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := n.send(ctx, msg)
		if err == nil {
			return nil
		}

		lastErr = err
		n.log.Warn("telegram send failed, retrying",
			logger.F("attempt", i+1),
			logger.F("max_retries", n.config.RetryCount),
			logger.F("error", err),
		)
	}

	return fmt.Errorf("%w: %v", ErrSendFailed, lastErr)
}

// send performs the actual HTTP request to Telegram
func (n *Notifier) send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	url := fmt.Sprintf(telegramAPIURL, n.config.BotToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var tgResp Response
	if err := json.Unmarshal(respBody, &tgResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !tgResp.OK {
		n.log.Error("telegram API error",
			logger.F("error_code", tgResp.ErrorCode),
			logger.F("description", tgResp.Description),
		)
		return fmt.Errorf("telegram API error: %s", tgResp.Description)
	}

	n.log.Debug("message sent successfully")
	return nil
}

// Formatter helps format messages for Telegram
type Formatter struct{}

// NewFormatter creates a new message formatter
func NewFormatter() *Formatter {
	return &Formatter{}
}

// FormatTransaction formats a transaction event for Telegram (legacy format)
func (f *Formatter) FormatTransaction(hash, from, to, value, symbol string, amount float64) string {
	return fmt.Sprintf(`🔄 *Transaction Alert*

*Hash:* `+"`%s`"+`
*From:* `+"`%s`"+`
*To:* `+"`%s`"+`
*Value:* %s
*Token:* %s
*Amount:* %.6f
*Time:* %s`,
		truncateHash(hash),
		truncateAddress(from),
		truncateAddress(to),
		value,
		symbol,
		amount,
		time.Now().Format(time.RFC3339),
	)
}

// FormatW3WTransaction formats a W3W transaction event for Telegram with Solscan link
func (f *Formatter) FormatW3WTransaction(txHash, txType, token0Addr, token1Addr, token0Symbol, token1Symbol string, amount0, amount1, valueUSD, token0Price, token1Price float64, platformID int) string {
	return f.FormatW3WTransactionWithTicker(txHash, txType, token0Addr, token1Addr, token0Symbol, token1Symbol, amount0, amount1, valueUSD, token0Price, token1Price, platformID, "", nil)
}

// FormatW3WTransactionWithTicker formats a W3W transaction event with optional ticker data
func (f *Formatter) FormatW3WTransactionWithTicker(txHash, txType, token0Addr, token1Addr, token0Symbol, token1Symbol string, amount0, amount1, valueUSD, token0Price, token1Price float64, platformID int, makerAddress string, tickerData TickerData) string {
	// Determine emoji based on transaction type
	emoji := "🔄"
	typeLabel := "Swap"
	switch txType {
	case "buy":
		emoji = "🟢"
		typeLabel = "Buy"
	case "sell":
		emoji = "🔴"
		typeLabel = "Sell"
	}

	// Build explorer link based on platform
	var explorerLink string
	var explorerName string
	var makerExplorerLink string
	switch platformID {
	case 16: // Solana
		explorerLink = fmt.Sprintf("https://solscan.io/tx/%s", txHash)
		explorerName = "Solscan"
		if makerAddress != "" {
			makerExplorerLink = fmt.Sprintf("https://solscan.io/account/%s", makerAddress)
		}
	case 1: // Ethereum
		explorerLink = fmt.Sprintf("https://etherscan.io/tx/%s", txHash)
		explorerName = "Etherscan"
		if makerAddress != "" {
			makerExplorerLink = fmt.Sprintf("https://etherscan.io/address/%s", makerAddress)
		}
	case 56: // BSC
		explorerLink = fmt.Sprintf("https://bscscan.com/tx/%s", txHash)
		explorerName = "BscScan"
		if makerAddress != "" {
			makerExplorerLink = fmt.Sprintf("https://bscscan.com/address/%s", makerAddress)
		}
	default:
		explorerLink = fmt.Sprintf("https://solscan.io/tx/%s", txHash)
		explorerName = "Explorer"
		if makerAddress != "" {
			makerExplorerLink = fmt.Sprintf("https://solscan.io/account/%s", makerAddress)
		}
	}

	// Build maker info section if available
	makerInfo := ""
	if makerAddress != "" {
		makerInfo = fmt.Sprintf(`
*Maker:* [%s](%s)`, truncateAddress(makerAddress), makerExplorerLink)
	}

	// Build ticker info section if available
	tickerInfo := ""
	if tickerData != nil {
		priceChangeEmoji := "📈"
		if tickerData.GetPriceChange24h() < 0 {
			priceChangeEmoji = "📉"
		}

		tickerInfo = fmt.Sprintf(`

📊 *Token Stats*
*Price:* $%s
*24h Change:* %s %.2f%%
*Market Cap:* $%s
*Volume 24h:* $%s
*Liquidity:* $%s`,
			formatPriceCompact(tickerData.GetPrice()),
			priceChangeEmoji,
			tickerData.GetPriceChange24h(),
			formatLargeNumber(tickerData.GetMarketCap()),
			formatLargeNumber(tickerData.GetVolume24h()),
			formatLargeNumber(tickerData.GetLiquidity()),
		)
	}

	return fmt.Sprintf(`%s *%s Transaction*

*Value:* $%.2f%s

*%s:* %.6f
*%s:* %.6f

*%s Price:* $%.6f
*%s Price:* $%.6f%s

[View on %s](%s)
*Time:* %s`,
		emoji,
		typeLabel,
		valueUSD,
		makerInfo,
		token0Symbol,
		amount0,
		token1Symbol,
		amount1,
		token0Symbol,
		token0Price,
		token1Symbol,
		token1Price,
		tickerInfo,
		explorerName,
		explorerLink,
		time.Now().Format(time.RFC3339),
	)
}

// FormatBlock formats a block event for Telegram
func (f *Formatter) FormatBlock(number uint64, hash string, txCount int, gasUsed uint64) string {
	return fmt.Sprintf(`📦 *New Block*

*Block Number:* %d
*Hash:* `+"`%s`"+`
*Transactions:* %d
*Gas Used:* %d
*Time:* %s`,
		number,
		truncateHash(hash),
		txCount,
		gasUsed,
		time.Now().Format(time.RFC3339),
	)
}

// FormatPrice formats a price event for Telegram
func (f *Formatter) FormatPrice(symbol string, price, change24h, changePercent float64) string {
	emoji := "📈"
	if changePercent < 0 {
		emoji = "📉"
	}

	return fmt.Sprintf(`%s *Price Update*

*Symbol:* %s
*Price:* $%.4f
*24h Change:* $%.4f (%.2f%%)
*Time:* %s`,
		emoji,
		symbol,
		price,
		change24h,
		changePercent,
		time.Now().Format(time.RFC3339),
	)
}

// FormatAlert formats a general alert for Telegram
func (f *Formatter) FormatAlert(level, title, message string) string {
	emoji := "ℹ️"
	switch level {
	case "warning":
		emoji = "⚠️"
	case "error":
		emoji = "❌"
	case "critical":
		emoji = "🚨"
	case "success":
		emoji = "✅"
	}

	return fmt.Sprintf(`%s *%s*

%s

*Time:* %s`,
		emoji,
		title,
		message,
		time.Now().Format(time.RFC3339),
	)
}

// FormatError formats an error message for Telegram
func (f *Formatter) FormatError(err error, context string) string {
	return fmt.Sprintf(`❌ *Error Alert*

*Context:* %s
*Error:* %s
*Time:* %s`,
		context,
		err.Error(),
		time.Now().Format(time.RFC3339),
	)
}

// FormatTicker24h formats a W3W ticker 24h event for Telegram
func (f *Formatter) FormatTicker24h(contractAddress string, price, priceChange24h, volume24h, marketCap, liquidity float64) string {
	emoji := "📈"
	if priceChange24h < 0 {
		emoji = "📉"
	}

	return fmt.Sprintf(`%s *Ticker 24h Update*

*Contract:* `+"`%s`"+`
*Price:* $%.6f
*24h Change:* %.2f%%
*Volume 24h:* $%s
*Market Cap:* $%s
*Liquidity:* $%s
*Time:* %s`,
		emoji,
		truncateAddress(contractAddress),
		price,
		priceChange24h,
		formatLargeNumber(volume24h),
		formatLargeNumber(marketCap),
		formatLargeNumber(liquidity),
		time.Now().Format(time.RFC3339),
	)
}

// FormatKline formats a kline/candlestick event for Telegram
func (f *Formatter) FormatKline(contractAddress, interval string, open, high, low, closePrice, volume float64, closed bool) string {
	status := "📊"
	if closed {
		status = "✅"
	}

	change := ((closePrice - open) / open) * 100
	changeEmoji := "📈"
	if change < 0 {
		changeEmoji = "📉"
	}

	return fmt.Sprintf(`%s *Kline %s* %s

*Contract:* `+"`%s`"+`
*Open:* $%.6f
*High:* $%.6f
*Low:* $%.6f
*Close:* $%.6f
*Change:* %s %.2f%%
*Volume:* $%s
*Closed:* %v
*Time:* %s`,
		status,
		interval,
		changeEmoji,
		truncateAddress(contractAddress),
		open,
		high,
		low,
		closePrice,
		changeEmoji,
		change,
		formatLargeNumber(volume),
		closed,
		time.Now().Format(time.RFC3339),
	)
}

// FormatHolders formats a holder count event for Telegram
func (f *Formatter) FormatHolders(contractAddress string, holderCount, kycHolderCount int64) string {
	return fmt.Sprintf(`👥 *Holder Update*

*Contract:* `+"`%s`"+`
*Total Holders:* %s
*KYC Holders:* %s
*Time:* %s`,
		truncateAddress(contractAddress),
		formatNumber(holderCount),
		formatNumber(kycHolderCount),
		time.Now().Format(time.RFC3339),
	)
}

// Helper functions
func truncateHash(hash string) string {
	if len(hash) > 16 {
		return hash[:8] + "..." + hash[len(hash)-8:]
	}
	return hash
}

func truncateAddress(addr string) string {
	if len(addr) > 12 {
		return addr[:6] + "..." + addr[len(addr)-4:]
	}
	return addr
}

// formatLargeNumber formats a large number with K, M, B suffixes
func formatLargeNumber(n float64) string {
	if n >= 1e12 {
		return fmt.Sprintf("%.2fT", n/1e12)
	}
	if n >= 1e9 {
		return fmt.Sprintf("%.2fB", n/1e9)
	}
	if n >= 1e6 {
		return fmt.Sprintf("%.2fM", n/1e6)
	}
	if n >= 1e3 {
		return fmt.Sprintf("%.2fK", n/1e3)
	}
	return fmt.Sprintf("%.2f", n)
}

// formatNumber formats an integer with comma separators
func formatNumber(n int64) string {
	if n >= 1e9 {
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	}
	if n >= 1e6 {
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	}
	if n >= 1e3 {
		return fmt.Sprintf("%.2fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// formatPriceCompact formats a price with appropriate precision
func formatPriceCompact(price float64) string {
	if price >= 1000 {
		return fmt.Sprintf("%.2f", price)
	}
	if price >= 1 {
		return fmt.Sprintf("%.4f", price)
	}
	if price >= 0.0001 {
		return fmt.Sprintf("%.6f", price)
	}
	return fmt.Sprintf("%.10f", price)
}
