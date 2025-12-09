package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TimelineResponse struct {
	Items []EventItem        `json:"items"`
	Page  PageInfo           `json:"page"`
	Meta  MetaInfo           `json:"meta"`
}

type EventItem struct {
	ID            string            `json:"id"`
	Kind          int               `json:"kind"`
	Pubkey        string            `json:"pubkey"`
	CreatedAt     int64             `json:"created_at"`
	Content       string            `json:"content"`
	Tags          [][]string        `json:"tags"`
	Sig           string            `json:"sig"`
	RelaysSeen    []string          `json:"relays_seen"`
	AuthorProfile *ProfileInfo      `json:"author_profile,omitempty"`
	Reactions     *ReactionsSummary `json:"reactions,omitempty"`
	ReplyCount    int               `json:"reply_count"`
}

type ProfileInfo struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Picture     string `json:"picture,omitempty"`
	Nip05       string `json:"nip05,omitempty"`
	About       string `json:"about,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Lud16       string `json:"lud16,omitempty"`
	Website     string `json:"website,omitempty"`
}

type ReactionsSummary struct {
	Total   int            `json:"total"`
	ByType  map[string]int `json:"by_type"`
}

type PageInfo struct {
	Until *int64  `json:"until,omitempty"`
	Next  *string `json:"next,omitempty"`
}

type MetaInfo struct {
	QueriedRelays int       `json:"queried_relays"`
	EOSE          bool      `json:"eose"`
	GeneratedAt   time.Time `json:"generated_at"`
}

type ThreadResponse struct {
	Root    EventItem   `json:"root"`
	Replies []EventItem `json:"replies"`
	Meta    MetaInfo    `json:"meta"`
}

type ProfileResponse struct {
	Pubkey  string           `json:"pubkey"`
	Profile *ProfileInfo     `json:"profile"`
	Notes   TimelineResponse `json:"notes"`
}

func timelineHandler(w http.ResponseWriter, r *http.Request) {
	// Tell browser to cache based on Accept header
	w.Header().Set("Vary", "Accept")

	// If browser navigation (Accept: text/html), serve the client app
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		// Don't cache HTML - always serve fresh so JS can fetch the real data
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "./static/index.html")
		return
	}

	// Parse query parameters
	q := r.URL.Query()

	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
			"wss://nos.lol",
			"wss://nostr.mom",
		}
	}

	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	limit := parseLimit(q.Get("limit"), 50)
	since := parseInt64(q.Get("since"))
	until := parseInt64(q.Get("until"))
	fast := q.Get("fast") == "1" || q.Get("fast") == "true"

	// Build filter
	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   limit,
		Since:   since,
		Until:   until,
	}

	// Check if we should filter out replies
	noReplies := q.Get("no_replies") != "0" // Default to filtering replies

	// Fetch events from relays (with caching)
	log.Printf("Fetching events: kinds=%v, authors=%v, limit=%d", kinds, authors, limit)
	start := time.Now()
	events, eose := fetchEventsFromRelaysCached(relays, filter)
	log.Printf("Fetched %d events in %v (eose=%v)", len(events), time.Since(start), eose)

	// Filter out replies (events with e tags) from main timeline
	// Note: kind 6 (reposts) use e tags to reference the reposted event, not as replies
	if noReplies {
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			if !isReply(evt) || evt.Kind == 6 {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
		log.Printf("After filtering replies: %d events", len(events))
	}

	// Filter out kind 30311 (live events) that don't have a streaming or recording URL
	// These are non-video "live activities" like game presence, not actual streams to watch
	{
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			if evt.Kind == 30311 {
				hasStreamingURL := false
				for _, tag := range evt.Tags {
					if len(tag) >= 2 && (tag[0] == "streaming" || tag[0] == "recording") && tag[1] != "" {
						hasStreamingURL = true
						break
					}
				}
				if !hasStreamingURL {
					continue // Skip non-streaming live events
				}
			}
			filtered = append(filtered, evt)
		}
		if len(filtered) != len(events) {
			log.Printf("After filtering non-streaming live events: %d events (removed %d)", len(filtered), len(events)-len(filtered))
		}
		events = filtered
	}

	// Collect unique pubkeys and event IDs for enrichment
	pubkeySet := make(map[string]bool)
	eventIDs := make([]string, 0, len(events))
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
		eventIDs = append(eventIDs, evt.ID)
	}

	profiles := make(map[string]*ProfileInfo)
	reactions := make(map[string]*ReactionsSummary)
	replyCounts := make(map[string]int)

	// Always fetch profiles (they're quick), only fetch reactions/replies in full mode
	var wg sync.WaitGroup

	if len(pubkeySet) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Fetching profiles for %d authors", len(pubkeySet))
			pubkeys := make([]string, 0, len(pubkeySet))
			for pk := range pubkeySet {
				pubkeys = append(pubkeys, pk)
			}
			profiles = fetchProfiles(relays, pubkeys)
			log.Printf("Fetched %d profiles", len(profiles))
		}()
	}

	// Only fetch reactions and reply counts in full mode (not fast)
	if !fast && len(eventIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Fetching reactions for %d events", len(eventIDs))
			reactions = fetchReactions(relays, eventIDs)
			log.Printf("Fetched reactions for %d events", len(reactions))
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("Fetching reply counts for %d events", len(eventIDs))
			replyCounts = fetchReplyCounts(relays, eventIDs)
			log.Printf("Fetched reply counts: %d events have replies", len(replyCounts))
		}()
	}

	wg.Wait()

	// Build response
	items := make([]EventItem, len(events))
	for i, evt := range events {
		items[i] = EventItem{
			ID:            evt.ID,
			Kind:          evt.Kind,
			Pubkey:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Content:       evt.Content,
			Tags:          evt.Tags,
			Sig:           evt.Sig,
			RelaysSeen:    evt.RelaysSeen,
			AuthorProfile: profiles[evt.PubKey],
			Reactions:     reactions[evt.ID],
			ReplyCount:    replyCounts[evt.ID],
		}
	}

	resp := TimelineResponse{
		Items: items,
		Page:  PageInfo{},
		Meta: MetaInfo{
			QueriedRelays: len(relays),
			EOSE:          eose,
			GeneratedAt:   time.Now(),
		},
	}

	// Add pagination if we have results
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		resp.Page.Until = &lastCreatedAt
		nextURL := r.URL.Path + "?relays=" + strings.Join(relays, ",") +
			"&until=" + strconv.FormatInt(lastCreatedAt, 10) +
			"&limit=" + strconv.Itoa(limit)
		if len(authors) > 0 {
			nextURL += "&authors=" + strings.Join(authors, ",")
		}
		if len(kinds) > 0 {
			kindsStr := make([]string, len(kinds))
			for i, k := range kinds {
				kindsStr[i] = strconv.Itoa(k)
			}
			nextURL += "&kinds=" + strings.Join(kindsStr, ",")
		}
		// Preserve fast mode in pagination
		if fast {
			nextURL += "&fast=1"
		}
		resp.Page.Next = &nextURL
	}

	// Generate ETag from first/last ID and count
	etag := generateETag(items)
	w.Header().Set("ETag", etag)

	// Set Last-Modified based on most recent event
	if len(items) > 0 {
		lastMod := time.Unix(items[0].CreatedAt, 0).UTC()
		w.Header().Set("Last-Modified", lastMod.Format(http.TimeFormat))
	}

	// Check If-None-Match for ETag caching
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Cache-Control", "max-age=5")

	// Check Accept header for hypermedia format
	if strings.Contains(accept, "application/vnd.siren+json") {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		siren := toSirenTimeline(resp, relays, authors, kinds, limit, fast)
		json.NewEncoder(w).Encode(siren)
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func parseStringList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func parseIntList(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			result = append(result, n)
		}
	}
	return result
}

func parseLimit(s string, defaultLimit int) int {
	if s == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 200 {
		return defaultLimit
	}
	return n
}

func parseInt64(s string) *int64 {
	if s == "" {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

func generateETag(items []EventItem) string {
	if len(items) == 0 {
		return `"empty"`
	}
	data := fmt.Sprintf("%s:%s:%d", items[0].ID, items[len(items)-1].ID, len(items))
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf(`"%x"`, hash[:8])
}

func timeNow() time.Time {
	return time.Now()
}

// threadHandler fetches a thread (root event + replies)
func threadHandler(w http.ResponseWriter, r *http.Request) {
	// Tell browser to cache based on Accept header
	w.Header().Set("Vary", "Accept")

	// If browser navigation (Accept: text/html), serve the client app
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		// Don't cache HTML - always serve fresh so JS can fetch the real data
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "./static/index.html")
		return
	}

	// Extract event ID from path: /thread/{eventId}
	eventID := strings.TrimPrefix(r.URL.Path, "/thread/")
	if eventID == "" {
		http.Error(w, "Event ID required", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
			"wss://nos.lol",
			"wss://nostr.mom",
		}
	}

	log.Printf("Fetching thread for event: %s", eventID)

	// Fetch the root event and replies in parallel
	var rootEvent *Event
	var replies []Event
	var wg sync.WaitGroup

	// Fetch root event by ID
	wg.Add(1)
	go func() {
		defer wg.Done()
		events := fetchEventByID(relays, eventID)
		if len(events) > 0 {
			rootEvent = &events[0]
		}
	}()

	// Fetch replies (events that reference this event via #e tag)
	wg.Add(1)
	go func() {
		defer wg.Done()
		replies, _ = fetchEventsFromRelaysWithETags(relays, []string{eventID})
		// Filter to only kind 1 (notes) that are actual replies
		filtered := make([]Event, 0)
		for _, evt := range replies {
			if evt.Kind == 1 {
				filtered = append(filtered, evt)
			}
		}
		replies = filtered
	}()

	wg.Wait()

	if rootEvent == nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	// Collect pubkeys for profile enrichment
	pubkeySet := make(map[string]bool)
	pubkeySet[rootEvent.PubKey] = true
	for _, reply := range replies {
		pubkeySet[reply.PubKey] = true
	}

	// Fetch profiles
	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}
	profiles := fetchProfiles(relays, pubkeys)

	// Build response
	rootItem := EventItem{
		ID:            rootEvent.ID,
		Kind:          rootEvent.Kind,
		Pubkey:        rootEvent.PubKey,
		CreatedAt:     rootEvent.CreatedAt,
		Content:       rootEvent.Content,
		Tags:          rootEvent.Tags,
		Sig:           rootEvent.Sig,
		RelaysSeen:    rootEvent.RelaysSeen,
		AuthorProfile: profiles[rootEvent.PubKey],
	}

	replyItems := make([]EventItem, len(replies))
	for i, evt := range replies {
		replyItems[i] = EventItem{
			ID:            evt.ID,
			Kind:          evt.Kind,
			Pubkey:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Content:       evt.Content,
			Tags:          evt.Tags,
			Sig:           evt.Sig,
			RelaysSeen:    evt.RelaysSeen,
			AuthorProfile: profiles[evt.PubKey],
		}
	}

	// Sort replies by created_at ASC (oldest first for reading order)
	sort.Slice(replyItems, func(i, j int) bool {
		return replyItems[i].CreatedAt < replyItems[j].CreatedAt
	})

	resp := ThreadResponse{
		Root:    rootItem,
		Replies: replyItems,
		Meta: MetaInfo{
			QueriedRelays: len(relays),
			EOSE:          true,
			GeneratedAt:   time.Now(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=10")
	json.NewEncoder(w).Encode(resp)
}

// isReply checks if an event is a reply (has e tags)
func isReply(evt Event) bool {
	for _, tag := range evt.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			return true
		}
	}
	return false
}

func buildPaginationURL(path string, relays []string, authors []string, kinds []int, limit int, until int64) string {
	parts := []string{path + "?"}

	if len(relays) > 0 {
		parts = append(parts, "relays="+strings.Join(relays, ","))
	}
	if len(authors) > 0 {
		parts = append(parts, "authors="+strings.Join(authors, ","))
	}
	if len(kinds) > 0 {
		kindsStr := make([]string, len(kinds))
		for i, k := range kinds {
			kindsStr[i] = strconv.Itoa(k)
		}
		parts = append(parts, "kinds="+strings.Join(kindsStr, ","))
	}
	parts = append(parts, "limit="+strconv.Itoa(limit))
	parts = append(parts, "until="+strconv.FormatInt(until, 10))

	return strings.Join(parts, "&")
}
