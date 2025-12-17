package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2/schnorr"
)

// parseBolt11Amount extracts the amount in satoshis from a bolt11 invoice.
// Returns 0 if amount cannot be parsed.
func parseBolt11Amount(bolt11 string) int64 {
	bolt11 = strings.ToLower(bolt11)
	// Match lnbc/lntb/lnbcrt followed by amount and optional multiplier
	re := regexp.MustCompile(`^ln(?:bc|tb|bcrt)(\d+)([munp])?`)
	matches := re.FindStringSubmatch(bolt11)
	if len(matches) < 2 {
		return 0
	}

	amount, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return 0
	}

	// Convert to satoshis based on multiplier
	// m = milli (10^-3 BTC = 100,000 sats)
	// u = micro (10^-6 BTC = 100 sats)
	// n = nano (10^-9 BTC = 0.1 sats)
	// p = pico (10^-12 BTC = 0.0001 sats)
	multiplier := ""
	if len(matches) >= 3 {
		multiplier = matches[2]
	}

	switch multiplier {
	case "m":
		return amount * 100000
	case "u":
		return amount * 100
	case "n":
		return amount / 10 // 0.1 sats per nano, round down
	case "p":
		return amount / 10000 // 0.0001 sats per pico, round down
	default:
		// No multiplier = BTC
		return amount * 100000000
	}
}

// isCustomEmojiShortcode returns true for :shortcode: emoji (can't render without image URL)
func isCustomEmojiShortcode(reaction string) bool {
	return len(reaction) >= 3 && strings.HasPrefix(reaction, ":") && strings.HasSuffix(reaction, ":")
}

// shortID truncates ID/pubkey to 12 chars for logging
func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}

// truncateString limits string length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

type Filter struct {
	IDs     []string
	Authors []string
	Kinds   []int
	Limit   int
	Since   *int64
	Until   *int64
	PTags   []string // #p tag filter (mentions)
	ATags   []string // #a tag filter (addressable events)
	Search  string   // NIP-50 search query
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

// parseEventFromInterface converts raw websocket data to Event (avoids JSON re-encoding)
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

	if evt.Sig != "" && !validateEventSignature(&evt) {
		slog.Warn("event signature validation failed", "event_id", shortID(evt.ID))
		return Event{}, false
	}

	return evt, evt.ID != ""
}

// validateEventSignature verifies Schnorr signature
func validateEventSignature(evt *Event) bool {
	if len(evt.Sig) != 128 || len(evt.PubKey) != 64 {
		return false
	}

	sigBytes, err := hex.DecodeString(evt.Sig)
	if err != nil {
		return false
	}
	pubKeyBytes, err := hex.DecodeString(evt.PubKey)
	if err != nil {
		return false
	}
	idBytes, err := hex.DecodeString(evt.ID)
	if err != nil {
		return false
	}

	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		return false
	}
	pubKey, err := schnorr.ParsePubKey(pubKeyBytes)
	if err != nil {
		return false
	}

	return sig.Verify(idBytes, pubKey)
}

func fetchEventsFromRelays(relays []string, filter Filter) ([]Event, bool) {
	return fetchEventsFromRelaysWithTimeout(relays, filter, 1500*time.Millisecond)
}

// fetchEventsFromRelaysCached checks cache first, then fetches from relays
func fetchEventsFromRelaysCached(relays []string, filter Filter) ([]Event, bool) {
	return fetchEventsFromRelaysCachedWithOptions(relays, filter, false)
}

// fetchEventsFromRelaysCachedWithOptions checks cache first, then fetches from relays
// If cacheOnly is true, returns empty on cache miss instead of fetching from relays
func fetchEventsFromRelaysCachedWithOptions(relays []string, filter Filter, cacheOnly bool) ([]Event, bool) {
	if events, eose, ok := eventCache.Get(relays, filter); ok {
		IncrementCacheHit()
		slog.Debug("cache hit", "limit", filter.Limit, "authors", len(filter.Authors))
		return events, eose
	}

	IncrementCacheMiss()
	if cacheOnly {
		slog.Debug("cache miss (cache_only)", "limit", filter.Limit, "authors", len(filter.Authors))
		return []Event{}, true
	}

	slog.Debug("cache miss", "limit", filter.Limit, "authors", len(filter.Authors))
	events, eose := fetchEventsFromRelays(relays, filter)
	eventCache.Set(relays, filter, events, eose)

	return events, eose
}

func fetchEventsFromRelaysWithTimeout(relays []string, filter Filter, timeout time.Duration) ([]Event, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sortedRelays := relayHealth.SortRelaysByScore(relays)

	// Buffer size based on expected results: 3x limit for deduplication overhead
	// Min 100 to prevent blocking, max 500 to cap memory
	bufferSize := filter.Limit * 3
	if bufferSize < 100 {
		bufferSize = 100
	}
	if bufferSize > 500 {
		bufferSize = 500
	}

	var wg sync.WaitGroup
	eventChan := make(chan Event, bufferSize)
	eoseChan := make(chan string, len(sortedRelays))

	for _, relay := range sortedRelays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchFromRelayWithURL(ctx, relayURL, filter, eventChan, eoseChan)
		}(relay)
	}

	// Close channels when all goroutines complete
	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	seenIDs := make(map[string]bool)
	seenAuthors := make(map[string]bool) // Track author diversity
	events := []Event{}
	// Early exit when we have enough unique events (limit + small buffer for sorting)
	targetCount := filter.Limit + 20
	if targetCount < filter.Limit {
		targetCount = filter.Limit // Overflow protection
	}
	eoseCount := 0
	eoseRelays := make([]string, 0, len(sortedRelays))

	// For multi-author feeds (follows), require diversity before early exit
	// Single-author or small author lists can exit faster
	isMultiAuthorFeed := len(filter.Authors) > 10
	minAuthorsForEarlyExit := 5 // Require events from at least 5 different authors
	minEOSEForMultiAuthor := 2  // Require at least 2 relays to respond

	// Start grace period after first relay responds (don't wait for slow relays)
	minResponses := 1
	if isMultiAuthorFeed {
		minResponses = 2 // Multi-author feeds wait for 2 relays
	}
	expectedTime := relayHealth.GetExpectedResponseTime(sortedRelays, minResponses)
	gracePeriod := time.Duration(float64(expectedTime) * 1.2)
	if gracePeriod < 100*time.Millisecond {
		gracePeriod = 100 * time.Millisecond
	}
	if gracePeriod > 400*time.Millisecond {
		gracePeriod = 400 * time.Millisecond
	}

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
				seenAuthors[evt.PubKey] = true
				events = append(events, evt)

				// Check if we can early exit
				hasEnoughEvents := len(events) >= filter.Limit
				hasAuthorDiversity := !isMultiAuthorFeed || len(seenAuthors) >= minAuthorsForEarlyExit
				hasEnoughRelays := !isMultiAuthorFeed || eoseCount >= minEOSEForMultiAuthor

				// Early exit when we have enough unique events
				if len(events) >= targetCount && hasAuthorDiversity {
					slog.Debug("event fetch: got enough events, returning early",
						"count", len(events), "target", targetCount, "authors", len(seenAuthors))
					cancel()
					break collectLoop
				}
				// Also exit early if we have limit events, diversity, and relay responses
				if hasEnoughEvents && hasAuthorDiversity && hasEnoughRelays {
					slog.Debug("event fetch: got limit events with diversity, returning early",
						"count", len(events), "limit", filter.Limit,
						"authors", len(seenAuthors), "eose_count", eoseCount)
					cancel()
					break collectLoop
				}
			}
		case relayURL := <-eoseChan:
			eoseCount++
			eoseRelays = append(eoseRelays, relayURL)

			// Start grace period after enough relays respond
			if eoseCount >= minResponses && graceTimer == nil {
				graceTimer = time.After(gracePeriod)
			}

			// Check if we can exit now that a relay finished
			hasEnoughEvents := len(events) >= filter.Limit
			hasAuthorDiversity := !isMultiAuthorFeed || len(seenAuthors) >= minAuthorsForEarlyExit
			hasEnoughRelays := !isMultiAuthorFeed || eoseCount >= minEOSEForMultiAuthor

			if hasEnoughEvents && hasAuthorDiversity && hasEnoughRelays {
				slog.Debug("event fetch: relay finished with enough diverse events",
					"count", len(events), "limit", filter.Limit,
					"authors", len(seenAuthors), "relay", relayURL)
				cancel()
				break collectLoop
			}
			if eoseCount >= len(sortedRelays) {
				break collectLoop
			}
		case <-graceTimer:
			break collectLoop
		case <-ctx.Done():
			break collectLoop
		}
	}

	allEOSE := eoseCount == len(sortedRelays)

	sort.Slice(events, func(i, j int) bool { // Newest first, ID for tie-break
		if events[i].CreatedAt != events[j].CreatedAt {
			return events[i].CreatedAt > events[j].CreatedAt
		}
		return events[i].ID > events[j].ID
	})

	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[:filter.Limit]
	}

	return events, allEOSE
}

// fetchProfileEventsFromRelays fetches kind 0 profile events with early exit when all pubkeys found
// This is optimized for profile fetches where we want exactly 1 event per pubkey
func fetchProfileEventsFromRelays(relays []string, pubkeys []string, timeout time.Duration) []Event {
	if len(pubkeys) == 0 || len(relays) == 0 {
		return nil
	}

	filter := Filter{
		Authors: pubkeys,
		Kinds:   []int{0},
		Limit:   len(pubkeys),
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sortedRelays := relayHealth.SortRelaysByScore(relays)

	bufferSize := len(pubkeys) * 2 // Small buffer for profiles
	if bufferSize < 20 {
		bufferSize = 20
	}

	var wg sync.WaitGroup
	eventChan := make(chan Event, bufferSize)
	eoseChan := make(chan string, len(sortedRelays))

	for _, relay := range sortedRelays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchFromRelayWithURL(ctx, relayURL, filter, eventChan, eoseChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	// Track which pubkeys we've found - exit early when all found
	foundPubkeys := make(map[string]bool)
	targetPubkeys := make(map[string]bool)
	for _, pk := range pubkeys {
		targetPubkeys[pk] = true
	}

	events := []Event{}
	eoseCount := 0

	// Shorter grace period for profiles since we know exactly what we're looking for
	gracePeriod := 150 * time.Millisecond
	var graceTimer <-chan time.Time

collectLoop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break collectLoop
			}
			// Only accept kind 0 events for pubkeys we're looking for
			if evt.Kind == 0 && targetPubkeys[evt.PubKey] && !foundPubkeys[evt.PubKey] {
				foundPubkeys[evt.PubKey] = true
				events = append(events, evt)

				// Early exit: found all requested profiles
				if len(foundPubkeys) >= len(pubkeys) {
					slog.Debug("profile fetch: found all pubkeys, returning early",
						"found", len(foundPubkeys), "requested", len(pubkeys))
					cancel()
					break collectLoop
				}
			}
		case <-eoseChan:
			eoseCount++
			if eoseCount >= 1 && graceTimer == nil {
				// Start grace period after first EOSE
				graceTimer = time.After(gracePeriod)
			}
			if eoseCount >= len(sortedRelays) {
				break collectLoop
			}
		case <-graceTimer:
			// Only exit on grace timer if we have at least some results
			if len(events) > 0 {
				break collectLoop
			}
		case <-ctx.Done():
			break collectLoop
		}
	}

	return events
}

func fetchFromRelayWithURL(ctx context.Context, relayURL string, filter Filter, eventChan chan<- Event, eoseChan chan<- string) {
	startTime := time.Now()
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
	if len(filter.ATags) > 0 {
		reqFilter["#a"] = filter.ATags
	}
	if filter.Search != "" {
		reqFilter["search"] = filter.Search
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		slog.Debug("subscribe failed", "relay", relayURL, "error", err)
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
			responseTime := time.Since(startTime)
			relayHealth.recordResponseTime(relayURL, responseTime)
			eoseChan <- relayURL
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

// fetchEventByID fetches a single event by ID
func fetchEventByID(relays []string, eventID string) []Event {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

// fetchAddressableEvent fetches a replaceable event by kind, author, and d-tag
func fetchAddressableEvent(relays []string, kind uint32, author string, dTag string) []Event {
	filter := Filter{
		Kinds:   []int{int(kind)},
		Authors: []string{author},
		Limit:   10, // Fetch a few in case author has multiple
	}

	events, _ := fetchEventsFromRelays(relays, filter)

	for i := range events { // Find matching d-tag
		ev := &events[i]
		evDTag := ""
		for _, tag := range ev.Tags {
			if len(tag) >= 2 && tag[0] == "d" {
				evDTag = tag[1]
				break
			}
		}
		if evDTag == dTag {
			return []Event{*ev}
		}
	}

	return nil
}

// fetchProfiles fetches kind 0 profiles (uses cache, tries profile relays first)
func fetchProfiles(relays []string, pubkeys []string) map[string]*ProfileInfo {
	return fetchProfilesWithOptions(relays, pubkeys, false)
}

// fetchProfilesWithOptions fetches kind 0 profiles with cacheOnly option
// If cacheOnly is true, returns only cached profiles without fetching from relays
// Priority: relays parameter (NIP-65) first, then profileRelays as fallback
func fetchProfilesWithOptions(relays []string, pubkeys []string, cacheOnly bool) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := profileCache.GetMultiple(pubkeys)
	if len(missing) == 0 {
		IncrementCacheHit()
		return cached
	}

	IncrementCacheMiss()
	if cacheOnly {
		return cached
	}

	var events []Event
	foundPubkeys := make(map[string]bool)

	// Stage 1: Try user's relays first (NIP-65 read relays when logged in)
	// Uses optimized profile fetch with early exit when all pubkeys found
	if len(relays) > 0 {
		primaryEvents := fetchProfileEventsFromRelays(relays, missing, 1500*time.Millisecond)
		events = append(events, primaryEvents...)
		for _, evt := range primaryEvents {
			foundPubkeys[evt.PubKey] = true
		}
	}

	// Stage 2: Fall back to profileRelays (aggregators) for any still missing
	var stillMissing []string
	for _, pk := range missing {
		if !foundPubkeys[pk] {
			stillMissing = append(stillMissing, pk)
		}
	}

	if len(stillMissing) > 0 {
		profileRelays := ConfigGetProfileRelays()
		fallbackEvents := fetchProfileEventsFromRelays(profileRelays, stillMissing, 2000*time.Millisecond)
		events = append(events, fallbackEvents...)
	}

	freshProfiles := make(map[string]*ProfileInfo)
	for _, evt := range events {
		if evt.Kind != 0 {
			continue
		}
		if _, ok := freshProfiles[evt.PubKey]; ok { // Keep newest only
			continue
		}

		var profileData map[string]interface{}
		if err := json.Unmarshal([]byte(evt.Content), &profileData); err != nil {
			slog.Debug("invalid profile JSON", "pubkey", shortID(evt.PubKey), "error", err)
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
		if lud16, ok := profileData["lud16"].(string); ok {
			profile.Lud16 = truncateString(lud16, 200)
		}
		if lud06, ok := profileData["lud06"].(string); ok {
			profile.Lud06 = truncateString(lud06, 500)
		}

		freshProfiles[evt.PubKey] = profile
	}

	if len(freshProfiles) > 0 {
		profileCache.SetMultiple(freshProfiles)
	}

	var notFound []string // Cache "not found" to avoid repeated lookups
	for _, pk := range missing {
		if _, ok := freshProfiles[pk]; !ok {
			notFound = append(notFound, pk)
		}
	}
	if len(notFound) > 0 {
		profileCache.SetNotFound(notFound)
	}

	result := make(map[string]*ProfileInfo, len(cached)+len(freshProfiles))
	for pk, p := range cached {
		result[pk] = p
	}
	for pk, p := range freshProfiles {
		result[pk] = p
	}

	return result
}

// fetchProfilesWithTimeout fetches kind 0 profiles with a single timeout (for quick avatar lookups)
// Skips the two-stage relay fallback of fetchProfilesWithOptions for faster response
func fetchProfilesWithTimeout(relays []string, pubkeys []string, timeout time.Duration) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := profileCache.GetMultiple(pubkeys)
	if len(missing) == 0 {
		IncrementCacheHit()
		return cached
	}

	IncrementCacheMiss()

	filter := Filter{
		Authors: missing,
		Kinds:   []int{0},
		Limit:   len(missing),
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, timeout)

	freshProfiles := make(map[string]*ProfileInfo)
	for _, evt := range events {
		if evt.Kind != 0 {
			continue
		}
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
		if lud16, ok := profileData["lud16"].(string); ok {
			profile.Lud16 = truncateString(lud16, 200)
		}
		if lud06, ok := profileData["lud06"].(string); ok {
			profile.Lud06 = truncateString(lud06, 500)
		}

		freshProfiles[evt.PubKey] = profile
	}

	if len(freshProfiles) > 0 {
		profileCache.SetMultiple(freshProfiles)
	}

	// Don't cache "not found" for quick lookups - let full fetch try again
	result := make(map[string]*ProfileInfo, len(cached)+len(freshProfiles))
	for pk, p := range cached {
		result[pk] = p
	}
	for pk, p := range freshProfiles {
		result[pk] = p
	}

	return result
}

// fetchReactions fetches kind 7 reactions for event IDs
func fetchReactions(relays []string, eventIDs []string) map[string]*ReactionsSummary {
	if len(eventIDs) == 0 {
		return nil
	}

	eventIDSet := make(map[string]bool, len(eventIDs))
	for _, id := range eventIDs {
		eventIDSet[id] = true
	}

	events, _ := fetchEventsFromRelaysWithETags(relays, eventIDs)
	reactions := make(map[string]*ReactionsSummary)
	for _, evt := range events {
		if evt.Kind != 7 {
			continue
		}

		var targetEventID string // Last e-tag is target
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				targetEventID = tag[1]
			}
		}

		if targetEventID == "" || !eventIDSet[targetEventID] {
			continue
		}

		summary, ok := reactions[targetEventID]
		if !ok {
			summary = &ReactionsSummary{
				Total:  0,
				ByType: make(map[string]int),
			}
			reactions[targetEventID] = summary
		}

		summary.Total++
		reactionType := evt.Content
		if reactionType == "" || reactionType == "+" { // Normalize to ❤️
			reactionType = "❤️"
		}
		if isCustomEmojiShortcode(reactionType) { // Skip unrenderable custom emoji
			continue
		}
		summary.ByType[reactionType]++
	}

	return reactions
}

// fetchEventsFromRelaysWithETags fetches events with #e tags matching eventIDs
func fetchEventsFromRelaysWithETags(relays []string, eventIDs []string) ([]Event, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 300) // Bounded by queried events
	eoseChan := make(chan string, len(relays))

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
	eoseCount := 0

	// EOSE-based early termination (same pattern as main event fetch)
	minResponses := 2
	if len(relays) < minResponses {
		minResponses = len(relays)
	}
	gracePeriod := 500 * time.Millisecond // Fixed grace period for reactions
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
			}
		case relayURL := <-eoseChan:
			eoseCount++
			_ = relayURL // Used for tracking

			if eoseCount >= minResponses && graceTimer == nil {
				graceTimer = time.After(gracePeriod)
			}
			if eoseCount >= len(relays) {
				break collectLoop
			}
		case <-graceTimer:
			break collectLoop
		case <-ctx.Done():
			break collectLoop
		}
	}

	return events, eoseCount == len(relays)
}

func fetchReactionsFromRelay(ctx context.Context, relayURL string, eventIDs []string, eventChan chan<- Event, eoseChan chan<- string) {
	subID := "react-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{7},
		"#e":    eventIDs,
		"limit": 500,
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
			eoseChan <- relayURL
			return
		}
	}
}

// fetchReplies fetches kind 1 replies to event IDs
func fetchReplies(relays []string, eventIDs []string) []Event {
	if len(eventIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 300) // Bounded by queried events

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
			break collectLoop
		}
	}

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

// fetchRepliesSince fetches kind 1 replies created after the given timestamp
func fetchRepliesSince(relays []string, eventIDs []string, since int64) []Event {
	if len(eventIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 200)

	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			fetchRepliesSinceFromRelay(ctx, relayURL, eventIDs, since, eventChan)
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
			break collectLoop
		}
	}

	return events
}

func fetchRepliesSinceFromRelay(ctx context.Context, relayURL string, eventIDs []string, since int64, eventChan chan<- Event) {
	subID := "replies-since-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{1},
		"#e":    eventIDs,
		"since": since,
		"limit": 50,
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

// fetchReplyCounts counts replies for event IDs
func fetchReplyCounts(relays []string, eventIDs []string) map[string]int {
	if len(eventIDs) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	eventChan := make(chan Event, 300) // Bounded by queried events

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

			var targetEventID string // Last e-tag is target
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
	Read  []string
	Write []string
}

// fetchRelayList fetches kind:10002 relay list (cached)
func fetchRelayList(pubkey string) *RelayList {
	if relayList, notFound, ok := relayListCache.Get(pubkey); ok {
		IncrementCacheHit()
		if notFound {
			return nil
		}
		return relayList
	}

	IncrementCacheMiss()
	indexerRelays := ConfigGetDefaultRelays()

	filter := Filter{
		Authors: []string{pubkey},
		Kinds:   []int{10002},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(indexerRelays, filter, 2*time.Second)
	if len(events) == 0 {
		relayListCache.Set(pubkey, nil)
		return nil
	}

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
		default: // No marker = both read and write
			relayList.Read = append(relayList.Read, relayURL)
			relayList.Write = append(relayList.Write, relayURL)
		}
	}

	relayListCache.Set(pubkey, relayList)

	return relayList
}

// fetchContactList fetches kind:3 contact list (follows)
func fetchContactList(relays []string, pubkey string) []string {
	filter := Filter{
		Authors: []string{pubkey},
		Kinds:   []int{3},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 3*time.Second)
	if len(events) == 0 {
		return nil
	}

	var contacts []string
	for _, tag := range events[0].Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			contacts = append(contacts, tag[1])
		}
	}

	return contacts
}

type NotificationType string

const (
	NotificationMention  NotificationType = "mention"
	NotificationReply    NotificationType = "reply"
	NotificationReaction NotificationType = "reaction"
	NotificationRepost   NotificationType = "repost"
	NotificationZap      NotificationType = "zap"
)

type Notification struct {
	Event            Event
	Type             NotificationType
	TargetEventID    string // Event being reacted to/reposted/zapped
	ZapAmountSats    int64  // Zap amount (from zap request)
	ZapSenderPubkey  string // Zap sender (from zap request)
}

// parseEventToNotification converts an event to a notification with type detection
func parseEventToNotification(evt Event) Notification {
	notif := Notification{
		Event: evt,
	}

	switch evt.Kind {
	case 1: // Note: reply if has e-tag, otherwise mention
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

	case 6: // Repost
		notif.Type = NotificationRepost
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				notif.TargetEventID = tag[1]
				break
			}
		}

	case 7: // Reaction
		notif.Type = NotificationReaction
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				notif.TargetEventID = tag[1]
				break
			}
		}

	case 9735: // Zap receipt
		notif.Type = NotificationZap
		var bolt11 string
		for _, tag := range evt.Tags {
			if len(tag) >= 2 {
				switch tag[0] {
				case "e":
					if notif.TargetEventID == "" {
						notif.TargetEventID = tag[1]
					}
				case "bolt11":
					bolt11 = tag[1]
				case "description": // Contains zap request JSON with sender pubkey and amount
					var zapRequest struct {
						PubKey string     `json:"pubkey"`
						Tags   [][]string `json:"tags"`
					}
					if err := json.Unmarshal([]byte(tag[1]), &zapRequest); err == nil {
						notif.ZapSenderPubkey = zapRequest.PubKey
						// Extract amount from zap request tags
						for _, reqTag := range zapRequest.Tags {
							if len(reqTag) >= 2 && reqTag[0] == "amount" {
								if msats, err := strconv.ParseInt(reqTag[1], 10, 64); err == nil {
									notif.ZapAmountSats = msats / 1000
								}
								break
							}
						}
					}
				}
			}
		}
		// Fallback: parse amount from bolt11 invoice if not found in zap request
		if notif.ZapAmountSats == 0 && bolt11 != "" {
			notif.ZapAmountSats = parseBolt11Amount(bolt11)
		}
	}

	return notif
}

// eventsToNotifications converts events to notifications, filtering self-notifications
func eventsToNotifications(events []Event, userPubkey string) []Notification {
	notifications := make([]Notification, 0, len(events))
	for _, evt := range events {
		if evt.PubKey == userPubkey { // Skip self-notifications
			continue
		}
		notifications = append(notifications, parseEventToNotification(evt))
	}
	return notifications
}

// notificationsToCached converts Notifications to CachedNotifications format
func notificationsToCached(notifications []Notification) []CachedNotification {
	cached := make([]CachedNotification, len(notifications))
	for i, n := range notifications {
		cached[i] = CachedNotification{
			Event:           n.Event,
			Type:            string(n.Type),
			TargetEventID:   n.TargetEventID,
			ZapSenderPubkey: n.ZapSenderPubkey,
			ZapAmountSats:   n.ZapAmountSats,
		}
	}
	return cached
}

// cachedToNotifications converts CachedNotifications format back to Notifications
func cachedToNotifications(cached []CachedNotification) []Notification {
	notifications := make([]Notification, len(cached))
	for i, c := range cached {
		notifications[i] = Notification{
			Event:           c.Event,
			Type:            NotificationType(c.Type),
			TargetEventID:   c.TargetEventID,
			ZapSenderPubkey: c.ZapSenderPubkey,
			ZapAmountSats:   c.ZapAmountSats,
		}
	}
	return notifications
}

// fetchNotificationsWithOptions fetches notifications with caching and incremental updates
// Returns notifications and a hasMore flag indicating if more are available beyond limit
func fetchNotificationsWithOptions(relays []string, userPubkey string, limit int, until *int64, cacheOnly bool) ([]Notification, bool) {
	ctx := context.Background()

	// Pagination (until set) skips cache - we're fetching older notifications
	if until != nil {
		return fetchNotificationsFromRelays(relays, userPubkey, limit, until, cacheOnly)
	}

	// Check cache first
	cached, found, _ := notificationCacheStore.Get(ctx, userPubkey)
	if found && cached != nil {
		slog.Debug("notification cache hit", "pubkey", userPubkey[:12], "cached_count", len(cached.Notifications))

		// Return cached notifications
		notifications := cachedToNotifications(cached.Notifications)

		// If cacheOnly, just return cached data
		if cacheOnly {
			hasMore := len(notifications) > limit
			if hasMore {
				notifications = notifications[:limit]
			}
			return notifications, hasMore
		}

		// Fetch new notifications since cache time (incremental update)
		since := cached.NewestSeen
		if since > 0 {
			newNotifications := fetchNotificationsSince(relays, userPubkey, since)
			if len(newNotifications) > 0 {
				slog.Debug("notification cache incremental update", "new_count", len(newNotifications))

				// Merge: new notifications first, then cached (dedupe by event ID)
				seenIDs := make(map[string]bool)
				merged := make([]Notification, 0, len(newNotifications)+len(notifications))

				for _, n := range newNotifications {
					if !seenIDs[n.Event.ID] {
						seenIDs[n.Event.ID] = true
						merged = append(merged, n)
					}
				}
				for _, n := range notifications {
					if !seenIDs[n.Event.ID] {
						seenIDs[n.Event.ID] = true
						merged = append(merged, n)
					}
				}
				notifications = merged

				// Update cache with merged results
				newestSeen := cached.NewestSeen
				if len(newNotifications) > 0 && newNotifications[0].Event.CreatedAt > newestSeen {
					newestSeen = newNotifications[0].Event.CreatedAt
				}
				updatedCache := &CachedNotifications{
					Notifications: notificationsToCached(notifications),
					NewestSeen:    newestSeen,
					CachedAt:      time.Now().Unix(),
				}
				notificationCacheStore.Set(ctx, userPubkey, updatedCache, cacheConfig.NotificationCacheTTL)
			}
		}

		hasMore := len(notifications) > limit
		if hasMore {
			notifications = notifications[:limit]
		}
		return notifications, hasMore
	}

	// Cache miss - fetch from relays
	slog.Debug("notification cache miss", "pubkey", userPubkey[:12])
	notifications, hasMore := fetchNotificationsFromRelays(relays, userPubkey, limit, nil, cacheOnly)

	// Cache the results (only if we got results and not cacheOnly)
	if len(notifications) > 0 && !cacheOnly {
		newestSeen := notifications[0].Event.CreatedAt
		newCache := &CachedNotifications{
			Notifications: notificationsToCached(notifications),
			NewestSeen:    newestSeen,
			CachedAt:      time.Now().Unix(),
		}
		notificationCacheStore.Set(ctx, userPubkey, newCache, cacheConfig.NotificationCacheTTL)
	}

	return notifications, hasMore
}

// fetchNotificationsFromRelays fetches notifications directly from relays
// Returns notifications and a hasMore flag indicating if more are available
func fetchNotificationsFromRelays(relays []string, userPubkey string, limit int, until *int64, cacheOnly bool) ([]Notification, bool) {
	filter := Filter{
		PTags: []string{userPubkey},
		Kinds: []int{1, 6, 7, 9735},
		Limit: limit * 2, // Fetch more to filter out self-notifications
		Until: until,
	}

	var events []Event
	if cacheOnly {
		events, _ = fetchEventsFromRelaysCachedWithOptions(relays, filter, true)
	} else {
		events, _ = fetchEventsFromRelaysWithTimeout(relays, filter, 5*time.Second)
	}

	notifications := eventsToNotifications(events, userPubkey)

	hasMore := len(notifications) > limit
	if hasMore {
		notifications = notifications[:limit]
	}

	return notifications, hasMore
}

// fetchNotificationsSince fetches notifications newer than the given timestamp
func fetchNotificationsSince(relays []string, userPubkey string, since int64) []Notification {
	sinceTime := since + 1 // Exclusive of the since timestamp
	filter := Filter{
		PTags: []string{userPubkey},
		Kinds: []int{1, 6, 7, 9735},
		Since: &sinceTime,
		Limit: 100, // Reasonable limit for incremental updates
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 5*time.Second)
	return eventsToNotifications(events, userPubkey)
}

// hasUnreadNotifications checks for notifications newer than lastSeen
func hasUnreadNotifications(relays []string, userPubkey string, lastSeen int64) bool {
	if lastSeen == 0 { // Never visited = has unread
		return true
	}

	filter := Filter{
		PTags: []string{userPubkey},
		Kinds: []int{1, 6, 7, 9735},
		Limit: 5,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 2*time.Second)

	for _, ev := range events {
		if ev.PubKey == userPubkey {
			continue
		}
		if ev.CreatedAt > lastSeen {
			return true
		}
	}

	return false
}
