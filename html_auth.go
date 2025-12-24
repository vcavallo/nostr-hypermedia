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

	cfgpkg "nostr-server/internal/config"
	"nostr-server/internal/nips"
	"nostr-server/internal/util"
	"nostr-server/templates"
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

// addClientTag adds the NIP-89 client tag to an event if enabled for that kind
func addClientTag(event *UnsignedEvent) {
	clientConfig := cfgpkg.GetClientConfig()
	if clientTag := clientConfig.GetClientTag(); clientTag != nil && clientConfig.ShouldTagKind(event.Kind) {
		event.Tags = append(event.Tags, clientTag)
	}
}

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
			// Not enough IPs for trusted proxy count - header might be spoofed
			// Fall back to RemoteAddr for safety instead of trusting index 0
			return parseRemoteAddr(r.RemoteAddr)
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
	cachedLoginTemplate, err = template.New("login").Funcs(templateFuncMap).Parse(htmlLoginTemplate)
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
	if pubkeyHex == "" {
		return
	}
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
			slog.Debug("prefetchUserProfile timed out", "pubkey", shortID(pubkeyHex))
		}
	}()
}

// prefetchUserContactList caches who the user follows and prefetches their profiles (call after login)
func prefetchUserContactList(session *BunkerSession, relays []string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
		defer cancel()

		pubkeyHex := hex.EncodeToString(session.UserPubKey)
		if pubkeyHex == "" {
			slog.Debug("prefetchUserContactList skipped: no user pubkey")
			return
		}

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
			slog.Debug("prefetchUserContactList timed out", "pubkey", shortID(pubkeyHex))
		}
	}()
}

// prefetchContactProfiles prefetches profiles for followed users in batches
func prefetchContactProfiles(contacts []string) {
	const batchSize = 50
	profileRelays := cfgpkg.GetProfileRelays()

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
		if pubkeyHex == "" {
			slog.Debug("prefetch skipped: no user pubkey")
			return
		}
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
				session.mu.Lock()
				session.BookmarkedEventIDs = util.GetTagValues(bookmarkEvents[0].Tags, "e")
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
					if eid := util.GetTagValue(event.Tags, "e"); eid != "" {
						eventIDs = append(eventIDs, eid)
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
					if eid := util.GetTagValue(event.Tags, "e"); eid != "" {
						eventIDs = append(eventIDs, eid)
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
			slog.Debug("fetching mute list", "pubkey", shortID(pubkeyHex), "relays", len(relays))
			muteEvents := fetchKind10000(relays, pubkeyHex)
			slog.Debug("mute list fetch result", "pubkey", shortID(pubkeyHex), "events", len(muteEvents))
			if len(muteEvents) > 0 {
				pubkeys, eventIDs, hashtags, words := parseMuteList(muteEvents[0])
				session.mu.Lock()
				session.MutedPubkeys = pubkeys
				session.MutedEventIDs = eventIDs
				session.MutedHashtags = hashtags
				session.MutedWords = words
				session.mu.Unlock()
				slog.Debug("loaded mute list", "pubkey", shortID(pubkeyHex),
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
			slog.Debug("prefetchUserData timed out", "pubkey", shortID(pubkeyHex))
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
	return "@" + shortID(pubkeyHex) + "..."
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
	profiles := fetchProfilesWithTimeout(cfgpkg.GetProfileRelays(), []string{pubkeyHex}, 500*time.Millisecond)
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

	// Check if we're returning from a check-connection attempt with an existing secret
	// This preserves the pending connection when the user needs to retry
	existingSecret := r.URL.Query().Get("secret")
	var nostrConnectURL, secret string
	var err error

	if existingSecret != "" {
		// Verify this pending connection still exists
		pending := pendingConnections.Get(existingSecret)
		if pending != nil {
			// Reuse the existing secret and regenerate the nostrconnect URL
			secret = existingSecret
			kp, kpErr := GetServerKeypair()
			if kpErr == nil {
				u := url.URL{
					Scheme: "nostrconnect",
					Host:   hex.EncodeToString(kp.PubKey),
				}
				q := u.Query()
				for _, relay := range pending.Relays {
					q.Add("relay", relay)
				}
				q.Set("secret", secret)
				q.Set("name", "Nostr Hypermedia")
				q.Set("perms", "sign_event:1")
				u.RawQuery = q.Encode()
				nostrConnectURL = u.String()
			}
		}
	}

	// Generate new nostrconnect URL if we don't have an existing one
	if nostrConnectURL == "" {
		nostrConnectURL, secret, err = GenerateNostrConnectURL(defaultNostrConnectRelays())
		if err != nil {
			slog.Error("failed to generate nostrconnect URL", "error", err)
		}
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
	SetCookie(w, r, anonSessionCookieName, anonSessionID, "/login", anonSessionMaxAge, http.SameSiteStrictMode)

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
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get anonymous session from cookie for CSRF validation
	anonCookie, err := r.Cookie(anonSessionCookieName)
	if err != nil {
		redirectWithError(w, r, "/login", "Session expired, please try again")
		return
	}

	// Validate CSRF token against this anonymous session
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(anonCookie.Value, csrfToken) {
		redirectWithError(w, r, "/login", "Invalid security token, please try again")
		return
	}

	// Clear the anonymous session cookie (it's single-use)
	DeleteCookie(w, r, anonSessionCookieName, "/login")

	// Rate limit login attempts by IP
	if err := CheckLoginRateLimit(getClientIP(r)); err != nil {
		redirectWithError(w, r, "/login", "Too many login attempts. Please wait a minute and try again.")
		return
	}

	bunkerURL := strings.TrimSpace(r.FormValue("bunker_url"))
	if bunkerURL == "" {
		redirectWithError(w, r, "/login", "Please enter a bunker URL")
		return
	}

	// Parse bunker URL
	session, err := ParseBunkerURL(bunkerURL)
	if err != nil {
		redirectWithError(w, r, "/login", sanitizeErrorForUser("Parse bunker URL", err))
		return
	}

	// Attempt to connect (with timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := session.Connect(ctx); err != nil {
		redirectWithError(w, r, "/login", sanitizeErrorForUser("Connect to bunker", err))
		return
	}

	// Store session
	bunkerSessions.Set(session)

	// Set session cookie
	SetSessionCookie(w, r, sessionCookieName, session.ID, int(sessionMaxAge.Seconds()))

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
	// Note: Connection checks are user-initiated polling, separate from login attempts.
	// We don't rate-limit these aggressively since users may need to click multiple times
	// while waiting for their signer app to respond.

	secret := r.URL.Query().Get("secret")
	if secret == "" {
		redirectWithError(w, r, "/login", "Missing connection secret")
		return
	}

	session := CheckConnection(secret)
	if session == nil {
		// Not connected yet - keep secret in URL so user can retry
		setFlashError(w, r, "Connection not ready. Make sure you approved in your signer app, then try again.")
		http.Redirect(w, r, util.BuildURL("/login", map[string]string{"secret": secret}), http.StatusSeeOther)
		return
	}

	// Connected! Store session and set cookie
	bunkerSessions.Set(session)

	SetSessionCookie(w, r, sessionCookieName, session.ID, int(sessionMaxAge.Seconds()))

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
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Get anonymous session from cookie for CSRF validation
	anonCookie, err := r.Cookie(anonSessionCookieName)
	if err != nil {
		redirectWithError(w, r, "/login", "Session expired, please try again")
		return
	}

	// Validate CSRF token against this anonymous session
	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(anonCookie.Value, csrfToken) {
		redirectWithError(w, r, "/login", "Invalid security token, please try again")
		return
	}

	// Clear the anonymous session cookie (it's single-use)
	DeleteCookie(w, r, anonSessionCookieName, "/login")

	signerPubKey := strings.TrimSpace(r.FormValue("signer_pubkey"))
	if signerPubKey == "" {
		redirectWithError(w, r, "/login", "Please enter your signer pubkey")
		return
	}

	// Handle npub format
	if strings.HasPrefix(signerPubKey, "npub1") {
		// Decode bech32 npub to hex
		decoded, err := decodeBech32Pubkey(signerPubKey)
		if err != nil {
			redirectWithError(w, r, "/login", "Invalid npub format")
			return
		}
		signerPubKey = decoded
	}

	// Validate hex
	if len(signerPubKey) != 64 {
		redirectWithError(w, r, "/login", "Invalid pubkey length (expected 64 hex chars or npub)")
		return
	}

	session, err := TryReconnectToSigner(signerPubKey, defaultNostrConnectRelays())
	if err != nil {
		redirectWithError(w, r, "/login", sanitizeErrorForUser("Reconnect to signer", err))
		return
	}

	// Success! Store session and set cookie
	bunkerSessions.Set(session)

	SetSessionCookie(w, r, sessionCookieName, session.ID, int(sessionMaxAge.Seconds()))

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
	_, data, err := nips.Bech32Decode(npub)
	if err != nil {
		return "", err
	}

	// Convert 5-bit groups to 8-bit bytes
	decoded, err := nips.Bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return "", err
	}

	if len(decoded) != 32 {
		return "", errors.New("invalid pubkey length")
	}

	return hex.EncodeToString(decoded), nil
}

// encodeBech32Pubkey encodes a hex pubkey to npub format
func encodeBech32Pubkey(hexPubkey string) (string, error) {
	return nips.EncodePubkey(hexPubkey)
}

// encodeBech32EventID encodes a hex event ID to note format
func encodeBech32EventID(hexEventID string) (string, error) {
	return nips.EncodeEventID(hexEventID)
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
	DeleteCookie(w, r, sessionCookieName, "/")

	redirectWithSuccess(w, r, "/login", "Logged out")
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
	contentWarning := strings.TrimSpace(r.FormValue("content_warning"))
	contentWarningCustom := strings.TrimSpace(r.FormValue("content_warning_custom"))
	mentionsJSON := r.FormValue("mentions")

	content = util.AppendGifToContent(content, gifURL)

	// Process @mentions: replace @displayname with nostr:nprofile1...
	content, mentionedPubkeys := processMentions(content, mentionsJSON)

	if content == "" {
		redirectWithError(w, r, DefaultTimelineURL(), "You cannot post an empty note!")
		return
	}

	// Build tags
	tags := [][]string{}

	// Add p-tags for mentioned users (enables notifications per NIP-27)
	for _, pubkey := range mentionedPubkeys {
		tags = append(tags, []string{"p", pubkey})
	}

	// Add content-warning tag if specified (NIP-36)
	// Custom reason takes priority over preset
	if contentWarningCustom != "" {
		tags = append(tags, []string{"content-warning", contentWarningCustom})
	} else if contentWarning != "" {
		tags = append(tags, []string{"content-warning", contentWarning})
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      1,
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	addClientTag(&event)

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign event", "error", err)
		if isHelmRequest(r) {
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		redirectWithError(w, r, DefaultTimelineURL(), sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays following outbox model (author write + tagged users' read)
	relays := getPublishRelaysForEvent(session, signedEvent)
	publishEvent(ctx, relays, signedEvent)

	// For HelmJS requests, return the cleared form + success flash
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		html, err := renderPostResponse(newCSRFToken, nil)
		if err != nil {
			slog.Error("failed to render post response", "error", err)
			util.RespondInternalError(w, "Failed to render response")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html + renderOOBFlash("Note published", "success")))
		return
	}

	redirectWithSuccess(w, r, DefaultTimelineURLLoggedIn(), "Note published")
}

// htmlReplyHandler handles replying to events via POST form
// For kind 1 notes: creates NIP-10 reply (kind 1)
// For other kinds: creates NIP-22 comment (kind 1111)
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
	replyToKindStr := r.FormValue("reply_to_kind")
	replyToDTag := strings.TrimSpace(r.FormValue("reply_to_dtag"))
	replyToRoot := strings.TrimSpace(r.FormValue("reply_to_root"))
	replyCountStr := r.FormValue("reply_count")
	contentWarning := strings.TrimSpace(r.FormValue("content_warning"))
	contentWarningCustom := strings.TrimSpace(r.FormValue("content_warning_custom"))
	mentionsJSON := r.FormValue("mentions")

	content = util.AppendGifToContent(content, gifURL)

	// Process @mentions: replace @displayname with nostr:nprofile1...
	content, mentionedPubkeys := processMentions(content, mentionsJSON)

	// Parse target kind, default to 1 (note)
	replyToKind := 1
	if replyToKindStr != "" {
		if k, err := strconv.Atoi(replyToKindStr); err == nil && k > 0 {
			replyToKind = k
		}
	}

	// Validate event ID first to prevent path injection
	if replyTo == "" || !isValidEventID(replyTo) {
		redirectWithError(w, r, DefaultTimelineURL(), "Invalid reply target")
		return
	}

	if content == "" {
		replyToNote, _ := nips.EncodeEventID(replyTo)
		if replyToNote == "" {
			replyToNote = replyTo
		}
		redirectWithError(w, r, "/thread/"+replyToNote, "You cannot post an empty note!")
		return
	}

	// Determine event kind and build tags based on target event kind
	// NIP-10 for kind 1 notes, NIP-22 for everything else
	var eventKind int
	var tags [][]string

	if replyToKind == 1 {
		// NIP-10 reply (kind 1): use explicit root and reply markers
		eventKind = 1

		// Determine if we're replying to the thread root or a nested reply
		if replyToRoot == "" || replyToRoot == replyTo {
			// Replying directly to the thread root: single tag with "root" marker
			tags = [][]string{
				{"e", replyTo, "", "root"},
			}
		} else {
			// Replying to a nested reply: both root and reply tags
			tags = [][]string{
				{"e", replyToRoot, "", "root"},
				{"e", replyTo, "", "reply"},
			}
		}

		if replyToPubkey != "" {
			if decoded, err := hex.DecodeString(replyToPubkey); err == nil && len(decoded) == 32 {
				tags = append(tags, []string{"p", replyToPubkey})
			}
		}
	} else {
		// NIP-22 comment (kind 1111): uppercase tags for root, lowercase for parent
		eventKind = 1111
		kindStr := strconv.Itoa(replyToKind)

		// For addressable events (kind 30xxx), use A tag; otherwise use E tag
		if replyToKind >= 30000 && replyToKind < 40000 && replyToDTag != "" && replyToPubkey != "" {
			// Addressable event: A tag format is "kind:pubkey:d-tag"
			aTagValue := fmt.Sprintf("%d:%s:%s", replyToKind, replyToPubkey, replyToDTag)
			tags = [][]string{
				{"A", aTagValue, ""},      // Root scope (uppercase)
				{"a", aTagValue, ""},      // Parent (same as root for top-level comment)
				{"K", kindStr},            // Root kind
				{"k", kindStr},            // Parent kind
			}
		} else {
			// Regular event: E tag
			tags = [][]string{
				{"E", replyTo, ""},        // Root scope (uppercase)
				{"e", replyTo, ""},        // Parent (same as root for top-level comment)
				{"K", kindStr},            // Root kind
				{"k", kindStr},            // Parent kind
			}
		}
		// Add author p/P tags
		if replyToPubkey != "" {
			if decoded, err := hex.DecodeString(replyToPubkey); err == nil && len(decoded) == 32 {
				tags = append(tags, []string{"P", replyToPubkey}) // Root author
				tags = append(tags, []string{"p", replyToPubkey}) // Parent author
			}
		}
	}

	// Add p-tags for mentioned users (enables notifications per NIP-27)
	for _, pubkey := range mentionedPubkeys {
		tags = append(tags, []string{"p", pubkey})
	}

	// Add content-warning tag if specified (NIP-36)
	// Custom reason takes priority over preset
	if contentWarningCustom != "" {
		tags = append(tags, []string{"content-warning", contentWarningCustom})
	} else if contentWarning != "" {
		tags = append(tags, []string{"content-warning", contentWarning})
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      eventKind,
		Content:   content,
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	addClientTag(&event)

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign reply", "error", err)
		if isHelmRequest(r) {
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		replyToNote, _ := nips.EncodeEventID(replyTo)
		if replyToNote == "" {
			replyToNote = replyTo
		}
		redirectWithError(w, r, "/thread/"+replyToNote, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Publish to relays following outbox model (author write + tagged users' read)
	relays := getPublishRelaysForEvent(session, signedEvent)
	publishEvent(ctx, relays, signedEvent)

	// For HelmJS requests, return the cleared form + new reply as OOB
	if isHelmRequest(r) {
		// Generate new CSRF token
		newCSRFToken := generateCSRFToken(session.ID)

		// Build HTMLEventItem for the new reply
		userPubkey := hex.EncodeToString(session.UserPubKey)
		npub, _ := encodeBech32Pubkey(userPubkey)

		// Get user's profile for display
		authorProfile := getCachedProfile(userPubkey)

		// Get user display name and avatar
		userDisplayName := getUserDisplayName(userPubkey)
		userAvatarURL := getUserAvatarURL(userPubkey)

		// Build actions for the new reply/comment
		replyToNote, _ := nips.EncodeEventID(replyTo)
		if replyToNote == "" {
			replyToNote = replyTo
		}
		returnURL := "/thread/" + replyToNote
		actionCtx := ActionContext{
			EventID:      signedEvent.ID,
			EventPubkey:  userPubkey,
			Kind:         eventKind,
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
		actionGroups := GroupActionsForKind(entity.Actions, eventKind)

		// Get template name for the created event kind
		kindDef := GetKindDefinition(eventKind)

		newReply := &HTMLEventItem{
			ID:             signedEvent.ID,
			Pubkey:         userPubkey,
			Npub:           npub,
			NpubShort:      formatNpubShort(npub),
			Kind:           eventKind,
			TemplateName:   kindDef.TemplateName,
			RenderTemplate: "render-" + kindDef.TemplateName,
			Tags:           signedEvent.Tags,
			Content:        content,
			ContentHTML:    processContentToHTML(content),
			AuthorProfile:  authorProfile,
			CreatedAt:      signedEvent.CreatedAt,
			ActionGroups:   actionGroups,
			LoggedIn:       true,
		}

		// Increment reply count from form (avoids relay query, matches displayed count)
		replyCount := 1
		if n, err := strconv.Atoi(replyCountStr); err == nil {
			replyCount = n + 1
		}

		html, err := renderReplyResponse(newCSRFToken, replyTo, replyToPubkey, replyToKind, replyToDTag, replyToRoot, userDisplayName, userAvatarURL, npub, newReply, replyCount)
		if err != nil {
			slog.Error("failed to render reply response", "error", err)
			util.RespondInternalError(w, "Failed to render response")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	replyToNoteFinal, _ := nips.EncodeEventID(replyTo)
	if replyToNoteFinal == "" {
		replyToNoteFinal = replyTo
	}
	redirectWithSuccess(w, r, "/thread/"+replyToNoteFinal, "Reply published")
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

	addClientTag(&event)

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign reaction", "error", err)
		// For HelmJS requests, return an error response
		if isHelmRequest(r) {
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Update session's reaction cache optimistically (before publish)
	session.mu.Lock()
	session.ReactedEventIDs = append(session.ReactedEventIDs, eventID)
	session.mu.Unlock()

	// Get read relays for rendering footer
	readRelays := cfgpkg.GetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		readRelays = session.UserRelayList.Read
	}

	// Check current state for footer display
	isBookmarked := session.IsEventBookmarked(eventID)
	isReposted := session.IsEventReposted(eventID)
	hasWallet := session.HasWallet()

	// Capture data for failure callback (will run async after response is sent)
	sessionID := session.ID

	// Publish asynchronously - response is sent before relay confirmation
	relays := getPublishRelaysForEvent(session, signedEvent)
	publishEventAsync(relays, signedEvent, func(failedEventID string, err error) {
		// Remove from session cache on failure
		session.mu.Lock()
		for i, id := range session.ReactedEventIDs {
			if id == eventID {
				session.ReactedEventIDs = append(session.ReactedEventIDs[:i], session.ReactedEventIDs[i+1:]...)
				break
			}
		}
		session.mu.Unlock()

		// Render corrected footer (isReacted = false, add error indicator)
		newCSRFToken := generateCSRFToken(sessionID)
		html, renderErr := renderFooterFragmentWithError(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, false, isReposted, false, hasWallet, "", readRelays)
		if renderErr != nil {
			slog.Error("failed to render correction footer", "error", renderErr)
			return
		}

		// Send correction via SSE
		SendCorrectionToSession(sessionID, "#footer-"+eventID, html, "react")
	})

	// For HelmJS requests, return the updated footer fragment immediately (optimistic)
	if isHelmRequest(r) {
		// Generate new CSRF token for the updated form
		newCSRFToken := generateCSRFToken(session.ID)
		// Pass the reaction so it shows in the UI, isReacted is true since user just reacted
		html, err := renderFooterFragment(eventID, eventPubkey, kind, true, newCSRFToken, returnURL, isBookmarked, true, isReposted, false, hasWallet, reaction, readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			util.RespondInternalError(w, "Failed to render response")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}

// htmlRepostHandler handles reposting an event (kind 6 for notes, kind 16 for other events)
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

	// Parse target event kind, default to 1 (note) if not provided or invalid
	targetKind := 1
	if kindStr != "" {
		if parsedKind, err := strconv.Atoi(kindStr); err == nil && parsedKind > 0 {
			targetKind = parsedKind
		}
	}

	if eventID == "" || !isValidEventID(eventID) {
		redirectWithError(w, r, DefaultTimelineURL(), "Invalid event ID")
		return
	}

	// Build tags for repost (NIP-18)
	// e tag for the reposted event, p tag to mention the original author
	// k tag for the target event kind (required for kind 16)
	tags := [][]string{
		{"e", eventID, ""},
	}
	if eventPubkey != "" {
		tags = append(tags, []string{"p", eventPubkey})
	}

	// Determine repost kind per NIP-18:
	// - Kind 6: repost of kind 1 notes
	// - Kind 16: generic repost for all other event kinds
	repostKind := 6
	if targetKind != 1 {
		repostKind = 16
		tags = append(tags, []string{"k", strconv.Itoa(targetKind)})
	}

	// Create unsigned event
	event := UnsignedEvent{
		Kind:      repostKind,
		Content:   "", // Repost content is typically empty
		Tags:      tags,
		CreatedAt: time.Now().Unix(),
	}

	addClientTag(&event)

	// Sign via bunker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign repost", "error", err)
		if isHelmRequest(r) {
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Update session cache optimistically (before publish)
	session.mu.Lock()
	session.RepostedEventIDs = append(session.RepostedEventIDs, eventID)
	session.mu.Unlock()

	// Get read relays for rendering footer
	readRelays := cfgpkg.GetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		readRelays = session.UserRelayList.Read
	}

	// Check current state for footer display
	isBookmarked := session.IsEventBookmarked(eventID)
	isReacted := session.IsEventReacted(eventID)
	hasWallet := session.HasWallet()

	// Capture data for failure callback
	sessionID := session.ID

	// Publish asynchronously - response is sent before relay confirmation
	relays := getPublishRelaysForEvent(session, signedEvent)
	publishEventAsync(relays, signedEvent, func(failedEventID string, err error) {
		// Remove from session cache on failure
		session.mu.Lock()
		for i, id := range session.RepostedEventIDs {
			if id == eventID {
				session.RepostedEventIDs = append(session.RepostedEventIDs[:i], session.RepostedEventIDs[i+1:]...)
				break
			}
		}
		session.mu.Unlock()

		// Render corrected footer (isReposted = false)
		newCSRFToken := generateCSRFToken(sessionID)
		html, renderErr := renderFooterFragmentWithError(eventID, eventPubkey, targetKind, true, newCSRFToken, returnURL, isBookmarked, isReacted, false, false, hasWallet, "", readRelays)
		if renderErr != nil {
			slog.Error("failed to render correction footer", "error", renderErr)
			return
		}

		// Send correction via SSE
		SendCorrectionToSession(sessionID, "#footer-"+eventID, html, "repost")
	})

	// For HelmJS partial update, return updated footer immediately (optimistic)
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		// isReposted is true since user just reposted
		html, err := renderFooterFragment(eventID, eventPubkey, targetKind, true, newCSRFToken, returnURL, isBookmarked, isReacted, true, false, hasWallet, "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			util.RespondInternalError(w, "Failed to render response")
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
	relays := cfgpkg.GetPublishRelays()
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
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Update session's bookmark cache optimistically (before publish)
	newBookmarkState := action == "add"
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

	// Get read relays for rendering footer
	readRelays := cfgpkg.GetDefaultRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Read) > 0 {
		readRelays = session.UserRelayList.Read
	}

	// Check current state for footer display
	isReacted := session.IsEventReacted(eventID)
	isReposted := session.IsEventReposted(eventID)
	hasWallet := session.HasWallet()

	// Capture data for failure callback
	sessionID := session.ID
	wasAdding := action == "add"

	// Publish asynchronously - response is sent before relay confirmation
	publishEventAsync(relays, signedEvent, func(failedEventID string, err error) {
		// Revert session cache on failure
		session.mu.Lock()
		if wasAdding {
			// Was adding, so remove it
			newBookmarks := make([]string, 0, len(session.BookmarkedEventIDs))
			for _, id := range session.BookmarkedEventIDs {
				if id != eventID {
					newBookmarks = append(newBookmarks, id)
				}
			}
			session.BookmarkedEventIDs = newBookmarks
		} else {
			// Was removing, so add it back
			session.BookmarkedEventIDs = append(session.BookmarkedEventIDs, eventID)
		}
		session.mu.Unlock()

		// Render corrected footer (revert bookmark state)
		newCSRFToken := generateCSRFToken(sessionID)
		revertedBookmarkState := !wasAdding
		html, renderErr := renderFooterFragmentWithError(eventID, "", kind, true, newCSRFToken, returnURL, revertedBookmarkState, isReacted, isReposted, false, hasWallet, "", readRelays)
		if renderErr != nil {
			slog.Error("failed to render correction footer", "error", renderErr)
			return
		}

		// Send correction via SSE
		SendCorrectionToSession(sessionID, "#footer-"+eventID, html, "bookmark")
	})

	// For HelmJS requests, return the updated footer fragment immediately (optimistic)
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		html, err := renderFooterFragment(eventID, "", kind, true, newCSRFToken, returnURL, newBookmarkState, isReacted, isReposted, false, hasWallet, "", readRelays)
		if err != nil {
			slog.Error("failed to render footer fragment", "error", err)
			util.RespondInternalError(w, "Failed to render response")
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
		util.RespondMethodNotAllowed(w, "Method not allowed")
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
	relays := cfgpkg.GetPublishRelays()
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
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
			return
		}
		redirectWithError(w, r, returnURL, sanitizeErrorForUser("Sign event", err))
		return
	}

	// Update session's mute cache optimistically (before publish)
	newMuteState := action == "mute"
	session.mu.Lock()
	if action == "mute" {
		// Check if already muted to avoid duplicates
		alreadyMuted := false
		for _, pk := range session.MutedPubkeys {
			if pk == pubkeyToMute {
				alreadyMuted = true
				break
			}
		}
		if !alreadyMuted {
			session.MutedPubkeys = append(session.MutedPubkeys, pubkeyToMute)
		}
		slog.Debug("muted user", "pubkey", shortID(pubkeyToMute))
	} else {
		// Remove from mute list
		newMuted := make([]string, 0, len(session.MutedPubkeys))
		for _, pk := range session.MutedPubkeys {
			if pk != pubkeyToMute {
				newMuted = append(newMuted, pk)
			}
		}
		session.MutedPubkeys = newMuted
		slog.Debug("unmuted user", "pubkey", shortID(pubkeyToMute))
	}
	session.mu.Unlock()

	// Capture data for failure callback
	sessionID := session.ID
	wasMuting := action == "mute"

	// Publish asynchronously - response is sent before relay confirmation
	publishEventAsync(relays, signedEvent, func(failedEventID string, err error) {
		// Revert session cache on failure
		session.mu.Lock()
		if wasMuting {
			// Was muting, so remove it
			newMuted := make([]string, 0, len(session.MutedPubkeys))
			for _, pk := range session.MutedPubkeys {
				if pk != pubkeyToMute {
					newMuted = append(newMuted, pk)
				}
			}
			session.MutedPubkeys = newMuted
		} else {
			// Was unmuting, so add it back
			session.MutedPubkeys = append(session.MutedPubkeys, pubkeyToMute)
		}
		session.mu.Unlock()

		// Render corrected mute button (revert mute state)
		newCSRFToken := generateCSRFToken(sessionID)
		revertedMuteState := !wasMuting
		html, renderErr := renderMuteButtonWithError(pubkeyToMute, newCSRFToken, returnURL, revertedMuteState)
		if renderErr != nil {
			slog.Error("failed to render correction mute button", "error", renderErr)
			return
		}

		// Send correction via SSE
		SendCorrectionToSession(sessionID, "#mute-btn-"+pubkeyToMute, html, "mute")
	})

	// For HelmJS requests, return updated mute button immediately (optimistic)
	if isHelmRequest(r) {
		newCSRFToken := generateCSRFToken(session.ID)
		data := map[string]interface{}{
			"Pubkey":    pubkeyToMute,
			"IsMuted":   newMuteState,
			"CSRFToken": newCSRFToken,
			"ReturnURL": returnURL,
		}
		tmpl := util.MustCompileTemplate("mute-button", templateFuncMap, templates.GetMuteButtonTemplate())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "mute-button", data); err != nil {
			slog.Error("failed to render mute button", "error", err)
			util.RespondInternalError(w, "Error rendering response")
		}
		return
	}

	// For non-HelmJS requests, redirect back
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

// htmlReportHandler handles both displaying the report form (GET) and submitting (POST)
func htmlReportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// Handle report submission
		htmlReportSubmitHandler(w, r)
		return
	}

	// GET: Show report form
	// Extract event ID from path: /report/{identifier}
	identifier := strings.TrimPrefix(r.URL.Path, "/report/")
	if identifier == "" {
		util.RespondBadRequest(w, "Invalid event ID")
		return
	}

	// Decode bech32 to hex if needed
	var eventID string
	switch {
	case strings.HasPrefix(identifier, "note1"):
		decoded, err := nips.DecodeNote(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid note ID")
			return
		}
		eventID = decoded
	case strings.HasPrefix(identifier, "nevent1"):
		decoded, err := nips.DecodeNEvent(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid nevent ID")
			return
		}
		eventID = decoded.EventID
	default:
		// Assume hex format
		if !isValidEventID(identifier) {
			util.RespondBadRequest(w, "Invalid event ID")
			return
		}
		eventID = identifier
	}

	themeClass, themeLabel := getThemeFromRequest(r)

	// Check login status
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	relays := cfgpkg.GetDefaultRelays()

	// Fetch the event to be reported
	events := fetchEventByID(relays, eventID)
	if len(events) == 0 {
		util.RespondNotFound(w, "Event not found")
		return
	}
	rawEvent := events[0]

	// Fetch author profile
	profiles := fetchProfiles(relays, []string{rawEvent.PubKey})
	authorProfile := profiles[rawEvent.PubKey]

	// Build HTMLEventItem for rendering
	npub, _ := encodeBech32Pubkey(rawEvent.PubKey)
	resolvedRefs := batchResolveNostrRefs(extractNostrRefs([]string{rawEvent.Content}), relays)
	linkPreviews := FetchLinkPreviews(ExtractPreviewableURLs(rawEvent.Content))
	kindDef := GetKindDefinition(rawEvent.Kind)

	reportedEventItem := HTMLEventItem{
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

	// Extract title/summary for long-form content
	if kindDef.ExtractTitle {
		reportedEventItem.Title = util.GetTagValue(rawEvent.Tags, "title")
	}
	if kindDef.ExtractSummary {
		reportedEventItem.Summary = util.GetTagValue(rawEvent.Tags, "summary")
	}

	// Get return URL from query param or referer
	returnURL := r.URL.Query().Get("return_url")
	if returnURL == "" {
		returnURL = r.Header.Get("Referer")
	}
	returnURL = sanitizeReturnURL(returnURL, loggedIn)

	// Generate CSRF token for forms (use session ID if logged in)
	var csrfToken string
	if session != nil && session.Connected {
		csrfToken = generateCSRFToken(session.ID)
	}

	// Get flash messages from cookies
	flash := getFlashMessages(w, r)

	data := struct {
		Title                  string
		PageDescription        string
		PageImage              string
		CanonicalURL           string
		ThemeClass             string
		ThemeLabel             string
		LoggedIn               bool
		ReportedEvent          HTMLEventItem
		ReturnURL              string
		Error                  string
		Success                string
		GeneratedAt            time.Time
		CSRFToken              string
		FeedModes              []FeedMode
		KindFilters            []KindFilter
		NavItems               []NavItem
		SettingsItems          []SettingsItem
		SettingsToggle         SettingsToggle
		ActiveRelays           []string
		ShowPostForm           bool
		HasUnreadNotifications bool
		CurrentURL             string
	}{
		Title:           cfgpkg.I18n("report.title"),
		PageDescription: "Report content",
		PageImage:       "",
		CanonicalURL:    r.URL.Path,
		ThemeClass:      themeClass,
		ThemeLabel:      themeLabel,
		LoggedIn:        loggedIn,
		ReportedEvent:   reportedEventItem,
		ReturnURL:       returnURL,
		Error:           flash.Error,
		Success:         flash.Success,
		GeneratedAt:     time.Now(),
		CSRFToken:       csrfToken,
		FeedModes: GetFeedModes(FeedModeContext{
			LoggedIn:    loggedIn,
			ActiveFeed:  "",
			CurrentPage: "report",
		}),
		KindFilters: GetKindFilters(KindFilterContext{
			LoggedIn:    loggedIn,
			ActiveFeed:  "",
			ActiveKinds: "",
		}),
		NavItems: GetNavItems(NavContext{
			LoggedIn:   loggedIn,
			ActivePage: "",
		}),
		SettingsItems: GetSettingsItems(SettingsContext{
			LoggedIn:      loggedIn,
			ThemeLabel:    themeLabel,
			UserAvatarURL: func() string {
				if session != nil {
					return getUserAvatarURL(hex.EncodeToString(session.UserPubKey))
				}
				return ""
			}(),
		}),
		SettingsToggle: GetSettingsToggle(SettingsContext{
			LoggedIn:      loggedIn,
			ThemeLabel:    themeLabel,
			UserAvatarURL: func() string {
				if session != nil {
					return getUserAvatarURL(hex.EncodeToString(session.UserPubKey))
				}
				return ""
			}(),
		}),
		ActiveRelays: relays,
		CurrentURL:   r.URL.String(),
	}

	// Determine if this is a HelmJS fragment request
	isFragment := isHelmRequest(r)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isFragment {
		if err := cachedReportFragment.ExecuteTemplate(w, tmplFragment, data); err != nil {
			slog.Error("report fragment template error", "error", err)
			util.RespondInternalError(w, "Internal server error")
		}
	} else {
		if err := cachedReportTemplate.ExecuteTemplate(w, tmplBase, data); err != nil {
			slog.Error("report template error", "error", err)
			util.RespondInternalError(w, "Internal server error")
		}
	}
}

// htmlReportSubmitHandler handles the POST submission of a report
func htmlReportSubmitHandler(w http.ResponseWriter, r *http.Request) {
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
	category := strings.TrimSpace(r.FormValue("category"))
	content := strings.TrimSpace(r.FormValue("content"))
	muteUser := r.FormValue("mute_user") == "1"
	returnURL := sanitizeReturnURL(strings.TrimSpace(r.FormValue("return_url")), true)

	// Validate event ID
	if !isValidEventID(eventID) {
		redirectWithError(w, r, returnURL, "Invalid event ID")
		return
	}

	// Validate category
	validCategories := map[string]bool{
		"spam": true, "impersonation": true, "illegal": true,
		"nudity": true, "malware": true, "profanity": true, "other": true,
	}
	if !validCategories[category] {
		redirectWithError(w, r, returnURL, "Invalid report category")
		return
	}

	// Validate pubkey format
	if eventPubkey != "" && len(eventPubkey) != 64 {
		redirectWithError(w, r, returnURL, "Invalid pubkey")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build report event (NIP-56 kind 1984)
	// Tags: p tag for reported user, e tag for reported event with category
	tags := [][]string{}

	// Add p tag for the reported user
	if eventPubkey != "" {
		tags = append(tags, []string{"p", eventPubkey, category})
	}

	// Add e tag for the reported event
	tags = append(tags, []string{"e", eventID, category})

	event := UnsignedEvent{
		Kind:      1984,
		Tags:      tags,
		Content:   content,
		CreatedAt: time.Now().Unix(),
	}

	// Sign the event
	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign report event", "error", err)
		redirectWithError(w, r, returnURL, "Failed to sign report")
		return
	}

	// Publish to user's write relays
	relays := cfgpkg.GetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}
	publishEvent(ctx, relays, signedEvent)

	slog.Info("report published",
		"event_id", eventID,
		"category", category,
		"reporter", hex.EncodeToString(session.UserPubKey)[:16],
	)

	// If mute checkbox was checked, also mute the user
	if muteUser && eventPubkey != "" {
		userPubkey := hex.EncodeToString(session.UserPubKey)

		// Don't allow muting yourself
		if eventPubkey != userPubkey {
			// Fetch user's current mute list (kind 10000)
			existingTags := [][]string{}
			muteEvents := fetchKind10000(relays, userPubkey)
			if len(muteEvents) > 0 {
				existingTags = muteEvents[0].Tags
			}

			// Check if already muted
			alreadyMuted := false
			for _, tag := range existingTags {
				if len(tag) >= 2 && tag[0] == "p" && tag[1] == eventPubkey {
					alreadyMuted = true
					break
				}
			}

			// Add to mute list if not already muted
			if !alreadyMuted {
				newTags := append(existingTags, []string{"p", eventPubkey})

				muteEvent := UnsignedEvent{
					Kind:      10000,
					Content:   "",
					Tags:      newTags,
					CreatedAt: time.Now().Unix(),
				}

				signedMuteEvent, err := session.SignEvent(ctx, muteEvent)
				if err != nil {
					slog.Error("failed to sign mute list after report", "error", err)
					// Continue anyway - report was successful
				} else {
					publishEvent(ctx, relays, signedMuteEvent)

					// Update session's mute cache
					session.mu.Lock()
					// Check if already muted to avoid duplicates
					alreadyMuted := false
					for _, pk := range session.MutedPubkeys {
						if pk == eventPubkey {
							alreadyMuted = true
							break
						}
					}
					if !alreadyMuted {
						session.MutedPubkeys = append(session.MutedPubkeys, eventPubkey)
					}
					session.mu.Unlock()

					slog.Info("muted user after report", "pubkey", shortID(eventPubkey))
				}
			}
		}
	}

	// Redirect back with success message
	redirectWithSuccess(w, r, returnURL, cfgpkg.I18n("report.success"))
}

// htmlQuoteHandler handles both displaying the quote form (GET) and submitting (POST)
func htmlQuoteHandler(w http.ResponseWriter, r *http.Request) {
	// Extract event ID from path: /quote/{identifier}
	// Accepts hex, note1, or nevent1 format
	identifier := strings.TrimPrefix(r.URL.Path, "/quote/")
	if identifier == "" {
		util.RespondBadRequest(w, "Invalid event ID")
		return
	}

	// Decode bech32 to hex if needed
	var eventID string
	switch {
	case strings.HasPrefix(identifier, "note1"):
		decoded, err := nips.DecodeNote(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid note ID")
			return
		}
		eventID = decoded
	case strings.HasPrefix(identifier, "nevent1"):
		decoded, err := nips.DecodeNEvent(identifier)
		if err != nil {
			util.RespondBadRequest(w, "Invalid nevent ID")
			return
		}
		eventID = decoded.EventID
	default:
		// Assume hex format
		if !isValidEventID(identifier) {
			util.RespondBadRequest(w, "Invalid event ID")
			return
		}
		eventID = identifier
	}

	themeClass, themeLabel := getThemeFromRequest(r)

	// Check login status
	session := getSessionFromRequest(r)
	loggedIn := session != nil && session.Connected

	if r.Method == http.MethodPost {
		// Handle quote submission
		if !loggedIn {
			redirectWithError(w, r, "/login", "Please login first")
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
		mentionsJSON := r.FormValue("mentions")

		content = util.AppendGifToContent(content, gifURL)

		// Process @mentions: replace @displayname with nostr:nprofile1...
		content, mentionedPubkeys := processMentions(content, mentionsJSON)

		if content == "" {
			redirectWithError(w, r, "/quote/"+identifier, "You cannot post an empty note!")
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

		// Add p-tags for mentioned users (enables notifications per NIP-27)
		for _, pubkey := range mentionedPubkeys {
			tags = append(tags, []string{"p", pubkey})
		}

		// Create unsigned event
		event := UnsignedEvent{
			Kind:      1,
			Content:   fullContent,
			Tags:      tags,
			CreatedAt: time.Now().Unix(),
		}

		addClientTag(&event)

		// Sign via bunker
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		signedEvent, err := session.SignEvent(ctx, event)
		if err != nil {
			slog.Error("failed to sign quote", "error", err)
			if isHelmRequest(r) {
				util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
				return
			}
			redirectWithError(w, r, "/quote/"+identifier, sanitizeErrorForUser("Sign event", err))
			return
		}

		// Publish to relays following outbox model (author write + tagged users' read)
		relays := getPublishRelaysForEvent(session, signedEvent)
		publishEvent(ctx, relays, signedEvent)

		redirectWithSuccess(w, r, DefaultTimelineURLLoggedIn(), "Quote published")
		return
	}

	// GET: Show quote form with preview of the quoted note
	relays := cfgpkg.GetDefaultRelays()

	// Fetch the event to be quoted
	events := fetchEventByID(relays, eventID)
	if len(events) == 0 {
		util.RespondNotFound(w, "Event not found")
		return
	}
	rawEvent := events[0]

	// Use outbox model: fetch author's relay list and refetch from their write relays if not found in content
	// (This helps resolve events that only exist on the author's relays)
	if rawEvent.Content == "" || len(rawEvent.Tags) == 0 {
		if outboxRelays := buildOutboxRelaysForPubkey(rawEvent.PubKey, 3); len(outboxRelays) > 0 {
			if outboxEvents := fetchEventByID(outboxRelays, eventID); len(outboxEvents) > 0 && outboxEvents[0].Content != "" {
				rawEvent = outboxEvents[0]
				slog.Debug("quote: found event via outbox", "author", shortID(rawEvent.PubKey))
			}
		}
	}

	// Fetch author profile (use outbox relays if available for better profile discovery)
	profileRelays := relays
	if authorRelayList := fetchRelayList(rawEvent.PubKey); authorRelayList != nil && len(authorRelayList.Write) > 0 {
		profileRelays = append(authorRelayList.Write, relays...)
	}
	profiles := fetchProfiles(profileRelays, []string{rawEvent.PubKey})
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
		PageDescription        string
		PageImage              string
		CanonicalURL           string
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
		PageDescription: "Quote and comment on a note",
		PageImage:       "",
		CanonicalURL:    r.URL.Path,
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

	// Determine if this is a HelmJS fragment request
	isFragment := isHelmRequest(r)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if isFragment {
		if err := cachedQuoteFragment.ExecuteTemplate(w, tmplFragment, data); err != nil {
			slog.Error("quote fragment template error", "error", err)
			util.RespondInternalError(w, "Internal server error")
		}
	} else {
		if err := cachedQuoteTemplate.ExecuteTemplate(w, tmplBase, data); err != nil {
			slog.Error("quote template error", "error", err)
			util.RespondInternalError(w, "Internal server error")
		}
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
		redirectWithError(w, r, "/login", "Please login first")
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
	defaultURL := DefaultTimelineURL()
	if loggedIn {
		defaultURL = DefaultTimelineURLLoggedIn()
	}

	if returnURL == "" {
		return defaultURL
	}

	// Must start with / and not // (which could be protocol-relative URL)
	if !strings.HasPrefix(returnURL, "/") || strings.HasPrefix(returnURL, "//") {
		return defaultURL
	}

	// Parse URL to ensure it's valid and has no host component
	parsed, err := url.Parse(returnURL)
	if err != nil {
		return defaultURL
	}

	// Reject if URL has scheme or host (shouldn't be possible after prefix check, but defense in depth)
	if parsed.Scheme != "" || parsed.Host != "" {
		return defaultURL
	}

	// Reject if path contains dangerous patterns
	if strings.Contains(parsed.Path, "..") || strings.Contains(parsed.Path, "\\") {
		return defaultURL
	}

	// Strip fragment to prevent javascript: URIs in anchors (defense in depth)
	parsed.Fragment = ""

	// Reconstruct the safe URL (path + query, no fragment)
	result := parsed.Path
	if parsed.RawQuery != "" {
		result += "?" + parsed.RawQuery
	}
	return result
}

// publishEvent publishes a signed event to relays
func publishEvent(_ context.Context, relays []string, event *Event) {
	// Create independent context for publishing - this ensures publish operations
	// complete even after the HTTP handler returns and cancels its context.
	// Each relay gets 15 seconds to accept the event.
	publishCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

	var wg sync.WaitGroup
	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			if err := publishToRelay(publishCtx, relayURL, event); err != nil {
				slog.Warn("failed to publish", "relay", relayURL, "error", err)
			}
		}(relay)
	}

	// Wait for at least some relays to respond (500ms), then continue
	// The goroutines will complete in the background
	go func() {
		wg.Wait()
		cancel()
	}()

	time.Sleep(500 * time.Millisecond)
}

// PublishFailureCallback is called when an async publish fails on all relays
type PublishFailureCallback func(eventID string, err error)

// publishEventAsync publishes a signed event to relays without blocking.
// Returns immediately. If publish fails on all relays, calls onFailure.
// Used for optimistic UI where the response is sent before relay confirmation.
func publishEventAsync(relays []string, event *Event, onFailure PublishFailureCallback) {
	go func() {
		publishCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var successCount int
		var lastErr error
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, relay := range relays {
			wg.Add(1)
			go func(relayURL string) {
				defer wg.Done()
				if err := publishToRelay(publishCtx, relayURL, event); err != nil {
					mu.Lock()
					lastErr = err
					mu.Unlock()
					slog.Warn("async publish failed", "relay", relayURL, "error", err)
				} else {
					mu.Lock()
					successCount++
					mu.Unlock()
					slog.Debug("async publish succeeded", "relay", relayURL, "event", event.ID[:8])
				}
			}(relay)
		}

		wg.Wait()

		if successCount == 0 && onFailure != nil {
			slog.Error("async publish failed on all relays", "event", event.ID[:8], "relays", len(relays))
			onFailure(event.ID, lastErr)
		}
	}()
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

// getPublishRelaysForEvent returns relays for publishing an event following the outbox model:
// - Author's write relays (where the author publishes)
// - Tagged users' read relays (so they see notifications)
// - Fallback to default publish relays if author has no relay list
func getPublishRelaysForEvent(session *BunkerSession, event *Event) []string {
	relaySet := make(map[string]bool)

	// 1. Author's write relays (primary destination)
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		for _, r := range session.UserRelayList.Write {
			relaySet[r] = true
		}
	} else {
		// Fallback to default publish relays
		for _, r := range cfgpkg.GetPublishRelays() {
			relaySet[r] = true
		}
	}

	// 2. Extract tagged pubkeys from p-tags
	var taggedPubkeys []string
	for _, tag := range event.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			pk := tag[1]
			// Validate it looks like a hex pubkey
			if len(pk) == 64 {
				taggedPubkeys = append(taggedPubkeys, pk)
			}
		}
	}

	// 3. Fetch relay lists for tagged users and collect their READ relays
	if len(taggedPubkeys) > 0 {
		// Limit to first 10 tagged users to avoid excessive relay fetching
		if len(taggedPubkeys) > 10 {
			taggedPubkeys = taggedPubkeys[:10]
		}

		relayLists := fetchRelayLists(taggedPubkeys)
		for _, pk := range taggedPubkeys {
			if rl, ok := relayLists[pk]; ok && rl != nil && len(rl.Read) > 0 {
				// Add up to 2 read relays per tagged user
				added := 0
				for _, r := range rl.Read {
					if !relaySet[r] && added < 2 {
						relaySet[r] = true
						added++
					}
				}
			}
		}
	}

	// Convert to slice
	relays := make([]string, 0, len(relaySet))
	for r := range relaySet {
		relays = append(relays, r)
	}

	authorRelayCount := 0
	if session.UserRelayList != nil {
		authorRelayCount = len(session.UserRelayList.Write)
	}
	slog.Debug("publish relays for event",
		"author_relays", authorRelayCount,
		"tagged_users", len(taggedPubkeys),
		"total_relays", len(relays))

	return relays
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
      <a href="/timeline?feed=global&amp;kinds=1&amp;limit=20">Timeline</a>
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
        <a href="{{buildURL "/check-connection" "secret" .Secret}}" class="submit-btn submit-btn-block">
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

      <form class="login-form login-section" method="POST" action="/login">
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

      <form class="login-form login-section" method="POST" action="/reconnect">
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
	relays := cfgpkg.GetPublishRelays()
	if session.UserRelayList != nil && len(session.UserRelayList.Write) > 0 {
		relays = session.UserRelayList.Write
	}

	// Fetch user's current contact list (kind 3)
	existingTags := [][]string{}
	existingContent := "" // Preserve content (some clients store relay hints as JSON)
	contactEvents := fetchKind3(relays, userPubkey)
	if len(contactEvents) > 0 {
		existingTags = contactEvents[0].Tags
		existingContent = contactEvents[0].Content
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
		Content:   existingContent, // Preserve original content (relay hints, etc.)
		Tags:      newTags,
		CreatedAt: time.Now().Unix(),
	}

	// Sign via bunker
	signedEvent, err := session.SignEvent(ctx, event)
	if err != nil {
		slog.Error("failed to sign contact list", "error", err)
		if isHelmRequest(r) {
			util.RespondInternalError(w, sanitizeErrorForUser("Sign event", err))
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
			util.RespondInternalError(w, "Failed to render response")
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

// htmlProfileEditHandler handles GET and POST for /profile/edit
func htmlProfileEditHandler(w http.ResponseWriter, r *http.Request) {
	// Get session
	session := getSessionFromRequest(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
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
			KindFilters: nil, // Hide kind submenu in edit mode
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
			util.RespondInternalError(w, "Internal server error")
		}
		return
	}

	// POST - save profile
	if r.Method != http.MethodPost {
		util.RespondMethodNotAllowed(w, "Method not allowed")
		return
	}

	// Validate CSRF
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "/profile/edit", "Invalid form data")
		return
	}

	csrfToken := r.FormValue("csrf_token")
	if !validateCSRFToken(session.ID, csrfToken) {
		redirectWithError(w, r, "/profile/edit", "Invalid session, please try again")
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
		redirectWithError(w, r, "/profile/edit", "Invalid picture URL")
		return
	}
	if banner != "" && !isValidURL(banner) {
		redirectWithError(w, r, "/profile/edit", "Invalid banner URL")
		return
	}
	if website != "" && !isValidURL(website) {
		redirectWithError(w, r, "/profile/edit", "Invalid website URL")
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
		redirectWithError(w, r, "/profile/edit", "Failed to encode profile")
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
		redirectWithError(w, r, "/profile/edit", sanitizeErrorForUser("Sign profile", err))
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

	redirectWithSuccess(w, r, "/profile/"+userPubKeyHex, "Profile updated")
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
		http.Redirect(w, r, "/wallet", http.StatusSeeOther)
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
		redirectWithError(w, r, "/wallet", cfgpkg.I18n("wallet.error_empty_uri"))
		return
	}

	config, err := ParseNWCURI(nwcURI)
	if err != nil {
		slog.Warn("invalid NWC URI", "error", err)
		redirectWithError(w, r, "/wallet", cfgpkg.I18n("wallet.error_invalid_uri"))
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

	// Redirect to return URL or wallet page (sanitize to prevent open redirect)
	returnURL := sanitizeReturnURL(r.FormValue("return_url"), true)
	if returnURL == DefaultTimelineURLLoggedIn() {
		returnURL = "/wallet" // Default to wallet page, not timeline
	}
	redirectWithSuccess(w, r, returnURL, cfgpkg.I18n("wallet.connected_success"))
}

// htmlWalletDisconnectHandler handles wallet disconnection
func htmlWalletDisconnectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/wallet", http.StatusSeeOther)
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

	// Redirect to return URL or wallet page (sanitize to prevent open redirect)
	returnURL := sanitizeReturnURL(r.FormValue("return_url"), true)
	if returnURL == DefaultTimelineURLLoggedIn() {
		returnURL = "/wallet" // Default to wallet page, not timeline
	}
	redirectWithSuccess(w, r, returnURL, cfgpkg.I18n("wallet.disconnected_success"))
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
		w.Write([]byte(`<div class="wallet-info-error">` + cfgpkg.I18n("wallet.not_connected") + `</div>`))
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
			util.RespondInternalError(w, "Failed to render wallet info")
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
			util.RespondInternalError(w, "Failed to render wallet info")
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
		w.Write([]byte(`<div class="wallet-info-error">` + cfgpkg.I18n("wallet.connection_failed") + `</div>`))
		return
	}

	// Cache the result
	SetCachedWalletInfo(userPubkeyHex, cached)

	data := cachedToHTMLWalletInfo(cached)
	html, err := renderWalletInfoFragment(data)
	if err != nil {
		slog.Error("wallet info: failed to render", "error", err)
		util.RespondInternalError(w, "Failed to render wallet info")
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
		result.Error = cfgpkg.I18n("wallet.balance_failed")
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
			relays := cfgpkg.GetProfileRelays()
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
		readRelays := cfgpkg.GetDefaultRelays()
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
		walletURL := util.BuildURL("/wallet", map[string]string{"return": returnURL})
		if isHelmRequest(r) {
			// For HelmJS: show error with link, keep footer intact
			errorMsg := cfgpkg.I18n("wallet.connect_to_zap") + ` <a href="` + template.HTMLEscapeString(walletURL) + `">` + cfgpkg.I18n("wallet.connect") + `</a>`
			respondWithErrorAndFragment(w, r, returnURL, errorMsg, renderFooterForError())
		} else {
			// For regular requests: redirect to wallet page
			setFlashError(w, r, cfgpkg.I18n("wallet.connect_to_zap"))
			http.Redirect(w, r, walletURL, http.StatusSeeOther)
		}
		return
	}
	slog.Info("zap: wallet OK")

	// Fetch recipient's profile to get their lightning address
	slog.Info("zap: fetching profile", "pubkey", eventPubkey[:16])
	relays := cfgpkg.GetProfileRelays()
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

	// Resolve LNURL to get payment info (check cache first)
	slog.Info("zap: resolving LNURL", "lud16", profile.Lud16)
	lnurlInfo, cached := GetCachedLNURLPayInfo(eventPubkey)
	if !cached || lnurlInfo == nil {
		// Not cached or was a "not found" entry - resolve fresh
		var err error
		lnurlInfo, err = ResolveLNURLFromProfile(profile)
		if err != nil {
			slog.Error("zap: failed to resolve LNURL", "error", err, "pubkey", eventPubkey[:8])
			respondWithErrorAndFragment(w, r, returnURL, "Could not resolve recipient's Lightning address", renderFooterForError())
			return
		}
		// Cache for future requests
		if lnurlCacheStore != nil {
			lnurlCacheStore.Set(eventPubkey, lnurlInfo)
		}
		slog.Info("zap: LNURL resolved (fresh)", "allowsNostr", lnurlInfo.AllowsNostr, "nostrPubkey", lnurlInfo.NostrPubkey != "")
	} else {
		slog.Info("zap: LNURL cache hit", "allowsNostr", lnurlInfo.AllowsNostr, "nostrPubkey", lnurlInfo.NostrPubkey != "")
	}

	// Parse amount from form (in sats), default to 21 sats
	amountStr := strings.TrimSpace(r.FormValue("amount"))
	amountSats := int64(21) // Default 21 sats
	if amountStr != "" {
		if parsed, err := strconv.ParseInt(amountStr, 10, 64); err == nil && parsed > 0 {
			amountSats = parsed
		}
	}
	amountMsats := amountSats * 1000 // Convert sats to millisats
	slog.Info("zap: amount", "sats", amountSats, "msats", amountMsats)

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
	writeRelays := cfgpkg.GetPublishRelays()
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
		readRelays := cfgpkg.GetDefaultRelays()
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
			util.RespondInternalError(w, "Failed to render response")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(html))
		return
	}

	// Redirect with success
	redirectWithSuccess(w, r, returnURL, "Zapped 21 sats ⚡")
}

// processMentions parses the mentions JSON mapping and replaces @displayname with nostr:nprofile1...
// Returns the modified content and a slice of mentioned pubkeys for p-tags.
// If mentions JSON is empty or invalid, returns original content unchanged.
func processMentions(content string, mentionsJSON string) (string, []string) {
	if mentionsJSON == "" || mentionsJSON == "{}" {
		return content, nil
	}

	// Parse JSON: {"displayname": "pubkeyhex", ...}
	var mentions map[string]string
	if err := json.Unmarshal([]byte(mentionsJSON), &mentions); err != nil {
		slog.Warn("failed to parse mentions JSON", "error", err, "json", mentionsJSON)
		return content, nil
	}

	if len(mentions) == 0 {
		return content, nil
	}

	var mentionedPubkeys []string
	modifiedContent := content

	for displayName, pubkeyHex := range mentions {
		if displayName == "" || pubkeyHex == "" {
			continue
		}

		// Encode as nprofile with relay hints (use publish relays for discoverability)
		relayHints := cfgpkg.GetPublishRelays()
		if len(relayHints) > 2 {
			relayHints = relayHints[:2] // Limit to 2 relay hints
		}

		nprofile, err := nips.EncodeNProfile(pubkeyHex, relayHints)
		if err != nil {
			slog.Warn("failed to encode nprofile for mention", "error", err, "pubkey", pubkeyHex)
			continue
		}

		// Replace @displayname with nostr:nprofile1...
		// Use case-insensitive replacement since display names may vary
		searchPattern := "@" + displayName
		replacement := "nostr:" + nprofile
		modifiedContent = strings.Replace(modifiedContent, searchPattern, replacement, -1)

		// Track pubkey for p-tag
		mentionedPubkeys = append(mentionedPubkeys, pubkeyHex)
	}

	return modifiedContent, mentionedPubkeys
}
