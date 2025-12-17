package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Global cache instances - these maintain backward compatibility with existing code
var (
	profileCache     *ProfileCacheWrapper
	contactCache     *ContactCacheWrapper
	relayListCache   *RelayListCacheWrapper
	avatarCache      *AvatarCacheWrapper
	linkPreviewCache *LinkPreviewCacheWrapper

	// Session and pending connection stores
	sessionStore     SessionStore
	pendingConnStore PendingConnStore

	// Rate limiting and search caching
	rateLimitStore         RateLimitStore
	searchCacheStore       SearchCacheStore
	threadCacheStore       ThreadCacheStore
	notificationReadStore  NotificationReadStore
	notificationCacheStore NotificationCacheStore

	// Cache backend (memory or redis)
	cacheBackend CacheBackend

	// Cache configuration
	cacheConfig CacheConfig

	// Cache backend type for health reporting
	cacheBackendType string // "redis" or "memory"
)

// InitCaches initializes all caches with Redis if REDIS_URL is set, otherwise memory
func InitCaches() error {
	cacheConfig = DefaultCacheConfig()
	redisURL := os.Getenv("REDIS_URL")

	if redisURL != "" {
		slog.Info("initializing Redis cache")
		redisCache, err := NewRedisCache(redisURL, "nostr:")
		if err != nil {
			slog.Warn("Redis connection failed, using memory cache", "error", err)
			return initMemoryCaches()
		}

		cacheBackend = redisCache
		cacheBackendType = "redis"

		// Initialize Redis session/pending stores using same client
		redisClient := redisCache.GetClient()
		sessionStore = NewRedisSessionStore(redisClient, "nostr:", cacheConfig.SessionTTL)
		pendingConnStore = NewRedisPendingConnStore(redisClient, "nostr:", cacheConfig.PendingConnTTL)
		rateLimitStore = NewRedisRateLimitStore(redisClient, "nostr:")
		searchCacheStore = NewRedisSearchCacheStore(redisClient, "nostr:")
		threadCacheStore = NewRedisThreadCacheStore(redisClient, "nostr:")
		notificationReadStore = NewRedisNotificationReadStore(redisClient, "nostr:", cacheConfig.NotificationReadTTL)
		notificationCacheStore = NewRedisNotificationCacheStore(redisClient, "nostr:")

		slog.Info("Redis cache initialized")
	} else {
		if err := initMemoryCaches(); err != nil {
			return err
		}
	}

	// Initialize typed wrappers
	profileCache = NewProfileCacheWrapper(cacheBackend, cacheConfig)
	contactCache = NewContactCacheWrapper(cacheBackend, cacheConfig)
	relayListCache = NewRelayListCacheWrapper(cacheBackend, cacheConfig)
	avatarCache = NewAvatarCacheWrapper(cacheBackend, cacheConfig)
	linkPreviewCache = NewLinkPreviewCacheWrapper(cacheBackend, cacheConfig)

	return nil
}

func initMemoryCaches() error {
	slog.Info("initializing in-memory cache")

	cacheBackend = NewMemoryCache(10000, 2*time.Minute)
	cacheBackendType = "memory"
	sessionStore = NewMemorySessionStore(cacheConfig.SessionTTL)
	pendingConnStore = NewMemoryPendingConnStore(cacheConfig.PendingConnTTL)
	rateLimitStore = NewMemoryRateLimitStore()
	searchCacheStore = NewMemorySearchCacheStore()
	threadCacheStore = NewMemoryThreadCacheStore()
	notificationReadStore = NewMemoryNotificationReadStore(cacheConfig.NotificationReadTTL)
	notificationCacheStore = NewMemoryNotificationCacheStore()

	return nil
}

// ProfileCacheWrapper provides typed access to profile cache
type ProfileCacheWrapper struct {
	backend CacheBackend
	config  CacheConfig
}

func NewProfileCacheWrapper(backend CacheBackend, config CacheConfig) *ProfileCacheWrapper {
	return &ProfileCacheWrapper{backend: backend, config: config}
}

// Get retrieves a profile from cache if it exists and isn't expired
// Returns (profile, notFound, inCache) - if inCache is true but notFound is true, we know it doesn't exist
func (c *ProfileCacheWrapper) Get(pubkey string) (*ProfileInfo, bool, bool) {
	ctx := context.Background()
	data, found, err := c.backend.Get(ctx, "profile:"+pubkey)
	if err != nil || !found {
		return nil, false, false
	}

	var cached CachedProfile
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false, false
	}

	return cached.Profile, cached.NotFound, true
}

// Delete removes a profile from the cache
func (c *ProfileCacheWrapper) Delete(pubkey string) {
	ctx := context.Background()
	c.backend.Delete(ctx, "profile:"+pubkey)
}

// SetMultiple stores multiple profiles at once (nil profiles are stored as "not found")
func (c *ProfileCacheWrapper) SetMultiple(profiles map[string]*ProfileInfo) {
	ctx := context.Background()
	now := time.Now().Unix()

	for pubkey, profile := range profiles {
		cached := CachedProfile{
			Profile:   profile,
			FetchedAt: now,
			NotFound:  profile == nil,
		}
		data, err := json.Marshal(cached)
		if err != nil {
			continue
		}

		ttl := c.config.ProfileTTL
		if profile == nil {
			ttl = c.config.ProfileNotFoundTTL
		}
		c.backend.Set(ctx, "profile:"+pubkey, data, ttl)
	}
}

// SetNotFound marks multiple pubkeys as "not found" in cache
func (c *ProfileCacheWrapper) SetNotFound(pubkeys []string) {
	ctx := context.Background()
	now := time.Now().Unix()

	for _, pubkey := range pubkeys {
		cached := CachedProfile{
			Profile:   nil,
			FetchedAt: now,
			NotFound:  true,
		}
		data, err := json.Marshal(cached)
		if err != nil {
			continue
		}
		c.backend.Set(ctx, "profile:"+pubkey, data, c.config.ProfileNotFoundTTL)
	}
}

// GetMultiple retrieves multiple profiles, returning found ones and list of missing pubkeys
// Pubkeys with cached "not found" status are NOT included in missing (they're known to not exist)
func (c *ProfileCacheWrapper) GetMultiple(pubkeys []string) (found map[string]*ProfileInfo, missing []string) {
	found = make(map[string]*ProfileInfo)
	ctx := context.Background()

	keys := make([]string, len(pubkeys))
	for i, pk := range pubkeys {
		keys[i] = "profile:" + pk
	}

	results, err := c.backend.GetMultiple(ctx, keys)
	if err != nil {
		return found, pubkeys
	}

	for i, pubkey := range pubkeys {
		data, ok := results[keys[i]]
		if !ok {
			missing = append(missing, pubkey)
			continue
		}

		var cached CachedProfile
		if err := json.Unmarshal(data, &cached); err != nil {
			missing = append(missing, pubkey)
			continue
		}

		// If it's a "not found" entry, don't add to found but also don't add to missing
		if !cached.NotFound && cached.Profile != nil {
			found[pubkey] = cached.Profile
		}
	}

	return found, missing
}

// ContactCacheWrapper provides typed access to contact cache
type ContactCacheWrapper struct {
	backend CacheBackend
	config  CacheConfig
}

func NewContactCacheWrapper(backend CacheBackend, config CacheConfig) *ContactCacheWrapper {
	return &ContactCacheWrapper{backend: backend, config: config}
}

// Get retrieves contacts from cache if not expired
func (c *ContactCacheWrapper) Get(pubkey string) ([]string, bool) {
	ctx := context.Background()
	data, found, err := c.backend.Get(ctx, "contacts:"+pubkey)
	if err != nil || !found {
		return nil, false
	}

	var cached CachedContacts
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}

	return cached.Pubkeys, true
}

// Set stores contacts in the cache
func (c *ContactCacheWrapper) Set(pubkey string, contacts []string) {
	ctx := context.Background()
	cached := CachedContacts{
		Pubkeys:   contacts,
		FetchedAt: time.Now().Unix(),
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	c.backend.Set(ctx, "contacts:"+pubkey, data, c.config.ContactTTL)
}

// RelayListCacheWrapper provides typed access to relay list cache
type RelayListCacheWrapper struct {
	backend CacheBackend
	config  CacheConfig
}

func NewRelayListCacheWrapper(backend CacheBackend, config CacheConfig) *RelayListCacheWrapper {
	return &RelayListCacheWrapper{backend: backend, config: config}
}

// Get retrieves a relay list from cache if not expired
func (c *RelayListCacheWrapper) Get(pubkey string) (*RelayList, bool, bool) {
	ctx := context.Background()
	data, found, err := c.backend.Get(ctx, "relaylist:"+pubkey)
	if err != nil || !found {
		return nil, false, false
	}

	var cached CachedRelayList
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false, false
	}

	return cached.RelayList, cached.NotFound, true
}

// Set stores a relay list in the cache
func (c *RelayListCacheWrapper) Set(pubkey string, relayList *RelayList) {
	ctx := context.Background()
	cached := CachedRelayList{
		RelayList: relayList,
		FetchedAt: time.Now().Unix(),
		NotFound:  relayList == nil,
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}

	ttl := c.config.RelayListTTL
	if relayList == nil {
		ttl = c.config.RelayListNotFoundTTL
	}
	c.backend.Set(ctx, "relaylist:"+pubkey, data, ttl)
}

// AvatarCacheWrapper provides typed access to avatar cache
type AvatarCacheWrapper struct {
	backend CacheBackend
	config  CacheConfig
}

func NewAvatarCacheWrapper(backend CacheBackend, config CacheConfig) *AvatarCacheWrapper {
	return &AvatarCacheWrapper{backend: backend, config: config}
}

// Get checks if an avatar URL validation result is cached
// Returns (isValid, inCache)
func (c *AvatarCacheWrapper) Get(url string) (bool, bool) {
	ctx := context.Background()
	// Hash URL for key to avoid issues with special characters
	hash := sha256.Sum256([]byte(url))
	key := "avatar:" + hex.EncodeToString(hash[:8])

	data, found, err := c.backend.Get(ctx, key)
	if err != nil || !found {
		return false, false
	}

	var cached CachedAvatarResult
	if err := json.Unmarshal(data, &cached); err != nil {
		return false, false
	}

	return cached.Valid, true
}

// Set stores an avatar URL validation result
func (c *AvatarCacheWrapper) Set(url string, valid bool) {
	ctx := context.Background()
	hash := sha256.Sum256([]byte(url))
	key := "avatar:" + hex.EncodeToString(hash[:8])

	cached := CachedAvatarResult{
		Valid:     valid,
		CheckedAt: time.Now().Unix(),
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}

	ttl := c.config.AvatarTTL
	if !valid {
		ttl = c.config.AvatarFailTTL
	}
	c.backend.Set(ctx, key, data, ttl)
}

// LinkPreviewCacheWrapper provides typed access to link preview cache
type LinkPreviewCacheWrapper struct {
	backend CacheBackend
	config  CacheConfig
}

func NewLinkPreviewCacheWrapper(backend CacheBackend, config CacheConfig) *LinkPreviewCacheWrapper {
	return &LinkPreviewCacheWrapper{backend: backend, config: config}
}

// Set stores a link preview in the cache
func (c *LinkPreviewCacheWrapper) Set(url string, preview *LinkPreview) {
	ctx := context.Background()
	hash := sha256.Sum256([]byte(url))
	key := "linkpreview:" + hex.EncodeToString(hash[:8])

	cached := CachedLinkPreview{
		Preview:   preview,
		FetchedAt: time.Now().Unix(),
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}

	ttl := c.config.LinkPreviewTTL
	if preview.Failed {
		ttl = c.config.LinkPreviewFailTTL
	}
	c.backend.Set(ctx, key, data, ttl)
}

// GetMultiple retrieves multiple previews, returning found ones and missing URLs
func (c *LinkPreviewCacheWrapper) GetMultiple(urls []string) (found map[string]*LinkPreview, missing []string) {
	found = make(map[string]*LinkPreview)
	ctx := context.Background()

	keys := make([]string, len(urls))
	urlByKey := make(map[string]string)
	for i, url := range urls {
		hash := sha256.Sum256([]byte(url))
		key := "linkpreview:" + hex.EncodeToString(hash[:8])
		keys[i] = key
		urlByKey[key] = url
	}

	results, err := c.backend.GetMultiple(ctx, keys)
	if err != nil {
		return found, urls
	}

	for _, url := range urls {
		hash := sha256.Sum256([]byte(url))
		key := "linkpreview:" + hex.EncodeToString(hash[:8])

		data, ok := results[key]
		if !ok {
			missing = append(missing, url)
			continue
		}

		var cached CachedLinkPreview
		if err := json.Unmarshal(data, &cached); err != nil {
			missing = append(missing, url)
			continue
		}

		found[url] = cached.Preview
	}

	return found, missing
}

// EventCache provides in-memory caching for relay queries
// This stays in-memory only due to short TTL and instance-specific queries
type EventCache struct {
	mu      sync.RWMutex
	entries map[string]*EventCacheEntry
	maxSize int
	stopCh  chan struct{}
}

// EventCacheEntry holds cached events with expiration
type EventCacheEntry struct {
	Events    []Event
	EOSE      bool
	ExpiresAt time.Time
}

// Global event cache - max 500 cached queries (stays in-memory)
var eventCache = NewEventCache(500)

// NewEventCache creates a new cache with the given max size
func NewEventCache(maxSize int) *EventCache {
	cache := &EventCache{
		entries: make(map[string]*EventCacheEntry),
		maxSize: maxSize,
		stopCh:  make(chan struct{}),
	}
	// Start background cleanup
	go cache.cleanupLoop()
	return cache
}

// Close stops the cleanup goroutine
func (c *EventCache) Close() {
	close(c.stopCh)
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
		return 60 * time.Second
	}
	if len(filter.Authors) <= 5 {
		// Small author list (maybe a profile page)
		return 45 * time.Second
	}
	// Large author list (follow list) - moderate cache
	return 30 * time.Second
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
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.cleanup()
		}
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

// DefaultAvatarURL is the fallback avatar path
const DefaultAvatarURL = "/static/avatar.jpg"

// avatarHTTPClient is a dedicated client for avatar validation with short timeout
var avatarHTTPClient = &http.Client{
	Timeout: 3 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		// Allow up to 3 redirects
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// validateAvatarURL checks if an avatar URL is reachable via HEAD request
func validateAvatarURL(url string) bool {
	if url == "" {
		return false
	}

	// Only validate http/https URLs
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false
	}

	// Set a reasonable user agent
	req.Header.Set("User-Agent", "NostrHypermedia/1.0")

	resp, err := avatarHTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Accept 2xx status codes as valid
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// GetValidatedAvatarURL returns the avatar URL if valid, or the default avatar URL
// This function checks the cache first, then validates if needed
func GetValidatedAvatarURL(url string) string {
	if url == "" {
		return DefaultAvatarURL
	}

	// Check cache first
	if valid, inCache := avatarCache.Get(url); inCache {
		if valid {
			return url
		}
		return DefaultAvatarURL
	}

	// Validate and cache the result
	valid := validateAvatarURL(url)
	avatarCache.Set(url, valid)

	if valid {
		return url
	}
	return DefaultAvatarURL
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

// --- Wallet Info Cache ---
// Caches wallet balance and transaction data to avoid repeated NWC requests

// GetCachedWalletInfo retrieves cached wallet info for a user
// Returns (cached, found)
func GetCachedWalletInfo(userPubkeyHex string) (*CachedWalletInfo, bool) {
	if cacheBackend == nil {
		return nil, false
	}

	ctx := context.Background()
	data, found, err := cacheBackend.Get(ctx, "wallet-info:"+userPubkeyHex)
	if err != nil || !found {
		return nil, false
	}

	var cached CachedWalletInfo
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}

	return &cached, true
}

// SetCachedWalletInfo stores wallet info in the cache
func SetCachedWalletInfo(userPubkeyHex string, info *CachedWalletInfo) {
	if cacheBackend == nil {
		return
	}

	info.CachedAt = time.Now().Unix()
	data, err := json.Marshal(info)
	if err != nil {
		slog.Debug("failed to marshal wallet info for cache", "error", err)
		return
	}

	ctx := context.Background()
	cacheBackend.Set(ctx, "wallet-info:"+userPubkeyHex, data, cacheConfig.WalletInfoTTL)
}

// DeleteCachedWalletInfo removes wallet info from cache (e.g., when wallet disconnected)
func DeleteCachedWalletInfo(userPubkeyHex string) {
	if cacheBackend == nil {
		return
	}

	ctx := context.Background()
	cacheBackend.Delete(ctx, "wallet-info:"+userPubkeyHex)
}

// --- Wallet Info Prefetch ---
// Tracks in-flight wallet info fetches so multiple requests can share the same fetch

var (
	walletInfoPrefetch   = make(map[string]chan *CachedWalletInfo)
	walletInfoPrefetchMu sync.Mutex
)

// StartWalletInfoPrefetch starts a background fetch of wallet info if not already in progress.
// Returns a channel that will receive the result (or nil if fetch already started by another caller).
func StartWalletInfoPrefetch(userPubkeyHex string) chan *CachedWalletInfo {
	walletInfoPrefetchMu.Lock()
	defer walletInfoPrefetchMu.Unlock()

	// Check if prefetch already in progress
	if _, exists := walletInfoPrefetch[userPubkeyHex]; exists {
		return nil // Another goroutine is already fetching
	}

	// Create result channel (buffered to avoid blocking)
	resultCh := make(chan *CachedWalletInfo, 1)
	walletInfoPrefetch[userPubkeyHex] = resultCh

	return resultCh
}

// CompleteWalletInfoPrefetch marks a prefetch as complete and notifies waiters
func CompleteWalletInfoPrefetch(userPubkeyHex string, result *CachedWalletInfo) {
	walletInfoPrefetchMu.Lock()
	ch, exists := walletInfoPrefetch[userPubkeyHex]
	delete(walletInfoPrefetch, userPubkeyHex)
	walletInfoPrefetchMu.Unlock()

	if exists && ch != nil {
		// Send result to waiting channel (non-blocking)
		select {
		case ch <- result:
		default:
		}
		close(ch)
	}
}

// WaitForWalletInfoPrefetch waits for an in-flight prefetch to complete
// Returns (result, found) - found is false if no prefetch in progress
func WaitForWalletInfoPrefetch(userPubkeyHex string, timeout time.Duration) (*CachedWalletInfo, bool) {
	walletInfoPrefetchMu.Lock()
	ch, exists := walletInfoPrefetch[userPubkeyHex]
	walletInfoPrefetchMu.Unlock()

	if !exists || ch == nil {
		return nil, false
	}

	// Wait for result with timeout
	select {
	case result := <-ch:
		return result, true
	case <-time.After(timeout):
		return nil, false
	}
}
