package crawl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"crawlr/types"
)

func Parse(message []byte, initiatingRelay string) error {
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

	for _, tag := range tags {
		tagArr, ok := tag.([]interface{})
		if !ok || len(tagArr) < 2 || tagArr[0] != "r" {
			continue
		}

		relayURL, ok := tagArr[1].(string)
		if !ok {
			continue
		}

		// Check if the relay should be excluded
		if shouldExcludeRelay(relayURL) {
			markRelayOffline(relayURL)
			StatusMutex.Lock()
			offlineRelaysCount++ // Increment offline for excluded relays
			StatusMutex.Unlock()
			UpdateStatus()
			continue // Skip excluded relays
		}

		// Skip non ws:// or wss:// protocols
		if !strings.HasPrefix(relayURL, "ws://") && !strings.HasPrefix(relayURL, "wss://") {
			continue
		}

		parsedURL, err := url.Parse(relayURL)
		if err != nil {
			continue
		}
		relayURL = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Hostname())

		RelaysMutex.Lock()
		_, exists := Relays[relayURL]
		if !exists {
			// Increment found relays only when a new relay is found
			foundRelaysCount++
			Relays[relayURL] = &types.RelayInfo{
				URL:          relayURL,
				Count:        1,
				DiscoveredBy: initiatingRelay,
			}
		} else {
			Relays[relayURL].Count++
		}
		RelaysMutex.Unlock()

		UpdateStatus()

		// Check if the relay has already been crawled
		CrawledMutex.Lock()
		alreadyCrawled := CrawledRelays[relayURL]
		CrawledMutex.Unlock()

		// If not crawled, attempt to crawl it
		if !alreadyCrawled {
			go Init(relayURL, initiatingRelay, 2) 
		}
	}

	return nil
}