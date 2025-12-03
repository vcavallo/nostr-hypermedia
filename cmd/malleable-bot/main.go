package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/gorilla/websocket"
)

// Configuration
var (
	privateKeyHex string
	relays        = []string{
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://relay.primal.net",
	}
	anthropicAPIKey string
	claudeModel     = "claude-sonnet-4-20250514"
)

// NIP-90 DVM kinds for malleable UI generation
const (
	KindJobRequest = 5666 // Malleable UI generation request
	KindJobResult  = 6666 // Malleable UI generation result
)

// Nostr event structure
type Event struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// Bot state
type Bot struct {
	privateKey *btcec.PrivateKey
	publicKey  string
	npub       string
	sockets    map[string]*websocket.Conn
	mu         sync.RWMutex
	seen       map[string]bool // track processed events
	seenMu     sync.Mutex
}

func main() {
	// Parse flags
	flag.StringVar(&privateKeyHex, "nsec", "", "Bot's private key (hex)")
	flag.StringVar(&anthropicAPIKey, "anthropic-key", "", "Anthropic API key")
	flag.Parse()

	// Check environment variables as fallback
	if privateKeyHex == "" {
		privateKeyHex = os.Getenv("BOT_NSEC")
	}
	if anthropicAPIKey == "" {
		anthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	if privateKeyHex == "" {
		log.Fatal("Private key required: use -nsec flag or BOT_NSEC env var")
	}
	if anthropicAPIKey == "" {
		log.Fatal("Anthropic API key required: use -anthropic-key flag or ANTHROPIC_API_KEY env var")
	}

	// Initialize bot
	bot, err := NewBot(privateKeyHex)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}

	log.Printf("Malleable Bot starting...")
	log.Printf("Public key: %s", bot.publicKey)
	log.Printf("NPub: %s", bot.npub)
	log.Printf("Listening on %d relays", len(relays))

	// Connect to relays and start listening
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bot.Run(ctx)

	// Wait for interrupt
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down...")
	cancel()
	bot.Close()
}

func NewBot(privKeyHex string) (*Bot, error) {
	privKeyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key hex: %w", err)
	}

	privateKey, _ := btcec.PrivKeyFromBytes(privKeyBytes)
	publicKey := privateKey.PubKey()
	pubKeyBytes := publicKey.SerializeCompressed()[1:] // Remove prefix byte
	pubKeyHex := hex.EncodeToString(pubKeyBytes)

	// Encode as npub
	npub, _ := encodeBech32("npub", pubKeyBytes)

	return &Bot{
		privateKey: privateKey,
		publicKey:  pubKeyHex,
		npub:       npub,
		sockets:    make(map[string]*websocket.Conn),
		seen:       make(map[string]bool),
	}, nil
}

func (b *Bot) Run(ctx context.Context) {
	// Connect to all relays
	for _, relay := range relays {
		go b.connectAndListen(ctx, relay)
	}

	<-ctx.Done()
}

func (b *Bot) connectAndListen(ctx context.Context, relayURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := b.connectRelay(ctx, relayURL)
		if err != nil {
			log.Printf("[%s] Connection error: %v", relayURL, err)
		}

		// Reconnect after delay
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			log.Printf("[%s] Reconnecting...", relayURL)
		}
	}
}

func (b *Bot) connectRelay(ctx context.Context, relayURL string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}

	b.mu.Lock()
	b.sockets[relayURL] = conn
	b.mu.Unlock()

	defer func() {
		conn.Close()
		b.mu.Lock()
		delete(b.sockets, relayURL)
		b.mu.Unlock()
	}()

	log.Printf("[%s] Connected", relayURL)

	// Subscribe to DVM job requests (kind:5666)
	subID := "dvm-requests"
	filter := map[string]interface{}{
		"kinds": []int{KindJobRequest},
		"since": time.Now().Unix() - 60, // Last minute on startup, then live
		"limit": 10,
	}

	req := []interface{}{"REQ", subID, filter}
	reqJSON, _ := json.Marshal(req)

	if err := conn.WriteMessage(websocket.TextMessage, reqJSON); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	log.Printf("[%s] Subscribed to DVM job requests (kind:%d)", relayURL, KindJobRequest)

	// Read messages
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read failed: %w", err)
		}

		b.handleMessage(ctx, relayURL, message)
	}
}

func (b *Bot) handleMessage(ctx context.Context, relay string, message []byte) {
	var msg []json.RawMessage
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	if len(msg) < 2 {
		return
	}

	var msgType string
	json.Unmarshal(msg[0], &msgType)

	switch msgType {
	case "EVENT":
		if len(msg) < 3 {
			return
		}
		var event Event
		if err := json.Unmarshal(msg[2], &event); err != nil {
			log.Printf("[%s] Failed to parse event: %v", relay, err)
			return
		}

		// Only process DVM job requests
		if event.Kind != KindJobRequest {
			return
		}

		// Check if we've already processed this event
		b.seenMu.Lock()
		if b.seen[event.ID] {
			b.seenMu.Unlock()
			return
		}
		b.seen[event.ID] = true
		b.seenMu.Unlock()

		// Don't respond to our own events
		if event.PubKey == b.publicKey {
			return
		}

		log.Printf("[%s] Received DVM job request (kind:%d) from %s", relay, event.Kind, event.PubKey[:8])

		// Process in background
		go b.handleJobRequest(ctx, event)

	case "EOSE":
		log.Printf("[%s] End of stored events", relay)

	case "OK":
		var eventID string
		var success bool
		var message string
		if len(msg) >= 3 {
			json.Unmarshal(msg[1], &eventID)
			json.Unmarshal(msg[2], &success)
		}
		if len(msg) >= 4 {
			json.Unmarshal(msg[3], &message)
		}
		if success {
			log.Printf("[%s] Event %s published successfully", relay, eventID[:8])
		} else {
			log.Printf("[%s] Event %s rejected: %s", relay, eventID[:8], message)
		}
	}
}

func (b *Bot) handleJobRequest(ctx context.Context, event Event) {
	log.Printf("Processing DVM job request from %s...", event.PubKey[:8])

	// Extract input from NIP-90 tags
	// Look for ["i", "<data>", "<input-type>", ...]
	var request string
	for _, tag := range event.Tags {
		if len(tag) >= 3 && tag[0] == "i" && tag[2] == "text" {
			request = tag[1]
			break
		}
	}

	// Fallback to content if no "i" tag found
	if request == "" {
		request = event.Content
	}

	if strings.TrimSpace(request) == "" {
		log.Printf("Empty request, ignoring")
		return
	}

	log.Printf("Job input: %s", truncate(request, 100))

	// Generate UI spec using Claude
	uiSpec, err := b.generateUISpec(ctx, request)
	if err != nil {
		log.Printf("Failed to generate UI spec: %v", err)
		b.publishJobError(ctx, event, err.Error())
		return
	}

	log.Printf("Generated UI spec: %s", truncate(uiSpec, 200))
	log.Printf("UI spec byte length: %d, rune count: %d", len(uiSpec), len([]rune(uiSpec)))

	// Debug: check for unusual bytes in the content
	hasUnusual := false
	for i, b := range []byte(uiSpec) {
		// Log any non-printable ASCII or unusual control characters
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			log.Printf("WARNING: Unusual byte at position %d: 0x%02x", i, b)
			hasUnusual = true
		}
	}

	// Also check for Unicode BOM or other problematic sequences
	if len(uiSpec) >= 3 {
		if uiSpec[0] == 0xEF && uiSpec[1] == 0xBB && uiSpec[2] == 0xBF {
			log.Printf("WARNING: UTF-8 BOM detected at start of content!")
			hasUnusual = true
		}
	}

	// Log first and last 50 bytes in hex for debugging
	if !hasUnusual {
		log.Printf("Content starts with (hex): % x", []byte(uiSpec[:min(50, len(uiSpec))]))
		log.Printf("Content ends with (hex): % x", []byte(uiSpec[max(0, len(uiSpec)-50):]))
	}

	// Create job result event (kind:6666)
	result := b.createJobResult(event, uiSpec)

	// Publish to all relays
	b.publish(result)

	log.Printf("Published job result (kind:%d): %s", KindJobResult, result.ID[:8])
}

func (b *Bot) generateUISpec(ctx context.Context, request string) (string, error) {
	systemPrompt := `You are a UI specification generator for a Nostr-based malleable UI system.
Users describe UIs they want, and you generate JSON UI specifications that can be rendered by a malleable hypermedia client.

IMPORTANT: Respond with ONLY the JSON UI spec. No markdown, no explanation, no code fences. Just pure JSON.

=== MALLEABLE UI SPECIFICATION FORMAT ===

Top-level structure:
{
  "layout": "card",      // Required: "card", "list", or "form"
  "title": "Page Title", // Optional: sets browser title
  "elements": [],        // Required: array of UI elements
  "actions": [],         // Optional: named actions for buttons
  "state": {},           // Optional: initial state values
  "style": ""            // Optional: custom CSS
}

=== BASIC ELEMENT TYPES ===

HEADING - Large title text
  {"type": "heading", "value": "Welcome!"}
  {"type": "heading", "bind": "$.npub"}  // Can bind to data

TEXT - Paragraph text
  {"type": "text", "value": "Some description text here."}

IMAGE - Display an image
  {"type": "image", "src": "https://example.com/image.jpg"}

LINK - Clickable hyperlink
  {"type": "link", "href": "https://example.com", "label": "Visit Site"}

BUTTON - Triggers an action or navigates
  {"type": "button", "label": "Click Me", "action": "action-id"}
  {"type": "button", "label": "Go Home", "href": "/home"}

INPUT - Text input field
  {"type": "input", "name": "username", "label": "Your name:"}

CONTAINER - Groups elements, useful for layouts
  {"type": "container", "children": [...]}
  {"type": "container", "style": "options", "children": [...]}  // "options" = flex row layout

DATA - Displays bound data with optional label
  {"type": "data", "bind": "$.id", "label": "Event ID: "}

HR - Horizontal rule/divider
  {"type": "hr"}

=== ADVANCED ELEMENT TYPES ===

QUERY - Fetch events from Nostr relays
  {
    "type": "query",
    "filter": {"kinds": [1], "limit": 10},  // Standard Nostr filter
    "as": "notes",                           // Store results in $.notes
    "timeout": 5000,                         // Optional timeout in ms
    "children": [...]                        // Elements to render with results
  }
  Filter supports: kinds, authors, ids, #e, #p, since, until, limit
  Results are available as $.notes (or whatever "as" specifies)
  Each result has: .id, .pubkey, .content, .created_at, .kind, .tags

FOREACH - Iterate over arrays
  {
    "type": "foreach",
    "items": "$.notes",           // Path to array
    "as": "note",                 // Name for each item (default: "item")
    "index": "i",                 // Optional index variable
    "template": {...}             // Single element template
    // OR "children": [...]       // Multiple elements
  }
  Inside template: $.note.content, $.note.pubkey, $.i (index)

IF - Conditional rendering
  {
    "type": "if",
    "condition": "$.notes.length > 0",  // Expression to evaluate
    "then": [...],                       // Elements if true
    "else": [...]                        // Elements if false (optional)
  }
  Condition supports:
  - Comparisons: ==, !=, >, <, >=, <=
  - Arithmetic: +, -, *, /, %
  - Truthiness: "$.pubkey" (true if exists and not empty)
  Examples: "$.count > 5", "$.minute % 2 == 0", "$.kind == 1"

EMBED - Embed another Nostr event
  {
    "type": "embed",
    "event": "nevent1..."  // Event ID (hex, nevent, or note format)
  }

EVAL - Evaluate JavaScript expression
  {
    "type": "eval",
    "expr": "new Date().getMinutes()",  // JS expression
    "as": "minute",                      // Store result in context (optional)
    "display": true                      // Show result (default: true if no "as")
  }
  Available in expr: Date, Math, JSON, and all context values

SCRIPT - Execute JavaScript code (for complex logic)
  {
    "type": "script",
    "code": "const now = new Date(); emit('<p>' + now.toLocaleString() + '</p>');"
  }
  Use emit(html) to output HTML. Available: ctx, Date, Math, JSON, escapeHtml

STATE-VALUE - Display a state variable
  {"type": "state-value", "key": "count"}

=== CONTEXT VALUES ===

These are automatically available in bindings and expressions:
  $.id        - Current event ID (64 char hex)
  $.pubkey    - Author's public key (64 char hex)
  $.npub      - Author's npub (bech32 encoded)
  $.content   - Raw event content
  $.time      - Formatted timestamp
  $.kind      - Event kind number
  $.created_at - Unix timestamp

NOTE: There are NO built-in date/time values. Use "eval" to compute them:
  {"type": "eval", "expr": "new Date().getMinutes()", "as": "minute", "display": false}
Then use $.minute in conditions.

=== ACTIONS ===

Actions define what happens when buttons are clicked.

PUBLISH ACTION - Sign and publish a new Nostr event:
{
  "id": "vote-yes",
  "publish": {
    "kind": 7,                              // Event kind
    "content": "yes",                       // Event content
    "tags": [["e", "{{$.id}}"]]            // Event tags
  }
}

LINK ACTION - Navigate to URL:
{
  "id": "view-profile",
  "link": "/html/profile/{{$.pubkey}}"
}

=== TEMPLATE VARIABLES ===

In action content and tags, use {{$.path}} for substitution:
  {{$.id}}              - Current event's ID
  {{$.pubkey}}          - Current event's author pubkey
  {{input:fieldname}}   - Value from input with name="fieldname"

=== EXAMPLES ===

LIVE FEED (query + foreach):
{"layout":"card","elements":[{"type":"heading","value":"Recent Notes"},{"type":"query","filter":{"kinds":[1],"limit":5},"as":"notes","children":[{"type":"if","condition":"$.notes.length > 0","then":[{"type":"foreach","items":"$.notes","as":"note","template":{"type":"container","children":[{"type":"data","bind":"$.note.pubkey","label":"By: "},{"type":"text","bind":"$.note.content"}]}}],"else":[{"type":"text","value":"No notes found"}]}]}]}

TIME-BASED CONDITIONAL (eval + if):
{"layout":"card","elements":[{"type":"heading","value":"Time-Based UI"},{"type":"eval","expr":"new Date().getMinutes()","as":"minute","display":false},{"type":"data","bind":"$.minute","label":"Current minute: "},{"type":"if","condition":"$.minute % 2 == 0","then":[{"type":"text","value":"Even minute! 🎉"}],"else":[{"type":"text","value":"Odd minute... ⏰"}]}]}

POLL:
{"layout":"card","elements":[{"type":"heading","value":"Vote!"},{"type":"container","style":"options","children":[{"type":"button","label":"Option A","action":"vote-a"},{"type":"button","label":"Option B","action":"vote-b"}]}],"actions":[{"id":"vote-a","publish":{"kind":7,"content":"a","tags":[["e","{{$.id}}"]]}},{"id":"vote-b","publish":{"kind":7,"content":"b","tags":[["e","{{$.id}}"]]}}]}

=== COMPOSITION (REUSABLE COMPONENTS) ===

UIs can be composed from other malleable UI events (kind:6666).

To make a UI reusable as a component:
1. Add "name": "MyComponent" to the spec
2. Add "tags": ["reusable", "category"] for discoverability
3. Use {"type": "slot"} where parent content should go
4. Access passed props via $.props.propName or directly as $.propName

Example reusable card component:
{
  "name": "NoteCard",
  "tags": ["reusable", "card", "note"],
  "layout": "card",
  "elements": [
    {"type": "data", "bind": "$.author", "label": "By: "},
    {"type": "text", "bind": "$.content"},
    {"type": "slot", "default": [{"type": "text", "value": "No actions"}]}
  ]
}

To USE a component in another UI:
{
  "type": "component",
  "ref": "nevent1...",           // Direct reference to component event
  // OR "query": {"#t": ["component:notecard"], "kinds": [6666], "limit": 1}
  "props": {                     // Props passed to component
    "author": "$.note.pubkey",   // Bindings resolved from parent context
    "content": "$.note.content"
  },
  "children": [                  // Rendered in component's <slot>
    {"type": "button", "label": "Like", "action": "like"}
  ]
}

Discovering components:
- Query kind:6666 with #t filter: {"kinds": [6666], "#t": ["component:notecard"]}
- All malleable UIs are tagged with "malleable-ui"
- Named components tagged "component:name"

=== DESIGN GUIDELINES ===

1. Use "eval" with "as" to compute values, then use them in conditions
2. Always set "display": false on eval elements that just set context values
3. Use query+foreach to display live Nostr data
4. Use if/else for conditional rendering based on data or computed values
5. For time-based logic, use eval with Date/Math functions
6. Keep UIs focused and simple
7. For reusable components, add "name" and "tags" fields
8. Use "slot" to allow parent UIs to inject content

Now generate a UI spec for the user's request. Output ONLY valid JSON, nothing else.`

	userMessage := fmt.Sprintf("Create a UI spec for: %s", request)

	// Call Claude API
	reqBody := map[string]interface{}{
		"model":      claudeModel,
		"max_tokens": 2048,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userMessage},
		},
	}

	reqJSON, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqJSON))
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", anthropicAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody)
		return "", fmt.Errorf("API error %d: %v", resp.StatusCode, errBody)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response failed: %w", err)
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Claude")
	}

	uiSpec := strings.TrimSpace(result.Content[0].Text)

	// Clean up any markdown code fences if present
	uiSpec = strings.TrimPrefix(uiSpec, "```json")
	uiSpec = strings.TrimPrefix(uiSpec, "```")
	uiSpec = strings.TrimSuffix(uiSpec, "```")
	uiSpec = strings.TrimSpace(uiSpec)

	// Validate it's valid JSON
	var test interface{}
	if err := json.Unmarshal([]byte(uiSpec), &test); err != nil {
		return "", fmt.Errorf("invalid JSON from Claude: %w", err)
	}

	return uiSpec, nil
}

// createJobResult creates a NIP-90 job result event (kind:6666)
func (b *Bot) createJobResult(request Event, uiSpec string) *Event {
	now := time.Now().Unix()

	// Stringify the original request for the "request" tag
	requestJSON, _ := json.Marshal(request)

	// Build NIP-90 job result tags
	tags := [][]string{
		{"request", string(requestJSON)},       // Original request as JSON
		{"e", request.ID},                      // Reference to job request
		{"p", request.PubKey},                  // Customer's pubkey
		{"alt", "Malleable UI specification"},  // NIP-31 alt tag for unknown event handling
		{"t", "malleable-ui"},                  // Tag for discoverability
	}

	// Extract component name/type from the spec for tagging
	var specObj map[string]interface{}
	if err := json.Unmarshal([]byte(uiSpec), &specObj); err == nil {
		// If spec has a "name" field, use it as a component tag
		if name, ok := specObj["name"].(string); ok && name != "" {
			tags = append(tags, []string{"t", "component:" + strings.ToLower(name)})
		}
		// If spec has a "tags" array, add those
		if specTags, ok := specObj["tags"].([]interface{}); ok {
			for _, t := range specTags {
				if tagStr, ok := t.(string); ok {
					tags = append(tags, []string{"t", tagStr})
				}
			}
		}
		// Add layout type as a tag
		if layout, ok := specObj["layout"].(string); ok {
			tags = append(tags, []string{"t", "layout:" + layout})
		}
	}

	// Copy input tags from request
	for _, tag := range request.Tags {
		if len(tag) >= 1 && tag[0] == "i" {
			tags = append(tags, tag)
		}
	}

	event := &Event{
		PubKey:    b.publicKey,
		CreatedAt: now,
		Kind:      KindJobResult,
		Tags:      tags,
		Content:   uiSpec, // UI spec goes in content
	}

	// Compute event ID
	event.ID = computeEventID(event)

	// Debug: log the serialization
	serializedArray := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}
	serializedJSON, _ := json.Marshal(serializedArray)
	log.Printf("Event serialization (first 500 chars): %s", truncate(string(serializedJSON), 500))
	log.Printf("Computed event ID: %s", event.ID)

	// Sign
	event.Sig = b.signEvent(event.ID)

	return event
}

// publishJobError publishes a NIP-90 job feedback event with error status
func (b *Bot) publishJobError(ctx context.Context, request Event, errMsg string) {
	now := time.Now().Unix()

	tags := [][]string{
		{"status", "error", errMsg},
		{"e", request.ID},
		{"p", request.PubKey},
	}

	event := &Event{
		PubKey:    b.publicKey,
		CreatedAt: now,
		Kind:      7000, // NIP-90 job feedback
		Tags:      tags,
		Content:   fmt.Sprintf("Failed to generate UI: %s", errMsg),
	}

	event.ID = computeEventID(event)
	event.Sig = b.signEvent(event.ID)

	b.publish(event)
	log.Printf("Published job error feedback: %s", event.ID[:8])
}

func (b *Bot) publish(event *Event) {
	// Use encoder with SetEscapeHTML(false) to avoid escaping <, >, &
	// which would cause ID mismatch when relay re-computes the hash
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.Encode(event)
	eventJSON := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))

	msg := fmt.Sprintf(`["EVENT",%s]`, eventJSON)

	// Debug: Write the full event to a file for analysis
	os.WriteFile("/tmp/last_published_event.json", eventJSON, 0644)
	log.Printf("Full event JSON saved to /tmp/last_published_event.json")

	// Debug: log the full event being published
	log.Printf("Publishing event JSON (first 1000 chars): %s", truncate(string(eventJSON), 1000))

	// Debug: compute what a relay would compute and compare
	var replayEvent Event
	json.Unmarshal(eventJSON, &replayEvent)
	replayID := computeEventID(&replayEvent)
	if replayID != event.ID {
		log.Printf("ERROR: Replay ID mismatch! event.ID=%s, replay=%s", event.ID, replayID)
		log.Printf("Event JSON length: %d", len(eventJSON))
	}

	// Verify the event before publishing
	computedID := computeEventID(event)
	if computedID != event.ID {
		log.Printf("ERROR: Event ID mismatch before publish! computed=%s, event.ID=%s", computedID, event.ID)
	}

	// Verify signature
	idBytes, _ := hex.DecodeString(event.ID)
	sigBytes, _ := hex.DecodeString(event.Sig)
	pubKeyBytes, _ := hex.DecodeString(event.PubKey)

	sig, err := schnorr.ParseSignature(sigBytes)
	if err != nil {
		log.Printf("ERROR: Failed to parse signature: %v", err)
	} else {
		pubKey, err := schnorr.ParsePubKey(pubKeyBytes)
		if err != nil {
			log.Printf("ERROR: Failed to parse pubkey: %v", err)
		} else {
			if !sig.Verify(idBytes, pubKey) {
				log.Printf("ERROR: Signature verification FAILED before publish!")
			} else {
				log.Printf("Signature verification passed before publish")
			}
		}
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for relay, conn := range b.sockets {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
			log.Printf("[%s] Failed to publish: %v", relay, err)
		} else {
			log.Printf("[%s] Publishing event %s...", relay, event.ID[:8])
		}
	}
}

func (b *Bot) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, conn := range b.sockets {
		conn.Close()
	}
}

func computeEventID(event *Event) string {
	// Nostr event ID is SHA256 of the canonical JSON serialization:
	// [0, pubkey, created_at, kind, tags, content]
	//
	// IMPORTANT: We must NOT escape HTML characters (<, >, &) because
	// Nostr relays expect unescaped JSON. Go's json.Marshal escapes these
	// by default, so we use json.Encoder with SetEscapeHTML(false).
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.Encode(serialized)

	// Encoder.Encode adds a trailing newline, remove it
	jsonBytes := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))

	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

// escapeJSONString properly escapes a string for JSON (used in tests)
func escapeJSONString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (b *Bot) signEvent(eventID string) string {
	idBytes, _ := hex.DecodeString(eventID)
	sig, _ := schnorr.Sign(b.privateKey, idBytes)
	return hex.EncodeToString(sig.Serialize())
}

// Bech32 encoding for npub
const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func encodeBech32(hrp string, data []byte) (string, error) {
	// Convert 8-bit to 5-bit
	var values []int
	acc := 0
	bits := 0
	for _, b := range data {
		acc = (acc << 8) | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			values = append(values, (acc>>bits)&31)
		}
	}
	if bits > 0 {
		values = append(values, (acc<<(5-bits))&31)
	}

	// Add checksum
	checksum := bech32Checksum(hrp, values)
	values = append(values, checksum...)

	// Encode
	var result strings.Builder
	result.WriteString(hrp)
	result.WriteString("1")
	for _, v := range values {
		result.WriteByte(bech32Charset[v])
	}

	return result.String(), nil
}

func bech32Checksum(hrp string, data []int) []int {
	values := bech32HrpExpand(hrp)
	values = append(values, data...)
	values = append(values, 0, 0, 0, 0, 0, 0)
	polymod := bech32Polymod(values) ^ 1
	checksum := make([]int, 6)
	for i := 0; i < 6; i++ {
		checksum[i] = (polymod >> (5 * (5 - i))) & 31
	}
	return checksum
}

func bech32HrpExpand(hrp string) []int {
	var ret []int
	for _, c := range hrp {
		ret = append(ret, int(c>>5))
	}
	ret = append(ret, 0)
	for _, c := range hrp {
		ret = append(ret, int(c&31))
	}
	return ret
}

func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		b := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (b>>i)&1 == 1 {
				chk ^= gen[i]
			}
		}
	}
	return chk
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
