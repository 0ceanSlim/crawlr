package main

import (
	"encoding/csv"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// classifyRelay categorizes the relay URL into the appropriate list
func classifyRelay(relayURL string) {
	// Normalize the URL to ensure consistent categorization
	normalizedURL := normalizeURL(relayURL)

	if isMalformedRelay(normalizedURL) {
		malformed[normalizedURL]++
	} else if isLocalRelay(normalizedURL) {
		local[normalizedURL]++
	} else if isOnionRelay(normalizedURL) {
		onion[normalizedURL]++
	} else {
		clearOnline[normalizedURL]++
	}
}

// normalizeURL strips trailing slashes and converts the URL to lowercase for comparison
func normalizeURL(url string) string {
	url = strings.TrimRight(url, "/")
	return strings.ToLower(url)
}

// isMalformedRelay checks if the URL is malformed
func isMalformedRelay(urlStr string) bool {
	// Check if the URL starts with a quote or other invalid character
	if strings.HasPrefix(urlStr, `"`) || (!strings.HasPrefix(urlStr, "ws://") && !strings.HasPrefix(urlStr, "wss://")) {
		return true
	}

	// Attempt to parse the URL to check if it's valid
	_, err := url.Parse(urlStr)
	return err != nil
}

// isLocalRelay checks if the URL contains a private/local IP
func isLocalRelay(urlStr string) bool {
	host := extractHost(urlStr)
	ip := net.ParseIP(host)

	if ip == nil {
		return false
	}

	// Check if the IP is loopback or within the reserved ranges
	return ip.IsLoopback() || isReservedIP(ip)
}

// isOnionRelay checks if the URL points to a .onion address
func isOnionRelay(urlStr string) bool {
	host := extractHost(urlStr)
	return strings.HasSuffix(host, ".onion")
}

// extractHost extracts the host part from the URL
func extractHost(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}
	return parsedURL.Hostname()
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
