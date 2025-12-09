package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/gorilla/websocket"
)

// Rate limiting constants for NIP-46 operations
const (
	signRateLimit    = 10              // Max sign requests per window
	signRateWindow   = 1 * time.Minute // Rate limit window
)

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
	// Rate limiting for sign operations
	signRequestTimes []time.Time
	mu               sync.Mutex
}

// checkSignRateLimit returns an error if the session has exceeded the sign rate limit
func (s *BunkerSession) checkSignRateLimit() error {
	now := time.Now()
	cutoff := now.Add(-signRateWindow)

	// Remove old entries
	validTimes := make([]time.Time, 0, len(s.signRequestTimes))
	for _, t := range s.signRequestTimes {
		if t.After(cutoff) {
			validTimes = append(validTimes, t)
		}
	}
	s.signRequestTimes = validTimes

	// Check if we're over the limit
	if len(s.signRequestTimes) >= signRateLimit {
		return errors.New("rate limit exceeded: too many sign requests")
	}

	// Record this request
	s.signRequestTimes = append(s.signRequestTimes, now)
	return nil
}

// BunkerSessionStore manages active bunker sessions
type BunkerSessionStore struct {
	sessions map[string]*BunkerSession
	mu       sync.RWMutex
}

// Global session store
var bunkerSessions = &BunkerSessionStore{
	sessions: make(map[string]*BunkerSession),
}

// Session cleanup interval
const sessionCleanupInterval = 10 * time.Minute

func init() {
	// Start session cleanup goroutine
	// Uses sessionMaxAge from html_auth.go (24 hours)
	go func() {
		ticker := time.NewTicker(sessionCleanupInterval)
		for range ticker.C {
			bunkerSessions.CleanupExpired(sessionMaxAge)
		}
	}()
}

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
	clientPrivKey, err := GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client keypair: %v", err)
	}
	clientPubKey, err := GetPublicKey(clientPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive public key: %v", err)
	}

	// Pre-compute conversation key
	conversationKey, err := GetConversationKey(clientPrivKey, remoteSignerPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute conversation key: %v", err)
	}

	// Generate session ID
	sessionID := generateSessionID()

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

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: use timestamp-based ID (less random but functional)
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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

	log.Printf("NIP-46: Connected to bunker, user pubkey: %s", userPubKeyHex)

	// Fetch user's NIP-65 relay list in background
	go func() {
		relayList := fetchRelayList(userPubKeyHex)
		s.mu.Lock()
		s.UserRelayList = relayList
		s.mu.Unlock()
	}()

	return nil
}

// SignEvent requests the remote signer to sign an event
func (s *BunkerSession) SignEvent(ctx context.Context, event UnsignedEvent) (*Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Connected {
		return nil, errors.New("not connected to bunker")
	}

	// Check rate limit
	if err := s.checkSignRateLimit(); err != nil {
		return nil, err
	}

	// Serialize event for signing
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}

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

// sendRequest sends a NIP-46 request and waits for response
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
	encryptedContent, err := Nip44Encrypt(string(requestJSON), s.ConversationKey)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %v", err)
	}

	// Create kind 24133 event
	requestEvent := createNIP46Event(s.ClientPrivKey, s.ClientPubKey, s.RemoteSignerPubKey, encryptedContent)

	// Try each relay until we get a response
	for _, relay := range s.Relays {
		result, err := s.sendToRelay(ctx, relay, requestEvent, reqID)
		if err != nil {
			log.Printf("NIP-46: Relay %s failed: %v", relay, err)
			continue
		}
		return result, nil
	}

	return "", errors.New("all relays failed")
}

func (s *BunkerSession) sendToRelay(ctx context.Context, relayURL string, event *Event, expectedReqID string) (string, error) {
	// Connect to relay
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to responses (kind 24133 p-tagged to our pubkey)
	subID := "nip46-" + generateSessionID()[:8]
	subFilter := map[string]interface{}{
		"kinds": []int{24133},
		"#p":    []string{hex.EncodeToString(s.ClientPubKey)},
		"since": time.Now().Unix() - 10,
	}
	subReq := []interface{}{"REQ", subID, subFilter}
	if err := conn.WriteJSON(subReq); err != nil {
		return "", fmt.Errorf("failed to subscribe: %v", err)
	}

	// Publish request event
	pubReq := []interface{}{"EVENT", event}
	if err := conn.WriteJSON(pubReq); err != nil {
		return "", fmt.Errorf("failed to publish: %v", err)
	}

	// Wait for response with timeout
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout:
			return "", errors.New("timeout waiting for response")
		default:
			var msg []interface{}
			if err := conn.ReadJSON(&msg); err != nil {
				return "", fmt.Errorf("read error: %v", err)
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
				if len(msg) < 3 {
					continue
				}
				eventData, err := json.Marshal(msg[2])
				if err != nil {
					continue
				}
				var responseEvent Event
				if err := json.Unmarshal(eventData, &responseEvent); err != nil {
					continue
				}

				// Verify it's from the remote signer
				if responseEvent.PubKey != hex.EncodeToString(s.RemoteSignerPubKey) {
					continue
				}

				// Decrypt response
				decrypted, err := Nip44Decrypt(responseEvent.Content, s.ConversationKey)
				if err != nil {
					log.Printf("NIP-46: Failed to decrypt response: %v", err)
					continue
				}

				var response NIP46Response
				if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
					log.Printf("NIP-46: Failed to parse response: %v", err)
					continue
				}

				// Check if this is our response
				if response.ID != expectedReqID {
					continue
				}

				if response.Error != "" {
					return "", errors.New(response.Error)
				}

				return response.Result, nil

			case "OK":
				// Event was accepted, continue waiting for response
				continue

			case "NOTICE":
				if len(msg) >= 2 {
					log.Printf("NIP-46: Relay notice: %v", msg[1])
				}
			}
		}
	}
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
		log.Printf("Failed to sign event: empty private key")
		return ""
	}

	privKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	if privKey == nil {
		log.Printf("Failed to sign event: invalid private key")
		return ""
	}

	eventIDBytes, err := hex.DecodeString(eventID)
	if err != nil {
		log.Printf("Failed to sign event: invalid event ID hex: %v", err)
		return ""
	}

	sig, err := schnorr.Sign(privKey, eventIDBytes)
	if err != nil {
		log.Printf("Failed to sign event: %v", err)
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

// Session store methods

func (store *BunkerSessionStore) Get(sessionID string) *BunkerSession {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.sessions[sessionID]
}

func (store *BunkerSessionStore) Set(session *BunkerSession) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions[session.ID] = session
}

func (store *BunkerSessionStore) Delete(sessionID string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, sessionID)
}

// CleanupExpired removes sessions older than the given duration
func (store *BunkerSessionStore) CleanupExpired(maxAge time.Duration) {
	store.mu.Lock()
	defer store.mu.Unlock()

	now := time.Now()
	for id, session := range store.sessions {
		if now.Sub(session.CreatedAt) > maxAge {
			delete(store.sessions, id)
		}
	}
}
