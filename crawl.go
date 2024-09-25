package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"golang.org/x/net/websocket"
)

// ReqKind10002 sends a REQ message for kind 10002 events and parses relay URLs
func ReqKind10002(relayURL string) error {
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
		var msg []byte
		err = websocket.Message.Receive(ws, &msg)
		if err != nil {
			if err == io.EOF {
				return nil // Connection closed normally
			}
			return fmt.Errorf("receive error: %v", err)
		}
		fmt.Printf("Received message: %s\n", string(msg)) // Debugging

		// Parse the message to check if it's an EOSE message
		var response []interface{}
		if err := json.Unmarshal(msg, &response); err != nil {
			fmt.Printf("Error parsing message: %v\n", err)
			continue
		}

		if len(response) > 0 && response[0] == "EOSE" {
			fmt.Println("Received EOSE, stopping reception from this relay.")
			break
		}

		if err := parseRelayList(msg); err != nil {
			fmt.Printf("Error parsing relay list: %v\n", err)
		}
	}
	return nil
}

// parseRelayList parses relay URLs from kind 10002 messages
func parseRelayList(message []byte) error {
	var response []interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return fmt.Errorf("failed to parse message: %v", err)
	}

	// Ensure it's an "EVENT" message
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

	// Lock once for all relay classifications
	mu.Lock()
	defer mu.Unlock()
	for _, relayURL := range relayURLs {
		classifyRelay(relayURL)
	}

	return nil
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
					fmt.Printf("Failed to crawl relay %s: %v\n", r, err)

					mu.Lock()
					clearOffline[r] = clearOnline[r]
					delete(clearOnline, r)
					mu.Unlock()
				} else {
					mu.Lock()
					crawledRelays[r] = true
					mu.Unlock()
					break
				}
			}
		}(relay)
	}

	wg.Wait()
}