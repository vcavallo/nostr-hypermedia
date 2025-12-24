package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/gorilla/websocket"
	"nostr-server/internal/config"
	"nostr-server/internal/nips"
)

// Rate limiting constants for NIP-46 operations
const (
	signRateLimit  = 10              // Max sign requests per window
	signRateWindow = 1 * time.Minute // Rate limit window
	loginRateLimit = 5               // Max login attempts per window

	// Stricter fallback limits for when primary rate limiter is unavailable
	fallbackLoginRateLimit = 3 // More restrictive for security-critical operations

	// NIP-46 relay connection settings
	nip46PingInterval   = 30 * time.Second // Send ping to keep connection alive
	nip46ReconnectDelay = 2 * time.Second  // Wait before reconnecting
	nip46RequestTimeout = 30 * time.Second // Timeout for individual requests
)

// fallbackRateLimiter is a simple in-memory rate limiter used when the primary
// rate limit store (Redis) is unavailable. This ensures rate limiting still works
// even if Redis is down, preventing complete bypass of rate limits.
var fallbackRateLimiter = newFallbackRateLimiter()

// Maximum number of rate limit buckets to prevent memory exhaustion
const maxRateLimitBuckets = 5000

type fallbackRateLimiterStore struct {
	mu      sync.Mutex
	buckets map[string]*fallbackBucket
	stopCh  chan struct{}
}

type fallbackBucket struct {
	count   int
	resetAt time.Time
}

func newFallbackRateLimiter() *fallbackRateLimiterStore {
	store := &fallbackRateLimiterStore{
		buckets: make(map[string]*fallbackBucket),
		stopCh:  make(chan struct{}),
	}
	// Start background cleanup goroutine
	go store.cleanupLoop()
	return store
}

func (f *fallbackRateLimiterStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
			f.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (f *fallbackRateLimiterStore) Close() {
	close(f.stopCh)
}

// Allow checks if a request is allowed under the rate limit.
// Returns true if allowed, false if rate limit exceeded.
func (f *fallbackRateLimiterStore) Allow(key string, limit int, window time.Duration) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	bucket, exists := f.buckets[key]

	// Create new bucket or reset expired bucket
	if !exists || now.After(bucket.resetAt) {
		// Enforce max bucket count to prevent memory exhaustion
		if !exists && len(f.buckets) >= maxRateLimitBuckets {
			// Too many buckets - run emergency cleanup
			f.cleanupLocked()
			// If still at limit after cleanup, reject to prevent memory growth
			if len(f.buckets) >= maxRateLimitBuckets {
				slog.Warn("rate limiter bucket limit reached, rejecting request")
				return false
			}
		}
		f.buckets[key] = &fallbackBucket{
			count:   1,
			resetAt: now.Add(window),
		}
		return true
	}

	// Check if within limit
	if bucket.count >= limit {
		return false
	}

	bucket.count++
	return true
}

// cleanup removes expired buckets (call periodically to prevent memory growth)
func (f *fallbackRateLimiterStore) cleanup() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupLocked()
}

// cleanupLocked removes expired buckets (caller must hold lock)
func (f *fallbackRateLimiterStore) cleanupLocked() {
	now := time.Now()
	for key, bucket := range f.buckets {
		if now.After(bucket.resetAt) {
			delete(f.buckets, key)
		}
	}
}

// nip46RelayConn manages a persistent WebSocket connection to a NIP-46 relay
type nip46RelayConn struct {
	url            string
	conn           *websocket.Conn
	session        *BunkerSession        // Back-reference to parent session
	pending        map[string]chan string // reqID -> response channel
	pendingMu      sync.Mutex
	connected      bool
	subID          string // Active subscription ID
	done           chan struct{}
	reconnecting   bool
	lastActivity   time.Time
}

// BunkerSession represents an active NIP-46 connection to a remote signer
type BunkerSession struct {
	ID                 string    // Session ID (for cookies)
	ClientPrivKey      []byte    // Disposable client private key
	ClientPubKey       []byte    // Client public key (hex)
	RemoteSignerPubKey []byte    // Remote signer's pubkey
	UserPubKey         []byte    // User's actual pubkey (from get_public_key)
	Relays             []string  // Relays to communicate through
	Secret             string    // Optional connection secret
	ConversationKey    []byte    // Cached conversation key
	Connected          bool
	CreatedAt          time.Time
	UserRelayList      *RelayList // User's NIP-65 relay list
	FollowingPubkeys   []string   // Cached list of followed pubkeys (from kind 3)
	BookmarkedEventIDs []string   // Cached list of bookmarked event IDs (from kind 10003)
	ReactedEventIDs    []string   // Cached list of event IDs the user has reacted to (from kind 7)
	RepostedEventIDs   []string   // Cached list of event IDs the user has reposted (from kind 6)
	ZappedEventIDs     []string   // Cached list of event IDs the user has zapped
	MutedPubkeys       []string   // Cached list of muted pubkeys (from kind 10000)
	MutedEventIDs      []string   // Cached list of muted event IDs (from kind 10000)
	MutedHashtags      []string   // Cached list of muted hashtags (from kind 10000)
	MutedWords         []string   // Cached list of muted words (from kind 10000)
	// NWC (Nostr Wallet Connect) for zaps
	NWCConfig          *NWCConfig // Wallet connection config (nil if no wallet connected)
	relayConns         map[string]*nip46RelayConn // Persistent relay connections
	relayConnsMu       sync.RWMutex
	closed             bool
	mu                 sync.Mutex
}

// IsEventBookmarked checks if an event ID is in the user's cached bookmarks
func (s *BunkerSession) IsEventBookmarked(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.BookmarkedEventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

// IsEventReacted checks if an event ID is in the user's cached reactions
func (s *BunkerSession) IsEventReacted(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.ReactedEventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

// IsEventReposted checks if an event ID is in the user's cached reposts
func (s *BunkerSession) IsEventReposted(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.RepostedEventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

// IsEventZapped checks if an event ID is in the user's cached zaps
func (s *BunkerSession) IsEventZapped(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.ZappedEventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

// AddZappedEvent adds an event ID to the user's zapped list
func (s *BunkerSession) AddZappedEvent(eventID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check if already in list
	for _, id := range s.ZappedEventIDs {
		if id == eventID {
			return
		}
	}
	s.ZappedEventIDs = append(s.ZappedEventIDs, eventID)
}

// IsPubkeyMuted checks if a pubkey is in the user's mute list
func (s *BunkerSession) IsPubkeyMuted(pubkey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pk := range s.MutedPubkeys {
		if pk == pubkey {
			return true
		}
	}
	return false
}

// IsEventMuted checks if an event ID is in the user's muted events
func (s *BunkerSession) IsEventMuted(eventID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.MutedEventIDs {
		if id == eventID {
			return true
		}
	}
	return false
}

// IsHashtagMuted checks if a hashtag is muted (case-insensitive)
func (s *BunkerSession) IsHashtagMuted(hashtag string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lowerTag := strings.ToLower(hashtag)
	for _, tag := range s.MutedHashtags {
		if strings.ToLower(tag) == lowerTag {
			return true
		}
	}
	return false
}

// ContainsMutedWord checks if content contains any muted words (case-insensitive)
func (s *BunkerSession) ContainsMutedWord(content string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lowerContent := strings.ToLower(content)
	for _, word := range s.MutedWords {
		if strings.Contains(lowerContent, strings.ToLower(word)) {
			return true
		}
	}
	return false
}

// IsEventFromMutedSource checks if an event should be filtered based on mute settings
// This checks pubkey, event ID, hashtags in tags, and muted words in content
func (s *BunkerSession) IsEventFromMutedSource(pubkey, eventID, content string, tags [][]string) bool {
	if s.IsPubkeyMuted(pubkey) {
		return true
	}
	if s.IsEventMuted(eventID) {
		return true
	}
	// Check hashtags in tags
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "t" {
			if s.IsHashtagMuted(tag[1]) {
				return true
			}
		}
	}
	// Check muted words in content
	if s.ContainsMutedWord(content) {
		return true
	}
	return false
}

// HasWallet checks if the session has a wallet connected for zaps
func (s *BunkerSession) HasWallet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.NWCConfig != nil
}

// GetWalletRelay returns the wallet relay URL if connected
func (s *BunkerSession) GetWalletRelay() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.NWCConfig != nil {
		return s.NWCConfig.Relay
	}
	return ""
}

// GetFollowingPubkeys returns a copy of the user's following list (thread-safe).
func (s *BunkerSession) GetFollowingPubkeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, len(s.FollowingPubkeys))
	copy(result, s.FollowingPubkeys)
	return result
}

// GetMutedPubkeys returns a deduplicated copy of the user's muted pubkeys list (thread-safe).
func (s *BunkerSession) GetMutedPubkeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := make(map[string]bool)
	result := make([]string, 0, len(s.MutedPubkeys))
	for _, pk := range s.MutedPubkeys {
		if !seen[pk] {
			seen[pk] = true
			result = append(result, pk)
		}
	}
	return result
}

// IsFollowing checks if a pubkey is in the user's following list (thread-safe).
func (s *BunkerSession) IsFollowing(pubkey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pk := range s.FollowingPubkeys {
		if pk == pubkey {
			return true
		}
	}
	return false
}

// SetNWCConfig sets the NWC wallet configuration
func (s *BunkerSession) SetNWCConfig(config *NWCConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NWCConfig = config
}

// ClearNWCConfig removes the wallet configuration
func (s *BunkerSession) ClearNWCConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.NWCConfig = nil
}

// checkSignRateLimit returns an error if the session has exceeded the sign rate limit
// Uses the centralized rate limit store which works with both memory and Redis backends.
// If the primary rate limiter fails, falls back to an in-memory limiter to maintain security.
func (s *BunkerSession) checkSignRateLimit() error {
	ctx := context.Background()
	key := "sign:" + s.ID

	// Check current rate
	allowed, remaining, err := rateLimitStore.Check(ctx, key, signRateLimit, signRateWindow)
	if err != nil {
		// Primary rate limiter failed - use fallback instead of failing open
		slog.Warn("rate limit check error, using fallback", "error", err)
		if !fallbackRateLimiter.Allow(key, signRateLimit, signRateWindow) {
			return errors.New("rate limit exceeded: too many sign requests (fallback limiter)")
		}
		return nil
	}

	if !allowed {
		return fmt.Errorf("rate limit exceeded: too many sign requests (remaining: %d)", remaining)
	}

	// Increment the counter
	if err := rateLimitStore.Increment(ctx, key, signRateWindow); err != nil {
		slog.Warn("rate limit increment error", "error", err)
	}

	return nil
}

// CheckLoginRateLimit checks if a login attempt is allowed for the given IP.
// Returns error if rate limit exceeded.
// Uses stricter fallback limits when primary rate limiter is unavailable.
func CheckLoginRateLimit(ip string) error {
	ctx := context.Background()
	key := "login:" + ip

	allowed, remaining, err := rateLimitStore.Check(ctx, key, loginRateLimit, signRateWindow)
	if err != nil {
		// Primary rate limiter failed - use stricter fallback for login security
		slog.Warn("login rate limit check error, using stricter fallback", "error", err, "security", true)
		if !fallbackRateLimiter.Allow(key, fallbackLoginRateLimit, signRateWindow) {
			return errors.New("rate limit exceeded: too many login attempts (fallback limiter)")
		}
		return nil
	}

	if !allowed {
		return fmt.Errorf("rate limit exceeded: too many login attempts (remaining: %d)", remaining)
	}

	// Increment the counter
	if err := rateLimitStore.Increment(ctx, key, signRateWindow); err != nil {
		slog.Warn("login rate limit increment error", "error", err)
	}

	return nil
}

// bunkerSessions provides backward-compatible access to session store
// This wraps the sessionStore interface for existing code
var bunkerSessions = &bunkerSessionsWrapper{}

// NIP46Request is a JSON-RPC request to the remote signer
type NIP46Request struct {
	ID     string   `json:"id"`
	Method string   `json:"method"`
	Params []string `json:"params"`
}

// NIP46Response is a JSON-RPC response from the remote signer
type NIP46Response struct {
	ID     string `json:"id"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// UnsignedEvent is an event that needs to be signed
type UnsignedEvent struct {
	Kind      int        `json:"kind"`
	Content   string     `json:"content"`
	Tags      [][]string `json:"tags"`
	CreatedAt int64      `json:"created_at"`
}

// ParseBunkerURL parses a bunker:// URL into session parameters
func ParseBunkerURL(bunkerURL string) (*BunkerSession, error) {
	// bunker://<remote-signer-pubkey>?relay=<wss://...>&relay=<wss://...>&secret=<optional>
	if !strings.HasPrefix(bunkerURL, "bunker://") {
		return nil, errors.New("invalid bunker URL: must start with bunker://")
	}

	u, err := url.Parse(bunkerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid bunker URL: %v", err)
	}

	// Extract remote signer pubkey from host
	remoteSignerPubKeyHex := u.Host
	if len(remoteSignerPubKeyHex) != 64 {
		return nil, errors.New("invalid remote signer pubkey in bunker URL")
	}
	remoteSignerPubKey, err := hex.DecodeString(remoteSignerPubKeyHex)
	if err != nil {
		return nil, errors.New("invalid remote signer pubkey hex")
	}

	// Extract relays
	relays := u.Query()["relay"]
	if len(relays) == 0 {
		return nil, errors.New("bunker URL must specify at least one relay")
	}

	// Extract optional secret
	secret := u.Query().Get("secret")

	// Generate disposable client keypair
	clientPrivKey, err := nips.GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client keypair: %v", err)
	}
	clientPubKey, err := nips.GetPublicKey(clientPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive public key: %v", err)
	}

	// Pre-compute conversation key
	conversationKey, err := nips.GetConversationKey(clientPrivKey, remoteSignerPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute conversation key: %v", err)
	}

	// Generate session ID
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %v", err)
	}

	return &BunkerSession{
		ID:                 sessionID,
		ClientPrivKey:      clientPrivKey,
		ClientPubKey:       clientPubKey,
		RemoteSignerPubKey: remoteSignerPubKey,
		Relays:             relays,
		Secret:             secret,
		ConversationKey:    conversationKey,
		Connected:          false,
		CreatedAt:          time.Now(),
	}, nil
}

func generateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %v", err)
	}
	return hex.EncodeToString(b), nil
}

// Connect establishes a connection with the remote signer
func (s *BunkerSession) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Build connect params
	params := []string{hex.EncodeToString(s.RemoteSignerPubKey)}
	if s.Secret != "" {
		params = append(params, s.Secret)
	}

	// Send connect request
	result, err := s.sendRequest(ctx, "connect", params)
	if err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}

	// Verify response (should be "ack" or the secret)
	if result != "ack" && result != s.Secret {
		return fmt.Errorf("unexpected connect response: %s", result)
	}

	// Get user's actual public key
	userPubKeyHex, err := s.sendRequest(ctx, "get_public_key", []string{})
	if err != nil {
		return fmt.Errorf("get_public_key failed: %v", err)
	}

	userPubKey, err := hex.DecodeString(userPubKeyHex)
	if err != nil {
		return fmt.Errorf("invalid user pubkey: %v", err)
	}

	s.UserPubKey = userPubKey
	s.Connected = true

	slog.Info("NIP-46: connected to bunker", "user_pubkey", userPubKeyHex)

	// Fetch user's NIP-65 relay list in background
	go func() {
		relayList := fetchRelayList(userPubKeyHex)
		s.mu.Lock()
		s.UserRelayList = relayList
		s.mu.Unlock()
	}()

	// Fetch user's profile in background (for avatar in settings toggle)
	go func() {
		fetchProfilesWithOptions(config.GetProfileRelays(), []string{userPubKeyHex}, false)
	}()

	return nil
}

// SignEvent requests the remote signer to sign an event
func (s *BunkerSession) SignEvent(ctx context.Context, event UnsignedEvent) (*Event, error) {
	// Check connection state under lock (brief hold)
	s.mu.Lock()
	if !s.Connected {
		s.mu.Unlock()
		return nil, errors.New("not connected to bunker")
	}
	s.mu.Unlock()

	// Check rate limit (uses immutable s.ID and thread-safe store)
	if err := s.checkSignRateLimit(); err != nil {
		return nil, err
	}

	// Serialize event for signing
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

	// Network call without holding lock (all accessed fields are immutable after connection)
	result, err := s.sendRequest(ctx, "sign_event", []string{string(eventJSON)})
	if err != nil {
		return nil, fmt.Errorf("sign_event failed: %v", err)
	}

	// Parse signed event
	var signedEvent Event
	if err := json.Unmarshal([]byte(result), &signedEvent); err != nil {
		return nil, fmt.Errorf("failed to parse signed event: %v", err)
	}

	return &signedEvent, nil
}

// getOrCreateRelayConn gets an existing relay connection or creates a new one
func (s *BunkerSession) getOrCreateRelayConn(ctx context.Context, relayURL string) (*nip46RelayConn, error) {
	s.relayConnsMu.RLock()
	if s.closed {
		s.relayConnsMu.RUnlock()
		return nil, errors.New("session closed")
	}
	rc, exists := s.relayConns[relayURL]
	s.relayConnsMu.RUnlock()

	if exists && rc.connected {
		return rc, nil
	}

	// Need to create or reconnect
	s.relayConnsMu.Lock()
	defer s.relayConnsMu.Unlock()

	if s.closed {
		return nil, errors.New("session closed")
	}

	// Double-check after acquiring write lock
	if rc, exists := s.relayConns[relayURL]; exists && rc.connected {
		return rc, nil
	}

	// Initialize map if needed
	if s.relayConns == nil {
		s.relayConns = make(map[string]*nip46RelayConn)
	}

	// Create new connection
	rc = &nip46RelayConn{
		url:          relayURL,
		session:      s,
		pending:      make(map[string]chan string),
		done:         make(chan struct{}),
		lastActivity: time.Now(),
	}

	if err := rc.connect(ctx); err != nil {
		return nil, err
	}

	s.relayConns[relayURL] = rc
	return rc, nil
}

// connect establishes the WebSocket connection and starts the reader goroutine
func (rc *nip46RelayConn) connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, rc.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", rc.url, err)
	}

	rc.conn = conn
	rc.connected = true
	rc.lastActivity = time.Now()

	// Subscribe to responses (kind 24133 p-tagged to our pubkey)
	subIDSuffix, err := generateSessionID()
	if err != nil {
		conn.Close()
		return fmt.Errorf("failed to generate subscription ID: %v", err)
	}
	rc.subID = "nip46-" + subIDSuffix[:8]
	subFilter := map[string]interface{}{
		"kinds": []int{24133},
		"#p":    []string{hex.EncodeToString(rc.session.ClientPubKey)},
		"since": time.Now().Unix() - 10,
	}
	subReq := []interface{}{"REQ", rc.subID, subFilter}
	if err := conn.WriteJSON(subReq); err != nil {
		conn.Close()
		return fmt.Errorf("failed to subscribe: %v", err)
	}

	slog.Debug("NIP-46: connected to relay", "relay", rc.url, "subID", rc.subID)

	// Start background reader
	go rc.readLoop()

	// Start ping loop to keep connection alive
	go rc.pingLoop()

	return nil
}

// readLoop reads messages from the relay and routes responses to waiting requests
func (rc *nip46RelayConn) readLoop() {
	defer func() {
		rc.connected = false
		if rc.conn != nil {
			rc.conn.Close()
		}
		// Notify all pending requests of failure
		rc.pendingMu.Lock()
		for reqID, ch := range rc.pending {
			close(ch)
			delete(rc.pending, reqID)
		}
		rc.pendingMu.Unlock()
	}()

	for {
		select {
		case <-rc.done:
			return
		default:
			var msg []interface{}
			if err := rc.conn.ReadJSON(&msg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					slog.Debug("NIP-46: relay connection closed", "relay", rc.url, "error", err)
				}
				// Attempt reconnect in background
				go rc.scheduleReconnect()
				return
			}

			rc.lastActivity = time.Now()

			if len(msg) < 2 {
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				continue
			}

			switch msgType {
			case "EVENT":
				rc.handleEvent(msg)
			case "OK":
				// Event was accepted, nothing to do
			case "NOTICE":
				if len(msg) >= 2 {
					slog.Debug("NIP-46: relay notice", "relay", rc.url, "message", msg[1])
				}
			case "CLOSED":
				slog.Debug("NIP-46: subscription closed by relay", "relay", rc.url)
				go rc.scheduleReconnect()
				return
			}
		}
	}
}

// handleEvent processes an incoming EVENT message
func (rc *nip46RelayConn) handleEvent(msg []interface{}) {
	if len(msg) < 3 {
		return
	}

	eventData, err := json.Marshal(msg[2])
	if err != nil {
		return
	}

	var responseEvent Event
	if err := json.Unmarshal(eventData, &responseEvent); err != nil {
		return
	}

	// Verify it's from the remote signer
	if responseEvent.PubKey != hex.EncodeToString(rc.session.RemoteSignerPubKey) {
		return
	}

	// Decrypt response
	decrypted, err := nips.Nip44Decrypt(responseEvent.Content, rc.session.ConversationKey)
	if err != nil {
		slog.Error("NIP-46: failed to decrypt response", "relay", rc.url, "error", err)
		return
	}

	var response NIP46Response
	if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
		slog.Error("NIP-46: failed to parse response", "relay", rc.url, "error", err)
		return
	}

	// Route to waiting request
	rc.pendingMu.Lock()
	ch, exists := rc.pending[response.ID]
	if exists {
		delete(rc.pending, response.ID)
	}
	rc.pendingMu.Unlock()

	if exists {
		if response.Error != "" {
			// Send error as special value (will be detected by caller)
			ch <- "ERROR:" + response.Error
		} else {
			ch <- response.Result
		}
	}
}

// pingLoop sends periodic pings to keep the connection alive
func (rc *nip46RelayConn) pingLoop() {
	ticker := time.NewTicker(nip46PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-rc.done:
			return
		case <-ticker.C:
			if !rc.connected || rc.conn == nil {
				return
			}
			if err := rc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				slog.Debug("NIP-46: ping failed", "relay", rc.url, "error", err)
				return
			}
		}
	}
}

// scheduleReconnect attempts to reconnect after a delay
func (rc *nip46RelayConn) scheduleReconnect() {
	rc.pendingMu.Lock()
	if rc.reconnecting {
		rc.pendingMu.Unlock()
		return
	}
	rc.reconnecting = true
	rc.pendingMu.Unlock()

	defer func() {
		rc.pendingMu.Lock()
		rc.reconnecting = false
		rc.pendingMu.Unlock()
	}()

	select {
	case <-rc.done:
		return
	case <-time.After(nip46ReconnectDelay):
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := rc.connect(ctx); err != nil {
		slog.Debug("NIP-46: reconnect failed", "relay", rc.url, "error", err)
	} else {
		slog.Debug("NIP-46: reconnected", "relay", rc.url)
	}
}

// sendEvent sends an event through this relay connection and waits for response
func (rc *nip46RelayConn) sendEvent(ctx context.Context, event *Event, reqID string) (string, error) {
	if !rc.connected || rc.conn == nil {
		return "", errors.New("not connected")
	}

	// Create response channel
	respCh := make(chan string, 1)
	rc.pendingMu.Lock()
	rc.pending[reqID] = respCh
	rc.pendingMu.Unlock()

	// Cleanup on exit
	defer func() {
		rc.pendingMu.Lock()
		delete(rc.pending, reqID)
		rc.pendingMu.Unlock()
	}()

	// Publish request event
	pubReq := []interface{}{"EVENT", event}
	if err := rc.conn.WriteJSON(pubReq); err != nil {
		return "", fmt.Errorf("failed to publish: %v", err)
	}

	// Wait for response with timeout
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result, ok := <-respCh:
		if !ok {
			return "", errors.New("connection closed")
		}
		// Check for error response
		if strings.HasPrefix(result, "ERROR:") {
			return "", errors.New(strings.TrimPrefix(result, "ERROR:"))
		}
		return result, nil
	}
}

// Close closes the relay connection
func (rc *nip46RelayConn) Close() {
	select {
	case <-rc.done:
		return // Already closed
	default:
		close(rc.done)
	}
	if rc.conn != nil {
		rc.conn.Close()
	}
}

// CloseRelayConns closes all persistent relay connections for this session
func (s *BunkerSession) CloseRelayConns() {
	s.relayConnsMu.Lock()
	defer s.relayConnsMu.Unlock()

	s.closed = true
	for _, rc := range s.relayConns {
		rc.Close()
	}
	s.relayConns = nil
	slog.Debug("NIP-46: closed all relay connections", "session", s.ID[:8])
}

// sendRequest sends a NIP-46 request and waits for response using persistent connections
func (s *BunkerSession) sendRequest(ctx context.Context, method string, params []string) (string, error) {
	// Generate request ID
	reqIDBytes := make([]byte, 8)
	if _, err := rand.Read(reqIDBytes); err != nil {
		return "", fmt.Errorf("failed to generate request ID: %v", err)
	}
	reqID := hex.EncodeToString(reqIDBytes)

	// Build request
	request := NIP46Request{
		ID:     reqID,
		Method: method,
		Params: params,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	// Encrypt with NIP-44
	encryptedContent, err := nips.Nip44Encrypt(string(requestJSON), s.ConversationKey)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %v", err)
	}

	// Create kind 24133 event
	requestEvent := createNIP46Event(s.ClientPrivKey, s.ClientPubKey, s.RemoteSignerPubKey, encryptedContent)

	// Create context with timeout for the request
	reqCtx, cancel := context.WithTimeout(ctx, nip46RequestTimeout)
	defer cancel()

	// Try each relay until we get a response
	var lastErr error
	for _, relayURL := range s.Relays {
		// Get or create persistent connection
		rc, err := s.getOrCreateRelayConn(reqCtx, relayURL)
		if err != nil {
			slog.Debug("NIP-46: failed to get relay connection", "relay", relayURL, "error", err)
			lastErr = err
			continue
		}

		// Send through persistent connection
		result, err := rc.sendEvent(reqCtx, requestEvent, reqID)
		if err != nil {
			slog.Debug("NIP-46: relay request failed", "relay", relayURL, "error", err)
			lastErr = err
			continue
		}
		return result, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("all relays failed: %v", lastErr)
	}
	return "", errors.New("all relays failed")
}

// createNIP46Event creates a signed kind 24133 event
func createNIP46Event(privKey, pubKey, targetPubKey []byte, content string) *Event {
	now := time.Now().Unix()

	event := &Event{
		PubKey:    hex.EncodeToString(pubKey),
		CreatedAt: now,
		Kind:      24133,
		Tags:      [][]string{{"p", hex.EncodeToString(targetPubKey)}},
		Content:   content,
	}

	// Calculate event ID
	event.ID = calculateEventID(event)

	// Sign the event
	event.Sig = signEvent(privKey, event.ID)

	return event
}

func calculateEventID(event *Event) string {
	// [0, pubkey, created_at, kind, tags, content]
	serialized := fmt.Sprintf(`[0,"%s",%d,%d,%s,"%s"]`,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		mustJSON(event.Tags),
		escapeJSON(event.Content),
	)

	hash := sha256.Sum256([]byte(serialized))
	return hex.EncodeToString(hash[:])
}

func signEvent(privKeyBytes []byte, eventID string) string {
	if len(privKeyBytes) == 0 {
		slog.Error("failed to sign event: empty private key")
		return ""
	}

	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	if privKey == nil {
		slog.Error("failed to sign event: invalid private key")
		return ""
	}

	eventIDBytes, err := hex.DecodeString(eventID)
	if err != nil {
		slog.Error("failed to sign event: invalid event ID hex", "error", err)
		return ""
	}

	sig, err := schnorr.Sign(privKey, eventIDBytes)
	if err != nil {
		slog.Error("failed to sign event", "error", err)
		return ""
	}

	return hex.EncodeToString(sig.Serialize())
}

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func escapeJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil || len(b) < 2 {
		// Fallback: return original string (shouldn't happen for valid strings)
		return s
	}
	// Remove surrounding quotes
	return string(b[1 : len(b)-1])
}

// bunkerSessionsWrapper provides backward-compatible access to session store
type bunkerSessionsWrapper struct{}

func (w *bunkerSessionsWrapper) Get(sessionID string) *BunkerSession {
	session, _ := sessionStore.Get(context.Background(), sessionID)
	return session
}

func (w *bunkerSessionsWrapper) Set(session *BunkerSession) {
	sessionStore.Set(context.Background(), session)
}

func (w *bunkerSessionsWrapper) Delete(sessionID string) {
	sessionStore.Delete(context.Background(), sessionID)
}
