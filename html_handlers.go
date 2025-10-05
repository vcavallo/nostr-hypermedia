package main

import (
	"log"
	"net/http"
)

func htmlTimelineHandler(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters (same as JSON handler)
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
			GeneratedAt:   timeNow(),
		},
	}

	// Add pagination if we have results
	if len(items) > 0 {
		lastCreatedAt := items[len(items)-1].CreatedAt
		resp.Page.Until = &lastCreatedAt
		nextURL := buildPaginationURL(r.URL.Path, relays, authors, kinds, limit, lastCreatedAt)
		resp.Page.Next = &nextURL
	}

	// Render HTML
	html, err := renderHTML(resp, relays, authors, kinds, limit)
	if err != nil {
		log.Printf("Error rendering HTML: %v", err)
		http.Error(w, "Error rendering page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=5")
	w.Write([]byte(html))
}
