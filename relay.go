package main

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Filter struct {
	Authors []string
	Kinds   []int
	Limit   int
	Since   *int64
	Until   *int64
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

	// Collect events and dedupe - return early if we have enough
	seenIDs := make(map[string]bool)
	events := []Event{}
	targetCount := filter.Limit * 2 // Collect 2x limit to allow for deduplication

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
		case <-ctx.Done():
			log.Printf("Context timeout, got %d events", len(events))
			break collectLoop
		}
	}

	// Check if all relays sent EOSE (non-blocking drain)
	eoseCount := 0
	for {
		select {
		case _, ok := <-eoseChan:
			if !ok {
				goto sortEvents
			}
			eoseCount++
		default:
			goto sortEvents
		}
	}

sortEvents:
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
	log.Printf("Connecting to relay: %s", relayURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		log.Printf("Failed to connect to %s: %v", relayURL, err)
		return
	}
	defer conn.Close()
	log.Printf("Connected to relay: %s", relayURL)

	// Build NIP-01 REQ message
	subID := "sub-" + randomString(8)
	reqFilter := map[string]interface{}{
		"limit": filter.Limit,
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

	req := []interface{}{"REQ", subID, reqFilter}
	if err := conn.WriteJSON(req); err != nil {
		log.Printf("Failed to send REQ to %s: %v", relayURL, err)
		return
	}

	// Read events until EOSE or context timeout
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg NostrMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if len(msg) < 2 {
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "EVENT":
				if len(msg) >= 3 {
					eventData, err := json.Marshal(msg[2])
					if err != nil {
						continue
					}
					var evt Event
					if err := json.Unmarshal(eventData, &evt); err != nil {
						continue
					}
					evt.RelaysSeen = []string{relayURL}

					select {
					case eventChan <- evt:
					case <-ctx.Done():
						return
					}
				}
			case "EOSE":
				log.Printf("Received EOSE from %s", relayURL)
				eoseChan <- true
				return
			}
		}
	}
}

func randomString(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
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
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	subID := "sub-" + randomString(8)
	reqFilter := map[string]interface{}{
		"ids":   []string{eventID},
		"limit": 1,
	}

	req := []interface{}{"REQ", subID, reqFilter}
	if err := conn.WriteJSON(req); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg NostrMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if len(msg) < 2 {
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "EVENT":
				if len(msg) >= 3 {
					eventData, err := json.Marshal(msg[2])
					if err != nil {
						continue
					}
					var evt Event
					if err := json.Unmarshal(eventData, &evt); err != nil {
						continue
					}
					evt.RelaysSeen = []string{relayURL}

					select {
					case eventChan <- evt:
					case <-ctx.Done():
						return
					}
				}
			case "EOSE":
				return
			}
		}
	}
}

// fetchProfiles fetches kind 0 (profile metadata) events for the given pubkeys
// Uses the global profileCache to avoid redundant relay queries
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

	// Fetch only missing profiles from relays
	filter := Filter{
		Authors: missing,
		Kinds:   []int{0},
		Limit:   len(missing),
	}

	// Profile timeout - balance between speed and completeness
	events, _ := fetchEventsFromRelaysWithTimeout(relays, filter, 2500*time.Millisecond)

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
			profile.Name = name
		}
		if displayName, ok := profileData["display_name"].(string); ok {
			profile.DisplayName = displayName
		}
		if picture, ok := profileData["picture"].(string); ok {
			profile.Picture = picture
		}
		if nip05, ok := profileData["nip05"].(string); ok {
			profile.Nip05 = nip05
		}
		if about, ok := profileData["about"].(string); ok {
			profile.About = about
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

		// Check if this event ID is in our list
		found := false
		for _, id := range eventIDs {
			if id == targetEventID {
				found = true
				break
			}
		}
		if !found {
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

	// Non-blocking drain of eoseChan
	eoseCount := 0
	for {
		select {
		case _, ok := <-eoseChan:
			if !ok {
				goto returnResults
			}
			eoseCount++
		default:
			goto returnResults
		}
	}

returnResults:
	return events, eoseCount == len(relays)
}

func fetchReactionsFromRelay(ctx context.Context, relayURL string, eventIDs []string, eventChan chan<- Event, eoseChan chan<- bool) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		log.Printf("Failed to connect to %s: %v", relayURL, err)
		return
	}
	defer conn.Close()

	subID := "sub-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{7},
		"#e":    eventIDs,
		"limit": 500,
	}

	req := []interface{}{"REQ", subID, reqFilter}
	if err := conn.WriteJSON(req); err != nil {
		log.Printf("Failed to send REQ to %s: %v", relayURL, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg NostrMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if len(msg) < 2 {
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "EVENT":
				if len(msg) >= 3 {
					eventData, err := json.Marshal(msg[2])
					if err != nil {
						continue
					}
					var evt Event
					if err := json.Unmarshal(eventData, &evt); err != nil {
						continue
					}
					evt.RelaysSeen = []string{relayURL}

					select {
					case eventChan <- evt:
					case <-ctx.Done():
						return
					}
				}
			case "EOSE":
				log.Printf("Received EOSE from %s", relayURL)
				eoseChan <- true
				return
			}
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
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		log.Printf("Failed to connect to %s for replies: %v", relayURL, err)
		return
	}
	defer conn.Close()

	subID := "replies-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": []int{1}, // Kind 1 = notes/replies
		"#e":    eventIDs,
		"limit": 100,
	}

	req := []interface{}{"REQ", subID, reqFilter}
	if err := conn.WriteJSON(req); err != nil {
		log.Printf("Failed to send REQ to %s: %v", relayURL, err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			var msg NostrMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			if len(msg) < 2 {
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "EVENT":
				if len(msg) >= 3 {
					eventData, err := json.Marshal(msg[2])
					if err != nil {
						continue
					}
					var evt Event
					if err := json.Unmarshal(eventData, &evt); err != nil {
						continue
					}
					evt.RelaysSeen = []string{relayURL}

					select {
					case eventChan <- evt:
					case <-ctx.Done():
						return
					}
				}
			case "EOSE":
				log.Printf("Received EOSE for replies from %s", relayURL)
				return
			}
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
func fetchRelayList(pubkey string) *RelayList {
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
		log.Printf("No relay list found for %s", pubkey[:12])
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

	log.Printf("Found relay list for %s: %d read, %d write relays", pubkey[:12], len(relayList.Read), len(relayList.Write))
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
		log.Printf("No contact list found for %s", pubkey[:12])
		return nil
	}

	// Parse contacts from p tags
	var contacts []string
	for _, tag := range events[0].Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			contacts = append(contacts, tag[1])
		}
	}

	log.Printf("Found %d contacts for %s", len(contacts), pubkey[:12])
	return contacts
}
