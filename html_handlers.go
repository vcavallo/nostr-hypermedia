package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// prefetchSemaphore limits concurrent prefetch goroutines to prevent memory spikes
// under high load. 10 concurrent prefetches is reasonable for warming cache.
var prefetchSemaphore = make(chan struct{}, 10)

// buildCurrentURL returns the request URL with specified query params removed.
// Uses RawQuery to preserve original encoding (avoids double-encoding commas).
func buildCurrentURL(r *http.Request, removeParams ...string) string {
	// Fast path: no params to remove
	if len(removeParams) == 0 {
		return r.URL.RequestURI()
	}

	// Parse and rebuild query string, preserving raw values
	query := r.URL.Query()
	for _, param := range removeParams {
		query.Del(param)
	}

	// Rebuild without re-encoding by using raw values where possible
	// This preserves commas as-is instead of encoding to %2C
	var parts []string
	for key, values := range query {
		for _, val := range values {
			parts = append(parts, key+"="+val)
		}
	}

	if len(parts) == 0 {
		return r.URL.Path
	}
	return r.URL.Path + "?" + strings.Join(parts, "&")
}

// getNotificationsLastSeen returns when user last viewed notifications
// For logged-in users, checks Redis/store first, falls back to cookie
// For logged-out users, uses cookie only
func getNotificationsLastSeen(r *http.Request, pubkeyHex string) int64 {
	// If logged in, try store first
	if pubkeyHex != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if timestamp, found, err := notificationReadStore.GetLastRead(ctx, pubkeyHex); err == nil && found {
			return timestamp
		}
	}

	// Fall back to cookie
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

// setNotificationsLastSeen updates the last seen timestamp
// For logged-in users, updates both Redis/store and cookie
func setNotificationsLastSeen(w http.ResponseWriter, r *http.Request, pubkeyHex string, timestamp int64) {
	// If logged in, update store
	if pubkeyHex != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		if err := notificationReadStore.SetLastRead(ctx, pubkeyHex, timestamp); err != nil {
			slog.Error("failed to set notification read state", "error", err)
		}
	}

	// Always set cookie as fallback
	http.SetCookie(w, &http.Cookie{
		Name:     "notifications_last_seen",
		Value:    fmt.Sprintf("%d", timestamp),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func checkUnreadNotifications(r *http.Request, session *BunkerSession, relays []string) bool {
	if session == nil || !session.Connected || session.UserPubKey == nil {
		return false
	}
	pubkeyHex := hex.EncodeToString(session.UserPubKey)
	lastSeen := getNotificationsLastSeen(r, pubkeyHex)
	return hasUnreadNotifications(relays, pubkeyHex, lastSeen)
}

func htmlTimelineHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	session := getSessionFromRequest(r)

	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	limit := parseLimit(q.Get("limit"), 50)
	since := parseInt64(q.Get("since"))
	until := parseInt64(q.Get("until"))
	cacheOnly := q.Get("cache_only") == "1"

	// Determine feed mode first (affects relay selection)
	feedMode := q.Get("feed") // "follows", "global", or "me"
	if feedMode == "" {
		if session != nil && session.Connected {
			feedMode = "follows"
		} else {
			feedMode = "global"
		}
	}

	// Relay selection is feed-aware:
	// - global: always use defaultRelays (aggregators for broad content)
	// - follows/me: use NIP-65 relays (user's chosen relays)
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		if feedMode == "global" {
			// Global feed always uses default relays (aggregators)
			relays = ConfigGetDefaultRelays()
			slog.Debug("using default relays for global feed", "count", len(relays))
		} else if session != nil && session.Connected {
			// Follows/Me feeds use user's NIP-65 relays
			if session.UserRelayList == nil && session.UserPubKey != nil {
				pubkeyHex := hex.EncodeToString(session.UserPubKey)
				slog.Debug("fetching relay list", "pubkey", pubkeyHex[:12])
				relayList := fetchRelayList(pubkeyHex)
				if relayList != nil {
					session.mu.Lock()
					session.UserRelayList = relayList
					session.mu.Unlock()
				}
			}

			if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
				relays = session.UserRelayList.Read
				slog.Debug("using user NIP-65 relays", "count", len(relays))
			}
		}

		if len(relays) == 0 {
			relays = ConfigGetDefaultRelays()
		}
	}

	// Feed=follows: fetch user's contact list
	if feedMode == "follows" && session != nil && session.Connected && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)

		contacts, ok := contactCache.Get(pubkeyHex)
		if !ok {
			slog.Debug("fetching contact list", "pubkey", pubkeyHex[:12])
			contacts = fetchContactList(relays, pubkeyHex)
			if contacts != nil {
				contactCache.Set(pubkeyHex, contacts)
				// Trigger background profile prefetch for contacts
				go prefetchContactProfiles(contacts)
			}
		} else {
			slog.Debug("contact cache hit", "pubkey", pubkeyHex[:12], "count", len(contacts))
		}

		if len(contacts) > 0 {
			// Exclude user's own pubkey from follows feed - it should only show others' content
			filteredContacts := make([]string, 0, len(contacts))
			for _, c := range contacts {
				if c != pubkeyHex {
					filteredContacts = append(filteredContacts, c)
				}
			}
			authors = filteredContacts
			slog.Debug("filtering to followed authors (excluding self)", "count", len(authors))
		}
	}

	// Feed=me: show only user's own notes
	if feedMode == "me" && session != nil && session.Connected && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		authors = []string{pubkeyHex}
		slog.Debug("showing notes for user", "pubkey", pubkeyHex[:12])
	}

	// Bookmarks (kind 10003): fetch user's bookmark list, then fetch bookmarked events
	var bookmarkedEventIDs []string
	isBookmarksView := len(kinds) == 1 && kinds[0] == 10003
	if isBookmarksView && session != nil && session.Connected {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		bookmarkEvents := fetchKind10003(relays, pubkeyHex)
		if len(bookmarkEvents) > 0 {
			for _, tag := range bookmarkEvents[0].Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					bookmarkedEventIDs = append(bookmarkedEventIDs, tag[1])
				}
			}
			slog.Debug("found bookmarked events", "count", len(bookmarkedEventIDs), "pubkey", pubkeyHex[:12])
		}
		kinds = nil // Fetch actual events by ID
	}

	noReplies := q.Get("no_replies") != "0" // Default: filter replies

	fetchLimit := limit
	if noReplies {
		fetchLimit = limit * 5 // Fetch more to compensate for reply filtering
	}

	var events []Event
	var eose bool

	if isBookmarksView && len(bookmarkedEventIDs) > 0 {
		filter := Filter{
			IDs:   bookmarkedEventIDs,
			Limit: len(bookmarkedEventIDs),
		}
		events, eose = fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
		slog.Debug("fetched bookmarked events", "count", len(events))
	} else if isBookmarksView {
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
		events, eose = fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
	}

	// Filter replies (kinds with ExcludeFromReplyFilter use e tags for references, not replies)
	if noReplies {
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			kindDef := GetKindDefinition(evt.Kind)
			if !isReply(evt) || kindDef.ExcludeFromReplyFilter {
				filtered = append(filtered, evt)
			}
		}
		events = filtered
		if len(events) > limit {
			events = events[:limit]
		}
	}

	// Filter events missing required tags
	{
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			kindDef := GetKindDefinition(evt.Kind)
			if !kindDef.HasRequiredTags(evt.Tags) {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	// Filter muted content
	if session != nil && session.Connected {
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			if session.IsEventFromMutedSource(evt.PubKey, evt.ID, evt.Content, evt.Tags) {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	// Collect pubkeys (including mentions) and event IDs for enrichment
	pubkeySet := make(map[string]bool)
	eventIDs := make([]string, 0, len(events))
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
		eventIDs = append(eventIDs, evt.ID)
		contents = append(contents, evt.Content)
	}

	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	// Extract referenced event IDs from reference-only reposts (kind 6 with empty content)
	// Also collect their authors (from p tags) for profile fetching
	repostRefs := extractRepostEventIDs(events)
	var repostEventIDs []string
	for _, evt := range events {
		if evt.Kind == 6 && strings.TrimSpace(evt.Content) == "" {
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					pubkeySet[tag[1]] = true // Add reposted author to profile fetch
					break
				}
			}
		}
	}
	for _, refID := range repostRefs {
		repostEventIDs = append(repostEventIDs, refID)
	}

	profiles := make(map[string]*ProfileInfo)
	reactions := make(map[string]*ReactionsSummary)
	replyCounts := make(map[string]int)
	repostEvents := make(map[string]*Event)
	var wg sync.WaitGroup

	if len(pubkeySet) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pubkeys := make([]string, 0, len(pubkeySet))
			for pk := range pubkeySet {
				pubkeys = append(pubkeys, pk)
			}
			profiles = fetchProfilesWithOptions(relays, pubkeys, cacheOnly)
		}()
	}

	if len(eventIDs) > 0 && !cacheOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replyCounts = fetchReplyCounts(relays, eventIDs)
		}()
	}

	if !cacheOnly && len(eventIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reactions = fetchReactions(relays, eventIDs)
		}()
	}

	// Fetch referenced events for reference-only reposts
	if len(repostEventIDs) > 0 && !cacheOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			filter := Filter{
				IDs:   repostEventIDs,
				Limit: len(repostEventIDs),
			}
			fetchedEvents, _ := fetchEventsFromRelaysCached(relays, filter)
			for i := range fetchedEvents {
				repostEvents[fetchedEvents[i].ID] = &fetchedEvents[i]
			}
		}()
	}

	wg.Wait()

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

	// Pagination (subtract 1 from until - Nostr's filter is inclusive)
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		resp.Page.Until = &lastCreatedAt
		nextURL := r.URL.Path + "?" +
			"until=" + strconv.FormatInt(lastCreatedAt-1, 10) +
			"&limit=" + strconv.Itoa(limit)
		// Only include authors if not using feed mode (follows/me fetch authors server-side)
		// This prevents massive URLs with 100+ authors in the query string
		if len(authors) > 0 && feedMode != "follows" && feedMode != "me" {
			nextURL += "&authors=" + strings.Join(authors, ",")
		}
		if len(kinds) > 0 {
			kindsStr := make([]string, len(kinds))
			for i, k := range kinds {
				kindsStr[i] = strconv.Itoa(k)
			}
			nextURL += "&kinds=" + strings.Join(kindsStr, ",")
		}
		nextURL += "&feed=" + feedMode
		resp.Page.Next = &nextURL

		// Prefetch next page with semaphore to limit concurrent goroutines
		// Non-blocking acquire: skip prefetch if too many already running
		select {
		case prefetchSemaphore <- struct{}{}:
			prefetchCtx, prefetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
			go func() {
				defer func() { <-prefetchSemaphore }() // Release semaphore
				defer prefetchCancel()
				prefetchNextPage(prefetchCtx, relays, authors, kinds, limit, lastCreatedAt-1, noReplies)
			}()
		default:
			// Semaphore full, skip prefetch to prevent overload
			slog.Debug("skipping prefetch, too many concurrent prefetches")
		}
	}

	flash := getFlashMessages(w, r)
	errorMsg := flash.Error
	successMsg := flash.Success
	currentURL := buildCurrentURL(r, "cache_only", "refresh")
	themeClass, themeLabel := getThemeFromRequest(r)

	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)
	refresh := q.Get("refresh") == "1"
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh // cache_only/refresh requests need full page for body morph
	isAppend := q.Get("append") == "1"

	var newestTimestamp int64
	if len(items) > 0 {
		newestTimestamp = items[0].CreatedAt
	}

	html, err := renderHTML(resp, relays, authors, kinds, limit, session, errorMsg, successMsg, feedMode, currentURL, themeClass, themeLabel, csrfToken, hasUnreadNotifs, isFragment, isAppend, newestTimestamp, repostEvents)
	if err != nil {
		slog.Error("error rendering HTML", "error", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=5")
	w.Write([]byte(html))
}

func htmlThreadHandler(w http.ResponseWriter, r *http.Request) {
	identifier := strings.TrimPrefix(r.URL.Path, "/html/thread/") // hex, note1, nevent1, naddr1
	if identifier == "" {
		http.Error(w, "Invalid event identifier", http.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = ConfigGetDefaultRelays()
	}
	cacheOnly := q.Get("cache_only") == "1"

	var eventID string
	var naddrRef *NAddr

	switch {
	case strings.HasPrefix(identifier, "naddr1"):
		na, err := DecodeNAddr(identifier)
		if err != nil {
			http.Error(w, "Invalid naddr identifier", http.StatusBadRequest)
			return
		}
		naddrRef = na
	case strings.HasPrefix(identifier, "nevent1"):
		decoded, err := DecodeNEvent(identifier)
		if err != nil {
			http.Error(w, "Invalid nevent identifier", http.StatusBadRequest)
			return
		}
		eventID = decoded.EventID
		if len(decoded.RelayHints) > 0 {
			relays = append(decoded.RelayHints, relays...)
		}
	case strings.HasPrefix(identifier, "note1"):
		decoded, err := DecodeNote(identifier)
		if err != nil {
			http.Error(w, "Invalid note identifier", http.StatusBadRequest)
			return
		}
		eventID = decoded
	case isValidEventID(identifier):
		eventID = identifier
	default:
		http.Error(w, "Invalid event identifier", http.StatusBadRequest)
		return
	}

	slog.Debug("fetching thread", "identifier", identifier)

	var rootEvent *Event
	var replies []Event
	var cacheHit bool

	// Try thread cache first (only for direct event ID lookups, not naddr)
	if naddrRef == nil && eventID != "" {
		ctx := context.Background()
		cached, found, err := threadCacheStore.Get(ctx, eventID)
		if err == nil && found {
			rootEvent = &cached.RootEvent
			replies = cached.Replies
			cacheHit = true
			slog.Debug("thread cache hit", "event_id", eventID, "replies", len(replies))

			// Fetch new replies since cache time (skip in cache_only mode)
			if !cacheOnly {
				since := cached.CachedAt
				newReplies := fetchRepliesSince(relays, []string{eventID}, since)
				if len(newReplies) > 0 {
					slog.Debug("thread cache found new replies", "count", len(newReplies))
					// Merge new replies, avoiding duplicates
					existingIDs := make(map[string]bool)
					for _, r := range replies {
						existingIDs[r.ID] = true
					}
					for _, r := range newReplies {
						if !existingIDs[r.ID] {
							replies = append(replies, r)
						}
					}
				}
			}
		}
	}

	// Fetch from relays if not in cache (skip in cache_only mode)
	if !cacheHit && !cacheOnly {
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			if naddrRef != nil {
				events := fetchAddressableEvent(relays, naddrRef.Kind, naddrRef.Author, naddrRef.DTag)
				if len(events) > 0 {
					rootEvent = &events[0]
				}
			} else {
				events := fetchEventByID(relays, eventID)
				if len(events) > 0 {
					rootEvent = &events[0]
				}
			}
		}()

		if eventID != "" { // For naddr, wait for root event to get ID
			wg.Add(1)
			go func() {
				defer wg.Done()
				replies = fetchReplies(relays, []string{eventID})
			}()
		}

		wg.Wait()

		if rootEvent == nil {
			http.Error(w, "Event not found", http.StatusNotFound)
			return
		}

		if naddrRef != nil && rootEvent != nil {
			replies = fetchReplies(relays, []string{rootEvent.ID})
		}

		// Cache the thread (only for regular event IDs, not naddr)
		if naddrRef == nil && rootEvent != nil {
			ctx := context.Background()
			cached := &CachedThread{
				RootEvent: *rootEvent,
				Replies:   replies,
				CachedAt:  time.Now().Unix(),
			}
			if err := threadCacheStore.Set(ctx, rootEvent.ID, cached, cacheConfig.ThreadTTL); err != nil {
				slog.Warn("failed to cache thread", "error", err)
			}
		}
	}

	if rootEvent == nil {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	// Check if root is a reference-only repost (kind 6 with empty content)
	// If so, fetch the referenced event and its author's profile
	repostEvents := make(map[string]*Event)
	var repostAuthorPubkey string
	if rootEvent.Kind == 6 && strings.TrimSpace(rootEvent.Content) == "" {
		for _, tag := range rootEvent.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				// Fetch the referenced event
				filter := Filter{
					IDs:   []string{tag[1]},
					Limit: 1,
				}
				fetchedEvents, _ := fetchEventsFromRelaysCached(relays, filter)
				if len(fetchedEvents) > 0 {
					repostEvents[fetchedEvents[0].ID] = &fetchedEvents[0]
				}
				break
			}
		}
		// Get the original author from p tag for profile fetch
		for _, tag := range rootEvent.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				repostAuthorPubkey = tag[1]
				break
			}
		}
	}

	session := getSessionFromRequest(r)

	// Filter muted replies
	if session != nil && session.Connected {
		filtered := make([]Event, 0, len(replies))
		for _, reply := range replies {
			if session.IsEventFromMutedSource(reply.PubKey, reply.ID, reply.Content, reply.Tags) {
				continue
			}
			filtered = append(filtered, reply)
		}
		replies = filtered
	}

	// Collect pubkeys (including mentions) for profile enrichment
	pubkeySet := make(map[string]bool)
	pubkeySet[rootEvent.PubKey] = true
	if repostAuthorPubkey != "" {
		pubkeySet[repostAuthorPubkey] = true
	}
	contents := []string{rootEvent.Content}
	for _, reply := range replies {
		pubkeySet[reply.PubKey] = true
		contents = append(contents, reply.Content)
	}

	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	allEventIDs := make([]string, 0, 1+len(replies))
	allEventIDs = append(allEventIDs, rootEvent.ID)
	for _, reply := range replies {
		allEventIDs = append(allEventIDs, reply.ID)
	}

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
		profiles = fetchProfilesWithOptions(relays, pubkeys, cacheOnly)
	}()

	if !cacheOnly {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			replyCounts = fetchReplyCounts(relays, allEventIDs)
		}()
	}

	wg2.Wait()

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

	sort.Slice(replyItems, func(i, j int) bool { // Oldest first
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

	currentURL := buildCurrentURL(r, "cache_only", "refresh")
	themeClass, themeLabel := getThemeFromRequest(r)
	flash := getFlashMessages(w, r)
	successMsg := flash.Success

	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)
	refresh := q.Get("refresh") == "1"
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh // cache_only/refresh requests need full page for body morph

	htmlContent, err := renderThreadHTML(resp, relays, session, currentURL, themeClass, themeLabel, successMsg, csrfToken, hasUnreadNotifs, isFragment, repostEvents)
	if err != nil {
		slog.Error("error rendering thread HTML", "error", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=10")
	w.Write([]byte(htmlContent))
}

func htmlProfileHandler(w http.ResponseWriter, r *http.Request) {
	pubkey := strings.TrimPrefix(r.URL.Path, "/html/profile/")
	if pubkey == "" {
		http.Error(w, "Pubkey required", http.StatusBadRequest)
		return
	}

	if strings.HasPrefix(pubkey, "npub1") { // Decode npub to hex
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
		relays = ConfigGetDefaultRelays()
	}
	cacheOnly := q.Get("cache_only") == "1"

	limit := parseLimit(q.Get("limit"), 10)
	until := parseInt64(q.Get("until"))

	slog.Debug("fetching profile", "pubkey", pubkey[:16])

	var profile *ProfileInfo
	var events []Event
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		profiles := fetchProfilesWithOptions(relays, []string{pubkey}, cacheOnly)
		profile = profiles[pubkey]
	}()

	wg.Add(1) // Fetch user's top-level notes
	go func() {
		defer wg.Done()
		filter := Filter{
			Authors: []string{pubkey},
			Kinds:   []int{1},
			Limit:   limit * 2, // Fetch more since we'll filter out replies
			Until:   until,
		}
		events, _ = fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
	}()

	wg.Wait()

	// Filter replies
	topLevelNotes := make([]Event, 0, len(events))
	for _, evt := range events {
		if !isReply(evt) {
			topLevelNotes = append(topLevelNotes, evt)
		}
	}
	if len(topLevelNotes) > limit {
		topLevelNotes = topLevelNotes[:limit]
	}

	// Fetch profiles for mentioned pubkeys
	contents := make([]string, len(topLevelNotes))
	for i, evt := range topLevelNotes {
		contents[i] = evt.Content
	}
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	if len(mentionedPubkeys) > 0 {
		fetchProfilesWithOptions(relays, mentionedPubkeys, cacheOnly)
	}

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

	// Pagination (subtract 1 - until is inclusive)
	var pageUntil *int64
	var nextURL *string
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		pageUntil = &lastCreatedAt
		next := fmt.Sprintf("/html/profile/%s?limit=%d&until=%d", pubkey, limit, lastCreatedAt-1)
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

	themeClass, themeLabel := getThemeFromRequest(r)
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	var isFollowing, isSelf bool
	if session != nil && session.Connected {
		userPubkeyHex := hex.EncodeToString(session.UserPubKey)
		isSelf = pubkey == userPubkeyHex

		session.mu.Lock()
		for _, followedPubkey := range session.FollowingPubkeys {
			if followedPubkey == pubkey {
				isFollowing = true
				break
			}
		}
		session.mu.Unlock()
	}

	currentURL := buildCurrentURL(r, "cache_only", "refresh")

	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)
	refresh := q.Get("refresh") == "1"
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh // cache_only/refresh requests need full page for body morph
	isAppend := q.Get("append") == "1"

	htmlContent, err := renderProfileHTML(resp, relays, limit, themeClass, themeLabel, loggedIn, currentURL, csrfToken, isFollowing, isSelf, hasUnreadNotifs, isFragment, isAppend, session)
	if err != nil {
		slog.Error("error rendering profile HTML", "error", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=30")
	w.Write([]byte(htmlContent))
}

func htmlThemeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	currentTheme := ""
	if cookie, err := r.Cookie("theme"); err == nil {
		currentTheme = cookie.Value
	}

	// Cycle: "" (system) -> "dark" -> "light" -> "" (system)
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

	http.SetCookie(w, &http.Cookie{
		Name:     "theme",
		Value:    newTheme,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60, // 1 year
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Redirect back (extract path only to prevent open redirect)
	returnURL := r.Header.Get("Referer")
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
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected
	returnURL = sanitizeReturnURL(returnURL, loggedIn)
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

	// Get user's relays: combine NIP-65 + defaults for better notification coverage
	// People tagging you might publish to aggregator relays, not just your NIP-65 relays
	relays := ConfigGetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		// Combine NIP-65 read relays with defaults, deduplicate
		relaySet := make(map[string]bool)
		combined := make([]string, 0, len(session.UserRelayList.Read)+len(relays))
		// NIP-65 relays first (user's preference)
		for _, r := range session.UserRelayList.Read {
			if !relaySet[r] {
				relaySet[r] = true
				combined = append(combined, r)
			}
		}
		// Then default relays (aggregators)
		for _, r := range relays {
			if !relaySet[r] {
				relaySet[r] = true
				combined = append(combined, r)
			}
		}
		relays = combined
		slog.Debug("notification relays combined", "nip65", len(session.UserRelayList.Read), "defaults", len(ConfigGetDefaultRelays()), "total", len(relays))
	}
	cacheOnly := r.URL.Query().Get("cache_only") == "1"

	// Fetch notifications - hasMore indicates if there are more beyond limit
	const limit = 10
	notifications, hasMore := fetchNotificationsWithOptions(relays, pubkeyHex, limit, until, cacheOnly)

	// Filter out notifications from muted users
	{
		filtered := make([]Notification, 0, len(notifications))
		for _, notif := range notifications {
			if session.IsEventFromMutedSource(notif.Event.PubKey, notif.Event.ID, notif.Event.Content, notif.Event.Tags) {
				continue // Skip notifications from muted sources
			}
			filtered = append(filtered, notif)
		}
		notifications = filtered
	}

	// Collect pubkeys for profile enrichment and target event IDs
	pubkeySet := make(map[string]bool)
	targetEventIDs := make([]string, 0)
	for _, notif := range notifications {
		pubkeySet[notif.Event.PubKey] = true
		// For zaps, also collect the actual sender pubkey (not LNURL provider)
		if notif.Type == NotificationZap && notif.ZapSenderPubkey != "" {
			pubkeySet[notif.ZapSenderPubkey] = true
		}
		// Collect target event IDs for reactions/reposts/zaps
		if notif.TargetEventID != "" && (notif.Type == NotificationReaction || notif.Type == NotificationRepost || notif.Type == NotificationZap) {
			targetEventIDs = append(targetEventIDs, notif.TargetEventID)
		}
	}
	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}

	// Fetch profiles
	profiles := fetchProfilesWithOptions(relays, pubkeys, cacheOnly)

	// Fetch target events for reactions/reposts to show context
	targetEvents := make(map[string]*Event)
	if len(targetEventIDs) > 0 {
		// Fetch events by their IDs using a filter
		filter := Filter{
			IDs:   targetEventIDs,
			Limit: len(targetEventIDs),
		}
		events, _ := fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
		for i := range events {
			targetEvents[events[i].ID] = &events[i]
		}
	}

	// Collect all content for batch processing (nostr refs and link previews)
	var contents []string
	for _, notif := range notifications {
		// Add notification content (for replies, mentions, quotes)
		if notif.Type != NotificationReaction && notif.Type != NotificationRepost && notif.Type != NotificationZap {
			contents = append(contents, notif.Event.Content)
		}
	}
	// Also add target event contents
	for _, ev := range targetEvents {
		contents = append(contents, ev.Content)
	}

	// Batch resolve nostr references
	nostrRefs := extractNostrRefs(contents)
	resolvedRefs := batchResolveNostrRefs(nostrRefs, relays)

	// Batch fetch link previews
	var allURLs []string
	for _, content := range contents {
		allURLs = append(allURLs, ExtractPreviewableURLs(content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

	// Collect q tags for quote posts (from mentions and replies)
	var qTagValues []string
	for _, notif := range notifications {
		if notif.Type == NotificationMention || notif.Type == NotificationReply {
			kindDef := GetKindDefinition(notif.Event.Kind)
			if kindDef.SupportsQuotePosts {
				for _, tag := range notif.Event.Tags {
					if len(tag) >= 2 && tag[0] == "q" {
						qTagValues = append(qTagValues, tag[1])
						break
					}
				}
			}
		}
	}

	// Fetch quoted events and their profiles
	quotedEvents, quotedEventProfiles := fetchQuotedEvents(qTagValues)

	// Add quoted event profiles to the profiles map
	for pk, profile := range quotedEventProfiles {
		if _, exists := profiles[pk]; !exists {
			profiles[pk] = profile
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

	// Calculate pagination - use hasMore flag from fetch (accounts for cache total)
	var pagination *HTMLPagination
	if hasMore && len(notifications) > 0 {
		// Use the timestamp of the last notification for the next page
		// Subtract 1 to exclude the last item from the next page (until is inclusive)
		lastNotif := notifications[len(notifications)-1]
		nextUntil := lastNotif.Event.CreatedAt - 1
		pagination = &HTMLPagination{
			Next: fmt.Sprintf("/html/notifications?until=%d", nextUntil),
		}
	}

	// Update last seen timestamp - current time (only on first page)
	if until == nil {
		setNotificationsLastSeen(w, r, pubkeyHex, time.Now().Unix())
	}

	// Check if this is a HelmJS partial request
	isFragment := isHelmRequest(r) && !cacheOnly // cache_only requests need full page for body morph
	isAppend := r.URL.Query().Get("append") == "1"

	// Render template
	htmlContent, err := renderNotificationsHTML(notifications, profiles, targetEvents, relays, resolvedRefs, linkPreviews, quotedEvents, themeClass, themeLabel, userDisplayName, pubkeyHex, pagination, isFragment, isAppend)
	if err != nil {
		slog.Error("error rendering notifications HTML", "error", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(htmlContent))
}

// prefetchNextPage warms the cache for the next page of events
// Takes a context to allow cancellation when the parent request is cancelled
func prefetchNextPage(ctx context.Context, relays, authors []string, kinds []int, limit int, until int64, noReplies bool) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return
	default:
	}

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

	events, _ := fetchEventsFromRelaysCached(relays, filter)

	if len(events) == 0 {
		return
	}

	// Check context before profile fetch
	select {
	case <-ctx.Done():
		slog.Debug("prefetch cancelled after fetching events", "count", len(events))
		return
	default:
	}

	pubkeySet := make(map[string]bool)
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
		contents = append(contents, evt.Content)
	}

	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}

	if len(pubkeySet) > 0 {
		pubkeys := make([]string, 0, len(pubkeySet))
		for pk := range pubkeySet {
			pubkeys = append(pubkeys, pk)
		}
		fetchProfiles(relays, pubkeys)
	}

	slog.Debug("prefetch warmed cache", "events", len(events), "profiles", len(pubkeySet))
}

func htmlMutesHandler(w http.ResponseWriter, r *http.Request) {
	// Get session - must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	// Get muted pubkeys from session cache
	session.mu.Lock()
	mutedPubkeys := make([]string, len(session.MutedPubkeys))
	copy(mutedPubkeys, session.MutedPubkeys)
	session.mu.Unlock()
	slog.Debug("mutes page: loaded from session", "count", len(mutedPubkeys))

	// Get relays for profile fetching
	relays := ConfigGetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays = session.UserRelayList.Read
	}
	cacheOnly := r.URL.Query().Get("cache_only") == "1"

	// Fetch profiles for muted users
	profiles := make(map[string]*ProfileInfo)
	if len(mutedPubkeys) > 0 {
		profiles = fetchProfilesWithOptions(relays, mutedPubkeys, cacheOnly)
	}

	// Build muted user list
	mutedUsers := make([]HTMLMutedUser, 0, len(mutedPubkeys))
	for _, pubkey := range mutedPubkeys {
		npub, _ := encodeBech32Pubkey(pubkey)
		npubShort := npub
		if len(npubShort) > 16 {
			npubShort = npubShort[:12] + "..."
		}

		mutedUsers = append(mutedUsers, HTMLMutedUser{
			Pubkey:    pubkey,
			Npub:      npub,
			NpubShort: npubShort,
			Profile:   profiles[pubkey],
		})
	}

	// Get user info for header
	userPubkey := hex.EncodeToString(session.UserPubKey)
	userDisplayName := getUserDisplayName(userPubkey)
	themeClass, themeLabel := getThemeFromRequest(r)
	csrfToken := generateCSRFToken(session.ID)

	// Render
	isFragment := isHelmRequest(r) && !cacheOnly // cache_only requests need full page for body morph
	html, err := renderMutesHTML(mutedUsers, themeClass, themeLabel, userDisplayName, userPubkey, csrfToken, isFragment)
	if err != nil {
		slog.Error("error rendering mutes", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func htmlWalletHandler(w http.ResponseWriter, r *http.Request) {
	// Get session - must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	// Get return URL from query param (for redirect after connect)
	returnURL := r.URL.Query().Get("return")
	if returnURL == "" {
		returnURL = DefaultTimelineURLLoggedIn()
	}

	// Get user info for header
	userPubkey := hex.EncodeToString(session.UserPubKey)
	userDisplayName := getUserDisplayName(userPubkey)
	themeClass, themeLabel := getThemeFromRequest(r)
	csrfToken := generateCSRFToken(session.ID)

	// Get flash messages
	flash := getFlashMessages(w, r)

	// Build data
	hasWallet := session.HasWallet()
	data := HTMLWalletData{
		Title:           I18n("nav.wallet"),
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		UserDisplayName: userDisplayName,
		UserPubKey:      userPubkey,
		CurrentURL:      "/html/wallet",
		CSRFToken:       csrfToken,
		ReturnURL:       returnURL,
		HasWallet:       hasWallet,
		WalletRelay:     session.GetWalletRelay(),
		WalletDomain:    extractDomain(session.GetWalletRelay()),
		Success:         flash.Success,
		Error:           flash.Error,
		LoggedIn:        true,
	}

	// Prefetch wallet info if wallet is connected (warms the cache for /html/wallet/info)
	if hasWallet && session.NWCConfig != nil {
		PrefetchWalletInfo(userPubkey, session.NWCConfig)
	}

	// Build navigation items
	data.FeedModes = GetFeedModes(FeedModeContext{
		LoggedIn:   true,
		ActiveFeed: "",
	})
	// No KindFilters for wallet page - it's a settings page, not a timeline
	data.NavItems = GetNavItems(NavContext{
		LoggedIn:   true,
		ActivePage: "wallet",
	})
	settingsCtx := SettingsContext{
		LoggedIn:      true,
		ThemeLabel:    themeLabel,
		UserAvatarURL: getUserAvatarURL(userPubkey),
	}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	// Render
	cacheOnly := r.URL.Query().Get("cache_only") == "1"
	refresh := r.URL.Query().Get("refresh") == "1"
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh
	html, err := renderWalletHTML(data, isFragment)
	if err != nil {
		slog.Error("error rendering wallet", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func getSearchRelays() []string {
	return ConfigGetSearchRelays()
}

func htmlSearchHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cacheOnly := r.URL.Query().Get("cache_only") == "1"
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	var userPubKey, userDisplayName, csrfToken string
	var hasUnreadNotifs bool
	if loggedIn {
		userPubKey = hex.EncodeToString(session.UserPubKey)
		userDisplayName = getUserDisplayName(userPubKey)
		csrfToken = generateCSRFToken(session.ID)

		relays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			relays = session.UserRelayList.Read
		}
		lastSeen := getNotificationsLastSeen(r, userPubKey)
		hasUnreadNotifs = hasUnreadNotifications(relays, userPubKey, lastSeen)
	}

	themeClass, themeLabel := getThemeFromRequest(r)
	isFragment := isHelmRequest(r) && !cacheOnly // cache_only requests need full page for body morph
	isAppend := r.URL.Query().Get("append") == "1"
	hTarget := r.Header.Get("H-Target")
	// Live search: return just #search-results, not full fragment with form
	isLiveSearch := isFragment && !isAppend && (hTarget == "#search-results" || hTarget == "")

	if query == "" {
		html, err := renderSearchHTML(nil, nil, query, themeClass, themeLabel, loggedIn, userPubKey, userDisplayName, csrfToken, hasUnreadNotifs, nil, isFragment, false, isLiveSearch, nil, nil)
		if err != nil {
			http.Error(w, "Failed to render search page", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	var until *int64
	if untilStr := r.URL.Query().Get("until"); untilStr != "" {
		if u, err := strconv.ParseInt(untilStr, 10, 64); err == nil {
			until = &u
		}
	}

	const limit = 25
	filter := Filter{
		Kinds:  []int{1},
		Limit:  limit + 1, // Request one extra to check if there are more
		Search: query,
		Until:  until,
	}

	// Check search cache first (only for initial search, not paginated)
	var events []Event
	var cacheHit bool
	if until == nil {
		ctx := context.Background()
		cachedEvents, found, err := searchCacheStore.Get(ctx, query, filter.Kinds, limit+1)
		if err == nil && found {
			events = cachedEvents
			cacheHit = true
			slog.Debug("search cache hit", "query", query, "results", len(events))
		}
	}

	// Fetch from relays if not in cache (skip in cache_only mode)
	if !cacheHit && !cacheOnly {
		slog.Debug("searching NIP-50 relays", "query", query)
		events, _ = fetchEventsFromRelaysWithTimeout(getSearchRelays(), filter, 3*time.Second)
		slog.Debug("search returned results", "count", len(events))

		// Cache the results (only for initial search, not paginated)
		if until == nil && len(events) > 0 {
			ctx := context.Background()
			if err := searchCacheStore.Set(ctx, query, filter.Kinds, limit+1, events, cacheConfig.SearchResultTTL); err != nil {
				slog.Warn("failed to cache search results", "error", err)
			}
		}
	}

	// Filter muted content
	if session != nil && session.Connected {
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			if session.IsEventFromMutedSource(evt.PubKey, evt.ID, evt.Content, evt.Tags) {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	var pagination *HTMLPagination
	if len(events) > limit {
		events = events[:limit]
		lastEvent := events[len(events)-1]
		pagination = &HTMLPagination{
			Next: fmt.Sprintf("/html/search?q=%s&until=%d", url.QueryEscape(query), lastEvent.CreatedAt-1),
		}
	}

	pubkeySet := make(map[string]bool)
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
	}
	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}

	profiles := fetchProfilesWithOptions(getSearchRelays(), pubkeys, cacheOnly)

	var qTagValues []string
	for _, evt := range events {
		kindDef := GetKindDefinition(evt.Kind)
		if kindDef.SupportsQuotePosts {
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "q" {
					qTagValues = append(qTagValues, tag[1])
					break // Only one q tag per event
				}
			}
		}
	}

	quotedEvents, quotedEventProfiles := fetchQuotedEvents(qTagValues)
	for pk, profile := range quotedEventProfiles {
		if _, exists := profiles[pk]; !exists {
			profiles[pk] = profile
		}
	}

	html, err := renderSearchHTML(events, profiles, query, themeClass, themeLabel, loggedIn, userPubKey, userDisplayName, csrfToken, hasUnreadNotifs, pagination, isFragment, isAppend, isLiveSearch, getSearchRelays(), quotedEvents)
	if err != nil {
		http.Error(w, "Failed to render search results", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// NewNotesIndicatorData holds data for the new notes indicator template.
type NewNotesIndicatorData struct {
	Since      int64  // Timestamp for polling continuation
	Kinds      string // Kinds param for polling (e.g., "1,6")
	Filter     string // Filter name for label (e.g., "notes", "all")
	RefreshURL string // URL to navigate to when clicked
	Count      int    // Number of new posts
	Label      string // Display label (e.g., "5 new notes")
}

// getFilterLabel returns the i18n-aware label for new posts based on filter name
func getFilterLabel(filter string, count int) string {
	// Get singular/plural word based on filter
	var word string
	switch filter {
	case "notes":
		if count == 1 {
			word = I18n("new_posts.note")
		} else {
			word = I18n("new_posts.notes")
		}
	case "photos":
		if count == 1 {
			word = I18n("new_posts.photo")
		} else {
			word = I18n("new_posts.photos")
		}
	case "longform":
		if count == 1 {
			word = I18n("new_posts.article")
		} else {
			word = I18n("new_posts.articles")
		}
	case "highlights":
		if count == 1 {
			word = I18n("new_posts.highlight")
		} else {
			word = I18n("new_posts.highlights")
		}
	case "live":
		if count == 1 {
			word = I18n("new_posts.stream")
		} else {
			word = I18n("new_posts.streams")
		}
	case "classifieds":
		if count == 1 {
			word = I18n("new_posts.listing")
		} else {
			word = I18n("new_posts.listings")
		}
	case "shorts":
		if count == 1 {
			word = I18n("new_posts.short")
		} else {
			word = I18n("new_posts.shorts")
		}
	default: // "all" or unknown
		if count == 1 {
			word = I18n("new_posts.post")
		} else {
			word = I18n("new_posts.posts")
		}
	}
	return fmt.Sprintf("%d new %s", count, word)
}

func renderNewNotesIndicator(data NewNotesIndicatorData) string {
	var buf strings.Builder
	if err := cachedNewNotesIndicator.ExecuteTemplate(&buf, "new-notes-indicator", data); err != nil {
		slog.Error("failed to render new notes indicator", "error", err)
		return `<div id="new-notes-indicator"></div>`
	}
	return buf.String()
}

// htmlCheckNewNotesHandler checks for new posts (polling endpoint for "X new posts" indicator)
func htmlCheckNewNotesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	q := r.URL.Query()
	sinceStr := q.Get("since")
	kindsStr := q.Get("kinds")
	filter := q.Get("filter")
	refreshURL := q.Get("url")

	if sinceStr == "" {
		slog.Debug("check new posts: missing since param")
		w.Write([]byte(`<div id="new-notes-indicator"></div>`))
		return
	}

	since, err := strconv.ParseInt(sinceStr, 10, 64)
	if err != nil {
		slog.Debug("check new posts: invalid since param", "since", sinceStr)
		w.Write([]byte(`<div id="new-notes-indicator"></div>`))
		return
	}

	// Default filter name
	if filter == "" {
		filter = "all"
	}

	// Default refresh URL
	if refreshURL == "" {
		refreshURL = "/html/timeline?feed=follows"
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		slog.Debug("check new posts: no session or not connected")
		data := NewNotesIndicatorData{Since: since, Kinds: kindsStr, Filter: filter, RefreshURL: refreshURL}
		w.Write([]byte(renderNewNotesIndicator(data)))
		return
	}

	relays := ConfigGetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays = session.UserRelayList.Read
	}

	pubkeyHex := hex.EncodeToString(session.UserPubKey)
	contacts, ok := contactCache.Get(pubkeyHex)
	if !ok || len(contacts) == 0 {
		contacts = fetchContactList(relays, pubkeyHex)
		if contacts == nil || len(contacts) == 0 {
			slog.Debug("check new posts: no contacts", "pubkey", pubkeyHex[:12])
			data := NewNotesIndicatorData{Since: since, Kinds: kindsStr, Filter: filter, RefreshURL: refreshURL}
			w.Write([]byte(renderNewNotesIndicator(data)))
			return
		}
		contactCache.Set(pubkeyHex, contacts)
		slog.Debug("check new posts: refreshed contacts", "count", len(contacts), "pubkey", pubkeyHex[:12])
	}

	// Parse kinds from param, default to kind 1 if empty
	kinds := parseIntList(kindsStr)
	if len(kinds) == 0 {
		kinds = []int{1}
	}

	// Query for posts AFTER current newest (hence since+1)
	queryFilter := Filter{
		Authors: contacts,
		Kinds:   kinds,
		Since:   &[]int64{since + 1}[0],
		Limit:   50,
	}

	events, _ := fetchEventsFromRelaysWithTimeout(relays, queryFilter, 3*time.Second)
	slog.Debug("check new posts: found events", "since", since, "kinds", kinds, "count", len(events), "contacts", len(contacts))

	// Filter replies and user's own posts
	filtered := make([]Event, 0, len(events))
	for _, evt := range events {
		// Reposts (kind 6) use e tags for reference, not reply
		kindDef := GetKindDefinition(evt.Kind)
		if kindDef.ExcludeFromReplyFilter || !isReply(evt) {
			if evt.PubKey != pubkeyHex {
				filtered = append(filtered, evt)
			}
		}
	}
	events = filtered
	slog.Debug("check new posts: after filtering", "count", len(events))

	data := NewNotesIndicatorData{
		Since:      since,
		Kinds:      kindsStr,
		Filter:     filter,
		RefreshURL: refreshURL,
		Count:      len(events),
	}

	if len(events) > 0 {
		data.Label = getFilterLabel(filter, len(events))
	}

	w.Write([]byte(renderNewNotesIndicator(data)))
}

// htmlGifsHandler handles the GIF search page/panel
// GET /html/gifs - returns full page (no-JS) or panel fragment (HelmJS)
func htmlGifsHandler(w http.ResponseWriter, r *http.Request) {
	if !GiphyEnabled() {
		http.Error(w, "GIF search not available", http.StatusNotFound)
		return
	}

	query := r.URL.Query().Get("q")
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		targetID = "post" // Default target for main post form
	}

	isFragment := isHelmRequest(r)

	if isFragment {
		// Return just the GIF panel for HelmJS inline insert
		html, err := renderGifPanel(targetID)
		if err != nil {
			slog.Error("error rendering gif panel", "error", err)
			http.Error(w, "Failed to render GIF panel", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	// Full page render for no-JS
	session := getSessionFromRequest(r)
	themeClass, themeLabel := getThemeFromRequest(r)

	var results []GifResult
	if query != "" {
		client := GetGiphyClient()
		if client != nil {
			var err error
			results, err = client.Search(query, 20)
			if err != nil {
				slog.Error("giphy search error", "error", err)
			}
		}
	}

	data := struct {
		ThemeClass     string
		ThemeLabel     string
		Title          string
		LoggedIn       bool
		CSRFToken      string
		Query          string
		Results        []GifResult
		FeedModes      []FeedMode
		NavItems       []NavItem
		SettingsItems  []SettingsItem
		SettingsToggle SettingsToggle
		CurrentURL     string
	}{
		ThemeClass:  themeClass,
		ThemeLabel:  themeLabel,
		Title:       "Search GIFs",
		LoggedIn:    session != nil && session.Connected,
		Query:       query,
		Results:     results,
		CurrentURL:  r.URL.String(),
	}

	if data.LoggedIn {
		data.CSRFToken = generateCSRFToken(session.ID)
	}

	// Build navigation
	feedModeCtx := FeedModeContext{LoggedIn: data.LoggedIn, ActiveFeed: "", CurrentPage: "gifs"}
	data.FeedModes = GetFeedModes(feedModeCtx)
	navCtx := NavContext{LoggedIn: data.LoggedIn, ActivePage: "gifs"}
	data.NavItems = GetNavItems(navCtx)
	settingsCtx := SettingsContext{LoggedIn: data.LoggedIn, ThemeLabel: themeLabel}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if err := cachedGifsTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
		slog.Error("error rendering gifs page", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// htmlGifsSearchHandler handles GIF search requests
// GET /html/gifs/search?q=... - returns results grid fragment
func htmlGifsSearchHandler(w http.ResponseWriter, r *http.Request) {
	if !GiphyEnabled() {
		http.Error(w, "GIF search not available", http.StatusNotFound)
		return
	}

	query := r.URL.Query().Get("q")
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		targetID = "post"
	}

	if !isHelmRequest(r) {
		// No-JS fallback: redirect to full page
		http.Redirect(w, r, "/html/gifs?q="+url.QueryEscape(query), http.StatusSeeOther)
		return
	}

	var results []GifResult
	if query != "" {
		client := GetGiphyClient()
		if client != nil {
			var err error
			results, err = client.Search(query, 20)
			if err != nil {
				slog.Error("giphy search error", "error", err)
			}
		}
	}

	html, err := renderGifResults(results, query, targetID)
	if err != nil {
		slog.Error("error rendering gif results", "error", err)
		http.Error(w, "Failed to render results", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// htmlGifsSelectHandler handles GIF selection
// GET /html/gifs/select?url=...&thumb=... - returns attachment preview + OOB panel clear
func htmlGifsSelectHandler(w http.ResponseWriter, r *http.Request) {
	gifURL := r.URL.Query().Get("url")
	thumbURL := r.URL.Query().Get("thumb")
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		targetID = "post"
	}

	if gifURL == "" {
		http.Error(w, "Missing url parameter", http.StatusBadRequest)
		return
	}

	if thumbURL == "" {
		thumbURL = gifURL
	}

	if !isHelmRequest(r) {
		// No-JS fallback: redirect to compose page
		http.Redirect(w, r, "/html/compose?media_url="+url.QueryEscape(gifURL)+"&media_thumb="+url.QueryEscape(thumbURL), http.StatusSeeOther)
		return
	}

	html, err := renderGifAttachment(gifURL, thumbURL, targetID)
	if err != nil {
		slog.Error("error rendering gif attachment", "error", err)
		http.Error(w, "Failed to render attachment", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// htmlGifsClearHandler clears the GIF attachment
// GET /html/gifs/clear - returns empty content to clear the attachment area
func htmlGifsClearHandler(w http.ResponseWriter, r *http.Request) {
	if !isHelmRequest(r) {
		// No-JS fallback: redirect back
		referer := r.Header.Get("Referer")
		if referer == "" {
			referer = "/html/timeline"
		}
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	// Return empty content to clear the attachment
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// htmlGifsCloseHandler closes the GIF panel
// GET /html/gifs/close - returns empty content to close the panel
func htmlGifsCloseHandler(w http.ResponseWriter, r *http.Request) {
	if !isHelmRequest(r) {
		// No-JS fallback: redirect back
		referer := r.Header.Get("Referer")
		if referer == "" {
			referer = "/html/timeline"
		}
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	// Return empty content to close the panel
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// htmlComposeHandler handles the compose page (no-JS fallback for media)
// GET /html/compose?media_url=... - compose page with attached media
func htmlComposeHandler(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		// Redirect to login
		http.Redirect(w, r, "/html/login?return_url="+url.QueryEscape(r.URL.String()), http.StatusSeeOther)
		return
	}

	mediaURL := r.URL.Query().Get("media_url")
	mediaThumb := r.URL.Query().Get("media_thumb")
	themeClass, themeLabel := getThemeFromRequest(r)

	userPubkeyHex := hex.EncodeToString(session.UserPubKey)
	userAvatarURL := getUserAvatarURL(userPubkeyHex)

	data := struct {
		ThemeClass             string
		ThemeLabel             string
		Title                  string
		LoggedIn               bool
		CSRFToken              string
		MediaURL               string
		MediaThumb             string
		FeedModes              []FeedMode
		KindFilters            []KindFilter
		NavItems               []NavItem
		SettingsItems          []SettingsItem
		SettingsToggle         SettingsToggle
		CurrentURL             string
		HasUnreadNotifications bool
		ShowPostForm           bool
		ActiveRelays           []string
		UserDisplayName        string
		UserNpubShort          string
	}{
		ThemeClass:             themeClass,
		ThemeLabel:             themeLabel,
		Title:                  "Compose Note",
		LoggedIn:               true,
		CSRFToken:              generateCSRFToken(session.ID),
		MediaURL:               mediaURL,
		MediaThumb:             mediaThumb,
		CurrentURL:             r.URL.String(),
		HasUnreadNotifications: false,
		ShowPostForm:           false,
		ActiveRelays:           ConfigGetDefaultRelays(),
	}

	// Build navigation
	feedModeCtx := FeedModeContext{LoggedIn: true, ActiveFeed: "", CurrentPage: "compose"}
	data.FeedModes = GetFeedModes(feedModeCtx)
	data.KindFilters = GetKindFilters(KindFilterContext{LoggedIn: true, ActiveFeed: "", ActiveKinds: ""})
	navCtx := NavContext{LoggedIn: true, ActivePage: "compose"}
	data.NavItems = GetNavItems(navCtx)
	settingsCtx := SettingsContext{LoggedIn: true, ThemeLabel: themeLabel, UserAvatarURL: userAvatarURL}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if err := cachedComposeTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
		slog.Error("error rendering compose page", "error", err)
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}
