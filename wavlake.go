package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Wavlake URL patterns
var (
	// Matches: /track/{uuid}, /album/{uuid}, /playlist/{uuid}
	wavlakeUUIDPattern = regexp.MustCompile(`^https?://(?:www\.)?wavlake\.com/(track|album|playlist)/([a-f0-9-]{36})(?:\?.*)?$`)
	// Matches: /feed/show/{uuid} (podcast RSS feeds)
	wavlakeFeedPattern = regexp.MustCompile(`^https?://(?:www\.)?wavlake\.com/feed/show/([a-f0-9-]{36})(?:\?.*)?$`)
	// Matches: /podcast/{slug}
	wavlakePodcastPattern = regexp.MustCompile(`^https?://(?:www\.)?wavlake\.com/podcast/([a-zA-Z0-9_-]+)(?:\?.*)?$`)
	// Matches: /{artist-slug} (but not other paths)
	wavlakeArtistPattern = regexp.MustCompile(`^https?://(?:www\.)?wavlake\.com/([a-zA-Z0-9_-]+)(?:\?.*)?$`)
)

// WavlakeContentType represents the type of Wavlake content
type WavlakeContentType string

const (
	WavlakeTypeTrack    WavlakeContentType = "track"
	WavlakeTypeAlbum    WavlakeContentType = "album"
	WavlakeTypePlaylist WavlakeContentType = "playlist"
	WavlakeTypeFeed     WavlakeContentType = "feed"
	WavlakeTypePodcast  WavlakeContentType = "podcast"
	WavlakeTypeArtist   WavlakeContentType = "artist"
)

// WavlakeURL represents a parsed Wavlake URL
type WavlakeURL struct {
	Type       WavlakeContentType
	ID         string // UUID for track/album/playlist, slug for podcast/artist
	OriginalURL string
}

// NOMContent represents the Nostr Open Media content structure
type NOMContent struct {
	Title       string `json:"title"`
	GUID        string `json:"guid"`
	Creator     string `json:"creator"`
	Type        string `json:"type"`         // MIME type, e.g., "audio/mpeg"
	Duration    int    `json:"duration"`     // seconds
	PublishedAt string `json:"published_at"` // Unix timestamp as string
	Link        string `json:"link"`         // Wavlake page URL
	Enclosure   string `json:"enclosure"`    // Audio stream URL
	Version     string `json:"version"`
}

// WavlakeTrack represents a parsed Wavlake track ready for display
type WavlakeTrack struct {
	ID           string
	Title        string
	Creator      string
	Duration     int    // seconds
	AudioURL     string // From enclosure
	PageURL      string // From link
	ArtistPubkey string // From event.pubkey
	EventID      string // For actions/zaps
	ContentType  WavlakeContentType
}

// ParseWavlakeURL parses a URL and returns WavlakeURL if it's a valid Wavlake URL
func ParseWavlakeURL(url string) *WavlakeURL {
	url = strings.TrimSpace(url)

	// Check for track/album/playlist (UUID-based)
	if matches := wavlakeUUIDPattern.FindStringSubmatch(url); matches != nil {
		contentType := WavlakeContentType(matches[1])
		return &WavlakeURL{
			Type:        contentType,
			ID:          matches[2],
			OriginalURL: url,
		}
	}

	// Check for feed/show (UUID-based podcast feed)
	if matches := wavlakeFeedPattern.FindStringSubmatch(url); matches != nil {
		return &WavlakeURL{
			Type:        WavlakeTypeFeed,
			ID:          matches[1],
			OriginalURL: url,
		}
	}

	// Check for podcast (slug-based)
	if matches := wavlakePodcastPattern.FindStringSubmatch(url); matches != nil {
		return &WavlakeURL{
			Type:        WavlakeTypePodcast,
			ID:          matches[1],
			OriginalURL: url,
		}
	}

	// Check for artist (slug-based) - must be last as it's the most generic
	if matches := wavlakeArtistPattern.FindStringSubmatch(url); matches != nil {
		slug := matches[1]
		// Exclude known paths that aren't artist slugs
		excludedPaths := []string{"track", "album", "playlist", "podcast", "feed", "api", "embed", "static", "favicon.ico"}
		for _, excluded := range excludedPaths {
			if slug == excluded {
				return nil
			}
		}
		return &WavlakeURL{
			Type:        WavlakeTypeArtist,
			ID:          slug,
			OriginalURL: url,
		}
	}

	return nil
}

// IsWavlakeURL returns true if the URL is a Wavlake URL
func IsWavlakeURL(url string) bool {
	return ParseWavlakeURL(url) != nil
}

// FetchWavlakeTrack fetches track metadata from nostr relays via kind 32123 NOM events
// Only tracks have NOM events; playlists, albums, artists, and podcasts use link previews
func FetchWavlakeTrack(wavlakeURL *WavlakeURL) (*WavlakeTrack, error) {
	if wavlakeURL == nil {
		return nil, fmt.Errorf("nil wavlake URL")
	}

	// Only tracks have NOM events - everything else uses link preview
	if wavlakeURL.Type != WavlakeTypeTrack {
		return nil, fmt.Errorf("%s pages use link preview, not NOM", wavlakeURL.Type)
	}

	// Check cache first
	if cached, ok := wavlakeCache.Get(wavlakeURL.ID); ok {
		return cached, nil
	}

	// Query for kind 32123 with matching d-tag
	filter := Filter{
		Kinds: []int{32123},
		DTags: []string{wavlakeURL.ID},
		Limit: 1,
	}

	// Use default relays (aggregators) - may need to add relay.wavlake.com later
	relays := DefaultRelays()

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 5*time.Second)

	if len(events) == 0 {
		return nil, fmt.Errorf("NOM event not found for %s/%s", wavlakeURL.Type, wavlakeURL.ID)
	}

	event := events[0]

	// Parse NOM content from event
	var nom NOMContent
	if err := json.Unmarshal([]byte(event.Content), &nom); err != nil {
		return nil, fmt.Errorf("failed to parse NOM content: %w", err)
	}

	track := &WavlakeTrack{
		ID:           wavlakeURL.ID,
		Title:        nom.Title,
		Creator:      nom.Creator,
		Duration:     nom.Duration,
		AudioURL:     nom.Enclosure,
		PageURL:      nom.Link,
		ArtistPubkey: event.PubKey,
		EventID:      event.ID,
		ContentType:  wavlakeURL.Type,
	}

	// Use original URL as fallback for PageURL
	if track.PageURL == "" {
		track.PageURL = wavlakeURL.OriginalURL
	}

	// Cache the result
	wavlakeCache.Set(wavlakeURL.ID, track, 1*time.Hour)

	return track, nil
}

// FormatDuration formats seconds as "m:ss" or "h:mm:ss"
func FormatDuration(seconds int) string {
	if seconds < 0 {
		return "0:00"
	}

	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, secs)
	}
	return fmt.Sprintf("%d:%02d", minutes, secs)
}

// GetWavlakeIcon returns the appropriate icon for the content type
func GetWavlakeIcon(contentType WavlakeContentType) string {
	switch contentType {
	case WavlakeTypePodcast:
		return "🎙️"
	default:
		return "🎵"
	}
}
