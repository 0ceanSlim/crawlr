package crawl

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"crawlr/types"
)

var (
	foundRelaysCount    int
	crawledRelaysCount  int
	offlineRelaysCount  int
	RemainingRelaysCount int
)

// Mutex for safely updating counts
var StatusMutex sync.Mutex

// Update the status in the terminal
func UpdateStatus() {
	StatusMutex.Lock()
	defer StatusMutex.Unlock()

	// Remaining relays should be the number of found relays minus the crawled relays
	RemainingRelaysCount = foundRelaysCount - crawledRelaysCount

	fmt.Printf("\rFound Relays: %d | Crawled Relays: %d | Offline Relays: %d | Remaining Relays: %d",
		foundRelaysCount, crawledRelaysCount, offlineRelaysCount, RemainingRelaysCount)
}

var (
	Relays        = make(map[string]*types.RelayInfo)
	RelaysMutex   sync.RWMutex
	CrawledRelays = make(map[string]bool)  // Tracks which relays have been crawled
	CrawledMutex  sync.RWMutex
	OfflineRelays = make(map[string]bool)  // Tracks which relays are offline
	OfflineMutex  sync.RWMutex
)

const (
	maxRetries    = 1
	retryDelay    = 2 * time.Second
	timeout       = 2 * time.Second
)

// Exclude certain relay URLs (local, .onion, etc.)
func shouldExcludeRelay(relayURL string) bool {
	parsedURL, err := url.Parse(relayURL)
	if err != nil {
		return true
	}
	hostname := parsedURL.Hostname()
	return strings.HasSuffix(hostname, ".onion") ||
		strings.HasSuffix(hostname, ".local") ||
		strings.HasPrefix(hostname, "192.168.") ||
		strings.HasPrefix(hostname, "127.0.0.")
}

// Normalize URL by removing trailing slashes and lowercasing
func normalizeURL(url string) string {
	return strings.TrimSuffix(strings.ToLower(url), "/")
}

// Mark relay as offline
func markRelayOffline(relayURL string) {
	OfflineMutex.Lock()
	defer OfflineMutex.Unlock()
	OfflineRelays[relayURL] = true
	log.Printf("Relay marked as offline: %s", relayURL)
}

// Crawl a relay URL with retry logic
func Init(relayURL string, discoveredBy string, maxDepth int) {
	if maxDepth <= 0 {
		return
	}

	normalizedRelayURL := normalizeURL(relayURL)

	// Check if the relay should be excluded
	if shouldExcludeRelay(normalizedRelayURL) {
		markRelayOffline(normalizedRelayURL)
		StatusMutex.Lock()
		offlineRelaysCount++ // Increment the offline count for excluded relays
		StatusMutex.Unlock()
		UpdateStatus()
		return
	}

	// Check if the relay is offline
	OfflineMutex.RLock()
	if OfflineRelays[normalizedRelayURL] {
		OfflineMutex.RUnlock()
		return
	}
	OfflineMutex.RUnlock()

	CrawledMutex.Lock()
	if CrawledRelays[normalizedRelayURL] {
		CrawledMutex.Unlock()
		return
	}
	CrawledRelays[normalizedRelayURL] = true
	crawledRelaysCount++ // Increment the counter for crawled relays
	CrawledMutex.Unlock()

	// Retry logic
	for retry := 0; retry < maxRetries; retry++ {
		err := Fetch(normalizedRelayURL, discoveredBy)
		if err != nil {
			if retry < maxRetries-1 {
				time.Sleep(retryDelay)
			}
		} else {
			UpdateStatus() // Successfully crawled
			return
		}
	}

	// Mark as offline after all retries
	markRelayOffline(normalizedRelayURL)

	StatusMutex.Lock()
	offlineRelaysCount++ // Increment the counter for offline relays after retries
	StatusMutex.Unlock()

	UpdateStatus()
}