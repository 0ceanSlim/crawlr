package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type RelayInfo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Pubkey        string   `json:"pubkey"`
	Contact       string   `json:"contact"`
	SupportedNIPs []int    `json:"supported_nips"`
	Software      string   `json:"software"`
	Version       string   `json:"version"`
}

const (
	NoSoftwareListed = "No Software Listed"
	Offline          = "Offline"
	Other            = "Other"
)

func main() {
	file, err := os.Open("relays.csv")
	if err != nil {
		fmt.Println("Error opening CSV file:", err)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	softwareCounts := make(map[string]int)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Println("Error reading CSV:", err)
			return
		}

		if len(record) > 0 {
			url := record[0]
			wg.Add(1)
			go func(url string) {
				defer wg.Done()
				software := getSoftwareInfo(url)
				mu.Lock()
				softwareCounts[software]++
				mu.Unlock()
			}(url)
		}
	}

	wg.Wait()

	// Process software counts to group less common software into "Other"
	threshold := 10
	groupedCounts := make(map[string]int)
	for software, count := range softwareCounts {
		if count < threshold {
			groupedCounts[Other] += count
		} else {
			groupedCounts[software] = count
		}
	}

	// Write results to a new CSV file
	outputFile, err := os.Create("software_counts.csv")
	if err != nil {
		fmt.Println("Error creating output CSV file:", err)
		return
	}
	defer outputFile.Close()

	writer := csv.NewWriter(outputFile)
	defer writer.Flush()

	writer.Write([]string{"Software", "Count"})
	for software, count := range groupedCounts {
		writer.Write([]string{software, fmt.Sprintf("%d", count)})
	}

	fmt.Println("Software counts have been written to software_counts.csv")
}

func getSoftwareInfo(wsURL string) string {
	httpURL := strings.Replace(wsURL, "wss://", "https://", 1)
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", httpURL, nil)
	if err != nil {
		return Offline
	}

	req.Header.Set("Accept", "application/nostr+json")

	resp, err := client.Do(req)
	if err != nil {
		return Offline
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Offline
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Offline
	}

	var relayInfo RelayInfo
	if err := json.Unmarshal(body, &relayInfo); err != nil {
		return Offline
	}

	if relayInfo.Software == "" {
		return NoSoftwareListed
	}

	return strings.TrimSpace(relayInfo.Software)
}
