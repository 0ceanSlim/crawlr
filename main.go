package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olekukonko/ts"
)

// Update progress and display in the terminal
func updateProgress() {
	for {
		mu.Lock()
		totalRelays := len(clearOnline) + len(clearOffline) // Include both online and offline relays
		crawled := len(crawledRelays)
		mu.Unlock()

		remaining := totalRelays - crawled
		if remaining < 0 {
			remaining = 0
		}

		// Progress calculation
		var progress float64
		if totalRelays > 0 {
			progress = (float64(crawled) / float64(totalRelays)) * 100
		}

		// Print the status at the bottom
		screen, _ := ts.GetSize() // Get terminal size to dynamically adjust progress bar width
		barWidth := screen.Col() - 30 // Adjust width for bar
		progressBar := generateProgressBar(int(progress), barWidth)

		// Clear last line and print status
		fmt.Printf("\rDiscovered Relays: %d | Crawled Relays: %d | Remaining: %d | [%s] %.2f%%",
			totalRelays, crawled, remaining, progressBar, progress)

		time.Sleep(1 * time.Second)
	}
}

// Generate a progress bar
func generateProgressBar(progress int, width int) string {
	filled := (progress * width) / 100
	bar := ""
	for i := 0; i < filled; i++ {
		bar += "="
	}
	for i := filled; i < width; i++ {
		bar += " "
	}
	return bar
}

func main() {
	exitSignal := make(chan os.Signal, 1)
	signal.Notify(exitSignal, os.Interrupt, syscall.SIGTERM)

	go logRelayEvents() // Start the logger goroutine

	go func() {
		initialRelay := "wss://nos.lol"
		concurrency := 200 // Adjust this value based on your needs and system capabilities

		for {
			err := ReqKind10002(initialRelay)
			if err != nil {
				logChannel <- fmt.Sprintf("Initial crawl failed: %v", err)
			}

			crawlClearOnlineRelays(concurrency)

			mu.Lock()
			logChannel <- fmt.Sprintf("Discovered relays: %d", len(clearOnline))
			mu.Unlock()

			time.Sleep(2 * time.Second)
		}
	}()

	// Start the progress updater in a separate goroutine
	go updateProgress()

	// Wait for an exit signal (Ctrl+C or kill)
	<-exitSignal

	fmt.Println("\nReceived exit signal, writing logs and exiting...")
	finalize()
}