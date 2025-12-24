package types

import "time"

// LinkPreview holds Open Graph metadata for a URL
type LinkPreview struct {
	URL         string
	Title       string
	Description string
	Image       string
	SiteName    string
	FetchedAt   time.Time
	Failed      bool // true if we tried but couldn't get OG tags
}

// DVMMetadata holds display metadata for a DVM
type DVMMetadata struct {
	Name        string // Display name
	Image       string // Image/avatar URL
	Description string // About text
	Pubkey      string // DVM pubkey (for fallback display)
}

// GifResult represents a GIF from the Giphy API
type GifResult struct {
	ID       string
	Title    string
	URL      string
	ThumbURL string
	Width    int
	Height   int
}
