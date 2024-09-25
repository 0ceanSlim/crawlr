package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt, syscall.SIGTERM)

	go func() {
		initialRelay := "wss://nos.lol"
		concurrency := 50 // Adjust this value based on your needs and system capabilities

		for {
			err := ReqKind10002(initialRelay)
			if err != nil {
				fmt.Printf("Initial crawl failed: %v\n", err)
			}

			crawlClearOnlineRelays(concurrency)

			mu.Lock()
			fmt.Printf("Discovered relays: %d\n", len(clearOnline))
			remainingRelays := len(clearOnline) - len(crawledRelays)
			mu.Unlock()

			if remainingRelays == 0 {
				fmt.Println("No more relays to crawl. Waiting for new relays...")
				time.Sleep(30 * time.Second) // Wait before retrying
				continue
			}

			time.Sleep(2 * time.Second)
		}
	}()

	// Wait for an exit signal (Ctrl+C or kill)
	<-exitSignal

	fmt.Println("\nReceived exit signal, writing logs and exiting...")
	finalize()
}
