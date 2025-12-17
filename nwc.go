package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// NWC (Nostr Wallet Connect) client implementation - NIP-47

const (
	nwcRequestKind    = 23194             // Client request to wallet
	nwcResponseKind   = 23195             // Wallet response to client
	nwcRequestTimeout = 15 * time.Second  // Timeout for NWC response (some wallets don't respond)
)

// NWCConfig holds wallet connection parameters extracted from URI
type NWCConfig struct {
	WalletPubKey     []byte `json:"wallet_pubkey"`      // Wallet's public key (32 bytes)
	Relay            string `json:"relay"`              // Relay URL for communication
	Secret           []byte `json:"secret"`             // Secret key for signing requests (32 bytes)
	ClientPubKey     []byte `json:"client_pubkey"`      // Derived public key from secret
	ConversationKey  []byte `json:"conversation_key"`   // Pre-computed conversation key (NIP-44)
	Nip04SharedKey   []byte `json:"nip04_shared_key"`   // Pre-computed shared secret (NIP-04)
}

// NWCRequest is a JSON-RPC request to the wallet
type NWCRequest struct {
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// NWCPayInvoiceParams are the parameters for pay_invoice method
type NWCPayInvoiceParams struct {
	Invoice string `json:"invoice"`
}

// NWCResponse is a JSON-RPC response from the wallet
type NWCResponse struct {
	ResultType string          `json:"result_type"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *NWCError       `json:"error,omitempty"`
}

// NWCError represents an error from the wallet
type NWCError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NWCPayInvoiceResult is the result of a successful payment
type NWCPayInvoiceResult struct {
	Preimage string `json:"preimage"`
}

// NWCBalanceResult is the result of get_balance
type NWCBalanceResult struct {
	Balance int64 `json:"balance"` // millisatoshis
}

// NWCTransaction represents a single transaction from list_transactions
type NWCTransaction struct {
	Type            string `json:"type"`                       // "incoming" or "outgoing"
	Invoice         string `json:"invoice,omitempty"`          // BOLT11 invoice
	Description     string `json:"description,omitempty"`      // Payment description
	DescriptionHash string `json:"description_hash,omitempty"` // Hash of description
	Preimage        string `json:"preimage,omitempty"`         // Payment preimage
	PaymentHash     string `json:"payment_hash,omitempty"`     // Payment hash
	Amount          int64  `json:"amount"`                     // Amount in millisatoshis
	FeesPaid        int64  `json:"fees_paid,omitempty"`        // Fees in millisatoshis
	CreatedAt       int64  `json:"created_at"`                 // Unix timestamp
	SettledAt       int64  `json:"settled_at,omitempty"`       // Unix timestamp when settled
}

// NWCListTransactionsResult is the result of list_transactions
type NWCListTransactionsResult struct {
	Transactions []NWCTransaction `json:"transactions"`
}

// ParseNWCURI parses a nostr+walletconnect:// URI into NWCConfig
// Format: nostr+walletconnect://<wallet-pubkey>?relay=<wss://...>&secret=<hex>
func ParseNWCURI(nwcURI string) (*NWCConfig, error) {
	if !strings.HasPrefix(nwcURI, "nostr+walletconnect://") {
		return nil, errors.New("invalid NWC URI: must start with nostr+walletconnect://")
	}

	// Replace scheme for URL parsing (Go's url.Parse doesn't like nostr+walletconnect)
	parseable := strings.Replace(nwcURI, "nostr+walletconnect://", "https://", 1)
	u, err := url.Parse(parseable)
	if err != nil {
		return nil, fmt.Errorf("invalid NWC URI: %v", err)
	}

	// Extract wallet pubkey from host
	walletPubKeyHex := u.Host
	if len(walletPubKeyHex) != 64 {
		return nil, errors.New("invalid wallet pubkey: must be 64 hex characters")
	}
	walletPubKey, err := hex.DecodeString(walletPubKeyHex)
	if err != nil {
		return nil, errors.New("invalid wallet pubkey: not valid hex")
	}

	// Extract relay URL
	relay := u.Query().Get("relay")
	if relay == "" {
		return nil, errors.New("NWC URI must include relay parameter")
	}
	// Validate relay URL
	if !strings.HasPrefix(relay, "wss://") && !strings.HasPrefix(relay, "ws://") {
		return nil, errors.New("invalid relay URL: must start with wss:// or ws://")
	}

	// Extract secret key
	secretHex := u.Query().Get("secret")
	if secretHex == "" {
		return nil, errors.New("NWC URI must include secret parameter")
	}
	if len(secretHex) != 64 {
		return nil, errors.New("invalid secret: must be 64 hex characters")
	}
	secret, err := hex.DecodeString(secretHex)
	if err != nil {
		return nil, errors.New("invalid secret: not valid hex")
	}

	// Derive client public key from secret
	clientPubKey, err := GetPublicKey(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to derive public key: %v", err)
	}

	// Pre-compute conversation key for NIP-44 encryption
	conversationKey, err := GetConversationKey(secret, walletPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute conversation key: %v", err)
	}

	// Pre-compute NIP-04 shared secret (for wallets that don't support NIP-44)
	nip04SharedKey, err := GetNip04SharedSecret(secret, walletPubKey)
	if err != nil {
		return nil, fmt.Errorf("failed to compute NIP-04 shared key: %v", err)
	}

	return &NWCConfig{
		WalletPubKey:     walletPubKey,
		Relay:            relay,
		Secret:           secret,
		ClientPubKey:     clientPubKey,
		ConversationKey:  conversationKey,
		Nip04SharedKey:   nip04SharedKey,
	}, nil
}

// NWCClient handles communication with a Nostr wallet
type NWCClient struct {
	config        *NWCConfig
	conn          *websocket.Conn
	mu            sync.Mutex
	connected     bool
	subID         string
	pending       map[string]chan *NWCResponse
	pendingMu     sync.Mutex
	done          chan struct{}
	eoseReceived  chan struct{} // Signals when EOSE is received
	acceptedMu    sync.Mutex
	acceptedIDs   map[string]bool // Track event IDs that relay accepted (OK=true)
}

// NewNWCClient creates a new NWC client from config
func NewNWCClient(config *NWCConfig) *NWCClient {
	return &NWCClient{
		config:       config,
		pending:      make(map[string]chan *NWCResponse),
		done:         make(chan struct{}),
		eoseReceived: make(chan struct{}),
		acceptedIDs:  make(map[string]bool),
	}
}

// Connect establishes connection to the wallet relay
func (c *NWCClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.config.Relay, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to relay %s: %v", c.config.Relay, err)
	}

	c.conn = conn
	c.connected = true

	// Subscribe to wallet responses (kind 23195 p-tagged to our pubkey)
	c.subID = fmt.Sprintf("nwc-%d", time.Now().UnixNano()%1000000)
	walletPubKeyHex := hex.EncodeToString(c.config.WalletPubKey)
	clientPubKeyHex := hex.EncodeToString(c.config.ClientPubKey)

	// Build subscription filter - using explicit types for proper JSON encoding
	subFilter := map[string]interface{}{
		"kinds":   []int{nwcResponseKind},
		"authors": []string{walletPubKeyHex},
		"#p":      []string{clientPubKeyHex},
		// No "since" filter - we don't want to miss responses due to clock skew
	}
	subReq := []interface{}{"REQ", c.subID, subFilter}

	// Log the actual subscription JSON for debugging
	subReqJSON, _ := json.Marshal(subReq)
	slog.Debug("NWC: subscription request", "json", string(subReqJSON))

	slog.Debug("NWC: subscribing to responses",
		"sub_id", c.subID,
		"wallet_pubkey", walletPubKeyHex,
		"client_pubkey", clientPubKeyHex,
		"kind", nwcResponseKind)

	if err := conn.WriteJSON(subReq); err != nil {
		conn.Close()
		c.connected = false
		return fmt.Errorf("failed to subscribe: %v", err)
	}

	slog.Debug("NWC: connected to relay", "relay", c.config.Relay)

	// Start reader goroutine
	go c.readLoop()

	// Wait for EOSE to ensure subscription is active (with timeout)
	slog.Debug("NWC: waiting for EOSE")
	select {
	case <-c.eoseReceived:
		slog.Debug("NWC: EOSE received, subscription active")
	case <-time.After(5 * time.Second):
		slog.Debug("NWC: EOSE timeout, proceeding anyway")
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// readLoop processes incoming messages from the relay
func (c *NWCClient) readLoop() {
	defer func() {
		slog.Debug("NWC: readLoop exiting")
		c.mu.Lock()
		c.connected = false
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()

		// Notify all pending requests
		c.pendingMu.Lock()
		for _, ch := range c.pending {
			close(ch)
		}
		c.pending = make(map[string]chan *NWCResponse)
		c.pendingMu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			slog.Debug("NWC: readLoop received done signal")
			return
		default:
			var rawMsg json.RawMessage
			if err := c.conn.ReadJSON(&rawMsg); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					slog.Debug("NWC: connection closed unexpectedly", "error", err)
				} else {
					slog.Debug("NWC: readJSON error", "error", err)
				}
				return
			}

			// Log raw message for debugging
			slog.Debug("NWC: raw message received", "raw", string(rawMsg))

			var msg []interface{}
			if err := json.Unmarshal(rawMsg, &msg); err != nil {
				slog.Debug("NWC: failed to parse message as array", "error", err, "raw", string(rawMsg))
				continue
			}

			if len(msg) < 2 {
				slog.Debug("NWC: received short message", "len", len(msg))
				continue
			}

			msgType, ok := msg[0].(string)
			if !ok {
				slog.Debug("NWC: message type not string")
				continue
			}

			slog.Debug("NWC: received message", "type", msgType, "len", len(msg))

			if msgType == "EVENT" && len(msg) >= 3 {
				c.handleEvent(msg[2])
			} else if msgType == "OK" {
				// OK response for published events
				if len(msg) >= 3 {
					eventID, _ := msg[1].(string)
					success, _ := msg[2].(bool)
					reason := ""
					if len(msg) >= 4 {
						reason, _ = msg[3].(string)
					}
					slog.Debug("NWC: received OK", "event_id", eventID, "success", success, "reason", reason)

					// Track accepted events - important for wallets that don't send responses
					if success && eventID != "" {
						c.acceptedMu.Lock()
						c.acceptedIDs[eventID] = true
						c.acceptedMu.Unlock()
						slog.Debug("NWC: event accepted by relay", "event_id", eventID[:16])
					}
				}
			} else if msgType == "EOSE" {
				slog.Debug("NWC: received EOSE")
				select {
				case <-c.eoseReceived:
					// Already closed
				default:
					close(c.eoseReceived)
				}
			} else if msgType == "NOTICE" {
				if len(msg) >= 2 {
					notice, _ := msg[1].(string)
					slog.Debug("NWC: received NOTICE", "notice", notice)
				}
			} else if msgType == "AUTH" {
				// NIP-42 AUTH challenge - respond with signed auth event
				if len(msg) >= 2 {
					challenge, _ := msg[1].(string)
					slog.Debug("NWC: received AUTH challenge", "challenge", challenge[:16]+"...")
					c.handleAuth(challenge)
				}
			} else {
				slog.Debug("NWC: received unknown message type", "type", msgType)
			}
		}
	}
}

// handleAuth responds to a NIP-42 AUTH challenge
func (c *NWCClient) handleAuth(challenge string) {
	// Create kind 22242 AUTH event
	now := time.Now().Unix()
	event := &Event{
		PubKey:    hex.EncodeToString(c.config.ClientPubKey),
		CreatedAt: now,
		Kind:      22242, // NIP-42 AUTH event kind
		Tags: [][]string{
			{"relay", c.config.Relay},
			{"challenge", challenge},
		},
		Content: "",
	}

	// Calculate event ID and sign
	event.ID = calculateEventID(event)
	event.Sig = signEvent(c.config.Secret, event.ID)

	// Send AUTH response
	c.mu.Lock()
	err := c.conn.WriteJSON([]interface{}{"AUTH", event})
	c.mu.Unlock()

	if err != nil {
		slog.Error("NWC: failed to send AUTH response", "error", err)
		return
	}

	slog.Debug("NWC: sent AUTH response", "event_id", event.ID[:16])
}

// handleEvent processes an incoming event
func (c *NWCClient) handleEvent(eventData interface{}) {
	eventBytes, err := json.Marshal(eventData)
	if err != nil {
		slog.Debug("NWC: failed to marshal event data", "error", err)
		return
	}

	var event Event
	if err := json.Unmarshal(eventBytes, &event); err != nil {
		slog.Debug("NWC: failed to unmarshal event", "error", err)
		return
	}

	slog.Debug("NWC: handling event", "kind", event.Kind, "from", event.PubKey[:16], "id", event.ID[:16])

	// Verify it's from the wallet
	expectedPubKey := hex.EncodeToString(c.config.WalletPubKey)
	if event.PubKey != expectedPubKey {
		slog.Debug("NWC: event not from wallet", "got", event.PubKey[:16], "expected", expectedPubKey[:16])
		return
	}

	// Decrypt content (using NIP-04)
	decrypted, err := Nip04Decrypt(event.Content, c.config.Nip04SharedKey)
	if err != nil {
		slog.Error("NWC: failed to decrypt response", "error", err)
		return
	}

	slog.Debug("NWC: decrypted response", "content", decrypted)

	var response NWCResponse
	if err := json.Unmarshal([]byte(decrypted), &response); err != nil {
		slog.Error("NWC: failed to parse response", "error", err, "content", decrypted)
		return
	}

	slog.Debug("NWC: parsed response", "result_type", response.ResultType, "has_error", response.Error != nil)

	// Find the event ID this is responding to (from e tag)
	var requestEventID string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "e" {
			requestEventID = tag[1]
			break
		}
	}

	if requestEventID == "" {
		slog.Debug("NWC: response missing e tag", "tags", event.Tags)
		return
	}

	slog.Debug("NWC: response for request", "request_id", requestEventID[:16])

	// Route to waiting request
	c.pendingMu.Lock()
	ch, exists := c.pending[requestEventID]
	pendingCount := len(c.pending)
	if exists {
		delete(c.pending, requestEventID)
	}
	c.pendingMu.Unlock()

	slog.Debug("NWC: routing response", "found_pending", exists, "pending_count", pendingCount)

	if exists {
		ch <- &response
		slog.Debug("NWC: sent response to channel")
	} else {
		slog.Debug("NWC: no pending request found for response")
	}
}

// PayInvoice sends a payment request to the wallet
func (c *NWCClient) PayInvoice(ctx context.Context, bolt11Invoice string) (*NWCPayInvoiceResult, error) {
	slog.Debug("NWC: PayInvoice called", "invoice_len", len(bolt11Invoice))

	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		slog.Debug("NWC: not connected")
		return nil, errors.New("not connected to wallet")
	}
	c.mu.Unlock()

	// Build request
	request := NWCRequest{
		Method: "pay_invoice",
		Params: NWCPayInvoiceParams{
			Invoice: bolt11Invoice,
		},
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	slog.Debug("NWC: request JSON built", "json", string(requestJSON))

	// Encrypt with NIP-04 (many wallets don't support NIP-44 yet)
	encrypted, err := Nip04Encrypt(string(requestJSON), c.config.Nip04SharedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt request: %v", err)
	}

	slog.Debug("NWC: request encrypted (NIP-04)", "encrypted_len", len(encrypted))

	// Single attempt - if relay accepts, we consider it successful even without response
	// (some wallets like Primal process payments but don't send NIP-47 responses)
	result, err := c.sendPayInvoiceRequest(ctx, encrypted, 1)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// sendPayInvoiceRequest sends a single payment request and waits for response
func (c *NWCClient) sendPayInvoiceRequest(ctx context.Context, encrypted string, attempt int) (*NWCPayInvoiceResult, error) {
	// Create kind 23194 event (new event ID for each attempt)
	event := c.createRequestEvent(encrypted)

	slog.Debug("NWC: created request event", "event_id", event.ID, "kind", event.Kind, "attempt", attempt)

	// Set up response channel
	respCh := make(chan *NWCResponse, 1)
	c.pendingMu.Lock()
	c.pending[event.ID] = respCh
	pendingCount := len(c.pending)
	c.pendingMu.Unlock()

	slog.Debug("NWC: registered pending request", "event_id", event.ID[:16], "pending_count", pendingCount)

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, event.ID)
		c.pendingMu.Unlock()
		slog.Debug("NWC: cleaned up pending request", "event_id", event.ID[:16])
	}()

	// Publish request
	c.mu.Lock()
	err := c.conn.WriteJSON([]interface{}{"EVENT", event})
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to publish request: %v", err)
	}

	slog.Debug("NWC: sent pay_invoice request to relay", "event_id", event.ID[:16], "attempt", attempt)

	// Wait for response with timeout
	// Use Background() instead of parent ctx so retries get fresh timeouts
	timeoutCtx, cancel := context.WithTimeout(context.Background(), nwcRequestTimeout)
	defer cancel()

	slog.Debug("NWC: waiting for response", "timeout", nwcRequestTimeout, "attempt", attempt)

	select {
	case <-timeoutCtx.Done():
		slog.Debug("NWC: context done", "error", timeoutCtx.Err(), "attempt", attempt)
		// Check if the relay accepted our event - some wallets (like Primal) process
		// payments but don't send NIP-47 response events
		c.acceptedMu.Lock()
		wasAccepted := c.acceptedIDs[event.ID]
		c.acceptedMu.Unlock()
		if wasAccepted {
			slog.Info("NWC: payment likely succeeded (relay accepted, no response)", "event_id", event.ID[:16])
			// Return a synthetic success result - payment was likely processed
			return &NWCPayInvoiceResult{
				Preimage: "accepted-no-response", // Indicates we didn't get actual preimage
			}, nil
		}
		return nil, errors.New("payment timed out")
	case resp, ok := <-respCh:
		if !ok {
			slog.Debug("NWC: response channel closed")
			return nil, errors.New("connection closed")
		}
		slog.Debug("NWC: received response", "result_type", resp.ResultType, "has_error", resp.Error != nil)
		if resp.Error != nil {
			slog.Debug("NWC: response error", "code", resp.Error.Code, "message", resp.Error.Message)
			return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		if resp.ResultType != "pay_invoice" {
			return nil, fmt.Errorf("unexpected result type: %s", resp.ResultType)
		}

		var result NWCPayInvoiceResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("failed to parse result: %v", err)
		}
		slog.Debug("NWC: payment successful", "preimage", result.Preimage[:16]+"...")
		return &result, nil
	}
}

// subscribeForEventResponse subscribes with #e filter for a specific event ID
// This is needed because some NWC relays (like Primal) only deliver responses
// to subscriptions that include the specific event ID in the filter.
// Returns the subscription ID to close later.
func (c *NWCClient) subscribeForEventResponse(eventID string) (string, error) {
	walletPubKeyHex := hex.EncodeToString(c.config.WalletPubKey)
	subID := fmt.Sprintf("nwc-req-%d", time.Now().UnixNano()%1000000)

	subFilter := map[string]interface{}{
		"kinds":   []int{nwcResponseKind},
		"authors": []string{walletPubKeyHex},
		"#e":      []string{eventID},
	}
	subReq := []interface{}{"REQ", subID, subFilter}

	slog.Debug("NWC: subscribing for specific event response", "sub_id", subID, "event_id", eventID[:16])

	c.mu.Lock()
	err := c.conn.WriteJSON(subReq)
	c.mu.Unlock()
	if err != nil {
		return "", fmt.Errorf("failed to subscribe: %v", err)
	}

	return subID, nil
}

// closeSubscription closes a subscription by ID
func (c *NWCClient) closeSubscription(subID string) {
	c.mu.Lock()
	c.conn.WriteJSON([]interface{}{"CLOSE", subID})
	c.mu.Unlock()
	slog.Debug("NWC: closed subscription", "sub_id", subID)
}

// GetBalance queries the wallet balance (useful for testing NWC connectivity)
func (c *NWCClient) GetBalance(ctx context.Context) (*NWCBalanceResult, error) {
	slog.Debug("NWC: GetBalance called")

	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, errors.New("not connected to wallet")
	}
	c.mu.Unlock()

	// Build request (no params for get_balance)
	request := map[string]interface{}{
		"method": "get_balance",
		"params": map[string]interface{}{},
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	slog.Debug("NWC: get_balance request JSON", "json", string(requestJSON))

	// Encrypt with NIP-04 (many wallets don't support NIP-44 yet)
	encrypted, err := Nip04Encrypt(string(requestJSON), c.config.Nip04SharedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt request: %v", err)
	}

	// Create kind 23194 event FIRST to get the event ID
	event := c.createRequestEvent(encrypted)
	slog.Debug("NWC: created get_balance event", "event_id", event.ID[:16])

	// Set up response channel
	respCh := make(chan *NWCResponse, 1)
	c.pendingMu.Lock()
	c.pending[event.ID] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, event.ID)
		c.pendingMu.Unlock()
	}()

	// Subscribe with #e filter for THIS specific event ID BEFORE publishing
	// This is required for Primal's NWC relay which only delivers to #e subscriptions
	subID, err := c.subscribeForEventResponse(event.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe for response: %v", err)
	}
	defer c.closeSubscription(subID)

	// Small delay to ensure subscription is active on relay
	time.Sleep(50 * time.Millisecond)

	// NOW publish the request
	c.mu.Lock()
	err = c.conn.WriteJSON([]interface{}{"EVENT", event})
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to publish request: %v", err)
	}

	slog.Debug("NWC: sent get_balance request to relay")

	// Wait for response with timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), nwcRequestTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		slog.Debug("NWC: get_balance timed out")
		return nil, errors.New("get_balance timed out")
	case resp, ok := <-respCh:
		if !ok {
			return nil, errors.New("connection closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		if resp.ResultType != "get_balance" {
			return nil, fmt.Errorf("unexpected result type: %s", resp.ResultType)
		}

		var result NWCBalanceResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("failed to parse result: %v", err)
		}
		slog.Debug("NWC: got balance", "balance_msats", result.Balance)
		return &result, nil
	}
}

// ListTransactions retrieves recent transactions from the wallet
func (c *NWCClient) ListTransactions(ctx context.Context, limit int) (*NWCListTransactionsResult, error) {
	slog.Debug("NWC: ListTransactions called", "limit", limit)

	c.mu.Lock()
	if !c.connected {
		c.mu.Unlock()
		return nil, errors.New("not connected to wallet")
	}
	c.mu.Unlock()

	// Build request with optional limit
	params := map[string]interface{}{}
	if limit > 0 {
		params["limit"] = limit
	}
	request := map[string]interface{}{
		"method": "list_transactions",
		"params": params,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	slog.Debug("NWC: list_transactions request JSON", "json", string(requestJSON))

	// Encrypt with NIP-04
	encrypted, err := Nip04Encrypt(string(requestJSON), c.config.Nip04SharedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt request: %v", err)
	}

	// Create kind 23194 event FIRST to get the event ID
	event := c.createRequestEvent(encrypted)
	slog.Debug("NWC: created list_transactions event", "event_id", event.ID[:16])

	// Set up response channel
	respCh := make(chan *NWCResponse, 1)
	c.pendingMu.Lock()
	c.pending[event.ID] = respCh
	c.pendingMu.Unlock()

	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, event.ID)
		c.pendingMu.Unlock()
	}()

	// Subscribe with #e filter for THIS specific event ID BEFORE publishing
	// This is required for Primal's NWC relay which only delivers to #e subscriptions
	subID, err := c.subscribeForEventResponse(event.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe for response: %v", err)
	}
	defer c.closeSubscription(subID)

	// Small delay to ensure subscription is active on relay
	time.Sleep(50 * time.Millisecond)

	// NOW publish the request
	c.mu.Lock()
	err = c.conn.WriteJSON([]interface{}{"EVENT", event})
	c.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to publish request: %v", err)
	}

	slog.Debug("NWC: sent list_transactions request to relay")

	// Wait for response with timeout
	timeoutCtx, cancel := context.WithTimeout(context.Background(), nwcRequestTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		slog.Debug("NWC: list_transactions timed out")
		return nil, errors.New("list_transactions timed out")
	case resp, ok := <-respCh:
		if !ok {
			return nil, errors.New("connection closed")
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
		}
		if resp.ResultType != "list_transactions" {
			return nil, fmt.Errorf("unexpected result type: %s", resp.ResultType)
		}

		var result NWCListTransactionsResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			return nil, fmt.Errorf("failed to parse result: %v", err)
		}
		slog.Debug("NWC: got transactions", "count", len(result.Transactions))
		return &result, nil
	}
}

// createRequestEvent creates a signed kind 23194 event
func (c *NWCClient) createRequestEvent(encryptedContent string) *Event {
	now := time.Now().Unix()

	clientPubKeyHex := hex.EncodeToString(c.config.ClientPubKey)
	walletPubKeyHex := hex.EncodeToString(c.config.WalletPubKey)

	event := &Event{
		PubKey:    clientPubKeyHex,
		CreatedAt: now,
		Kind:      nwcRequestKind,
		Tags: [][]string{
			{"p", walletPubKeyHex},
			// No "encryption" tag = NIP-04 assumed (many wallets don't support NIP-44 yet)
		},
		Content: encryptedContent,
	}

	// Calculate event ID
	event.ID = calculateEventID(event)

	// Sign the event
	event.Sig = signEvent(c.config.Secret, event.ID)

	// Detailed debugging - log everything
	slog.Debug("NWC: createRequestEvent",
		"client_pubkey", clientPubKeyHex,
		"wallet_pubkey", walletPubKeyHex,
		"secret_first8", hex.EncodeToString(c.config.Secret[:8]),
		"shared_key_first8", hex.EncodeToString(c.config.Nip04SharedKey[:8]),
		"event_id", event.ID,
		"created_at", now)

	// Log full event for debugging
	eventJSON, _ := json.Marshal(event)
	slog.Debug("NWC: full request event", "event_json", string(eventJSON))

	return event
}

// Close closes the NWC client connection
func (c *NWCClient) Close() {
	select {
	case <-c.done:
		return
	default:
		close(c.done)
	}

	c.mu.Lock()
	if c.conn != nil {
		// Unsubscribe before closing
		if c.subID != "" {
			c.conn.WriteJSON([]interface{}{"CLOSE", c.subID})
		}
		c.conn.Close()
	}
	c.connected = false
	c.mu.Unlock()
}

// WalletPubKeyHex returns the wallet's public key as hex string
func (c *NWCClient) WalletPubKeyHex() string {
	return hex.EncodeToString(c.config.WalletPubKey)
}

// ClientPubKeyHex returns the client's public key as hex string
func (c *NWCClient) ClientPubKeyHex() string {
	return hex.EncodeToString(c.config.ClientPubKey)
}

// NWCErrorCodes are standard error codes from NIP-47
const (
	NWCErrorRateLimited        = "RATE_LIMITED"
	NWCErrorNotImplemented     = "NOT_IMPLEMENTED"
	NWCErrorInsufficientBalance = "INSUFFICIENT_BALANCE"
	NWCErrorQuotaExceeded      = "QUOTA_EXCEEDED"
	NWCErrorRestricted         = "RESTRICTED"
	NWCErrorUnauthorized       = "UNAUTHORIZED"
	NWCErrorInternal           = "INTERNAL"
	NWCErrorOther              = "OTHER"
	NWCErrorPaymentFailed      = "PAYMENT_FAILED"
	NWCErrorNotFound           = "NOT_FOUND"
)

// IsRetryableError returns true if the error might succeed on retry
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, NWCErrorRateLimited) ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection")
}

// IsConnected returns whether the client has an active connection
func (c *NWCClient) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// --- NWC Connection Pool ---
// Global pool of NWC clients keyed by user pubkey (hex)
// Connections are reused across requests and cleaned up after idle timeout

const (
	nwcPoolIdleTimeout    = 10 * time.Minute // Close connections idle longer than this
	nwcPoolCleanupInterval = 5 * time.Minute  // How often to run cleanup
)

// nwcPoolEntry holds a client and its last activity time
type nwcPoolEntry struct {
	client     *NWCClient
	lastActive time.Time
	mu         sync.Mutex
}

// updateActivity updates the last active timestamp
func (e *nwcPoolEntry) updateActivity() {
	e.mu.Lock()
	e.lastActive = time.Now()
	e.mu.Unlock()
}

// isIdle returns true if the entry has been idle longer than timeout
func (e *nwcPoolEntry) isIdle(timeout time.Duration) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return time.Since(e.lastActive) > timeout
}

var (
	nwcPool        = make(map[string]*nwcPoolEntry) // userPubkeyHex -> entry
	nwcPoolMu      sync.RWMutex
	nwcPoolStarted bool
)

// startNWCPoolCleanup starts the background cleanup goroutine (called once)
func startNWCPoolCleanup() {
	nwcPoolMu.Lock()
	if nwcPoolStarted {
		nwcPoolMu.Unlock()
		return
	}
	nwcPoolStarted = true
	nwcPoolMu.Unlock()

	go func() {
		ticker := time.NewTicker(nwcPoolCleanupInterval)
		defer ticker.Stop()

		for range ticker.C {
			cleanupIdleNWCConnections()
		}
	}()
}

// cleanupIdleNWCConnections closes and removes idle connections
func cleanupIdleNWCConnections() {
	nwcPoolMu.Lock()
	defer nwcPoolMu.Unlock()

	for userPubkey, entry := range nwcPool {
		if entry.isIdle(nwcPoolIdleTimeout) || !entry.client.IsConnected() {
			slog.Debug("NWC pool: closing idle/disconnected connection", "user", userPubkey[:16])
			entry.client.Close()
			delete(nwcPool, userPubkey)
		}
	}

	slog.Debug("NWC pool: cleanup complete", "active_connections", len(nwcPool))
}

// GetPooledNWCClient returns a connected NWC client from the pool, creating one if needed.
// The returned client should NOT be closed by the caller - the pool manages lifecycle.
func GetPooledNWCClient(ctx context.Context, userPubkeyHex string, config *NWCConfig) (*NWCClient, error) {
	// Ensure cleanup goroutine is running
	startNWCPoolCleanup()

	// Check for existing connected client
	nwcPoolMu.RLock()
	entry, exists := nwcPool[userPubkeyHex]
	nwcPoolMu.RUnlock()

	if exists && entry.client.IsConnected() {
		entry.updateActivity()
		slog.Debug("NWC pool: reusing existing connection", "user", userPubkeyHex[:16])
		return entry.client, nil
	}

	// Need to create new client - acquire write lock
	nwcPoolMu.Lock()
	defer nwcPoolMu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have created it)
	entry, exists = nwcPool[userPubkeyHex]
	if exists && entry.client.IsConnected() {
		entry.updateActivity()
		slog.Debug("NWC pool: reusing connection (after lock)", "user", userPubkeyHex[:16])
		return entry.client, nil
	}

	// Close old disconnected client if exists
	if exists {
		slog.Debug("NWC pool: closing stale connection", "user", userPubkeyHex[:16])
		entry.client.Close()
		delete(nwcPool, userPubkeyHex)
	}

	// Create and connect new client
	slog.Debug("NWC pool: creating new connection", "user", userPubkeyHex[:16])
	client := NewNWCClient(config)
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect NWC client: %w", err)
	}

	// Store in pool
	nwcPool[userPubkeyHex] = &nwcPoolEntry{
		client:     client,
		lastActive: time.Now(),
	}

	slog.Debug("NWC pool: connection established", "user", userPubkeyHex[:16], "pool_size", len(nwcPool))
	return client, nil
}

// CloseNWCPoolConnection closes and removes a specific user's connection from the pool.
// Use this when wallet is disconnected or user logs out.
func CloseNWCPoolConnection(userPubkeyHex string) {
	nwcPoolMu.Lock()
	defer nwcPoolMu.Unlock()

	if entry, exists := nwcPool[userPubkeyHex]; exists {
		slog.Debug("NWC pool: explicitly closing connection", "user", userPubkeyHex[:16])
		entry.client.Close()
		delete(nwcPool, userPubkeyHex)
	}
}

// NWCPoolStats returns current pool statistics
func NWCPoolStats() (activeConnections int) {
	nwcPoolMu.RLock()
	defer nwcPoolMu.RUnlock()
	return len(nwcPool)
}
