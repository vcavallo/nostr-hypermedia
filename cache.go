package main

import (
	"sync"
	"time"
)

// ProfileCache stores profile information with TTL
type ProfileCache struct {
	profiles sync.Map
	ttl      time.Duration
}

type cachedProfile struct {
	profile   *ProfileInfo
	fetchedAt time.Time
}

// Global profile cache - 10 minute TTL
var profileCache = &ProfileCache{
	ttl: 10 * time.Minute,
}

// Get retrieves a profile from cache if it exists and isn't expired
func (c *ProfileCache) Get(pubkey string) (*ProfileInfo, bool) {
	val, ok := c.profiles.Load(pubkey)
	if !ok {
		return nil, false
	}

	cached := val.(*cachedProfile)
	if time.Since(cached.fetchedAt) > c.ttl {
		// Expired, remove from cache
		c.profiles.Delete(pubkey)
		return nil, false
	}

	return cached.profile, true
}

// Set stores a profile in the cache
func (c *ProfileCache) Set(pubkey string, profile *ProfileInfo) {
	c.profiles.Store(pubkey, &cachedProfile{
		profile:   profile,
		fetchedAt: time.Now(),
	})
}

// SetMultiple stores multiple profiles at once
func (c *ProfileCache) SetMultiple(profiles map[string]*ProfileInfo) {
	now := time.Now()
	for pubkey, profile := range profiles {
		c.profiles.Store(pubkey, &cachedProfile{
			profile:   profile,
			fetchedAt: now,
		})
	}
}

// GetMultiple retrieves multiple profiles, returning found ones and list of missing pubkeys
func (c *ProfileCache) GetMultiple(pubkeys []string) (found map[string]*ProfileInfo, missing []string) {
	found = make(map[string]*ProfileInfo)
	now := time.Now()

	for _, pubkey := range pubkeys {
		val, ok := c.profiles.Load(pubkey)
		if !ok {
			missing = append(missing, pubkey)
			continue
		}

		cached := val.(*cachedProfile)
		if now.Sub(cached.fetchedAt) > c.ttl {
			c.profiles.Delete(pubkey)
			missing = append(missing, pubkey)
			continue
		}

		found[pubkey] = cached.profile
	}

	return found, missing
}
