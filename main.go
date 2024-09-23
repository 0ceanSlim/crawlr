package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

type RelayInfo struct {
	URL          string
	Count        int
	DiscoveredBy string
	Offline      bool
}

var (
	relays        = make(map[string]*RelayInfo)
	relaysMutex   sync.RWMutex
	crawledRelays = make(map[string]bool)
	crawledMutex  sync.RWMutex
	offlineRelays = make(map[string]bool)
	offlineMutex  sync.RWMutex
)

const (
	maxRetries    = 2
	retryDelay    = 3 * time.Second
	logFolder     = "logs/"
	relayLogFile  = logFolder + "relays.csv"
	timeout       = 2 * time.Second
	writeInterval = 5 * time.Minute
)

func initLogging() {
	if _, err := os.Stat(logFolder); os.IsNotExist(err) {
		if err := os.Mkdir(logFolder, os.ModePerm); err != nil {
			log.Fatalf("Failed to create log directory: %v", err)
		}
	}
}

func normalizeURL(url string) string {
	return strings.TrimSuffix(strings.ToLower(url), "/")
}

func writeToCSV() {
	relaysMutex.RLock()
	defer relaysMutex.RUnlock()

	file, err := os.Create(relayLogFile)
	if err != nil {
		log.Printf("Failed to create CSV file: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"Relay URL", "Count", "Discovered By", "Offline"})

	var relayList []*RelayInfo
	for _, relay := range relays {
		relayList = append(relayList, relay)
	}

	sort.Slice(relayList, func(i, j int) bool {
		return relayList[i].Count > relayList[j].Count
	})

	for _, relay := range relayList {
		offlineMutex.RLock()
		offline := offlineRelays[relay.URL]
		offlineMutex.RUnlock()
		writer.Write([]string{relay.URL, fmt.Sprintf("%d", relay.Count), relay.DiscoveredBy, fmt.Sprintf("%v", offline)})
	}

	log.Println("Data written to CSV file")
}

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

func CrawlRelay(relayURL string, discoveredBy string, maxDepth int) {
	if maxDepth <= 0 {
		return
	}

	if !strings.HasPrefix(relayURL, "wss://") {
		log.Printf("Ignoring relay (does not start with wss): %s", relayURL)
		return
	}

	if shouldExcludeRelay(relayURL) {
		log.Printf("Ignoring excluded relay: %s", relayURL)
		return
	}

	normalizedRelayURL := normalizeURL(relayURL)

	crawledMutex.Lock()
	if crawledRelays[normalizedRelayURL] {
		crawledMutex.Unlock()
		return
	}
	crawledRelays[normalizedRelayURL] = true
	crawledMutex.Unlock()

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

func allOnlineRelaysCrawled() bool {
	relaysMutex.RLock()
	defer relaysMutex.RUnlock()
	offlineMutex.RLock()
	defer offlineMutex.RUnlock()
	crawledMutex.RLock()
	defer crawledMutex.RUnlock()

	for relayURL := range relays {
		if !offlineRelays[relayURL] && !crawledRelays[relayURL] {
			return false
		}
	}
	return true
}

func markRelayOffline(relayURL string) {
	offlineMutex.Lock()
	defer offlineMutex.Unlock()
	offlineRelays[relayURL] = true
}

func connectAndFetch(relayURL string, initiatingRelay string) error {
	config, err := websocket.NewConfig(relayURL, "http://localhost/")
	if err != nil {
		return fmt.Errorf("config error: %v", err)
	}

	ws, err := websocket.DialConfig(config)
	if err != nil {
		return fmt.Errorf("dial error: %v", err)
	}
	defer ws.Close()

	subscriptionID := "relay-crawl-1"
	req := []interface{}{
		"REQ", subscriptionID, map[string]interface{}{
			"kinds": []int{10002},
			"limit": 100,
		},
	}

	if err := websocket.JSON.Send(ws, req); err != nil {
		return fmt.Errorf("write error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var message []byte
			if err := websocket.Message.Receive(ws, &message); err != nil {
				log.Printf("Read error: %v", err)
				return
			}

			if err := parseRelayList(message, initiatingRelay); err != nil {
				log.Printf("Handle response error: %v", err)
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("Timeout from relay: %s", relayURL)
		return fmt.Errorf("timeout")
	}

	return nil
}

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

		relaysMutex.Lock()
		relayInfo, exists := relays[relayURL]
		if exists {
			relayInfo.Count++
		} else {
			relayInfo = &RelayInfo{
				URL:          relayURL,
				Count:        1,
				DiscoveredBy: initiatingRelay,
			}
			relays[relayURL] = relayInfo
		}
		relaysMutex.Unlock()

		// Check if we've already crawled this relay
		crawledMutex.Lock()
		alreadyCrawled := crawledRelays[relayURL]
		crawledMutex.Unlock()

		if !alreadyCrawled {
			go CrawlRelay(relayURL, initiatingRelay, 2)
		}

		log.Printf("Discovered relay: %s (Discovered by: %s, Count: %d)", relayURL, initiatingRelay, relayInfo.Count)
	}

	// Log the counts
	log.Printf("Relay counts - WS: %d, WSS: %d, Onion: %d", wsCount, wssCount, onionCount)

	return nil
}

func readRelaysFromCSV() []string {
	file, err := os.Open(relayLogFile)
	if err != nil {
		log.Printf("Could not open CSV file: %v", err)
		return nil
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		log.Printf("Could not read CSV file: %v", err)
		return nil
	}

	var relays []string
	for _, record := range records[1:] { // Skip header
		if len(record) > 0 {
			relays = append(relays, record[0]) // Use the relay URL
		}
	}
	return relays
}

func startPeriodicWrite() {
	ticker := time.NewTicker(writeInterval)
	go func() {
		for range ticker.C {
			writeToCSV()
		}
	}()
}

func main() {
	initLogging()
	startPeriodicWrite()

	var wg sync.WaitGroup
	crawlComplete := make(chan struct{})

	relaysFromCSV := readRelaysFromCSV()
	if len(relaysFromCSV) > 0 {
		log.Println("Starting crawl from relays in CSV...")
		for _, relay := range relaysFromCSV {
			wg.Add(1)
			go func(r string) {
				defer wg.Done()
				CrawlRelay(r, r, 3)
			}(relay)
		}
	} else {
		startingRelay := "wss://nos.lol"
		log.Println("No relays found in CSV. Starting from:", startingRelay)
		wg.Add(1)
		go func() {
			defer wg.Done()
			CrawlRelay(startingRelay, "starting-point", 3)
		}()
	}

	go func() {
		wg.Wait()
		for {
			if allOnlineRelaysCrawled() {
				close(crawlComplete)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	select {
	case <-crawlComplete:
		log.Println("Crawl complete. All online relays have been contacted.")
	case <-interrupt:
		log.Println("Received interrupt signal.")
	}

	log.Println("Writing final results to CSV...")
	writeToCSV()
	log.Println("Final results written to", relayLogFile)
}
