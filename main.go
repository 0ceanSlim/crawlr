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

	return crawl.RemainingRelaysCount == 0
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

    var onlineRelays []string
    for _, record := range records[1:] { // Skip header
        if len(record) > 0 {
            relayURL := record[0]

            // Check if the relay is offline
            crawl.OfflineMutex.RLock()
            offline := crawl.OfflineRelays[relayURL]
            crawl.OfflineMutex.RUnlock()

            // Only add online relays
            if !offline {
                onlineRelays = append(onlineRelays, relayURL)
            }
        }
    }
    return onlineRelays
}


func main() {
    initLogging()

    var wg sync.WaitGroup
    crawlComplete := make(chan struct{})

    // Read only online relays from CSV
    relaysFromCSV := readRelaysFromCSV()
    if len(relaysFromCSV) > 0 {
        log.Println("Starting crawl from online relays in CSV...")
        for _, relay := range relaysFromCSV {
            wg.Add(1)
            go func(r string) {
                defer wg.Done()
                crawl.Init(r, r, 3)
            }(relay)
        }
    } else {
        // If no online relays in CSV, start from a default relay
        startingRelay := "wss://nos.lol"
        log.Println("No online relays found in CSV. Starting from:", startingRelay)
        wg.Add(1)
        go func() {
            defer wg.Done()
            crawl.Init(startingRelay, "starting-point", 3)
        }()
    }

    // Wait for all crawls to complete
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

    // Handle interrupt signal (Ctrl+C)
    interrupt := make(chan os.Signal, 1)
    signal.Notify(interrupt, os.Interrupt)

    select {
    case <-crawlComplete:
        log.Println("Crawl complete. All online relays have been contacted.")
    case <-interrupt:
        log.Println("Received interrupt signal.")
    }

    // Write the final results to CSV
    log.Println("Writing final results to CSV...")
    writeToCSV()
    log.Println("Final results written to", relayLogFile)
}
