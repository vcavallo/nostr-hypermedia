package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// isCustomEmojiShortcode checks if a reaction is a custom emoji shortcode (:name:)
// Custom emoji shortcodes can't be rendered as they require image URLs from emoji tags
func isCustomEmojiShortcode(reaction string) bool {
	return len(reaction) >= 3 && strings.HasPrefix(reaction, ":") && strings.HasSuffix(reaction, ":")
}

// shortID safely truncates an ID/pubkey for logging (returns first 12 chars or full string if shorter)
func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

// truncateString truncates a string to maxLen characters to prevent memory abuse
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

type Filter struct {
	IDs     []string // Event IDs to fetch
	Authors []string
	Kinds   []int
	Limit   int
	Since   *int64
	Until   *int64
	PTags   []string // Filter by p-tag (events mentioning these pubkeys)
}

type Event struct {
	ID         string     `json:"id"`
	PubKey     string     `json:"pubkey"`
	CreatedAt  int64      `json:"created_at"`
	Kind       int        `json:"kind"`
	Tags       [][]string `json:"tags"`
	Content    string     `json:"content"`
	Sig        string     `json:"sig"`
	RelaysSeen []string   `json:"-"`
}

type NostrMessage []interface{}

// parseEventFromInterface converts a map[string]interface{} to an Event struct
// This avoids the Marshal/Unmarshal round-trip when parsing events from websocket messages
func parseEventFromInterface(data interface{}) (Event, bool) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return Event{}, false
	}

	evt := Event{}

	if id, ok := m["id"].(string); ok {
		evt.ID = id
	}
	if pk, ok := m["pubkey"].(string); ok {
		evt.PubKey = pk
	}
	if createdAt, ok := m["created_at"].(float64); ok {
		evt.CreatedAt = int64(createdAt)
	}
	if kind, ok := m["kind"].(float64); ok {
		evt.Kind = int(kind)
	}
	if content, ok := m["content"].(string); ok {
		evt.Content = content
	}
	if sig, ok := m["sig"].(string); ok {
		evt.Sig = sig
	}

	// Parse tags
	if tags, ok := m["tags"].([]interface{}); ok {
		evt.Tags = make([][]string, 0, len(tags))
		for _, tag := range tags {
			if tagArr, ok := tag.([]interface{}); ok {
				strTag := make([]string, 0, len(tagArr))
				for _, elem := range tagArr {
					if s, ok := elem.(string); ok {
						strTag = append(strTag, s)
					}
				}
				evt.Tags = append(evt.Tags, strTag)
			}
		}
	}

	// Validate signature if present
	if evt.Sig != "" && !validateEventSignature(&evt) {
		log.Printf("Event signature validation failed for event %s", shortID(evt.ID))
		return Event{}, false
	}

	return evt, evt.ID != ""
}

// validateEventSignature verifies that the event signature is valid for the pubkey
func validateEventSignature(evt *Event) bool {
	// Signature must be 128 hex characters (64 bytes)
	if len(evt.Sig) != 128 {
		return false
	}

	// Pubkey must be 64 hex characters (32 bytes)
	if len(evt.PubKey) != 64 {
		return false
	}

	// Decode the signature
	sigBytes, err := hex.DecodeString(evt.Sig)
	if err != nil {
		return false
	}

	// Decode the pubkey
	pubKeyBytes, err := hex.DecodeString(evt.PubKey)
	if err != nil {
		return false
	}

	// Decode the event ID (message that was signed)
	idBytes, err := hex.DecodeString(evt.ID)
	if err != nil {
		return false
	}

	// Parse the Schnorr signature
	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return false
	}

	// Parse the public key (x-only format for Schnorr)
	pubKey, err := schnorr.ParsePubKey(pubKeyBytes)
	if err != nil {
		return false
	}

	// Verify the signature
	return sig.Verify(idBytes, pubKey)
}

func fetchEventsFromRelays(relays []string, filter Filter) ([]Event, bool) {
	return fetchEventsFromRelaysWithTimeout(relays, filter, 1500*time.Millisecond)
}

// fetchEventsFromRelaysCached checks cache first, then fetches from relays
func fetchEventsFromRelaysCached(relays []string, filter Filter) ([]Event, bool) {
	// Check cache first
	if events, eose, ok := eventCache.Get(relays, filter); ok {
		log.Printf("Cache hit for query (limit=%d, authors=%d)", filter.Limit, len(filter.Authors))
		return events, eose
	}

	// Cache miss - fetch from relays
	log.Printf("Cache miss for query (limit=%d, authors=%d)", filter.Limit, len(filter.Authors))
	events, eose := fetchEventsFromRelays(relays, filter)

	// Store in cache
	eventCache.Set(relays, filter, events, eose)

	return events, eose
}

func fetchEventsFromRelaysWithTimeout(relays []string, filter Filter, timeout time.Duration) ([]Event, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 1000)
	eoseChan := make(chan bool, len(relays))

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchFromRelay(ctx, relayURL, filter, eventChan, eoseChan)
		}(relay)
	}

	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	// Collect events and dedupe
	// Wait for at least 2 relays to EOSE before considering timeout
	seenIDs := make(map[string]bool)
	events := []Event{}
	targetCount := filter.Limit * 2 // Collect 2x limit to allow for deduplication
	eoseCount := 0
	minEOSE := 2
	if len(relays) < minEOSE {
		minEOSE = len(relays)
	}

	// Grace period after we have enough EOSEs - collect remaining events briefly
	var graceTimer <-chan time.Time

collectLoop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break collectLoop
			}
			if !seenIDs[evt.ID] {
				seenIDs[evt.ID] = true
				events = append(events, evt)
				// Early exit once we have enough events
				if len(events) >= targetCount {
					log.Printf("Got %d events, returning early", len(events))
					cancel() // Cancel remaining relay operations
					break collectLoop
				}
			}
		case <-eoseChan:
			eoseCount++
			log.Printf("EOSE count: %d/%d relays", eoseCount, len(relays))
			// Once we have enough EOSEs, start a short grace period
			if eoseCount >= minEOSE && graceTimer == nil {
				graceTimer = time.After(500 * time.Millisecond)
			}
			// If all relays sent EOSE, we're done
			if eoseCount >= len(relays) {
				log.Printf("All %d relays sent EOSE, got %d events", len(relays), len(events))
				break collectLoop
			}
		case <-graceTimer:
			log.Printf("Grace period ended after %d EOSEs, got %d events", eoseCount, len(events))
			break collectLoop
		case <-ctx.Done():
			log.Printf("Context timeout, got %d events (EOSE from %d relays)", len(events), eoseCount)
			break collectLoop
		}
	}

	allEOSE := eoseCount == len(relays)

	// Sort by created_at DESC, then by ID DESC for tie-break
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt != events[j].CreatedAt {
			return events[i].CreatedAt > events[j].CreatedAt
		}
		return events[i].ID > events[j].ID
	})

	// Apply limit
	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[:filter.Limit]
	}

	return events, allEOSE
}

func fetchFromRelay(ctx context.Context, relayURL string, filter Filter, eventChan chan<- Event, eoseChan chan<- bool) {
	// Build filter
	subID := "sub-" + randomString(8)
	reqFilter := map[string]interface{}{
		"limit": filter.Limit,
	}
	if len(filter.IDs) > 0 {
		reqFilter["ids"] = filter.IDs
	}
	if len(filter.Authors) > 0 {
		reqFilter["authors"] = filter.Authors
	}
	if len(filter.Kinds) > 0 {
		reqFilter["kinds"] = filter.Kinds
	}
	if filter.Since != nil {
		reqFilter["since"] = *filter.Since
	}
	if filter.Until != nil {
		reqFilter["until"] = *filter.Until
	}
	if len(filter.PTags) > 0 {
		reqFilter["#p"] = filter.PTags
	}

	// Subscribe using the pool
	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		log.Printf("Failed to subscribe to %s: %v", relayURL, err)
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	// Read events until EOSE or context timeout
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			log.Printf("Received EOSE from %s", relayURL)
			eoseChan <- true
			return
		}
	}
}

func randomString(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	randomBytes := make([]byte, n)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to less random but functional string
		for i := range b {
			b[i] = chars[i%len(chars)]
		}
		return string(b)
	}
	for i := range b {
		b[i] = chars[int(randomBytes[i])%len(chars)]
	}
	return string(b)
}

// fetchEventByID fetches a specific event by its ID
func fetchEventByID(relays []string, eventID string) []Event {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 10)

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchSingleEvent(ctx, relayURL, eventID, eventChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
	}()

	// Collect events (should be just one, but may get duplicates)
	seenIDs := make(map[string]bool)
	events := []Event{}

	for evt := range eventChan {
		if !seenIDs[evt.ID] {
			seenIDs[evt.ID] = true
			events = append(events, evt)
		}
	}

	return events
}

func fetchSingleEvent(ctx context.Context, relayURL string, eventID string, eventChan chan<- Event) {
	subID := "single-" + randomString(8)
	reqFilter := map[string]interface{}{
		"ids":   []string{eventID},
		"limit": 1,
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			return
		}
	}
}

// Specialized relay for profile lookups - has better coverage for kind 0 events
const profileRelay = "wss://purplepag.es"

// fetchProfiles fetches kind 0 (profile metadata) events for the given pubkeys
// Uses the global profileCache to avoid redundant relay queries
// Tries purplepag.es first for faster lookups, falls back to provided relays
func fetchProfiles(relays []string, pubkeys []string) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := profileCache.GetMultiple(pubkeys)
	if len(missing) == 0 {
		log.Printf("Profile cache hit for all %d pubkeys", len(pubkeys))
		return cached
	}
	log.Printf("Profile cache: %d hits, %d misses", len(cached), len(missing))

	// Build filter for missing profiles
	filter := Filter{
		Authors: missing,
		Kinds:   []int{0},
		Limit:   len(missing),
	}

	// Try purplepag.es first with a short timeout (specialized profile relay)
	var events []Event
	purpleEvents, _ := fetchEventsFromRelaysWithTimeout([]string{profileRelay}, filter, 1500*time.Millisecond)
	events = append(events, purpleEvents...)

	// Check which pubkeys we still need
	foundPubkeys := make(map[string]bool)
	for _, evt := range events {
		if evt.Kind == 0 {
			foundPubkeys[evt.PubKey] = true
		}
	}

	// Fall back to other relays for any still-missing profiles
	var stillMissing []string
	for _, pk := range missing {
		if !foundPubkeys[pk] {
			stillMissing = append(stillMissing, pk)
		}
	}

	if len(stillMissing) > 0 {
		log.Printf("purplepag.es found %d/%d profiles, falling back to relays for %d", len(foundPubkeys), len(missing), len(stillMissing))
		fallbackFilter := Filter{
			Authors: stillMissing,
			Kinds:   []int{0},
			Limit:   len(stillMissing),
		}
		fallbackEvents, _ := fetchEventsFromRelaysWithTimeout(relays, fallbackFilter, 2000*time.Millisecond)
		events = append(events, fallbackEvents...)
	} else {
		log.Printf("purplepag.es found all %d profiles", len(missing))
	}

	// Parse profile content and build map
	freshProfiles := make(map[string]*ProfileInfo)
	for _, evt := range events {
		if evt.Kind != 0 {
			continue
		}
		// Only keep the newest profile for each pubkey
		if _, ok := freshProfiles[evt.PubKey]; ok {
			continue
		}

		var profileData map[string]interface{}
		if err := json.Unmarshal([]byte(evt.Content), &profileData); err != nil {
			continue
		}

		profile := &ProfileInfo{}
		if name, ok := profileData["name"].(string); ok {
			profile.Name = truncateString(name, 100)
		}
		if displayName, ok := profileData["display_name"].(string); ok {
			profile.DisplayName = truncateString(displayName, 100)
		}
		if picture, ok := profileData["picture"].(string); ok {
			profile.Picture = truncateString(picture, 500)
		}
		if nip05, ok := profileData["nip05"].(string); ok {
			profile.Nip05 = truncateString(nip05, 200)
		}
		if about, ok := profileData["about"].(string); ok {
			profile.About = truncateString(about, 1000)
		}

		freshProfiles[evt.PubKey] = profile
	}

	// Store freshly fetched profiles in cache
	if len(freshProfiles) > 0 {
		profileCache.SetMultiple(freshProfiles)
		log.Printf("Cached %d new profiles", len(freshProfiles))
	}

	// Merge cached and fresh profiles
	result := make(map[string]*ProfileInfo, len(cached)+len(freshProfiles))
	for pk, p := range cached {
		result[pk] = p
	}
	for pk, p := range freshProfiles {
		result[pk] = p
	}

	return result
}

// fetchReactions fetches kind 7 (reaction) events for the given event IDs
func fetchReactions(relays []string, eventIDs []string) map[string]*ReactionsSummary {
	if len(eventIDs) == 0 {
		return nil
	}

	// Build a set for O(1) lookup instead of O(n) array scan
	eventIDSet := make(map[string]bool, len(eventIDs))
	for _, id := range eventIDs {
		eventIDSet[id] = true
	}

	// Fetch reactions referencing the event IDs via #e tag filter
	events, _ := fetchEventsFromRelaysWithETags(relays, eventIDs)

	// Build reaction summaries per event
	reactions := make(map[string]*ReactionsSummary)
	for _, evt := range events {
		if evt.Kind != 7 {
			continue
		}

		// Find the event being reacted to (last "e" tag)
		var targetEventID string
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				targetEventID = tag[1]
			}
		}

		if targetEventID == "" {
			continue
		}

		// Check if this event ID is in our set (O(1) lookup)
		if !eventIDSet[targetEventID] {
			continue
		}

		// Get or create reaction summary
		summary, ok := reactions[targetEventID]
		if !ok {
			summary = &ReactionsSummary{
				Total:  0,
				ByType: make(map[string]int),
			}
			reactions[targetEventID] = summary
		}

		// Count the reaction
		summary.Total++
		reactionType := evt.Content
		// Normalize "+" and empty to "❤️" (like/heart)
		if reactionType == "" || reactionType == "+" {
			reactionType = "❤️"
		}
		// Skip custom emoji shortcodes (e.g., :amy:, :turtlehappy_sm:) that can't be rendered
		if isCustomEmojiShortcode(reactionType) {
			continue
		}
		summary.ByType[reactionType]++
	}

	return reactions
}

// fetchEventsFromRelaysWithETags fetches reactions referencing specific event IDs
func fetchEventsFromRelaysWithETags(relays []string, eventIDs []string) ([]Event, bool) {
	// Longer timeout for reactions - they can be slow to query
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 1000)
	eoseChan := make(chan bool, len(relays))

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchReactionsFromRelay(ctx, relayURL, eventIDs, eventChan, eoseChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	seenIDs := make(map[string]bool)
	events := []Event{}

	// Use select to respect context timeout instead of blocking on channel
collectLoop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break collectLoop
			}
			if !seenIDs[evt.ID] {
				seenIDs[evt.ID] = true
				events = append(events, evt)
			}
		case <-ctx.Done():
			log.Printf("Reactions fetch timeout, got %d events", len(events))
			break collectLoop
		}
	}

	// Drain eoseChan - count how many relays sent EOSE
	// Use len() to get buffered count since channel is buffered
	eoseCount := len(eoseChan)

	return events, eoseCount == len(relays)
}

func fetchReactionsFromRelay(ctx context.Context, relayURL string, eventIDs []string, eventChan chan<- Event, eoseChan chan<- bool) {
	subID := "react-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{7},
		"#e":    eventIDs,
		"limit": 500,
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		log.Printf("Failed to subscribe to %s for reactions: %v", relayURL, err)
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			log.Printf("Received EOSE from %s", relayURL)
			eoseChan <- true
			return
		}
	}
}

// fetchReplies fetches kind 1 replies to the given event IDs
func fetchReplies(relays []string, eventIDs []string) []Event {
	if len(eventIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 1000)

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchRepliesFromRelay(ctx, relayURL, eventIDs, eventChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
	}()

	seenIDs := make(map[string]bool)
	events := []Event{}

collectLoop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break collectLoop
			}
			if !seenIDs[evt.ID] {
				seenIDs[evt.ID] = true
				events = append(events, evt)
			}
		case <-ctx.Done():
			log.Printf("Replies fetch timeout, got %d events", len(events))
			break collectLoop
		}
	}

	log.Printf("Fetched %d replies for thread", len(events))
	return events
}

func fetchRepliesFromRelay(ctx context.Context, relayURL string, eventIDs []string, eventChan chan<- Event) {
	subID := "replies-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{1}, // Kind 1 = notes/replies
		"#e":    eventIDs,
		"limit": 100,
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		log.Printf("Failed to subscribe to %s for replies: %v", relayURL, err)
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			log.Printf("Received EOSE for replies from %s", relayURL)
			return
		}
	}
}

// fetchReplyCounts fetches reply counts for the given event IDs
func fetchReplyCounts(relays []string, eventIDs []string) map[string]int {
	if len(eventIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 1000)

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchRepliesFromRelay(ctx, relayURL, eventIDs, eventChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
	}()

	seenIDs := make(map[string]bool)
	replyCounts := make(map[string]int)

collectLoop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break collectLoop
			}
			if seenIDs[evt.ID] {
				continue
			}
			seenIDs[evt.ID] = true

			// Find the event being replied to (last "e" tag)
			var targetEventID string
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					targetEventID = tag[1]
				}
			}
			if targetEventID != "" {
				replyCounts[targetEventID]++
			}
		case <-ctx.Done():
			break collectLoop
		}
	}

	return replyCounts
}

// RelayList represents a user's NIP-65 relay list
type RelayList struct {
	Read  []string // Relays where user reads mentions
	Write []string // Relays where user writes events
}

// fetchRelayList fetches a user's kind:10002 relay list metadata
// Uses global cache to avoid repeated lookups
func fetchRelayList(pubkey string) *RelayList {
	// Check cache first
	if relayList, notFound, ok := relayListCache.Get(pubkey); ok {
		if notFound {
			log.Printf("Relay list cache hit (not found) for %s", shortID(pubkey))
			return nil
		}
		log.Printf("Relay list cache hit for %s", shortID(pubkey))
		return relayList
	}

	// Use well-known indexer relays to find relay lists
	indexerRelays := []string{
		"wss://purplepag.es",
		"wss://relay.nostr.band",
		"wss://relay.damus.io",
	}

	filter := Filter{
		Authors: []string{pubkey},
		Kinds:   []int{10002},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(indexerRelays, filter, 2*time.Second)
	if len(events) == 0 {
		log.Printf("No relay list found for %s", shortID(pubkey))
		// Cache the "not found" result
		relayListCache.Set(pubkey, nil)
		return nil
	}

	// Parse the relay list from tags
	relayList := &RelayList{
		Read:  []string{},
		Write: []string{},
	}

	for _, tag := range events[0].Tags {
		if len(tag) < 2 || tag[0] != "r" {
			continue
		}

		relayURL := tag[1]
		marker := ""
		if len(tag) >= 3 {
			marker = tag[2]
		}

		switch marker {
		case "read":
			relayList.Read = append(relayList.Read, relayURL)
		case "write":
			relayList.Write = append(relayList.Write, relayURL)
		default:
			// No marker means both read and write
			relayList.Read = append(relayList.Read, relayURL)
			relayList.Write = append(relayList.Write, relayURL)
		}
	}

	log.Printf("Found relay list for %s: %d read, %d write relays", shortID(pubkey), len(relayList.Read), len(relayList.Write))

	// Cache the result
	relayListCache.Set(pubkey, relayList)

	return relayList
}

// fetchContactList fetches a user's kind:3 contact list (who they follow)
func fetchContactList(relays []string, pubkey string) []string {
	filter := Filter{
		Authors: []string{pubkey},
		Kinds:   []int{3},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 3*time.Second)
	if len(events) == 0 {
		log.Printf("No contact list found for %s", shortID(pubkey))
		return nil
	}

	// Parse contacts from p tags
	var contacts []string
	for _, tag := range events[0].Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			contacts = append(contacts, tag[1])
		}
	}

	log.Printf("Found %d contacts for %s", len(contacts), shortID(pubkey))
	return contacts
}

// NotificationType indicates what kind of notification this is
type NotificationType string

const (
	NotificationMention NotificationType = "mention"
	NotificationReply   NotificationType = "reply"
	NotificationReaction NotificationType = "reaction"
	NotificationRepost  NotificationType = "repost"
)

// Notification represents a notification event with its type
type Notification struct {
	Event   Event
	Type    NotificationType
	// For reactions/reposts, this is the event being reacted to/reposted
	TargetEventID string
}

// fetchNotifications fetches notifications for a user (events where they are p-tagged)
// Returns mentions (kind 1), replies (kind 1 with e-tag), reactions (kind 7), and reposts (kind 6)
// If until is provided, only fetches events before that timestamp (for pagination)
func fetchNotifications(relays []string, userPubkey string, limit int, until *int64) []Notification {
	// Fetch events where user is p-tagged
	// kinds: 1 (mentions/replies), 6 (reposts), 7 (reactions)
	filter := Filter{
		PTags: []string{userPubkey},
		Kinds: []int{1, 6, 7},
		Limit: limit * 2, // Fetch more to filter out self-notifications
		Until: until,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 3*time.Second)

	// Convert to notifications, filtering out self-notifications
	notifications := make([]Notification, 0, len(events))
	for _, evt := range events {
		// Skip events from the user themselves
		if evt.PubKey == userPubkey {
			continue
		}

		notif := Notification{
			Event: evt,
		}

		switch evt.Kind {
		case 1:
			// Check if it's a reply (has e-tag) or a mention
			hasETag := false
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					hasETag = true
					notif.TargetEventID = tag[1]
					break
				}
			}
			if hasETag {
				notif.Type = NotificationReply
			} else {
				notif.Type = NotificationMention
			}

		case 6:
			notif.Type = NotificationRepost
			// Get the reposted event ID from e-tag
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					notif.TargetEventID = tag[1]
					break
				}
			}

		case 7:
			notif.Type = NotificationReaction
			// Get the reacted event ID from e-tag
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					notif.TargetEventID = tag[1]
					break
				}
			}
		}

		notifications = append(notifications, notif)
	}

	// Apply limit after filtering
	if len(notifications) > limit {
		notifications = notifications[:limit]
	}

	log.Printf("Fetched %d notifications for %s", len(notifications), shortID(userPubkey))
	return notifications
}

// hasUnreadNotifications checks if there are any notifications newer than the lastSeen timestamp
// This does a quick check by fetching just a few recent events
func hasUnreadNotifications(relays []string, userPubkey string, lastSeen int64) bool {
	// If lastSeen is 0, there are unread notifications (user hasn't visited yet)
	if lastSeen == 0 {
		return true
	}

	filter := Filter{
		PTags: []string{userPubkey},
		Kinds: []int{1, 6, 7}, // Notes, reposts, reactions
		Limit: 5,              // Just check a few recent events
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 2*time.Second)

	for _, ev := range events {
		// Skip self-notifications
		if ev.PubKey == userPubkey {
			continue
		}
		// If any event is newer than lastSeen, we have unread notifications
		if ev.CreatedAt > lastSeen {
			return true
		}
	}

	return false
}
