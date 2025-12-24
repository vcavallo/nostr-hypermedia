package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"nostr-server/internal/types"
)

// DVMResult represents the result of a DVM request
type DVMResult struct {
	EventRefs []DVMEventRef // Event references from the DVM response
	FetchedAt time.Time     // When this result was fetched
}

// DVMEventRef represents an event reference from a DVM response
type DVMEventRef struct {
	Type     string // "e" or "a"
	ID       string // Event ID (for "e") or address (for "a")
	RelayURL string // Relay hint
}

// DVMClient handles DVM requests
type DVMClient struct {
	pool   *RelayPool
	mu     sync.Mutex
}

// NewDVMClient creates a new DVM client
func NewDVMClient(pool *RelayPool) *DVMClient {
	return &DVMClient{
		pool: pool,
	}
}

// Global DVM client instance
var (
	dvmClient     *DVMClient
	dvmClientOnce sync.Once
)

// GetDVMClient returns the global DVM client instance
func GetDVMClient() *DVMClient {
	dvmClientOnce.Do(func() {
		dvmClient = NewDVMClient(relayPool)
	})
	return dvmClient
}

// RequestContent sends a content discovery request (kind 5300) to a DVM
// and waits for the response (kind 6300)
func (c *DVMClient) RequestContent(ctx context.Context, dvmConfig *DVMConfig, userPubkey string) (*DVMResult, error) {
	// Get server keypair for signing
	kp, err := GetServerKeypair()
	if err != nil {
		return nil, err
	}

	// Build the request event
	event, err := c.buildRequestEvent(kp, dvmConfig, userPubkey)
	if err != nil {
		return nil, err
	}

	// Get relays to use (DVM-specific or fallback to default)
	relays := dvmConfig.GetRelays()
	if len(relays) == 0 {
		return nil, errors.New("no relays configured for DVM")
	}

	slog.Debug("DVM request starting", "request_id", event.ID[:16], "dvm_pubkey", dvmConfig.Pubkey[:16], "relays", relays)

	// Create response channel
	responseChan := make(chan *Event, 1)
	errChan := make(chan error, 1)
	readyChan := make(chan struct{}, len(relays)) // Signal when subscriptions are ready

	// Subscribe for response before publishing
	go c.subscribeForResponse(ctx, relays, event.ID, dvmConfig.Pubkey, responseChan, errChan, readyChan)

	// Wait for at least one subscription to be ready (or timeout after 200ms)
	slog.Debug("DVM waiting for subscriptions", "request_id", event.ID[:16])
	select {
	case <-readyChan:
		slog.Debug("DVM subscription ready", "request_id", event.ID[:16])
	case <-time.After(200 * time.Millisecond):
		slog.Debug("DVM subscription wait timeout, proceeding", "request_id", event.ID[:16])
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Publish request to relays (non-blocking - don't wait for OK)
	go c.publishRequest(ctx, relays, event)

	slog.Debug("DVM request publish started, waiting for response", "request_id", event.ID[:16])

	// Wait for response or timeout
	select {
	case response := <-responseChan:
		slog.Debug("DVM response received", "request_id", event.ID[:16], "response_id", response.ID[:16])
		return c.parseResponse(response)
	case err := <-errChan:
		slog.Debug("DVM subscription error", "request_id", event.ID[:16], "error", err)
		return nil, err
	case <-ctx.Done():
		slog.Debug("DVM request timed out", "request_id", event.ID[:16])
		return nil, ctx.Err()
	}
}

// buildRequestEvent creates a kind 5300 DVM request event
func (c *DVMClient) buildRequestEvent(kp *ServerKeypair, dvmConfig *DVMConfig, userPubkey string) (*Event, error) {
	pubKeyHex := hex.EncodeToString(kp.PubKey)

	// Build tags
	tags := [][]string{
		{"p", dvmConfig.Pubkey}, // DVM pubkey
	}

	// Add user pubkey for personalized requests
	if dvmConfig.Personalized && userPubkey != "" {
		tags = append(tags, []string{"p", userPubkey})
	}

	// Add params from config
	for key, value := range dvmConfig.Params {
		tags = append(tags, []string{"param", key, value})
	}

	event := &Event{
		PubKey:    pubKeyHex,
		CreatedAt: time.Now().Unix(),
		Kind:      dvmConfig.Kind, // 5300 for content discovery
		Tags:      tags,
		Content:   "",
	}

	// Calculate event ID
	event.ID = calculateEventID(event)

	// Sign the event
	event.Sig = signEvent(kp.PrivKey, event.ID)
	if event.Sig == "" {
		return nil, errors.New("failed to sign DVM request")
	}

	return event, nil
}

// publishRequest publishes the DVM request to relays
func (c *DVMClient) publishRequest(ctx context.Context, relays []string, event *Event) error {
	eventMsg := []interface{}{"EVENT", event}

	var lastErr error
	successCount := 0

	slog.Debug("DVM publishRequest starting", "event_id", event.ID[:16], "relays", len(relays))

	// Publish to all relays in parallel for speed
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			slog.Debug("DVM publishing to relay", "relay", relayURL, "event_id", event.ID[:16])
			resp, err := c.pool.PublishEvent(ctx, relayURL, event.ID, eventMsg)
			if err != nil {
				slog.Debug("DVM publish failed", "relay", relayURL, "error", err)
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}
			if !resp.Success {
				slog.Debug("DVM publish rejected", "relay", relayURL, "message", resp.Message)
				mu.Lock()
				lastErr = errors.New(resp.Message)
				mu.Unlock()
				return
			}
			mu.Lock()
			successCount++
			mu.Unlock()
			slog.Debug("DVM published successfully", "relay", relayURL, "event_id", event.ID[:16])
		}(relay)
	}

	wg.Wait()
	slog.Debug("DVM publishRequest complete", "event_id", event.ID[:16], "success_count", successCount)

	if successCount == 0 {
		if lastErr != nil {
			return lastErr
		}
		return errors.New("failed to publish DVM request to any relay")
	}

	return nil
}

// subscribeForResponse subscribes to relays for the DVM response
func (c *DVMClient) subscribeForResponse(ctx context.Context, relays []string, requestID string, dvmPubkey string, responseChan chan *Event, errChan chan error, readyChan chan struct{}) {
	// Response kind is request kind + 1000 (5300 -> 6300)
	responseKind := 6300

	// Subscribe to each relay
	var wg sync.WaitGroup
	found := make(chan *Event, 1)

	slog.Debug("DVM subscribing for response", "request_id", requestID[:16], "relays", len(relays), "response_kind", responseKind)

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()

			subID := "dvm-" + requestID[:8]
			filter := map[string]interface{}{
				"kinds":   []int{responseKind},
				"authors": []string{dvmPubkey},
				"#e":      []string{requestID}, // Response references our request
				"limit":   1,
			}

			sub, err := c.pool.Subscribe(ctx, relayURL, subID, filter)
			if err != nil {
				slog.Debug("DVM subscription failed", "relay", relayURL, "error", err)
				return
			}
			defer sub.Close()

			slog.Debug("DVM subscription established", "relay", relayURL, "sub_id", subID)

			// Signal that subscription is ready
			select {
			case readyChan <- struct{}{}:
			default:
			}

			select {
			case event := <-sub.EventChan:
				eventIDShort := event.ID
				if len(eventIDShort) > 16 {
					eventIDShort = eventIDShort[:16]
				}
				// Verify signature before trusting (defense-in-depth)
				if !validateEventSignature(&event) {
					slog.Warn("DVM response signature invalid", "relay", relayURL, "event_id", eventIDShort)
					// Invalid signature - don't send to found channel
				} else {
					slog.Debug("DVM event received on subscription", "relay", relayURL, "event_id", eventIDShort)
					select {
					case found <- &event:
					default:
					}
				}
			case <-sub.EOSEChan:
				slog.Debug("DVM EOSE received, waiting for live events", "relay", relayURL)
				// No stored response, keep waiting for live
				select {
				case event := <-sub.EventChan:
					eventIDShort := event.ID
					if len(eventIDShort) > 16 {
						eventIDShort = eventIDShort[:16]
					}
					// Verify signature before trusting (defense-in-depth)
					if !validateEventSignature(&event) {
						slog.Warn("DVM live response signature invalid", "relay", relayURL, "event_id", eventIDShort)
						// Invalid signature - don't send to found channel
					} else {
						slog.Debug("DVM live event received", "relay", relayURL, "event_id", eventIDShort)
						select {
						case found <- &event:
						default:
						}
					}
				case <-ctx.Done():
					slog.Debug("DVM subscription context done after EOSE", "relay", relayURL)
				}
			case <-ctx.Done():
				slog.Debug("DVM subscription context done", "relay", relayURL)
			}
		}(relay)
	}

	// Wait for first response or all subscriptions to finish
	select {
	case event := <-found:
		slog.Debug("DVM found response", "event_id", event.ID[:16])
		responseChan <- event
	case <-ctx.Done():
		slog.Debug("DVM all subscriptions timed out")
		errChan <- ctx.Err()
	}
}

// parseResponse parses the DVM response event into event references
func (c *DVMClient) parseResponse(event *Event) (*DVMResult, error) {
	result := &DVMResult{
		FetchedAt: time.Now(),
	}

	// Parse event references from tags
	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}

		switch tag[0] {
		case "e":
			ref := DVMEventRef{
				Type: "e",
				ID:   tag[1],
			}
			if len(tag) >= 3 {
				ref.RelayURL = tag[2]
			}
			result.EventRefs = append(result.EventRefs, ref)

		case "a":
			ref := DVMEventRef{
				Type: "a",
				ID:   tag[1],
			}
			if len(tag) >= 3 {
				ref.RelayURL = tag[2]
			}
			result.EventRefs = append(result.EventRefs, ref)
		}
	}

	// Also try parsing content as JSON array of tags
	if event.Content != "" {
		var contentTags [][]string
		if err := json.Unmarshal([]byte(event.Content), &contentTags); err == nil {
			for _, tag := range contentTags {
				if len(tag) < 2 {
					continue
				}
				switch tag[0] {
				case "e":
					ref := DVMEventRef{
						Type: "e",
						ID:   tag[1],
					}
					if len(tag) >= 3 {
						ref.RelayURL = tag[2]
					}
					result.EventRefs = append(result.EventRefs, ref)

				case "a":
					ref := DVMEventRef{
						Type: "a",
						ID:   tag[1],
					}
					if len(tag) >= 3 {
						ref.RelayURL = tag[2]
					}
					result.EventRefs = append(result.EventRefs, ref)
				}
			}
		}
	}

	slog.Info("parsed DVM response", "event_refs", len(result.EventRefs))
	return result, nil
}

// GetContentWithCache fetches DVM content with caching
// Returns cached results if available and not expired
func (c *DVMClient) GetContentWithCache(ctx context.Context, dvmConfig *DVMConfig, userPubkey string) (*DVMResult, error) {
	// Build cache key
	cacheKey := buildDVMCacheKey(dvmConfig, userPubkey)

	// Check cache
	cached, found, err := dvmCacheStore.Get(ctx, cacheKey)
	if err != nil {
		slog.Debug("DVM cache error", "error", err)
	}

	if found {
		slog.Debug("DVM cache hit", "key", cacheKey)
		return cachedResultToDVMResult(cached), nil
	}

	slog.Debug("DVM cache miss, fetching from DVM", "key", cacheKey)

	// Fetch from DVM
	result, err := c.RequestContent(ctx, dvmConfig, userPubkey)
	if err != nil {
		return nil, err
	}

	// Cache the result
	cachedResult := dvmResultToCached(result)
	ttl := time.Duration(dvmConfig.GetCacheTTL()) * time.Second
	if err := dvmCacheStore.Set(ctx, cacheKey, cachedResult, ttl); err != nil {
		slog.Warn("failed to cache DVM result", "error", err)
	}

	return result, nil
}

// buildDVMCacheKey builds the cache key for DVM results
// Generic: "dvm:{kind}:{dvm-pubkey}"
// Personalized: "dvm:{kind}:{dvm-pubkey}:{user-pubkey}"
func buildDVMCacheKey(dvmConfig *DVMConfig, userPubkey string) string {
	if dvmConfig.Personalized && userPubkey != "" {
		return fmt.Sprintf("dvm:%d:%s:%s", dvmConfig.Kind, dvmConfig.Pubkey, userPubkey)
	}
	return fmt.Sprintf("dvm:%d:%s", dvmConfig.Kind, dvmConfig.Pubkey)
}

// dvmResultToCached converts DVMResult to CachedDVMResult for storage
func dvmResultToCached(result *DVMResult) *CachedDVMResult {
	cached := &CachedDVMResult{
		CachedAt: time.Now().Unix(),
	}
	for _, ref := range result.EventRefs {
		cached.EventRefs = append(cached.EventRefs, CachedDVMEventRef{
			Type:     ref.Type,
			ID:       ref.ID,
			RelayURL: ref.RelayURL,
		})
	}
	return cached
}

// cachedResultToDVMResult converts CachedDVMResult back to DVMResult
func cachedResultToDVMResult(cached *CachedDVMResult) *DVMResult {
	result := &DVMResult{
		FetchedAt: time.Unix(cached.CachedAt, 0),
	}
	for _, ref := range cached.EventRefs {
		result.EventRefs = append(result.EventRefs, DVMEventRef{
			Type:     ref.Type,
			ID:       ref.ID,
			RelayURL: ref.RelayURL,
		})
	}
	return result
}

// Type alias for internal/types
type DVMMetadata = types.DVMMetadata

// DVMAnnouncementContent represents the JSON content of a kind 31990 event
type DVMAnnouncementContent struct {
	Name    string `json:"name"`
	About   string `json:"about"`
	Picture string `json:"picture"`
	Image   string `json:"image"` // Some DVMs use "image" instead of "picture"
}

// GetDVMMetadata returns metadata for a DVM, using config overrides or fetching from kind 31990
func GetDVMMetadata(ctx context.Context, dvmConfig *DVMConfig) *DVMMetadata {
	// Start with config overrides
	meta := &DVMMetadata{
		Name:        dvmConfig.Name,
		Image:       dvmConfig.Image,
		Description: dvmConfig.Description,
		Pubkey:      dvmConfig.Pubkey,
	}

	// If all fields are configured, no need to fetch
	if meta.Name != "" && meta.Image != "" && meta.Description != "" {
		return meta
	}

	// Try to fetch from kind 31990 (DVM announcement)
	fetched := fetchDVMAnnouncementMetadata(ctx, dvmConfig.Pubkey, dvmConfig.GetRelays())

	// Fill in missing fields from fetched data
	if meta.Name == "" && fetched != nil {
		meta.Name = fetched.Name
	}
	if meta.Image == "" && fetched != nil {
		meta.Image = fetched.Image
	}
	if meta.Description == "" && fetched != nil {
		meta.Description = fetched.Description
	}

	// Fallback: use truncated pubkey if no name
	if meta.Name == "" {
		meta.Name = truncatePubkey(meta.Pubkey)
	}

	return meta
}

// fetchDVMAnnouncementMetadata fetches and parses kind 31990 metadata with caching
func fetchDVMAnnouncementMetadata(ctx context.Context, pubkey string, relays []string) *DVMMetadata {
	// Check cache first
	cacheKey := "dvm-meta:" + pubkey
	cached, found, _ := dvmMetaCacheStore.Get(ctx, cacheKey)
	if found {
		return cached
	}

	// Fetch kind 31990 from relays
	filter := map[string]interface{}{
		"kinds":   []int{31990},
		"authors": []string{pubkey},
		"limit":   1,
	}

	var announcement *Event
	for _, relay := range relays {
		subID := "dvm-meta-" + pubkey[:8]
		sub, err := relayPool.Subscribe(ctx, relay, subID, filter)
		if err != nil {
			continue
		}

		// Wait for event or EOSE
		select {
		case evt := <-sub.EventChan:
			announcement = &evt
			sub.Close()
			break
		case <-sub.EOSEChan:
			sub.Close()
			continue
		case <-ctx.Done():
			sub.Close()
			return nil
		}

		if announcement != nil {
			break
		}
	}

	if announcement == nil {
		// Cache negative result for shorter time (5 minutes)
		dvmMetaCacheStore.Set(ctx, cacheKey, nil, 5*time.Minute)
		return nil
	}

	// Parse the content JSON
	var content DVMAnnouncementContent
	if err := json.Unmarshal([]byte(announcement.Content), &content); err != nil {
		slog.Debug("failed to parse DVM announcement content", "error", err)
		return nil
	}

	meta := &DVMMetadata{
		Name:        content.Name,
		Description: content.About,
		Pubkey:      pubkey,
	}

	// Use picture or image field
	if content.Picture != "" {
		meta.Image = content.Picture
	} else if content.Image != "" {
		meta.Image = content.Image
	}

	// Cache for 1 hour
	dvmMetaCacheStore.Set(ctx, cacheKey, meta, time.Hour)

	slog.Debug("fetched DVM metadata", "pubkey", pubkey[:16], "name", meta.Name)
	return meta
}

// truncatePubkey truncates a hex pubkey for display
func truncatePubkey(pubkey string) string {
	if len(pubkey) > 16 {
		return pubkey[:8] + "..." + pubkey[len(pubkey)-4:]
	}
	return pubkey
}
