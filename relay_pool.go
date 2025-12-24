package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"nostr-server/internal/config"
	"nostr-server/internal/nostr"
	"nostr-server/internal/util"
)

// Connection pool limits
const (
	maxTotalConnections      = 150 // Maximum total relay connections (increased for outbox model)
	lruEvictionThreshold     = 140 // Start LRU eviction when pool reaches this size
	lruEvictionTargetFree    = 20  // Number of connections to free when evicting
	lruEvictionMinIdleTime   = 30 * time.Second // Minimum idle time before eligible for eviction
)

// Per-relay concurrency limits to prevent "too many concurrent REQs" errors
const (
	maxConcurrentReqsPerRelay = 10               // Max concurrent subscriptions per relay
	semaphoreAcquireTimeout   = 5 * time.Second  // Timeout for acquiring semaphore slot
)

// DNS cache to avoid repeated lookups for the same host
const (
	dnsCacheTTL     = 5 * time.Minute
	dnsCacheMaxSize = 500 // Limit entries to prevent unbounded growth
)

type dnsCacheEntry struct {
	ips       []net.IP
	expiresAt time.Time
	safe      bool // cached safety check result
}

var (
	dnsCache   = make(map[string]*dnsCacheEntry)
	dnsCacheMu sync.RWMutex
)

// Write-only relay detection (relays that accept EVENT but not REQ)
const writeOnlyRelayTTL = 1 * time.Hour

type writeOnlyRelayEntry struct {
	detectedAt time.Time
}

var (
	writeOnlyRelays   = make(map[string]*writeOnlyRelayEntry)
	writeOnlyRelaysMu sync.RWMutex
)

// IsWriteOnlyRelay checks if a relay is known to be write-only (from config or dynamic detection)
func IsWriteOnlyRelay(relayURL string) bool {
	normalized := normalizeRelayURL(relayURL)
	if normalized == "" {
		return false
	}

	// Check static config first
	for _, r := range config.GetWriteOnlyRelays() {
		if normalizeRelayURL(r) == normalized {
			return true
		}
	}

	// Check dynamic cache
	writeOnlyRelaysMu.RLock()
	entry, exists := writeOnlyRelays[normalized]
	writeOnlyRelaysMu.RUnlock()

	if exists && time.Since(entry.detectedAt) < writeOnlyRelayTTL {
		return true
	}

	return false
}

// markRelayAsWriteOnly adds a relay to the dynamic write-only cache
func markRelayAsWriteOnly(relayURL string) {
	normalized := normalizeRelayURL(relayURL)
	if normalized == "" {
		return
	}

	writeOnlyRelaysMu.Lock()
	writeOnlyRelays[normalized] = &writeOnlyRelayEntry{
		detectedAt: time.Now(),
	}
	writeOnlyRelaysMu.Unlock()

	slog.Info("relay detected as write-only", "relay", normalized)
}

// lookupIPCached performs DNS lookup with caching
func lookupIPCached(host string) ([]net.IP, error) {
	dnsCacheMu.RLock()
	entry, exists := dnsCache[host]
	dnsCacheMu.RUnlock()

	if exists && time.Now().Before(entry.expiresAt) {
		return entry.ips, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	dnsCacheMu.Lock()
	// Evict oldest entries if at max size
	if len(dnsCache) >= dnsCacheMaxSize {
		dnsCacheEvictOldest()
	}
	dnsCache[host] = &dnsCacheEntry{
		ips:       ips,
		expiresAt: time.Now().Add(dnsCacheTTL),
	}
	dnsCacheMu.Unlock()

	return ips, nil
}

// dnsCacheEvictOldest removes 10% of oldest entries (must hold write lock)
func dnsCacheEvictOldest() {
	toRemove := dnsCacheMaxSize / 10
	if toRemove < 1 {
		toRemove = 1
	}

	type hostExpiry struct {
		host      string
		expiresAt time.Time
	}

	entries := make([]hostExpiry, 0, len(dnsCache))
	for host, entry := range dnsCache {
		entries = append(entries, hostExpiry{host, entry.expiresAt})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].expiresAt.Before(entries[j].expiresAt)
	})

	for i := 0; i < toRemove && i < len(entries); i++ {
		delete(dnsCache, entries[i].host)
	}
}

// Custom WebSocket dialer with proper timeouts
var wsDialer = &websocket.Dialer{
	Proxy:            http.ProxyFromEnvironment,
	HandshakeTimeout: 10 * time.Second,
	NetDialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
}

// DNS cache cleanup goroutine
func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			dnsCacheMu.Lock()
			now := time.Now()
			for host, entry := range dnsCache {
				if now.After(entry.expiresAt) {
					delete(dnsCache, host)
				}
			}
			dnsCacheMu.Unlock()
		}
	}()
}

// normalizeRelayURL validates and normalizes a relay URL from NIP-65 events
// Returns empty string if URL is invalid/malformed
var normalizeRelayURL = nostr.NormalizeRelayURL

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

	// Block internal/unreachable hosts (.onion, .local, .internal)
	if util.IsInternalHost(host) {
		return false
	}

	// Allow localhost for development
	if util.IsLoopbackHost(host) {
		return true
	}

	ips, err := lookupIPCached(host)
	if err != nil {
		// Can't resolve - allow unless obviously internal
		if len(host) > 0 && host[len(host)-1] == '.' {
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

// relaySemaphore limits concurrent subscriptions to a single relay
type relaySemaphore struct {
	ch chan struct{}
}

func newRelaySemaphore(size int) *relaySemaphore {
	return &relaySemaphore{ch: make(chan struct{}, size)}
}

// acquire blocks until a slot is available or context is cancelled
func (s *relaySemaphore) acquire(ctx context.Context) error {
	select {
	case s.ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tryAcquire attempts to acquire a slot with timeout
func (s *relaySemaphore) tryAcquire(timeout time.Duration) bool {
	select {
	case s.ch <- struct{}{}:
		return true
	case <-time.After(timeout):
		return false
	}
}

// release returns a slot to the semaphore
func (s *relaySemaphore) release() {
	select {
	case <-s.ch:
	default:
		// Semaphore wasn't held - log but don't panic
		slog.Warn("relay semaphore release called without acquire")
	}
}

type RelayPool struct {
	mu          sync.RWMutex
	connections map[string]*RelayConn
	connecting  map[string]*sync.Mutex // Per-relay mutex to prevent duplicate dial attempts
	connectMu   sync.Mutex             // Protects connecting map
	semaphores  map[string]*relaySemaphore // Per-relay concurrency limiters
	semMu       sync.RWMutex               // Protects semaphores map
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

var relayPool = NewRelayPool()

func NewRelayPool() *RelayPool {
	pool := &RelayPool{
		connections: make(map[string]*RelayConn),
		connecting:  make(map[string]*sync.Mutex),
		semaphores:  make(map[string]*relaySemaphore),
		stopCh:      make(chan struct{}),
	}
	go pool.cleanupLoop()
	return pool
}

// getSemaphore returns the concurrency limiter for a relay, creating one if needed
func (p *RelayPool) getSemaphore(relayURL string) *relaySemaphore {
	p.semMu.RLock()
	sem := p.semaphores[relayURL]
	p.semMu.RUnlock()

	if sem != nil {
		return sem
	}

	p.semMu.Lock()
	defer p.semMu.Unlock()

	// Double-check after acquiring write lock
	if sem = p.semaphores[relayURL]; sem != nil {
		return sem
	}

	sem = newRelaySemaphore(maxConcurrentReqsPerRelay)
	p.semaphores[relayURL] = sem
	return sem
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
	return config.GetDefaultRelays()
}

// IsConnected returns true if a relay connection exists and is not closed
func (p *RelayPool) IsConnected(relayURL string) bool {
	relayURL = strings.TrimSuffix(relayURL, "/")
	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()
	return rc != nil && !rc.isClosed()
}

// GetConnectedRelays returns a list of currently connected relay URLs
func (p *RelayPool) GetConnectedRelays() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	connected := make([]string, 0, len(p.connections))
	for url, rc := range p.connections {
		if !rc.closed {
			connected = append(connected, url)
		}
	}
	return connected
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
	if relayHealthStore.shouldSkip(relayURL) {
		return nil, errors.New("relay in backoff period")
	}

	// Fast path: check if connection already exists
	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()

	if rc != nil && !rc.isClosed() {
		return rc, nil
	}

	// Get or create per-relay connection mutex to prevent duplicate dial attempts
	p.connectMu.Lock()
	relayMu, exists := p.connecting[relayURL]
	if !exists {
		relayMu = &sync.Mutex{}
		p.connecting[relayURL] = relayMu
	}
	p.connectMu.Unlock()

	// Serialize connection attempts for this specific relay
	relayMu.Lock()
	defer relayMu.Unlock()

	// Re-check after acquiring relay mutex (another goroutine may have connected)
	p.mu.RLock()
	rc = p.connections[relayURL]
	p.mu.RUnlock()

	if rc != nil && !rc.isClosed() {
		return rc, nil
	}

	// Check connection limit before creating new connection
	p.mu.RLock()
	connCount := len(p.connections)
	p.mu.RUnlock()
	if connCount >= lruEvictionThreshold {
		// Try LRU eviction to make room
		if !p.evictLRU() && connCount >= maxTotalConnections {
			return nil, errors.New("connection pool limit reached")
		}
	}

	slog.Debug("creating new relay connection", "relay", relayURL)
	conn, _, err := wsDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		// Don't penalize relay for context cancellation (early exit optimization)
		if ctx.Err() == nil {
			relayHealthStore.recordFailure(relayURL)
		}
		return nil, err
	}

	if tcpConn, ok := conn.UnderlyingConn().(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true) // Reduce latency
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Another goroutine may have connected while we were dialing (shouldn't happen with relay mutex)
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
	relayHealthStore.recordSuccess(relayURL)

	go rc.readLoop()
	go rc.pingLoop()

	return rc, nil
}

func (p *RelayPool) Subscribe(ctx context.Context, relayURL string, subID string, filter map[string]interface{}) (*Subscription, error) {
	relayURL = strings.TrimSuffix(relayURL, "/")

	// Acquire per-relay semaphore to limit concurrent REQs
	sem := p.getSemaphore(relayURL)
	if err := sem.acquire(ctx); err != nil {
		return nil, errors.New("semaphore acquire cancelled: " + err.Error())
	}

	// Create subscription with semaphore release on close
	sub, err := p.subscribeWithSemaphore(ctx, relayURL, subID, filter, sem)
	if err != nil {
		sem.release() // Release on error
		return nil, err
	}

	return sub, nil
}

// subscribeWithSemaphore performs the actual subscription after semaphore is acquired
func (p *RelayPool) subscribeWithSemaphore(ctx context.Context, relayURL string, subID string, filter map[string]interface{}, sem *relaySemaphore) (*Subscription, error) {
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

	// Start goroutine to release semaphore when subscription closes
	go func() {
		<-sub.Done
		sem.release()
	}()

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

				// Detect write-only relays from NOTICE messages
				noticeLower := strings.ToLower(notice)
				if strings.Contains(noticeLower, "does not accept req") ||
					strings.Contains(noticeLower, "req not supported") ||
					strings.Contains(noticeLower, "read requests disabled") {
					markRelayAsWriteOnly(rc.relayURL)
				}
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

// evictLRU removes least recently used connections when pool approaches limit
// Returns true if eviction made room for new connections
func (p *RelayPool) evictLRU() bool {
	now := time.Now()

	// Collect connection stats under read lock
	p.mu.RLock()
	connCount := len(p.connections)
	if connCount < lruEvictionThreshold {
		p.mu.RUnlock()
		return true // Plenty of room
	}

	type connInfo struct {
		url          string
		lastActivity time.Time
		subCount     int
		closed       bool
	}

	candidates := make([]connInfo, 0, connCount)
	for url, rc := range p.connections {
		rc.mu.Lock()
		info := connInfo{
			url:          url,
			lastActivity: rc.lastActivity,
			subCount:     len(rc.subscriptions),
			closed:       rc.closed,
		}
		rc.mu.Unlock()
		candidates = append(candidates, info)
	}
	p.mu.RUnlock()

	// Sort by eligibility for eviction:
	// 1. Closed connections first
	// 2. Connections with no subscriptions and old activity
	// 3. By last activity time (oldest first)
	sort.Slice(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]

		// Closed connections always evict first
		if ci.closed != cj.closed {
			return ci.closed
		}

		// Connections with no active subscriptions preferred for eviction
		iIdle := ci.subCount == 0 && now.Sub(ci.lastActivity) > lruEvictionMinIdleTime
		jIdle := cj.subCount == 0 && now.Sub(cj.lastActivity) > lruEvictionMinIdleTime
		if iIdle != jIdle {
			return iIdle
		}

		// Among idle connections, evict oldest first
		return ci.lastActivity.Before(cj.lastActivity)
	})

	// Evict connections to reach target
	toEvict := connCount - (maxTotalConnections - lruEvictionTargetFree)
	if toEvict <= 0 {
		return true
	}
	if toEvict > len(candidates) {
		toEvict = len(candidates)
	}

	var evicted int
	for i := 0; i < toEvict && i < len(candidates); i++ {
		c := candidates[i]
		// Only evict closed or idle connections
		if !c.closed && (c.subCount > 0 || now.Sub(c.lastActivity) <= lruEvictionMinIdleTime) {
			continue
		}

		p.mu.Lock()
		if rc, exists := p.connections[c.url]; exists {
			delete(p.connections, c.url)
			if !rc.isClosed() {
				rc.markClosed()
			}
			evicted++
			slog.Debug("LRU evicted relay connection", "relay", c.url, "idle_time", now.Sub(c.lastActivity))
		}
		p.mu.Unlock()
	}

	slog.Info("LRU eviction completed", "evicted", evicted, "pool_size", connCount-evicted)
	return evicted > 0
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
