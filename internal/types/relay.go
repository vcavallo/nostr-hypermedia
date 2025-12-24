package types

// RelayList represents a user's NIP-65 relay list
type RelayList struct {
	Read  []string
	Write []string
}

// RelayGroup represents a relay and the pubkeys that write to it
type RelayGroup struct {
	RelayURL    string
	Pubkeys     []string
	Score       int  // Composite score for prioritization
	IsConnected bool // Already has active connection
}
