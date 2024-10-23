package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// ReqKind10002 initiates a request to a relay URL with kind 10002 and processes responses.
func ReqKind10002(relayURL string) error {
	// Create context with a timeout for the entire operation.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Establish a WebSocket connection.
	ws, err := establishWebSocketConnection(relayURL)
	if err != nil {
		return err
	}
	defer ws.Close()

	// Send the "REQ" message.
	if err := sendREQMessage(ws); err != nil {
		return fmt.Errorf("failed to send REQ message: %v", err)
	}

	// Continuously receive and process messages until "EOSE" or connection closed.
	return receiveMessages(ctx, ws)
}

// establishWebSocketConnection sets up and establishes the WebSocket connection.
func establishWebSocketConnection(relayURL string) (*websocket.Conn, error) {
	config, err := websocket.NewConfig(relayURL, "http://localhost/")
	if err != nil {
		return nil, fmt.Errorf("config error: %v", err)
	}

	ws, err := websocket.DialConfig(config)
	if err != nil {
		return nil, fmt.Errorf("dial error: %v", err)
	}

	return ws, nil
}

// sendREQMessage creates and sends a REQ message to the WebSocket connection.
func sendREQMessage(ws *websocket.Conn) error {
	subscriptionID := "crawlr"
	req := []interface{}{
		"REQ", subscriptionID, map[string]interface{}{
			"kinds": []int{10002},
			"limit": 100,
		},
	}

	return websocket.JSON.Send(ws, req)
}

// receiveMessages continuously receives and processes messages from the WebSocket connection.
func receiveMessages(ctx context.Context, ws *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout: no response from relay")
		default:
			var msg []byte
			if err := websocket.Message.Receive(ws, &msg); err != nil {
				if err == io.EOF {
					return nil // Connection closed normally.
				}
				return fmt.Errorf("receive error: %v", err)
			}

			if err := handleMessage(msg); err != nil {
				logError(fmt.Sprintf("Error handling message: %v", err))
			}
		}
	}
}

// handleMessage unmarshals a message and checks for "EOSE" or parses relay list data.
func handleMessage(msg []byte) error {
	var response []interface{}
	if err := json.Unmarshal(msg, &response); err != nil {
		return fmt.Errorf("unmarshal error: %v", err)
	}

	// Check if the message indicates "EOSE" (End of Stream).
	if len(response) > 0 && response[0] == "EOSE" {
		return nil // EOSE received, successfully end.
	}

	// Otherwise, parse relay list.
	return parseRelayList(msg)
}

// logError logs error messages (could be sent to a logging channel or external system).
func logError(message string) {
	// In this example, we'll just print to the console.
	// You can replace this with sending to a logging channel or external system.
	fmt.Println(message)
}

// parseRelayList parses relay URLs from kind 10002 messages
func parseRelayList(message []byte) error {
	var response []interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return fmt.Errorf("failed to parse message: %v", err)
	}

	// Expect the message to have at least 3 elements and be an "EVENT"
	if len(response) < 3 || response[0] != "EVENT" {
		return nil // Not an event message or insufficient data
	}

	// Extract event data, must be a map
	eventData, ok := response[2].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid event data format")
	}

	// Extract "tags" from event data
	tags, ok := eventData["tags"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid tags format")
	}

	// Collect all valid relay URLs
	var relayURLs []string
	for _, tag := range tags {
		if tagArr, ok := tag.([]interface{}); ok && len(tagArr) >= 2 && tagArr[0] == "r" {
			// The second element must be the relay URL
			if relayURL, ok := tagArr[1].(string); ok {
				relayURLs = append(relayURLs, relayURL)
			}
		}
	}

	// Lock the global mutex only when modifying shared state
	mu.Lock()
	defer mu.Unlock()

	for _, relayURL := range relayURLs {
		classifyRelay(relayURL) // Classify each relay URL
	}

	return nil
}

// classifyRelay categorizes the relay URL into the appropriate list
func classifyRelay(relayURL string) {
	normalizedURL := normalizeURL(relayURL)

	if isMalformedRelay(normalizedURL) {
		malformed[normalizedURL]++
	} else if isLocalRelay(normalizedURL) {
		local[normalizedURL]++
	} else if isOnionRelay(normalizedURL) {
		onion[normalizedURL]++
	} else if isAPIRelay(normalizedURL) {
		clearAPI[normalizedURL]++
	} else {
		clearOnline[normalizedURL]++
	}
}

// crawlClearOnlineRelays crawls the relays from the clearOnline list concurrently
func crawlClearOnlineRelays(concurrency int) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	mu.Lock()
	relays := make([]string, 0, len(clearOnline))
	for relay := range clearOnline {
		if !crawledRelays[relay] {
			relays = append(relays, relay)
		}
	}
	mu.Unlock()

	for _, relay := range relays {
		wg.Add(1)
		sem <- struct{}{} // Block when reaching concurrency limit

		go func(r string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore after task

			for i := 0; i < maxTries; i++ {
				err := attemptCrawl(r)
				if err != nil {
					logChannel <- fmt.Sprintf("Failed to crawl relay %s: %v", r, err)

					mu.Lock()
					clearOffline[r] = clearOnline[r] // Mark as offline after failure
					delete(clearOnline, r)           // Remove from online list
					crawledRelays[r] = true          // Mark it as crawled
					mu.Unlock()

					time.Sleep(backoffDuration) // Apply backoff between retries

				} else {
					logChannel <- fmt.Sprintf("Successfully crawled relay: %s", r)

					mu.Lock()
					crawledRelays[r] = true // Mark it as crawled after success
					mu.Unlock()
					break
				}
			}
		}(relay)
	}

	wg.Wait() // Wait for all goroutines to finish
}

// attemptCrawl handles the crawl attempt and returns an error if unsuccessful
func attemptCrawl(relayURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), crawlTimeout)
	defer cancel()

	wsConfig, err := websocket.NewConfig(relayURL, "http://localhost/")
	if err != nil {
		return fmt.Errorf("config error: %v", err)
	}

	ws, err := websocket.DialConfig(wsConfig)
	if err != nil {
		return fmt.Errorf("dial error: %v", err)
	}
	defer ws.Close()

	// Send REQ message
	subscriptionID := "crawlr"
	req := []interface{}{
		"REQ", subscriptionID, map[string]interface{}{
			"kinds": []int{10002},
			"limit": 100,
		},
	}

	err = websocket.JSON.Send(ws, req)
	if err != nil {
		return fmt.Errorf("failed to send REQ message: %v", err)
	}

	// Wait for response or timeout
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout: no response from relay")
	default:
		var msg []byte
		err := websocket.Message.Receive(ws, &msg)
		if err != nil {
			return fmt.Errorf("receive error: %v", err)
		}

		// Parse response
		var response []interface{}
		if err := json.Unmarshal(msg, &response); err != nil {
			return fmt.Errorf("failed to parse message: %v", err)
		}

		if len(response) > 0 && response[0] == "EOSE" {
			return nil // Successfully reached end of stream
		}

		// Handle any other messages or continue to parse...
	}

	return nil
}

// Logger that prints messages without affecting the status bar
func logRelayEvents() {
	for msg := range logChannel {
		// Move the cursor up to print above the status bar
		fmt.Printf("\033[F%s\n", msg)
	}
}
