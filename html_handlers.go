package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nostr-server/internal/config"
	"nostr-server/internal/nips"
	"nostr-server/internal/util"
	"nostr-server/templates"
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
	SetLaxCookie(w, r, "notifications_last_seen", fmt.Sprintf("%d", timestamp), 365*24*60*60)
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
	feedMode := q.Get("feed") // "follows", "global", "me", or DVM feed name
	if feedMode == "" {
		if session != nil && session.Connected {
			feedMode = "follows"
		} else {
			feedMode = "global"
		}
	}

	// Check if this is a DVM feed
	dvmConfig := GetDVMConfig(feedMode)
	if dvmConfig != nil {
		handleDVMTimeline(w, r, dvmConfig, feedMode, session, q)
		return
	}

	// Relay selection is feed-aware:
	// - global: always use defaultRelays (aggregators for broad content)
	// - me: use user's write relays (where they publish their own content)
	// - follows: use default relays initially (outbox model queries followed users' write relays later)
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		if feedMode == "global" {
			// Global feed always uses default relays (aggregators)
			relays = config.GetDefaultRelays()
			slog.Debug("using default relays for global feed", "count", len(relays))
		} else if session != nil && session.Connected {
			// Fetch user's relay list if not cached
			if session.UserRelayList == nil && len(session.UserPubKey) > 0 {
				pubkeyHex := hex.EncodeToString(session.UserPubKey)
				slog.Debug("fetching relay list", "pubkey", shortID(pubkeyHex))
				relayList := fetchRelayList(pubkeyHex)
				if relayList != nil {
					session.mu.Lock()
					session.UserRelayList = relayList
					session.mu.Unlock()
				}
			}

			if feedMode == "me" && session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
				// Me feed: use user's write relays + aggregator relays as fallback
				// User's posts might be on aggregators even if not in their declared write relays
				relays = session.UserRelayList.Write
				for _, r := range config.GetDefaultRelays() {
					found := false
					for _, wr := range relays {
						if r == wr {
							found = true
							break
						}
					}
					if !found {
						relays = append(relays, r)
					}
				}
				slog.Debug("using user write relays + aggregators for me feed", "count", len(relays))
			} else if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
				// Follows feed: use read relays as base (outbox model will query followed users' write relays)
				relays = session.UserRelayList.Read
				slog.Debug("using user NIP-65 read relays", "count", len(relays))
			}
		}

		if len(relays) == 0 {
			relays = config.GetDefaultRelays()
		}
	}

	// Feed=follows: fetch user's contact list
	if feedMode == "follows" && session != nil && session.Connected && len(session.UserPubKey) > 0 && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)

		contacts, ok := contactCache.Get(pubkeyHex)
		if !ok {
			slog.Debug("fetching contact list", "pubkey", shortID(pubkeyHex))
			contacts = fetchContactList(relays, pubkeyHex)
			if contacts != nil {
				contactCache.Set(pubkeyHex, contacts)
				// Trigger background profile prefetch for contacts
				go prefetchContactProfiles(contacts)
			}
		} else {
			slog.Debug("contact cache hit", "pubkey", shortID(pubkeyHex), "count", len(contacts))
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
	if feedMode == "me" && session != nil && session.Connected && len(session.UserPubKey) > 0 && len(authors) == 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		authors = []string{pubkeyHex}
		slog.Debug("showing notes for user", "pubkey", shortID(pubkeyHex))
	}

	// Bookmarks (kind 10003): fetch user's bookmark list, then fetch bookmarked events
	var bookmarkedEventIDs []string
	isBookmarksView := len(kinds) == 1 && kinds[0] == 10003
	if isBookmarksView && session != nil && session.Connected && len(session.UserPubKey) > 0 {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		bookmarkEvents := fetchKind10003(relays, pubkeyHex)
		if len(bookmarkEvents) > 0 {
			for _, tag := range bookmarkEvents[0].Tags {
				if len(tag) >= 2 && tag[0] == "e" {
					bookmarkedEventIDs = append(bookmarkedEventIDs, tag[1])
				}
			}
			slog.Debug("found bookmarked events", "count", len(bookmarkedEventIDs), "pubkey", shortID(pubkeyHex))
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
	} else if feedMode == "follows" && len(authors) > 0 && !cacheOnly {
		// Follows feed: use outbox model (NIP-65)
		// Fetch relay lists for all followed users, then query their write relays
		slog.Debug("fetching relay lists for follows feed", "authors", len(authors))
		relayLists := fetchRelayLists(authors)

		filter := Filter{
			Kinds: kinds,
			Limit: fetchLimit,
			Since: since,
			Until: until,
		}

		// Use outbox fetch with default relays as fallback for users without relay lists
		events, eose = fetchEventsFromOutbox(authors, relayLists, filter, config.GetDefaultRelays())
		slog.Debug("outbox fetch for follows complete", "events", len(events))
	} else {
		filter := Filter{
			Authors: authors,
			Kinds:   kinds,
			Limit:   fetchLimit,
			Since:   since,
			Until:   until,
		}

		// Try aggregator cache first for global timeline (no authors, kind 1)
		// This serves from the background subscription cache when available
		if len(authors) == 0 && len(kinds) == 1 && kinds[0] == 1 {
			if aggEvents := GetAggregatedEvents(filter); len(aggEvents) >= limit {
				events = aggEvents
				eose = true
				slog.Debug("timeline served from aggregator", "count", len(events))
			} else {
				events, eose = fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
			}
		} else {
			events, eose = fetchEventsFromRelaysCachedWithOptions(relays, filter, cacheOnly)
		}
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

	addMentionedPubkeysToSet(contents, pubkeySet)

	// Extract referenced event IDs from reference-only reposts (kinds 6, 16 with empty content)
	// Also collect their authors (from p tags) for profile fetching and outbox
	repostRefs := extractRepostEventIDs(events)
	var repostEventIDs []string
	repostAuthorPubkeys := make(map[string]bool)
	for _, evt := range events {
		kindDef := GetKindDefinition(evt.Kind)
		if kindDef.IsRepost && strings.TrimSpace(evt.Content) == "" {
			// Reference-only repost - get author from p tag
			for _, tag := range evt.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					pubkeySet[tag[1]] = true           // Add reposted author to profile fetch
					repostAuthorPubkeys[tag[1]] = true // Track for outbox relay fetch
					break
				}
			}
		} else if kindDef.IsRepost && strings.TrimSpace(evt.Content) != "" {
			// Embedded JSON repost - parse content to get author pubkey
			var embedded struct {
				PubKey string `json:"pubkey"`
			}
			if json.Unmarshal([]byte(evt.Content), &embedded) == nil && embedded.PubKey != "" {
				pubkeySet[embedded.PubKey] = true
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
			profiles = fetchProfilesWithOptions(relays, util.MapKeys(pubkeySet), cacheOnly)
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

	// Fetch referenced events for reference-only reposts (using outbox model)
	if len(repostEventIDs) > 0 && !cacheOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Use outbox relays from repost authors for better discovery
			repostRelays := relays
			if len(repostAuthorPubkeys) > 0 {
				outboxRelays := buildOutboxRelays(util.MapKeys(repostAuthorPubkeys), 2)
				if len(outboxRelays) > 0 {
					repostRelays = append(outboxRelays, relays...)
					slog.Debug("using outbox for repost resolution", "authors", len(repostAuthorPubkeys), "outbox_relays", len(outboxRelays))
				}
			}

			filter := Filter{
				IDs:   repostEventIDs,
				Limit: len(repostEventIDs),
			}
			fetchedEvents, _ := fetchEventsFromRelaysCached(repostRelays, filter)
			for i := range fetchedEvents {
				repostEvents[fetchedEvents[i].ID] = &fetchedEvents[i]
			}
		}()
	}

	wg.Wait()

	// Log profile availability
	var missingProfiles []string
	for _, evt := range events {
		if profiles[evt.PubKey] == nil {
			missingProfiles = append(missingProfiles, shortID(evt.PubKey))
		}
	}
	if len(missingProfiles) > 0 {
		slog.Debug("timeline items with missing profiles", "count", len(missingProfiles), "pubkeys", missingProfiles)
	}

	// Convert to EventItems
	items := EventsToItems(events, profiles, reactions, replyCounts)

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
		// Build pagination URL params
		params := map[string]string{
			"feed":  feedMode,
			"kinds": util.IntsToParam(kinds),
			"limit": strconv.Itoa(limit),
			"until": strconv.FormatInt(lastCreatedAt-1, 10),
		}
		// Only include authors if not using feed mode (follows/me fetch authors server-side)
		// This prevents massive URLs with 100+ authors in the query string
		if len(authors) > 0 && feedMode != "follows" && feedMode != "me" {
			params["authors"] = strings.Join(authors, ",")
		}
		nextURL := util.BuildURL(r.URL.Path, params)
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
	hTarget := r.Header.Get("H-Target")
	isBodyTarget := hTarget == "body" // body morph needs full page
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh && !isBodyTarget
	isAppend := q.Get("append") == "1"

	var newestTimestamp int64
	if len(items) > 0 {
		newestTimestamp = items[0].CreatedAt
	}

	html, err := renderHTML(resp, relays, authors, kinds, limit, session, errorMsg, successMsg, feedMode, currentURL, themeClass, themeLabel, csrfToken, hasUnreadNotifs, isFragment, isAppend, newestTimestamp, repostEvents, nil)
	if err != nil {
		slog.Error("error rendering HTML", "error", err)
		util.RespondInternalError(w, "Error rendering page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=5")
	w.Write([]byte(html))

	// Prefetch LNURL pay info for visible authors (non-blocking)
	go PrefetchLNURLForProfiles(profiles)
}

// handleDVMTimeline handles timeline requests for DVM feeds
func handleDVMTimeline(w http.ResponseWriter, r *http.Request, dvmConfig *DVMConfig, feedMode string, session *BunkerSession, q url.Values) {
	limit := parseLimit(q.Get("limit"), 10)
	page := 0
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	cacheOnly := q.Get("cache_only") == "1"

	// Check login requirement for personalized DVMs
	if dvmConfig.Personalized && (session == nil || !session.Connected) {
		util.RespondUnauthorized(w, config.I18n("error.login_required"))
		return
	}

	// Get user pubkey for personalized requests
	var userPubkey string
	if session != nil && session.Connected && session.UserPubKey != nil {
		userPubkey = hex.EncodeToString(session.UserPubKey)
	}

	// Create timeout context (30 second timeout for DVM requests)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get DVM metadata (name, image, description) for header display
	dvmMetadata := GetDVMMetadata(ctx, dvmConfig)

	// Get DVM results with caching
	dvmClient := GetDVMClient()
	var dvmResult *DVMResult
	var err error

	if cacheOnly {
		// Only check cache, don't make DVM request
		cacheKey := buildDVMCacheKey(dvmConfig, userPubkey)
		cached, found, _ := dvmCacheStore.Get(ctx, cacheKey)
		if found {
			dvmResult = cachedResultToDVMResult(cached)
		} else {
			// Return empty result for cache miss in cache_only mode
			dvmResult = &DVMResult{}
		}
	} else {
		dvmResult, err = dvmClient.GetContentWithCache(ctx, dvmConfig, userPubkey)
		if err != nil {
			slog.Error("DVM request failed", "error", err, "feed", feedMode)
			// Render error page with empty timeline
			renderDVMError(w, r, feedMode, session, err)
			return
		}
	}

	// Paginate results
	pageSize := dvmConfig.GetPageSize()
	if limit > 0 && limit < pageSize {
		pageSize = limit
	}
	startIdx := page * pageSize
	endIdx := startIdx + pageSize
	if startIdx >= len(dvmResult.EventRefs) {
		startIdx = len(dvmResult.EventRefs)
	}
	if endIdx > len(dvmResult.EventRefs) {
		endIdx = len(dvmResult.EventRefs)
	}
	pagedRefs := dvmResult.EventRefs[startIdx:endIdx]

	// Fetch events from the references
	events := fetchEventsFromDVMRefs(ctx, pagedRefs)

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

	// Collect pubkeys and event IDs for enrichment
	pubkeys, eventIDs := CollectEventData(events)
	relays := config.GetDefaultRelays()

	// Fetch enrichment data in parallel
	enrichment := FetchEventEnrichment(relays, pubkeys, eventIDs, cacheOnly)

	// Build EventItem array for TimelineResponse
	items := EventsToItems(events, enrichment.Profiles, enrichment.Reactions, enrichment.ReplyCounts)

	// Build pagination
	hasMore := endIdx < len(dvmResult.EventRefs)
	var nextURL *string
	if hasMore {
		next := util.BuildURL(r.URL.Path, map[string]string{
			"feed":   feedMode,
			"limit":  strconv.Itoa(limit),
			"page":   strconv.Itoa(page + 1),
			"append": "1",
		})
		nextURL = &next
	}

	// Build TimelineResponse
	resp := TimelineResponse{
		Items: items,
		Page: PageInfo{
			Next: nextURL,
		},
		Meta: MetaInfo{
			QueriedRelays: len(relays),
			EOSE:          true,
			GeneratedAt:   time.Now(),
		},
	}

	// Render using existing renderHTML function
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
	isFragment := isHelmRequest(r) && !cacheOnly && !refresh
	isAppend := q.Get("append") == "1"

	// Note: DVM feeds don't have repost events to inline
	repostEvents := make(map[string]*Event)

	html, renderErr := renderHTML(resp, relays, nil, nil, limit, session, errorMsg, successMsg, feedMode, currentURL, themeClass, themeLabel, csrfToken, hasUnreadNotifs, isFragment, isAppend, 0, repostEvents, dvmMetadata)
	if renderErr != nil {
		slog.Error("error rendering DVM timeline", "error", renderErr)
		util.RespondInternalError(w, "Error rendering page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=5")
	w.Write([]byte(html))
}

// renderDVMError renders an error page for DVM failures
func renderDVMError(w http.ResponseWriter, r *http.Request, feedMode string, session *BunkerSession, dvmErr error) {
	relays := config.GetDefaultRelays()

	// Empty response with error
	resp := TimelineResponse{
		Items: []EventItem{},
		Page:  PageInfo{},
		Meta: MetaInfo{
			QueriedRelays: 0,
			EOSE:          true,
			GeneratedAt:   time.Now(),
		},
	}

	themeClass, themeLabel := getThemeFromRequest(r)
	currentURL := buildCurrentURL(r, "cache_only", "refresh")

	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	hasUnreadNotifs := checkUnreadNotifications(r, session, relays)
	repostEvents := make(map[string]*Event)

	html, renderErr := renderHTML(resp, relays, nil, nil, 10, session, config.I18n("msg.dvm_unavailable"), "", feedMode, currentURL, themeClass, themeLabel, csrfToken, hasUnreadNotifs, false, false, 0, repostEvents, nil)
	if renderErr != nil {
		util.RespondInternalError(w, "Error rendering page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte(html))
}

// fetchEventsFromDVMRefs fetches events from DVM references
// Uses relay hints from DVM response, falls back to default relays
func fetchEventsFromDVMRefs(ctx context.Context, refs []DVMEventRef) []Event {
	if len(refs) == 0 {
		slog.Debug("DVM refs: no refs to fetch")
		return nil
	}

	// Group references by relay hint for efficient fetching
	relayToIDs := make(map[string][]string)
	noHintIDs := make([]string, 0)

	// Group "a" refs by relay hint
	type aRef struct {
		kind   int
		pubkey string
		dTag   string
		relay  string
	}
	var aRefs []aRef
	var noHintARefs []aRef

	for _, ref := range refs {
		if ref.Type == "e" {
			if ref.RelayURL != "" {
				relayToIDs[ref.RelayURL] = append(relayToIDs[ref.RelayURL], ref.ID)
			} else {
				noHintIDs = append(noHintIDs, ref.ID)
			}
		} else if ref.Type == "a" {
			// Parse "a" tag: kind:pubkey:d-tag
			parts := strings.SplitN(ref.ID, ":", 3)
			if len(parts) >= 2 {
				kind, err := strconv.Atoi(parts[0])
				if err != nil {
					continue
				}
				pubkey := parts[1]
				dTag := ""
				if len(parts) >= 3 {
					dTag = parts[2]
				}
				parsed := aRef{kind: kind, pubkey: pubkey, dTag: dTag, relay: ref.RelayURL}
				if ref.RelayURL != "" {
					aRefs = append(aRefs, parsed)
				} else {
					noHintARefs = append(noHintARefs, parsed)
				}
			}
		}
	}

	slog.Debug("DVM refs grouped", "with_hints", len(relayToIDs), "no_hints", len(noHintIDs), "a_refs_hinted", len(aRefs), "a_refs_no_hint", len(noHintARefs))

	var events []Event
	var mu sync.Mutex

	// Track found event IDs for fallback logic
	foundIDs := make(map[string]bool)
	var foundMu sync.Mutex

	// Fetch from hinted relays
	var wg sync.WaitGroup
	for relayURL, ids := range relayToIDs {
		wg.Add(1)
		go func(relay string, eventIDs []string) {
			defer wg.Done()
			filter := Filter{
				IDs:   eventIDs,
				Limit: len(eventIDs),
			}
			fetched, eose := fetchEventsFromRelaysCached([]string{relay}, filter)
			slog.Debug("DVM fetched from hinted relay", "relay", relay, "requested", len(eventIDs), "fetched", len(fetched), "eose", eose)
			mu.Lock()
			events = append(events, fetched...)
			mu.Unlock()
			// Track found IDs
			foundMu.Lock()
			for _, evt := range fetched {
				foundIDs[evt.ID] = true
			}
			foundMu.Unlock()
		}(relayURL, ids)
	}

	// Wait for hinted relay fetches to complete before fallback
	wg.Wait()

	// Collect IDs that weren't found from hinted relays for fallback
	var missingFromHints []string
	for _, ids := range relayToIDs {
		for _, id := range ids {
			if !foundIDs[id] {
				missingFromHints = append(missingFromHints, id)
			}
		}
	}

	// Combine with noHintIDs for default relay fetch
	allMissingIDs := append(noHintIDs, missingFromHints...)
	if len(missingFromHints) > 0 {
		slog.Debug("DVM fallback: hinted relays missing events", "missing", len(missingFromHints))
	}

	// Fetch events without hints (and fallback) from default relays
	if len(allMissingIDs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defaultRelays := config.GetDefaultRelays()
			filter := Filter{
				IDs:   allMissingIDs,
				Limit: len(allMissingIDs),
			}
			fetched, eose := fetchEventsFromRelaysCached(defaultRelays, filter)
			slog.Debug("DVM fetched from default relays", "requested", len(allMissingIDs), "fetched", len(fetched), "eose", eose)
			mu.Lock()
			events = append(events, fetched...)
			mu.Unlock()
		}()
	}

	// Track found addressable events for fallback
	type aRefKey struct {
		kind   int
		pubkey string
		dTag   string
	}
	foundARefs := make(map[aRefKey]bool)
	var foundAMu sync.Mutex

	// Fetch addressable events ("a" refs) with relay hints
	for _, ref := range aRefs {
		wg.Add(1)
		go func(r aRef) {
			defer wg.Done()
			filter := Filter{
				Kinds:   []int{r.kind},
				Authors: []string{r.pubkey},
				DTags:   []string{r.dTag},
				Limit:   1,
			}
			fetched, eose := fetchEventsFromRelaysCached([]string{r.relay}, filter)
			slog.Debug("DVM fetched addressable from hinted relay", "relay", r.relay, "kind", r.kind, "d", r.dTag, "fetched", len(fetched), "eose", eose)
			mu.Lock()
			events = append(events, fetched...)
			mu.Unlock()
			if len(fetched) > 0 {
				foundAMu.Lock()
				foundARefs[aRefKey{r.kind, r.pubkey, r.dTag}] = true
				foundAMu.Unlock()
			}
		}(ref)
	}

	// Fetch addressable events without hints from default relays
	defaultRelays := config.GetDefaultRelays()
	for _, ref := range noHintARefs {
		wg.Add(1)
		go func(r aRef) {
			defer wg.Done()
			filter := Filter{
				Kinds:   []int{r.kind},
				Authors: []string{r.pubkey},
				DTags:   []string{r.dTag},
				Limit:   1,
			}
			fetched, eose := fetchEventsFromRelaysCached(defaultRelays, filter)
			slog.Debug("DVM fetched addressable from default relays", "kind", r.kind, "d", r.dTag, "fetched", len(fetched), "eose", eose)
			mu.Lock()
			events = append(events, fetched...)
			mu.Unlock()
		}(ref)
	}

	wg.Wait()

	// Fallback: try default relays for addressable events not found on hinted relays
	var missingARefs []aRef
	for _, ref := range aRefs {
		if !foundARefs[aRefKey{ref.kind, ref.pubkey, ref.dTag}] {
			missingARefs = append(missingARefs, ref)
		}
	}
	if len(missingARefs) > 0 {
		slog.Debug("DVM fallback: hinted relays missing addressable events", "missing", len(missingARefs))
		for _, ref := range missingARefs {
			wg.Add(1)
			go func(r aRef) {
				defer wg.Done()
				filter := Filter{
					Kinds:   []int{r.kind},
					Authors: []string{r.pubkey},
					DTags:   []string{r.dTag},
					Limit:   1,
				}
				fetched, eose := fetchEventsFromRelaysCached(defaultRelays, filter)
				slog.Debug("DVM fallback addressable from default relays", "kind", r.kind, "d", r.dTag, "fetched", len(fetched), "eose", eose)
				mu.Lock()
				events = append(events, fetched...)
				mu.Unlock()
			}(ref)
		}
		wg.Wait()
	}

	// Deduplicate and filter events
	// Filter out DVM-specific kinds (5000-7999 range covers requests, responses, feedback)
	seen := make(map[string]bool)
	unique := make([]Event, 0, len(events))
	filtered := 0
	for _, evt := range events {
		if !seen[evt.ID] {
			seen[evt.ID] = true
			// Skip DVM-related events (requests 5xxx, responses 6xxx, feedback 7xxx)
			if evt.Kind >= 5000 && evt.Kind < 8000 {
				filtered++
				continue
			}
			unique = append(unique, evt)
		}
	}

	slog.Debug("DVM events fetched", "total_refs", len(refs), "fetched", len(unique), "filtered_dvm_kinds", filtered)

	return unique
}

func htmlThreadHandler(w http.ResponseWriter, r *http.Request) {
	// Delegate check-new requests to the check-new handler
	if strings.HasSuffix(r.URL.Path, "/check-new") {
		htmlThreadCheckNewHandler(w, r)
		return
	}

	identifier := strings.TrimPrefix(r.URL.Path, "/thread/") // hex, note1, nevent1, naddr1
	if identifier == "" {
		util.RespondBadRequest(w, "Invalid event identifier")
		return
	}

	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = config.GetDefaultRelays()
	}
	cacheOnly := q.Get("cache_only") == "1"

	var eventID string
	var naddrRef *nips.NAddr

	switch {
	case strings.HasPrefix(identifier, "naddr1"):
		na, err := nips.DecodeNAddr(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid naddr identifier")
			return
		}
		naddrRef = na
		if len(na.RelayHints) > 0 {
			relays = append(na.RelayHints, relays...)
		}
	case strings.HasPrefix(identifier, "nevent1"):
		decoded, err := nips.DecodeNEvent(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid nevent identifier")
			return
		}
		eventID = decoded.EventID
		if len(decoded.RelayHints) > 0 {
			relays = append(decoded.RelayHints, relays...)
		}
	case strings.HasPrefix(identifier, "note1"):
		decoded, err := nips.DecodeNote(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid note identifier")
			return
		}
		eventID = decoded
	case isValidEventID(identifier):
		eventID = identifier
	default:
		util.RespondBadRequest(w, "Invalid event identifier")
		return
	}

	slog.Debug("fetching thread", "identifier", identifier)

	// Extract author for outbox model (from nevent or naddr)
	var authorPubkey string
	if naddrRef != nil {
		authorPubkey = naddrRef.Author
	} else if strings.HasPrefix(identifier, "nevent1") {
		if decoded, err := nips.DecodeNEvent(identifier); err == nil && decoded.Author != "" {
			authorPubkey = decoded.Author
		}
	}

	var rootEvent *Event
	var replies []Event
	var cacheHit bool

	// Try thread cache first (only for direct event ID lookups, not naddr)
	// Check cache BEFORE relay list fetch to avoid blocking on cache hits
	if naddrRef == nil && eventID != "" {
		ctx := context.Background()
		cached, found, err := threadCacheStore.Get(ctx, eventID)
		if err == nil && found {
			rootEvent = &cached.RootEvent
			replies = cached.Replies
			cacheHit = true
			slog.Debug("thread cache hit", "event_id", eventID, "replies", len(replies))

			// Spawn background refresh to update cache (non-blocking)
			// Client will poll for new replies via /thread/{id}/check-new
			if !cacheOnly {
				go refreshThreadCacheInBackground(eventID, relays, cached.CachedAt)
			}
		}
	}

	// If cache miss, fetch author's relay list for outbox model
	// This is done after cache check to avoid blocking on cache hits
	if !cacheHit && authorPubkey != "" && !cacheOnly {
		if outboxRelays := buildOutboxRelaysForPubkey(authorPubkey, 5); len(outboxRelays) > 0 {
			relays = append(outboxRelays, relays...)
			slog.Debug("using outbox relays for thread root", "author", shortID(authorPubkey), "relays", len(outboxRelays))
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
			util.RespondNotFound(w, "Event not found")
			return
		}

		// For naddr or addressable events, fetch replies and comments in parallel
		if rootEvent != nil {
			var repliesWg sync.WaitGroup
			var addressableComments []Event
			var repliesMu sync.Mutex

			// Fetch replies by event ID (for naddr, we now have the root ID)
			if naddrRef != nil {
				repliesWg.Add(1)
				go func() {
					defer repliesWg.Done()
					fetched := fetchReplies(relays, []string{rootEvent.ID})
					repliesMu.Lock()
					replies = fetched
					repliesMu.Unlock()
				}()
			}

			// For addressable events (kind 30xxx), also fetch NIP-22 comments using #A tag
			if rootEvent.Kind >= 30000 && rootEvent.Kind < 40000 {
				dTag := util.GetTagValue(rootEvent.Tags, "d")
				if dTag != "" {
					aTagValue := fmt.Sprintf("%d:%s:%s", rootEvent.Kind, rootEvent.PubKey, dTag)
					repliesWg.Add(1)
					go func(aTag string) {
						defer repliesWg.Done()
						addressableComments = fetchAddressableComments(relays, aTag)
						if len(addressableComments) > 0 {
							slog.Debug("fetched addressable comments", "count", len(addressableComments), "a_tag", aTag)
						}
					}(aTagValue)
				}
			}

			repliesWg.Wait()

			// Merge addressable comments with replies, avoiding duplicates
			if len(addressableComments) > 0 {
				existingIDs := make(map[string]bool)
				for _, r := range replies {
					existingIDs[r.ID] = true
				}
				for _, c := range addressableComments {
					if !existingIDs[c.ID] {
						replies = append(replies, c)
					}
				}
			}
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
		util.RespondNotFound(w, "Event not found")
		return
	}

	// Check if root is a reference-only repost (kinds 6, 16 with empty content)
	// If so, fetch the referenced event and its author's profile
	repostEvents := make(map[string]*Event)
	var repostAuthorPubkey string
	rootKindDef := GetKindDefinition(rootEvent.Kind)
	if rootKindDef.IsRepost && strings.TrimSpace(rootEvent.Content) == "" {
		// Get the original author from p tag first (for outbox)
		for _, tag := range rootEvent.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				repostAuthorPubkey = tag[1]
				break
			}
		}

		for _, tag := range rootEvent.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				// Use outbox model: fetch original author's relay list
				repostRelays := relays
				if repostAuthorPubkey != "" && !cacheOnly {
					if outboxRelays := buildOutboxRelaysForPubkey(repostAuthorPubkey, 3); len(outboxRelays) > 0 {
						repostRelays = append(outboxRelays, relays...)
						slog.Debug("using outbox for repost resolution", "author", shortID(repostAuthorPubkey))
					}
				}

				// Fetch the referenced event
				filter := Filter{
					IDs:   []string{tag[1]},
					Limit: 1,
				}
				fetchedEvents, _ := fetchEventsFromRelaysCached(repostRelays, filter)
				if len(fetchedEvents) > 0 {
					repostEvents[fetchedEvents[0].ID] = &fetchedEvents[0]
				}
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

	addMentionedPubkeysToSet(contents, pubkeySet)

	allEventIDs := make([]string, 0, 1+len(replies))
	allEventIDs = append(allEventIDs, rootEvent.ID)
	for _, reply := range replies {
		allEventIDs = append(allEventIDs, reply.ID)
	}

	pubkeys := util.MapKeys(pubkeySet)

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

	htmlContent, err := renderThreadHTML(resp, relays, session, currentURL, identifier, themeClass, themeLabel, successMsg, csrfToken, hasUnreadNotifs, isFragment, repostEvents)
	if err != nil {
		slog.Error("error rendering thread HTML", "error", err)
		util.RespondInternalError(w, "Error rendering page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=10")
	w.Write([]byte(htmlContent))

	// Prefetch LNURL pay info for visible authors (non-blocking)
	go PrefetchLNURLForProfiles(profiles)
}

// refreshThreadCacheInBackground fetches new replies and updates the thread cache.
// Called as a goroutine to avoid blocking the response.
func refreshThreadCacheInBackground(eventID string, relays []string, since int64) {
	newReplies := fetchRepliesSince(relays, []string{eventID}, since)
	if len(newReplies) == 0 {
		return
	}

	slog.Debug("background thread refresh found new replies", "event_id", eventID, "count", len(newReplies))

	// Update the thread cache with new replies
	ctx := context.Background()
	cached, found, err := threadCacheStore.Get(ctx, eventID)
	if err != nil || !found {
		return
	}

	// Merge new replies, avoiding duplicates
	existingIDs := make(map[string]bool)
	for _, r := range cached.Replies {
		existingIDs[r.ID] = true
	}
	for _, r := range newReplies {
		if !existingIDs[r.ID] {
			cached.Replies = append(cached.Replies, r)
		}
	}
	cached.CachedAt = time.Now().Unix()

	if err := threadCacheStore.Set(ctx, eventID, cached, cacheConfig.ThreadTTL); err != nil {
		slog.Warn("failed to update thread cache in background", "error", err)
	}
}

// htmlThreadCheckNewHandler checks for new replies since a given timestamp.
// Returns a "new replies" indicator if there are new replies, empty otherwise.
// GET /thread/{id}/check-new?since=timestamp&url=refresh_url
func htmlThreadCheckNewHandler(w http.ResponseWriter, r *http.Request) {
	// Extract event ID from path: /thread/{id}/check-new
	path := strings.TrimPrefix(r.URL.Path, "/thread/")
	path = strings.TrimSuffix(path, "/check-new")
	identifier := path

	if identifier == "" {
		util.RespondBadRequest(w, "Invalid event identifier")
		return
	}

	q := r.URL.Query()
	sincePtr := parseInt64(q.Get("since"))
	var since int64
	if sincePtr != nil {
		since = *sincePtr
	}
	refreshURL := q.Get("url")
	if refreshURL == "" {
		refreshURL = "/thread/" + identifier
	}

	// Decode identifier to event ID
	var eventID string
	switch {
	case strings.HasPrefix(identifier, "nevent1"):
		decoded, err := nips.DecodeNEvent(identifier)
		if err == nil {
			eventID = decoded.EventID
		}
	case strings.HasPrefix(identifier, "note1"):
		decoded, err := nips.DecodeNote(identifier)
		if err == nil {
			eventID = decoded
		}
	case isValidEventID(identifier):
		eventID = identifier
	}

	if eventID == "" {
		// Can't check for naddr, return empty
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(renderThreadNewRepliesIndicator(identifier, since, refreshURL, 0)))
		return
	}

	// Check cache for new reply count
	ctx := context.Background()
	cached, found, _ := threadCacheStore.Get(ctx, eventID)

	var newCount int
	if found && since > 0 {
		for _, reply := range cached.Replies {
			if reply.CreatedAt > since {
				newCount++
			}
		}
	}

	// Use cache timestamp for next poll, or original since if no cache
	nextSince := since
	if found && cached.CachedAt > 0 {
		nextSince = cached.CachedAt
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(renderThreadNewRepliesIndicator(identifier, nextSince, refreshURL, newCount)))
}

// renderThreadNewRepliesIndicator returns the polling indicator HTML.
// If count > 0, shows a "N new replies" button.
func renderThreadNewRepliesIndicator(identifier string, since int64, refreshURL string, count int) string {
	sinceStr := fmt.Sprintf("%d", since)
	pollURL := fmt.Sprintf("/thread/%s/check-new?since=%s&url=%s", identifier, sinceStr, url.QueryEscape(refreshURL))

	if count > 0 {
		label := fmt.Sprintf("%d new replies", count)
		if count == 1 {
			label = "1 new reply"
		}
		return fmt.Sprintf(`<div id="thread-new-replies" h-poll="%s 10s" h-target="#thread-new-replies" h-swap="outer" h-poll-pause-hidden>
	<a href="%s" h-get h-target="#page-content" h-swap="inner" h-push-url class="new-replies-btn">%s</a>
</div>`, pollURL, refreshURL, label)
	}

	return fmt.Sprintf(`<div id="thread-new-replies" h-poll="%s 10s" h-target="#thread-new-replies" h-swap="outer" h-poll-pause-hidden></div>`, pollURL)
}

func htmlProfileHandler(w http.ResponseWriter, r *http.Request) {
	pubkeyParam := strings.TrimPrefix(r.URL.Path, "/profile/")
	if pubkeyParam == "" {
		util.RespondBadRequest(w, "Pubkey required")
		return
	}

	// Keep original format for URLs, convert to hex for internal use
	pubkey := pubkeyParam
	if strings.HasPrefix(pubkeyParam, "npub1") { // Decode npub to hex
		hexPubkey, err := decodeBech32Pubkey(pubkeyParam)
		if err != nil {
			util.RespondBadRequest(w, "Invalid npub format")
			return
		}
		pubkey = hexPubkey
	}

	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = config.GetDefaultRelays()
	}
	cacheOnly := q.Get("cache_only") == "1"

	limit := parseLimit(q.Get("limit"), 10)
	until := parseInt64(q.Get("until"))

	slog.Debug("fetching profile", "pubkey", shortID(pubkey))

	// First, fetch the user's relay list for outbox model (NIP-65)
	var userRelayList *RelayList
	if !cacheOnly {
		userRelayList = fetchRelayList(pubkey)
	}

	// Determine which relays to use for fetching events
	// Outbox model: use user's write relays if available
	eventRelays := relays
	if userRelayList != nil && len(userRelayList.Write) > 0 {
		eventRelays = util.LimitSlice(relayHealthStore.SortRelaysByScore(userRelayList.Write), 5)
		slog.Debug("using outbox relays for profile", "pubkey", shortID(pubkey), "relays", len(eventRelays))
	}

	var profile *ProfileInfo
	var events []Event
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		profiles := fetchProfilesWithOptions(relays, []string{pubkey}, cacheOnly)
		profile = profiles[pubkey]
	}()

	wg.Add(1) // Fetch user's top-level notes from their write relays (outbox)
	go func() {
		defer wg.Done()
		filter := Filter{
			Authors: []string{pubkey},
			Kinds:   []int{1},
			Limit:   limit * 2, // Fetch more since we'll filter out replies
			Until:   until,
		}
		events, _ = fetchEventsFromRelaysCachedWithOptions(eventRelays, filter, cacheOnly)
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
	mentionedPubkeys := CollectEventPubkeys(topLevelNotes)
	if len(mentionedPubkeys) > 0 {
		fetchProfilesWithOptions(relays, mentionedPubkeys, cacheOnly)
	}

	// Convert to EventItems using the author's profile for all notes
	items := EventsToItemsWithProfile(topLevelNotes, profile)

	// Pagination (subtract 1 - until is inclusive)
	// Use pubkeyParam (original format from URL) to maintain consistent URL format
	var pageUntil *int64
	var nextURL *string
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		pageUntil = &lastCreatedAt
		next := util.BuildURL("/profile/"+pubkeyParam, map[string]string{
			"limit": strconv.Itoa(limit),
			"until": strconv.FormatInt(lastCreatedAt-1, 10),
		})
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
		isFollowing = session.IsFollowing(pubkey)
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
		util.RespondInternalError(w, "Error rendering page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=30")
	w.Write([]byte(htmlContent))

	// Prefetch LNURL pay info for profile author (non-blocking)
	if profile != nil {
		go PrefetchLNURLForProfiles(map[string]*ProfileInfo{pubkey: profile})
	}
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

	SetLaxCookie(w, r, "theme", newTheme, 365*24*60*60)

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
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
	relays := config.GetDefaultRelays()
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
		slog.Debug("notification relays combined", "nip65", len(session.UserRelayList.Read), "defaults", len(config.GetDefaultRelays()), "total", len(relays))
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
	pubkeys := util.MapKeys(pubkeySet)

	// Fetch profiles using outbox model for better discovery
	// Notification authors might not be indexed on aggregator relays
	profileRelays := relays
	if len(pubkeys) > 0 && !cacheOnly {
		if outboxRelays := buildOutboxRelays(pubkeys, 2); len(outboxRelays) > 0 {
			profileRelays = append(outboxRelays, relays...)
			slog.Debug("using outbox for notification profiles", "authors", len(pubkeys), "outbox_relays", len(outboxRelays))
		}
	}
	profiles := fetchProfilesWithOptions(profileRelays, pubkeys, cacheOnly)

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
			Next: util.BuildURL("/notifications", map[string]string{
				"until": strconv.FormatInt(nextUntil, 10),
			}),
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
		util.RespondInternalError(w, "Error rendering page")
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

	addMentionedPubkeysToSet(contents, pubkeySet)

	if len(pubkeySet) > 0 {
		fetchProfiles(relays, util.MapKeys(pubkeySet))
	}

	slog.Debug("prefetch warmed cache", "events", len(events), "profiles", len(pubkeySet))
}

func htmlMutesHandler(w http.ResponseWriter, r *http.Request) {
	// Get session - must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get muted pubkeys from session cache
	mutedPubkeys := session.GetMutedPubkeys()
	slog.Debug("mutes page: loaded from session", "count", len(mutedPubkeys))

	// Get relays for profile fetching
	relays := config.GetDefaultRelays()
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
		util.RespondInternalError(w, "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func htmlWalletHandler(w http.ResponseWriter, r *http.Request) {
	// Get session - must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
		Title:           config.I18n("nav.wallet"),
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		UserDisplayName: userDisplayName,
		UserPubKey:      userPubkey,
		CurrentURL:      "/wallet",
		CSRFToken:       csrfToken,
		ReturnURL:       returnURL,
		HasWallet:       hasWallet,
		WalletRelay:     session.GetWalletRelay(),
		WalletDomain:    extractDomain(session.GetWalletRelay()),
		Success:         flash.Success,
		Error:           flash.Error,
		LoggedIn:        true,
	}

	// Prefetch wallet info if wallet is connected (warms the cache for /wallet/info)
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
	userNpub, _ := encodeBech32Pubkey(userPubkey)
	settingsCtx := SettingsContext{
		LoggedIn:      true,
		ThemeLabel:    themeLabel,
		UserAvatarURL: getUserAvatarURL(userPubkey),
		UserNpub:      userNpub,
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
		util.RespondInternalError(w, "Internal server error")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func getSearchRelays() []string {
	return config.GetSearchRelays()
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

		relays := config.GetDefaultRelays()
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
		html, err := renderSearchHTML(nil, nil, query, themeClass, themeLabel, loggedIn, userPubKey, userDisplayName, csrfToken, hasUnreadNotifs, nil, isFragment, false, isLiveSearch, nil, nil, nil)
		if err != nil {
			util.RespondInternalError(w, "Failed to render search page")
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
			Next: util.BuildURL("/search", map[string]string{
				"q":     query,
				"until": strconv.FormatInt(lastEvent.CreatedAt-1, 10),
			}),
		}
	}

	pubkeySet := make(map[string]bool)
	for _, evt := range events {
		pubkeySet[evt.PubKey] = true
	}
	pubkeys := util.MapKeys(pubkeySet)
	profiles := fetchProfilesWithOptions(config.GetProfileRelays(), pubkeys, cacheOnly)

	var qTagValues []string
	for _, evt := range events {
		kindDef := GetKindDefinition(evt.Kind)
		if kindDef.SupportsQuotePosts {
			if qTag := util.GetTagValue(evt.Tags, "q"); qTag != "" {
				qTagValues = append(qTagValues, qTag)
			}
		}
	}

	quotedEvents, quotedEventProfiles := fetchQuotedEvents(qTagValues)
	for pk, profile := range quotedEventProfiles {
		if _, exists := profiles[pk]; !exists {
			profiles[pk] = profile
		}
	}

	// Fetch link previews for all event content
	var allURLs []string
	for _, evt := range events {
		allURLs = append(allURLs, ExtractPreviewableURLs(evt.Content)...)
	}
	linkPreviews := FetchLinkPreviews(allURLs)

	html, err := renderSearchHTML(events, profiles, query, themeClass, themeLabel, loggedIn, userPubKey, userDisplayName, csrfToken, hasUnreadNotifs, pagination, isFragment, isAppend, isLiveSearch, getSearchRelays(), quotedEvents, linkPreviews)
	if err != nil {
		slog.Error("search render failed", "error", err, "query", query, "results", len(events))
		util.RespondInternalError(w, "Failed to render search results")
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
			word = config.I18n("new_posts.note")
		} else {
			word = config.I18n("new_posts.notes")
		}
	case "photos":
		if count == 1 {
			word = config.I18n("new_posts.photo")
		} else {
			word = config.I18n("new_posts.photos")
		}
	case "longform":
		if count == 1 {
			word = config.I18n("new_posts.article")
		} else {
			word = config.I18n("new_posts.articles")
		}
	case "highlights":
		if count == 1 {
			word = config.I18n("new_posts.highlight")
		} else {
			word = config.I18n("new_posts.highlights")
		}
	case "live":
		if count == 1 {
			word = config.I18n("new_posts.stream")
		} else {
			word = config.I18n("new_posts.streams")
		}
	case "classifieds":
		if count == 1 {
			word = config.I18n("new_posts.listing")
		} else {
			word = config.I18n("new_posts.listings")
		}
	case "shorts":
		if count == 1 {
			word = config.I18n("new_posts.short")
		} else {
			word = config.I18n("new_posts.shorts")
		}
	default: // "all" or unknown
		if count == 1 {
			word = config.I18n("new_posts.post")
		} else {
			word = config.I18n("new_posts.posts")
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
		refreshURL = util.BuildURL("/timeline", map[string]string{"feed": "follows"})
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected || len(session.UserPubKey) == 0 {
		slog.Debug("check new posts: no session or not connected")
		data := NewNotesIndicatorData{Since: since, Kinds: kindsStr, Filter: filter, RefreshURL: refreshURL}
		w.Write([]byte(renderNewNotesIndicator(data)))
		return
	}

	// Use aggregator relays (same as outbox model fallback) for consistency
	// Don't use user's read relays - they might have events that outbox won't fetch
	relays := config.GetDefaultRelays()

	pubkeyHex := hex.EncodeToString(session.UserPubKey)
	contacts, ok := contactCache.Get(pubkeyHex)
	if !ok || len(contacts) == 0 {
		contacts = fetchContactList(relays, pubkeyHex)
		if contacts == nil || len(contacts) == 0 {
			slog.Debug("check new posts: no contacts", "pubkey", shortID(pubkeyHex))
			data := NewNotesIndicatorData{Since: since, Kinds: kindsStr, Filter: filter, RefreshURL: refreshURL}
			w.Write([]byte(renderNewNotesIndicator(data)))
			return
		}
		contactCache.Set(pubkeyHex, contacts)
		slog.Debug("check new posts: refreshed contacts", "count", len(contacts), "pubkey", shortID(pubkeyHex))
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
// GET /gifs - returns full page (no-JS) or panel fragment (HelmJS)
func htmlGifsHandler(w http.ResponseWriter, r *http.Request) {
	if !GiphyEnabled() {
		util.RespondNotFound(w, "GIF search not available")
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
			util.RespondInternalError(w, "Failed to render GIF panel")
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

	var userNpub string
	if data.LoggedIn {
		data.CSRFToken = generateCSRFToken(session.ID)
		userNpub, _ = encodeBech32Pubkey(hex.EncodeToString(session.UserPubKey))
	}

	// Build navigation
	feedModeCtx := FeedModeContext{LoggedIn: data.LoggedIn, ActiveFeed: "", CurrentPage: "gifs"}
	data.FeedModes = GetFeedModes(feedModeCtx)
	navCtx := NavContext{LoggedIn: data.LoggedIn, ActivePage: "gifs"}
	data.NavItems = GetNavItems(navCtx)
	settingsCtx := SettingsContext{LoggedIn: data.LoggedIn, ThemeLabel: themeLabel, UserNpub: userNpub}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if err := cachedGifsTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
		slog.Error("error rendering gifs page", "error", err)
		util.RespondInternalError(w, "Failed to render page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// htmlGifsSearchHandler handles GIF search requests
// GET /gifs/search?q=... - returns results grid fragment
func htmlGifsSearchHandler(w http.ResponseWriter, r *http.Request) {
	if !GiphyEnabled() {
		util.RespondNotFound(w, "GIF search not available")
		return
	}

	query := r.URL.Query().Get("q")
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		targetID = "post"
	}

	if !isHelmRequest(r) {
		// No-JS fallback: redirect to full page
		http.Redirect(w, r, util.BuildURL("/gifs", map[string]string{"q": query}), http.StatusSeeOther)
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
		util.RespondInternalError(w, "Failed to render results")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// htmlGifsSelectHandler handles GIF selection
// GET /gifs/select?url=...&thumb=... - returns attachment preview + OOB panel clear
func htmlGifsSelectHandler(w http.ResponseWriter, r *http.Request) {
	gifURL := r.URL.Query().Get("url")
	thumbURL := r.URL.Query().Get("thumb")
	targetID := r.URL.Query().Get("target")
	if targetID == "" {
		targetID = "post"
	}

	if gifURL == "" {
		util.RespondBadRequest(w, "Missing url parameter")
		return
	}

	if thumbURL == "" {
		thumbURL = gifURL
	}

	if !isHelmRequest(r) {
		// No-JS fallback: redirect to compose page
		http.Redirect(w, r, util.BuildURL("/compose", map[string]string{
			"media_url":   gifURL,
			"media_thumb": thumbURL,
		}), http.StatusSeeOther)
		return
	}

	html, err := renderGifAttachment(gifURL, thumbURL, targetID)
	if err != nil {
		slog.Error("error rendering gif attachment", "error", err)
		util.RespondInternalError(w, "Failed to render attachment")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// htmlGifsClearHandler clears the GIF attachment
// GET /gifs/clear - returns empty content to clear the attachment area
func htmlGifsClearHandler(w http.ResponseWriter, r *http.Request) {
	if !isHelmRequest(r) {
		// No-JS fallback: redirect back
		referer := r.Header.Get("Referer")
		if referer == "" {
			referer = "/timeline"
		}
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	// Return empty content to clear the attachment
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// htmlGifsCloseHandler closes the GIF panel
// GET /gifs/close - returns empty content to close the panel
func htmlGifsCloseHandler(w http.ResponseWriter, r *http.Request) {
	if !isHelmRequest(r) {
		// No-JS fallback: redirect back
		referer := r.Header.Get("Referer")
		if referer == "" {
			referer = "/timeline"
		}
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}

	// Return empty content to close the panel
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
}

// htmlComposeHandler handles the compose page (no-JS fallback for media)
// GET /compose?media_url=... - compose page with attached media
func htmlComposeHandler(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		// Redirect to login
		http.Redirect(w, r, util.BuildURL("/login", map[string]string{
			"return_url": r.URL.String(),
		}), http.StatusSeeOther)
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
		ActiveRelays:           config.GetDefaultRelays(),
	}

	// Build navigation
	feedModeCtx := FeedModeContext{LoggedIn: true, ActiveFeed: "", CurrentPage: "compose"}
	data.FeedModes = GetFeedModes(feedModeCtx)
	data.KindFilters = GetKindFilters(KindFilterContext{LoggedIn: true, ActiveFeed: "", ActiveKinds: ""})
	navCtx := NavContext{LoggedIn: true, ActivePage: "compose"}
	data.NavItems = GetNavItems(navCtx)
	userNpub, _ := encodeBech32Pubkey(userPubkeyHex)
	settingsCtx := SettingsContext{LoggedIn: true, ThemeLabel: themeLabel, UserAvatarURL: userAvatarURL, UserNpub: userNpub}
	data.SettingsItems = GetSettingsItems(settingsCtx)
	data.SettingsToggle = GetSettingsToggle(settingsCtx)

	var buf strings.Builder
	if err := cachedComposeTemplate.ExecuteTemplate(&buf, tmplBase, data); err != nil {
		slog.Error("error rendering compose page", "error", err)
		util.RespondInternalError(w, "Failed to render page")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// htmlMentionsHandler handles @mention autocomplete search
// GET /mentions?target=post&content=Hello+@vin
func htmlMentionsHandler(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		// Return empty for non-logged-in users
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}

	target := r.URL.Query().Get("target")
	if target == "" {
		target = "post"
	}
	content := r.URL.Query().Get("content")

	// Find @mention query at end of content (at least 3 chars after @)
	query := extractMentionQuery(content)
	if query == "" || len(query) < 3 {
		// No valid mention query, return empty
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Search for matching profiles
	results := searchMentionProfiles(session, query)

	// Render dropdown
	data := templates.MentionsDropdownData{
		Results: results,
		Target:  target,
		Query:   query,
	}

	var buf strings.Builder
	if err := cachedMentionsDropdown.ExecuteTemplate(&buf, "mentions-dropdown", data); err != nil {
		slog.Error("error rendering mentions dropdown", "error", err)
		util.RespondInternalError(w, "Failed to render")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// htmlMentionSelectHandler handles mention selection
// GET /mentions/select?target=post&query=vin&name=alice&pubkey=abc123
func htmlMentionSelectHandler(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")
	if target == "" {
		target = "post"
	}
	query := r.URL.Query().Get("query")
	name := r.URL.Query().Get("name")
	pubkey := r.URL.Query().Get("pubkey")

	if query == "" || name == "" || pubkey == "" {
		util.RespondBadRequest(w, "Missing parameters")
		return
	}

	// Render OOB response
	data := templates.MentionsSelectData{
		Target: target,
		Query:  query,
		Name:   name,
		Pubkey: pubkey,
	}

	var buf strings.Builder
	if err := cachedMentionsSelectResponse.ExecuteTemplate(&buf, "mentions-select-response", data); err != nil {
		slog.Error("error rendering mentions select response", "error", err)
		util.RespondInternalError(w, "Failed to render")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(buf.String()))
}

// extractMentionQuery extracts the @mention query from the end of content
// Returns empty string if no valid @mention found
func extractMentionQuery(content string) string {
	if content == "" {
		return ""
	}

	// Find last @ in content
	lastAt := strings.LastIndex(content, "@")
	if lastAt == -1 {
		return ""
	}

	// Extract text after @
	after := content[lastAt+1:]

	// Must be word characters only (letters, numbers, underscore)
	// Stop at first non-word character
	var query strings.Builder
	for _, r := range after {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			query.WriteRune(r)
		} else {
			break
		}
	}

	return query.String()
}

// searchMentionProfiles searches for profiles matching the query
// Searches follows first, then cached profiles
func searchMentionProfiles(session *BunkerSession, query string) []templates.MentionResult {
	queryLower := strings.ToLower(query)
	var results []templates.MentionResult
	seen := make(map[string]bool)
	const maxResults = 8

	// Helper to add a profile match
	addMatch := func(pubkey string, profile *ProfileInfo) {
		if seen[pubkey] || len(results) >= maxResults {
			return
		}
		seen[pubkey] = true

		displayName := getProfileDisplayName(profile, pubkey)
		npubShort := formatPubkeyShort(pubkey)
		picture := ""
		if profile != nil && profile.Picture != "" {
			picture = profile.Picture
		}

		results = append(results, templates.MentionResult{
			Pubkey:      pubkey,
			DisplayName: displayName,
			NpubShort:   npubShort,
			Picture:     picture,
		})
	}

	// Search follows first (most relevant)
	follows := session.GetFollowingPubkeys()

	// Check cached profiles for follows
	for _, pubkey := range follows {
		if len(results) >= maxResults {
			break
		}
		profile := getCachedProfile(pubkey)
		if profile == nil {
			continue
		}
		if matchesProfile(profile, pubkey, queryLower) {
			addMatch(pubkey, profile)
		}
	}

	// Note: Could extend to search all cached profiles if needed,
	// but follows are the most relevant results for mentions

	return results
}

// matchesProfile checks if a profile matches the search query
func matchesProfile(profile *ProfileInfo, pubkey, queryLower string) bool {
	if profile == nil {
		return false
	}

	// Match against display_name
	if profile.DisplayName != "" && strings.Contains(strings.ToLower(profile.DisplayName), queryLower) {
		return true
	}

	// Match against name
	if profile.Name != "" && strings.Contains(strings.ToLower(profile.Name), queryLower) {
		return true
	}

	// Match against nip05 (before @)
	if profile.Nip05 != "" {
		nip05Lower := strings.ToLower(profile.Nip05)
		atIdx := strings.Index(nip05Lower, "@")
		if atIdx > 0 {
			localPart := nip05Lower[:atIdx]
			if strings.Contains(localPart, queryLower) {
				return true
			}
		} else if strings.Contains(nip05Lower, queryLower) {
			return true
		}
	}

	// Match against npub prefix
	npub, err := encodeBech32Pubkey(pubkey)
	if err == nil && strings.HasPrefix(strings.ToLower(npub), "npub1"+queryLower) {
		return true
	}

	return false
}

// getProfileDisplayName returns a display name for a profile
func getProfileDisplayName(profile *ProfileInfo, pubkey string) string {
	if profile != nil {
		if profile.DisplayName != "" {
			return profile.DisplayName
		}
		if profile.Name != "" {
			return profile.Name
		}
	}
	return formatPubkeyShort(pubkey)
}

// formatPubkeyShort formats a pubkey as a short npub
func formatPubkeyShort(pubkey string) string {
	npub, err := encodeBech32Pubkey(pubkey)
	if err != nil {
		if len(pubkey) > 12 {
			return pubkey[:8] + "..." + pubkey[len(pubkey)-4:]
		}
		return pubkey
	}
	if len(npub) > 16 {
		return npub[:9] + "..." + npub[len(npub)-4:]
	}
	return npub
}

// htmlAuthorFragmentHandler handles /fragment/author/{npub} for lazy-loading author info.
// This endpoint is used by HelmJS to fetch author headers for notes where the profile
// timed out during initial page load. It uses a longer timeout since it's async.
func htmlAuthorFragmentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		util.RespondMethodNotAllowed(w, "Method not allowed")
		return
	}

	// Extract npub from path: /fragment/author/{npub}
	path := strings.TrimPrefix(r.URL.Path, "/fragment/author/")
	if path == "" || path == r.URL.Path {
		util.RespondBadRequest(w, "Missing npub")
		return
	}

	npub := strings.TrimSuffix(path, "/")
	pubkey, err := decodeBech32Pubkey(npub)
	if err != nil {
		util.RespondBadRequest(w, "Invalid npub")
		return
	}

	// Fetch profile with longer timeout (5s) since this is async
	relays := config.GetProfileRelays()
	profiles := fetchProfilesWithTimeout(relays, []string{pubkey}, 5*time.Second)
	profile := profiles[pubkey]

	// If still no profile, return minimal placeholder that won't re-fetch
	if profile == nil {
		// Return a placeholder that shows the npub but won't keep retrying
		npubShort := npub
		if len(npubShort) > 16 {
			npubShort = npubShort[:9] + "..." + npubShort[len(npubShort)-4:]
		}

		html := fmt.Sprintf(`<div class="note-author" id="author-%s">
  <a href="/profile/%s" class="text-muted" rel="author">
  <img class="author-avatar" src="/static/avatar.jpg" alt="User's avatar" loading="lazy">
  </a>
  <div class="author-info">
    <a href="/profile/%s" class="text-muted" rel="author">
    <span class="pubkey" title="%s">%s</span>
    </a>
  </div>
</div>`, pubkey, npub, npub, pubkey, npubShort)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(html))
		return
	}

	// Build the author header HTML with profile data
	npubShort := npub
	if len(npubShort) > 16 {
		npubShort = npubShort[:9] + "..." + npubShort[len(npubShort)-4:]
	}

	// Determine display name
	displayName := npubShort
	if profile.DisplayName != "" {
		displayName = profile.DisplayName
	} else if profile.Name != "" {
		displayName = profile.Name
	}

	// Build avatar URL
	avatar := GetValidatedAvatarURL(profile.Picture)

	// Build NIP-05 badge if verified
	nip05Badge := ""
	if profile.NIP05Verified && profile.NIP05Domain != "" {
		nip05Badge = fmt.Sprintf(` <a href="%s" target="_blank" rel="noopener" title="%s" class="nip05-verified">%s</a>`,
			GetNIP05VerificationURL(profile.Nip05), profile.NIP05Domain, config.GetNIP05Badge())
	}

	html := fmt.Sprintf(`<div class="note-author" id="author-%s">
  <a href="/profile/%s" class="text-muted" rel="author">
  <img class="author-avatar" src="%s" alt="%s's avatar" loading="lazy">
  </a>
  <div class="author-info">
    <a href="/profile/%s" class="text-muted" rel="author">
    <span class="author-name">%s%s</span>
    </a>
  </div>
</div>`, pubkey, npub, avatar, displayName, npub, displayName, nip05Badge)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=300") // Cache successful lookups for 5 min
	w.Write([]byte(html))
}
