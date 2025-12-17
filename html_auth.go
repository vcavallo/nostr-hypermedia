package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
)

// trustedProxyCount is loaded from TRUSTED_PROXY_COUNT env var at startup.
// 0 = don't trust proxy headers (direct connection, safe default)
// 1 = one proxy in front (e.g., nginx)
// 2 = two proxies (e.g., cloudflare -> nginx)
var trustedProxyCount int

func init() {
	if count := os.Getenv("TRUSTED_PROXY_COUNT"); count != "" {
		if n, err := strconv.Atoi(count); err == nil && n >= 0 {
			trustedProxyCount = n
			slog.Info("trusted proxy count configured", "count", trustedProxyCount)
		}
	}
}

const sessionCookieName = "nostr_session"
const sessionMaxAge = 24 * time.Hour

// anonSessionCookieName is for anonymous CSRF protection on login forms.
// Each login page load gets a unique anonymous session ID bound to the CSRF token.
const anonSessionCookieName = "anon_session"
const anonSessionMaxAge = 300 // 5 minutes

// getClientIP extracts the real client IP from the request.
// Uses TRUSTED_PROXY_COUNT to determine which IP in X-Forwarded-For to trust.
// This prevents IP spoofing attacks where attackers set fake X-Forwarded-For headers.
func getClientIP(r *http.Request) string {
	// If no trusted proxies, don't trust any headers - use RemoteAddr only
	if trustedProxyCount == 0 {
		return parseRemoteAddr(r.RemoteAddr)
	}

	// Check X-Forwarded-For with trusted proxy count
	// XFF format: "client, proxy1, proxy2" - rightmost IPs are added by proxies we control
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		// Take IP at position: len(ips) - trustedProxyCount
		// This skips the trusted proxy IPs at the end that we know are legitimate
		idx := len(ips) - trustedProxyCount
		if idx < 0 {
			idx = 0
		}
		return strings.TrimSpace(ips[idx])
	}

	// Check X-Real-IP (nginx proxy header) - only trust if we have trusted proxies
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	return parseRemoteAddr(r.RemoteAddr)
}

// parseRemoteAddr extracts the IP from RemoteAddr (which includes port)
func parseRemoteAddr(remoteAddr string) string {
	// Remove port if present
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		remoteAddr = remoteAddr[:idx]
	}
	// Handle IPv6 brackets
	remoteAddr = strings.TrimPrefix(remoteAddr, "[")
	remoteAddr = strings.TrimSuffix(remoteAddr, "]")
	return remoteAddr
}

// sanitizeErrorForUser returns a user-safe error message, logging the full error
func sanitizeErrorForUser(context string, err error) string {
	slog.Error(context, "error", err)
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

var cachedLoginTemplate *template.Template

func initAuthTemplates() {
	var err error
	cachedLoginTemplate, err = template.New("login").Parse(htmlLoginTemplate)
	if err != nil {
		slog.Error("failed to compile login template", "error", err)
		os.Exit(1)
	}

	slog.Info("auth templates compiled successfully")
}

func generateQRCodeDataURL(content string) string {
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		slog.Error("failed to generate QR code", "error", err)
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// prefetchTimeout is the maximum time allowed for background prefetch operations
const prefetchTimeout = 30 * time.Second

// prefetchUserProfile caches the user's profile (call after login)
func prefetchUserProfile(pubkeyHex string, relays []string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()

		done := make(chan struct{})
		go func() {
			fetchProfiles(relays, []string{pubkeyHex})
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-ctx.Done():
			slog.Debug("prefetchUserProfile timed out", "pubkey", pubkeyHex[:12])
		}
	}()
}

// prefetchUserContactList caches who the user follows and prefetches their profiles (call after login)
func prefetchUserContactList(session *BunkerSession, relays []string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()

		pubkeyHex := hex.EncodeToString(session.UserPubKey)

		done := make(chan struct{})
		var contacts []string
		go func() {
			contacts = fetchContactList(relays, pubkeyHex)
			close(done)
		}()

		select {
		case <-done:
			if contacts != nil {
				session.mu.Lock()
				session.FollowingPubkeys = contacts
				session.mu.Unlock()

				// Prefetch profiles for contacts (for follows feed avatars)
				if len(contacts) > 0 {
					go prefetchContactProfiles(contacts)
				}
			}
		case <-ctx.Done():
			slog.Debug("prefetchUserContactList timed out", "pubkey", pubkeyHex[:12])
		}
	}()
}

// prefetchContactProfiles prefetches profiles for followed users in batches
func prefetchContactProfiles(contacts []string) {
	const batchSize = 50
	profileRelays := ConfigGetProfileRelays()

	for i := 0; i < len(contacts); i += batchSize {
		end := i + batchSize
		if end > len(contacts) {
			end = len(contacts)
		}
		batch := contacts[i:end]

		// Fetch profiles - this will cache them
		fetchProfiles(profileRelays, batch)
	}
	slog.Debug("prefetched contact profiles", "count", len(contacts))
}

// getSessionRelays returns user's NIP-65 relays or fallback (caches in session)
func getSessionRelays(session *BunkerSession, fallbackRelays []string) []string {
	pubkeyHex := hex.EncodeToString(session.UserPubKey)

	session.mu.Lock()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays := session.UserRelayList.Read
		session.mu.Unlock()
		return relays
	}
	session.mu.Unlock()

	// Wait briefly for nip46 goroutine to populate relay list
	time.Sleep(100 * time.Millisecond)

	session.mu.Lock()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays := session.UserRelayList.Read
		session.mu.Unlock()
		return relays
	}
	session.mu.Unlock()

	relayList := fetchRelayList(pubkeyHex)
	if relayList != nil && len(relayList.Read) > 0 {
		session.mu.Lock()
		if session.UserRelayList == nil {
			session.UserRelayList = relayList
		}
		session.mu.Unlock()
		return relayList.Read
	}

	return fallbackRelays
}

// prefetchUserData fetches bookmarks, reactions, reposts, mutes in parallel
func prefetchUserData(session *BunkerSession, fallbackRelays []string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()

		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		relays := getSessionRelays(session, fallbackRelays)

		var wg sync.WaitGroup
		wg.Add(4)

		go func() { // Bookmarks
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			bookmarkEvents := fetchKind10003(relays, pubkeyHex)
			if len(bookmarkEvents) > 0 {
				var eventIDs []string
				for _, tag := range bookmarkEvents[0].Tags {
					if tag[0] == "e" && len(tag) > 1 {
						eventIDs = append(eventIDs, tag[1])
					}
				}
				session.mu.Lock()
				session.BookmarkedEventIDs = eventIDs
				session.mu.Unlock()
			}
		}()

		go func() { // Reactions
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			reactionEvents := fetchUserReactions(relays, pubkeyHex)
			if len(reactionEvents) > 0 {
				var eventIDs []string
				for _, event := range reactionEvents {
					for _, tag := range event.Tags {
						if tag[0] == "e" && len(tag) > 1 {
							eventIDs = append(eventIDs, tag[1])
							break
						}
					}
				}
				session.mu.Lock()
				session.ReactedEventIDs = eventIDs
				session.mu.Unlock()
			}
		}()

		go func() { // Reposts
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			repostEvents := fetchUserReposts(relays, pubkeyHex)
			if len(repostEvents) > 0 {
				var eventIDs []string
				for _, event := range repostEvents {
					for _, tag := range event.Tags {
						if tag[0] == "e" && len(tag) > 1 {
							eventIDs = append(eventIDs, tag[1])
							break
						}
					}
				}
				session.mu.Lock()
				session.RepostedEventIDs = eventIDs
				session.mu.Unlock()
			}
		}()

		go func() { // Mute list
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			default:
			}
			slog.Debug("fetching mute list", "pubkey", pubkeyHex[:12], "relays", len(relays))
			muteEvents := fetchKind10000(relays, pubkeyHex)
			slog.Debug("mute list fetch result", "pubkey", pubkeyHex[:12], "events", len(muteEvents))
			if len(muteEvents) > 0 {
				pubkeys, eventIDs, hashtags, words := parseMuteList(muteEvents[0])
				session.mu.Lock()
				session.MutedPubkeys = pubkeys
				session.MutedEventIDs = eventIDs
				session.MutedHashtags = hashtags
				session.MutedWords = words
				session.mu.Unlock()
				slog.Debug("loaded mute list", "pubkey", pubkeyHex[:12],
					"pubkeys", len(pubkeys), "events", len(eventIDs),
					"hashtags", len(hashtags), "words", len(words))
			}
		}()

		// Wait with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// All prefetch operations completed - save updated session to cache
			if err := sessionStore.Set(ctx, session); err != nil {
				slog.Debug("failed to save session after prefetch", "error", err)
			}
		case <-ctx.Done():
			slog.Debug("prefetchUserData timed out", "pubkey", pubkeyHex[:12])
		}
	}()
}

func getUserDisplayName(pubkeyHex string) string {
	if profile, _, inCache := profileCache.Get(pubkeyHex); inCache && profile != nil {
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

// getUserAvatarURL returns the user's profile picture URL, or empty string if not available
// If not cached, triggers a quick fetch to populate the cache
func getUserAvatarURL(pubkeyHex string) string {
	if profile, _, inCache := profileCache.Get(pubkeyHex); inCache && profile != nil {
		if profile.Picture != "" {
			return GetValidatedAvatarURL(profile.Picture)
		}
		return "" // Profile exists but has no picture
	}

	// Not in cache - fetch with short timeout to avoid blocking
	profiles := fetchProfilesWithTimeout(ConfigGetProfileRelays(), []string{pubkeyHex}, 500*time.Millisecond)
	if profile := profiles[pubkeyHex]; profile != nil && profile.Picture != "" {
		return GetValidatedAvatarURL(profile.Picture)
	}
	return ""
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
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	// Generate nostrconnect:// URL for the user
	nostrConnectURL, secret, err := GenerateNostrConnectURL(defaultNostrConnectRelays())
	if err != nil {
		slog.Error("failed to generate nostrconnect URL", "error", err)
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

	// Generate unique anonymous session ID for CSRF protection
	// This ensures each login page load has a unique token bound to a cookie
	anonSessionID, err := generateSessionID()
	if err != nil {
		slog.Error("failed to generate anonymous session ID", "error", err)
		anonSessionID = "fallback-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}

	// Store anonymous session ID in cookie (SameSite=Strict for login security)
	http.SetCookie(w, &http.Cookie{
		Name:     anonSessionCookieName,
		Value:    anonSessionID,
		Path:     "/html/login",
		MaxAge:   anonSessionMaxAge,
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteStrictMode,
	})

	// Generate CSRF token bound to this specific anonymous session
	csrfToken := generateCSRFToken(anonSessionID)

	data := struct {
		Title           string
		Error           string
		Success         string
		NostrConnectURL string
		Secret          string
		QRCodeDataURL   template.URL
		ServerPubKey    string
		ThemeClass      string
		CSRFToken       string
	}{
		Title:           "Login with Nostr Connect",
		NostrConnectURL: nostrConnectURL,
		Secret:          secret,
		QRCodeDataURL:   template.URL(qrCodeDataURL),
		ServerPubKey:    serverPubKey,
		ThemeClass:      themeClass,
		CSRFToken:       csrfToken,
	}

	// Check for flash messages from cookies
	flash := getFlashMessages(w, r)
	data.Error = flash.Error
	data.Success = flash.Success

	renderLoginPage(w, data)
}

// htmlLoginSubmitHandler processes the bunker URL submission
func htmlLoginSubmitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	// Get anonymous session from cookie for CSRF validation
	anonCookie, err := r.Cookie(anonSessionCookieName)
	if err != nil {
		redirectWithError(w, r, "/html/login", "Session expired, please try again")
		return
	}

	// Validate CSRF token against this anonymous session
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(anonCookie.Value, csrfToken) {
		redirectWithError(w, r, "/html/login", "Invalid security token, please try again")
		return
	}

	// Clear the anonymous session cookie (it's single-use)
	http.SetCookie(w, &http.Cookie{
		Name:   anonSessionCookieName,
		Value:  "",
		Path:   "/html/login",
		MaxAge: -1,
	})

	// Rate limit login attempts by IP
	if err := CheckLoginRateLimit(getClientIP(r)); err != nil {
		redirectWithError(w, r, "/html/login", "Too many login attempts. Please wait a minute and try again.")
		return
	}

	bunkerURL := strings.TrimSpace(r.FormValue("bunker_url"))
	if bunkerURL == "" {
		redirectWithError(w, r, "/html/login", "Please enter a bunker URL")
		return
	}

	// Parse bunker URL
	session, err := ParseBunkerURL(bunkerURL)
	if err != nil {
		redirectWithError(w, r, "/html/login", sanitizeErrorForUser("Parse bunker URL", err))
		return
	}

	// Attempt to connect (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := session.Connect(ctx); err != nil {
		redirectWithError(w, r, "/html/login", sanitizeErrorForUser("Connect to bunker", err))
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
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Prefetch user profile, contact list, bookmarks, reactions, and reposts in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)
	prefetchUserData(session, session.Relays)

	// Redirect to logged-in user's default feed (from config)
	feedsConfig := GetFeedsConfig()
	defaultFeed := feedsConfig.Defaults.Feed
	if defaultFeed == "" {
		defaultFeed = "follows"
	}
	redirectWithSuccess(w, r, DefaultTimelineURLWithFeed(defaultFeed), "Logged in successfully")
}

// htmlCheckConnectionHandler checks if a nostrconnect session is ready
func htmlCheckConnectionHandler(w http.ResponseWriter, r *http.Request) {
	// Rate limit connection checks by IP
	if err := CheckLoginRateLimit(getClientIP(r)); err != nil {
		redirectWithError(w, r, "/html/login", "Too many connection attempts. Please wait a minute and try again.")
		return
	}

	secret := r.URL.Query().Get("secret")
	if secret == "" {
		redirectWithError(w, r, "/html/login", "Missing connection secret")
		return
	}

	session := CheckConnection(secret)
	if session == nil {
		// Not connected yet - keep secret in URL so user can retry
		setFlashError(w, r, "Connection not ready. Make sure you approved in your signer app, then try again.")
		http.Redirect(w, r, "/html/login?secret="+escapeURLParam(secret), http.StatusSeeOther)
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
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Prefetch user profile, contact list, bookmarks, reactions, and reposts in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)
	prefetchUserData(session, session.Relays)

	// Redirect to logged-in user's default feed (from config)
	feedsConfigNC := GetFeedsConfig()
	defaultFeedNC := feedsConfigNC.Defaults.Feed
	if defaultFeedNC == "" {
		defaultFeedNC = "follows"
	}
	redirectWithSuccess(w, r, DefaultTimelineURLWithFeed(defaultFeedNC), "Logged in successfully")
}

// htmlReconnectHandler tries to reconnect to an existing approved signer
func htmlReconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/login", http.StatusSeeOther)
		return
	}

	// Get anonymous session from cookie for CSRF validation
	anonCookie, err := r.Cookie(anonSessionCookieName)
	if err != nil {
		redirectWithError(w, r, "/html/login", "Session expired, please try again")
		return
	}

	// Validate CSRF token against this anonymous session
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(anonCookie.Value, csrfToken) {
		redirectWithError(w, r, "/html/login", "Invalid security token, please try again")
		return
	}

	// Clear the anonymous session cookie (it's single-use)
	http.SetCookie(w, &http.Cookie{
		Name:   anonSessionCookieName,
		Value:  "",
		Path:   "/html/login",
		MaxAge: -1,
	})

	signerPubKey := strings.TrimSpace(r.FormValue("signer_pubkey"))
	if signerPubKey == "" {
		redirectWithError(w, r, "/html/login", "Please enter your signer pubkey")
		return
	}

	// Handle npub format
	if strings.HasPrefix(signerPubKey, "npub1") {
		// Decode bech32 npub to hex
		decoded, err := decodeBech32Pubkey(signerPubKey)
		if err != nil {
			redirectWithError(w, r, "/html/login", "Invalid npub format")
			return
		}
		signerPubKey = decoded
	}

	// Validate hex
	if len(signerPubKey) != 64 {
		redirectWithError(w, r, "/html/login", "Invalid pubkey length (expected 64 hex chars or npub)")
		return
	}

	session, err := TryReconnectToSigner(signerPubKey, defaultNostrConnectRelays())
	if err != nil {
		redirectWithError(w, r, "/html/login", sanitizeErrorForUser("Reconnect to signer", err))
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
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})

	// Prefetch user profile, contact list, bookmarks, reactions, and reposts in background so they're ready for display
	prefetchUserProfile(hex.EncodeToString(session.UserPubKey), session.Relays)
	prefetchUserContactList(session, session.Relays)
	prefetchUserData(session, session.Relays)

	// Redirect to logged-in user's default feed (from config)
	feedsConfigRC := GetFeedsConfig()
	defaultFeedRC := feedsConfigRC.Defaults.Feed
	if defaultFeedRC == "" {
		defaultFeedRC = "follows"
	}
	redirectWithSuccess(w, r, DefaultTimelineURLWithFeed(defaultFeedRC), "Reconnected successfully")
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
		// Close persistent NIP-46 relay connections before deleting session
		session.CloseRelayConns()
		bunkerSessions.Delete(session.ID)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
	})

	redirectWithSuccess(w, r, "/html/login", "Logged out")
}

// htmlPostNoteHandler handles note posting via POST form
func htmlPostNoteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	gifURL := strings.TrimSpace(r.FormValue("gif_url"))

	// Append GIF URL to content if present
	if gifURL != "" {
		if content != "" {
			content = content + "\n\n" + gifURL
		} else {
			content = gifURL
		}
	}

	if content == "" {
		redirectWithError(w, r, DefaultTimelineURL(), "You cannot post an empty note!")
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
		slog.Error("failed to sign event", "error", err)
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, DefaultTimelineURL(), sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays (use NIP-65 write relays if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	// For HelmJS requests, return the cleared form + new note as OOB
	if isHelmRequest(r) {
		// Generate new CSRF token
		newCSRFToken := generateCSRFToken(session.ID)

		// Build HTMLEventItem for the new note
		userPubkey := hex.EncodeToString(session.UserPubKey)
		npub, _ := encodeBech32Pubkey(userPubkey)

		// Get user's profile for display
		var authorProfile *ProfileInfo
		if profile, _, inCache := profileCache.Get(userPubkey); inCache && profile != nil {
			authorProfile = profile
		}

		// Build actions for the new note
		actionCtx := ActionContext{
			EventID:      signedEvent.ID,
			EventPubkey:  userPubkey,
			Kind:         1,
			IsBookmarked: false,
			IsReacted:    false,
			IsReposted:   false,
			ReplyCount:   0,
			LoggedIn:     true,
			HasWallet:    session.HasWallet(),
			IsAuthor:     true,
			CSRFToken:    newCSRFToken,
			ReturnURL:    DefaultTimelineURLLoggedIn(),
		}
		entity := BuildHypermediaEntity(actionCtx, signedEvent.Tags, nil)
		actionGroups := GroupActionsForKind(entity.Actions, 1)

		newNote := &HTMLEventItem{
			ID:            signedEvent.ID,
			Pubkey:        userPubkey,
			Npub:          npub,
			NpubShort:     formatNpubShort(npub),
			Kind:          1,
			Tags:          signedEvent.Tags,
			Content:       content,
			ContentHTML:   processContentToHTML(content),
			AuthorProfile: authorProfile,
			CreatedAt:     signedEvent.CreatedAt,
			ActionGroups:  actionGroups,
			LoggedIn:      true,
		}

		html, err := renderPostResponse(newCSRFToken, newNote)
		if err != nil {
			slog.Error("failed to render post response", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	redirectWithSuccess(w, r, DefaultTimelineURLLoggedIn(), "Note published")
}

// htmlReplyHandler handles replying to a note via POST form
func htmlReplyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	content := strings.TrimSpace(r.FormValue("content"))
	gifURL := strings.TrimSpace(r.FormValue("gif_url"))
	replyTo := strings.TrimSpace(r.FormValue("reply_to"))
	replyToPubkey := strings.TrimSpace(r.FormValue("reply_to_pubkey"))
	replyCountStr := r.FormValue("reply_count")

	// Append GIF URL to content if present
	if gifURL != "" {
		if content != "" {
			content = content + "\n\n" + gifURL
		} else {
			content = gifURL
		}
	}

	// Validate event ID first to prevent path injection
	if replyTo == "" || !isValidEventID(replyTo) {
		redirectWithError(w, r, DefaultTimelineURL(), "Invalid reply target")
		return
	}

	if content == "" {
		redirectWithError(w, r, "/html/thread/"+replyTo, "You cannot post an empty note!")
		return
	}

	// Build tags for reply
	// NIP-10: e tag with "reply" marker, p tag to mention the author
	tags := [][]string{
		{"e", replyTo, "", "reply"},
	}
	if replyToPubkey != "" {
		// Validate pubkey is valid 64-char hex (32 bytes)
		if decoded, err := hex.DecodeString(replyToPubkey); err != nil || len(decoded) != 32 {
			slog.Warn("reply: invalid pubkey format", "pubkey", replyToPubkey)
			// Skip invalid pubkey but continue with reply
		} else {
			tags = append(tags, []string{"p", replyToPubkey})
		}
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
		slog.Error("failed to sign reply", "error", err)
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, "/html/thread/"+replyTo, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays (use NIP-65 write relays if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	// For HelmJS requests, return the cleared form + new reply as OOB
	if isHelmRequest(r) {
		// Generate new CSRF token
		newCSRFToken := generateCSRFToken(session.ID)

		// Build HTMLEventItem for the new reply
		userPubkey := hex.EncodeToString(session.UserPubKey)
		npub, _ := encodeBech32Pubkey(userPubkey)

		// Get user's profile for display
		var authorProfile *ProfileInfo
		if profile, _, inCache := profileCache.Get(userPubkey); inCache && profile != nil {
			authorProfile = profile
		}

		// Get user display name and avatar
		userDisplayName := getUserDisplayName(userPubkey)
		userAvatarURL := getUserAvatarURL(userPubkey)

		// Build actions for the new reply
		returnURL := "/html/thread/" + replyTo
		actionCtx := ActionContext{
			EventID:      signedEvent.ID,
			EventPubkey:  userPubkey,
			Kind:         1,
			IsBookmarked: false,
			IsReacted:    false,
			IsReposted:   false,
			ReplyCount:   0,
			LoggedIn:     true,
			HasWallet:    session.HasWallet(),
			IsAuthor:     true,
			CSRFToken:    newCSRFToken,
			ReturnURL:    returnURL,
		}
		entity := BuildHypermediaEntity(actionCtx, signedEvent.Tags, nil)
		actionGroups := GroupActionsForKind(entity.Actions, 1)

		newReply := &HTMLEventItem{
			ID:            signedEvent.ID,
			Pubkey:        userPubkey,
			Npub:          npub,
			NpubShort:     formatNpubShort(npub),
			Kind:          1,
			Tags:          signedEvent.Tags,
			Content:       content,
			ContentHTML:   processContentToHTML(content),
			AuthorProfile: authorProfile,
			CreatedAt:     signedEvent.CreatedAt,
			ActionGroups:  actionGroups,
			LoggedIn:      true,
		}

		// Increment reply count from form (avoids relay query, matches displayed count)
		replyCount := 1
		if n, err := strconv.Atoi(replyCountStr); err == nil {
			replyCount = n + 1
		}

		html, err := renderReplyResponse(newCSRFToken, replyTo, replyToPubkey, userDisplayName, userAvatarURL, npub, newReply, replyCount)
		if err != nil {
			slog.Error("failed to render reply response", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	redirectWithSuccess(w, r, "/html/thread/"+replyTo, "Reply published")
}

// htmlReactHandler handles adding a reaction to a note
func htmlReactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	kindStr := strings.TrimSpace(r.FormValue("kind"))
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true) // logged in (requireAuth passed)
	reaction := strings.TrimSpace(r.FormValue("reaction"))

	if eventID == "" || !isValidEventID(eventID) {
		redirectWithError(w, r, returnURL, "Invalid event ID")
		return
	}

	// Parse kind, default to 1 if not provided
	kind := 1
	if kindStr != "" {
		if parsedKind, err := strconv.Atoi(kindStr); err == nil {
			kind = parsedKind
		}
	}

	if reaction == "" {
		reaction = "+"
	}

	// Build tags for reaction (NIP-25)
	tags := [][]string{
		{"e", eventID},
		{"k", kindStr}, // Kind of the event being reacted to
	}
	if kindStr == "" {
		tags[1][1] = "1" // Default to kind 1
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
		slog.Error("failed to sign reaction", "error", err)
		// For HelmJS requests, return an error response
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Get relays to publish to (use NIP-65 if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	// Update session's reaction cache
	session.mu.Lock()
	session.ReactedEventIDs = append(session.ReactedEventIDs, eventID)
	session.mu.Unlock()

	// For HelmJS requests, return the updated footer fragment
	if isHelmRequest(r) {
		// Generate new CSRF token for the updated form
		newCSRFToken := generateCSRFToken(session.ID)
		// Get read relays to fetch existing reactions
		readRelays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			readRelays = session.UserRelayList.Read
		}
		// Check if user had bookmarked or reposted this event (for footer display)
		isBookmarked := session.IsEventBookmarked(eventID)
		isReposted := session.IsEventReposted(eventID)
		// Pass the reaction so it shows in the UI, isReacted is true since user just reacted
		html, err := renderFooterFragment(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, true, isReposted, false, session.HasWallet(), reaction, readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// htmlRepostHandler handles reposting a note (kind 6)
func htmlRepostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	kindStr := strings.TrimSpace(r.FormValue("kind"))
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true) // logged in (requireAuth passed)

	// Parse kind, default to 1 (note) if not provided or invalid
	kind := 1
	if kindStr != "" {
		if parsedKind, err := strconv.Atoi(kindStr); err == nil && parsedKind > 0 {
			kind = parsedKind
		}
	}

	if eventID == "" || !isValidEventID(eventID) {
		redirectWithError(w, r, DefaultTimelineURL(), "Invalid event ID")
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
		slog.Error("failed to sign repost", "error", err)
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Get relays to publish to (use NIP-65 if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	publishEvent(ctx, relays, signedEvent)

	// Update session cache to reflect new repost
	session.mu.Lock()
	session.RepostedEventIDs = append(session.RepostedEventIDs, eventID)
	session.mu.Unlock()

	// For HelmJS partial update, return updated footer
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		readRelays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			readRelays = session.UserRelayList.Read
		}
		// Check other state for footer display
		isBookmarked := session.IsEventBookmarked(eventID)
		isReacted := session.IsEventReacted(eventID)
		// isReposted is now true since user just reposted
		html, err := renderFooterFragment(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, isReacted, true, false, session.HasWallet(), "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	redirectWithSuccess(w, r, returnURL, "Reposted")
}

// htmlBookmarkHandler handles adding/removing a note from user's bookmarks (kind 10003)
func htmlBookmarkHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	kindStr := strings.TrimSpace(r.FormValue("kind"))
	action := strings.TrimSpace(r.FormValue("action")) // "add" or "remove"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true) // logged in (requireAuth passed)

	if eventID == "" || !isValidEventID(eventID) {
		redirectWithError(w, r, returnURL, "Invalid event ID")
		return
	}

	// Parse kind, default to 1 if not provided
	kind := 1
	if kindStr != "" {
		if parsedKind, err := strconv.Atoi(kindStr); err == nil {
			kind = parsedKind
		}
	}

	if action != "add" && action != "remove" {
		action = "add"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	userPubkey := hex.EncodeToString(session.UserPubKey)

	// Get relays (use NIP-65 if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
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
		slog.Error("failed to sign bookmark list", "error", err)
		// For HelmJS requests, return an error response
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	// Update session's bookmark cache
	session.mu.Lock()
	if action == "add" {
		session.BookmarkedEventIDs = append(session.BookmarkedEventIDs, eventID)
	} else {
		// Remove from bookmark list
		newBookmarks := make([]string, 0, len(session.BookmarkedEventIDs))
		for _, id := range session.BookmarkedEventIDs {
			if id != eventID {
				newBookmarks = append(newBookmarks, id)
			}
		}
		session.BookmarkedEventIDs = newBookmarks
	}
	session.mu.Unlock()

	// For HelmJS requests, return the updated footer fragment
	if isHelmRequest(r) {
		// Generate new CSRF token for the updated form
		newCSRFToken := generateCSRFToken(session.ID)
		// isBookmarked is now the opposite of what it was (we toggled it)
		newBookmarkState := action == "add"
		// Get read relays to fetch existing reactions
		readRelays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			readRelays = session.UserRelayList.Read
		}
		// Check if user had reacted or reposted this event (for footer display)
		isReacted := session.IsEventReacted(eventID)
		isReposted := session.IsEventReposted(eventID)
		// Empty string for userReaction since this is a bookmark action
		html, err := renderFooterFragment(eventID, "", kind, true, newCSRFToken, returnURL, newBookmarkState, isReacted, isReposted, false, session.HasWallet(), "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

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

// fetchKind10000 fetches the user's mute list (kind 10000)
func fetchKind10000(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{10000},
		Authors: []string{pubkey},
		Limit:   1,
	}

	events, _ := fetchEventsFromRelays(relays, filter)
	return events
}

// parseMuteList extracts mute list items from a kind 10000 event
// Returns pubkeys, eventIDs, hashtags, words
func parseMuteList(event Event) (pubkeys []string, eventIDs []string, hashtags []string, words []string) {
	for _, tag := range event.Tags {
		if len(tag) < 2 {
			continue
		}
		switch tag[0] {
		case "p":
			pubkeys = append(pubkeys, tag[1])
		case "e":
			eventIDs = append(eventIDs, tag[1])
		case "t":
			hashtags = append(hashtags, tag[1])
		case "word":
			words = append(words, tag[1])
		}
	}
	return
}

// htmlMuteHandler handles muting/unmuting a user (kind 10000)
func htmlMuteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	pubkeyToMute := strings.TrimSpace(r.FormValue("pubkey"))
	action := strings.TrimSpace(r.FormValue("action")) // "mute" or "unmute"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true) // logged in (requireAuth passed)

	// Validate pubkey format (64 hex chars)
	if pubkeyToMute == "" || len(pubkeyToMute) != 64 {
		redirectWithError(w, r, returnURL, "Invalid pubkey")
		return
	}

	// Don't allow muting yourself
	userPubkey := hex.EncodeToString(session.UserPubKey)
	if pubkeyToMute == userPubkey {
		redirectWithError(w, r, returnURL, "Cannot mute yourself")
		return
	}

	if action != "mute" && action != "unmute" {
		action = "mute"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get relays (use NIP-65 if available, otherwise defaults)
	relays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	// Fetch user's current mute list (kind 10000)
	existingTags := [][]string{}
	muteEvents := fetchKind10000(relays, userPubkey)
	if len(muteEvents) > 0 {
		existingTags = muteEvents[0].Tags
	}

	// Build new tags list
	var newTags [][]string
	found := false

	for _, tag := range existingTags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == pubkeyToMute {
			found = true
			if action == "unmute" {
				// Skip this tag (removes the mute)
				continue
			}
		}
		newTags = append(newTags, tag)
	}

	// If muting and not found, add new p tag
	if action == "mute" && !found {
		newTags = append(newTags, []string{"p", pubkeyToMute})
	}

	// If unmuting and not found, nothing to do
	if action == "unmute" && !found {
		http.Redirect(w, r, returnURL, http.StatusSeeOther)
		return
	}

	// Create the kind 10000 event (replaceable)
	event := UnsignedEvent{
		Kind:      10000,
		Content:   "",
		Tags:      newTags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign mute list", "error", err)
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	// Update session's mute cache
	session.mu.Lock()
	if action == "mute" {
		session.MutedPubkeys = append(session.MutedPubkeys, pubkeyToMute)
		slog.Debug("muted user", "pubkey", pubkeyToMute[:12])
	} else {
		// Remove from mute list
		newMuted := make([]string, 0, len(session.MutedPubkeys))
		for _, pk := range session.MutedPubkeys {
			if pk != pubkeyToMute {
				newMuted = append(newMuted, pk)
			}
		}
		session.MutedPubkeys = newMuted
		slog.Debug("unmuted user", "pubkey", pubkeyToMute[:12])
	}
	session.mu.Unlock()

	// For HelmJS requests, return empty response (h-swap="delete" removes the element)
	if isHelmRequest(r) {
		w.WriteHeader(http.StatusOK)
		return
	}

	// For unmute or non-HelmJS requests, redirect back
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// fetchUserReactions fetches the user's recent reactions (kind 7)
// Limited to recent reactions to avoid fetching entire history
func fetchUserReactions(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{7},
		Authors: []string{pubkey},
		Limit:   500, // Reasonable limit for recent reactions
	}

	events, _ := fetchEventsFromRelays(relays, filter)
	return events
}

// fetchUserReposts fetches the user's recent reposts (kind 6)
// Limited to recent reposts to avoid fetching entire history
func fetchUserReposts(relays []string, pubkey string) []Event {
	filter := Filter{
		Kinds:   []int{6},
		Authors: []string{pubkey},
		Limit:   200, // Reasonable limit for recent reposts
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
			redirectWithError(w, r, "/html/login", "Please login first")
			return
		}

		// Validate CSRF token
		csrfToken := r.FormValue("csrf_token")
		if !requireCSRF(w, session.ID, csrfToken) {
			return
		}

		content := strings.TrimSpace(r.FormValue("content"))
		quotedPubkey := strings.TrimSpace(r.FormValue("quoted_pubkey"))
		gifURL := strings.TrimSpace(r.FormValue("gif_url"))

		// Append GIF URL to content if present
		if gifURL != "" {
			if content != "" {
				content = content + "\n\n" + gifURL
			} else {
				content = gifURL
			}
		}

		if content == "" {
			redirectWithError(w, r, "/html/quote/"+eventID, "You cannot post an empty note!")
			return
		}

		// Convert event ID to note1 bech32 format for embedding in content
		noteID, err := encodeBech32EventID(eventID)
		if err != nil {
			slog.Error("failed to encode event ID", "error", err)
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
			slog.Error("failed to sign quote", "error", err)
			if isHelmRequest(r) {
				http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
				return
			}
			redirectWithError(w, r, "/html/quote/"+eventID, sanitizeErrorForUser("Sign event", err))
			return
		}

		// Publish to relays
		relays := ConfigGetPublishRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
			relays = session.UserRelayList.Write
		}

		publishEvent(ctx, relays, signedEvent)

		redirectWithSuccess(w, r, DefaultTimelineURLLoggedIn(), "Quote published")
		return
	}

	// GET: Show quote form with preview of the quoted note
	relays := ConfigGetDefaultRelays()

	// Fetch the event to be quoted
	events := fetchEventByID(relays, eventID)
	if len(events) == 0 {
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}
	rawEvent := events[0]

	// Fetch author profile
	profiles := fetchProfiles(relays, []string{rawEvent.PubKey})
	authorProfile := profiles[rawEvent.PubKey]

	// Build HTMLEventItem for proper rendering (with processed content)
	npub, _ := encodeBech32Pubkey(rawEvent.PubKey)
	resolvedRefs := batchResolveNostrRefs(extractNostrRefs([]string{rawEvent.Content}), relays)
	linkPreviews := FetchLinkPreviews(ExtractPreviewableURLs(rawEvent.Content))
	kindDef := GetKindDefinition(rawEvent.Kind)

	quotedEventItem := HTMLEventItem{
		ID:             rawEvent.ID,
		Kind:           rawEvent.Kind,
		TemplateName:   kindDef.TemplateName,
		RenderTemplate: computeRenderTemplate(kindDef.TemplateName, rawEvent.Tags),
		Pubkey:         rawEvent.PubKey,
		Npub:           npub,
		NpubShort:      formatNpubShort(npub),
		CreatedAt:      rawEvent.CreatedAt,
		Content:        rawEvent.Content,
		ContentHTML:    processContentToHTMLFull(rawEvent.Content, relays, resolvedRefs, linkPreviews),
		AuthorProfile:  authorProfile,
	}

	// Get user display name if logged in
	userDisplayName := ""
	userNpubShort := ""
	userNpub := ""
	userAvatarURL := ""
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
		userNpub, _ = encodeBech32Pubkey(userPubkey)
		userNpubShort = formatNpubShort(userNpub)
		userAvatarURL = getUserAvatarURL(userPubkey)
		if userDisplayName == "" {
			userDisplayName = userNpubShort
		}
	}

	// Generate CSRF token for forms (use session ID if logged in)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	// Get flash messages from cookies
	flash := getFlashMessages(w, r)

	data := struct {
		Title                  string
		ThemeClass             string
		ThemeLabel             string
		LoggedIn               bool
		UserDisplayName        string
		UserNpubShort          string
		UserNpub               string
		UserAvatarURL          string
		QuotedEvent            HTMLEventItem
		Error                  string
		Success                string
		GeneratedAt            time.Time
		CSRFToken              string
		FeedModes              []FeedMode
		KindFilters            []KindFilter
		NavItems               []NavItem
		SettingsItems          []SettingsItem
		SettingsToggle         SettingsToggle
		ActiveRelays           []string // For base template compatibility
		ShowPostForm           bool     // For base template compatibility (always false for quote)
		HasUnreadNotifications bool     // For base template compatibility
		CurrentURL             string   // For base template compatibility
		ShowGifButton          bool     // For GIF picker
	}{
		Title:           "Quote Note",
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		LoggedIn:        loggedIn,
		UserDisplayName: userDisplayName,
		UserNpubShort:   userNpubShort,
		UserNpub:        userNpub,
		UserAvatarURL:   userAvatarURL,
		QuotedEvent:     quotedEventItem,
		Error:           flash.Error,
		Success:         flash.Success,
		GeneratedAt:     time.Now(),
		CSRFToken:       csrfToken,
		FeedModes: GetFeedModes(FeedModeContext{
			LoggedIn:    loggedIn,
			ActiveFeed:  "",
			CurrentPage: "quote",
		}),
		KindFilters: GetKindFilters(KindFilterContext{
			LoggedIn:    loggedIn,
			ActiveFeed:  "",
			ActiveKinds: "1",
		}),
		NavItems: GetNavItems(NavContext{
			LoggedIn:   loggedIn,
			ActivePage: "",
		}),
		SettingsItems: GetSettingsItems(SettingsContext{
			LoggedIn:      loggedIn,
			ThemeLabel:    themeLabel,
			UserAvatarURL: getUserAvatarURL(hex.EncodeToString(session.UserPubKey)),
		}),
		SettingsToggle: GetSettingsToggle(SettingsContext{
			LoggedIn:      loggedIn,
			ThemeLabel:    themeLabel,
			UserAvatarURL: getUserAvatarURL(hex.EncodeToString(session.UserPubKey)),
		}),
		ActiveRelays:  relays,
		CurrentURL:    r.URL.String(),
		ShowGifButton: GiphyEnabled(),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := cachedQuoteTemplate.ExecuteTemplate(w, tmplBase, data); err != nil {
		slog.Error("quote template error", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// getSessionFromRequest retrieves the bunker session from the request cookie
func getSessionFromRequest(r *http.Request) *BunkerSession {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	return bunkerSessions.Get(cookie.Value)
}

// requireAuth checks for a valid connected session and redirects to login if not found.
// Returns the session if valid, nil if redirected (caller should return immediately).
func requireAuth(w http.ResponseWriter, r *http.Request) *BunkerSession {
	session := getSessionFromRequest(r)
	if session == nil || !session.Connected {
		redirectWithError(w, r, "/html/login", "Please login first")
		return nil
	}
	return session
}

// validEventID matches a 64-character lowercase hex string (nostr event ID)
var validEventID = regexp.MustCompile(`^[a-f0-9]{64}$`)

// isValidEventID checks if a string is a valid nostr event ID (64 hex chars)
func isValidEventID(id string) bool {
	return validEventID.MatchString(id)
}

// sanitizeReturnURL ensures the return URL is a local path to prevent open redirects
// loggedIn determines which default timeline to use when returnURL is empty or invalid
func sanitizeReturnURL(returnURL string, loggedIn bool) string {
	if returnURL == "" {
		if loggedIn {
			return DefaultTimelineURLLoggedIn()
		}
		return DefaultTimelineURL()
	}
	// Must start with / and not // (which could be protocol-relative URL)
	if !strings.HasPrefix(returnURL, "/") || strings.HasPrefix(returnURL, "//") {
		if loggedIn {
			return DefaultTimelineURLLoggedIn()
		}
		return DefaultTimelineURL()
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
				slog.Warn("failed to publish", "relay", relayURL, "error", err)
			}
		}(relay)
	}
	// Give relays a moment to receive
	time.Sleep(500 * time.Millisecond)
}

func publishToRelay(ctx context.Context, relayURL string, event *Event) error {
	eventMsg := []interface{}{"EVENT", event}

	resp, err := relayPool.PublishEvent(ctx, relayURL, event.ID, eventMsg)
	if err != nil {
		return fmt.Errorf("publish failed: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("relay rejected event %s: %s", resp.EventID, resp.Message)
	}
	return nil
}

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
      <a href="/html/timeline?kinds=1&limit=20">Timeline</a>
    </nav>

    <main>
      {{if .Error}}
      <div class="alert alert-error" role="alert">{{.Error}}</div>
      {{end}}
      {{if .Success}}
      <div class="alert alert-success" role="alert">{{.Success}}</div>
      {{end}}

      {{if .NostrConnectURL}}
      <div class="login-form login-section">
        <h2>Option 1: Scan with Signer App</h2>
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
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <h2>Option 2: Paste Bunker URL</h2>
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
        <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
        <h2>Option 3: Reconnect to Existing Bunker</h2>
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
        <h2>How it works</h2>
        <p>
          This login uses <strong>NIP-46 (Nostr Connect)</strong> - your private key never leaves your signer app.
          The server only sees your public key and cannot sign events without your approval.
        </p>
        <h2>Supported signers</h2>
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
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	targetPubkey := strings.TrimSpace(r.FormValue("pubkey"))
	action := strings.TrimSpace(r.FormValue("action")) // "follow" or "unfollow"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true) // logged in (requireAuth passed)

	// Validate pubkey (same format as event IDs: 64 hex chars)
	if targetPubkey == "" || !isValidEventID(targetPubkey) {
		redirectWithError(w, r, returnURL, "Invalid pubkey")
		return
	}

	if action != "follow" && action != "unfollow" {
		action = "follow"
	}

	// Don't allow following yourself
	userPubkey := hex.EncodeToString(session.UserPubKey)
	if targetPubkey == userPubkey {
		redirectWithError(w, r, returnURL, "Cannot follow yourself")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get relays to use
	relays := ConfigGetPublishRelays()
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
		slog.Error("failed to sign contact list", "error", err)
		if isHelmRequest(r) {
			http.Error(w, sanitizeErrorForUser("Sign event", err), http.StatusInternalServerError)
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
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

	// For HelmJS requests, return the updated follow button fragment
	if isHelmRequest(r) {
		// Generate new CSRF token for the updated form
		newCSRFToken := generateCSRFToken(session.ID)
		// isFollowing is now the opposite of what it was (we toggled it)
		newFollowingState := action == "follow"
		html, err := renderFollowButtonFragment(targetPubkey, newCSRFToken, returnURL, newFollowingState)
		if err != nil {
			slog.Error("failed to render follow button fragment", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

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

	if r.Method == http.MethodGet {
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
			relays = defaultNostrConnectRelays()
		}

		var profile ProfileInfo
		var rawContent map[string]interface{}

		events := fetchKind0(relays, userPubKeyHex)
		if len(events) > 0 {
			if err := json.Unmarshal([]byte(events[0].Content), &profile); err != nil {
				slog.Error("failed to parse profile", "error", err)
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

		// Get flash messages from cookies
		flash := getFlashMessages(w, r)

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
			Error:      flash.Error,
			Success:    flash.Success,
			// Navigation (NATEOAS)
			FeedModes: GetFeedModes(FeedModeContext{
				LoggedIn:    true,
				ActiveFeed:  "me",
				CurrentPage: "profile",
			}),
			KindFilters: GetKindFilters(KindFilterContext{
				LoggedIn:    true,
				ActiveFeed:  "me",
				ActiveKinds: "",
			}),
			NavItems: GetNavItems(NavContext{
				LoggedIn:   true,
				ActivePage: "",
			}),
			SettingsItems: GetSettingsItems(SettingsContext{
				LoggedIn:      true,
				ThemeLabel:    themeLabel,
				UserAvatarURL: getUserAvatarURL(userPubKeyHex),
			}),
			SettingsToggle: GetSettingsToggle(SettingsContext{
				LoggedIn:      true,
				ThemeLabel:    themeLabel,
				UserAvatarURL: getUserAvatarURL(userPubKeyHex),
			}),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := cachedProfileTemplate.ExecuteTemplate(w, tmplBase, data); err != nil {
			slog.Error("template error", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// POST - save profile
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate CSRF
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/html/profile/edit", "Invalid form data")
		return
	}

	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		redirectWithError(w, r, "/html/profile/edit", "Invalid session, please try again")
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
		redirectWithError(w, r, "/html/profile/edit", "Invalid picture URL")
		return
	}
	if banner != "" && !isValidURL(banner) {
		redirectWithError(w, r, "/html/profile/edit", "Invalid banner URL")
		return
	}
	if website != "" && !isValidURL(website) {
		redirectWithError(w, r, "/html/profile/edit", "Invalid website URL")
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
		redirectWithError(w, r, "/html/profile/edit", "Failed to encode profile")
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
		slog.Error("failed to sign profile update", "error", err)
		redirectWithError(w, r, "/html/profile/edit", sanitizeErrorForUser("Sign profile", err))
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
		relays = defaultNostrConnectRelays()
	}

	// Publish to relays
	publishEvent(ctx, relays, signedEvent)

	// Invalidate cached profile
	profileCache.Delete(userPubKeyHex)

	redirectWithSuccess(w, r, "/html/profile/"+userPubKeyHex, "Profile updated")
}

// isValidURL checks if a string is a valid HTTP/HTTPS URL
func isValidURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// htmlWalletConnectHandler handles wallet connection via NWC URI
func htmlWalletConnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/wallet", http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	// Parse NWC URI
	nwcURI := strings.TrimSpace(r.FormValue("nwc_uri"))
	if nwcURI == "" {
		redirectWithError(w, r, "/html/wallet", I18n("wallet.error_empty_uri"))
		return
	}

	config, err := ParseNWCURI(nwcURI)
	if err != nil {
		slog.Warn("invalid NWC URI", "error", err)
		redirectWithError(w, r, "/html/wallet", I18n("wallet.error_invalid_uri"))
		return
	}

	// Store wallet config in session
	session.SetNWCConfig(config)

	// Update session in store
	bunkerSessions.Set(session)

	userPubkeyHex := hex.EncodeToString(session.UserPubKey)
	slog.Info("wallet connected", "user", userPubkeyHex[:8], "relay", config.Relay)

	// Start background connection and prefetch (non-blocking)
	// This establishes the NWC WebSocket connection and fetches balance/transactions
	// so when user eventually visits wallet page, everything is ready
	PrefetchWalletInfo(userPubkeyHex, config)

	// Redirect to return URL or wallet page
	returnURL := r.FormValue("return_url")
	if returnURL == "" {
		returnURL = "/html/wallet"
	}
	redirectWithSuccess(w, r, returnURL, I18n("wallet.connected_success"))
}

// htmlWalletDisconnectHandler handles wallet disconnection
func htmlWalletDisconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/html/wallet", http.StatusSeeOther)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		return
	}

	// Close pooled NWC connection and clear cache
	userPubkeyHex := hex.EncodeToString(session.UserPubKey)
	CloseNWCPoolConnection(userPubkeyHex)
	DeleteCachedWalletInfo(userPubkeyHex)

	// Clear wallet config
	session.ClearNWCConfig()

	// Update session in store
	bunkerSessions.Set(session)

	slog.Info("wallet disconnected", "user", userPubkeyHex[:8])

	// Redirect to return URL or wallet page
	returnURL := r.FormValue("return_url")
	if returnURL == "" {
		returnURL = "/html/wallet"
	}
	redirectWithSuccess(w, r, returnURL, I18n("wallet.disconnected_success"))
}

// HTMLWalletInfoData holds data for the wallet info fragment
type HTMLWalletInfoData struct {
	Balance      string // Formatted balance (e.g., "⚡ 57,344")
	BalanceMsats int64  // Raw balance in millisatoshis
	Transactions []HTMLWalletTransaction
	Error        string // Error message if fetch failed
}

// HTMLWalletTransaction represents a transaction for display
type HTMLWalletTransaction struct {
	Type        string // "incoming" or "outgoing"
	TypeIcon    string // "↓" or "↑"
	Amount      string // Formatted amount (e.g., "2,100")
	AmountMsats int64  // Raw amount in millisatoshis
	Description string // Truncated description or zap context
	TimeAgo     string // Relative time (e.g., "2h ago")
	CreatedAt   int64  // Unix timestamp
	// Zap context
	IsZap          bool
	ZapDisplayName string // "Zapped @alice" or "From @bob"
}

func htmlWalletInfoHandler(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}

	if session.NWCConfig == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(`<div class="wallet-info-error">` + I18n("wallet.not_connected") + `</div>`))
		return
	}

	ctx := r.Context()
	userPubkeyHex := hex.EncodeToString(session.UserPubKey)

	// Check cache first
	if cached, found := GetCachedWalletInfo(userPubkeyHex); found {
		slog.Debug("wallet info: serving from cache", "user", userPubkeyHex[:16])
		data := cachedToHTMLWalletInfo(cached)
		html, err := renderWalletInfoFragment(data)
		if err != nil {
			slog.Error("wallet info: failed to render cached data", "error", err)
			http.Error(w, "Failed to render wallet info", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(html))
		return
	}

	// Check if prefetch is in progress (started by wallet page handler)
	if result, found := WaitForWalletInfoPrefetch(userPubkeyHex, 20*time.Second); found && result != nil {
		slog.Debug("wallet info: served from prefetch", "user", userPubkeyHex[:16])
		data := cachedToHTMLWalletInfo(result)
		html, err := renderWalletInfoFragment(data)
		if err != nil {
			slog.Error("wallet info: failed to render prefetch data", "error", err)
			http.Error(w, "Failed to render wallet info", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(html))
		return
	}

	// No cache, no prefetch - fetch directly
	slog.Debug("wallet info: fetching from NWC", "user", userPubkeyHex[:16])
	cached := fetchWalletInfo(ctx, userPubkeyHex, session.NWCConfig)
	if cached == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write([]byte(`<div class="wallet-info-error">` + I18n("wallet.connection_failed") + `</div>`))
		return
	}

	// Cache the result
	SetCachedWalletInfo(userPubkeyHex, cached)

	data := cachedToHTMLWalletInfo(cached)
	html, err := renderWalletInfoFragment(data)
	if err != nil {
		slog.Error("wallet info: failed to render", "error", err)
		http.Error(w, "Failed to render wallet info", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(html))
}

// fetchWalletInfo fetches balance and transactions from NWC and returns cached format
func fetchWalletInfo(ctx context.Context, userPubkeyHex string, config *NWCConfig) *CachedWalletInfo {
	// Get pooled NWC client
	nwcClient, err := GetPooledNWCClient(ctx, userPubkeyHex, config)
	if err != nil {
		slog.Warn("fetchWalletInfo: failed to get pooled client", "error", err)
		return nil
	}

	result := &CachedWalletInfo{}

	// Get balance
	balanceResult, err := nwcClient.GetBalance(ctx)
	if err != nil {
		slog.Warn("fetchWalletInfo: failed to get balance", "error", err)
		result.Error = I18n("wallet.balance_failed")
	} else {
		result.BalanceMsats = balanceResult.Balance
		sats := balanceResult.Balance / 1000
		result.Balance = formatSatsWithCommas(sats)
	}

	// Get recent transactions (limit to 10)
	txResult, err := nwcClient.ListTransactions(ctx, 10)
	if err != nil {
		slog.Debug("fetchWalletInfo: failed to get transactions", "error", err)
		// Not critical, just don't show transactions
	} else {
		// First pass: parse transactions and collect pubkeys for profile fetch
		var pubkeysToFetch []string
		for _, tx := range txResult.Transactions {
			cachedTx := CachedWalletTransaction{
				Type:        tx.Type,
				AmountMsats: tx.Amount,
				Amount:      formatSatsWithCommas(tx.Amount / 1000),
				Description: truncateWithEllipsis(tx.Description, 40),
				CreatedAt:   tx.CreatedAt,
				TimeAgo:     formatRelativeTime(tx.CreatedAt),
			}
			if tx.Type == "incoming" {
				cachedTx.TypeIcon = "↓"
			} else {
				cachedTx.TypeIcon = "↑"
			}

			// Try to parse zap context from description
			if zapPubkey := parseZapPubkeyFromDescription(tx.Description, tx.Type); zapPubkey != "" {
				cachedTx.IsZap = true
				cachedTx.ZapPubkey = zapPubkey
				pubkeysToFetch = append(pubkeysToFetch, zapPubkey)
			}

			result.Transactions = append(result.Transactions, cachedTx)
		}

		// Batch fetch profiles for zap pubkeys
		if len(pubkeysToFetch) > 0 {
			relays := ConfigGetProfileRelays()
			profiles := fetchProfiles(relays, pubkeysToFetch)

			// Second pass: update transactions with display names
			for i := range result.Transactions {
				tx := &result.Transactions[i]
				if tx.IsZap && tx.ZapPubkey != "" {
					displayName := "someone"
					if profile, ok := profiles[tx.ZapPubkey]; ok && profile != nil {
						if profile.DisplayName != "" {
							displayName = profile.DisplayName
						} else if profile.Name != "" {
							displayName = profile.Name
						}
					}
					if tx.Type == "outgoing" {
						tx.ZapDisplayName = "Zapped " + displayName
					} else {
						tx.ZapDisplayName = "From " + displayName
					}
				}
			}
		}
	}

	return result
}

// parseZapPubkeyFromDescription tries to parse a zap request JSON from transaction description
// Returns the relevant pubkey (recipient for outgoing, sender for incoming) or empty string
func parseZapPubkeyFromDescription(description string, txType string) string {
	if description == "" {
		return ""
	}

	// Try to parse as zap request JSON (kind 9734)
	var zapRequest struct {
		Kind   int        `json:"kind"`
		PubKey string     `json:"pubkey"` // Sender of the zap
		Tags   [][]string `json:"tags"`
	}

	if err := json.Unmarshal([]byte(description), &zapRequest); err != nil {
		return ""
	}

	// Verify it looks like a zap request (kind 9734)
	if zapRequest.Kind != 9734 {
		return ""
	}

	if txType == "outgoing" {
		// Outgoing zap: find recipient from p tag
		for _, tag := range zapRequest.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				return tag[1]
			}
		}
	} else {
		// Incoming zap: sender is the pubkey of the zap request
		return zapRequest.PubKey
	}

	return ""
}

// cachedToHTMLWalletInfo converts cached wallet info to HTML template data
func cachedToHTMLWalletInfo(cached *CachedWalletInfo) HTMLWalletInfoData {
	data := HTMLWalletInfoData{
		Balance:      cached.Balance,
		BalanceMsats: cached.BalanceMsats,
		Error:        cached.Error,
	}

	for _, tx := range cached.Transactions {
		data.Transactions = append(data.Transactions, HTMLWalletTransaction{
			Type:           tx.Type,
			TypeIcon:       tx.TypeIcon,
			Amount:         tx.Amount,
			AmountMsats:    tx.AmountMsats,
			Description:    tx.Description,
			TimeAgo:        tx.TimeAgo,
			CreatedAt:      tx.CreatedAt,
			IsZap:          tx.IsZap,
			ZapDisplayName: tx.ZapDisplayName,
		})
	}

	return data
}

// PrefetchWalletInfo starts a background fetch of wallet info
// Called from wallet page handler to warm the cache
func PrefetchWalletInfo(userPubkeyHex string, config *NWCConfig) {
	// Check cache first - if already cached, no need to prefetch
	if _, found := GetCachedWalletInfo(userPubkeyHex); found {
		slog.Debug("wallet prefetch: already cached", "user", userPubkeyHex[:16])
		return
	}

	// Try to start prefetch
	resultCh := StartWalletInfoPrefetch(userPubkeyHex)
	if resultCh == nil {
		slog.Debug("wallet prefetch: already in progress", "user", userPubkeyHex[:16])
		return
	}

	// Run fetch in background
	go func() {
		slog.Debug("wallet prefetch: starting", "user", userPubkeyHex[:16])
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		result := fetchWalletInfo(ctx, userPubkeyHex, config)
		if result != nil {
			SetCachedWalletInfo(userPubkeyHex, result)
			slog.Debug("wallet prefetch: complete", "user", userPubkeyHex[:16])
		} else {
			slog.Debug("wallet prefetch: failed", "user", userPubkeyHex[:16])
		}

		CompleteWalletInfoPrefetch(userPubkeyHex, result)
	}()
}

// formatSatsWithCommas formats a number with comma separators
func formatSatsWithCommas(n int64) string {
	if n < 0 {
		return "-" + formatSatsWithCommas(-n)
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return formatSatsWithCommas(n/1000) + "," + fmt.Sprintf("%03d", n%1000)
}

// truncateWithEllipsis truncates a string to maxLen characters with ellipsis
func truncateWithEllipsis(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// renderWalletInfoFragment renders the wallet info HTML fragment using compiled template
func renderWalletInfoFragment(data HTMLWalletInfoData) (string, error) {
	var buf strings.Builder
	if err := cachedWalletInfoFragment.ExecuteTemplate(&buf, "wallet-info", data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// htmlZapHandler handles zap requests (NIP-57)
func htmlZapHandler(w http.ResponseWriter, r *http.Request) {
	slog.Info("=== ZAP HANDLER START ===")
	if r.Method != http.MethodPost {
		http.Redirect(w, r, DefaultTimelineURL(), http.StatusSeeOther)
		return
	}

	slog.Info("zap: method is POST, checking auth")
	session := requireAuth(w, r)
	if session == nil {
		slog.Info("zap: no session, returning")
		return
	}
	slog.Info("zap: auth OK", "user", hex.EncodeToString(session.UserPubKey)[:16])

	// Validate CSRF token
	csrfToken := r.FormValue("csrf_token")
	if !requireCSRF(w, session.ID, csrfToken) {
		slog.Warn("zap: CSRF validation failed")
		return
	}
	slog.Info("zap: CSRF OK")

	eventID := strings.TrimSpace(r.FormValue("event_id"))
	eventPubkey := strings.TrimSpace(r.FormValue("event_pubkey"))
	kindStr := strings.TrimSpace(r.FormValue("kind"))
	returnURL := sanitizeReturnURL(r.FormValue("return_url"), true)

	slog.Info("zap: form values", "eventID", eventID, "eventPubkey", eventPubkey, "kind", kindStr)

	if eventID == "" || !isValidEventID(eventID) {
		slog.Warn("zap: invalid event ID", "eventID", eventID)
		respondWithError(w, r, returnURL, "Invalid event ID")
		return
	}

	if eventPubkey == "" || len(eventPubkey) != 64 {
		slog.Warn("zap: invalid pubkey", "pubkey", eventPubkey, "len", len(eventPubkey))
		respondWithError(w, r, returnURL, "Invalid recipient pubkey")
		return
	}
	slog.Info("zap: validation OK")

	// Parse kind for footer rendering
	kind := 1 // Default to kind 1 (note)
	if kindStr != "" {
		if k, err := strconv.Atoi(kindStr); err == nil {
			kind = k
		}
	}

	// Helper to render footer fragment for error responses (preserves UI)
	renderFooterForError := func() string {
		newCSRFToken := generateCSRFToken(session.ID)
		readRelays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			readRelays = session.UserRelayList.Read
		}
		isBookmarked := session.IsEventBookmarked(eventID)
		isReacted := session.IsEventReacted(eventID)
		isReposted := session.IsEventReposted(eventID)
		html, err := renderFooterFragment(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, isReacted, isReposted, false, session.HasWallet(), "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment for error", "error", err)
			return ""
		}
		return html
	}

	// Check wallet connection - show error with link to wallet setup
	slog.Info("zap: checking wallet", "hasWallet", session.HasWallet())
	if !session.HasWallet() {
		slog.Warn("zap: no wallet connected")
		walletURL := "/html/wallet?return=" + url.QueryEscape(returnURL)
		if isHelmRequest(r) {
			// For HelmJS: show error with link, keep footer intact
			errorMsg := I18n("wallet.connect_to_zap") + ` <a href="` + template.HTMLEscapeString(walletURL) + `">` + I18n("wallet.connect") + `</a>`
			respondWithErrorAndFragment(w, r, returnURL, errorMsg, renderFooterForError())
		} else {
			// For regular requests: redirect to wallet page
			setFlashError(w, r, I18n("wallet.connect_to_zap"))
			http.Redirect(w, r, walletURL, http.StatusSeeOther)
		}
		return
	}
	slog.Info("zap: wallet OK")

	// Fetch recipient's profile to get their lightning address
	slog.Info("zap: fetching profile", "pubkey", eventPubkey[:16])
	relays := ConfigGetProfileRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		relays = session.UserRelayList.Read
	}

	profiles := fetchProfiles(relays, []string{eventPubkey})
	profile := profiles[eventPubkey]
	if profile == nil {
		slog.Warn("zap: profile not found", "pubkey", eventPubkey[:16])
		respondWithErrorAndFragment(w, r, returnURL, "Could not find recipient's profile", renderFooterForError())
		return
	}

	// Check if recipient can receive zaps
	if !CanReceiveZaps(profile) {
		slog.Warn("zap: recipient can't receive zaps", "pubkey", eventPubkey[:16])
		respondWithErrorAndFragment(w, r, returnURL, "Recipient has no Lightning address configured", renderFooterForError())
		return
	}

	// Resolve LNURL to get payment info
	slog.Info("zap: resolving LNURL", "lud16", profile.Lud16)
	lnurlInfo, err := ResolveLNURLFromProfile(profile)
	if err != nil {
		slog.Error("zap: failed to resolve LNURL", "error", err, "pubkey", eventPubkey[:8])
		respondWithErrorAndFragment(w, r, returnURL, "Could not resolve recipient's Lightning address", renderFooterForError())
		return
	}
	slog.Info("zap: LNURL resolved", "allowsNostr", lnurlInfo.AllowsNostr, "nostrPubkey", lnurlInfo.NostrPubkey != "")

	// Fixed zap amount: 21 sats = 21000 msats
	amountMsats := int64(21000)

	// Check amount is within recipient's limits
	if amountMsats < lnurlInfo.MinSendable {
		respondWithErrorAndFragment(w, r, returnURL, fmt.Sprintf("Amount below minimum (%d sats)", MsatsToSats(lnurlInfo.MinSendable)), renderFooterForError())
		return
	}
	if amountMsats > lnurlInfo.MaxSendable {
		respondWithErrorAndFragment(w, r, returnURL, fmt.Sprintf("Amount above maximum (%d sats)", MsatsToSats(lnurlInfo.MaxSendable)), renderFooterForError())
		return
	}

	// Verify LNURL endpoint supports nostr zaps
	if !lnurlInfo.AllowsNostr {
		slog.Warn("zap: endpoint doesn't support nostr zaps", "pubkey", eventPubkey[:16])
		respondWithErrorAndFragment(w, r, returnURL, "Recipient's Lightning address doesn't support Nostr zaps", renderFooterForError())
		return
	}
	slog.Info("zap: creating zap request event")

	// Create zap request event (kind 9734)
	// NIP-57: Zap request tags
	zapRequestTags := [][]string{
		{"relays"}, // Will be populated with user's write relays
		{"amount", fmt.Sprintf("%d", amountMsats)},
		{"p", eventPubkey},
		{"e", eventID},
		{"k", kindStr}, // Event kind for proper client display
	}

	// Add relays to the relays tag
	writeRelays := ConfigGetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		writeRelays = session.UserRelayList.Write
	}
	for _, relay := range writeRelays {
		zapRequestTags[0] = append(zapRequestTags[0], relay)
	}

	// Note: lnurl tag omitted - we use lud16 (lightning address) which is different
	// from bech32-encoded lnurl. The lnurl tag is optional per NIP-57.
	lnurl := profile.Lud16

	zapRequest := UnsignedEvent{
		Kind:      9734,
		Content:   "", // Can add optional comment in future
		Tags:      zapRequestTags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign zap request with user's bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedZapRequest, err := session.SignEvent(ctx, zapRequest)
	if err != nil {
		slog.Error("failed to sign zap request", "error", err)
		respondWithErrorAndFragment(w, r, returnURL, sanitizeErrorForUser("Sign zap request", err), renderFooterForError())
		return
	}

	// Convert signed event to JSON for LNURL callback
	zapRequestJSON, err := json.Marshal(signedZapRequest)
	if err != nil {
		slog.Error("failed to marshal zap request", "error", err)
		respondWithErrorAndFragment(w, r, returnURL, "Failed to create zap request", renderFooterForError())
		return
	}

	// Request invoice from LNURL endpoint
	invoice, err := RequestInvoice(lnurlInfo, amountMsats, string(zapRequestJSON), lnurl)
	if err != nil {
		slog.Error("failed to get invoice", "error", err, "pubkey", eventPubkey[:8])
		respondWithErrorAndFragment(w, r, returnURL, "Could not get Lightning invoice from recipient", renderFooterForError())
		return
	}

	// Get pooled NWC client (reuses existing connection if available)
	slog.Info("zap: getting NWC client from pool")
	userPubkeyHex := hex.EncodeToString(session.UserPubKey)
	nwcClient, err := GetPooledNWCClient(ctx, userPubkeyHex, session.NWCConfig)
	if err != nil {
		slog.Error("zap: failed to get pooled client", "error", err)
		respondWithErrorAndFragment(w, r, returnURL, "Could not connect to your wallet", renderFooterForError())
		return
	}
	// Note: Do NOT close the client - it's managed by the pool

	slog.Info("zap: paying invoice via NWC")
	result, err := nwcClient.PayInvoice(ctx, invoice)
	if err != nil {
		slog.Error("failed to pay invoice", "error", err, "pubkey", eventPubkey[:8])
		// Provide more specific error messages for common NWC errors
		errMsg := err.Error()
		footerHTML := renderFooterForError()
		if strings.Contains(errMsg, NWCErrorInsufficientBalance) {
			respondWithErrorAndFragment(w, r, returnURL, "Insufficient wallet balance", footerHTML)
		} else if strings.Contains(errMsg, NWCErrorPaymentFailed) {
			respondWithErrorAndFragment(w, r, returnURL, "Payment failed - recipient may be offline", footerHTML)
		} else if strings.Contains(errMsg, "timeout") {
			respondWithErrorAndFragment(w, r, returnURL, "Payment timed out - please try again", footerHTML)
		} else {
			respondWithErrorAndFragment(w, r, returnURL, "Payment failed", footerHTML)
		}
		return
	}

	slog.Info("zap sent successfully",
		"user", hex.EncodeToString(session.UserPubKey)[:8],
		"recipient", eventPubkey[:8],
		"event", eventID[:8],
		"amount_sats", MsatsToSats(amountMsats),
		"preimage", result.Preimage[:16]+"...")

	// Track this zap in the session for UI state
	session.AddZappedEvent(eventID)
	// Save session to persist the zapped state
	if err := sessionStore.Set(r.Context(), session); err != nil {
		slog.Warn("failed to save session after zap", "error", err)
		// Don't fail the request - zap was successful
	}

	// For HelmJS requests, return the updated footer fragment
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		readRelays := ConfigGetDefaultRelays()
		if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
			readRelays = session.UserRelayList.Read
		}
		// Check other state for footer display
		isBookmarked := session.IsEventBookmarked(eventID)
		isReacted := session.IsEventReacted(eventID)
		isReposted := session.IsEventReposted(eventID)
		// isZapped is now true since user just zapped, hasWallet is true since we got here
		// Note: kind was already parsed at the start of the handler
		html, err := renderFooterFragment(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, isReacted, isReposted, true, true, "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			http.Error(w, "Failed to render response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	// Redirect with success
	redirectWithSuccess(w, r, returnURL, "Zapped 21 sats ⚡")
}
