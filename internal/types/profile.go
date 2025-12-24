package types

// ProfileInfo contains user profile metadata (kind 0)
type ProfileInfo struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Nip05       string `json:"nip05,omitempty"`
	About       string `json:"about,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Lud16       string `json:"lud16,omitempty"`
	Lud06       string `json:"lud06,omitempty"`
	Website     string `json:"website,omitempty"`

	// NIP-05 verification (populated async)
	NIP05Verified bool     `json:"nip05_verified,omitempty"`
	NIP05Domain   string   `json:"nip05_domain,omitempty"`   // Display domain (e.g., "example.com" for "_@example.com")
	NIP05Relays   []string `json:"nip05_relays,omitempty"`   // Relay hints from verification
}

// ReactionsSummary contains aggregated reaction counts for an event
type ReactionsSummary struct {
	Total  int            `json:"total"`
	ByType map[string]int `json:"by_type"` // Kept for API compatibility and user reaction detection
}
