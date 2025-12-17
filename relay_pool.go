package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Connection pool limits
const (
	maxTotalConnections = 50 // Maximum total relay connections
)

// isRelayURLSafe validates relay URL (blocks private IPs, allows localhost)
func isRelayURLSafe(relayURL string) bool {
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return false
	}

	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return false
	}

	host := parsed.Hostname()
	if host == "" {
		return false
	}

	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		// Can't resolve - allow unless obviously internal
		if len(host) > 0 && (host[len(host)-1] == '.' ||
			strings.Contains(host, ".local") || strings.Contains(host, ".internal")) {
			return false
		}
		return true
	}

	for _, ip := range ips {
		if !isRelayIPSafe(ip) {
			return false
		}
	}

	return true
}

// isRelayIPSafe returns false for private/internal IPs (except loopback)
func isRelayIPSafe(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) { // Cloud metadata
		return false
	}
	return true
}

type Subscription struct {
	ID        string
	EventChan chan Event
	EOSEChan  chan bool
	Done      chan struct{}
	closeOnce sync.Once
}

func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		close(s.Done)
	})
}

type OKResponse struct {
	EventID string
	Success bool
	Message string
}

type RelayConn struct {
	conn          *websocket.Conn
	relayURL      string
	mu            sync.Mutex
	writeMu       sync.Mutex
	subscriptions map[string]*Subscription
	okHandlers    map[string]chan OKResponse
	closed        bool
	lastActivity  time.Time
	lastPong      time.Time
}

// isClosed returns whether the connection has been closed (thread-safe)
func (rc *RelayConn) isClosed() bool {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.closed
}

type RelayPool struct {
	mu          sync.RWMutex
	connections map[string]*RelayConn
	stopCh      chan struct{}
	stopOnce    sync.Once
}

type relayFailure struct {
	lastFailure  time.Time
	failureCount int
	backoffUntil time.Time
}

type relayStats struct {
	avgResponseTime time.Duration
	responseCount   int
	lastResponse    time.Time
}

type RelayHealth struct {
	mu       sync.RWMutex
	failures map[string]*relayFailure
	stats    map[string]*relayStats
}

var relayHealth = &RelayHealth{
	failures: make(map[string]*relayFailure),
	stats:    make(map[string]*relayStats),
}

func (h *RelayHealth) shouldSkip(relayURL string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	f := h.failures[relayURL]
	if f == nil {
		return false
	}
	return time.Now().Before(f.backoffUntil)
}

func (h *RelayHealth) recordFailure(relayURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	f := h.failures[relayURL]
	if f == nil {
		f = &relayFailure{}
		h.failures[relayURL] = f
	}

	f.lastFailure = time.Now()
	f.failureCount++

	var backoff time.Duration // Exponential: 30s, 60s, 2m, 5m max
	switch {
	case f.failureCount <= 1:
		backoff = 30 * time.Second
	case f.failureCount == 2:
		backoff = 60 * time.Second
	case f.failureCount == 3:
		backoff = 2 * time.Minute
	default:
		backoff = 5 * time.Minute
	}

	f.backoffUntil = time.Now().Add(backoff)
	slog.Warn("relay connection failed",
		"relay", relayURL,
		"failure_count", f.failureCount,
		"backoff_until", f.backoffUntil.Format("15:04:05"))
}

func (h *RelayHealth) recordSuccess(relayURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.failures, relayURL)
}

func (h *RelayHealth) recordResponseTime(relayURL string, duration time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.stats[relayURL]
	if s == nil {
		s = &relayStats{}
		h.stats[relayURL] = s
	}

	// Exponential moving average (alpha=0.3)
	if s.responseCount == 0 {
		s.avgResponseTime = duration
	} else {
		alpha := 0.3
		s.avgResponseTime = time.Duration(alpha*float64(duration) + (1-alpha)*float64(s.avgResponseTime))
	}

	s.responseCount++
	s.lastResponse = time.Now()
}

func (h *RelayHealth) getAverageResponseTime(relayURL string) time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()

	s := h.stats[relayURL]
	if s == nil || s.responseCount == 0 {
		return 1 * time.Second
	}
	return s.avgResponseTime
}

// getRelayScore returns 0-100 (higher = better, factors in response time + failures)
func (h *RelayHealth) getRelayScore(relayURL string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	score := 50

	if s := h.stats[relayURL]; s != nil && s.responseCount > 0 {
		avgMs := s.avgResponseTime.Milliseconds()
		switch {
		case avgMs < 200:
			score = 50
		case avgMs < 500:
			score = 40
		case avgMs < 1000:
			score = 25
		default:
			score = 10
		}

		bonus := s.responseCount
		if bonus > 10 {
			bonus = 10
		}
		score += bonus
	}

	if f := h.failures[relayURL]; f != nil {
		penalty := f.failureCount * 10
		if penalty > 30 {
			penalty = 30
		}
		score -= penalty

		if time.Now().Before(f.backoffUntil) {
			score -= 20
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// SortRelaysByScore returns relays sorted by score (best first)
func (h *RelayHealth) SortRelaysByScore(relays []string) []string {
	if len(relays) <= 1 {
		return relays
	}

	scores := make(map[string]int, len(relays))
	for _, relay := range relays {
		scores[relay] = h.getRelayScore(relay)
	}

	sorted := make([]string, len(relays))
	copy(sorted, relays)

	sort.Slice(sorted, func(i, j int) bool {
		return scores[sorted[i]] > scores[sorted[j]]
	})

	return sorted
}

// GetExpectedResponseTime returns expected time for fastest N relays (+50% buffer)
func (h *RelayHealth) GetExpectedResponseTime(relays []string, minRelays int) time.Duration {
	if len(relays) == 0 {
		return 500 * time.Millisecond
	}

	times := make([]time.Duration, 0, len(relays))
	for _, relay := range relays {
		times = append(times, h.getAverageResponseTime(relay))
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j]
	})

	idx := minRelays - 1
	if idx >= len(times) {
		idx = len(times) - 1
	}
	if idx < 0 {
		idx = 0
	}

	expected := times[idx] + times[idx]/2 // +50% buffer
	if expected < 200*time.Millisecond {
		expected = 200 * time.Millisecond
	}
	if expected > 2*time.Second {
		expected = 2 * time.Second
	}

	return expected
}

var relayPool = NewRelayPool()

func NewRelayPool() *RelayPool {
	pool := &RelayPool{
		connections: make(map[string]*RelayConn),
		stopCh:      make(chan struct{}),
	}
	go pool.cleanupLoop()
	return pool
}

// Close shuts down the relay pool and all connections
func (p *RelayPool) Close() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, rc := range p.connections {
			rc.markClosed()
		}
		p.connections = make(map[string]*RelayConn)
	})
}

func DefaultRelays() []string {
	return ConfigGetDefaultRelays()
}

// WarmupConnections pre-connects to default relays at startup
func WarmupConnections() {
	slog.Debug("warming up relay connections")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, relay := range DefaultRelays() {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			if _, err := relayPool.getOrCreateConn(ctx, relayURL); err != nil {
				slog.Debug("warmup failed", "relay", relayURL, "error", err)
			} else {
				slog.Debug("warmup connected", "relay", relayURL)
			}
		}(relay)
	}
	wg.Wait()
	slog.Info("relay warmup complete", "relay_count", len(DefaultRelays()))
}

func (p *RelayPool) getOrCreateConn(ctx context.Context, relayURL string) (*RelayConn, error) {
	relayURL = strings.TrimSuffix(relayURL, "/")

	if !isRelayURLSafe(relayURL) {
		return nil, errors.New("relay URL blocked: unsafe destination")
	}
	if relayHealth.shouldSkip(relayURL) {
		return nil, errors.New("relay in backoff period")
	}

	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()

	if rc != nil && !rc.isClosed() {
		return rc, nil
	}

	// Check connection limit before creating new connection
	p.mu.RLock()
	connCount := len(p.connections)
	p.mu.RUnlock()
	if connCount >= maxTotalConnections {
		return nil, errors.New("connection pool limit reached")
	}

	slog.Debug("creating new relay connection", "relay", relayURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		relayHealth.recordFailure(relayURL)
		return nil, err
	}

	if tcpConn, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true) // Reduce latency
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Another goroutine may have connected while we were dialing
	if existing := p.connections[relayURL]; existing != nil && !existing.closed {
		conn.Close()
		return existing, nil
	}

	rc = &RelayConn{
		conn:          conn,
		relayURL:      relayURL,
		subscriptions: make(map[string]*Subscription),
		okHandlers:    make(map[string]chan OKResponse),
		lastActivity:  time.Now(),
		lastPong:      time.Now(),
	}

	rc.conn.SetPongHandler(func(string) error {
		rc.mu.Lock()
		rc.lastPong = time.Now()
		rc.mu.Unlock()
		return nil
	})

	p.connections[relayURL] = rc
	relayHealth.recordSuccess(relayURL)

	go rc.readLoop()
	go rc.pingLoop()

	return rc, nil
}

func (p *RelayPool) Subscribe(ctx context.Context, relayURL string, subID string, filter map[string]interface{}) (*Subscription, error) {
	const maxRetries = 3
	var rc *RelayConn
	var err error

	for attempt := 0; attempt < maxRetries; attempt++ {
		rc, err = p.getOrCreateConn(ctx, relayURL)
		if err != nil {
			return nil, err
		}

		// Check if connection is still valid using thread-safe method
		if rc.isClosed() {
			// Remove stale connection from pool
			// Acquire pool lock first (consistent lock ordering: pool -> conn)
			p.mu.Lock()
			if p.connections[relayURL] == rc {
				delete(p.connections, relayURL)
			}
			p.mu.Unlock()
			continue
		}
		break
	}

	if rc == nil || rc.isClosed() {
		return nil, errors.New("failed to establish connection after retries")
	}

	bufSize := 100 // Event buffer size
	if limit, ok := filter["limit"].(float64); ok && limit > 0 {
		bufSize = int(limit) * 2
	}
	if bufSize < 50 {
		bufSize = 50
	}
	if bufSize > 500 {
		bufSize = 500
	}
	sub := &Subscription{
		ID:        subID,
		EventChan: make(chan Event, bufSize),
		EOSEChan:  make(chan bool, 1),
		Done:      make(chan struct{}),
	}

	// Add subscription under lock
	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return nil, errors.New("connection closed during subscribe")
	}
	rc.subscriptions[subID] = sub
	rc.mu.Unlock()

	req := []interface{}{"REQ", subID, filter}
	rc.writeMu.Lock()
	rc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = rc.conn.WriteJSON(req)
	rc.conn.SetWriteDeadline(time.Time{}) // Clear deadline
	rc.writeMu.Unlock()

	if err != nil {
		rc.mu.Lock()
		delete(rc.subscriptions, subID)
		rc.mu.Unlock()
		rc.markClosed()
		return nil, err
	}

	rc.mu.Lock()
	rc.lastActivity = time.Now()
	rc.mu.Unlock()
	return sub, nil
}

func (p *RelayPool) Unsubscribe(relayURL string, sub *Subscription) {
	if sub == nil {
		return
	}

	relayURL = strings.TrimSuffix(relayURL, "/")

	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()

	if rc == nil {
		return
	}

	rc.mu.Lock()
	_, exists := rc.subscriptions[sub.ID]
	shouldSendClose := !rc.closed && exists
	if exists {
		delete(rc.subscriptions, sub.ID)
	}
	rc.mu.Unlock()

	if shouldSendClose { // Send CLOSE (best effort)
		closeMsg := []interface{}{"CLOSE", sub.ID}
		rc.writeMu.Lock()
		rc.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		rc.conn.WriteJSON(closeMsg)
		rc.conn.SetWriteDeadline(time.Time{})
		rc.writeMu.Unlock()
	}

	sub.Close()
}

func (rc *RelayConn) readLoop() {
	defer rc.markClosed()

	// Read timeout should be longer than ping interval (30s) + pong timeout (60s)
	// to allow normal operation while catching truly hung connections
	const readTimeout = 90 * time.Second

	for {
		var msg []interface{}
		rc.conn.SetReadDeadline(time.Now().Add(readTimeout))
		err := rc.conn.ReadJSON(&msg)
		if err != nil {
			rc.mu.Lock()
			closed := rc.closed
			rc.mu.Unlock()
			if !closed {
				slog.Debug("relay read error", "relay", rc.relayURL, "error", err)
			}
			return
		}

		rc.mu.Lock()
		rc.lastActivity = time.Now()
		rc.mu.Unlock()

		if len(msg) < 2 {
			continue
		}

		msgType, ok := msg[0].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "EVENT":
			if len(msg) < 3 {
				continue
			}
			subID, ok := msg[1].(string)
			if !ok {
				continue
			}

			evt, ok := parseEventFromInterface(msg[2])
			if !ok {
				continue
			}
			evt.RelaysSeen = []string{rc.relayURL}

			rc.mu.Lock()
			sub := rc.subscriptions[subID]
			rc.mu.Unlock()

			if sub != nil {
				select {
				case sub.EventChan <- evt:
				case <-sub.Done:
				default:
					// Channel full, drop event
					droppedEventCount.Add(1)
					slog.Debug("dropped event (channel full)",
						"event_id", shortID(evt.ID),
						"relay", rc.relayURL,
						"total_dropped", droppedEventCount.Load())
				}
			}

		case "EOSE":
			if len(msg) < 2 {
				continue
			}
			subID, ok := msg[1].(string)
			if !ok {
				continue
			}

			rc.mu.Lock()
			sub := rc.subscriptions[subID]
			rc.mu.Unlock()

			if sub != nil {
				select {
				case sub.EOSEChan <- true:
				default:
				}
			}

		case "CLOSED": // Subscription closed by relay
			if len(msg) >= 2 {
				subID, _ := msg[1].(string)
				rc.mu.Lock()
				sub := rc.subscriptions[subID]
				if sub != nil {
					delete(rc.subscriptions, subID)
				}
				rc.mu.Unlock()
				if sub != nil {
					sub.Close()
				}
			}

		case "OK": // EVENT submission response
			if len(msg) >= 3 {
				eventID, _ := msg[1].(string)
				success, _ := msg[2].(bool)
				message := ""
				if len(msg) >= 4 {
					message, _ = msg[3].(string)
				}

				rc.mu.Lock()
				handler := rc.okHandlers[eventID]
				if handler != nil {
					delete(rc.okHandlers, eventID)
				}
				rc.mu.Unlock()

				if handler != nil {
					select {
					case handler <- OKResponse{EventID: eventID, Success: success, Message: message}:
					default:
					}
				}
			}

		case "NOTICE":
			if len(msg) >= 2 {
				notice, _ := msg[1].(string)
				slog.Debug("relay NOTICE", "relay", rc.relayURL, "message", notice)
			}
		}
	}
}

func (rc *RelayConn) pingLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		rc.mu.Lock()
		closed := rc.closed
		lastPong := rc.lastPong
		rc.mu.Unlock()

		if closed {
			return
		}

		if time.Since(lastPong) > 60*time.Second { // No pong in 60s = dead
			slog.Debug("relay connection dead (no pong)", "relay", rc.relayURL)
			rc.markClosed()
			return
		}

		rc.writeMu.Lock()
		rc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		err := rc.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
		rc.conn.SetWriteDeadline(time.Time{})
		rc.writeMu.Unlock()

		if err != nil {
			slog.Debug("relay ping failed", "relay", rc.relayURL, "error", err)
			rc.markClosed()
			return
		}
	}
}

func (rc *RelayConn) markClosed() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.closed {
		return
	}

	rc.closed = true
	rc.conn.Close()

	for _, sub := range rc.subscriptions {
		sub.Close()
	}
	rc.subscriptions = make(map[string]*Subscription)

	for eventID, handler := range rc.okHandlers {
		select {
		case handler <- OKResponse{EventID: eventID, Success: false, Message: "connection closed"}:
		default:
		}
	}
	rc.okHandlers = make(map[string]chan OKResponse)
}

func (p *RelayPool) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.cleanup()
		}
	}
}

func (p *RelayPool) cleanup() {
	now := time.Now()

	// First pass: collect connections to check (under read lock)
	p.mu.RLock()
	toCheck := make(map[string]*RelayConn)
	for url, rc := range p.connections {
		toCheck[url] = rc
	}
	p.mu.RUnlock()

	// Check each connection without holding pool lock (avoid nested locks)
	var toRemove []string
	var toClose []*RelayConn
	for url, rc := range toCheck {
		rc.mu.Lock()
		idle := len(rc.subscriptions) == 0 && now.Sub(rc.lastActivity) > 2*time.Minute
		closed := rc.closed
		rc.mu.Unlock()

		if closed || idle {
			toRemove = append(toRemove, url)
			if !closed {
				slog.Debug("closing idle relay connection", "relay", url)
				toClose = append(toClose, rc)
			}
		}
	}

	// Close idle connections
	for _, rc := range toClose {
		rc.markClosed()
	}

	// Second pass: remove from pool (under write lock)
	if len(toRemove) > 0 {
		p.mu.Lock()
		for _, url := range toRemove {
			// Double-check the connection is still the same before removing
			if rc, exists := p.connections[url]; exists && (rc.isClosed() || toCheck[url] == rc) {
				delete(p.connections, url)
			}
		}
		p.mu.Unlock()
	}
}

// PublishEvent sends an event to the relay and waits for OK response
func (p *RelayPool) PublishEvent(ctx context.Context, relayURL string, eventID string, eventMsg []interface{}) (OKResponse, error) {
	rc, err := p.getOrCreateConn(ctx, relayURL)
	if err != nil {
		return OKResponse{}, err
	}

	okChan := make(chan OKResponse, 1)

	rc.mu.Lock()
	if rc.closed {
		rc.mu.Unlock()
		return OKResponse{}, errors.New("connection closed")
	}
	rc.okHandlers[eventID] = okChan
	rc.mu.Unlock()

	defer func() {
		rc.mu.Lock()
		delete(rc.okHandlers, eventID)
		rc.mu.Unlock()
	}()

	rc.writeMu.Lock()
	rc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = rc.conn.WriteJSON(eventMsg)
	rc.conn.SetWriteDeadline(time.Time{})
	rc.writeMu.Unlock()

	if err != nil {
		return OKResponse{}, err
	}

	select {
	case resp := <-okChan:
		return resp, nil
	case <-ctx.Done():
		return OKResponse{}, ctx.Err()
	}
}
