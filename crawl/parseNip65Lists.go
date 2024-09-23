package crawl

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"

	"crawlr/types"
)

func parseRelayList(message []byte, initiatingRelay string) error {
	var response []interface{}
	if err := json.Unmarshal(message, &response); err != nil {
		return err
	}

	if len(response) < 3 || response[0] != "EVENT" {
		return nil // Not an event message, ignore
	}

	eventData, ok := response[2].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid event data format")
	}

	tags, ok := eventData["tags"].([]interface{})
	if !ok {
		return fmt.Errorf("invalid tags format")
	}

	var wsCount, wssCount, onionCount int

	for _, tag := range tags {
		tagArr, ok := tag.([]interface{})
		if !ok || len(tagArr) < 2 || tagArr[0] != "r" {
			continue
		}

		relayURL, ok := tagArr[1].(string)
		if !ok {
			continue
		}

		// Count .onion sites
		if strings.HasSuffix(relayURL, ".onion") {
			onionCount++
			continue
		}

		// Only process ws:// or wss:// URLs
		if !strings.HasPrefix(relayURL, "ws://") && !strings.HasPrefix(relayURL, "wss://") {
			continue
		}

		// Remove anything after the high-level domain
		parsedURL, err := url.Parse(relayURL)
		if err != nil {
			continue
		}
		relayURL = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Hostname())

		// Count ws and wss relays
		if strings.HasPrefix(relayURL, "ws://") {
			wsCount++
		} else if strings.HasPrefix(relayURL, "wss://") {
			wssCount++
		}

		RelaysMutex.Lock()
		relayInfo, exists := Relays[relayURL]
		if exists {
			relayInfo.Count++
		} else {
			relayInfo = &types.RelayInfo{
				URL:          relayURL,
				Count:        1,
				DiscoveredBy: initiatingRelay,
			}
			Relays[relayURL] = relayInfo
		}
		RelaysMutex.Unlock()

		// Check if we've already crawled this relay
		CrawledMutex.Lock()
		alreadyCrawled := CrawledRelays[relayURL]
		CrawledMutex.Unlock()

		if !alreadyCrawled {
			go CrawlRelay(relayURL, initiatingRelay, 2)
		}

		log.Printf("Discovered relay: %s (Discovered by: %s, Count: %d)", relayURL, initiatingRelay, relayInfo.Count)
	}

	// Log the counts
	log.Printf("Relay counts - WS: %d, WSS: %d, Onion: %d", wsCount, wssCount, onionCount)

	return nil
}