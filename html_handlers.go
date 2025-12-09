package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// getNotificationsLastSeen gets the notifications_last_seen timestamp from cookie
func getNotificationsLastSeen(r *http.Request) int64 {
	cookie, err := r.Cookie("notifications_last_seen")
	if err != nil {
		return 0
	}
	ts, err := strconv.ParseInt(cookie.Value, 10, 64)
	if err != nil {
		return 0
	}
	return ts
}

// checkUnreadNotifications checks if a logged-in user has unread notifications
// Returns false if user is not logged in
func checkUnreadNotifications(r *http.Request, session *BunkerSession, relays []string) bool {
	if session == nil || !session.Connected || session.UserPubKey == nil {
		return false
	}
	pubkeyHex := hex.EncodeToString(session.UserPubKey)
	lastSeen := getNotificationsLastSeen(r)
	return hasUnreadNotifications(relays, pubkeyHex, lastSeen)
}

func htmlTimelineHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (same as JSON handler)
	q := r.URL.Query()

	// Get session early to check for user's relay list
	session := getSessionFromRequest(r)

	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		// Use user's read relays if logged in and have a relay list (NIP-65)
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

			if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
				relays = session.UserRelayList.Read
				log.Printf("Using user's %d read relays from NIP-65", len(relays))
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

	// If feed=me, show only the user's own notes
	if feedMode == "me" && session != nil && session.Connected && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		authors = []string{pubkeyHex}
		log.Printf("Showing notes for user %s", pubkeyHex[:12])
	}

	// Special handling for bookmarks (kind 10003)
	// When kinds=10003, we need to fetch the user's bookmark list and then fetch the bookmarked events
	var bookmarkedEventIDs []string
	isBookmarksView := len(kinds) == 1 && kinds[0] == 10003
	if isBookmarksView && session != nil && session.Connected {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		bookmarkEvents := fetchKind10003(relays, pubkeyHex)
		if len(bookmarkEvents) > 0 {
			// Extract event IDs from e tags
			for _, tag := range bookmarkEvents[0].Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					bookmarkedEventIDs = append(bookmarkedEventIDs, tag[1])
				}
			}
			log.Printf("Found %d bookmarked events for user %s", len(bookmarkedEventIDs), pubkeyHex[:12])
		}
		// Clear the kinds filter - we'll fetch the actual events by ID
		kinds = nil
	}

	// Check if we should filter out replies (default to true like JSON handler)
	noReplies := q.Get("no_replies") != "0"

	// Build filter - fetch more events if we're filtering replies, since many events are replies
	fetchLimit := limit
	if noReplies {
		fetchLimit = limit * 5 // Fetch 5x to compensate for reply filtering
	}

	var events []Event
	var eose bool

	// Special case: fetch bookmarked events by ID
	if isBookmarksView && len(bookmarkedEventIDs) > 0 {
		filter := Filter{
			IDs:   bookmarkedEventIDs,
			Limit: len(bookmarkedEventIDs),
		}
		events, eose = fetchEventsFromRelaysCached(relays, filter)
		log.Printf("Fetched %d bookmarked events", len(events))
	} else if isBookmarksView {
		// No bookmarks found
		events = []Event{}
		eose = true
	} else {
		filter := Filter{
			Authors: authors,
			Kinds:   kinds,
			Limit:   fetchLimit,
			Since:   since,
			Until:   until,
		}
		events, eose = fetchEventsFromRelaysCached(relays, filter)
	}

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
		// Apply original limit after filtering
		if len(events) > limit {
			events = events[:limit]
		}
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
		events = filtered
	}

	// Collect unique pubkeys and event IDs for enrichment
	pubkeySet := make(map[string]bool)
	eventIDs := make([]string, 0, len(events))
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
		eventIDs = append(eventIDs, evt.ID)
		contents = append(contents, evt.Content)
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

		// Prefetch next page in background to warm the cache
		// This makes clicking "Older →" feel instant
		go prefetchNextPage(relays, authors, kinds, limit, lastCreatedAt, noReplies)
	}

	// Get query params for messages (session already fetched at start)
	errorMsg := q.Get("error")
	successMsg := q.Get("success")

	// Build current URL for reaction redirects
	currentURL := r.URL.Path + "?" + r.URL.RawQuery

	// Get theme from cookie
	themeClass, themeLabel := getThemeFromRequest(r)

	// Generate CSRF token for forms (use session ID if logged in, otherwise empty)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	// Check for unread notifications
	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)

	// Render HTML - showReactions is opposite of fast mode
	html, err := renderHTML(resp, relays, authors, kinds, limit, session, errorMsg, successMsg, !fast, feedMode, currentURL, themeClass, themeLabel, csrfToken, hasUnreadNotifs)
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
	if eventID == "" || !isValidEventID(eventID) {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
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

	// Get theme from cookie
	themeClass, themeLabel := getThemeFromRequest(r)

	// Get success message from query param
	successMsg := q.Get("success")

	// Generate CSRF token for forms (use session ID if logged in)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	// Check for unread notifications
	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)

	// Render HTML
	htmlContent, err := renderThreadHTML(resp, relays, session, currentURL, themeClass, themeLabel, successMsg, csrfToken, hasUnreadNotifs)
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

	// Get theme from cookie
	themeClass, themeLabel := getThemeFromRequest(r)

	// Get session for login status
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	// Check if logged-in user follows this profile and if this is their own profile
	var isFollowing, isSelf bool
	if session != nil && session.Connected {
		userPubkeyHex := hex.EncodeToString(session.UserPubKey)
		isSelf = pubkey == userPubkeyHex

		// Check if profile pubkey is in session's following list
		session.mu.Lock()
		for _, followedPubkey := range session.FollowingPubkeys {
			if followedPubkey == pubkey {
				isFollowing = true
				break
			}
		}
		session.mu.Unlock()
	}

	// Build current URL for form redirects
	currentURL := r.URL.Path
	if r.URL.RawQuery != "" {
		currentURL += "?" + r.URL.RawQuery
	}

	// Generate CSRF token for forms (use session ID if logged in)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	// Check for unread notifications
	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)

	// Render HTML
	htmlContent, err := renderProfileHTML(resp, relays, limit, themeClass, themeLabel, loggedIn, currentURL, csrfToken, isFollowing, isSelf, hasUnreadNotifs)
	if err != nil {
		log.Printf("Error rendering profile HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=30")
	w.Write([]byte(htmlContent))
}

func htmlThemeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	// Read current theme from cookie
	currentTheme := ""
	if cookie, err := r.Cookie("theme"); err == nil {
		currentTheme = cookie.Value
	}

	// Cycle through states: "" (system) -> "dark" -> "light" -> "" (system)
	var newTheme string
	switch currentTheme {
	case "":
		newTheme = "dark"
	case "dark":
		newTheme = "light"
	case "light":
		newTheme = ""
	default:
		newTheme = ""
	}

	// Set cookie (1 year expiry)
	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    newTheme,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect back to referer or timeline (sanitize to prevent open redirect)
	returnURL := r.Header.Get("Referer")
	// Extract just the path from Referer header (ignore host) to prevent open redirect
	if returnURL != "" {
		if parsed, err := url.Parse(returnURL); err == nil {
			returnURL = parsed.Path
			if parsed.RawQuery != "" {
				returnURL += "?" + parsed.RawQuery
			}
		} else {
			returnURL = ""
		}
	}
	returnURL = sanitizeReturnURL(returnURL)
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

func htmlNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	// Get session - must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	pubkeyHex := hex.EncodeToString(session.UserPubKey)

	// Parse until parameter for pagination
	var until *int64
	if untilStr := r.URL.Query().Get("until"); untilStr != "" {
		if u, err := strconv.ParseInt(untilStr, 10, 64); err == nil {
			until = &u
		}
	}

	// Get user's relays
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
		"wss://nos.lol",
		"wss://nostr.mom",
	}

	// Use user's read relays if available
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays = session.UserRelayList.Read
	}

	// Fetch notifications (request one extra to know if there are more)
	const limit = 50
	notifications := fetchNotifications(relays, pubkeyHex, limit+1, until)

	// Collect pubkeys for profile enrichment and target event IDs
	pubkeySet := make(map[string]bool)
	targetEventIDs := make([]string, 0)
	for _, notif := range notifications {
		pubkeySet[notif.Event.PubKey] = true
		// Collect target event IDs for reactions/reposts
		if notif.TargetEventID != "" && (notif.Type == NotificationReaction || notif.Type == NotificationRepost) {
			targetEventIDs = append(targetEventIDs, notif.TargetEventID)
		}
	}
	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}

	// Fetch profiles
	profiles := fetchProfiles(relays, pubkeys)

	// Fetch target events for reactions/reposts to show context
	targetEvents := make(map[string]*Event)
	if len(targetEventIDs) > 0 {
		// Fetch events by their IDs using a filter
		filter := Filter{
			IDs:   targetEventIDs,
			Limit: len(targetEventIDs),
		}
		events, _ := fetchEventsFromRelays(relays, filter)
		for i := range events {
			targetEvents[events[i].ID] = &events[i]
		}
	}

	// Get theme
	themeClass, themeLabel := getThemeFromRequest(r)

	// Get user display name
	userDisplayName := "@" + pubkeyHex[:8]
	if userProfile, ok := profiles[pubkeyHex]; ok {
		if userProfile.DisplayName != "" {
			userDisplayName = userProfile.DisplayName
		} else if userProfile.Name != "" {
			userDisplayName = userProfile.Name
		}
	}

	// Calculate pagination - if we got more than limit, there are more pages
	var pagination *HTMLPagination
	if len(notifications) > limit {
		// Remove the extra notification we fetched
		notifications = notifications[:limit]
		// Use the timestamp of the last notification for the next page
		lastNotif := notifications[len(notifications)-1]
		nextUntil := lastNotif.Event.CreatedAt
		pagination = &HTMLPagination{
			Next: fmt.Sprintf("/html/notifications?until=%d", nextUntil),
		}
	}

	// Update last seen cookie - current time (only on first page)
	if until == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "notifications_last_seen",
			Value:    fmt.Sprintf("%d", time.Now().Unix()),
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60, // 1 year
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}

	// Render template
	htmlContent, err := renderNotificationsHTML(notifications, profiles, targetEvents, themeClass, themeLabel, userDisplayName, pubkeyHex, pagination)
	if err != nil {
		log.Printf("Error rendering notifications HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(htmlContent))
}

// prefetchNextPage fetches the next page of events in the background to warm the cache.
// This makes clicking "Older →" feel instant since the data is already cached.
func prefetchNextPage(relays, authors []string, kinds []int, limit int, until int64, noReplies bool) {
	// Use same fetch limit logic as the handler
	fetchLimit := limit
	if noReplies {
		fetchLimit = limit * 5
	}

	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   fetchLimit,
		Until:   &until,
	}

	// Fetch events - this populates the event cache
	events, _ := fetchEventsFromRelaysCached(relays, filter)

	if len(events) == 0 {
		return
	}

	// Collect unique pubkeys from the prefetched events
	pubkeySet := make(map[string]bool)
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
		contents = append(contents, evt.Content)
	}

	// Also collect pubkeys from npub/nprofile mentions in content
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	// Prefetch profiles - this populates the profile cache
	if len(pubkeySet) > 0 {
		pubkeys := make([]string, 0, len(pubkeySet))
		for pk := range pubkeySet {
			pubkeys = append(pubkeys, pk)
		}
		fetchProfiles(relays, pubkeys)
	}

	log.Printf("Prefetch: warmed cache for next page (%d events, %d profiles)", len(events), len(pubkeySet))
}
