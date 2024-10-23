package main

import "time"

const (
	ClearOnline  RelayCategory = "clear_online"
	ClearOffline RelayCategory = "clear_offline"
	ClearAPI     RelayCategory = "clear_api"
	Onion        RelayCategory = "onion"
	Local        RelayCategory = "local"
	Malformed    RelayCategory = "malformed"
)

// Max retries for relays before giving up
const maxTries = 1

// Increased timeout for slow relays
const crawlTimeout = 5 * time.Second

// Backoff duration after a failed attempt
const backoffDuration = 2 * time.Second
