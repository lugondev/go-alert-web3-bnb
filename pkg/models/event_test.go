package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventParseTransactionData(t *testing.T) {
	txData := TransactionData{
		Hash:        "0x123456789abcdef",
		From:        "0xabc123",
		To:          "0xdef456",
		Value:       "1000000000000000000",
		TokenSymbol: "BNB",
		Amount:      1.5,
		BlockNumber: 12345678,
	}

	data, err := json.Marshal(txData)
	if err != nil {
		t.Fatalf("failed to marshal transaction data: %v", err)
	}

	event := &Event{
		ID:        "1",
		Type:      EventTypeTransaction,
		Data:      data,
		Timestamp: time.Now(),
	}

	parsed, err := event.ParseTransactionData()
	if err != nil {
		t.Fatalf("failed to parse transaction data: %v", err)
	}

	if parsed.Hash != txData.Hash {
		t.Errorf("expected hash %s, got %s", txData.Hash, parsed.Hash)
	}
	if parsed.From != txData.From {
		t.Errorf("expected from %s, got %s", txData.From, parsed.From)
	}
	if parsed.Amount != txData.Amount {
		t.Errorf("expected amount %f, got %f", txData.Amount, parsed.Amount)
	}
}

func TestEventParseBlockData(t *testing.T) {
	blockData := BlockData{
		Number:       12345678,
		Hash:         "0xblock123",
		Transactions: 150,
		GasUsed:      8000000,
	}

	data, err := json.Marshal(blockData)
	if err != nil {
		t.Fatalf("failed to marshal block data: %v", err)
	}

	event := &Event{
		ID:        "2",
		Type:      EventTypeBlock,
		Data:      data,
		Timestamp: time.Now(),
	}

	parsed, err := event.ParseBlockData()
	if err != nil {
		t.Fatalf("failed to parse block data: %v", err)
	}

	if parsed.Number != blockData.Number {
		t.Errorf("expected number %d, got %d", blockData.Number, parsed.Number)
	}
	if parsed.Transactions != blockData.Transactions {
		t.Errorf("expected transactions %d, got %d", blockData.Transactions, parsed.Transactions)
	}
}

func TestEventParsePriceData(t *testing.T) {
	priceData := PriceData{
		Symbol:        "BNBUSDT",
		Price:         350.50,
		Change24h:     15.25,
		ChangePercent: 4.55,
	}

	data, err := json.Marshal(priceData)
	if err != nil {
		t.Fatalf("failed to marshal price data: %v", err)
	}

	event := &Event{
		ID:        "3",
		Type:      EventTypePrice,
		Data:      data,
		Timestamp: time.Now(),
	}

	parsed, err := event.ParsePriceData()
	if err != nil {
		t.Fatalf("failed to parse price data: %v", err)
	}

	if parsed.Symbol != priceData.Symbol {
		t.Errorf("expected symbol %s, got %s", priceData.Symbol, parsed.Symbol)
	}
	if parsed.Price != priceData.Price {
		t.Errorf("expected price %f, got %f", priceData.Price, parsed.Price)
	}
}

func TestEventParseAlertData(t *testing.T) {
	alertData := AlertData{
		Level:   "warning",
		Title:   "Test Alert",
		Message: "This is a test alert message",
	}

	data, err := json.Marshal(alertData)
	if err != nil {
		t.Fatalf("failed to marshal alert data: %v", err)
	}

	event := &Event{
		ID:        "4",
		Type:      EventTypeAlert,
		Data:      data,
		Timestamp: time.Now(),
	}

	parsed, err := event.ParseAlertData()
	if err != nil {
		t.Fatalf("failed to parse alert data: %v", err)
	}

	if parsed.Level != alertData.Level {
		t.Errorf("expected level %s, got %s", alertData.Level, parsed.Level)
	}
	if parsed.Title != alertData.Title {
		t.Errorf("expected title %s, got %s", alertData.Title, parsed.Title)
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventTypeTransaction, "transaction"},
		{EventTypeBlock, "block"},
		{EventTypePrice, "price"},
		{EventTypeAlert, "alert"},
		{EventTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(tt.eventType))
			}
		})
	}
}

func TestEventParseInvalidData(t *testing.T) {
	event := &Event{
		ID:        "5",
		Type:      EventTypeTransaction,
		Data:      []byte("invalid json"),
		Timestamp: time.Now(),
	}

	_, err := event.ParseTransactionData()
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
