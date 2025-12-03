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

	// Subscribe to mentions of our pubkey
	subID := "mentions"
	filter := map[string]interface{}{
		"kinds": []int{1},
		"#p":    []string{b.publicKey},
		"since": time.Now().Unix() - 60, // Last minute on startup, then live
		"limit": 10,
	}

	req := []interface{}{"REQ", subID, filter}
	reqJSON, _ := json.Marshal(req)

	if err := conn.WriteMessage(websocket.TextMessage, reqJSON); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	log.Printf("[%s] Subscribed to mentions", relayURL)

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

		log.Printf("[%s] Received mention from %s: %s", relay, event.PubKey[:8], truncate(event.Content, 100))

		// Process in background
		go b.handleMention(ctx, event)

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

func (b *Bot) handleMention(ctx context.Context, event Event) {
	log.Printf("Processing request from %s...", event.PubKey[:8])

	// Extract the request (remove any @mentions from content)
	request := cleanMentions(event.Content)
	if strings.TrimSpace(request) == "" {
		log.Printf("Empty request, ignoring")
		return
	}

	// Generate UI spec using Claude
	uiSpec, err := b.generateUISpec(ctx, request)
	if err != nil {
		log.Printf("Failed to generate UI spec: %v", err)
		// Reply with error message
		b.replyWithError(ctx, event, err.Error())
		return
	}

	log.Printf("Generated UI spec: %s", truncate(uiSpec, 200))
	log.Printf("UI spec byte length: %d, rune count: %d", len(uiSpec), len([]rune(uiSpec)))

	// Create reply event
	reply := b.createReply(event, uiSpec)

	// Publish to all relays
	b.publish(reply)

	log.Printf("Published reply: %s", reply.ID[:8])
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
  "style": ""            // Optional: custom CSS
}

=== ELEMENT TYPES ===

HEADING - Large title text
  {"type": "heading", "value": "Welcome!"}
  {"type": "heading", "bind": "$.npub"}  // Can bind to data

TEXT - Paragraph text
  {"type": "text", "value": "Some description text here."}

IMAGE - Display an image
  {"type": "image", "src": "https://example.com/image.jpg"}
  {"type": "image", "value": "https://..."}  // "value" also works

LINK - Clickable hyperlink
  {"type": "link", "href": "https://example.com", "label": "Visit Site"}

BUTTON - Triggers an action or navigates
  {"type": "button", "label": "Click Me", "action": "action-id"}
  {"type": "button", "label": "Go Home", "href": "/home"}

INPUT - Text input field
  {"type": "input", "name": "username", "label": "Your name:"}
  {"type": "input", "name": "email", "label": "Email:", "value": "default@example.com"}

CONTAINER - Groups elements, useful for layouts
  {"type": "container", "children": [...]}
  {"type": "container", "style": "options", "children": [...]}  // "options" = flex row layout

DATA - Displays bound data with optional label
  {"type": "data", "bind": "$.id", "label": "Event ID: "}
  {"type": "data", "bind": "$.time"}  // No label

HR - Horizontal rule/divider
  {"type": "hr"}

=== DATA BINDINGS ===

Use "bind" property to display event data:
  $.id        - Event ID (64 char hex)
  $.pubkey    - Author's public key (64 char hex)
  $.npub      - Author's npub (bech32 encoded)
  $.content   - Raw event content
  $.time      - Formatted timestamp
  $.kind      - Event kind number

Example: {"type": "data", "bind": "$.npub", "label": "Author: "}

=== ACTIONS ===

Actions define what happens when buttons are clicked.

PUBLISH ACTION - Sign and publish a new Nostr event:
{
  "id": "vote-yes",
  "publish": {
    "kind": 7,                              // Event kind (7=reaction, 1=note, 6=repost)
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

Example: "content": "User {{input:name}} voted {{input:choice}}"

=== NOSTR EVENT KINDS ===

Common kinds to use in actions:
  Kind 1  - Short text note (like a tweet)
  Kind 6  - Repost
  Kind 7  - Reaction (likes, emoji reactions, poll votes)
  Kind 30023 - Long-form content

For reactions/polls, use kind 7 with different content values.
Always tag the event being reacted to: ["e", "{{$.id}}"]

=== EXAMPLES ===

POLL:
{"layout":"card","elements":[{"type":"heading","value":"What's your favorite season?"},{"type":"text","value":"Cast your vote below!"},{"type":"hr"},{"type":"container","style":"options","children":[{"type":"button","label":"Spring","action":"vote-spring"},{"type":"button","label":"Summer","action":"vote-summer"},{"type":"button","label":"Fall","action":"vote-fall"},{"type":"button","label":"Winter","action":"vote-winter"}]},{"type":"hr"},{"type":"data","bind":"$.time","label":"Poll created: "}],"actions":[{"id":"vote-spring","publish":{"kind":7,"content":"spring","tags":[["e","{{$.id}}"]]}},{"id":"vote-summer","publish":{"kind":7,"content":"summer","tags":[["e","{{$.id}}"]]}},{"id":"vote-fall","publish":{"kind":7,"content":"fall","tags":[["e","{{$.id}}"]]}},{"id":"vote-winter","publish":{"kind":7,"content":"winter","tags":[["e","{{$.id}}"]]}}]}

FEEDBACK FORM:
{"layout":"card","elements":[{"type":"heading","value":"Send Feedback"},{"type":"text","value":"We'd love to hear from you!"},{"type":"input","name":"name","label":"Your name:"},{"type":"input","name":"feedback","label":"Your message:"},{"type":"hr"},{"type":"button","label":"Submit Feedback","action":"submit"}],"actions":[{"id":"submit","publish":{"kind":1,"content":"Feedback from {{input:name}}: {{input:feedback}}","tags":[["t","feedback"]]}}]}

RSVP:
{"layout":"card","elements":[{"type":"heading","value":"Party Invitation"},{"type":"text","value":"You're invited to the Nostr meetup!"},{"type":"text","value":"Saturday, 7pm at the usual place."},{"type":"hr"},{"type":"container","style":"options","children":[{"type":"button","label":"I'll be there!","action":"yes"},{"type":"button","label":"Can't make it","action":"no"},{"type":"button","label":"Maybe","action":"maybe"}]}],"actions":[{"id":"yes","publish":{"kind":7,"content":"attending","tags":[["e","{{$.id}}"]]}},{"id":"no","publish":{"kind":7,"content":"not-attending","tags":[["e","{{$.id}}"]]}},{"id":"maybe","publish":{"kind":7,"content":"maybe","tags":[["e","{{$.id}}"]]}}]}

SIMPLE COUNTER (Like button):
{"layout":"card","elements":[{"type":"heading","value":"Like This!"},{"type":"text","value":"Show your appreciation."},{"type":"button","label":"❤️ Like","action":"like"}],"actions":[{"id":"like","publish":{"kind":7,"content":"+","tags":[["e","{{$.id}}"]]}}]}

NEWSLETTER SIGNUP:
{"layout":"card","elements":[{"type":"heading","value":"Subscribe to Updates"},{"type":"text","value":"Get notified about new features."},{"type":"input","name":"email","label":"Your email:"},{"type":"button","label":"Subscribe","action":"subscribe"}],"actions":[{"id":"subscribe","publish":{"kind":1,"content":"Newsletter signup: {{input:email}}","tags":[["t","newsletter-signup"]]}}]}

=== DESIGN GUIDELINES ===

1. Keep it simple - malleable UIs work best when focused
2. Use clear labels on buttons
3. Add context with text elements explaining what the UI does
4. Use hr elements to visually separate sections
5. For polls/votes, use kind 7 reactions so votes are counted properly
6. Always include {{$.id}} in e-tags for reactions
7. Use the "options" style on containers for side-by-side buttons
8. IMPORTANT: Do NOT use emojis or special Unicode characters - stick to plain ASCII text only

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

func (b *Bot) createReply(original Event, content string) *Event {
	now := time.Now().Unix()

	// Build tags: reply to original event and mention original author
	tags := [][]string{
		{"e", original.ID, "", "reply"},
		{"p", original.PubKey},
	}

	event := &Event{
		PubKey:    b.publicKey,
		CreatedAt: now,
		Kind:      1,
		Tags:      tags,
		Content:   content,
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

func (b *Bot) replyWithError(ctx context.Context, original Event, errMsg string) {
	content := fmt.Sprintf("Sorry, I couldn't generate that UI: %s\n\nTry describing what you want more specifically, like:\n- \"make a poll about favorite pizza toppings\"\n- \"create a feedback form\"\n- \"build a simple counter app\"", errMsg)

	reply := b.createReply(original, content)
	b.publish(reply)
}

func (b *Bot) publish(event *Event) {
	eventJSON, _ := json.Marshal(event)
	msg := fmt.Sprintf(`["EVENT",%s]`, eventJSON)

	// Debug: log the full event being published
	log.Printf("Publishing event JSON (first 1000 chars): %s", truncate(string(eventJSON), 1000))

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
	// Use json.Marshal on the whole array to ensure consistency
	serialized := []interface{}{
		0,
		event.PubKey,
		event.CreatedAt,
		event.Kind,
		event.Tags,
		event.Content,
	}

	jsonBytes, _ := json.Marshal(serialized)
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

func cleanMentions(content string) string {
	// Remove nostr:npub... mentions
	words := strings.Fields(content)
	var cleaned []string
	for _, word := range words {
		if strings.HasPrefix(word, "nostr:npub") {
			continue
		}
		if strings.HasPrefix(word, "@npub") {
			continue
		}
		cleaned = append(cleaned, word)
	}
	return strings.Join(cleaned, " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
