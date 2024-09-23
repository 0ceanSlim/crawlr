package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
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

// Relay lists
var (
	clearOnline  = make(map[string]int)
	clearOffline = make(map[string]int)
	onion        = make(map[string]int)
	local        = make(map[string]int)
	malformed    = make(map[string]int)
)

// Max tries for a relay before moving to the offline list
const maxTries = 2

// WebSocket timeout in seconds
const wsTimeout = 2

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

	// Set a timeout for receiving messages
	ws.SetDeadline(time.Now().Add(wsTimeout * time.Second))

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

	// Receive and parse a single message
	var msg []byte
	err = websocket.Message.Receive(ws, &msg)
	if err != nil {
		return fmt.Errorf("receive error: %v", err)
	}
	fmt.Printf("Received message: %s\n", string(msg)) // Debugging

	if err := parseRelayList(msg); err != nil {
		return err
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

	for _, tag := range tags {
		tagArr, ok := tag.([]interface{})
		if !ok || len(tagArr) < 2 || tagArr[0] != "r" {
			continue
		}

		relayURL, ok := tagArr[1].(string)
		if !ok {
			continue
		}

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

// Crawl the relays from the clearOnline list
func crawlClearOnlineRelays() {
	for relay := range clearOnline {
		for i := 0; i < maxTries; i++ {
			err := ReqKind10002(relay)
			if err != nil {
				fmt.Printf("Failed to crawl relay %s: %v\n", relay, err)
				clearOffline[relay] = clearOnline[relay]
				delete(clearOnline, relay)
			} else {
				break
			}
		}
	}
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
	// Create a channel to capture OS signals
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt, syscall.SIGTERM)

	// Run the crawling process in a separate goroutine
	go func() {
		initialRelay := "wss://nos.lol"

		for {
			// Start crawl from initial relay
			err := ReqKind10002(initialRelay)
			if err != nil {
				fmt.Printf("Initial crawl failed: %v\n", err)
			}

			// Crawl discovered clearOnline relays
			crawlClearOnlineRelays()

			// Sleep for a while before retrying (e.g., 30 seconds)
			time.Sleep(2 * time.Second)

			// Check if there are any more relays left to crawl
			if len(clearOnline) == 0 {
				fmt.Println("No more relays to crawl. Exiting...")
				break
			}
		}

		// Finalize and write CSVs after crawling is complete
		finalize()
	}()

	// Wait for an exit signal (Ctrl+C or kill)
	<-exitSignal

	// Finalize and write CSVs when the program is interrupted
	fmt.Println("\nReceived exit signal, writing logs and exiting...")
	finalize()
}
