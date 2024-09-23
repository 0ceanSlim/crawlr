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


const(
	logFolder     = "logs/"
	relayLogFile  = logFolder + "relays.csv"
	writeInterval = 5 * time.Minute
)

func initLogging() {
	if _, err := os.Stat(logFolder); os.IsNotExist(err) {
		if err := os.Mkdir(logFolder, os.ModePerm); err != nil {
			log.Fatalf("Failed to create log directory: %v", err)
		}
	}
}

func writeToCSV() {
	crawl.RelaysMutex.RLock()
	defer crawl.RelaysMutex.RUnlock()

	file, err := os.Create(relayLogFile)
	if err != nil {
		log.Printf("Failed to create CSV file: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"Relay URL", "Count", "Discovered By", "Offline"})

	var relayList []*types.RelayInfo
	for _, relay := range crawl.Relays {
		relayList = append(relayList, relay)
	}

	sort.Slice(relayList, func(i, j int) bool {
		return relayList[i].Count > relayList[j].Count
	})

	for _, relay := range relayList {
		crawl.OfflineMutex.RLock()
		offline := crawl.OfflineRelays[relay.URL]
		crawl.OfflineMutex.RUnlock()
		writer.Write([]string{relay.URL, fmt.Sprintf("%d", relay.Count), relay.DiscoveredBy, fmt.Sprintf("%v", offline)})
	}

	log.Println("Data written to CSV file")
}

func allOnlineRelaysCrawled() bool {
	crawl.RelaysMutex.RLock()
	defer crawl.RelaysMutex.RUnlock()
	crawl.OfflineMutex.RLock()
	defer crawl.OfflineMutex.RUnlock()
	crawl.CrawledMutex.RLock()
	defer crawl.CrawledMutex.RUnlock()

	for relayURL := range crawl.Relays {
		if !crawl.OfflineRelays[relayURL] && !crawl.CrawledRelays[relayURL] {
			return false
		}
	}
	return true
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
				crawl.CrawlRelay(r, r, 3)
			}(relay)
		}
	} else {
		startingRelay := "wss://nos.lol"
		log.Println("No relays found in CSV. Starting from:", startingRelay)
		wg.Add(1)
		go func() {
			defer wg.Done()
			crawl.CrawlRelay(startingRelay, "starting-point", 3)
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
