package main

import (
	"encoding/csv"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
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
	} else if isAPIRelay(normalizedURL) {
		clearAPI[normalizedURL]++
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
	// Check if the URL starts with a quote or doesn't start with "ws://" or "wss://"
	if strings.HasPrefix(urlStr, `"`) || (!strings.HasPrefix(urlStr, "ws://") && !strings.HasPrefix(urlStr, "wss://")) {
		return true
	}

	// Attempt to parse the URL to check if it's valid
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return true
	}

	// Extract the host part (without the port)
	host := parsedURL.Hostname()

	// Ensure the host has a valid TLD (e.g., ".com", ".net")
	// Use a regular expression to check that the TLD has at least two alphabetic characters
	tldPattern := `\.[a-zA-Z]{2,}$`
	match, _ := regexp.MatchString(tldPattern, host)

	// Simplified return statement
	return !match // If no valid TLD, return true (malformed), else return false
}

// isLocalRelay checks if the URL contains a private/local IP or ends with .local
func isLocalRelay(urlStr string) bool {
	host := extractHost(urlStr)
	ip := net.ParseIP(host)

	// Check if the host ends with ".local", ignoring the port
	if strings.HasSuffix(host, ".local") {
		return true
	}

	// Check if the IP is loopback or within the reserved ranges
	if ip != nil && (ip.IsLoopback() || isReservedIP(ip)) {
		return true
	}

	return false
}

// isOnionRelay checks if the URL points to a .onion address, including cases with ports
func isOnionRelay(urlStr string) bool {
	host := extractHost(urlStr)
	// Check if the host ends with ".onion", ignoring the port
	return strings.HasSuffix(host, ".onion")
}

// isAPIRelay checks if the URL contains a path, indicating an API/filtered call
func isAPIRelay(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return u.Path != "" && u.Path != "/"
}

func extractHost(urlStr string) string {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return ""
	}

	// Hostname without port
	return parsedURL.Hostname()
}

// isReservedIP checks if the IP address is in a reserved range as defined by IETF and IANA
func isReservedIP(ip net.IP) bool {
	// Define all reserved IP ranges
	reservedBlocks := []*net.IPNet{
		// RFC1918: Private-use IP addresses
		{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(8, 32)},
		{IP: net.IPv4(172, 16, 0, 0), Mask: net.CIDRMask(12, 32)},
		{IP: net.IPv4(192, 168, 0, 0), Mask: net.CIDRMask(16, 32)},

		// RFC6598: Carrier-Grade NAT (CGN)
		{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},

		// RFC1122: Loopback addresses
		{IP: net.IPv4(127, 0, 0, 0), Mask: net.CIDRMask(8, 32)},

		// RFC3927: Link-local addresses
		{IP: net.IPv4(169, 254, 0, 0), Mask: net.CIDRMask(16, 32)},

		// RFC5737: Documentation IPs for examples and tests
		{IP: net.IPv4(192, 0, 2, 0), Mask: net.CIDRMask(24, 32)},    // TEST-NET-1
		{IP: net.IPv4(198, 51, 100, 0), Mask: net.CIDRMask(24, 32)}, // TEST-NET-2
		{IP: net.IPv4(203, 0, 113, 0), Mask: net.CIDRMask(24, 32)},  // TEST-NET-3

		// Multicast IP range
		{IP: net.IPv4(224, 0, 0, 0), Mask: net.CIDRMask(4, 32)},

		// Reserved for future use
		{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)},

		// Broadcast address
		{IP: net.IPv4(255, 255, 255, 255), Mask: net.CIDRMask(32, 32)},
	}

	// Check if the IP falls within any of the reserved ranges
	for _, block := range reservedBlocks {
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
	exportToCSV(ClearAPI, clearAPI)
	exportToCSV(Onion, onion)
	exportToCSV(Local, local)
	exportToCSV(Malformed, malformed)
}
