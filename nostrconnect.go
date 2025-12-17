package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ServerKeypair holds the persistent server keypair for NIP-46
type ServerKeypair struct {
	PrivKey []byte
	PubKey  []byte
}

var (
	serverKeypair     *ServerKeypair
	serverKeypairOnce sync.Once

	// pendingConnections provides backward-compatible access to pending connection store
	pendingConnections = &pendingConnectionsWrapper{}
)

const devKeypairFile = ".dev-keypair"

// PendingConnection represents a nostrconnect:// session waiting for signer approval
type PendingConnection struct {
	Secret          string    // Random secret we're expecting back
	ClientPrivKey   []byte    // Server's keypair for this connection
	ClientPubKey    []byte    // Server's public key
	Relays          []string  // Relays to listen on
	ConversationKey []byte    // Will be set when signer responds
	CreatedAt       time.Time
	// These get filled in when signer responds
	RemoteSignerPubKey []byte
	UserPubKey         []byte
	Connected          bool
	UserRelayList      *RelayList // User's NIP-65 relay list
	mu                 sync.Mutex
}

// pendingConnectionsWrapper provides backward-compatible access to pending connection store
type pendingConnectionsWrapper struct{}

func (w *pendingConnectionsWrapper) Get(secret string) *PendingConnection {
	conn, _ := pendingConnStore.Get(context.Background(), secret)
	return conn
}

func (w *pendingConnectionsWrapper) Set(secret string, conn *PendingConnection) {
	pendingConnStore.Set(context.Background(), secret, conn)
}

func (w *pendingConnectionsWrapper) Delete(secret string) {
	pendingConnStore.Delete(context.Background(), secret)
}

// GetServerKeypair returns the persistent server keypair, loading or generating as needed
func GetServerKeypair() (*ServerKeypair, error) {
	var err error
	serverKeypairOnce.Do(func() {
		serverKeypair, err = loadOrCreateKeypair()
	})
	return serverKeypair, err
}

func loadOrCreateKeypair() (*ServerKeypair, error) {
	devMode := os.Getenv("DEV_MODE") == "1"

	if devMode {
		// Try to load from file
		if data, err := os.ReadFile(devKeypairFile); err == nil {
			privKey, decodeErr := hex.DecodeString(string(data))
			if decodeErr == nil && len(privKey) == 32 {
				pubKey, pkErr := GetPublicKey(privKey)
				if pkErr == nil {
					slog.Info("NIP-46: loaded persistent dev keypair", "pubkey", hex.EncodeToString(pubKey))
					return &ServerKeypair{PrivKey: privKey, PubKey: pubKey}, nil
				}
			}
		}
	}

	// Generate new keypair
	privKey, err := GeneratePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate keypair: %v", err)
	}
	pubKey, err := GetPublicKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("failed to derive pubkey: %v", err)
	}

	if devMode {
		// Save for next time
		if err := os.WriteFile(devKeypairFile, []byte(hex.EncodeToString(privKey)), 0600); err != nil {
			slog.Warn("failed to save dev keypair", "error", err)
		} else {
			slog.Info("NIP-46: created and saved new dev keypair", "pubkey", hex.EncodeToString(pubKey))
		}
	} else {
		slog.Info("NIP-46: generated ephemeral keypair", "pubkey", hex.EncodeToString(pubKey))
	}

	return &ServerKeypair{PrivKey: privKey, PubKey: pubKey}, nil
}

// GenerateNostrConnectURL creates a nostrconnect:// URL for the user to paste into their signer
func GenerateNostrConnectURL(relays []string) (string, string, error) {
	kp, err := GetServerKeypair()
	if err != nil {
		return "", "", err
	}

	// Generate random secret
	secretBytes := make([]byte, 16)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", err
	}
	secret := hex.EncodeToString(secretBytes)

	// Build nostrconnect:// URL
	// nostrconnect://<client-pubkey>?relay=<relay>&secret=<secret>&name=<name>
	u := url.URL{
		Scheme: "nostrconnect",
		Host:   hex.EncodeToString(kp.PubKey),
	}
	q := u.Query()
	for _, relay := range relays {
		q.Add("relay", relay)
	}
	q.Set("secret", secret)
	q.Set("name", "Nostr Hypermedia")
	q.Set("perms", "sign_event:1") // Only need to sign kind 1 (notes)
	u.RawQuery = q.Encode()

	// Create pending connection
	pending := &PendingConnection{
		Secret:        secret,
		ClientPrivKey: kp.PrivKey,
		ClientPubKey:  kp.PubKey,
		Relays:        relays,
		CreatedAt:     time.Now(),
	}
	pendingConnections.Set(secret, pending)

	return u.String(), secret, nil
}

// StartConnectionListener starts listening for signer responses on relays
func StartConnectionListener(relays []string) {
	kp, err := GetServerKeypair()
	if err != nil {
		slog.Error("NIP-46: failed to get server keypair for listener", "error", err)
		return
	}

	for _, relay := range relays {
		go listenForConnections(relay, kp)
	}
}

func listenForConnections(relayURL string, kp *ServerKeypair) {
	for {
		err := listenOnRelay(relayURL, kp)
		if err != nil {
			slog.Warn("NIP-46: relay listener error, reconnecting", "relay", relayURL, "error", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func listenOnRelay(relayURL string, kp *ServerKeypair) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}
	defer conn.Close()

	// Subscribe to kind 24133 events p-tagged to our pubkey
	subID := "nc-listener"
	subFilter := map[string]interface{}{
		"kinds": []int{24133},
		"#p":    []string{hex.EncodeToString(kp.PubKey)},
		"since": time.Now().Unix() - 60, // Last minute
	}
	subReq := []interface{}{"REQ", subID, subFilter}
	if err := conn.WriteJSON(subReq); err != nil {
		return fmt.Errorf("subscribe failed: %v", err)
	}

	slog.Info("NIP-46: listening for connections", "relay", relayURL)

	// Set up pong handler to extend read deadline when server responds to our pings
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	// Start ping goroutine to keep connection alive
	// Ping interval must be shorter than read deadline to prevent timeouts on idle connections
	pingDone := make(chan struct{})
	var writeMu sync.Mutex
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-pingDone:
				return
			case <-ticker.C:
				writeMu.Lock()
				err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second))
				writeMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()
	defer close(pingDone)

	// Set initial read deadline (longer than ping interval to allow keepalive to work)
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	// Read loop
	for {
		var msg []interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read error: %v", err)
		}
		// Extend deadline after successful read
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		if len(msg) < 2 {
			continue
		}

		msgType, ok := msg[0].(string)
		if !ok {
			continue
		}

		if msgType == "EVENT" && len(msg) >= 3 {
			eventData, err := json.Marshal(msg[2])
			if err != nil {
				continue
			}
			var event Event
			if err := json.Unmarshal(eventData, &event); err != nil {
				continue
			}

			go handlePotentialConnectResponse(event, kp)
		}
	}
}

func handlePotentialConnectResponse(event Event, kp *ServerKeypair) {
	// Decode the remote signer's pubkey
	remoteSignerPubKey, err := hex.DecodeString(event.PubKey)
	if err != nil {
		return
	}

	// Compute conversation key
	convKey, err := GetConversationKey(kp.PrivKey, remoteSignerPubKey)
	if err != nil {
		slog.Error("NIP-46: failed to compute conversation key", "error", err)
		return
	}

	// Try to decrypt
	decrypted, err := Nip44Decrypt(event.Content, convKey)
	if err != nil {
		// Not for us or wrong key
		return
	}

	// Parse response
	var response NIP46Response
	if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
		slog.Error("NIP-46: failed to parse response", "error", err)
		return
	}

	// Check if this is a connect response with a secret we're expecting
	if response.Result == "" {
		return
	}

	// The result should be our secret
	pending := pendingConnections.Get(response.Result)
	if pending == nil {
		// Also check if it's "ack" for a secret-based response
		// The secret might be in response.ID
		return
	}

	// Check if already connected (avoid duplicate processing from multiple relays)
	pending.mu.Lock()
	if pending.Connected {
		pending.mu.Unlock()
		return
	}

	slog.Info("NIP-46: received connect response", "pubkey", event.PubKey[:16])

	// Update pending connection
	pending.RemoteSignerPubKey = remoteSignerPubKey
	pending.ConversationKey = convKey
	pending.Connected = true
	pending.mu.Unlock()

	// Persist to store (important for Redis)
	pendingConnections.Set(response.Result, pending)

	// Now get the user's public key
	go fetchUserPubKey(pending, event.PubKey, response.Result)
}

func fetchUserPubKey(pending *PendingConnection, remoteSignerPubKeyHex string, secret string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Send get_public_key request
	reqIDBytes := make([]byte, 8)
	if _, err := rand.Read(reqIDBytes); err != nil {
		slog.Error("NIP-46: failed to generate request ID", "error", err)
		return
	}
	reqID := hex.EncodeToString(reqIDBytes)

	request := NIP46Request{
		ID:     reqID,
		Method: "get_public_key",
		Params: []string{},
	}

	requestJSON, _ := json.Marshal(request)
	encryptedContent, err := Nip44Encrypt(string(requestJSON), pending.ConversationKey)
	if err != nil {
		slog.Error("NIP-46: failed to encrypt get_public_key request", "error", err)
		return
	}

	requestEvent := createNIP46Event(pending.ClientPrivKey, pending.ClientPubKey, pending.RemoteSignerPubKey, encryptedContent)

	// Send to relay and wait for response
	for _, relay := range pending.Relays {
		userPubKey, err := sendAndWaitForResponse(ctx, relay, requestEvent, reqID, pending.ConversationKey, remoteSignerPubKeyHex)
		if err != nil {
			slog.Debug("NIP-46: get_public_key failed", "relay", relay, "error", err)
			continue
		}

		userPubKeyBytes, err := hex.DecodeString(userPubKey)
		if err != nil {
			slog.Error("NIP-46: invalid user pubkey", "error", err)
			continue
		}

		pending.mu.Lock()
		pending.UserPubKey = userPubKeyBytes
		pending.mu.Unlock()
		slog.Info("NIP-46: got user pubkey", "pubkey", userPubKey)

		// Persist to store (important for Redis)
		pendingConnections.Set(secret, pending)

		// Fetch user's NIP-65 relay list in background
		go func(pubkeyHex string, secret string) {
			relayList := fetchRelayList(pubkeyHex)
			// Re-fetch from store to get latest state
			p := pendingConnections.Get(secret)
			if p != nil {
				p.mu.Lock()
				p.UserRelayList = relayList
				p.mu.Unlock()
				pendingConnections.Set(secret, p)
			}
		}(userPubKey, secret)

		// Fetch user's profile in background (for avatar in settings toggle)
		go func(pubkeyHex string) {
			fetchProfilesWithOptions(ConfigGetProfileRelays(), []string{pubkeyHex}, false)
		}(userPubKey)

		return
	}
}

func sendAndWaitForResponse(ctx context.Context, relayURL string, event *Event, expectedReqID string, convKey []byte, expectedPubKey string) (string, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return "", fmt.Errorf("connect failed: %v", err)
	}
	defer conn.Close()

	kp, _ := GetServerKeypair()

	// Subscribe to responses
	subID := "nc-resp-" + expectedReqID[:8]
	subFilter := map[string]interface{}{
		"kinds": []int{24133},
		"#p":    []string{hex.EncodeToString(kp.PubKey)},
		"since": time.Now().Unix() - 10,
	}
	if err := conn.WriteJSON([]interface{}{"REQ", subID, subFilter}); err != nil {
		return "", err
	}

	// Publish request
	if err := conn.WriteJSON([]interface{}{"EVENT", event}); err != nil {
		return "", err
	}

	// Wait for response
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(deadline)
		var msg []interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return "", err
		}

		if len(msg) < 3 {
			continue
		}

		msgType, _ := msg[0].(string)
		if msgType != "EVENT" {
			continue
		}

		eventData, _ := json.Marshal(msg[2])
		var respEvent Event
		if err := json.Unmarshal(eventData, &respEvent); err != nil {
			continue
		}

		if respEvent.PubKey != expectedPubKey {
			continue
		}

		decrypted, err := Nip44Decrypt(respEvent.Content, convKey)
		if err != nil {
			continue
		}

		var response NIP46Response
		if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
			continue
		}

		if response.ID != expectedReqID {
			continue
		}

		if response.Error != "" {
			return "", errors.New(response.Error)
		}

		return response.Result, nil
	}

	return "", errors.New("timeout")
}

// CheckConnection checks if a pending connection has been established
// Returns a BunkerSession if connected, nil otherwise
func CheckConnection(secret string) *BunkerSession {
	pending := pendingConnections.Get(secret)
	if pending == nil {
		return nil
	}

	if !pending.Connected || pending.UserPubKey == nil {
		return nil
	}

	// Connection established! Create a proper BunkerSession
	pending.mu.Lock()
	userRelayList := pending.UserRelayList
	pending.mu.Unlock()

	sessionID, err := generateSessionID()
	if err != nil {
		slog.Error("NIP-46: failed to generate session ID", "error", err)
		return nil
	}

	session := &BunkerSession{
		ID:                 sessionID,
		ClientPrivKey:      pending.ClientPrivKey,
		ClientPubKey:       pending.ClientPubKey,
		RemoteSignerPubKey: pending.RemoteSignerPubKey,
		UserPubKey:         pending.UserPubKey,
		Relays:             pending.Relays,
		ConversationKey:    pending.ConversationKey,
		Connected:          true,
		CreatedAt:          time.Now(),
		UserRelayList:      userRelayList,
	}

	// Clean up pending connection
	pendingConnections.Delete(secret)

	return session
}

// defaultNostrConnectRelays returns the default relays for nostrconnect from config
func defaultNostrConnectRelays() []string {
	return ConfigGetNostrConnectRelays()
}

// TryReconnectToSigner attempts to reconnect to an existing approved signer
// This works when the signer has already approved our server pubkey
func TryReconnectToSigner(signerPubKeyHex string, relays []string) (*BunkerSession, error) {
	kp, err := GetServerKeypair()
	if err != nil {
		return nil, fmt.Errorf("failed to get server keypair: %v", err)
	}

	signerPubKey, err := hex.DecodeString(signerPubKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid signer pubkey: %v", err)
	}

	// Compute conversation key
	convKey, err := GetConversationKey(kp.PrivKey, signerPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute conversation key: %v", err)
	}

	// Try to get_public_key from the signer
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	reqIDBytes := make([]byte, 8)
	if _, err := rand.Read(reqIDBytes); err != nil {
		return nil, fmt.Errorf("failed to generate request ID: %v", err)
	}
	reqID := hex.EncodeToString(reqIDBytes)

	request := NIP46Request{
		ID:     reqID,
		Method: "get_public_key",
		Params: []string{},
	}

	requestJSON, _ := json.Marshal(request)
	encryptedContent, err := Nip44Encrypt(string(requestJSON), convKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt request: %v", err)
	}

	requestEvent := createNIP46Event(kp.PrivKey, kp.PubKey, signerPubKey, encryptedContent)

	// Try each relay
	for _, relay := range relays {
		userPubKey, err := sendAndWaitForResponse(ctx, relay, requestEvent, reqID, convKey, signerPubKeyHex)
		if err != nil {
			slog.Debug("NIP-46: reconnect get_public_key failed", "relay", relay, "error", err)
			continue
		}

		userPubKeyBytes, err := hex.DecodeString(userPubKey)
		if err != nil {
			slog.Error("NIP-46: invalid user pubkey from reconnect", "error", err)
			continue
		}

		// Success! Create session
		sessionID, err := generateSessionID()
		if err != nil {
			slog.Error("NIP-46: failed to generate session ID", "error", err)
			continue
		}
		session := &BunkerSession{
			ID:                 sessionID,
			ClientPrivKey:      kp.PrivKey,
			ClientPubKey:       kp.PubKey,
			RemoteSignerPubKey: signerPubKey,
			UserPubKey:         userPubKeyBytes,
			Relays:             relays,
			ConversationKey:    convKey,
			Connected:          true,
			CreatedAt:          time.Now(),
		}

		slog.Info("NIP-46: reconnected to signer", "signer", signerPubKeyHex[:16], "user_pubkey", userPubKey)

		// Fetch user's profile in background (for avatar in settings toggle)
		go func(pubkeyHex string) {
			fetchProfilesWithOptions(ConfigGetProfileRelays(), []string{pubkeyHex}, false)
		}(userPubKey)

		return session, nil
	}

	return nil, errors.New("failed to reconnect: signer did not respond on any relay")
}
