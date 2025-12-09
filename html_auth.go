package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

const sessionCookieName = "nostr_session"
const sessionMaxAge = 24 * time.Hour

// sanitizeErrorForUser returns a user-safe error message, logging the full error
// This prevents leaking internal details like relay URLs, file paths, etc.
func sanitizeErrorForUser(context string, err error) string {
	// Log the full error for debugging
	log.Printf("%s: %v", context, err)

	// Return generic messages based on context
	errStr := err.Error()
	switch {
	case strings.Contains(errStr, "timeout"):
		return "Connection timed out"
	case strings.Contains(errStr, "connection refused"):
		return "Could not connect to relay"
	case strings.Contains(errStr, "rate limit"):
		return "Rate limit exceeded, please try again later"
	case strings.Contains(errStr, "invalid") || strings.Contains(errStr, "Invalid"):
		return "Invalid input format"
	case strings.Contains(errStr, "not connected"):
		return "Not connected to signer"
	default:
		return "Operation failed"
	}
}

// Cached auth templates - initialized at startup
var (
	cachedQuoteTemplate *template.Template
	cachedLoginTemplate *template.Template
)

// initAuthTemplates compiles auth templates once at startup for performance
func initAuthTemplates() {
	var err error

	// Compile quote template
	cachedQuoteTemplate, err = template.New("quote").Funcs(template.FuncMap{
		"formatTime": func(ts int64) string {
			return formatRelativeTime(ts)
		},
	}).Parse(htmlQuoteTemplate)
	if err != nil {
		log.Fatalf("Failed to compile quote template: %v", err)
	}

	// Compile login template
	cachedLoginTemplate, err = template.New("login").Parse(htmlLoginTemplate)
	if err != nil {
		log.Fatalf("Failed to compile login template: %v", err)
	}

	log.Printf("Auth templates compiled successfully")
}

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

// prefetchUserContactList fetches the user's contact list and populates the session
// This should be called after login to cache who the user follows
func prefetchUserContactList(session *BunkerSession, relays []string) {
	go func() {
		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		log.Printf("Prefetching contact list for logged-in user: %s", pubkeyHex[:16])

		contacts := fetchContactList(relays, pubkeyHex)
		if contacts != nil {
			session.mu.Lock()
			session.FollowingPubkeys = contacts
			session.mu.Unlock()
			log.Printf("Cached %d followed pubkeys for user %s", len(contacts), pubkeyHex[:16])
		}
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

	// Get theme from cookie
	themeClass, _ := getThemeFromRequest(r)

	data := struct {
		Title           string
		Error           string
		Success         string
		NostrConnectURL string
		Secret          string
		QRCodeDataURL   template.URL
		ServerPubKey    string
		ThemeClass      string
	}{
		Title:           "Login with Nostr Connect",
		NostrConnectURL: nostrConnectURL,
		Secret:          secret,
		QRCodeDataURL:   template.URL(qrCodeDataURL),
		ServerPubKey:    serverPubKey,
		ThemeClass:      themeClass,
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
		http.Redirect(w, r, "/html/login?error="+escapeURLParam(sanitizeErrorForUser("Parse bunker URL", err)), http.StatusSeeOther)
		return
	}

	// Attempt to connect (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("Connecting to bunker...")
	if err := session.Connect(ctx); err != nil {
		http.Redirect(w, r, "/html/login?error="+escapeURLParam(sanitizeErrorForUser("Connect to bunker", err)), http.StatusSeeOther)
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

	// Prefetch user profile and contact list in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)

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
		http.Redirect(w, r, "/html/login?error=Connection+not+ready.+Make+sure+you+approved+in+your+signer+app,+then+try+again.&secret="+escapeURLParam(secret), http.StatusSeeOther)
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

	// Prefetch user profile and contact list in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)

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
			http.Redirect(w, r, "/html/login?error="+escapeURLParam("Invalid npub format"), http.StatusSeeOther)
			return
		}
		signerPubKey = decoded
	}

	// Validate hex
	if len(signerPubKey) != 64 {
		http.Redirect(w, r, "/html/login?error=Invalid+pubkey+length+(expected+64+hex+chars+or+npub)", http.StatusSeeOther)
		return
	}

	log.Printf("Attempting to reconnect to signer...")

	session, err := TryReconnectToSigner(signerPubKey, defaultNostrConnectRelays)
	if err != nil {
		http.Redirect(w, r, "/html/login?error="+escapeURLParam(sanitizeErrorForUser("Reconnect to signer", err)), http.StatusSeeOther)
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

	// Prefetch user profile and contact list in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)

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

func encodeBech32EventID(hexEventID string) (string, error) {
	eventIDBytes, err := hex.DecodeString(hexEventID)
	if err != nil {
		return "", err
	}
	if len(eventIDBytes) != 32 {
		return "", errors.New("invalid event ID length")
	}

	// Convert 8-bit bytes to 5-bit groups
	data, err := bech32ConvertBits(eventIDBytes, 8, 5, true)
	if err != nil {
		return "", err
	}

	return bech32Encode("note", data)
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

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
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
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
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

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
		return
	}

	log.Printf("Reply: session valid, user=%s", hex.EncodeToString(session.UserPubKey)[:16])

	content := strings.TrimSpace(r.FormValue("content"))
	replyTo := strings.TrimSpace(r.FormValue("reply_to"))
	replyToPubkey := strings.TrimSpace(r.FormValue("reply_to_pubkey"))

	// Validate event ID first to prevent path injection
	if replyTo == "" || !isValidEventID(replyTo) {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error=Invalid+reply+target", http.StatusSeeOther)
		return
	}

	if content == "" {
		http.Redirect(w, r, "/html/thread/"+replyTo+"?error=Reply+content+is+required", http.StatusSeeOther)
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
		http.Redirect(w, r, "/html/thread/"+replyTo+"?error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
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

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")))
	reaction := strings.TrimSpace(r.FormValue("reaction"))

	if eventID == "" || !isValidEventID(eventID) {
		http.Redirect(w, r, returnURL+"?error=Invalid+event+ID", http.StatusSeeOther)
		return
	}

	if reaction == "" {
		reaction = "+"
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
		separator := "?"
		if strings.Contains(returnURL, "?") {
			separator = "&"
		}
		http.Redirect(w, r, returnURL+separator+"error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
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

// htmlRepostHandler handles reposting a note (kind 6)
func htmlRepostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")))

	if eventID == "" || !isValidEventID(eventID) {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&error=Invalid+event+ID", http.StatusSeeOther)
		return
	}

	// Build tags for repost (NIP-18)
	// e tag for the reposted event, p tag to mention the original author
	tags := [][]string{
		{"e", eventID, ""},
	}
	if eventPubkey != "" {
		tags = append(tags, []string{"p", eventPubkey})
	}

	// Create unsigned event (kind 6 = repost)
	event := UnsignedEvent{
		Kind:      6,
		Content:   "", // Repost content is typically empty
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign repost: %v", err)
		separator := "?"
		if strings.Contains(returnURL, "?") {
			separator = "&"
		}
		http.Redirect(w, r, returnURL+separator+"error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
		return
	}

	// Get relays to publish to
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
		"wss://nos.lol",
	}
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published repost: %s (reposting %s)", signedEvent.ID, eventID)

	// Add success message to return URL
	if strings.Contains(returnURL, "?") {
		returnURL += "&success=Reposted"
	} else {
		returnURL += "?success=Reposted"
	}
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// htmlBookmarkHandler handles adding/removing a note from user's bookmarks (kind 10003)
func htmlBookmarkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	action := strings.TrimSpace(r.FormValue("action")) // "add" or "remove"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")))

	if eventID == "" || !isValidEventID(eventID) {
		http.Redirect(w, r, returnURL+"?error=Invalid+event+ID", http.StatusSeeOther)
		return
	}

	if action != "add" && action != "remove" {
		action = "add"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	userPubkey := hex.EncodeToString(session.UserPubKey)

	// Get relays
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://nos.lol",
	}
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	// Fetch user's current bookmark list (kind 10003)
	existingTags := [][]string{}
	bookmarkEvents := fetchKind10003(relays, userPubkey)
	if len(bookmarkEvents) > 0 {
		// Use the most recent bookmark list
		existingTags = bookmarkEvents[0].Tags
	}

	// Build new tags list
	var newTags [][]string
	found := false

	for _, tag := range existingTags {
		if len(tag) >= 2 && tag[0] == "e" && tag[1] == eventID {
			found = true
			if action == "remove" {
				// Skip this tag (removes the bookmark)
				continue
			}
		}
		newTags = append(newTags, tag)
	}

	// If adding and not found, add new e tag
	if action == "add" && !found {
		newTags = append(newTags, []string{"e", eventID})
	}

	// If removing and not found, nothing to do
	if action == "remove" && !found {
		http.Redirect(w, r, returnURL, http.StatusSeeOther)
		return
	}

	// Create the kind 10003 event (replaceable)
	event := UnsignedEvent{
		Kind:      10003,
		Content:   "",
		Tags:      newTags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign bookmark list: %v", err)
		separator := "?"
		if strings.Contains(returnURL, "?") {
			separator = "&"
		}
		http.Redirect(w, r, returnURL+separator+"error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
		return
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	log.Printf("Published bookmark list update: %s (action=%s, event=%s)", signedEvent.ID, action, eventID)
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// fetchKind10003 fetches the user's bookmark list (kind 10003)
func fetchKind10003(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{10003},
		Authors: []string{pubkey},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelays(relays, filter)
	return events
}

// htmlQuoteHandler handles both displaying the quote form (GET) and submitting (POST)
func htmlQuoteHandler(w http.ResponseWriter, r *http.Request) {
	// Extract event ID from path: /html/quote/{eventId}
	eventID := strings.TrimPrefix(r.URL.Path, "/html/quote/")
	if eventID == "" || !isValidEventID(eventID) {
		http.Error(w, "Invalid event ID", http.StatusBadRequest)
		return
	}

	themeClass, themeLabel := getThemeFromRequest(r)

	// Check login status
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	if r.Method == http.MethodPost {
		// Handle quote submission
		if !loggedIn {
			http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
			return
		}

		// Validate CSRF token
		csrfToken := r.FormValue("csrf_token")
		if !validateCSRFToken(session.ID, csrfToken) {
			http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
			return
		}

		content := strings.TrimSpace(r.FormValue("content"))
		quotedPubkey := strings.TrimSpace(r.FormValue("quoted_pubkey"))

		if content == "" {
			http.Redirect(w, r, "/html/quote/"+eventID+"?error=Quote+content+is+required", http.StatusSeeOther)
			return
		}

		// Convert event ID to note1 bech32 format for embedding in content
		noteID, err := encodeBech32EventID(eventID)
		if err != nil {
			log.Printf("Failed to encode event ID: %v", err)
			noteID = eventID // fallback to hex
		}

		// Append nostr: reference to content
		fullContent := content + "\n\nnostr:" + noteID

		// Build tags for quote (NIP-18)
		// q tag for the quoted event, p tag to mention the original author
		tags := [][]string{
			{"q", eventID, ""},
		}
		if quotedPubkey != "" {
			tags = append(tags, []string{"p", quotedPubkey})
		}

		// Create unsigned event
		event := UnsignedEvent{
			Kind:      1,
			Content:   fullContent,
			Tags:      tags,
			CreatedAt: time.Now().Unix(),
		}

		// Sign via bunker
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		signedEvent, err := session.SignEvent(ctx, event)
		if err != nil {
			log.Printf("Failed to sign quote: %v", err)
			http.Redirect(w, r, "/html/quote/"+eventID+"?error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
			return
		}

		// Publish to relays
		relays := []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
			"wss://nos.lol",
		}
		if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
			relays = session.UserRelayList.Write
		}

		publishEvent(ctx, relays, signedEvent)

		log.Printf("Published quote: %s (quoting %s)", signedEvent.ID, eventID)
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20&success=Quote+published", http.StatusSeeOther)
		return
	}

	// GET: Show quote form with preview of the quoted note
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://relay.primal.net",
		"wss://nos.lol",
		"wss://nostr.mom",
	}

	// Fetch the event to be quoted
	events := fetchEventByID(relays, eventID)
	if len(events) == 0 {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	quotedEvent := events[0]

	// Fetch author profile
	var authorProfile *ProfileInfo
	profiles := fetchProfiles(relays, []string{quotedEvent.PubKey})
	if p, ok := profiles[quotedEvent.PubKey]; ok {
		authorProfile = p
	}

	// Get user display name if logged in
	userDisplayName := ""
	if loggedIn {
		userPubkey := hex.EncodeToString(session.UserPubKey)
		userProfiles := fetchProfiles(relays, []string{userPubkey})
		if p, ok := userProfiles[userPubkey]; ok {
			if p.DisplayName != "" {
				userDisplayName = p.DisplayName
			} else if p.Name != "" {
				userDisplayName = p.Name
			}
		}
		if userDisplayName == "" {
			npub, _ := encodeBech32Pubkey(userPubkey)
			userDisplayName = formatNpubShort(npub)
		}
	}

	// Prepare data for template
	npub, _ := encodeBech32Pubkey(quotedEvent.PubKey)

	// Generate CSRF token for forms (use session ID if logged in)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	data := struct {
		Title           string
		ThemeClass      string
		ThemeLabel      string
		LoggedIn        bool
		UserDisplayName string
		QuotedEvent     Event
		AuthorProfile   *ProfileInfo
		NpubShort       string
		Error           string
		GeneratedAt     time.Time
		CSRFToken       string
	}{
		Title:           "Quote Note",
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		LoggedIn:        loggedIn,
		UserDisplayName: userDisplayName,
		QuotedEvent:     quotedEvent,
		AuthorProfile:   authorProfile,
		NpubShort:       formatNpubShort(npub),
		Error:           r.URL.Query().Get("error"),
		GeneratedAt:     time.Now(),
		CSRFToken:       csrfToken,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cachedQuoteTemplate.Execute(w, data)
}

// getSessionFromRequest retrieves the bunker session from the request cookie
func getSessionFromRequest(r *http.Request) *BunkerSession {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	return bunkerSessions.Get(cookie.Value)
}

// validEventID matches a 64-character lowercase hex string (nostr event ID)
var validEventID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// isValidEventID checks if a string is a valid nostr event ID (64 hex chars)
func isValidEventID(id string) bool {
	return validEventID.MatchString(id)
}

// sanitizeReturnURL ensures the return URL is a local path to prevent open redirects
func sanitizeReturnURL(returnURL string) string {
	if returnURL == "" {
		return "/html/timeline?kinds=1&limit=20"
	}
	// Must start with / and not // (which could be protocol-relative URL)
	if !strings.HasPrefix(returnURL, "/") || strings.HasPrefix(returnURL, "//") {
		return "/html/timeline?kinds=1&limit=20"
	}
	return returnURL
}

// escapeURLParam properly escapes a string for use in URL query parameters
func escapeURLParam(s string) string {
	return url.QueryEscape(s)
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
	pc, err := relayPool.Get(ctx, relayURL)
	if err != nil {
		return err
	}
	defer relayPool.Release(pc)

	req := []interface{}{"EVENT", event}
	if err := pc.WriteJSON(req); err != nil {
		relayPool.Close(pc)
		return fmt.Errorf("write failed: %v", err)
	}

	// Wait for OK response
	pc.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg []interface{}
	if err := pc.ReadJSON(&msg); err != nil {
		relayPool.Close(pc)
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

var htmlQuoteTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --shadow: rgba(0,0,0,0.1);
    }
    @media (prefers-color-scheme: dark) {
      :root:not(.light) {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-card: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #71717a;
        --text-content: #e4e4e7;
        --border-color: #333333;
        --border-light: #333333;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --success: #4ade80;
        --error-bg: #2d1f1f;
        --error-border: #7f1d1d;
        --error-accent: #f87171;
        --shadow: rgba(0,0,0,0.3);
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-card: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #71717a;
      --text-content: #e4e4e7;
      --border-color: #333333;
      --border-light: #333333;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --success: #4ade80;
      --error-bg: #2d1f1f;
      --error-border: #7f1d1d;
      --error-accent: #f87171;
      --shadow: rgba(0,0,0,0.3);
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-card: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #999999;
      --text-content: #24292e;
      --border-color: #e1e4e8;
      --border-light: #dee2e6;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --success: #2e7d32;
      --error-bg: #fff5f5;
      --error-border: #fecaca;
      --error-accent: #dc2626;
      --shadow: rgba(0,0,0,0.1);
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
      background: var(--bg-page);
      color: var(--text-primary);
      line-height: 1.6;
      min-height: 100vh;
    }
    .container {
      max-width: 800px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px var(--shadow);
    }
    .flex-center { display: flex; align-items: center; }
    .gap-md { gap: 12px; }
    .text-muted { color: var(--text-muted); }
    .text-sm { font-size: 13px; }
    nav {
      display: flex;
      gap: 8px;
      padding: 12px 16px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-light);
      align-items: center;
    }
    .nav-tab {
      padding: 8px 16px;
      text-decoration: none;
      color: var(--text-secondary);
      border-radius: 4px;
      font-size: 14px;
      transition: all 0.2s;
    }
    .nav-tab:hover { background: var(--bg-card); color: var(--text-primary); }
    .nav-tab.active {
      background: var(--accent);
      color: white;
    }
    main { padding: 20px; overflow: hidden; }
    .error-message {
      background: var(--error-bg);
      color: var(--error-accent);
      border: 1px solid var(--error-border);
      padding: 12px;
      border-radius: 4px;
      margin-bottom: 16px;
    }
    .quoted-note {
      background: var(--bg-secondary);
      border: 1px solid var(--border-color);
      border-left: 3px solid var(--accent);
      border-radius: 4px;
      padding: 16px;
      margin-bottom: 20px;
    }
    .quoted-author {
      display: flex;
      align-items: center;
      gap: 10px;
      margin-bottom: 10px;
    }
    .quoted-avatar {
      width: 36px;
      height: 36px;
      border-radius: 50%;
      object-fit: cover;
      background: var(--bg-tertiary);
    }
    .quoted-name {
      font-weight: 600;
      color: var(--text-primary);
    }
    .quoted-npub {
      font-family: monospace;
      font-size: 11px;
      color: var(--text-muted);
    }
    .quoted-content {
      font-size: 14px;
      line-height: 1.5;
      color: var(--text-content);
      white-space: pre-wrap;
      word-wrap: break-word;
    }
    .quoted-meta {
      font-size: 12px;
      color: var(--text-muted);
      margin-top: 10px;
    }
    .quote-form {
      background: var(--bg-card);
      padding: 16px;
      border-radius: 8px;
    }
    .form-label {
      font-size: 13px;
      color: var(--text-secondary);
      margin-bottom: 10px;
    }
    .form-label strong {
      color: var(--success);
      font-weight: 500;
    }
    textarea {
      width: 100%;
      padding: 12px;
      border: 1px solid var(--border-color);
      border-radius: 4px;
      font-size: 14px;
      font-family: inherit;
      min-height: 100px;
      resize: vertical;
      margin-bottom: 12px;
      background: var(--bg-input);
      color: var(--text-primary);
    }
    textarea:focus {
      outline: none;
      border-color: var(--accent);
    }
    .submit-btn {
      padding: 10px 24px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%);
      color: white;
      border: none;
      border-radius: 4px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: opacity 0.2s;
    }
    .submit-btn:hover { opacity: 0.9; }
    .login-prompt {
      text-align: center;
      padding: 40px 20px;
      color: var(--text-secondary);
    }
    .login-prompt a {
      color: var(--accent);
      text-decoration: none;
      font-weight: 500;
    }
    /* Utility classes */
    .ml-auto { margin-left: auto; }
    .text-xs { font-size: 12px; }
    .inline-form { display: inline; margin: 0; }
    .ghost-btn {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-family: inherit;
      padding: 0;
    }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-light);
    }
  </style>
</head>
<body>
  <div class="container">
    <nav>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=follows" class="nav-tab">Follows</a>
      {{end}}
      <a href="/html/timeline?kinds=1&limit=20&feed=global" class="nav-tab">Global</a>
      {{if .LoggedIn}}
      <a href="/html/timeline?kinds=1&limit=20&feed=me" class="nav-tab">Me</a>
      {{end}}
      <div class="ml-auto flex-center gap-md">
        {{if .LoggedIn}}
        <a href="/html/notifications" class="text-muted text-sm" title="Notifications">🔔</a>
        <form method="POST" action="/html/logout" class="inline-form">
          <button type="submit" class="ghost-btn text-muted text-sm">Logout</button>
        </form>
        {{else}}
        <a href="/html/login" class="text-muted text-sm">Login</a>
        {{end}}
      </div>
    </nav>

    <main>
      {{if .Error}}
      <div class="error-message">{{.Error}}</div>
      {{end}}

      <div class="quoted-note">
        <div class="quoted-author">
          {{if and .AuthorProfile .AuthorProfile.Picture}}
          <img class="quoted-avatar" src="{{.AuthorProfile.Picture}}" alt="">
          {{else}}
          <img class="quoted-avatar" src="/static/avatar.jpg" alt="">
          {{end}}
          <div>
            {{if .AuthorProfile}}
              {{if or .AuthorProfile.DisplayName .AuthorProfile.Name}}
              <div class="quoted-name">{{if .AuthorProfile.DisplayName}}{{.AuthorProfile.DisplayName}}{{else}}{{.AuthorProfile.Name}}{{end}}</div>
              {{if .AuthorProfile.Nip05}}<div class="quoted-npub">{{.AuthorProfile.Nip05}}</div>{{end}}
              {{else if .AuthorProfile.Nip05}}
              <div class="quoted-name">{{.AuthorProfile.Nip05}}</div>
              {{else}}
              <div class="quoted-name">{{.NpubShort}}</div>
              {{end}}
            {{else}}
              <div class="quoted-name">{{.NpubShort}}</div>
            {{end}}
          </div>
        </div>
        <div class="quoted-content">{{.QuotedEvent.Content}}</div>
        <div class="quoted-meta">{{formatTime .QuotedEvent.CreatedAt}}</div>
      </div>

      {{if .LoggedIn}}
      <form method="POST" class="quote-form">
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <input type="hidden" name="quoted_pubkey" value="{{.QuotedEvent.PubKey}}">
        <div class="form-label">Quoting as: <strong>{{.UserDisplayName}}</strong></div>
        <textarea name="content" placeholder="Add your commentary..." required autofocus></textarea>
        <button type="submit" class="submit-btn">Post Commentary</button>
      </form>
      {{else}}
      <div class="login-prompt">
        <p>Please <a href="/html/login">login</a> to quote this note.</p>
      </div>
      {{end}}
    </main>

    <footer>
      <p>Generated: {{.GeneratedAt.Format "15:04:05"}} · Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
</body>
</html>
`

var htmlLoginTemplate = `<!DOCTYPE html>
<html lang="en"{{if .ThemeClass}} class="{{.ThemeClass}}"{{end}}>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>{{.Title}} - Nostr Hypermedia</title>
  <link rel="icon" href="/static/favicon.ico" />
  <style>
    :root {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-code: #e9ecef;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #555555;
      --border-color: #dee2e6;
      --border-input: #ced4da;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --error-bg: #fee2e2;
      --error-text: #dc2626;
      --error-border: #fecaca;
      --success-bg: #dcfce7;
      --success-text: #16a34a;
      --success-border: #bbf7d0;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg-page: #121212;
        --bg-container: #1e1e1e;
        --bg-secondary: #252525;
        --bg-input: #2a2a2a;
        --bg-code: #333333;
        --text-primary: #e4e4e7;
        --text-secondary: #a1a1aa;
        --text-muted: #a1a1aa;
        --border-color: #333333;
        --border-input: #444444;
        --accent: #818cf8;
        --accent-hover: #6366f1;
        --accent-secondary: #a78bfa;
        --error-bg: #2d1f1f;
        --error-text: #f87171;
        --error-border: #7f1d1d;
        --success-bg: #1f2d1f;
        --success-text: #4ade80;
        --success-border: #166534;
      }
    }
    html.dark {
      --bg-page: #121212;
      --bg-container: #1e1e1e;
      --bg-secondary: #252525;
      --bg-input: #2a2a2a;
      --bg-code: #333333;
      --text-primary: #e4e4e7;
      --text-secondary: #a1a1aa;
      --text-muted: #a1a1aa;
      --border-color: #333333;
      --border-input: #444444;
      --accent: #818cf8;
      --accent-hover: #6366f1;
      --accent-secondary: #a78bfa;
      --error-bg: #2d1f1f;
      --error-text: #f87171;
      --error-border: #7f1d1d;
      --success-bg: #1f2d1f;
      --success-text: #4ade80;
      --success-border: #166534;
    }
    html.light {
      --bg-page: #f5f5f5;
      --bg-container: #ffffff;
      --bg-secondary: #f8f9fa;
      --bg-input: #ffffff;
      --bg-code: #e9ecef;
      --text-primary: #333333;
      --text-secondary: #666666;
      --text-muted: #555555;
      --border-color: #dee2e6;
      --border-input: #ced4da;
      --accent: #667eea;
      --accent-hover: #5568d3;
      --accent-secondary: #764ba2;
      --error-bg: #fee2e2;
      --error-text: #dc2626;
      --error-border: #fecaca;
      --success-bg: #dcfce7;
      --success-text: #16a34a;
      --success-border: #bbf7d0;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    @keyframes flashFadeOut {
      0%, 60% { opacity: 1; max-height: 100px; padding: 12px 16px; margin-bottom: 20px; }
      100% { opacity: 0; max-height: 0; padding: 0; margin-bottom: 0; overflow: hidden; }
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
      line-height: 1.6;
      color: var(--text-primary);
      background: var(--bg-page);
      padding: 20px;
    }
    .container {
      max-width: 600px;
      margin: 0 auto;
      background: var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
      overflow: hidden;
    }
    header {
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%);
      color: white;
      padding: 12px;
      text-align: center;
      height: 60px;
      box-sizing: border-box;
      display: flex;
      align-items: center;
      justify-content: center;
      border-radius: 8px 8px 0 0;
    }
    header h1 { font-size: 18px; margin: 0; font-weight: 600; }
    nav {
      padding: 15px;
      background: var(--bg-secondary);
      border-bottom: 1px solid var(--border-color);
      display: flex;
      gap: 10px;
      flex-wrap: wrap;
    }
    nav a {
      padding: 8px 16px;
      background: var(--accent);
      color: white;
      text-decoration: none;
      border-radius: 4px;
      font-size: 14px;
      transition: background 0.2s;
    }
    nav a:hover { background: var(--accent-hover); }
    main { padding: 30px; }
    .alert {
      padding: 12px 16px;
      border-radius: 4px;
      margin-bottom: 20px;
      font-size: 14px;
    }
    .alert-error {
      background: var(--error-bg);
      color: var(--error-text);
      border: 1px solid var(--error-border);
    }
    .alert-success {
      background: var(--success-bg);
      color: var(--success-text);
      border: 1px solid var(--success-border);
      border-radius: 4px;
      font-size: 14px;
      animation: flashFadeOut 3s ease-out forwards;
    }
    .login-form {
      background: var(--bg-secondary);
      padding: 24px;
      border-radius: 8px;
      border: 1px solid var(--border-color);
    }
    .form-group {
      margin-bottom: 20px;
    }
    .form-group label {
      display: block;
      font-weight: 600;
      margin-bottom: 8px;
      color: var(--text-primary);
    }
    .form-group input {
      width: 100%;
      padding: 12px;
      border: 1px solid var(--border-input);
      border-radius: 4px;
      font-size: 14px;
      font-family: monospace;
      background: var(--bg-input);
      color: var(--text-primary);
    }
    .form-group input:focus {
      outline: none;
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.2);
    }
    .form-help {
      font-size: 12px;
      color: var(--text-secondary);
      margin-top: 8px;
    }
    .submit-btn {
      width: 100%;
      padding: 14px;
      background: linear-gradient(135deg, var(--accent) 0%, var(--accent-secondary) 100%);
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
      border-top: 1px solid var(--border-color);
    }
    .info-section h3 {
      font-size: 16px;
      margin-bottom: 12px;
      color: var(--text-muted);
    }
    .info-section p {
      font-size: 14px;
      color: var(--text-secondary);
      margin-bottom: 12px;
    }
    .info-section code {
      background: var(--bg-code);
      padding: 2px 6px;
      border-radius: 3px;
      font-size: 13px;
    }
    .info-section ul {
      margin-left: 20px;
      font-size: 14px;
      color: var(--text-secondary);
    }
    .info-section li {
      margin-bottom: 8px;
    }
    .info-section a {
      color: var(--accent);
    }
    /* Login form specific */
    .login-section { margin-bottom: 24px; }
    .login-section h3 {
      margin-bottom: 16px;
      color: var(--text-primary);
    }
    .qr-container {
      text-align: center;
      margin-bottom: 16px;
    }
    .qr-code {
      max-width: 256px;
      border: 4px solid var(--bg-container);
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.1);
    }
    .qr-help {
      font-size: 14px;
      color: var(--text-secondary);
      margin-bottom: 16px;
      text-align: center;
    }
    .url-details {
      margin-bottom: 16px;
    }
    .url-details summary {
      cursor: pointer;
      font-size: 13px;
      color: var(--text-secondary);
    }
    .url-box {
      background: var(--bg-code);
      padding: 12px;
      border-radius: 4px;
      font-family: monospace;
      font-size: 11px;
      word-break: break-all;
      margin-top: 8px;
      color: var(--text-primary);
    }
    .submit-btn-block {
      display: block;
      text-align: center;
      text-decoration: none;
    }
    .divider {
      text-align: center;
      color: var(--text-muted);
      margin: 20px 0;
      font-size: 14px;
    }
    .server-info {
      font-size: 13px;
      color: var(--text-secondary);
      margin-bottom: 16px;
      background: var(--bg-code);
      padding: 12px;
      border-radius: 4px;
    }
    .server-info code {
      font-size: 11px;
      word-break: break-all;
    }
    .server-info-note {
      font-size: 11px;
    }
    footer {
      text-align: center;
      padding: 20px;
      background: var(--bg-secondary);
      color: var(--text-secondary);
      font-size: 13px;
      border-top: 1px solid var(--border-color);
      border-radius: 0 0 8px 8px;
    }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <h1>{{.Title}}</h1>
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
      <div class="login-form login-section">
        <h3>Option 1: Scan with Signer App</h3>
        {{if .QRCodeDataURL}}
        <div class="qr-container">
          <img src="{{.QRCodeDataURL}}" alt="Scan this QR code with your signer app" class="qr-code">
        </div>
        <p class="qr-help">
          Scan this QR code with your signer app (Amber, etc.)
        </p>
        {{end}}
        <details class="url-details">
          <summary>Or copy URL manually</summary>
          <div class="url-box">
            {{.NostrConnectURL}}
          </div>
        </details>
        <a href="/html/check-connection?secret={{.Secret}}" class="submit-btn submit-btn-block">
          Check Connection
        </a>
        <p class="form-help">
          After approving in your signer app, click the button above to complete login.
        </p>
      </div>

      <div class="divider">
        &mdash; or &mdash;
      </div>
      {{end}}

      <form class="login-form login-section" method="POST" action="/html/login">
        <h3>Option 2: Paste Bunker URL</h3>
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

      <div class="divider">
        &mdash; or &mdash;
      </div>

      <form class="login-form login-section" method="POST" action="/html/reconnect">
        <h3>Option 3: Reconnect to Existing Bunker</h3>
        <p class="server-info">
          <strong>This server's pubkey:</strong><br>
          <code>{{.ServerPubKey}}</code><br>
          <span class="server-info-note">Look for this in your signer's approved connections list.</span>
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
      <p>Zero-JS Hypermedia Browser</p>
    </footer>
  </div>
</body>
</html>
`

func renderLoginPage(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	cachedLoginTemplate.Execute(w, data)
}

// htmlFollowHandler handles following/unfollowing a user (kind 3 contact list)
func htmlFollowHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusSeeOther)
		return
	}

	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		http.Redirect(w, r, "/html/login?error=Please+login+first", http.StatusSeeOther)
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Error(w, "Invalid or expired CSRF token", http.StatusForbidden)
		return
	}

	targetPubkey := strings.TrimSpace(r.FormValue("pubkey"))
	action := strings.TrimSpace(r.FormValue("action")) // "follow" or "unfollow"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")))

	// Validate pubkey (same format as event IDs: 64 hex chars)
	if targetPubkey == "" || !isValidEventID(targetPubkey) {
		http.Redirect(w, r, returnURL+"?error=Invalid+pubkey", http.StatusSeeOther)
		return
	}

	if action != "follow" && action != "unfollow" {
		action = "follow"
	}

	// Don't allow following yourself
	userPubkey := hex.EncodeToString(session.UserPubKey)
	if targetPubkey == userPubkey {
		http.Redirect(w, r, returnURL+"?error=Cannot+follow+yourself", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get relays to use
	relays := []string{
		"wss://relay.damus.io",
		"wss://relay.nostr.band",
		"wss://nos.lol",
	}
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	// Fetch user's current contact list (kind 3)
	existingTags := [][]string{}
	contactEvents := fetchKind3(relays, userPubkey)
	if len(contactEvents) > 0 {
		existingTags = contactEvents[0].Tags
	}

	// Build new tags list
	var newTags [][]string
	found := false

	for _, tag := range existingTags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetPubkey {
			found = true
			if action == "unfollow" {
				// Skip this tag (removes the follow)
				continue
			}
		}
		newTags = append(newTags, tag)
	}

	// If following and not found, add new p tag
	if action == "follow" && !found {
		newTags = append(newTags, []string{"p", targetPubkey})
	}

	// If unfollowing and not found, nothing to do
	if action == "unfollow" && !found {
		http.Redirect(w, r, returnURL, http.StatusSeeOther)
		return
	}

	// Create the kind 3 event (replaceable)
	event := UnsignedEvent{
		Kind:      3,
		Content:   "", // Content is usually empty for contact lists
		Tags:      newTags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign contact list: %v", err)
		separator := "?"
		if strings.Contains(returnURL, "?") {
			separator = "&"
		}
		http.Redirect(w, r, returnURL+separator+"error="+escapeURLParam(sanitizeErrorForUser("Sign event", err)), http.StatusSeeOther)
		return
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	// Update the session's cached following list
	session.mu.Lock()
	if action == "follow" {
		session.FollowingPubkeys = append(session.FollowingPubkeys, targetPubkey)
	} else {
		// Remove from following list
		newFollowing := make([]string, 0, len(session.FollowingPubkeys))
		for _, pk := range session.FollowingPubkeys {
			if pk != targetPubkey {
				newFollowing = append(newFollowing, pk)
			}
		}
		session.FollowingPubkeys = newFollowing
	}
	session.mu.Unlock()

	log.Printf("Published contact list update: %s (action=%s, target=%s)", signedEvent.ID, action, targetPubkey[:16])
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// fetchKind3 fetches the user's contact list (kind 3)
func fetchKind3(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{3},
		Authors: []string{pubkey},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelays(relays, filter)
	return events
}

// fetchKind0 fetches the user's profile metadata (kind 0)
func fetchKind0(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{0},
		Authors: []string{pubkey},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelays(relays, filter)
	return events
}

// htmlProfileEditHandler handles GET and POST for /html/profile/edit
func htmlProfileEditHandler(w http.ResponseWriter, r *http.Request) {
	// Get session
	session := getSessionFromRequest(r)
	if session == nil {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	userPubKeyHex := hex.EncodeToString(session.UserPubKey)

	if r.Method == "GET" {
		// Fetch current profile
		var relays []string
		session.mu.Lock()
		if session.UserRelayList != nil {
			relays = session.UserRelayList.Write
		}
		session.mu.Unlock()
		if len(relays) == 0 {
			relays = session.Relays
		}
		if len(relays) == 0 {
			relays = defaultNostrConnectRelays
		}

		var profile ProfileInfo
		var rawContent map[string]interface{}

		events := fetchKind0(relays, userPubKeyHex)
		if len(events) > 0 {
			if err := json.Unmarshal([]byte(events[0].Content), &profile); err != nil {
				log.Printf("Failed to parse profile: %v", err)
			}
			// Keep raw content to preserve unknown fields
			if err := json.Unmarshal([]byte(events[0].Content), &rawContent); err != nil {
				rawContent = make(map[string]interface{})
			}
		} else {
			rawContent = make(map[string]interface{})
		}

		// Encode raw content for hidden field (to preserve unknown fields)
		rawContentJSON, _ := json.Marshal(rawContent)

		// Generate npub from hex pubkey
		npub, _ := encodeBech32Pubkey(userPubKeyHex)

		themeClass, themeLabel := getThemeFromRequest(r)
		currentURL := r.URL.String()

		data := HTMLProfileData{
			Title:      "Edit Profile - Nostr Hypermedia",
			Pubkey:     userPubKeyHex,
			Npub:       npub,
			NpubShort:  formatNpubShort(npub),
			Profile:    &profile,
			Items:      []HTMLEventItem{}, // Empty - not showing notes in edit mode
			Pagination: nil,
			Meta:       &MetaInfo{GeneratedAt: time.Now()},
			ThemeClass: themeClass,
			ThemeLabel: themeLabel,
			LoggedIn:   true,
			CurrentURL: currentURL,
			CSRFToken:  generateCSRFToken(session.ID),
			IsFollowing: false, // Not relevant in edit mode
			IsSelf:     true,
			// Edit mode fields
			EditMode:   true,
			RawContent: string(rawContentJSON),
			Error:      r.URL.Query().Get("error"),
			Success:    r.URL.Query().Get("success"),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := cachedProfileTemplate.Execute(w, data); err != nil {
			log.Printf("Template error: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// POST - save profile
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate CSRF
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Invalid form data"), http.StatusSeeOther)
		return
	}

	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Invalid session, please try again"), http.StatusSeeOther)
		return
	}

	// Get form values
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	name := strings.TrimSpace(r.FormValue("name"))
	about := strings.TrimSpace(r.FormValue("about"))
	picture := strings.TrimSpace(r.FormValue("picture"))
	banner := strings.TrimSpace(r.FormValue("banner"))
	nip05 := strings.TrimSpace(r.FormValue("nip05"))
	lud16 := strings.TrimSpace(r.FormValue("lud16"))
	website := strings.TrimSpace(r.FormValue("website"))
	rawContentStr := r.FormValue("raw_content")

	// Basic URL validation for picture and banner
	if picture != "" && !isValidURL(picture) {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Invalid picture URL"), http.StatusSeeOther)
		return
	}
	if banner != "" && !isValidURL(banner) {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Invalid banner URL"), http.StatusSeeOther)
		return
	}
	if website != "" && !isValidURL(website) {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Invalid website URL"), http.StatusSeeOther)
		return
	}

	// Parse raw content to preserve unknown fields
	var profileData map[string]interface{}
	if rawContentStr != "" {
		if err := json.Unmarshal([]byte(rawContentStr), &profileData); err != nil {
			profileData = make(map[string]interface{})
		}
	} else {
		profileData = make(map[string]interface{})
	}

	// Update known fields (set or delete if empty)
	setOrDelete := func(key, value string) {
		if value != "" {
			profileData[key] = value
		} else {
			delete(profileData, key)
		}
	}

	setOrDelete("display_name", displayName)
	setOrDelete("name", name)
	setOrDelete("about", about)
	setOrDelete("picture", picture)
	setOrDelete("banner", banner)
	setOrDelete("nip05", nip05)
	setOrDelete("lud16", lud16)
	setOrDelete("website", website)

	// Serialize profile content
	contentJSON, err := json.Marshal(profileData)
	if err != nil {
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam("Failed to encode profile"), http.StatusSeeOther)
		return
	}

	// Create kind 0 event
	event := UnsignedEvent{
		Kind:      0,
		Content:   string(contentJSON),
		Tags:      [][]string{},
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		log.Printf("Failed to sign profile update: %v", err)
		http.Redirect(w, r, "/html/profile/edit?error="+escapeURLParam(sanitizeErrorForUser("Sign profile", err)), http.StatusSeeOther)
		return
	}

	// Get relays
	var relays []string
	session.mu.Lock()
	if session.UserRelayList != nil {
		relays = session.UserRelayList.Write
	}
	session.mu.Unlock()
	if len(relays) == 0 {
		relays = session.Relays
	}
	if len(relays) == 0 {
		relays = defaultNostrConnectRelays
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	// Invalidate cached profile
	profileCache.Delete(userPubKeyHex)

	log.Printf("Published profile update: %s (pubkey=%s)", signedEvent.ID, userPubKeyHex[:16])
	http.Redirect(w, r, "/html/profile/"+userPubKeyHex+"?success="+escapeURLParam("Profile updated"), http.StatusSeeOther)
}

// isValidURL checks if a string is a valid HTTP/HTTPS URL
func isValidURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
