package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/skip2/go-qrcode"
)

const sessionCookieName = "nostr_session"
const sessionMaxAge = 24 * time.Hour

// generateQRCodeDataURL creates a QR code as a base64 data URL
func generateQRCodeDataURL(content string) string {
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		log.Printf("Failed to generate QR code: %v", err)
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// prefetchUserProfile fetches and caches the user's profile in the background
// This should be called after login to ensure the profile is ready for display
func prefetchUserProfile(pubkeyHex string, relays []string) {
	go func() {
		log.Printf("Prefetching profile for logged-in user: %s", pubkeyHex[:16])
		fetchProfiles(relays, []string{pubkeyHex})
	}()
}

// getUserDisplayName returns a display name for a pubkey, checking cache first
func getUserDisplayName(pubkeyHex string) string {
	// Check profile cache
	if profile, ok := profileCache.Get(pubkeyHex); ok && profile != nil {
		if profile.DisplayName != "" {
			return "@" + profile.DisplayName
		}
		if profile.Name != "" {
			return "@" + profile.Name
		}
	}
	// Fall back to short npub
	if npub, err := encodeBech32Pubkey(pubkeyHex); err == nil {
		return "@" + formatNpubShort(npub)
	}
	return "@" + pubkeyHex[:12] + "..."
}

// htmlLoginHandler shows the login page (GET) or processes login (POST)
func htmlLoginHandler(w http.ResponseWriter, r *http.Request) {
	// Handle POST - delegate to submit handler (for bunker:// URLs)
	if r.Method == http.MethodPost {
		htmlLoginSubmitHandler(w, r)
		return
	}

	// Check if already logged in
	session := getSessionFromRequest(r)
	if session != nil && session.Connected {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	// Generate nostrconnect:// URL for the user
	nostrConnectURL, secret, err := GenerateNostrConnectURL(defaultNostrConnectRelays)
	if err != nil {
		log.Printf("Failed to generate nostrconnect URL: %v", err)
	}

	// Generate QR code
	var qrCodeDataURL string
	if nostrConnectURL != "" {
		qrCodeDataURL = generateQRCodeDataURL(nostrConnectURL)
	}

	// Get server pubkey for reconnect section
	var serverPubKey string
	if kp, err := GetServerKeypair(); err == nil {
		serverPubKey = hex.EncodeToString(kp.PubKey)
	}

	data := struct {
		Title           string
		Error           string
		Success         string
		NostrConnectURL string
		Secret          string
		QRCodeDataURL   template.URL
		ServerPubKey    string
	}{
		Title:           "Login with Nostr Connect",
		NostrConnectURL: nostrConnectURL,
		Secret:          secret,
		QRCodeDataURL:   template.URL(qrCodeDataURL),
		ServerPubKey:    serverPubKey,
	}

	// Check for error/success messages in query params
	data.Error = r.URL.Query().Get("error")
	data.Success = r.URL.Query().Get("success")

	renderLoginPage(w, data)
}

// htmlLoginSubmitHandler processes the bunker URL submission
func htmlLoginSubmitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	bunkerURL := strings.TrimSpace(r.FormValue("bunker_url"))
	if bunkerURL == "" {
		http.Redirect(w, r, "/html/login?error=Please+enter+a+bunker+URL", http.StatusSeeOther)
		return
	}

	// Parse bunker URL
	session, err := ParseBunkerURL(bunkerURL)
	if err != nil {
		log.Printf("Failed to parse bunker URL: %v", err)
		http.Redirect(w, r, "/html/login?error="+escapeURLParam(err.Error()), http.StatusSeeOther)
		return
	}

	// Attempt to connect (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("Connecting to bunker at %v...", session.Relays)
	if err := session.Connect(ctx); err != nil {
		log.Printf("Failed to connect to bunker: %v", err)
		http.Redirect(w, r, "/html/login?error="+escapeURLParam("Connection failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Store session
	bunkerSessions.Set(session)

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("User logged in: %s", hex.EncodeToString(session.UserPubKey))

	// Prefetch user profile in background so it's ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)

	http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&success=Logged+in+successfully", http.StatusSeeOther)
}

// htmlCheckConnectionHandler checks if a nostrconnect session is ready
func htmlCheckConnectionHandler(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if secret == "" {
		http.Redirect(w, r, "/html/login?error=Missing+connection+secret", http.StatusSeeOther)
		return
	}

	session := CheckConnection(secret)
	if session == nil {
		// Not connected yet
		http.Redirect(w, r, "/html/login?error=Connection+not+ready.+Make+sure+you+approved+in+your+signer+app,+then+try+again.&secret="+secret, http.StatusSeeOther)
		return
	}

	// Connected! Store session and set cookie
	bunkerSessions.Set(session)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("User logged in via nostrconnect: %s", hex.EncodeToString(session.UserPubKey))

	// Prefetch user profile in background so it's ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)

	http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&success=Logged+in+successfully", http.StatusSeeOther)
}

// htmlReconnectHandler tries to reconnect to an existing approved signer
func htmlReconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	signerPubKey := strings.TrimSpace(r.FormValue("signer_pubkey"))
	if signerPubKey == "" {
		http.Redirect(w, r, "/html/login?error=Please+enter+your+signer+pubkey", http.StatusSeeOther)
		return
	}

	// Handle npub format
	if strings.HasPrefix(signerPubKey, "npub1") {
		// Decode bech32 npub to hex
		decoded, err := decodeBech32Pubkey(signerPubKey)
		if err != nil {
			http.Redirect(w, r, "/html/login?error="+escapeURLParam("Invalid npub: "+err.Error()), http.StatusSeeOther)
			return
		}
		signerPubKey = decoded
	}

	// Validate hex
	if len(signerPubKey) != 64 {
		http.Redirect(w, r, "/html/login?error=Invalid+pubkey+length+(expected+64+hex+chars+or+npub)", http.StatusSeeOther)
		return
	}

	log.Printf("Attempting to reconnect to signer: %s", signerPubKey[:16])

	session, err := TryReconnectToSigner(signerPubKey, defaultNostrConnectRelays)
	if err != nil {
		log.Printf("Reconnect failed: %v", err)
		http.Redirect(w, r, "/html/login?error="+escapeURLParam("Reconnect failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Success! Store session and set cookie
	bunkerSessions.Set(session)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	log.Printf("User logged in via reconnect: %s", hex.EncodeToString(session.UserPubKey))

	// Prefetch user profile in background so it's ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)

	http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&success=Reconnected+successfully", http.StatusSeeOther)
}

// decodeBech32Pubkey decodes an npub to hex pubkey
func decodeBech32Pubkey(npub string) (string, error) {
	// Simple bech32 decoding - npub uses bech32
	if !strings.HasPrefix(npub, "npub1") {
		return "", errors.New("not an npub")
	}

	// Use standard bech32 decode
	_, data, err := bech32Decode(npub)
	if err != nil {
		return "", err
	}

	// Convert 5-bit groups to 8-bit bytes
	decoded, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return "", err
	}

	if len(decoded) != 32 {
		return "", errors.New("invalid pubkey length")
	}

	return hex.EncodeToString(decoded), nil
}

// bech32 charset
const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

func bech32Decode(bech string) (string, []byte, error) {
	if len(bech) < 8 {
		return "", nil, errors.New("too short")
	}

	// Find separator
	pos := strings.LastIndex(bech, "1")
	if pos < 1 || pos+7 > len(bech) {
		return "", nil, errors.New("invalid separator position")
	}

	hrp := bech[:pos]
	data := bech[pos+1:]

	// Decode data
	var values []byte
	for _, c := range data {
		idx := strings.IndexRune(bech32Charset, c)
		if idx == -1 {
			return "", nil, errors.New("invalid character")
		}
		values = append(values, byte(idx))
	}

	// Remove checksum (last 6 chars)
	if len(values) < 6 {
		return "", nil, errors.New("too short for checksum")
	}
	values = values[:len(values)-6]

	return hrp, values, nil
}

func bech32ConvertBits(data []byte, fromBits, toBits int, pad bool) ([]byte, error) {
	acc := 0
	bits := 0
	var ret []byte
	maxv := (1 << toBits) - 1

	for _, value := range data {
		acc = (acc << fromBits) | int(value)
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(toBits-bits))&maxv))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxv) != 0 {
		return nil, errors.New("invalid padding")
	}

	return ret, nil
}

// encodeBech32Pubkey encodes a hex pubkey to npub format
func encodeBech32Pubkey(hexPubkey string) (string, error) {
	pubkeyBytes, err := hex.DecodeString(hexPubkey)
	if err != nil {
		return "", err
	}
	if len(pubkeyBytes) != 32 {
		return "", errors.New("invalid pubkey length")
	}

	// Convert 8-bit bytes to 5-bit groups
	data, err := bech32ConvertBits(pubkeyBytes, 8, 5, true)
	if err != nil {
		return "", err
	}

	return bech32Encode("npub", data)
}

// bech32Encode encodes data with the given HRP
func bech32Encode(hrp string, data []byte) (string, error) {
	// Create checksum
	values := append([]byte{}, data...)
	checksum := bech32CreateChecksum(hrp, values)
	combined := append(values, checksum...)

	// Build result
	var result strings.Builder
	result.WriteString(hrp)
	result.WriteByte('1')
	for _, v := range combined {
		result.WriteByte(bech32Charset[v])
	}

	return result.String(), nil
}

// bech32 polymod for checksum calculation
func bech32Polymod(values []int) int {
	gen := []int{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	chk := 1
	for _, v := range values {
		top := chk >> 25
		chk = (chk&0x1ffffff)<<5 ^ v
		for i := 0; i < 5; i++ {
			if (top>>i)&1 != 0 {
				chk ^= gen[i]
			}
		}
	}
	return chk
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

func bech32CreateChecksum(hrp string, data []byte) []byte {
	values := bech32HrpExpand(hrp)
	for _, d := range data {
		values = append(values, int(d))
	}
	for i := 0; i < 6; i++ {
		values = append(values, 0)
	}
	polymod := bech32Polymod(values) ^ 1
	var checksum []byte
	for i := 0; i < 6; i++ {
		checksum = append(checksum, byte((polymod>>(5*(5-i)))&31))
	}
	return checksum
}

// htmlLogoutHandler logs out the user
func htmlLogoutHandler(w http.ResponseWriter, r *http.Request) {
	session := getSessionFromRequest(r)
	if session != nil {
		bunkerSessions.Delete(session.ID)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/html/login?success=Logged+out", http.StatusSeeOther)
}

// htmlPostNoteHandler handles note posting via POST form
func htmlPostNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error=Note+content+is+required", http.StatusSeeOther)
		return
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      1,
		Content:   content,
		Tags:      [][]string{},
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign event: %v", err)
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error="+escapeURLParam("Failed to sign: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Publish to relays
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
		"wss://nos.lol",
	}

	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published note: %s", signedEvent.ID)
	http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&success=Note+published", http.StatusSeeOther)
}

// htmlReplyHandler handles replying to a note via POST form
func htmlReplyHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("Reply handler called: method=%s", r.Method)

	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil {
		log.Printf("Reply failed: no session found")
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}
	if !session.Connected {
		log.Printf("Reply failed: session not connected")
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	log.Printf("Reply: session valid, user=%s", hex.EncodeToString(session.UserPubKey)[:16])

	content := strings.TrimSpace(r.FormValue("content"))
	replyTo := strings.TrimSpace(r.FormValue("reply_to"))
	replyToPubkey := strings.TrimSpace(r.FormValue("reply_to_pubkey"))

	if content == "" {
		http.Redirect(w, r, "/html/thread/"+replyTo+"?error=Reply+content+is+required", http.StatusSeeOther)
		return
	}

	if replyTo == "" {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error=Missing+reply+target", http.StatusSeeOther)
		return
	}

	// Build tags for reply
	// NIP-10: e tag with "reply" marker, p tag to mention the author
	tags := [][]string{
		{"e", replyTo, "", "reply"},
	}
	if replyToPubkey != "" {
		tags = append(tags, []string{"p", replyToPubkey})
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      1,
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign reply: %v", err)
		http.Redirect(w, r, "/html/thread/"+replyTo+"?error="+escapeURLParam("Failed to sign: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Publish to relays
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
		"wss://nos.lol",
	}

	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published reply: %s (to %s)", signedEvent.ID, replyTo)
	http.Redirect(w, r, "/html/thread/"+replyTo+"?success=Reply+published", http.StatusSeeOther)
}

// htmlReactHandler handles adding a reaction to a note
func htmlReactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	returnURL := strings.TrimSpace(r.FormValue("return_url"))
	reaction := strings.TrimSpace(r.FormValue("reaction"))

	if eventID == "" {
		http.Redirect(w, r, returnURL+"?error=Missing+event+ID", http.StatusSeeOther)
		return
	}

	if reaction == "" {
		reaction = "+"
	}

	if returnURL == "" {
		returnURL = "/html/timeline?kinds=1&limit=20"
	}

	// Build tags for reaction (NIP-25)
	tags := [][]string{
		{"e", eventID},
		{"k", "1"}, // Reacting to kind 1 (note)
	}
	if eventPubkey != "" {
		tags = append(tags, []string{"p", eventPubkey})
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      7,
		Content:   reaction,
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign reaction: %v", err)
		http.Redirect(w, r, returnURL+"?error="+escapeURLParam("Failed to sign: "+err.Error()), http.StatusSeeOther)
		return
	}

	// Get relays to publish to
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://nos.lol",
	}
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published reaction %s to event %s", reaction, eventID)
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// getSessionFromRequest retrieves the bunker session from the request cookie
func getSessionFromRequest(r *http.Request) *BunkerSession {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	return bunkerSessions.Get(cookie.Value)
}

func escapeURLParam(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "+"), ":", "%3A")
}

// publishEvent publishes a signed event to relays
func publishEvent(ctx context.Context, relays []string, event *Event) {
	for _, relay := range relays {
		go func(relayURL string) {
			if err := publishToRelay(ctx, relayURL, event); err != nil {
				log.Printf("Failed to publish to %s: %v", relayURL, err)
			} else {
				log.Printf("Published to %s", relayURL)
			}
		}(relay)
	}
	// Give relays a moment to receive
	time.Sleep(500 * time.Millisecond)
}

func publishToRelay(ctx context.Context, relayURL string, event *Event) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, relayURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	req := []interface{}{"EVENT", event}
	if err := conn.WriteJSON(req); err != nil {
		return fmt.Errorf("write failed: %v", err)
	}

	// Wait for OK response
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg []interface{}
	if err := conn.ReadJSON(&msg); err != nil {
		return fmt.Errorf("read OK failed: %v", err)
	}

	if len(msg) >= 4 {
		msgType, _ := msg[0].(string)
		if msgType == "OK" {
			eventID, _ := msg[1].(string)
			success, _ := msg[2].(bool)
			reason := ""
			if len(msg) > 3 {
				reason, _ = msg[3].(string)
			}
			if !success {
				return fmt.Errorf("relay rejected event %s: %s", eventID, reason)
			}
			log.Printf("Relay %s accepted event %s", relayURL, eventID[:16])
		}
	}

	return nil
}

var htmlLoginTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: #333;
      background: #f5f5f5;
      padding: 20px;
    }
    .container {
      max-width: 600px;
      margin: 0 auto;
      background: white;
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
      overflow: hidden;
    }
    header {
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      color: white;
      padding: 30px;
      text-align: center;
    }
    header h1 { font-size: 28px; margin-bottom: 8px; }
    .subtitle { opacity: 0.9; font-size: 14px; }
    nav {
      padding: 15px;
      background: #f8f9fa;
      border-bottom: 1px solid #dee2e6;
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    nav a {
      padding: 8px 16px;
      background: #667eea;
      color: white;
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s;
    }
    nav a:hover { background: #5568d3; }
    main { padding: 30px; }
    .alert {
      padding: 12px 16px;
      border-radius: 4px;
      margin-bottom: 20px;
      font-size: 14px;
    }
    .alert-error {
      background: #fee2e2;
      color: #dc2626;
      border: 1px solid #fecaca;
    }
    .alert-success {
      background: #dcfce7;
      color: #16a34a;
      border: 1px solid #bbf7d0;
    }
    .login-form {
      background: #f8f9fa;
      padding: 24px;
      border-radius: 8px;
      border: 1px solid #dee2e6;
    }
    .form-group {
      margin-bottom: 20px;
    }
    .form-group label {
      display: block;
      font-weight: 600;
      margin-bottom: 8px;
      color: #333;
    }
    .form-group input {
      width: 100%;
      padding: 12px;
      border: 1px solid #ced4da;
      border-radius: 4px;
      font-size: 14px;
      font-family: monospace;
    }
    .form-group input:focus {
      outline: none;
      border-color: #667eea;
      box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.2);
    }
    .form-help {
      font-size: 12px;
      color: #666;
      margin-top: 8px;
    }
    .submit-btn {
      width: 100%;
      padding: 14px;
      background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 16px;
      font-weight: 600;
      cursor: pointer;
      transition: transform 0.1s, box-shadow 0.2s;
    }
    .submit-btn:hover {
      transform: translateY(-1px);
      box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
    }
    .submit-btn:active {
      transform: translateY(0);
    }
    .info-section {
      margin-top: 30px;
      padding-top: 20px;
      border-top: 1px solid #dee2e6;
    }
    .info-section h3 {
      font-size: 16px;
      margin-bottom: 12px;
      color: #555;
    }
    .info-section p {
      font-size: 14px;
      color: #666;
      margin-bottom: 12px;
    }
    .info-section code {
      background: #e9ecef;
      padding: 2px 6px;
      border-radius: 3px;
      font-size: 13px;
    }
    .info-section ul {
      margin-left: 20px;
      font-size: 14px;
      color: #666;
    }
    .info-section li {
      margin-bottom: 8px;
    }
    .info-section a {
      color: #667eea;
    }
    footer {
      text-align: center;
      padding: 20px;
      background: #f8f9fa;
      color: #666;
      font-size: 13px;
      border-top: 1px solid #dee2e6;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>{{.Title}}</h1>
      <p class="subtitle">Zero-Trust Remote Signing (NIP-46)</p>
    </header>

    <nav>
      <a href="/html/timeline?kinds=1&limit=20&fast=1">Timeline</a>
    </nav>

    <main>
      {{if .Error}}
      <div class="alert alert-error">{{.Error}}</div>
      {{end}}
      {{if .Success}}
      <div class="alert alert-success">{{.Success}}</div>
      {{end}}

      {{if .NostrConnectURL}}
      <div class="login-form" style="margin-bottom: 24px;">
        <h3 style="margin-bottom: 16px; color: #333;">Option 1: Scan with Signer App</h3>
        {{if .QRCodeDataURL}}
        <div style="text-align: center; margin-bottom: 16px;">
          <img src="{{.QRCodeDataURL}}" alt="Scan this QR code with your signer app" style="max-width: 256px; border: 4px solid white; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1);">
        </div>
        <p style="font-size: 14px; color: #666; margin-bottom: 16px; text-align: center;">
          Scan this QR code with your signer app (Amber, etc.)
        </p>
        {{end}}
        <details style="margin-bottom: 16px;">
          <summary style="cursor: pointer; font-size: 13px; color: #666;">Or copy URL manually</summary>
          <div style="background: #e9ecef; padding: 12px; border-radius: 4px; font-family: monospace; font-size: 11px; word-break: break-all; margin-top: 8px;">
            {{.NostrConnectURL}}
          </div>
        </details>
        <a href="/html/check-connection?secret={{.Secret}}" class="submit-btn" style="display: block; text-align: center; text-decoration: none;">
          Check Connection
        </a>
        <p class="form-help" style="margin-top: 12px;">
          After approving in your signer app, click the button above to complete login.
        </p>
      </div>

      <div style="text-align: center; color: #999; margin: 20px 0; font-size: 14px;">
        &mdash; or &mdash;
      </div>
      {{end}}

      <form class="login-form" method="POST" action="/html/login">
        <h3 style="margin-bottom: 16px; color: #333;">Option 2: Paste Bunker URL</h3>
        <div class="form-group">
          <label for="bunker_url">Bunker URL</label>
          <input type="text" id="bunker_url" name="bunker_url"
                 placeholder="bunker://pubkey?relay=wss://...&secret=..."
                 required autocomplete="off">
          <p class="form-help">
            Paste your bunker:// URL from your Nostr signer app (nsec.app, Amber, etc.)
          </p>
        </div>
        <button type="submit" class="submit-btn">Connect</button>
      </form>

      <div style="text-align: center; color: #999; margin: 20px 0; font-size: 14px;">
        &mdash; or &mdash;
      </div>

      <form class="login-form" method="POST" action="/html/reconnect">
        <h3 style="margin-bottom: 16px; color: #333;">Option 3: Reconnect to Existing Bunker</h3>
        <p style="font-size: 13px; color: #666; margin-bottom: 16px; background: #e9ecef; padding: 12px; border-radius: 4px;">
          <strong>This server's pubkey:</strong><br>
          <code style="font-size: 11px; word-break: break-all;">{{.ServerPubKey}}</code><br>
          <span style="font-size: 11px;">Look for this in your signer's approved connections list.</span>
        </p>
        <div class="form-group">
          <label for="signer_pubkey">Signer Public Key</label>
          <input type="text" id="signer_pubkey" name="signer_pubkey"
                 placeholder="npub1... or hex pubkey"
                 required autocomplete="off">
          <p class="form-help">
            Enter the pubkey your signer uses for NIP-46 (found in Amber under the approved connection details).
          </p>
        </div>
        <button type="submit" class="submit-btn">Reconnect</button>
      </form>

      <div class="info-section">
        <h3>How it works</h3>
        <p>
          This login uses <strong>NIP-46 (Nostr Connect)</strong> - your private key never leaves your signer app.
          The server only sees your public key and cannot sign events without your approval.
        </p>
        <h3>Supported signers</h3>
        <ul>
          <li><a href="https://nsec.app" target="_blank">nsec.app</a> - Web-based remote signer</li>
          <li><a href="https://github.com/greenart7c3/Amber" target="_blank">Amber</a> - Android signer</li>
          <li>Any NIP-46 compatible bunker</li>
        </ul>
      </div>
    </main>

    <footer>
      <p>Pure HTML hypermedia - no JavaScript required</p>
    </footer>
  </div>
</body>
</html>
`

func renderLoginPage(w http.ResponseWriter, data interface{}) {
	tmpl, err := template.New("login").Parse(htmlLoginTemplate)
	if err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl.Execute(w, data)
}
