package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	// pendingConnections tracks nostrconnect sessions waiting for signer response
	pendingConnections = &PendingConnectionStore{
		connections: make(map[string]*PendingConnection),
	}
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

type PendingConnectionStore struct {
	connections map[string]*PendingConnection
	mu          sync.RWMutex
}

func (s *PendingConnectionStore) Get(secret string) *PendingConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[secret]
}

func (s *PendingConnectionStore) Set(secret string, conn *PendingConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[secret] = conn
}

func (s *PendingConnectionStore) Delete(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, secret)
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
					log.Printf("NIP-46: Loaded persistent dev keypair: %s", hex.EncodeToString(pubKey))
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
			log.Printf("Warning: failed to save dev keypair: %v", err)
		} else {
			log.Printf("NIP-46: Created and saved new dev keypair: %s", hex.EncodeToString(pubKey))
		}
	} else {
		log.Printf("NIP-46: Generated ephemeral keypair: %s", hex.EncodeToString(pubKey))
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
		log.Printf("NIP-46: Failed to get server keypair for listener: %v", err)
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
			log.Printf("NIP-46: Relay listener error (%s): %v, reconnecting...", relayURL, err)
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

	log.Printf("NIP-46: Listening for connections on %s", relayURL)

	// Read loop
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		var msg []interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read error: %v", err)
		}

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
		log.Printf("NIP-46: Failed to compute conversation key: %v", err)
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
		log.Printf("NIP-46: Failed to parse response: %v", err)
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

	log.Printf("NIP-46: Received connect response for secret %s from %s", response.Result[:8], event.PubKey[:16])

	// Update pending connection
	pending.RemoteSignerPubKey = remoteSignerPubKey
	pending.ConversationKey = convKey
	pending.Connected = true

	// Now get the user's public key
	go fetchUserPubKey(pending, event.PubKey)
}

func fetchUserPubKey(pending *PendingConnection, remoteSignerPubKeyHex string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Send get_public_key request
	reqIDBytes := make([]byte, 8)
	if _, err := rand.Read(reqIDBytes); err != nil {
		log.Printf("NIP-46: Failed to generate request ID: %v", err)
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
		log.Printf("NIP-46: Failed to encrypt get_public_key request: %v", err)
		return
	}

	requestEvent := createNIP46Event(pending.ClientPrivKey, pending.ClientPubKey, pending.RemoteSignerPubKey, encryptedContent)

	// Send to relay and wait for response
	for _, relay := range pending.Relays {
		userPubKey, err := sendAndWaitForResponse(ctx, relay, requestEvent, reqID, pending.ConversationKey, remoteSignerPubKeyHex)
		if err != nil {
			log.Printf("NIP-46: get_public_key failed on %s: %v", relay, err)
			continue
		}

		userPubKeyBytes, err := hex.DecodeString(userPubKey)
		if err != nil {
			log.Printf("NIP-46: Invalid user pubkey: %v", err)
			continue
		}

		pending.UserPubKey = userPubKeyBytes
		log.Printf("NIP-46: Got user pubkey: %s", userPubKey)

		// Fetch user's NIP-65 relay list in background
		go func(pubkeyHex string) {
			relayList := fetchRelayList(pubkeyHex)
			pending.mu.Lock()
			pending.UserRelayList = relayList
			pending.mu.Unlock()
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

	session := &BunkerSession{
		ID:                 generateSessionID(),
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

// Default relays for nostrconnect
var defaultNostrConnectRelays = []string{
	"wss://relay.nsec.app",
	"wss://relay.damus.io",
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
			log.Printf("NIP-46: reconnect get_public_key failed on %s: %v", relay, err)
			continue
		}

		userPubKeyBytes, err := hex.DecodeString(userPubKey)
		if err != nil {
			log.Printf("NIP-46: Invalid user pubkey from reconnect: %v", err)
			continue
		}

		// Success! Create session
		session := &BunkerSession{
			ID:                 generateSessionID(),
			ClientPrivKey:      kp.PrivKey,
			ClientPubKey:       kp.PubKey,
			RemoteSignerPubKey: signerPubKey,
			UserPubKey:         userPubKeyBytes,
			Relays:             relays,
			ConversationKey:    convKey,
			Connected:          true,
			CreatedAt:          time.Now(),
		}

		log.Printf("NIP-46: Reconnected to signer %s, user pubkey: %s", signerPubKeyHex[:16], userPubKey)
		return session, nil
	}

	return nil, errors.New("failed to reconnect: signer did not respond on any relay")
}
