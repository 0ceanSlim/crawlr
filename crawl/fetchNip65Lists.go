package crawl

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/net/websocket"
)

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
