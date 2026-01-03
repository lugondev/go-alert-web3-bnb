package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// SubscribeMessage represents the subscription request
type SubscribeMessage struct {
	ID     string   `json:"id"`
	Method string   `json:"method"`
	Params []string `json:"params"`
}

func main() {
	// WebSocket URL from config
	wsURL := "wss://nbstream.binance.com/w3w/stream"

	// Subscribe message
	subscribeMsg := SubscribeMessage{
		ID:     "3",
		Method: "SUBSCRIBE",
		Params: []string{
			"w3w@So11111111111111111111111111111111111111111@CT_501@ticker24h",
			"w3w@So11111111111111111111111111111111111111112@CT_501@ticker24h",

			"w3w@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@CT_501@ticker24h",
			"tx@16_pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn",
			"tx@tag@16_pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn",
			"w3w@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@CT_501@holders",
			"kl@16@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@5m",
		},
	}

	log.Printf("Connecting to WebSocket: %s", wsURL)

	// Create dialer with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// Connect to WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	conn, resp, err := dialer.DialContext(ctx, wsURL, nil)
	cancel()
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	if resp != nil {
		log.Printf("Connection response status: %s", resp.Status)
	}

	log.Println("Connected successfully!")

	// Send subscribe message
	msgBytes, err := json.Marshal(subscribeMsg)
	if err != nil {
		log.Fatalf("Failed to marshal subscribe message: %v", err)
	}

	log.Printf("Sending subscribe message: %s", string(msgBytes))

	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		log.Fatalf("Failed to send subscribe message: %v", err)
	}

	log.Println("Subscribe message sent! Listening for messages...")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create done channel
	done := make(chan struct{})

	// Start message reader goroutine
	go func() {
		defer close(done)
		messageCount := 0

		for {
			// Set read deadline
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Println("WebSocket closed normally")
					return
				}
				log.Printf("Read error: %v", err)
				return
			}

			messageCount++
			timestamp := time.Now().Format("2006-01-02 15:04:05.000")

			// Pretty print JSON if possible
			var prettyJSON map[string]interface{}
			if err := json.Unmarshal(message, &prettyJSON); err == nil {
				prettyBytes, _ := json.MarshalIndent(prettyJSON, "", "  ")
				log.Printf("\n[%s] Message #%d (type=%d):\n%s\n",
					timestamp, messageCount, messageType, string(prettyBytes))
			} else {
				log.Printf("\n[%s] Message #%d (type=%d):\n%s\n",
					timestamp, messageCount, messageType, string(message))
			}
		}
	}()

	// Start ping goroutine to keep connection alive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("Ping error: %v", err)
					return
				}
				log.Println("Ping sent")
			}
		}
	}()

	// Wait for interrupt signal or done
	select {
	case sig := <-sigChan:
		log.Printf("Received signal: %v, shutting down...", sig)
	case <-done:
		log.Println("Connection closed")
	}

	// Send close message
	log.Println("Sending close message...")
	conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)

	log.Println("Goodbye!")
}
