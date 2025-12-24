package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nostr-server/internal/config"
	"nostr-server/internal/types"
	"nostr-server/internal/util"
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

// Type aliases for internal/types
type ProfileInfo = types.ProfileInfo
type ReactionsSummary = types.ReactionsSummary

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
	w.Header().Set("Vary", "Accept")

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "./static/index.html")
		return
	}

	q := r.URL.Query()

	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = config.GetDefaultRelays()
	}

	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	limit := parseLimit(q.Get("limit"), 50)
	since := parseInt64(q.Get("since"))
	until := parseInt64(q.Get("until"))
	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   limit,
		Since:   since,
		Until:   until,
	}

	noReplies := q.Get("no_replies") != "0" // Default: filter replies

	events, eose := fetchEventsFromRelaysCached(relays, filter)

	// Filter events in a single pass: remove replies (if enabled) and events missing required tags
	{
		filtered := make([]Event, 0, len(events))
		for _, evt := range events {
			kindDef := GetKindDefinition(evt.Kind)
			// Skip replies unless kind is excluded from reply filter
			if noReplies && isReply(evt) && !kindDef.ExcludeFromReplyFilter {
				continue
			}
			// Skip events missing required tags
			if !kindDef.HasRequiredTags(evt.Tags) {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}

	// Collect pubkeys and event IDs for profile/reaction enrichment
	pubkeys, eventIDs := CollectEventData(events)

	// Fetch enrichment data in parallel
	enrichment := FetchEventEnrichment(relays, pubkeys, eventIDs, false)

	// Convert to EventItems
	items := EventsToItems(events, enrichment.Profiles, enrichment.Reactions, enrichment.ReplyCounts)

	resp := TimelineResponse{
		Items: items,
		Page:  PageInfo{},
		Meta: MetaInfo{
			QueriedRelays: len(relays),
			EOSE:          eose,
			GeneratedAt:   time.Now(),
		},
	}

	if len(items) > 0 { // Pagination
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
		resp.Page.Next = &nextURL
	}

	etag := generateETag(items)
	w.Header().Set("ETag", etag)
	if len(items) > 0 {
		lastMod := time.Unix(items[0].CreatedAt, 0).UTC()
		w.Header().Set("Last-Modified", lastMod.Format(http.TimeFormat))
	}

	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Cache-Control", "max-age=5")

	if strings.Contains(accept, "application/vnd.siren+json") {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		siren := toSirenTimeline(resp, relays, authors, kinds, limit)
		if err := json.NewEncoder(w).Encode(siren); err != nil {
			slog.Error("failed to encode siren timeline response", "error", err)
		}
	} else {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode timeline response", "error", err)
		}
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
	w.Header().Set("Vary", "Accept")

	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, "./static/index.html")
		return
	}

	eventID := strings.TrimPrefix(r.URL.Path, "/thread/")
	if eventID == "" {
		util.RespondBadRequest(w, "Event ID required")
		return
	}

	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = config.GetDefaultRelays()
	}

	// Fetch thread for eventID

	var rootEvent *Event
	var replies []Event
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		events := fetchEventByID(relays, eventID)
		if len(events) > 0 {
			rootEvent = &events[0]
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		replies, _ = fetchEventsFromRelaysWithETags(relays, []string{eventID})
		filtered := make([]Event, 0) // Filter to kind 1 only
		for _, evt := range replies {
			if evt.Kind == 1 {
				filtered = append(filtered, evt)
			}
		}
		replies = filtered
	}()

	wg.Wait()

	if rootEvent == nil {
		util.RespondNotFound(w, "Event not found")
		return
	}

	pubkeySet := make(map[string]bool)
	pubkeySet[rootEvent.PubKey] = true
	for _, reply := range replies {
		pubkeySet[reply.PubKey] = true
	}

	pubkeys := make([]string, 0, len(pubkeySet))
	for pk := range pubkeySet {
		pubkeys = append(pubkeys, pk)
	}
	profiles := fetchProfiles(relays, pubkeys)

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

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "max-age=10")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode thread response", "error", err)
	}
}

// isReply returns true if event has e-tags (is a reply)
func isReply(evt Event) bool {
	return util.HasTag(evt.Tags, "e")
}
