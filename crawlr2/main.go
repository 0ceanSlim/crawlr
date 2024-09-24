package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/websocket"
)

// Relay categories
type RelayCategory string

const (
	ClearOnline  RelayCategory = "clear_online"
	ClearOffline RelayCategory = "clear_offline"
	Onion        RelayCategory = "onion"
	Local        RelayCategory = "local"
	Malformed    RelayCategory = "malformed"
)

// Relay lists with mutex protection
var (
	mu            sync.Mutex
	clearOnline   = make(map[string]int)
	clearOffline  = make(map[string]int)
	onion         = make(map[string]int)
	local         = make(map[string]int)
	malformed     = make(map[string]int)
	crawledRelays = make(map[string]bool)
)

// Max tries for a relay before moving to the offline list
const maxTries = 2

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

// classifyRelay categorizes the relay URL into the appropriate list
func classifyRelay(relayURL string) {
	if isMalformedRelay(relayURL) {
		malformed[relayURL]++
	} else if isLocalRelay(relayURL) {
		local[relayURL]++
	} else if isOnionRelay(relayURL) {
		onion[relayURL]++
	} else {
		clearOnline[relayURL]++
	}
}

// Helper functions to categorize relays
func isMalformedRelay(url string) bool {
	return !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://")
}

func isLocalRelay(url string) bool {
	host := extractHost(url)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() || isReservedIP(ip)
}

func isOnionRelay(url string) bool {
	return strings.HasSuffix(extractHost(url), ".onion")
}

// extractHost extracts the host part from the URL
func extractHost(url string) string {
	parts := strings.Split(url, "/")
	if len(parts) >= 3 {
		return parts[2]
	}
	return ""
}

// isReservedIP checks if the IP address is in a reserved range
func isReservedIP(ip net.IP) bool {
	privateBlocks := []*net.IPNet{
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},
	}
	for _, block := range privateBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
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

// Export discovered relays to CSV
func exportToCSV(category RelayCategory, relayList map[string]int) {
	// Ensure logs directory exists
	if err := os.MkdirAll("logs", os.ModePerm); err != nil {
		fmt.Printf("Failed to create logs directory: %v\n", err)
		return
	}

	file, err := os.Create(fmt.Sprintf("logs/%s_relays.csv", category))
	if err != nil {
		fmt.Printf("Failed to create CSV file for %s: %v\n", category, err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	for relay, count := range relayList {
		err := writer.Write([]string{relay, fmt.Sprintf("%d", count)})
		if err != nil {
			fmt.Printf("Failed to write relay %s to CSV: %v\n", relay, err)
		}
	}
}

// On program exit, write CSVs and print results for debugging
func finalize() {
	exportToCSV(ClearOnline, clearOnline)
	exportToCSV(ClearOffline, clearOffline)
	exportToCSV(Onion, onion)
	exportToCSV(Local, local)
	exportToCSV(Malformed, malformed)
}

func main() {
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt, syscall.SIGTERM)

	go func() {
		initialRelay := "wss://nos.lol"
		concurrency := 10 // Adjust this value based on your needs and system capabilities

		for {
			err := ReqKind10002(initialRelay)
			if err != nil {
				fmt.Printf("Initial crawl failed: %v\n", err)
			}

			crawlClearOnlineRelays(concurrency)

			mu.Lock()
			fmt.Printf("Discovered relays: %d\n", len(clearOnline))
			remainingRelays := len(clearOnline) - len(crawledRelays)
			mu.Unlock()

			if remainingRelays == 0 {
				fmt.Println("No more relays to crawl. Waiting for new relays...")
				time.Sleep(30 * time.Second) // Wait before retrying
				continue
			}

			time.Sleep(2 * time.Second)
		}
	}()

	// Wait for an exit signal (Ctrl+C or kill)
	<-exitSignal

	fmt.Println("\nReceived exit signal, writing logs and exiting...")
	finalize()
}
