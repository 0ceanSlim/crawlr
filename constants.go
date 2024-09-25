package main

const (
	ClearOnline  RelayCategory = "clear_online"
	ClearOffline RelayCategory = "clear_offline"
	ClearAPI     RelayCategory = "clear_api"
	Onion        RelayCategory = "onion"
	Local        RelayCategory = "local"
	Malformed    RelayCategory = "malformed"
)

// Max tries for a relay before moving to the offline list
const maxTries = 1
