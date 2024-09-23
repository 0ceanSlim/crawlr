// miain.go
package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"sync"
	"time"

	"crawlr/crawl"
	"crawlr/types"
)

const (
	logFolder        = "logs/"
	onlineRelayFile  = logFolder + "online_relays.csv"
	offlineRelayFile = logFolder + "offline_relays.csv"
	localRelayFile   = logFolder + "local_relays.csv"
	onionRelayFile   = logFolder + "onion_relays.csv"
)

func initLogging() {
	if _, err := os.Stat(logFolder); os.IsNotExist(err) {
		if err := os.Mkdir(logFolder, os.ModePerm); err != nil {
			log.Fatalf("Failed to create log directory: %v", err)
		}
	}
}

func writeToCSV(filename string, relays []*types.RelayInfo) {
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create CSV file %s: %v", filename, err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"Relay URL", "Count", "Discovered By"})

	for _, relay := range relays {
		writer.Write([]string{relay.URL, fmt.Sprintf("%d", relay.Count), relay.DiscoveredBy})
	}

	log.Printf("Data written to CSV file: %s", filename)
}

func categorizeRelays() (online, offline, local, onion []*types.RelayInfo) {
	crawl.RelaysMutex.RLock()
	defer crawl.RelaysMutex.RUnlock()

	for _, relay := range crawl.Relays {
		if crawl.IsLocalRelay(relay.URL) {
			local = append(local, relay)
		} else if crawl.IsOnionRelay(relay.URL) {
			onion = append(onion, relay)
		} else {
			crawl.CrawledMutex.RLock()
			isCrawled := crawl.CrawledRelays[relay.URL]
			crawl.CrawledMutex.RUnlock()

			if isCrawled {
				online = append(online, relay)
			} else {
				offline = append(offline, relay)
			}
		}
	}

	return
}

func sortRelays(relays []*types.RelayInfo) {
	sort.Slice(relays, func(i, j int) bool {
		return relays[i].Count > relays[j].Count
	})
}

func writeAllCSVs() {
	online, offline, local, onion := categorizeRelays()

	sortRelays(online)
	sortRelays(offline)
	sortRelays(local)
	sortRelays(onion)

	writeToCSV(onlineRelayFile, online)
	writeToCSV(offlineRelayFile, offline)
	writeToCSV(localRelayFile, local)
	writeToCSV(onionRelayFile, onion)
}

func allOnlineRelaysCrawled() bool {
	crawl.RelaysMutex.RLock()
	defer crawl.RelaysMutex.RUnlock()
	crawl.OfflineMutex.RLock()
	defer crawl.OfflineMutex.RUnlock()
	crawl.CrawledMutex.RLock()
	defer crawl.CrawledMutex.RUnlock()

	for url := range crawl.Relays {
		if !crawl.OfflineRelays[url] && !crawl.CrawledRelays[url] {
			return false
		}
	}
	return true
}

func readOnlineRelaysFromCSV() []string {
	file, err := os.Open(onlineRelayFile)
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

	var onlineRelays []string
	for _, record := range records[1:] { // Skip header
		if len(record) > 0 {
			onlineRelays = append(onlineRelays, record[0])
		}
	}
	return onlineRelays
}

func startCrawling(relays []string) {
	var wg sync.WaitGroup
	for _, relay := range relays {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			crawl.Init(r, r, 3)
		}(relay)
	}
	wg.Wait()
}

func main() {

	crawl.ResetRelayData()

	initLogging()

	crawlComplete := make(chan struct{})

	// Read online relays from CSV
	onlineRelays := readOnlineRelaysFromCSV()
	if len(onlineRelays) > 0 {
		log.Println("Starting crawl from online relays in CSV...")
		startCrawling(onlineRelays)
	} else {
		// If no online relays in CSV, start from a default relay
		startingRelay := "wss://nos.lol"
		log.Println("No online relays found in CSV. Starting from:", startingRelay)
		startCrawling([]string{startingRelay})
	}

	// Continuation Logic: Wait for crawl to complete
	go func() {
		for {
			if allOnlineRelaysCrawled() {
				close(crawlComplete)
				return
			}
			time.Sleep(1 * time.Second)
		}
	}()

	// Handle interrupt signal (Ctrl+C)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	select {
	case <-crawlComplete:
		log.Println("Crawl complete. All online relays have been contacted.")
	case <-interrupt:
		log.Println("Received interrupt signal.")
	}

	// Write the final results to CSV files
	log.Println("Writing final results to CSV files...")
	writeAllCSVs()
	log.Println("Final results written to CSV files in", logFolder)
}
