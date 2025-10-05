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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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

	// Collect events and dedupe
	seenIDs := make(map[string]bool)
	events := []Event{}

	for evt := range eventChan {
		if !seenIDs[evt.ID] {
			seenIDs[evt.ID] = true
			events = append(events, evt)
		}
	}

	// Check if all relays sent EOSE
	eoseCount := 0
	for range eoseChan {
		eoseCount++
	}
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
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		log.Printf("Failed to connect to %s: %v", relayURL, err)
		return
	}
	defer conn.Close()

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
