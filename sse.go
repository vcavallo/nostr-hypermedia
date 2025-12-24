package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"nostr-server/internal/config"
	"nostr-server/internal/util"
)

// SSE event types
const (
	SSEEventNote    = "note"
	SSEEventEOSE    = "eose"
	SSEEventError   = "error"
	SSEEventPing    = "ping"
	SSEEventReload  = "reload"
)

// ConfigReloadBroadcaster manages SSE clients waiting for config reload events
type ConfigReloadBroadcaster struct {
	mu      sync.RWMutex
	clients map[chan struct{}]*clientInfo
}

// clientInfo tracks metadata about SSE clients for cleanup
type clientInfo struct {
	addedAt  time.Time
	closeOnce sync.Once
}

// Maximum age for SSE client channels before cleanup
const maxSSEClientAge = 15 * time.Minute

var configBroadcaster = &ConfigReloadBroadcaster{
	clients: make(map[chan struct{}]*clientInfo),
}

func init() {
	// Start periodic cleanup of orphaned SSE clients
	go configBroadcaster.cleanupLoop()
}

// cleanupLoop periodically removes orphaned client channels
func (b *ConfigReloadBroadcaster) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.cleanup()
	}
}

// cleanup removes channels that are too old (likely orphaned)
func (b *ConfigReloadBroadcaster) cleanup() {
	now := time.Now()
	type toRemoveItem struct {
		ch   chan struct{}
		info *clientInfo
	}
	var toRemove []toRemoveItem

	b.mu.RLock()
	for ch, info := range b.clients {
		if now.Sub(info.addedAt) > maxSSEClientAge {
			toRemove = append(toRemove, toRemoveItem{ch, info})
		}
	}
	b.mu.RUnlock()

	if len(toRemove) == 0 {
		return
	}

	b.mu.Lock()
	for _, item := range toRemove {
		// Double-check it still exists under write lock
		if _, exists := b.clients[item.ch]; exists {
			delete(b.clients, item.ch)
			// Close channel exactly once using sync.Once
			item.info.closeOnce.Do(func() {
				close(item.ch)
			})
		}
	}
	b.mu.Unlock()

	if len(toRemove) > 0 {
		slog.Debug("SSE config: cleaned up orphaned clients", "count", len(toRemove))
	}
}

// Subscribe adds a client channel to receive reload notifications
func (b *ConfigReloadBroadcaster) Subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = &clientInfo{addedAt: time.Now()}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a client channel safely
// Channel is closed after removal to unblock any pending receives
func (b *ConfigReloadBroadcaster) Unsubscribe(ch chan struct{}) {
	b.mu.Lock()
	info, exists := b.clients[ch]
	delete(b.clients, ch)
	b.mu.Unlock()

	// Close channel exactly once using sync.Once
	if exists && info != nil {
		info.closeOnce.Do(func() {
			close(ch)
		})
	}
}

// Broadcast sends reload signal to all connected clients
// Uses recover to handle any edge cases with closed channels
func (b *ConfigReloadBroadcaster) Broadcast() {
	b.mu.RLock()
	clients := make([]chan struct{}, 0, len(b.clients))
	for ch := range b.clients {
		clients = append(clients, ch)
	}
	b.mu.RUnlock()

	// Send outside lock to prevent blocking other operations
	for _, ch := range clients {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Debug("SSE config: recovered from send on closed channel")
				}
			}()
			select {
			case ch <- struct{}{}:
			default:
				// Client already has a pending notification, skip
			}
		}()
	}
	slog.Info("SSE config: broadcast reload", "clients", len(clients))
}

// BroadcastConfigReload triggers a reload event to all connected SSE clients
func BroadcastConfigReload() {
	configBroadcaster.Broadcast()
}

// SSEEvent represents an event to send over SSE
type SSEEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// streamTimelineHandler handles SSE connections for live timeline updates
// GET /stream/timeline?kinds=1&authors=...
func streamTimelineHandler(w http.ResponseWriter, r *http.Request) {
	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.RespondInternalError(w, "SSE not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Note: No CORS header - SSE is same-origin only for security

	// Track SSE connection
	IncrementSSEConnections()
	defer DecrementSSEConnections()

	// Parse query parameters
	q := r.URL.Query()
	relays := parseStringList(q.Get("relays"))
	if len(relays) == 0 {
		relays = config.GetDefaultRelays()
	}
	authors := parseStringList(q.Get("authors"))
	kinds := parseIntList(q.Get("kinds"))
	if len(kinds) == 0 {
		kinds = []int{1} // Default to notes
	}

	// Create context that cancels when client disconnects
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Track seen events to avoid duplicates
	seenIDs := make(map[string]bool)
	seenMu := sync.Mutex{}

	// Channel for events from relays
	eventChan := make(chan Event, 100)
	eoseChan := make(chan string, len(relays))

	// Subscribe to relays
	filter := Filter{
		Authors: authors,
		Kinds:   kinds,
		Limit:   50, // Initial batch
	}

	slog.Debug("SSE: starting stream", "kinds", kinds, "authors", len(authors), "relays", len(relays))

	// Start fetching from relays
	var wg sync.WaitGroup
	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			streamFromRelay(ctx, relayURL, filter, eventChan, eoseChan)
		}(relay)
	}

	// Close channels when all relay goroutines complete or context is cancelled
	closerDone := make(chan struct{})
	go func() {
		defer close(closerDone)
		// Wait for relay goroutines to finish
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		// Exit when either all relays finish or context is cancelled
		select {
		case <-done:
		case <-ctx.Done():
			// Context cancelled - relay goroutines will exit soon
			// Wait briefly for clean shutdown, then exit
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
		close(eventChan)
		close(eoseChan)
	}()
	defer func() { <-closerDone }() // Ensure closer goroutine exits before we return

	// Send initial connection event
	sendSSEEvent(w, flusher, "connected", map[string]interface{}{
		"relays": len(relays),
		"kinds":  kinds,
	})

	// Ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	eoseCount := 0

	// Event loop
	for {
		select {
		case <-ctx.Done():
			slog.Debug("SSE: client disconnected")
			return

		case evt, ok := <-eventChan:
			if !ok {
				// Channel closed, all relays done
				sendSSEEvent(w, flusher, "closed", nil)
				return
			}

			// Deduplicate
			seenMu.Lock()
			if seenIDs[evt.ID] {
				seenMu.Unlock()
				continue
			}
			seenIDs[evt.ID] = true
			seenMu.Unlock()

			// Skip replies for cleaner timeline (same logic as timelineHandler)
			if isReply(evt) && evt.Kind != 6 {
				continue
			}

			// Build event item with profile info
			item := EventItem{
				ID:         evt.ID,
				Kind:       evt.Kind,
				Pubkey:     evt.PubKey,
				CreatedAt:  evt.CreatedAt,
				Content:    evt.Content,
				Tags:       evt.Tags,
				Sig:        evt.Sig,
				RelaysSeen: evt.RelaysSeen,
			}

			// Try to get cached profile (don't block SSE for profile fetch)
			item.AuthorProfile = getCachedProfile(evt.PubKey)

			// Send event to client
			sendSSEEvent(w, flusher, SSEEventNote, item)

		case relayURL := <-eoseChan:
			eoseCount++
			slog.Debug("SSE: EOSE received", "relay", relayURL, "count", eoseCount, "total", len(relays))

			// Send EOSE event when all relays have sent EOSE
			if eoseCount == len(relays) {
				sendSSEEvent(w, flusher, SSEEventEOSE, map[string]interface{}{
					"relays": len(relays),
				})
			}

		case <-pingTicker.C:
			// Send ping to keep connection alive
			sendSSEEvent(w, flusher, SSEEventPing, nil)
		}
	}
}

// streamFromRelay subscribes to a relay and streams events
func streamFromRelay(ctx context.Context, relayURL string, filter Filter, eventChan chan<- Event, eoseChan chan<- string) {
	subID := "sse-" + randomString(8)
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
	if len(filter.PTags) > 0 {
		reqFilter["#p"] = filter.PTags
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		slog.Debug("SSE: failed to subscribe", "relay", relayURL, "error", err)
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			eoseChan <- relayURL
			// After EOSE, keep the subscription open for new events
			// Continue listening without returning
		}
	}
}

// sendSSEEvent sends a formatted SSE event to the client
func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	event := SSEEvent{
		Type: eventType,
		Data: data,
	}

	jsonData, err := json.Marshal(event)
	if err != nil {
		slog.Error("SSE: failed to marshal event", "error", err)
		return
	}

	// SSE format: "event: <type>\ndata: <json>\n\n"
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}

// sendSSEHTML sends raw HTML data over SSE (for HelmJS h-sse)
func sendSSEHTML(w http.ResponseWriter, flusher http.Flusher, eventType string, html string) {
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", html)
	flusher.Flush()
}

// streamNotificationsHandler handles SSE connections for live notification updates
// GET /stream/notifications?format=html|json
// Returns HTML for HelmJS clients, JSON for Siren clients
// The format parameter is required
func streamNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	// Check format parameter (required)
	format := r.URL.Query().Get("format")
	if format != "html" && format != "json" {
		util.RespondBadRequest(w, "Missing or invalid format parameter. Use ?format=html or ?format=json")
		return
	}

	// Must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		util.RespondUnauthorized(w, "Unauthorized")
		return
	}

	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.RespondInternalError(w, "SSE not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Track SSE connection
	IncrementSSEConnections()
	defer DecrementSSEConnections()

	isHTMLFormat := format == "html"

	userPubkeyHex := hex.EncodeToString(session.UserPubKey)

	// Get user's relays
	relays := config.GetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays = session.UserRelayList.Read
	}

	// Create context that cancels when client disconnects
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Channel for events from relays
	eventChan := make(chan Event, 100)
	eoseChan := make(chan string, len(relays))

	// Subscribe to events tagging this user, starting from now
	now := time.Now().Unix()
	filter := Filter{
		PTags: []string{userPubkeyHex},
		Kinds: []int{1, 6, 7, 9735}, // Notes, reposts, reactions, zaps
		Since: &now,
	}

	slog.Debug("SSE notifications: starting stream", "user", userPubkeyHex[:8], "relays", len(relays), "format", format)

	// Start subscribing to relays
	var wg sync.WaitGroup
	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			streamFromRelay(ctx, relayURL, filter, eventChan, eoseChan)
		}(relay)
	}

	// Close channels when all relay goroutines complete or context is cancelled
	closerDone := make(chan struct{})
	go func() {
		defer close(closerDone)
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
		close(eventChan)
		close(eoseChan)
	}()
	defer func() { <-closerDone }()

	// Ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	// Track seen events to avoid duplicate notifications
	seenIDs := make(map[string]bool)

	// Badge HTML for HelmJS clients - includes aria-label for screen readers
	badgeHTML := `<span class="notification-badge" id="notification-badge" role="status" aria-label="New notifications"></span>`

	// Event loop
	for {
		select {
		case <-ctx.Done():
			slog.Debug("SSE notifications: client disconnected")
			return

		case evt, ok := <-eventChan:
			if !ok {
				// Channel closed, all relays done
				return
			}

			// Skip self-notifications
			if evt.PubKey == userPubkeyHex {
				continue
			}

			// Deduplicate
			if seenIDs[evt.ID] {
				continue
			}
			seenIDs[evt.ID] = true

			slog.Debug("SSE notifications: new notification", "kind", evt.Kind, "from", evt.PubKey[:8])

			if isHTMLFormat {
				// HTML for HelmJS - just the badge
				sendSSEHTML(w, flusher, "notification", badgeHTML)
			} else {
				// JSON for Siren - full event data
				item := NotificationEvent{
					ID:        evt.ID,
					Kind:      evt.Kind,
					Pubkey:    evt.PubKey,
					CreatedAt: evt.CreatedAt,
					Content:   evt.Content,
				}
				sendSSEEvent(w, flusher, "notification", item)
			}

		case <-eoseChan:
			// Ignore EOSE for notifications - we just keep listening

		case <-pingTicker.C:
			// Send ping to keep connection alive
			if isHTMLFormat {
				sendSSEHTML(w, flusher, "ping", "")
			} else {
				sendSSEEvent(w, flusher, SSEEventPing, nil)
			}
		}
	}
}

// NotificationEvent is a minimal event structure for SSE notification updates
type NotificationEvent struct {
	ID        string `json:"id"`
	Kind      int    `json:"kind"`
	Pubkey    string `json:"pubkey"`
	CreatedAt int64  `json:"created_at"`
	Content   string `json:"content"`
}

// Correction represents a failed action that needs UI rollback
type Correction struct {
	Target string // CSS selector for the element to update
	HTML   string // Replacement HTML content
	Action string // Action type: "react", "bookmark", "mute", "repost"
}

// CorrectionsBroadcaster manages per-session SSE clients for action corrections
type CorrectionsBroadcaster struct {
	mu      sync.RWMutex
	clients map[string]map[chan Correction]*correctionClientInfo // sessionID -> channels
}

type correctionClientInfo struct {
	addedAt   time.Time
	closeOnce sync.Once
}

const maxCorrectionClientAge = 15 * time.Minute

var correctionsBroadcaster = &CorrectionsBroadcaster{
	clients: make(map[string]map[chan Correction]*correctionClientInfo),
}

func init() {
	go correctionsBroadcaster.cleanupLoop()
}

func (b *CorrectionsBroadcaster) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		b.cleanup()
	}
}

func (b *CorrectionsBroadcaster) cleanup() {
	now := time.Now()
	type toRemoveItem struct {
		sessionID string
		ch        chan Correction
		info      *correctionClientInfo
	}
	var toRemove []toRemoveItem

	b.mu.RLock()
	for sessionID, clients := range b.clients {
		for ch, info := range clients {
			if now.Sub(info.addedAt) > maxCorrectionClientAge {
				toRemove = append(toRemove, toRemoveItem{sessionID, ch, info})
			}
		}
	}
	b.mu.RUnlock()

	if len(toRemove) == 0 {
		return
	}

	b.mu.Lock()
	for _, item := range toRemove {
		if clients, exists := b.clients[item.sessionID]; exists {
			if _, chExists := clients[item.ch]; chExists {
				delete(clients, item.ch)
				item.info.closeOnce.Do(func() {
					close(item.ch)
				})
				if len(clients) == 0 {
					delete(b.clients, item.sessionID)
				}
			}
		}
	}
	b.mu.Unlock()

	if len(toRemove) > 0 {
		slog.Debug("SSE corrections: cleaned up orphaned clients", "count", len(toRemove))
	}
}

// Subscribe adds a client channel for a session to receive corrections
func (b *CorrectionsBroadcaster) Subscribe(sessionID string) chan Correction {
	ch := make(chan Correction, 10)
	b.mu.Lock()
	if b.clients[sessionID] == nil {
		b.clients[sessionID] = make(map[chan Correction]*correctionClientInfo)
	}
	b.clients[sessionID][ch] = &correctionClientInfo{addedAt: time.Now()}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a client channel for a session
func (b *CorrectionsBroadcaster) Unsubscribe(sessionID string, ch chan Correction) {
	b.mu.Lock()
	if clients, exists := b.clients[sessionID]; exists {
		if info, chExists := clients[ch]; chExists {
			delete(clients, ch)
			info.closeOnce.Do(func() {
				close(ch)
			})
			if len(clients) == 0 {
				delete(b.clients, sessionID)
			}
		}
	}
	b.mu.Unlock()
}

// SendCorrection sends a correction to all clients for a session
func (b *CorrectionsBroadcaster) SendCorrection(sessionID string, correction Correction) {
	b.mu.RLock()
	clients, exists := b.clients[sessionID]
	if !exists {
		b.mu.RUnlock()
		return
	}
	// Copy channels to avoid holding lock during send
	channels := make([]chan Correction, 0, len(clients))
	for ch := range clients {
		channels = append(channels, ch)
	}
	b.mu.RUnlock()

	for _, ch := range channels {
		select {
		case ch <- correction:
		default:
			// Channel full, skip
		}
	}
	slog.Debug("SSE corrections: sent correction", "session", sessionID[:8], "action", correction.Action)
}

// SendCorrectionToSession is a convenience function for sending corrections
func SendCorrectionToSession(sessionID string, target, html, action string) {
	correctionsBroadcaster.SendCorrection(sessionID, Correction{
		Target: target,
		HTML:   html,
		Action: action,
	})
}

// streamConfigHandler handles SSE connections for config reload notifications
// GET /stream/config
// Sends a "reload" event when server config is reloaded (via SIGHUP or Nostr)
func streamConfigHandler(w http.ResponseWriter, r *http.Request) {
	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.RespondInternalError(w, "SSE not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Track SSE connection
	IncrementSSEConnections()
	defer DecrementSSEConnections()

	// Create context that cancels when client disconnected
	ctx := r.Context()

	// Subscribe to config reload events
	reloadChan := configBroadcaster.Subscribe()
	defer configBroadcaster.Unsubscribe(reloadChan)

	// Ping ticker to keep connection alive (prevents proxy/browser timeouts)
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	slog.Debug("SSE config: client connected")

	// Event loop
	for {
		select {
		case <-ctx.Done():
			slog.Debug("SSE config: client disconnected")
			return

		case <-reloadChan:
			// Send reload event with non-empty data to ensure HelmJS processes it
			sendSSEHTML(w, flusher, SSEEventReload, "1")

		case <-pingTicker.C:
			// Send ping to keep connection alive
			sendSSEHTML(w, flusher, SSEEventPing, "")
		}
	}
}

// streamCorrectionsHandler handles SSE connections for action correction notifications
// GET /stream/corrections
// Sends correction events when async action publishes fail
func streamCorrectionsHandler(w http.ResponseWriter, r *http.Request) {
	// Must be logged in
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		util.RespondUnauthorized(w, "Unauthorized")
		return
	}

	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.RespondInternalError(w, "SSE not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Track SSE connection
	IncrementSSEConnections()
	defer DecrementSSEConnections()

	ctx := r.Context()

	// Subscribe to corrections for this session
	correctionChan := correctionsBroadcaster.Subscribe(session.ID)
	defer correctionsBroadcaster.Unsubscribe(session.ID, correctionChan)

	// Ping ticker to keep connection alive
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	slog.Debug("SSE corrections: client connected", "session", session.ID[:8])

	// Event loop
	for {
		select {
		case <-ctx.Done():
			slog.Debug("SSE corrections: client disconnected", "session", session.ID[:8])
			return

		case correction, ok := <-correctionChan:
			if !ok {
				return
			}
			// Send correction as HTML that HelmJS can swap
			// Format: target selector in event name, HTML in data
			// HelmJS will use h-target from the SSE template
			sendSSEHTML(w, flusher, "correction", correction.HTML)

		case <-pingTicker.C:
			sendSSEHTML(w, flusher, SSEEventPing, "")
		}
	}
}
