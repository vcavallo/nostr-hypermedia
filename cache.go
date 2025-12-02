package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
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

// EventCache provides in-memory caching for relay queries
type EventCache struct {
	mu      sync.RWMutex
	entries map[string]*EventCacheEntry
	maxSize int
}

// EventCacheEntry holds cached events with expiration
type EventCacheEntry struct {
	Events    []Event
	EOSE      bool
	ExpiresAt time.Time
}

// Global event cache - max 500 cached queries
var eventCache = NewEventCache(500)

// NewEventCache creates a new cache with the given max size
func NewEventCache(maxSize int) *EventCache {
	cache := &EventCache{
		entries: make(map[string]*EventCacheEntry),
		maxSize: maxSize,
	}
	// Start background cleanup
	go cache.cleanupLoop()
	return cache
}

// buildCacheKey creates a deterministic key from query parameters
func buildEventCacheKey(relays []string, filter Filter) string {
	// Sort relays for consistent keys
	sortedRelays := make([]string, len(relays))
	copy(sortedRelays, relays)
	sort.Strings(sortedRelays)

	// Sort authors for consistent keys
	sortedAuthors := make([]string, len(filter.Authors))
	copy(sortedAuthors, filter.Authors)
	sort.Strings(sortedAuthors)

	// Sort kinds for consistent keys
	sortedKinds := make([]int, len(filter.Kinds))
	copy(sortedKinds, filter.Kinds)
	sort.Ints(sortedKinds)

	// Build key string
	var sb strings.Builder
	sb.WriteString("relays:")
	sb.WriteString(strings.Join(sortedRelays, ","))
	sb.WriteString("|authors:")
	sb.WriteString(strings.Join(sortedAuthors, ","))
	sb.WriteString("|kinds:")
	for i, k := range sortedKinds {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%d", k))
	}
	sb.WriteString(fmt.Sprintf("|limit:%d", filter.Limit))
	if filter.Until != nil {
		sb.WriteString(fmt.Sprintf("|until:%d", *filter.Until))
	}

	// Hash the key to keep it short
	hash := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(hash[:16])
}

// getEventTTL returns appropriate TTL based on query type
func getEventTTL(filter Filter) time.Duration {
	if len(filter.Authors) == 0 {
		// Global timeline - cache longer, high hit rate
		return 30 * time.Second
	}
	if len(filter.Authors) <= 5 {
		// Small author list (maybe a profile page)
		return 20 * time.Second
	}
	// Large author list (follow list) - shorter cache
	return 15 * time.Second
}

// Get retrieves cached events if available and not expired
func (c *EventCache) Get(relays []string, filter Filter) ([]Event, bool, bool) {
	key := buildEventCacheKey(relays, filter)

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || time.Now().After(entry.ExpiresAt) {
		return nil, false, false
	}

	// Return a copy to avoid race conditions
	events := make([]Event, len(entry.Events))
	copy(events, entry.Events)
	return events, entry.EOSE, true
}

// Set stores events in the cache
func (c *EventCache) Set(relays []string, filter Filter, events []Event, eose bool) {
	key := buildEventCacheKey(relays, filter)
	ttl := getEventTTL(filter)

	// Make a copy of events to store
	eventsCopy := make([]Event, len(events))
	copy(eventsCopy, events)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Simple eviction if at max size: remove oldest entries
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	c.entries[key] = &EventCacheEntry{
		Events:    eventsCopy,
		EOSE:      eose,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// evictOldest removes the oldest 10% of entries (must hold write lock)
func (c *EventCache) evictOldest() {
	toRemove := c.maxSize / 10
	if toRemove < 1 {
		toRemove = 1
	}

	type keyExpiry struct {
		key     string
		expires time.Time
	}

	entries := make([]keyExpiry, 0, len(c.entries))
	for k, v := range c.entries {
		entries = append(entries, keyExpiry{k, v.ExpiresAt})
	}

	// Sort by expiration time (oldest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].expires.Before(entries[j].expires)
	})

	// Remove oldest entries
	for i := 0; i < toRemove && i < len(entries); i++ {
		delete(c.entries, entries[i].key)
	}
}

// cleanupLoop periodically removes expired entries
func (c *EventCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes all expired entries
func (c *EventCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}

// ContactCache stores contact lists with short TTL
type ContactCache struct {
	contacts sync.Map
	ttl      time.Duration
}

type cachedContacts struct {
	pubkeys   []string
	fetchedAt time.Time
}

// Global contact cache - 2 minute TTL (contacts change often)
var contactCache = &ContactCache{
	ttl: 2 * time.Minute,
}

// Get retrieves contacts from cache if not expired
func (c *ContactCache) Get(pubkey string) ([]string, bool) {
	val, ok := c.contacts.Load(pubkey)
	if !ok {
		return nil, false
	}

	cached := val.(*cachedContacts)
	if time.Since(cached.fetchedAt) > c.ttl {
		c.contacts.Delete(pubkey)
		return nil, false
	}

	return cached.pubkeys, true
}

// Set stores contacts in the cache
func (c *ContactCache) Set(pubkey string, contacts []string) {
	c.contacts.Store(pubkey, &cachedContacts{
		pubkeys:   contacts,
		fetchedAt: time.Now(),
	})
}

// LinkPreview holds Open Graph metadata for a URL
type LinkPreview struct {
	URL         string
	Title       string
	Description string
	Image       string
	SiteName    string
	FetchedAt   time.Time
	Failed      bool // true if we tried but couldn't get OG tags
}

// LinkPreviewCache stores link previews with long TTL
type LinkPreviewCache struct {
	previews sync.Map
	ttl      time.Duration
	failTTL  time.Duration // shorter TTL for failed fetches
}

type cachedLinkPreview struct {
	preview   *LinkPreview
	fetchedAt time.Time
}

// Global link preview cache - 24 hour TTL for success, 1 hour for failures
var linkPreviewCache = &LinkPreviewCache{
	ttl:     24 * time.Hour,
	failTTL: 1 * time.Hour,
}

// Get retrieves a link preview from cache if not expired
func (c *LinkPreviewCache) Get(url string) (*LinkPreview, bool) {
	val, ok := c.previews.Load(url)
	if !ok {
		return nil, false
	}

	cached := val.(*cachedLinkPreview)
	ttl := c.ttl
	if cached.preview.Failed {
		ttl = c.failTTL
	}
	if time.Since(cached.fetchedAt) > ttl {
		c.previews.Delete(url)
		return nil, false
	}

	return cached.preview, true
}

// Set stores a link preview in the cache
func (c *LinkPreviewCache) Set(url string, preview *LinkPreview) {
	c.previews.Store(url, &cachedLinkPreview{
		preview:   preview,
		fetchedAt: time.Now(),
	})
}

// GetMultiple retrieves multiple previews, returning found ones and missing URLs
func (c *LinkPreviewCache) GetMultiple(urls []string) (found map[string]*LinkPreview, missing []string) {
	found = make(map[string]*LinkPreview)
	now := time.Now()

	for _, url := range urls {
		val, ok := c.previews.Load(url)
		if !ok {
			missing = append(missing, url)
			continue
		}

		cached := val.(*cachedLinkPreview)
		ttl := c.ttl
		if cached.preview.Failed {
			ttl = c.failTTL
		}
		if now.Sub(cached.fetchedAt) > ttl {
			c.previews.Delete(url)
			missing = append(missing, url)
			continue
		}

		found[url] = cached.preview
	}

	return found, missing
}
