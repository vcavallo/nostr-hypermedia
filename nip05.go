package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nostr-server/internal/util"
)

// NIP-05 verification with caching
// Verifies nip05 identifiers (user@domain.com) against .well-known/nostr.json

var nip05HTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

// NIP05Result contains the verification result for a nip05 identifier
type NIP05Result struct {
	Verified  bool
	Pubkey    string   // The verified pubkey (hex)
	Domain    string   // Display domain
	Relays    []string // Relay hints
	CheckedAt time.Time
}

// nip05CacheTTL is the TTL for NIP-05 cache entries
var nip05CacheTTL = 24 * time.Hour

// GetCachedNIP05 checks if we have a valid cached NIP-05 verification for this identifier/pubkey.
// Returns the cached result if valid, nil if not cached or expired.
// Use this to avoid triggering async verification for already-verified profiles.
func GetCachedNIP05(nip05 string, pubkey string) *NIP05Result {
	if nip05 == "" || pubkey == "" {
		return nil
	}

	cached, ok := nip05CacheStore.Get(nip05)
	if ok && cached.Verified && cached.Pubkey == pubkey {
		return cached
	}
	return nil
}

// VerifyNIP05 verifies a nip05 identifier for a given pubkey
// Returns the verification result (from cache if available)
func VerifyNIP05(nip05 string, pubkey string) *NIP05Result {
	if nip05 == "" || pubkey == "" {
		return nil
	}

	// Check cache first
	if cached, ok := nip05CacheStore.Get(nip05); ok {
		// Cache hit - verify pubkey matches
		if cached.Verified && cached.Pubkey == pubkey {
			return cached
		}
		// Cached but pubkey doesn't match (someone else verified this identifier)
		if cached.Verified && cached.Pubkey != pubkey {
			return &NIP05Result{Verified: false}
		}
		return cached
	}

	// Cache miss or expired - fetch and verify
	result := fetchAndVerifyNIP05(nip05, pubkey)

	// Store in cache
	nip05CacheStore.Set(nip05, result)

	return result
}

// VerifyNIP05Async verifies a nip05 identifier asynchronously and updates the profile cache
func VerifyNIP05Async(nip05 string, pubkey string) {
	go func() {
		result := VerifyNIP05(nip05, pubkey)
		if result != nil && result.Verified {
			// Update the profile in cache with verification result
			updateProfileWithNIP05(pubkey, result)
		}
	}()
}

// updateProfileWithNIP05 updates a cached profile with NIP-05 verification data
func updateProfileWithNIP05(pubkey string, result *NIP05Result) {
	profile := getCachedProfile(pubkey)
	if profile == nil {
		return
	}

	profile.NIP05Verified = result.Verified
	profile.NIP05Domain = result.Domain
	profile.NIP05Relays = result.Relays

	// Update in cache
	profileCache.SetMultiple(map[string]*ProfileInfo{pubkey: profile})

	slog.Debug("updated profile with NIP-05 verification",
		"pubkey", shortID(pubkey),
		"domain", result.Domain,
		"relays", len(result.Relays))
}

// fetchAndVerifyNIP05 fetches the .well-known/nostr.json and verifies the pubkey
func fetchAndVerifyNIP05(nip05 string, pubkey string) *NIP05Result {
	result := &NIP05Result{
		Verified:  false,
		CheckedAt: time.Now(),
	}

	// Parse nip05: name@domain
	parts := strings.SplitN(nip05, "@", 2)
	if len(parts) != 2 {
		slog.Debug("invalid nip05 format", "nip05", nip05)
		return result
	}

	name := strings.ToLower(parts[0])
	domain := strings.ToLower(parts[1])

	// Validate domain
	if domain == "" || strings.Contains(domain, "/") || strings.Contains(domain, "\\") {
		slog.Debug("invalid nip05 domain", "domain", domain)
		return result
	}

	// Block internal/private hosts
	if util.IsPrivateHost(domain) {
		slog.Debug("nip05 domain is private/internal", "domain", domain)
		return result
	}

	// Set display domain (for "_@domain", show just "domain")
	if name == "_" {
		result.Domain = domain
	} else {
		result.Domain = nip05
	}

	// Build URL
	url := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, name)

	// Fetch
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		slog.Debug("failed to create nip05 request", "url", url, "error", err)
		return result
	}
	req.Header.Set("Accept", "application/json")

	resp, err := nip05HTTPClient.Do(req)
	if err != nil {
		slog.Debug("nip05 fetch failed", "url", url, "error", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("nip05 fetch returned non-200", "url", url, "status", resp.StatusCode)
		return result
	}

	// Parse response
	var data struct {
		Names  map[string]string   `json:"names"`
		Relays map[string][]string `json:"relays"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Debug("failed to parse nip05 response", "url", url, "error", err)
		return result
	}

	// Verify pubkey matches
	verifiedPubkey, ok := data.Names[name]
	if !ok {
		slog.Debug("nip05 name not found in response", "name", name, "url", url)
		return result
	}

	// Normalize pubkey comparison (lowercase)
	verifiedPubkey = strings.ToLower(verifiedPubkey)
	if verifiedPubkey != strings.ToLower(pubkey) {
		slog.Debug("nip05 pubkey mismatch",
			"expected", shortID(pubkey),
			"got", shortID(verifiedPubkey))
		return result
	}

	// Success!
	result.Verified = true
	result.Pubkey = verifiedPubkey

	// Extract relay hints if available
	if relays, ok := data.Relays[verifiedPubkey]; ok {
		for _, relay := range relays {
			if normalizedRelay := normalizeRelayURL(relay); normalizedRelay != "" {
				result.Relays = append(result.Relays, normalizedRelay)
			}
		}
	}

	slog.Debug("nip05 verified",
		"nip05", nip05,
		"pubkey", shortID(pubkey),
		"relays", len(result.Relays))

	return result
}

// GetNIP05VerificationURL returns the .well-known URL for a nip05 identifier
func GetNIP05VerificationURL(nip05 string) string {
	parts := strings.SplitN(nip05, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	name := strings.ToLower(parts[0])
	domain := strings.ToLower(parts[1])
	return fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, name)
}
