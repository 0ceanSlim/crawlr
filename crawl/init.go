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

// Mutex for safely updating counts and relays
var (
	StatusMutex    sync.Mutex
	RelaysMutex    sync.RWMutex
	CrawledMutex   sync.RWMutex
	OfflineMutex   sync.RWMutex
)

// Relay storage
var (
	Relays        = make(map[string]*types.RelayInfo) // Stores all relays discovered
	CrawledRelays = make(map[string]bool)             // Tracks which relays have been crawled
	OfflineRelays = make(map[string]bool)             // Tracks which relays are offline
)

const (
	maxRetries = 1
	retryDelay = 2 * time.Second
	timeout    = 2 * time.Second
)

// Clear all relay data at the start of the program to avoid stale data
func ResetRelayData() {
    RelaysMutex.Lock()
    defer RelaysMutex.Unlock()

    CrawledMutex.Lock()
    defer CrawledMutex.Unlock()

    OfflineMutex.Lock()
    defer OfflineMutex.Unlock()

    Relays = make(map[string]*types.RelayInfo)
    CrawledRelays = make(map[string]bool)
    OfflineRelays = make(map[string]bool)

    foundRelaysCount = 0
    crawledRelaysCount = 0
    offlineRelaysCount = 0
    RemainingRelaysCount = 0

    log.Println("Relay data reset successfully.")
}

func IsLocalRelay(relayURL string) bool {
	parsedURL, err := url.Parse(relayURL)
	if err != nil {
		return false
	}
	hostname := parsedURL.Hostname()
	return strings.HasSuffix(hostname, ".local") ||
		strings.HasPrefix(hostname, "192.168.") ||
		strings.HasPrefix(hostname, "127.0.0.") ||
		strings.HasPrefix(hostname, "localhost")
}

func IsOnionRelay(relayURL string) bool {
	parsedURL, err := url.Parse(relayURL)
	if err != nil {
		return false
	}
	return strings.Contains(parsedURL.Hostname(), ".onion")
}

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

// Initialize the crawl process
func Init(relayURL string, discoveredBy string, maxDepth int) {
	if maxDepth <= 0 {
		return
	}

	normalizedRelayURL := normalizeURL(relayURL)

	// Check if the relay should be excluded
	if shouldExcludeRelay(normalizedRelayURL) {
		markRelayOffline(normalizedRelayURL)
		return
	}

	// Check if the relay has already been crawled
	CrawledMutex.Lock()
	if CrawledRelays[normalizedRelayURL] {
		CrawledMutex.Unlock()
		return
	}
	CrawledRelays[normalizedRelayURL] = true // Mark as crawled
	crawledRelaysCount++                     // Increment crawled count
	CrawledMutex.Unlock()

	// Retry mechanism for fetching the relay data
	success := false
	for retry := 0; retry < maxRetries; retry++ {
		err := Fetch(normalizedRelayURL, discoveredBy) // Fetch relay data
		if err != nil {
			log.Printf("Failed to fetch relay: %s (attempt %d/%d)", relayURL, retry+1, maxRetries)
			time.Sleep(retryDelay) // Retry after delay
		} else {
			success = true // Relay fetched successfully
			break
		}
	}

	// Mark relay offline if all retries fail
	if !success {
		markRelayOffline(normalizedRelayURL)
		offlineRelaysCount++ // Increment offline count
	}

	// Update the status after each crawl
	UpdateStatus()
}

// Continue crawling based on uncrawled online relays
func ContinueCrawl() {
	for {
		RelaysMutex.RLock()
		remainingRelays := getUncrawledOnlineRelays()
		RelaysMutex.RUnlock()

		// If no uncrawled relays left, exit
		if len(remainingRelays) == 0 {
			log.Println("No remaining online relays to crawl.")
			break
		}

		for _, relay := range remainingRelays {
			go Init(relay.URL, relay.DiscoveredBy, 3)
		}
		time.Sleep(1 * time.Second) // Allow time for the next set of crawls
	}
}

// Helper function to get uncrawled online relays
func getUncrawledOnlineRelays() []*types.RelayInfo {
	var uncrawled []*types.RelayInfo
	for url, relay := range Relays {
		// Skip if already crawled or should be excluded
		CrawledMutex.RLock()
		alreadyCrawled := CrawledRelays[url]
		CrawledMutex.RUnlock()

		if !alreadyCrawled && !shouldExcludeRelay(url) {
			uncrawled = append(uncrawled, relay)
		}
	}
	return uncrawled
}

// Update the status in the terminal
func UpdateStatus() {
	StatusMutex.Lock()
	defer StatusMutex.Unlock()

	// Remaining relays should be the number of found relays minus the crawled relays
	RemainingRelaysCount = foundRelaysCount - crawledRelaysCount

	fmt.Printf("\rFound Relays: %d | Crawled Relays: %d | Offline Relays: %d | Remaining Relays: %d",
		foundRelaysCount, crawledRelaysCount, offlineRelaysCount, RemainingRelaysCount)
}