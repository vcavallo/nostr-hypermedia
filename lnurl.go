package main

import (
	"errors"
	"log/slog"

	"nostr-server/internal/services"
)

// LNURL-pay handling for Lightning payments

// Type aliases for backward compatibility
type LNURLPayInfo = services.LNURLPayInfo
type LNURLPayResponse = services.LNURLPayResponse
type LNURLError = services.LNURLError

// Function aliases
var (
	ResolveLud16     = services.ResolveLud16
	ResolveLud06     = services.ResolveLud06
	RequestInvoice   = services.RequestInvoice
	MsatsToSats      = services.MsatsToSats
)

// ResolveLNURLFromProfile extracts and resolves LNURL info from a profile's lud16/lud06
// Returns nil if no Lightning address is configured
func ResolveLNURLFromProfile(profile *ProfileInfo) (*LNURLPayInfo, error) {
	if profile == nil {
		return nil, errors.New("profile is nil")
	}

	// Try lud16 first (more common)
	if profile.Lud16 != "" {
		return ResolveLud16(profile.Lud16)
	}

	// Fall back to lud06
	if profile.Lud06 != "" {
		return ResolveLud06(profile.Lud06)
	}

	return nil, errors.New("no Lightning address configured")
}

// CanReceiveZaps checks if the profile can receive zaps (has lud16 or lud06)
func CanReceiveZaps(profile *ProfileInfo) bool {
	if profile == nil {
		return false
	}
	return profile.Lud16 != "" || profile.Lud06 != ""
}

// GetCachedLNURLPayInfo returns cached LNURL pay info for a pubkey, or nil if not cached
func GetCachedLNURLPayInfo(pubkey string) (*LNURLPayInfo, bool) {
	if lnurlCacheStore == nil {
		return nil, false
	}
	return lnurlCacheStore.Get(pubkey)
}

// PrefetchLNURLForProfiles pre-fetches and caches LNURL pay info for visible authors.
// This runs in background goroutines and doesn't block the caller.
// Only profiles with lud16/lud06 that aren't already cached are fetched.
func PrefetchLNURLForProfiles(profiles map[string]*ProfileInfo) {
	if lnurlCacheStore == nil || len(profiles) == 0 {
		return
	}

	// Filter to profiles with lightning addresses that aren't cached
	var toFetch []struct {
		pubkey  string
		profile *ProfileInfo
	}

	for pubkey, profile := range profiles {
		if profile == nil || !CanReceiveZaps(profile) {
			continue
		}

		// Skip if already cached
		if _, cached := lnurlCacheStore.Get(pubkey); cached {
			continue
		}

		toFetch = append(toFetch, struct {
			pubkey  string
			profile *ProfileInfo
		}{pubkey, profile})
	}

	if len(toFetch) == 0 {
		return
	}

	slog.Debug("prefetching LNURL for authors", "count", len(toFetch))

	// Fetch in background goroutines (limit concurrency to avoid hammering external services)
	semaphore := make(chan struct{}, 5) // Max 5 concurrent LNURL fetches

	for _, item := range toFetch {
		semaphore <- struct{}{} // Acquire
		go func(pubkey string, profile *ProfileInfo) {
			defer func() { <-semaphore }() // Release

			info, err := ResolveLNURLFromProfile(profile)
			if err != nil {
				slog.Debug("LNURL prefetch failed", "pubkey", shortID(pubkey), "error", err)
				lnurlCacheStore.SetNotFound(pubkey)
				return
			}

			lnurlCacheStore.Set(pubkey, info)
			slog.Debug("LNURL prefetch success", "pubkey", shortID(pubkey), "allowsNostr", info.AllowsNostr)
		}(item.pubkey, item.profile)
	}
}
