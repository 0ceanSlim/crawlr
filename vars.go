package main

import "sync"

// Relay lists with mutex protection
var (
	mu            sync.Mutex
	clearOnline   = make(map[string]int)
	clearOffline  = make(map[string]int)
	clearAPI      = make(map[string]int)
	onion         = make(map[string]int)
	local         = make(map[string]int)
	malformed     = make(map[string]int)
	crawledRelays = make(map[string]bool)
	logChannel    = make(chan string, 100)
)