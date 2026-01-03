package models

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// EventType represents the type of WebSocket event
type EventType string

const (
	EventTypeTransaction EventType = "transaction"
	EventTypeBlock       EventType = "block"
	EventTypePrice       EventType = "price"
	EventTypeAlert       EventType = "alert"
	EventTypeTicker24h   EventType = "ticker24h"
	EventTypeKline       EventType = "kline"
	EventTypeHolders     EventType = "holders"
	EventTypeUnknown     EventType = "unknown"
)

// Event represents a WebSocket event received from the server
type Event struct {
	ID        string          `json:"id,omitempty"`
	Type      EventType       `json:"type"`
	Stream    string          `json:"stream,omitempty"`
	Data      json.RawMessage `json:"data"`
	Timestamp time.Time       `json:"timestamp"`
}

// W3WStreamMessage represents the raw message from Binance W3W WebSocket
type W3WStreamMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// SubscribeResponse represents the subscription response
type SubscribeResponse struct {
	ID     string      `json:"id"`
	Result interface{} `json:"result"`
}

// Ticker24hData represents W3W ticker 24h data
type Ticker24hData struct {
	// Contract address with chain type
	ContractAddress string `json:"ca"`
	// Timestamp
	Timestamp string `json:"t"`
	// Price
	Price string `json:"p"`
	// Price change percentages
	PriceChange5m  string `json:"pc5m"`
	PriceChange1h  string `json:"pc1"`
	PriceChange4h  string `json:"pc4"`
	PriceChange24h string `json:"pc24"`
	// Price high/low 24h
	PriceHigh24h string `json:"ph24"`
	PriceLow24h  string `json:"pl24"`
	// Volume
	Volume24h string `json:"vol24"`
	Volume4h  string `json:"vol4"`
	Volume1h  string `json:"vol1"`
	Volume5m  string `json:"vol5m"`
	// Buy/Sell volumes
	VolumeBuy24h  string `json:"v24b"`
	VolumeSell24h string `json:"v24s"`
	VolumeBuy4h   string `json:"v4b"`
	VolumeSell4h  string `json:"v4s"`
	VolumeBuy1h   string `json:"v1b"`
	VolumeSell1h  string `json:"v1s"`
	VolumeBuy5m   string `json:"v5mb"`
	VolumeSell5m  string `json:"v5ms"`
	// Market cap
	MarketCap string `json:"mc"`
	// Liquidity
	Liquidity string `json:"liq"`
	// Circulating supply
	CirculatingSupply string `json:"cs"`
	// Total supply
	TotalSupply string `json:"ts"`
	// Transaction counts
	TxCount24h   string `json:"cnt24"`
	TxCount4h    string `json:"cnt4"`
	TxCount1h    string `json:"cnt1"`
	TxCount5m    string `json:"cnt5m"`
	TxCountBuy5m string `json:"cnt5mb"`
	// Trader counts
	TraderCount24h string `json:"td24"`
	TraderCount4h  string `json:"td4"`
	TraderCount1h  string `json:"td1"`
	TraderCount5m  string `json:"td5m"`
	// Binance volumes
	BinanceVolume24h    string `json:"vol24hBn"`
	BinanceVolume4h     string `json:"vol4hBn"`
	BinanceVolume1h     string `json:"vol1hBn"`
	BinanceVolume5m     string `json:"vol5mBn"`
	BinanceNetVolume24h string `json:"vol24hNetBn"`
	BinanceNetVolume4h  string `json:"vol4hNetBn"`
	BinanceNetVolume1h  string `json:"vol1hNetBn"`
	BinanceNetVolume5m  string `json:"vol5mNetBn"`
	// Binance ask/bid prices
	BinanceAskPrice string `json:"bnABP"`
	BinanceBidPrice string `json:"bnASP"`
	// Binance trader count
	BinanceTraderCount string `json:"bnTd"`
	// Holder count
	HolderCount    string `json:"hc"`
	KYCHolderCount string `json:"kycHCnt"`
	// Launch timestamp
	LaunchTimestamp string `json:"lt"`
	// Migration status
	MigrationStatus int `json:"mgst"`
	// Program (DEX)
	Program interface{} `json:"prog"`
}

// KlineData represents candlestick/kline data
type KlineData struct {
	ContractAddress string `json:"ca"`
	Interval        string `json:"i"`
	OpenTime        string `json:"t"`
	CloseTime       string `json:"T"`
	Open            string `json:"o"`
	High            string `json:"h"`
	Low             string `json:"l"`
	Close           string `json:"c"`
	Volume          string `json:"v"`
	Closed          bool   `json:"x"`
}

// GetOpenTimeUnix returns OpenTime as Unix timestamp (milliseconds)
func (k *KlineData) GetOpenTimeUnix() (int64, error) {
	return strconv.ParseInt(k.OpenTime, 10, 64)
}

// GetCloseTimeUnix returns CloseTime as Unix timestamp (milliseconds)
func (k *KlineData) GetCloseTimeUnix() (int64, error) {
	return strconv.ParseInt(k.CloseTime, 10, 64)
}

// HoldersData represents holder information
type HoldersData struct {
	ContractAddress string `json:"ca"`
	HolderCount     string `json:"hc"`
	KYCHolderCount  string `json:"kycHCnt"`
	Timestamp       string `json:"t"`
}

// TransactionData represents transaction event data
type TransactionData struct {
	Hash            string  `json:"hash"`
	From            string  `json:"from"`
	To              string  `json:"to"`
	Value           string  `json:"value"`
	TokenSymbol     string  `json:"token_symbol,omitempty"`
	Amount          float64 `json:"amount,omitempty"`
	BlockNumber     uint64  `json:"block_number"`
	TransactionType string  `json:"type,omitempty"` // buy, sell
}

// BlockData represents block event data
type BlockData struct {
	Number       uint64 `json:"number"`
	Hash         string `json:"hash"`
	Transactions int    `json:"transactions"`
	GasUsed      uint64 `json:"gas_used"`
}

// PriceData represents price event data
type PriceData struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price"`
	Change24h     float64 `json:"change_24h"`
	ChangePercent float64 `json:"change_percent"`
}

// AlertData represents alert event data
type AlertData struct {
	Level   string `json:"level"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// ParseStreamType extracts the event type from stream name
// Examples:
// - "w3w@So111...@CT_501@ticker24h" -> EventTypeTicker24h
// - "tx@16_8Ui..." -> EventTypeTransaction
// - "kl@16@8Ui...@5m" -> EventTypeKline
// - "w3w@8Ui...@CT_501@holders" -> EventTypeHolders
func ParseStreamType(stream string) EventType {
	parts := strings.Split(stream, "@")
	if len(parts) == 0 {
		return EventTypeUnknown
	}

	// Check prefix
	prefix := parts[0]
	switch prefix {
	case "tx":
		return EventTypeTransaction
	case "kl":
		return EventTypeKline
	case "w3w":
		// Check suffix for w3w streams
		if len(parts) >= 4 {
			suffix := parts[len(parts)-1]
			switch suffix {
			case "ticker24h":
				return EventTypeTicker24h
			case "holders":
				return EventTypeHolders
			}
		}
	}

	return EventTypeUnknown
}

// ParseTicker24hData parses event data as Ticker24hData
func (e *Event) ParseTicker24hData() (*Ticker24hData, error) {
	var data Ticker24hData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseKlineData parses event data as KlineData
func (e *Event) ParseKlineData() (*KlineData, error) {
	var data KlineData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseHoldersData parses event data as HoldersData
func (e *Event) ParseHoldersData() (*HoldersData, error) {
	var data HoldersData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseTransactionData parses event data as TransactionData
func (e *Event) ParseTransactionData() (*TransactionData, error) {
	var data TransactionData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseBlockData parses event data as BlockData
func (e *Event) ParseBlockData() (*BlockData, error) {
	var data BlockData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParsePriceData parses event data as PriceData
func (e *Event) ParsePriceData() (*PriceData, error) {
	var data PriceData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// ParseAlertData parses event data as AlertData
func (e *Event) ParseAlertData() (*AlertData, error) {
	var data AlertData
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return nil, err
	}
	return &data, nil
}
