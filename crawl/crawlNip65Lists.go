package crawl

import (
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"crawlr/types"
)

var (
	Relays        = make(map[string]*types.RelayInfo)
	RelaysMutex   sync.RWMutex
	CrawledRelays = make(map[string]bool)
	CrawledMutex  sync.RWMutex
	OfflineRelays = make(map[string]bool)
	OfflineMutex  sync.RWMutex
)

const (
	maxRetries    = 2
	retryDelay    = 3 * time.Second
	timeout       = 2 * time.Second
	
)

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

func normalizeURL(url string) string {
	return strings.TrimSuffix(strings.ToLower(url), "/")
}

func markRelayOffline(relayURL string) {
	OfflineMutex.Lock()
	defer OfflineMutex.Unlock()
	OfflineRelays[relayURL] = true
	log.Printf("Relay marked as offline: %s", relayURL)
}

func CrawlRelay(relayURL string, discoveredBy string, maxDepth int) {
	if maxDepth <= 0 {
		return
	}

	normalizedRelayURL := normalizeURL(relayURL)

	// Check if the relay is offline
	OfflineMutex.RLock()
	if OfflineRelays[normalizedRelayURL] {
		OfflineMutex.RUnlock()
		log.Printf("Skipping offline relay: %s", normalizedRelayURL)
		return
	}
	OfflineMutex.RUnlock()

	if !strings.HasPrefix(relayURL, "wss://") {
		log.Printf("Ignoring relay (does not start with wss): %s", relayURL)
		return
	}

	if shouldExcludeRelay(relayURL) {
		log.Printf("Ignoring excluded relay: %s", relayURL)
		return
	}

	CrawledMutex.Lock()
	if CrawledRelays[normalizedRelayURL] {
		CrawledMutex.Unlock()
		return
	}
	CrawledRelays[normalizedRelayURL] = true
	CrawledMutex.Unlock()

	log.Printf("Connecting to relay: %s (Discovered by: %s)", normalizedRelayURL, discoveredBy)

	for retry := 0; retry < maxRetries; retry++ {
		err := connectAndFetch(normalizedRelayURL, discoveredBy)
		if err != nil {
			log.Printf("Error fetching from relay %s: %v", normalizedRelayURL, err)
			if retry < maxRetries-1 {
				log.Printf("Retrying in %v... (%d/%d)", retryDelay, retry+1, maxRetries)
				time.Sleep(retryDelay)
			}
		} else {
			return
		}
	}

	log.Printf("Max retries reached for relay: %s", normalizedRelayURL)
	markRelayOffline(normalizedRelayURL)
}