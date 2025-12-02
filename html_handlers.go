package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

func htmlTimelineHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (same as JSON handler)
	q := r.URL.Query()

	// Get session early to check for user's relay list
	session := getSessionFromRequest(r)

	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		// Use user's write relays if logged in and have a relay list (NIP-65)
		if session != nil && session.Connected {
			// If relay list not fetched yet, try to fetch it now
			if session.UserRelayList == nil && session.UserPubKey != nil {
				pubkeyHex := hex.EncodeToString(session.UserPubKey)
				log.Printf("Fetching relay list for user %s...", pubkeyHex[:12])
				relayList := fetchRelayList(pubkeyHex)
				if relayList != nil {
					session.mu.Lock()
					session.UserRelayList = relayList
					session.mu.Unlock()
				}
			}

			if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
				relays = session.UserRelayList.Write
				log.Printf("Using user's %d write relays from NIP-65", len(relays))
			}
		}

		// Fallback to default relays
		if len(relays) == 0 {
			relays = []string{
				"wss://relay.damus.io",
				"wss://relay.nostr.band",
				"wss://relay.primal.net",
				"wss://nos.lol",
				"wss://nostr.mom",
			}
		}
	}

	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	limit := parseLimit(q.Get("limit"), 50)
	since := parseInt64(q.Get("since"))
	until := parseInt64(q.Get("until"))
	fast := q.Get("fast") == "1" || q.Get("fast") == "true"

	// Feed mode: "follows" or "global" (default to "follows" for logged-in users)
	feedMode := q.Get("feed")
	if feedMode == "" {
		if session != nil && session.Connected {
			feedMode = "follows"
		} else {
			feedMode = "global"
		}
	}

	// If feed=follows and user is logged in, fetch their contact list
	if feedMode == "follows" && session != nil && session.Connected && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)

		// Check cache first
		contacts, ok := contactCache.Get(pubkeyHex)
		if !ok {
			// Fetch from relays
			log.Printf("Fetching contact list for %s...", pubkeyHex[:12])
			contacts = fetchContactList(relays, pubkeyHex)
			if contacts != nil {
				contactCache.Set(pubkeyHex, contacts)
			}
		} else {
			log.Printf("Contact cache hit for %s (%d contacts)", pubkeyHex[:12], len(contacts))
		}

		if len(contacts) > 0 {
			authors = contacts
			log.Printf("Filtering to %d followed authors", len(authors))
		}
	}

	// Check if we should filter out replies (default to true like JSON handler)
	noReplies := q.Get("no_replies") != "0"

	// Build filter - fetch more events if we're filtering replies, since many events are replies
	fetchLimit := limit
	if noReplies {
		fetchLimit = limit * 5 // Fetch 5x to compensate for reply filtering
	}

	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   fetchLimit,
		Since:   since,
		Until:   until,
	}

	// Fetch events from relays (with caching)
	events, eose := fetchEventsFromRelaysCached(relays, filter)

	// Filter out replies (events with e tags) from main timeline
	if noReplies {
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			if !isReply(evt) {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
		// Apply original limit after filtering
		if len(events) > limit {
			events = events[:limit]
		}
	}

	// Collect unique pubkeys and event IDs for enrichment
	pubkeySet := make(map[string]bool)
	eventIDs := make([]string, 0, len(events))
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.Kind == 1 { // Only for notes
			pubkeySet[evt.PubKey] = true
			eventIDs = append(eventIDs, evt.ID)
			contents = append(contents, evt.Content)
		}
	}

	// Also collect pubkeys from npub/nprofile mentions in content
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	// Always fetch profiles and reply counts, only fetch reactions in full mode
	profiles := make(map[string]*ProfileInfo)
	reactions := make(map[string]*ReactionsSummary)
	replyCounts := make(map[string]int)

	var wg sync.WaitGroup

	if len(pubkeySet) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pubkeys := make([]string, 0, len(pubkeySet))
			for pk := range pubkeySet {
				pubkeys = append(pubkeys, pk)
			}
			profiles = fetchProfiles(relays, pubkeys)
		}()
	}

	// Always fetch reply counts (they're useful navigation)
	if len(eventIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replyCounts = fetchReplyCounts(relays, eventIDs)
		}()
	}

	// Only fetch reactions in full mode (slower)
	if !fast && len(eventIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reactions = fetchReactions(relays, eventIDs)
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
			GeneratedAt:   timeNow(),
		},
	}

	// Add pagination if we have results
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		resp.Page.Until = &lastCreatedAt
		nextURL := buildPaginationURL(r.URL.Path, relays, authors, kinds, limit, lastCreatedAt)
		// Preserve fast mode and feed mode in pagination
		if fast {
			nextURL += "&fast=1"
		}
		nextURL += "&feed=" + feedMode
		resp.Page.Next = &nextURL
	}

	// Get query params for messages (session already fetched at start)
	errorMsg := q.Get("error")
	successMsg := q.Get("success")

	// Build current URL for reaction redirects
	currentURL := r.URL.Path + "?" + r.URL.RawQuery

	// Render HTML - showReactions is opposite of fast mode
	html, err := renderHTML(resp, relays, authors, kinds, limit, session, errorMsg, successMsg, !fast, feedMode, currentURL)
	if err != nil {
		log.Printf("Error rendering HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=5")
	w.Write([]byte(html))
}

func htmlThreadHandler(w http.ResponseWriter, r *http.Request) {
	// Extract event ID from path: /html/thread/{eventId}
	eventID := strings.TrimPrefix(r.URL.Path, "/html/thread/")
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

	log.Printf("HTML: Fetching thread for event: %s", eventID)

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

	// Fetch replies (kind 1 events that reference this event via #e tag)
	wg.Add(1)
	go func() {
		defer wg.Done()
		replies = fetchReplies(relays, []string{eventID})
	}()

	wg.Wait()

	if rootEvent == nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	// Collect pubkeys for profile enrichment
	pubkeySet := make(map[string]bool)
	pubkeySet[rootEvent.PubKey] = true
	contents := []string{rootEvent.Content}
	for _, reply := range replies {
		pubkeySet[reply.PubKey] = true
		contents = append(contents, reply.Content)
	}

	// Also collect pubkeys from npub/nprofile mentions in content
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	// Collect all event IDs for reply count fetching
	allEventIDs := make([]string, 0, 1+len(replies))
	allEventIDs = append(allEventIDs, rootEvent.ID)
	for _, reply := range replies {
		allEventIDs = append(allEventIDs, reply.ID)
	}

	// Fetch profiles and reply counts in parallel
	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}

	var profiles map[string]*ProfileInfo
	var replyCounts map[string]int
	var wg2 sync.WaitGroup

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		profiles = fetchProfiles(relays, pubkeys)
	}()

	wg2.Add(1)
	go func() {
		defer wg2.Done()
		replyCounts = fetchReplyCounts(relays, allEventIDs)
	}()

	wg2.Wait()

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
		ReplyCount:    replyCounts[rootEvent.ID],
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
			ReplyCount:    replyCounts[evt.ID],
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

	// Get session for reply form
	session := getSessionFromRequest(r)

	// Build current URL for reaction redirects
	currentURL := r.URL.Path

	// Render HTML
	htmlContent, err := renderThreadHTML(resp, relays, session, currentURL)
	if err != nil {
		log.Printf("Error rendering thread HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=10")
	w.Write([]byte(htmlContent))
}

func htmlProfileHandler(w http.ResponseWriter, r *http.Request) {
	// Extract pubkey from path: /html/profile/{pubkey}
	pubkey := strings.TrimPrefix(r.URL.Path, "/html/profile/")
	if pubkey == "" {
		http.Error(w, "Pubkey required", http.StatusBadRequest)
		return
	}

	// Handle npub format - decode to hex if needed
	if strings.HasPrefix(pubkey, "npub1") {
		hexPubkey, err := decodeBech32Pubkey(pubkey)
		if err != nil {
			http.Error(w, "Invalid npub format", http.StatusBadRequest)
			return
		}
		pubkey = hexPubkey
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

	limit := parseLimit(q.Get("limit"), 20)
	until := parseInt64(q.Get("until"))

	log.Printf("HTML: Fetching profile for pubkey: %s", pubkey[:16])

	// Fetch profile and notes in parallel
	var profile *ProfileInfo
	var events []Event
	var wg sync.WaitGroup

	// Fetch profile (kind 0)
	wg.Add(1)
	go func() {
		defer wg.Done()
		profiles := fetchProfiles(relays, []string{pubkey})
		profile = profiles[pubkey]
	}()

	// Fetch user's top-level notes (kind 1, filtered to exclude replies)
	wg.Add(1)
	go func() {
		defer wg.Done()
		filter := Filter{
			Authors: []string{pubkey},
			Kinds:   []int{1},
			Limit:   limit * 2, // Fetch more since we'll filter out replies
			Until:   until,
		}
		events, _ = fetchEventsFromRelays(relays, filter)
	}()

	wg.Wait()

	// Filter out replies (notes with e tags)
	topLevelNotes := make([]Event, 0, len(events))
	for _, evt := range events {
		if !isReply(evt) {
			topLevelNotes = append(topLevelNotes, evt)
		}
	}

	// Apply limit after filtering
	if len(topLevelNotes) > limit {
		topLevelNotes = topLevelNotes[:limit]
	}

	// Extract and fetch profiles for mentioned pubkeys in content
	contents := make([]string, len(topLevelNotes))
	for i, evt := range topLevelNotes {
		contents[i] = evt.Content
	}
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	if len(mentionedPubkeys) > 0 {
		// Fetch mentioned profiles (will be cached for rendering)
		fetchProfiles(relays, mentionedPubkeys)
	}

	// Build response items with enrichment
	items := make([]EventItem, len(topLevelNotes))
	for i, evt := range topLevelNotes {
		items[i] = EventItem{
			ID:            evt.ID,
			Kind:          evt.Kind,
			Pubkey:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Content:       evt.Content,
			Tags:          evt.Tags,
			Sig:           evt.Sig,
			RelaysSeen:    evt.RelaysSeen,
			AuthorProfile: profile, // Use the fetched profile for all notes
		}
	}

	// Build pagination
	var pageUntil *int64
	var nextURL *string
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		pageUntil = &lastCreatedAt
		next := fmt.Sprintf("/html/profile/%s?limit=%d&until=%d", pubkey, limit, lastCreatedAt)
		nextURL = &next
	}

	resp := ProfileResponse{
		Pubkey:  pubkey,
		Profile: profile,
		Notes: TimelineResponse{
			Items: items,
			Page: PageInfo{
				Until: pageUntil,
				Next:  nextURL,
			},
			Meta: MetaInfo{
				QueriedRelays: len(relays),
				EOSE:          true,
				GeneratedAt:   time.Now(),
			},
		},
	}

	// Render HTML
	htmlContent, err := renderProfileHTML(resp, relays, limit)
	if err != nil {
		log.Printf("Error rendering profile HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=30")
	w.Write([]byte(htmlContent))
}
