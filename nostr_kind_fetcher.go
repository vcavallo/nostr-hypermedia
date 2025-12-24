package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// NostrKindMetadata represents action metadata fetched from Nostr kind 39001 events.
// This is separate from KindDefinition (kinds.go) which handles rendering hints.
type NostrKindMetadata struct {
	Kind       int
	Name       string
	Icon       string
	RenderHint string
	Color      string
	Actions    []ParsedActionTemplate
	FetchedAt  time.Time
	Source     string // "nostr" or "local"
}

// CachedKindMetadata holds fetched metadata with TTL
type CachedKindMetadata struct {
	mu        sync.RWMutex
	metadata  map[int]*NostrKindMetadata
	fetchedAt time.Time
	ttl       time.Duration
}

// Global cache for Nostr-sourced kind metadata
var kindMetadataCache = &CachedKindMetadata{
	metadata: make(map[int]*NostrKindMetadata),
	ttl:      1 * time.Hour,
}

// Config for Nostr-based kind definitions
var (
	nostrActionsEnabled bool
	nostrActionsRelays  []string
	nostrActionsTTL     time.Duration
)

func init() {
	// Load config from environment
	nostrActionsEnabled = os.Getenv("NOSTR_ACTIONS_ENABLED") == "1" ||
		os.Getenv("NOSTR_ACTIONS_ENABLED") == "true"

	if relays := os.Getenv("NOSTR_ACTIONS_RELAYS"); relays != "" {
		nostrActionsRelays = strings.Split(relays, ",")
		for i := range nostrActionsRelays {
			nostrActionsRelays[i] = strings.TrimSpace(nostrActionsRelays[i])
		}
	}

	if ttlStr := os.Getenv("NOSTR_ACTIONS_TTL"); ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil {
			nostrActionsTTL = d
			kindMetadataCache.ttl = d
		}
	}

	// Start background refresh if enabled
	if nostrActionsEnabled && len(nostrActionsRelays) > 0 {
		go kindDefinitionRefreshLoop()
	}
}

// IsNostrActionsEnabled returns whether Nostr-based action fetching is enabled
func IsNostrActionsEnabled() bool {
	return nostrActionsEnabled && len(nostrActionsRelays) > 0
}

// FetchKindDefinitionsFromNostr fetches kind definition events (kind 39001) from relays.
// Returns a map of kind number to NostrKindMetadata.
func FetchKindDefinitionsFromNostr(ctx context.Context, relays []string) (map[int]*NostrKindMetadata, error) {
	if len(relays) == 0 {
		return nil, nil
	}

	metadata := make(map[int]*NostrKindMetadata)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Query each relay for kind 39001 events
	filter := map[string]interface{}{
		"kinds": []int{39001},
		"limit": 100,
	}

	for _, relayURL := range relays {
		wg.Add(1)
		go func(relay string) {
			defer wg.Done()

			subID := "kinddef-" + strconv.FormatInt(time.Now().UnixNano(), 36)
			sub, err := relayPool.Subscribe(ctx, relay, subID, filter)
			if err != nil {
				slog.Debug("kind fetcher: failed to subscribe", "relay", relay, "error", err)
				return
			}
			defer relayPool.Unsubscribe(relay, sub)

			// Collect events until EOSE or timeout
			timeout := time.After(10 * time.Second)
			for {
				select {
				case evt := <-sub.EventChan:
					if meta := parseKindMetadataEvent(evt); meta != nil {
						mu.Lock()
						// Only update if we don't have this kind or this is newer
						existing := metadata[meta.Kind]
						if existing == nil || evt.CreatedAt > existing.FetchedAt.Unix() {
							metadata[meta.Kind] = meta
						}
						mu.Unlock()
					}
				case <-sub.EOSEChan:
					return
				case <-timeout:
					return
				case <-ctx.Done():
					return
				}
			}
		}(relayURL)
	}

	wg.Wait()
	return metadata, nil
}

// parseKindMetadataEvent parses a kind 39001 event into NostrKindMetadata.
// Expected tag format:
//   - ["k", "kind_number"]
//   - ["name", "Kind Name"]
//   - ["icon", "emoji"]
//   - ["render", "hint"]
//   - ["color", "#hexcolor"]
//   - ["action", name, method, href, field_spec...]
func parseKindMetadataEvent(evt Event) *NostrKindMetadata {
	if evt.Kind != 39001 {
		return nil
	}

	kind := ParseKindFromTags(evt.Tags)
	if kind == 0 {
		return nil
	}

	meta := &NostrKindMetadata{
		Kind:       kind,
		Name:       ParseKindNameFromTags(evt.Tags),
		RenderHint: ParseRenderHintFromTags(evt.Tags),
		Actions:    parseActionTags(evt.Tags),
		FetchedAt:  time.Unix(evt.CreatedAt, 0),
		Source:     "nostr",
	}

	// Parse icon and color tags
	for _, tag := range evt.Tags {
		if len(tag) >= 2 {
			switch tag[0] {
			case "icon":
				meta.Icon = tag[1]
			case "color":
				meta.Color = tag[1]
			}
		}
	}

	return meta
}

// GetCachedKindMetadata returns cached metadata for a kind, if available
func GetCachedKindMetadata(kind int) *NostrKindMetadata {
	kindMetadataCache.mu.RLock()
	defer kindMetadataCache.mu.RUnlock()
	return kindMetadataCache.metadata[kind]
}

// UpdateKindMetadataCache updates the cache with new metadata
func UpdateKindMetadataCache(metadata map[int]*NostrKindMetadata) {
	kindMetadataCache.mu.Lock()
	defer kindMetadataCache.mu.Unlock()

	for kind, meta := range metadata {
		kindMetadataCache.metadata[kind] = meta
	}
	kindMetadataCache.fetchedAt = time.Now()
}

// IsCacheStale returns true if the cache needs refreshing
func IsCacheStale() bool {
	kindMetadataCache.mu.RLock()
	defer kindMetadataCache.mu.RUnlock()

	if kindMetadataCache.fetchedAt.IsZero() {
		return true
	}
	return time.Since(kindMetadataCache.fetchedAt) > kindMetadataCache.ttl
}

// kindDefinitionRefreshLoop runs in the background to refresh metadata periodically
func kindDefinitionRefreshLoop() {
	// Initial fetch on startup (with delay to let other systems initialize)
	time.Sleep(5 * time.Second)
	RefreshKindMetadata()

	// Then refresh periodically
	ticker := time.NewTicker(kindMetadataCache.ttl / 2) // Refresh at half TTL
	defer ticker.Stop()

	for range ticker.C {
		if IsCacheStale() {
			RefreshKindMetadata()
		}
	}
}

// RefreshKindMetadata fetches fresh metadata from Nostr relays
func RefreshKindMetadata() {
	if !IsNostrActionsEnabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	slog.Debug("kind fetcher: refreshing metadata", "relays", len(nostrActionsRelays))
	startTime := time.Now()

	metadata, err := FetchKindDefinitionsFromNostr(ctx, nostrActionsRelays)
	if err != nil {
		slog.Error("kind fetcher: error fetching metadata", "error", err)
		return
	}

	if len(metadata) > 0 {
		UpdateKindMetadataCache(metadata)
		slog.Debug("kind fetcher: cached metadata entries", "count", len(metadata), "duration", time.Since(startTime))
	} else {
		slog.Debug("kind fetcher: no metadata found, using local fallback")
	}
}

