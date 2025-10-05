package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type TimelineResponse struct {
	Items []EventItem        `json:"items"`
	Page  PageInfo           `json:"page"`
	Meta  MetaInfo           `json:"meta"`
}

type EventItem struct {
	ID         string     `json:"id"`
	Kind       int        `json:"kind"`
	Pubkey     string     `json:"pubkey"`
	CreatedAt  int64      `json:"created_at"`
	Content    string     `json:"content"`
	Tags       [][]string `json:"tags"`
	Sig        string     `json:"sig"`
	RelaysSeen []string   `json:"relays_seen"`
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

func timelineHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	q := r.URL.Query()

	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
		}
	}

	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	limit := parseLimit(q.Get("limit"), 50)
	since := parseInt64(q.Get("since"))
	until := parseInt64(q.Get("until"))

	// Build filter
	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   limit,
		Since:   since,
		Until:   until,
	}

	// Fetch events from relays
	events, eose := fetchEventsFromRelays(relays, filter)

	// Build response
	items := make([]EventItem, len(events))
	for i, evt := range events {
		items[i] = EventItem{
			ID:         evt.ID,
			Kind:       evt.Kind,
			Pubkey:     evt.PubKey,
			CreatedAt:  evt.CreatedAt,
			Content:    evt.Content,
			Tags:       evt.Tags,
			Sig:        evt.Sig,
			RelaysSeen: evt.RelaysSeen,
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
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/vnd.siren+json") {
		w.Header().Set("Content-Type", "application/vnd.siren+json")
		siren := toSirenTimeline(resp, relays, authors, kinds, limit)
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
