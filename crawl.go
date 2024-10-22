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

// ReqKind10002 sends a REQ message for kind 10002 events and parses relay URLs
func ReqKind10002(relayURL string) error {
	// Set a timeout for the entire operation (e.g., 10 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	config, err := websocket.NewConfig(relayURL, "http://localhost/")
	if err != nil {
		return fmt.Errorf("config error: %v", err)
	}

	ws, err := websocket.DialConfig(config)
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

	// Continuously receive messages until EOSE or connection closed
	for {
		select {
		case <-ctx.Done():
			// Timeout case
			return fmt.Errorf("timeout: no response from relay")
		default:
			var msg []byte
			err = websocket.Message.Receive(ws, &msg)
			if err != nil {
				if err == io.EOF {
					return nil // Connection closed normally
				}
				return fmt.Errorf("receive error: %v", err)
			}

			var response []interface{}
			if err := json.Unmarshal(msg, &response); err != nil {
				continue
			}

			if len(response) > 0 && response[0] == "EOSE" {
				return nil // EOSE received, return successfully here
			}

			if err := parseRelayList(msg); err != nil {
				logChannel <- fmt.Sprintf("Error parsing relay list: %v", err)
			}
		}
	}
}


// parseRelayList parses relay URLs from kind 10002 messages
func parseRelayList(message []byte) error {
	var response []interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return fmt.Errorf("failed to parse message: %v", err)
	}

	if len(response) < 3 || response[0] != "EVENT" {
		return nil
	}

	eventData, ok := response[2].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid event data format")
	}

	tags, ok := eventData["tags"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid tags format")
	}

	relayURLs := make([]string, 0)

	for _, tag := range tags {
		tagArr, ok := tag.([]interface{})
		if !ok || len(tagArr) < 2 || tagArr[0] != "r" {
			continue
		}

		relayURL, ok := tagArr[1].(string)
		if !ok {
			continue
		}
		relayURLs = append(relayURLs, relayURL)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, relayURL := range relayURLs {
		classifyRelay(relayURL)
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
			defer func() { <-sem }() // Release semaphore

			for i := 0; i < maxTries; i++ {
				err := ReqKind10002(r)
				if err != nil {
					logChannel <- fmt.Sprintf("Failed to crawl relay %s: %v", r, err)

					mu.Lock()
					clearOffline[r] = clearOnline[r]
					delete(clearOnline, r)
					mu.Unlock()
				} else {
					logChannel <- fmt.Sprintf("Successfully crawled relay: %s", r)

					mu.Lock()
					crawledRelays[r] = true
					mu.Unlock()
					break
				}

				// If we've exhausted all attempts, mark it as crawled even if it's offline
				if i == maxTries-1 {
					mu.Lock()
					crawledRelays[r] = true // Mark as crawled after max tries
					mu.Unlock()
				}
			}
		}(relay)
	}

	wg.Wait() // Wait for all goroutines to finish
}

// Logger that prints messages without affecting the status bar
func logRelayEvents() {
	for msg := range logChannel {
		// Move the cursor up to print above the status bar
		fmt.Printf("\033[F%s\n", msg)
	}
}