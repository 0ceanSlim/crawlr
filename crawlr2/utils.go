package main

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"strings"
)

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