package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// isRelayURLSafe validates that a relay URL is safe to connect to
// Allows localhost for development but blocks other private IP ranges
func isRelayURLSafe(relayURL string) bool {
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return false
	}

	// Only allow ws:// and wss:// schemes
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return false
	}

	host := parsed.Hostname()
	if host == "" {
		return false
	}

	// Allow localhost for development
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	// Resolve hostname and check IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve, allow it (might be valid external host)
		// but block obvious internal names
		if len(host) > 0 && (host[len(host)-1] == '.' ||
			contains(host, ".local") || contains(host, ".internal")) {
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

// isRelayIPSafe checks if an IP is safe for relay connections
// Allows loopback (localhost) but blocks other private ranges
func isRelayIPSafe(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// Allow loopback (localhost)
	if ip.IsLoopback() {
		return true
	}

	// Block private networks (10.x, 172.16-31.x, 192.168.x)
	if ip.IsPrivate() {
		return false
	}

	// Block link-local (169.254.x.x)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// Block unspecified (0.0.0.0)
	if ip.IsUnspecified() {
		return false
	}

	// Block cloud metadata IP
	metadataIP := net.ParseIP("169.254.169.254")
	if ip.Equal(metadataIP) {
		return false
	}

	// Block multicast
	if ip.IsMulticast() {
		return false
	}

	return true
}

// contains checks if a string contains a substring (simple helper)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Subscription represents an active subscription on a relay connection
type Subscription struct {
	ID        string
	EventChan chan Event
	EOSEChan  chan bool
	Done      chan struct{}
	closeOnce sync.Once
}

// Close safely closes the Done channel exactly once
func (s *Subscription) Close() {
	s.closeOnce.Do(func() {
		close(s.Done)
	})
}

// RelayConn manages a single websocket connection with multiple subscriptions
type RelayConn struct {
	conn          *websocket.Conn
	relayURL      string
	mu            sync.Mutex
	writeMu       sync.Mutex
	subscriptions map[string]*Subscription
	closed        bool
	lastActivity  time.Time
}

// RelayPool manages connections to multiple relays
type RelayPool struct {
	mu          sync.RWMutex
	connections map[string]*RelayConn // relayURL -> connection
}

// Global relay pool
var relayPool = NewRelayPool()

// NewRelayPool creates a new connection pool
func NewRelayPool() *RelayPool {
	pool := &RelayPool{
		connections: make(map[string]*RelayConn),
	}
	go pool.cleanupLoop()
	return pool
}

// getOrCreateConn gets an existing connection or creates a new one
func (p *RelayPool) getOrCreateConn(ctx context.Context, relayURL string) (*RelayConn, error) {
	// Validate relay URL before connecting
	if !isRelayURLSafe(relayURL) {
		return nil, errors.New("relay URL blocked: unsafe destination")
	}

	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()

	if rc != nil && !rc.closed {
		return rc, nil
	}

	// Need to create a new connection
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	rc = p.connections[relayURL]
	if rc != nil && !rc.closed {
		return rc, nil
	}

	// Create new connection
	log.Printf("Pool: creating new connection to %s", relayURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return nil, err
	}

	rc = &RelayConn{
		conn:          conn,
		relayURL:      relayURL,
		subscriptions: make(map[string]*Subscription),
		lastActivity:  time.Now(),
	}

	p.connections[relayURL] = rc

	// Start the read loop for this connection
	go rc.readLoop()

	return rc, nil
}

// Subscribe creates a new subscription on the relay
func (p *RelayPool) Subscribe(ctx context.Context, relayURL string, subID string, filter map[string]interface{}) (*Subscription, error) {
	const maxRetries = 3
	var rc *RelayConn
	var err error
	var connected bool

	for attempt := 0; attempt < maxRetries; attempt++ {
		rc, err = p.getOrCreateConn(ctx, relayURL)
		if err != nil {
			return nil, err
		}

		// Check if connection is still valid
		rc.mu.Lock()
		if rc.closed {
			rc.mu.Unlock()
			// Connection was closed, remove and retry
			p.mu.Lock()
			delete(p.connections, relayURL)
			p.mu.Unlock()
			continue
		}
		connected = true
		break
	}

	if !connected {
		return nil, errors.New("failed to establish connection after retries")
	}

	sub := &Subscription{
		ID:        subID,
		EventChan: make(chan Event, 100),
		EOSEChan:  make(chan bool, 1),
		Done:      make(chan struct{}),
	}

	// Register subscription (rc.mu is already locked from the loop)
	rc.subscriptions[subID] = sub
	rc.mu.Unlock()

	// Send REQ
	req := []interface{}{"REQ", subID, filter}
	rc.writeMu.Lock()
	err = rc.conn.WriteJSON(req)
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

// Unsubscribe closes a subscription
func (p *RelayPool) Unsubscribe(relayURL string, sub *Subscription) {
	if sub == nil {
		return
	}

	p.mu.RLock()
	rc := p.connections[relayURL]
	p.mu.RUnlock()

	if rc == nil {
		return
	}

	// Check if we should send CLOSE and remove subscription
	rc.mu.Lock()
	_, exists := rc.subscriptions[sub.ID]
	shouldSendClose := !rc.closed && exists
	if exists {
		delete(rc.subscriptions, sub.ID)
	}
	rc.mu.Unlock()

	// Send CLOSE outside of mutex (best effort, connection may be closed)
	if shouldSendClose {
		closeMsg := []interface{}{"CLOSE", sub.ID}
		rc.writeMu.Lock()
		rc.conn.WriteJSON(closeMsg)
		rc.writeMu.Unlock()
	}

	// Signal done using thread-safe Close method
	sub.Close()
}

// readLoop continuously reads from the connection and routes messages
func (rc *RelayConn) readLoop() {
	defer rc.markClosed()

	for {
		var msg []interface{}
		err := rc.conn.ReadJSON(&msg)
		if err != nil {
			rc.mu.Lock()
			closed := rc.closed
			rc.mu.Unlock()
			if !closed {
				log.Printf("Pool: read error from %s: %v", rc.relayURL, err)
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

		case "CLOSED":
			// Subscription was closed by relay
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

		case "NOTICE":
			if len(msg) >= 2 {
				notice, _ := msg[1].(string)
				log.Printf("Pool: NOTICE from %s: %s", rc.relayURL, notice)
			}
		}
	}
}

// markClosed marks the connection as closed and cleans up
func (rc *RelayConn) markClosed() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.closed {
		return
	}

	rc.closed = true
	rc.conn.Close()

	// Close all subscription channels
	for _, sub := range rc.subscriptions {
		sub.Close()
	}
	rc.subscriptions = make(map[string]*Subscription)
}

// cleanupLoop periodically removes stale connections
func (p *RelayPool) cleanupLoop() {
	ticker := time.NewTicker(60 * time.Second)
	for range ticker.C {
		p.cleanup()
	}
}

// cleanup removes connections that have been idle too long
func (p *RelayPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for url, rc := range p.connections {
		rc.mu.Lock()
		idle := len(rc.subscriptions) == 0 && now.Sub(rc.lastActivity) > 2*time.Minute
		rc.mu.Unlock()

		if rc.closed || idle {
			if !rc.closed {
				log.Printf("Pool: closing idle connection to %s", url)
				rc.markClosed()
			}
			delete(p.connections, url)
		}
	}
}

// CloseRelay closes a specific relay connection
func (p *RelayPool) CloseRelay(relayURL string) {
	p.mu.Lock()
	rc := p.connections[relayURL]
	delete(p.connections, relayURL)
	p.mu.Unlock()

	if rc != nil {
		rc.markClosed()
	}
}

// PooledConn is a compatibility wrapper for code that expects the old interface
type PooledConn struct {
	pool     *RelayPool
	relayURL string
	sub      *Subscription
}

// Get returns a PooledConn for compatibility with existing code
func (p *RelayPool) Get(ctx context.Context, relayURL string) (*PooledConn, error) {
	// Just verify we can connect
	_, err := p.getOrCreateConn(ctx, relayURL)
	if err != nil {
		return nil, err
	}
	return &PooledConn{
		pool:     p,
		relayURL: relayURL,
	}, nil
}

// Release is a no-op for the new pool (subscriptions are explicitly unsubscribed)
func (p *RelayPool) Release(pc *PooledConn) {
	// No-op - subscriptions are closed via Unsubscribe
}

// Close marks connection as bad and removes it from pool
func (p *RelayPool) Close(pc *PooledConn) {
	if pc == nil {
		return
	}
	p.CloseRelay(pc.relayURL)
}

// WriteJSON sends a message on the connection with a timeout
func (pc *PooledConn) WriteJSON(v interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, err := pc.pool.getOrCreateConn(ctx, pc.relayURL)
	if err != nil {
		return err
	}
	rc.writeMu.Lock()
	defer rc.writeMu.Unlock()

	// Set write deadline to prevent indefinite blocking
	rc.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	defer rc.conn.SetWriteDeadline(time.Time{}) // Clear deadline after write

	return rc.conn.WriteJSON(v)
}

// ReadJSON is not supported - use Subscribe instead
func (pc *PooledConn) ReadJSON(v interface{}) error {
	return errors.New("ReadJSON not supported on pooled connections - use Subscribe")
}

// SetReadDeadline is not supported on pooled connections
func (pc *PooledConn) SetReadDeadline(t time.Time) error {
	return nil // No-op
}

// SetSubscriptionID is not used in the new implementation
func (pc *PooledConn) SetSubscriptionID(subID string) {
	// No-op
}
