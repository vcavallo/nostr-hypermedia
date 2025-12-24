package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nostr-server/internal/config"
	"nostr-server/internal/nips"
	"nostr-server/internal/nostr"
	"nostr-server/internal/types"
	"nostr-server/internal/util"
)

// Timeout constants for relay operations
// Using tiered timeouts based on operation complexity
const (
	// Quick operations: existence checks, small lookups
	timeoutQuick = 2 * time.Second

	// Standard operations: profile fetches, relay lists
	timeoutStandard = 3 * time.Second

	// Extended operations: full timeline fetches, notifications
	timeoutExtended = 5 * time.Second
)

// bolt11AmountRegex is pre-compiled for performance (used in zap receipt parsing)
var bolt11AmountRegex = regexp.MustCompile(`^ln(?:bc|tb|bcrt)(\d+)([munp])?`)

// parseBolt11Amount extracts the amount in satoshis from a bolt11 invoice.
// Returns 0 if amount cannot be parsed or would overflow.
func parseBolt11Amount(bolt11 string) int64 {
	bolt11 = strings.ToLower(bolt11)
	matches := bolt11AmountRegex.FindStringSubmatch(bolt11)
	if len(matches) < 2 {
		return 0
	}

	amount, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil || amount < 0 {
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

	// Check for overflow before multiplication
	switch multiplier {
	case "m":
		if amount > math.MaxInt64/100000 {
			return 0 // Would overflow
		}
		return amount * 100000
	case "u":
		if amount > math.MaxInt64/100 {
			return 0
		}
		return amount * 100
	case "n":
		return amount / 10 // 0.1 sats per nano, round down
	case "p":
		return amount / 10000 // 0.0001 sats per pico, round down
	default:
		// No multiplier = BTC
		if amount > math.MaxInt64/100000000 {
			return 0
		}
		return amount * 100000000
	}
}

// isCustomEmojiShortcode returns true for :shortcode: emoji (can't render without image URL)
func isCustomEmojiShortcode(reaction string) bool {
	return len(reaction) >= 3 && strings.HasPrefix(reaction, ":") && strings.HasSuffix(reaction, ":")
}

// truncateString limits string length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// Type aliases for internal/types (allows gradual migration)
type Filter = types.Filter
type Event = types.Event
type NostrMessage = types.NostrMessage

// Function aliases for internal/nostr
var parseEventFromInterface = nostr.ParseEventFromInterface
var validateEventSignature = nostr.ValidateEventSignature
var shortID = nostr.ShortID

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

	sortedRelays := relayHealthStore.SortRelaysByScore(relays)

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
	events := make([]Event, 0, filter.Limit)
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
	expectedTime := relayHealthStore.GetExpectedResponseTime(sortedRelays, minResponses)
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
// Returns events and a bool indicating if at least one relay responded (for not-found caching decisions)
func fetchProfileEventsFromRelays(relays []string, pubkeys []string, timeout time.Duration) ([]Event, bool) {
	if len(pubkeys) == 0 || len(relays) == 0 {
		return nil, false
	}

	filter := Filter{
		Authors: pubkeys,
		Kinds:   []int{0},
		Limit:   len(pubkeys),
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	sortedRelays := relayHealthStore.SortRelaysByScore(relays)

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

	events := make([]Event, 0, len(pubkeys))
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

	// Return events and whether any relay responded (eoseCount > 0)
	// This helps callers decide whether to cache "not found" results
	hadResponse := eoseCount > 0
	return events, hadResponse
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
	if len(filter.DTags) > 0 {
		reqFilter["#d"] = filter.DTags
	}
	if len(filter.KTags) > 0 {
		reqFilter["#k"] = filter.KTags
	}
	if len(filter.TTags) > 0 {
		reqFilter["#t"] = filter.TTags
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
			relayHealthStore.recordResponseTime(relayURL, responseTime)
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
	events := make([]Event, 0, 5) // Single event fetch, small capacity

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

// fetchProfilesWithOptionsDirect fetches kind 0 profiles directly without singleflight.
// If cacheOnly is true, returns only cached profiles without fetching from relays.
// Uses NIP-65 outbox model: fetches profiles from users' WRITE relays when relay lists are cached,
// falls back to aggregator relays (profileRelays) for users without cached relay lists.
// Note: The relays parameter is kept for API compatibility but is no longer used - the function
// now determines the correct relays to query based on each target user's cached relay list.
func fetchProfilesWithOptionsDirect(_ []string, pubkeys []string, cacheOnly bool) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := profileCache.GetMultiple(pubkeys)
	slog.Debug("profile cache check", "requested", len(pubkeys), "cached", len(cached), "missing", len(missing))
	if len(missing) == 0 {
		IncrementCacheHit()
		return cached
	}

	IncrementCacheMiss()
	if cacheOnly {
		slog.Debug("profile cache only mode, returning cached", "cached", len(cached), "missing", len(missing))
		return cached
	}

	var events []Event
	foundPubkeys := make(map[string]bool)
	hadAnyRelayResponse := false // Track if at least one relay responded (for not-found caching)

	// Check relay list cache to determine which pubkeys have known WRITE relays (outbox model)
	// This is a cache-only check - no network calls
	cachedRelayLists, _ := relayListCache.GetMultiple(missing)

	// Build outbox relays for pubkeys with cached relay lists
	var outboxRelays []string
	var outboxPubkeys []string
	var aggregatorPubkeys []string

	for _, pk := range missing {
		if rl, ok := cachedRelayLists[pk]; ok && rl != nil && len(rl.Write) > 0 {
			outboxPubkeys = append(outboxPubkeys, pk)
			// Add up to 2 WRITE relays per author (sorted by health)
			writeRelays := relayHealthStore.SortRelaysByScore(rl.Write)
			if len(writeRelays) > 2 {
				writeRelays = writeRelays[:2]
			}
			outboxRelays = append(outboxRelays, writeRelays...)
		} else {
			aggregatorPubkeys = append(aggregatorPubkeys, pk)
		}
	}

	// Stage 1: Fetch from outbox relays (target users' WRITE relays) - proper NIP-65
	if len(outboxPubkeys) > 0 {
		slog.Debug("profile fetch: using outbox relays", "pubkeys", len(outboxPubkeys), "relays", len(outboxRelays))
		outboxEvents, hadResponse := fetchProfileEventsFromRelays(outboxRelays, outboxPubkeys, 1500*time.Millisecond)
		if hadResponse {
			hadAnyRelayResponse = true
		}
		events = append(events, outboxEvents...)
		for _, evt := range outboxEvents {
			foundPubkeys[evt.PubKey] = true
		}
	}

	// Stage 2: Fetch from aggregators for pubkeys without cached relay lists
	// AND any outbox pubkeys that weren't found (fallback)
	var stillMissing []string
	for _, pk := range missing {
		if !foundPubkeys[pk] {
			stillMissing = append(stillMissing, pk)
		}
	}

	if len(stillMissing) > 0 {
		profileRelays := config.GetProfileRelays()
		slog.Debug("profile fetch: using aggregators", "pubkeys", len(stillMissing), "noRelayList", len(aggregatorPubkeys))
		fallbackEvents, hadResponse := fetchProfileEventsFromRelays(profileRelays, stillMissing, 2000*time.Millisecond)
		if hadResponse {
			hadAnyRelayResponse = true
		}
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
			// Apply cached NIP-05 verification if available
			if cached := GetCachedNIP05(profile.Nip05, evt.PubKey); cached != nil {
				profile.NIP05Verified = true
				profile.NIP05Domain = cached.Domain
				profile.NIP05Relays = cached.Relays
			}
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

		// Trigger async NIP-05 verification only for profiles not already verified
		for pk, profile := range freshProfiles {
			if profile.Nip05 != "" && !profile.NIP05Verified {
				VerifyNIP05Async(profile.Nip05, pk)
			}
		}
	}

	// Only mark profiles as "not found" if at least one relay responded
	// If all relays are in backoff (e.g., expired SSL certs), we shouldn't cache
	// negative results since the profiles may actually exist
	if hadAnyRelayResponse {
		var notFound []string
		for _, pk := range missing {
			if _, ok := freshProfiles[pk]; !ok {
				notFound = append(notFound, pk)
			}
		}
		if len(notFound) > 0 {
			slog.Debug("profile fetch: marking as not found", "count", len(notFound))
			profileCache.SetNotFound(notFound)
		}
	} else if len(missing) > 0 {
		slog.Debug("profile fetch: skipping not-found cache (no relays responded)", "missing", len(missing))
	}

	result := make(map[string]*ProfileInfo, len(cached)+len(freshProfiles))
	for pk, p := range cached {
		result[pk] = p
	}
	for pk, p := range freshProfiles {
		result[pk] = p
	}

	stillMissingCount := len(missing) - len(freshProfiles)
	slog.Debug("profile fetch complete",
		"requested", len(missing),
		"fetched", len(freshProfiles),
		"viaOutbox", len(outboxPubkeys),
		"viaAggregator", len(aggregatorPubkeys),
		"stillMissing", stillMissingCount,
		"hadRelayResponse", hadAnyRelayResponse,
		"returning", len(result))
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
			// Apply cached NIP-05 verification if available
			if cached := GetCachedNIP05(profile.Nip05, evt.PubKey); cached != nil {
				profile.NIP05Verified = true
				profile.NIP05Domain = cached.Domain
				profile.NIP05Relays = cached.Relays
			}
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

		// Trigger async NIP-05 verification only for profiles not already verified
		for pk, profile := range freshProfiles {
			if profile.Nip05 != "" && !profile.NIP05Verified {
				VerifyNIP05Async(profile.Nip05, pk)
			}
		}
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

// fetchReactionsDirect fetches kind 7 reactions directly without singleflight.
// Use fetchReactions for the singleflight version.
func fetchReactionsDirect(relays []string, eventIDs []string) map[string]*ReactionsSummary {
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

		targetEventID := util.GetLastTagValue(evt.Tags, "e")

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
	events := make([]Event, 0, len(eventIDs)*10) // Approximate reactions per event
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
	events := make([]Event, 0, len(eventIDs)*3) // Approximate replies per event

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
		"kinds": []int{1, 1111}, // Kind 1 = NIP-10 replies, Kind 1111 = NIP-22 comments
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

// fetchRepliesSince fetches replies/comments created after the given timestamp
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
	events := make([]Event, 0, len(eventIDs)*3) // Approximate replies per event

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
		"kinds": []int{1, 1111}, // Kind 1 = NIP-10 replies, Kind 1111 = NIP-22 comments
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

// fetchAddressableComments fetches kind 1111 (NIP-22) comments for addressable events
// using the #A tag filter with format "kind:pubkey:d-tag"
func fetchAddressableComments(relays []string, aTagValue string) []Event {
	if aTagValue == "" {
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
			fetchAddressableCommentsFromRelay(ctx, relayURL, aTagValue, eventChan)
		}(relay)
	}

	go func() {
		wg.Wait()
		close(eventChan)
	}()

	seenIDs := make(map[string]bool)
	events := make([]Event, 0, 20) // Approximate comments

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

func fetchAddressableCommentsFromRelay(ctx context.Context, relayURL string, aTagValue string, eventChan chan<- Event) {
	subID := "comments-a-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{1111}, // NIP-22 comments only
		"#A":    []string{aTagValue},
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

// fetchReplyCountsDirect counts replies directly without singleflight.
// Use fetchReplyCounts for the singleflight version.
func fetchReplyCountsDirect(relays []string, eventIDs []string) map[string]int {
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

			targetEventID := util.GetLastTagValue(evt.Tags, "e")
			if targetEventID != "" {
				replyCounts[targetEventID]++
			}
		case <-ctx.Done():
			break collectLoop
		}
	}

	return replyCounts
}

// Type alias for internal/types
type RelayList = types.RelayList

// fetchRelayListDirect fetches kind:10002 relay list directly without singleflight.
// Use fetchRelayList for the singleflight version.
func fetchRelayListDirect(pubkey string) *RelayList {
	if relayList, notFound, ok := relayListCache.Get(pubkey); ok {
		IncrementCacheHit()
		if notFound {
			return nil
		}
		return relayList
	}

	IncrementCacheMiss()
	indexerRelays := config.GetDefaultRelays()

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

		// Validate and normalize relay URL
		relayURL := normalizeRelayURL(tag[1])
		if relayURL == "" {
			continue // Skip invalid URLs
		}

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

// Type alias for internal/types
type RelayGroup = types.RelayGroup

// groupPubkeysByWriteRelays groups pubkeys by their write relays for efficient batch queries
// Returns groups sorted by composite score (best first) and a list of pubkeys with no relay list
// maxRelaysPerGroup limits how many relays each pubkey contributes to (0 = unlimited)
func groupPubkeysByWriteRelays(pubkeys []string, relayLists map[string]*RelayList, maxRelaysPerGroup int) ([]RelayGroup, []string) {
	// Track which relays each pubkey writes to
	relayToPubkeys := make(map[string][]string)
	var noRelayList []string

	for _, pk := range pubkeys {
		rl := relayLists[pk]
		if rl == nil || len(rl.Write) == 0 {
			noRelayList = append(noRelayList, pk)
			continue
		}

		// Add pubkey to each of their write relays (up to maxRelaysPerGroup)
		// Filter out write-only relays (e.g., sendit.nosflare.com) that don't accept REQ
		var writeRelays []string
		for _, r := range rl.Write {
			if !IsWriteOnlyRelay(r) {
				writeRelays = append(writeRelays, r)
			}
		}

		if len(writeRelays) == 0 {
			noRelayList = append(noRelayList, pk)
			continue
		}

		if maxRelaysPerGroup > 0 && len(writeRelays) > maxRelaysPerGroup {
			// Use relays sorted by health score for better performance
			writeRelays = relayHealthStore.SortRelaysByScore(writeRelays)
			writeRelays = writeRelays[:maxRelaysPerGroup]
		}

		for _, relayURL := range writeRelays {
			relayToPubkeys[relayURL] = append(relayToPubkeys[relayURL], pk)
		}
	}

	// Convert to groups with composite scoring
	groups := make([]RelayGroup, 0, len(relayToPubkeys))
	for relayURL, pks := range relayToPubkeys {
		isConnected := relayPool.IsConnected(relayURL)

		// Composite score: connected bonus + health score + pubkey coverage bonus
		// Connected relays avoid dial latency (huge win)
		// Health score reflects reliability/speed
		// More pubkeys = more efficient query
		score := relayHealthStore.getRelayScore(relayURL)
		if isConnected {
			score += 100 // Major bonus for avoiding dial overhead
		}
		// Pubkey coverage bonus (diminishing returns via log)
		if len(pks) > 1 {
			score += int(10 * (1 + float64(len(pks)-1)/10)) // +10 to +30 based on pubkey count
		}

		groups = append(groups, RelayGroup{
			RelayURL:    relayURL,
			Pubkeys:     pks,
			Score:       score,
			IsConnected: isConnected,
		})
	}

	// Sort by score (highest first) for optimal relay selection
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Score > groups[j].Score
	})

	return groups, noRelayList
}

// Outbox model constants
const (
	maxOutboxRelayGroups = 25            // Max relay groups to query
	tier1RelayCount      = 8             // Fast tier: connected + top scored relays
	tier1Timeout         = 800 * time.Millisecond // Short timeout for fast tier
	tier2Timeout         = 2 * time.Second        // Extended timeout if we need more
)

// getNIP05RelayHints returns relay hints from NIP-05 verification for the given pubkeys.
// These can supplement or substitute for kind:10002 relay lists.
// Only returns hints for pubkeys with verified NIP-05 and non-empty relay hints.
func getNIP05RelayHints(pubkeys []string) map[string]*RelayList {
	if len(pubkeys) == 0 {
		return nil
	}

	result := make(map[string]*RelayList)
	for _, pk := range pubkeys {
		profile := getCachedProfile(pk)
		if profile == nil || !profile.NIP05Verified || len(profile.NIP05Relays) == 0 {
			continue
		}

		// NIP-05 relays are treated as both read and write hints
		// (they indicate where to find content from/about this user)
		relayList := &RelayList{
			Read:  make([]string, 0, len(profile.NIP05Relays)),
			Write: make([]string, 0, len(profile.NIP05Relays)),
		}
		for _, relay := range profile.NIP05Relays {
			if normalized := normalizeRelayURL(relay); normalized != "" {
				relayList.Read = append(relayList.Read, normalized)
				relayList.Write = append(relayList.Write, normalized)
			}
		}
		if len(relayList.Write) > 0 {
			result[pk] = relayList
		}
	}

	return result
}

// fetchEventsFromOutbox fetches events from users' write relays using the outbox model
// Uses tiered querying: fast tier (connected relays) first, then expands if needed
func fetchEventsFromOutbox(pubkeys []string, relayLists map[string]*RelayList, filter Filter, fallbackRelays []string) ([]Event, bool) {
	if len(pubkeys) == 0 {
		return nil, true
	}

	// Group pubkeys by write relays (max 2 relays per user for efficiency)
	// Groups are sorted by composite score: connected > health > pubkey count
	groups, noRelayList := groupPubkeysByWriteRelays(pubkeys, relayLists, 2)

	// For users without kind:10002 relay lists, try NIP-05 relay hints as fallback
	if len(noRelayList) > 0 {
		nip05Hints := getNIP05RelayHints(noRelayList)
		if len(nip05Hints) > 0 {
			// Re-group the users with NIP-05 hints
			additionalGroups, stillNoList := groupPubkeysByWriteRelays(noRelayList, nip05Hints, 2)
			groups = append(groups, additionalGroups...)
			noRelayList = stillNoList

			slog.Debug("outbox using NIP-05 relay hints",
				"users_with_hints", len(nip05Hints),
				"additional_groups", len(additionalGroups))
		}
	}

	// Limit relay groups to prevent connection storms
	var overflow []string
	if len(groups) > maxOutboxRelayGroups {
		droppedPubkeys := make(map[string]bool)
		for i := maxOutboxRelayGroups; i < len(groups); i++ {
			for _, pk := range groups[i].Pubkeys {
				droppedPubkeys[pk] = true
			}
		}
		for i := 0; i < maxOutboxRelayGroups; i++ {
			for _, pk := range groups[i].Pubkeys {
				delete(droppedPubkeys, pk)
			}
		}
		for pk := range droppedPubkeys {
			overflow = append(overflow, pk)
		}
		groups = groups[:maxOutboxRelayGroups]
	}

	// Split into tiers: fast tier (connected/high-score) vs expansion tier
	tier1End := tier1RelayCount
	if tier1End > len(groups) {
		tier1End = len(groups)
	}
	tier1Groups := groups[:tier1End]
	tier2Groups := groups[tier1End:]

	// Count connected relays in tier 1
	connectedCount := 0
	for _, g := range tier1Groups {
		if g.IsConnected {
			connectedCount++
		}
	}

	slog.Debug("outbox fetch setup",
		"total_pubkeys", len(pubkeys),
		"tier1", len(tier1Groups),
		"tier1_connected", connectedCount,
		"tier2", len(tier2Groups),
		"no_relay_list", len(noRelayList),
		"overflow", len(overflow))

	// Create channels
	eventChan := make(chan Event, filter.Limit*3)
	eoseChan := make(chan string, len(groups)+len(fallbackRelays))

	// Use overall context for cleanup
	ctx, cancel := context.WithTimeout(context.Background(), tier1Timeout+tier2Timeout)
	defer cancel()

	var wg sync.WaitGroup

	// TIER 1: Query fast relays (connected + high score) + aggregator relays
	// Aggregators (nos.lol, relay.damus.io, etc.) are always queried - they're fast and have broad coverage
	for _, group := range tier1Groups {
		wg.Add(1)
		go func(g RelayGroup) {
			defer wg.Done()
			groupFilter := Filter{
				Authors: g.Pubkeys,
				Kinds:   filter.Kinds,
				Limit:   filter.Limit,
				Since:   filter.Since,
				Until:   filter.Until,
				TTags:   filter.TTags,
			}
			fetchFromRelayWithURL(ctx, g.RelayURL, groupFilter, eventChan, eoseChan)
		}(group)
	}

	// Query aggregator relays for ALL users as baseline coverage
	// This ensures we catch events that are on aggregators but not on users' declared write relays
	// (e.g., user published via different client, or write relay is slow/down)
	if len(fallbackRelays) > 0 {
		for _, relay := range fallbackRelays {
			wg.Add(1)
			go func(relayURL string) {
				defer wg.Done()
				fallbackFilter := Filter{
					Authors: pubkeys, // ALL followed users, not just those without relay lists
					Kinds:   filter.Kinds,
					Limit:   filter.Limit,
					Since:   filter.Since,
					Until:   filter.Until,
					TTags:   filter.TTags,
				}
				fetchFromRelayWithURL(ctx, relayURL, fallbackFilter, eventChan, eoseChan)
			}(relay)
		}
	}

	// Collect events with tier-aware timing
	seenIDs := make(map[string]bool)
	seenAuthors := make(map[string]bool)
	events := make([]Event, 0, filter.Limit)
	eoseCount := 0
	// Expected EOSE: tier1 groups + fallback relays (always queried for all users now)
	fallbackRelayCount := len(fallbackRelays)
	tier1Expected := len(tier1Groups) + fallbackRelayCount
	totalExpected := len(groups) + fallbackRelayCount

	// Tier 1 phase: collect from fast relays with short timeout
	tier1Timer := time.After(tier1Timeout)
	tier2Started := false

tier1Loop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break tier1Loop
			}
			if !seenIDs[evt.ID] {
				seenIDs[evt.ID] = true
				seenAuthors[evt.PubKey] = true
				events = append(events, evt)

				// Early exit: got plenty of events with good author diversity
				if len(events) >= filter.Limit+20 && len(seenAuthors) >= 5 {
					slog.Debug("outbox tier1: early exit",
						"events", len(events), "authors", len(seenAuthors))
					cancel()
					goto collectDone
				}
			}
		case <-eoseChan:
			eoseCount++
			// If tier 1 relays finished and we have enough, exit early
			if eoseCount >= tier1Expected && len(events) >= filter.Limit && len(seenAuthors) >= 3 {
				slog.Debug("outbox tier1: sufficient results",
					"events", len(events), "authors", len(seenAuthors), "eose", eoseCount)
				cancel()
				goto collectDone
			}
		case <-tier1Timer:
			// Tier 1 timeout - check if we need tier 2
			if len(events) >= filter.Limit && len(seenAuthors) >= 3 {
				slog.Debug("outbox tier1: timeout with sufficient results",
					"events", len(events), "authors", len(seenAuthors))
				cancel()
				goto collectDone
			}
			// Need more events - start tier 2
			break tier1Loop
		case <-ctx.Done():
			goto collectDone
		}
	}

	// TIER 2: Expand to additional relays if we need more events
	if len(tier2Groups) > 0 && len(events) < filter.Limit {
		tier2Started = true
		slog.Debug("outbox tier2: expanding",
			"events_so_far", len(events), "authors", len(seenAuthors), "additional_relays", len(tier2Groups))

		for _, group := range tier2Groups {
			wg.Add(1)
			go func(g RelayGroup) {
				defer wg.Done()
				groupFilter := Filter{
					Authors: g.Pubkeys,
					Kinds:   filter.Kinds,
					Limit:   filter.Limit,
					Since:   filter.Since,
					Until:   filter.Until,
					TTags:   filter.TTags,
				}
				fetchFromRelayWithURL(ctx, g.RelayURL, groupFilter, eventChan, eoseChan)
			}(group)
		}
	}

	// Continue collecting (tier 2 or remaining tier 1 events)
tier2Loop:
	for {
		select {
		case evt, ok := <-eventChan:
			if !ok {
				break tier2Loop
			}
			if !seenIDs[evt.ID] {
				seenIDs[evt.ID] = true
				seenAuthors[evt.PubKey] = true
				events = append(events, evt)

				if len(events) >= filter.Limit+20 && len(seenAuthors) >= 5 {
					slog.Debug("outbox tier2: early exit",
						"events", len(events), "authors", len(seenAuthors))
					cancel()
					break tier2Loop
				}
			}
		case <-eoseChan:
			eoseCount++
			expectedForPhase := tier1Expected
			if tier2Started {
				expectedForPhase = totalExpected
			}
			if eoseCount >= expectedForPhase {
				break tier2Loop
			}
		case <-ctx.Done():
			break tier2Loop
		}
	}

collectDone:
	// Wait for goroutines to finish (they'll exit quickly after cancel)
	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	// Drain remaining events from channel
	for evt := range eventChan {
		if !seenIDs[evt.ID] {
			seenIDs[evt.ID] = true
			seenAuthors[evt.PubKey] = true
			events = append(events, evt)
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(events, func(i, j int) bool {
		if events[i].CreatedAt != events[j].CreatedAt {
			return events[i].CreatedAt > events[j].CreatedAt
		}
		return events[i].ID > events[j].ID
	})

	// Limit results
	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[:filter.Limit]
	}

	allEOSE := eoseCount >= totalExpected
	slog.Debug("outbox fetch complete",
		"events", len(events), "authors", len(seenAuthors), "eose", eoseCount, "expected", totalExpected)

	return events, allEOSE
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

	return util.GetTagValues(events[0].Tags, "p")
}

// Type aliases for internal/types
type NotificationType = types.NotificationType
type Notification = types.Notification

// Re-export notification type constants
const (
	NotificationMention  = types.NotificationMention
	NotificationReply    = types.NotificationReply
	NotificationReaction = types.NotificationReaction
	NotificationRepost   = types.NotificationRepost
	NotificationZap      = types.NotificationZap
)

// parseEventToNotification converts an event to a notification with type detection
func parseEventToNotification(evt Event) Notification {
	notif := Notification{
		Event: evt,
	}

	switch evt.Kind {
	case 1: // Note: reply if has e-tag, otherwise mention
		notif.TargetEventID = util.GetTagValue(evt.Tags, "e")
		if notif.TargetEventID != "" {
			notif.Type = NotificationReply
		} else {
			notif.Type = NotificationMention
		}

	case 6: // Repost
		notif.Type = NotificationRepost
		notif.TargetEventID = util.GetTagValue(evt.Tags, "e")

	case 7: // Reaction
		notif.Type = NotificationReaction
		notif.TargetEventID = util.GetTagValue(evt.Tags, "e")

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
		slog.Debug("notification cache hit", "pubkey", shortID(userPubkey), "cached_count", len(cached.Notifications))

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
	slog.Debug("notification cache miss", "pubkey", shortID(userPubkey))
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

// handlerCache stores discovered app handlers by kind number
var handlerCache = struct {
	sync.RWMutex
	handlers map[int][]AppHandler
	fetched  map[int]time.Time
}{
	handlers: make(map[int][]AppHandler),
	fetched:  make(map[int]time.Time),
}

const handlerCacheTTL = 1 * time.Hour
const maxHandlerCacheKinds = 50 // Limit cached kinds to prevent unbounded growth

func init() {
	// Cleanup stale handler cache entries every 30 minutes
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			handlerCache.Lock()
			now := time.Now()
			for kind, fetchTime := range handlerCache.fetched {
				if now.Sub(fetchTime) > handlerCacheTTL {
					delete(handlerCache.handlers, kind)
					delete(handlerCache.fetched, kind)
				}
			}
			handlerCache.Unlock()
		}
	}()
}

// fetchHandlersForKind fetches kind 31990 events that handle the specified kind.
// Results are cached for 1 hour since handlers rarely change.
func fetchHandlersForKind(relays []string, kind int) []AppHandler {
	// Check cache first
	handlerCache.RLock()
	if handlers, ok := handlerCache.handlers[kind]; ok {
		if time.Since(handlerCache.fetched[kind]) < handlerCacheTTL {
			handlerCache.RUnlock()
			return handlers
		}
	}
	handlerCache.RUnlock()

	// Fetch from relays
	filter := Filter{
		Kinds: []int{31990},
		KTags: []string{strconv.Itoa(kind)},
		Limit: 20,
	}

	events, _ := fetchEventsFromRelaysCached(relays, filter)
	if len(events) == 0 {
		// Cache empty result to avoid repeated lookups
		handlerCache.Lock()
		handlerCache.handlers[kind] = nil
		handlerCache.fetched[kind] = time.Now()
		handlerCache.Unlock()
		return nil
	}

	// Parse handlers from events
	var handlers []AppHandler
	seen := make(map[string]bool) // Dedupe by name

	for _, evt := range events {
		handler := parseHandlerEvent(evt)
		if handler.Name != "" && handler.URL != "" && !seen[handler.Name] {
			handlers = append(handlers, handler)
			seen[handler.Name] = true
		}
	}

	// Limit to top 5 handlers
	if len(handlers) > 5 {
		handlers = handlers[:5]
	}

	// Cache result (with size limit)
	handlerCache.Lock()
	// Evict oldest entry if at capacity
	if len(handlerCache.handlers) >= maxHandlerCacheKinds {
		var oldestKind int
		oldestTime := time.Now()
		for k, t := range handlerCache.fetched {
			if t.Before(oldestTime) {
				oldestTime = t
				oldestKind = k
			}
		}
		delete(handlerCache.handlers, oldestKind)
		delete(handlerCache.fetched, oldestKind)
	}
	handlerCache.handlers[kind] = handlers
	handlerCache.fetched[kind] = time.Now()
	handlerCache.Unlock()

	return handlers
}

// parseHandlerEvent extracts handler info from a kind 31990 event
func parseHandlerEvent(evt Event) AppHandler {
	var handler AppHandler

	// Parse metadata from content (JSON)
	var metadata struct {
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.Unmarshal([]byte(evt.Content), &metadata); err == nil {
		handler.Name = metadata.Name
		handler.Picture = metadata.Picture
	}

	// Find web URL template from tags
	// Format: ["web", "https://app.com/<bech32>", "nevent"] - third element is bech32 type hint
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "web" {
			handler.URL = tag[1]
			if len(tag) >= 3 {
				handler.Bech32Type = tag[2] // "nevent", "naddr", or empty for note1
			}
			break
		}
	}

	// Fallback name from d tag if not in content
	if handler.Name == "" {
		handler.Name = util.GetTagValue(evt.Tags, "d")
	}

	return handler
}

// HandlerURLContext contains event info needed for bech32 encoding
type HandlerURLContext struct {
	EventID    string   // Event ID (hex)
	AuthorHex  string   // Author pubkey (hex)
	Kind       int      // Event kind
	DTag       string   // D-tag for addressable events
	RelayHints []string // Relay hints for nevent/naddr
}

// buildHandlerURL replaces <bech32> placeholder with the appropriate bech32 encoding
func buildHandlerURL(urlTemplate, bech32Type string, ctx HandlerURLContext) string {
	if urlTemplate == "" || ctx.EventID == "" {
		return ""
	}

	var bech32 string
	var err error

	switch bech32Type {
	case "nevent":
		// Include author and up to 2 relay hints
		relayHints := ctx.RelayHints
		if len(relayHints) > 2 {
			relayHints = relayHints[:2]
		}
		bech32, err = nips.EncodeNEvent(ctx.EventID, ctx.AuthorHex, relayHints)
	case "naddr":
		// For addressable events (kind 30000-39999)
		if ctx.Kind >= 30000 && ctx.Kind < 40000 && ctx.AuthorHex != "" {
			bech32, err = nips.EncodeNAddr(uint32(ctx.Kind), ctx.AuthorHex, ctx.DTag)
		} else {
			// Fall back to nevent if not actually addressable
			bech32, err = nips.EncodeNEvent(ctx.EventID, ctx.AuthorHex, ctx.RelayHints)
		}
	default:
		// Default to note1 (just event ID)
		bech32, err = nips.EncodeEventID(ctx.EventID)
	}

	if err != nil {
		return ""
	}

	// Replace placeholder
	return strings.Replace(urlTemplate, "<bech32>", bech32, 1)
}

// getHandlersForEvent returns handlers for an event, with URLs populated
// If follows is provided, handlers recommended by followed users are prioritized
func getHandlersForEvent(relays []string, ctx HandlerURLContext, follows []string) []AppHandler {
	handlers := fetchHandlersForKind(relays, ctx.Kind)
	if len(handlers) == 0 {
		return nil
	}

	// Fetch recommendations from follows if available
	var recommendations map[string]int // handler key -> recommendation count
	if len(follows) > 0 {
		recommendations = fetchHandlerRecommendations(relays, ctx.Kind, follows)
	}

	// Build URLs for each handler and apply recommendation counts
	result := make([]AppHandler, 0, len(handlers))
	for _, h := range handlers {
		url := buildHandlerURL(h.URL, h.Bech32Type, ctx)
		if url != "" {
			handler := AppHandler{
				Name:    h.Name,
				Picture: h.Picture,
				URL:     url,
			}
			// Check if this handler was recommended
			if recommendations != nil {
				// Try matching by URL template (most reliable)
				if count, ok := recommendations[h.URL]; ok {
					handler.RecommendedBy = count
				}
			}
			result = append(result, handler)
		}
	}

	// Sort by recommendation count (recommended first), then by name
	sort.Slice(result, func(i, j int) bool {
		if result[i].RecommendedBy != result[j].RecommendedBy {
			return result[i].RecommendedBy > result[j].RecommendedBy
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// fetchHandlerRecommendations fetches kind 31989 events from followed users
// Returns a map of handler URL template -> recommendation count
func fetchHandlerRecommendations(relays []string, kind int, follows []string) map[string]int {
	if len(follows) == 0 {
		return nil
	}

	// Limit follows to avoid huge queries
	queryFollows := follows
	if len(queryFollows) > 100 {
		queryFollows = queryFollows[:100]
	}

	// Fetch recommendations for this kind from followed users
	filter := Filter{
		Kinds:   []int{31989},
		Authors: queryFollows,
		DTags:   []string{strconv.Itoa(kind)},
		Limit:   50,
	}

	events, _ := fetchEventsFromRelaysCached(relays, filter)
	if len(events) == 0 {
		return nil
	}

	// Parse recommendations and count by handler
	// We'll track by handler's 31990 reference (pubkey:dtag)
	handlerRefs := make(map[string]int) // "pubkey:dtag" -> count

	for _, evt := range events {
		for _, tag := range evt.Tags {
			if len(tag) >= 2 && tag[0] == "a" {
				parts := strings.Split(tag[1], ":")
				if len(parts) >= 3 && parts[0] == "31990" {
					// Create key from pubkey:dtag
					key := parts[1] + ":" + parts[2]
					handlerRefs[key]++
				}
			}
		}
	}

	if len(handlerRefs) == 0 {
		return nil
	}

	// Now we need to resolve handler refs to URL templates
	// Fetch the referenced 31990 events to get their web URLs
	result := make(map[string]int)
	for ref, count := range handlerRefs {
		parts := strings.Split(ref, ":")
		if len(parts) < 2 {
			continue
		}
		pubkey, dtag := parts[0], parts[1]

		// Fetch this specific handler
		handlerFilter := Filter{
			Kinds:   []int{31990},
			Authors: []string{pubkey},
			DTags:   []string{dtag},
			Limit:   1,
		}
		handlerEvents, _ := fetchEventsFromRelaysCached(relays, handlerFilter)
		if len(handlerEvents) == 0 {
			continue
		}

		// Get the web URL template
		for _, tag := range handlerEvents[0].Tags {
			if len(tag) >= 2 && tag[0] == "web" {
				result[tag[1]] = count
				break
			}
		}
	}

	return result
}
