package main

import (
	"context"
	"encoding/hex"
	"log/slog"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// MemorySessionStore implements SessionStore using a map
type MemorySessionStore struct {
	sessions map[string]*BunkerSession
	mu       sync.RWMutex
	ttl      time.Duration
	stopCh   chan struct{}
}

// NewMemorySessionStore creates a new in-memory session store
func NewMemorySessionStore(ttl time.Duration) *MemorySessionStore {
	store := &MemorySessionStore{
		sessions: make(map[string]*BunkerSession),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemorySessionStore) Get(ctx context.Context, sessionID string) (*BunkerSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session := s.sessions[sessionID]
	if session == nil {
		return nil, nil
	}
	// Check expiry
	if time.Since(session.CreatedAt) > s.ttl {
		return nil, nil
	}
	return session, nil
}

func (s *MemorySessionStore) Set(ctx context.Context, session *BunkerSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *MemorySessionStore) Delete(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *MemorySessionStore) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *MemorySessionStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, session := range s.sessions {
		if now.Sub(session.CreatedAt) > s.ttl {
			delete(s.sessions, id)
		}
	}
}

func (s *MemorySessionStore) Close() {
	close(s.stopCh)
}

// MemoryPendingConnStore implements PendingConnStore using a map
type MemoryPendingConnStore struct {
	connections map[string]*PendingConnection
	mu          sync.RWMutex
	ttl         time.Duration
	stopCh      chan struct{}
}

// NewMemoryPendingConnStore creates a new in-memory pending connection store
func NewMemoryPendingConnStore(ttl time.Duration) *MemoryPendingConnStore {
	store := &MemoryPendingConnStore{
		connections: make(map[string]*PendingConnection),
		ttl:         ttl,
		stopCh:      make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryPendingConnStore) Get(ctx context.Context, secret string) (*PendingConnection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conn := s.connections[secret]
	if conn == nil {
		return nil, nil
	}
	// Check expiry
	if time.Since(conn.CreatedAt) > s.ttl {
		return nil, nil
	}
	return conn, nil
}

func (s *MemoryPendingConnStore) Set(ctx context.Context, secret string, conn *PendingConnection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[secret] = conn
	return nil
}

func (s *MemoryPendingConnStore) Delete(ctx context.Context, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, secret)
	return nil
}

func (s *MemoryPendingConnStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *MemoryPendingConnStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for secret, conn := range s.connections {
		if now.Sub(conn.CreatedAt) > s.ttl {
			delete(s.connections, secret)
		}
	}
}

func (s *MemoryPendingConnStore) Close() {
	close(s.stopCh)
}

// Session serialization helpers

func bunkerSessionToCached(s *BunkerSession) *CachedSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	cached := &CachedSession{
		ID:                 s.ID,
		ClientPrivKey:      hex.EncodeToString(s.ClientPrivKey),
		ClientPubKey:       hex.EncodeToString(s.ClientPubKey),
		RemoteSignerPubKey: hex.EncodeToString(s.RemoteSignerPubKey),
		UserPubKey:         hex.EncodeToString(s.UserPubKey),
		Relays:             s.Relays,
		Secret:             s.Secret,
		ConversationKey:    hex.EncodeToString(s.ConversationKey),
		Connected:          s.Connected,
		CreatedAt:          s.CreatedAt.Unix(),
		FollowingPubkeys:   s.FollowingPubkeys,
		BookmarkedEventIDs: s.BookmarkedEventIDs,
		ReactedEventIDs:    s.ReactedEventIDs,
		RepostedEventIDs:   s.RepostedEventIDs,
		ZappedEventIDs:     s.ZappedEventIDs,
		MutedPubkeys:       s.MutedPubkeys,
		MutedEventIDs:      s.MutedEventIDs,
		MutedHashtags:      s.MutedHashtags,
		MutedWords:         s.MutedWords,
	}

	if s.UserRelayList != nil {
		cached.UserRelayListRead = s.UserRelayList.Read
		cached.UserRelayListWrite = s.UserRelayList.Write
	}

	if s.NWCConfig != nil {
		cached.NWCWalletPubKey = hex.EncodeToString(s.NWCConfig.WalletPubKey)
		cached.NWCRelay = s.NWCConfig.Relay
		cached.NWCSecret = hex.EncodeToString(s.NWCConfig.Secret)
		cached.NWCClientPubKey = hex.EncodeToString(s.NWCConfig.ClientPubKey)
		cached.NWCConversationKey = hex.EncodeToString(s.NWCConfig.ConversationKey)
		cached.NWCNip04SharedKey = hex.EncodeToString(s.NWCConfig.Nip04SharedKey)
	}

	return cached
}

func cachedSessionToBunkerSession(c *CachedSession) (*BunkerSession, error) {
	clientPrivKey, _ := hex.DecodeString(c.ClientPrivKey)
	clientPubKey, _ := hex.DecodeString(c.ClientPubKey)
	remoteSignerPubKey, _ := hex.DecodeString(c.RemoteSignerPubKey)
	userPubKey, _ := hex.DecodeString(c.UserPubKey)
	conversationKey, _ := hex.DecodeString(c.ConversationKey)

	session := &BunkerSession{
		ID:                 c.ID,
		ClientPrivKey:      clientPrivKey,
		ClientPubKey:       clientPubKey,
		RemoteSignerPubKey: remoteSignerPubKey,
		UserPubKey:         userPubKey,
		Relays:             c.Relays,
		Secret:             c.Secret,
		ConversationKey:    conversationKey,
		Connected:          c.Connected,
		CreatedAt:          time.Unix(c.CreatedAt, 0),
		FollowingPubkeys:   c.FollowingPubkeys,
		BookmarkedEventIDs: c.BookmarkedEventIDs,
		ReactedEventIDs:    c.ReactedEventIDs,
		RepostedEventIDs:   c.RepostedEventIDs,
		ZappedEventIDs:     c.ZappedEventIDs,
		MutedPubkeys:       c.MutedPubkeys,
		MutedEventIDs:      c.MutedEventIDs,
		MutedHashtags:      c.MutedHashtags,
		MutedWords:         c.MutedWords,
	}

	if len(c.UserRelayListRead) > 0 || len(c.UserRelayListWrite) > 0 {
		session.UserRelayList = &RelayList{
			Read:  c.UserRelayListRead,
			Write: c.UserRelayListWrite,
		}
	}

	if c.NWCRelay != "" {
		walletPubKey, _ := hex.DecodeString(c.NWCWalletPubKey)
		nwcSecret, _ := hex.DecodeString(c.NWCSecret)
		nwcClientPubKey, _ := hex.DecodeString(c.NWCClientPubKey)
		nwcConversationKey, _ := hex.DecodeString(c.NWCConversationKey)
		nwcNip04SharedKey, _ := hex.DecodeString(c.NWCNip04SharedKey)
		session.NWCConfig = &NWCConfig{
			WalletPubKey:     walletPubKey,
			Relay:            c.NWCRelay,
			Secret:           nwcSecret,
			ClientPubKey:     nwcClientPubKey,
			ConversationKey:  nwcConversationKey,
			Nip04SharedKey:   nwcNip04SharedKey,
		}
	}

	return session, nil
}

// Pending connection serialization helpers

func pendingConnToCached(p *PendingConnection) *CachedPendingConnection {
	p.mu.Lock()
	defer p.mu.Unlock()

	cached := &CachedPendingConnection{
		Secret:             p.Secret,
		ClientPrivKey:      hex.EncodeToString(p.ClientPrivKey),
		ClientPubKey:       hex.EncodeToString(p.ClientPubKey),
		Relays:             p.Relays,
		ConversationKey:    hex.EncodeToString(p.ConversationKey),
		CreatedAt:          p.CreatedAt.Unix(),
		RemoteSignerPubKey: hex.EncodeToString(p.RemoteSignerPubKey),
		UserPubKey:         hex.EncodeToString(p.UserPubKey),
		Connected:          p.Connected,
	}

	if p.UserRelayList != nil {
		cached.UserRelayListRead = p.UserRelayList.Read
		cached.UserRelayListWrite = p.UserRelayList.Write
	}

	return cached
}

func cachedToPendingConn(c *CachedPendingConnection) (*PendingConnection, error) {
	clientPrivKey, _ := hex.DecodeString(c.ClientPrivKey)
	clientPubKey, _ := hex.DecodeString(c.ClientPubKey)
	conversationKey, _ := hex.DecodeString(c.ConversationKey)
	remoteSignerPubKey, _ := hex.DecodeString(c.RemoteSignerPubKey)
	userPubKey, _ := hex.DecodeString(c.UserPubKey)

	conn := &PendingConnection{
		Secret:             c.Secret,
		ClientPrivKey:      clientPrivKey,
		ClientPubKey:       clientPubKey,
		Relays:             c.Relays,
		ConversationKey:    conversationKey,
		CreatedAt:          time.Unix(c.CreatedAt, 0),
		RemoteSignerPubKey: remoteSignerPubKey,
		UserPubKey:         userPubKey,
		Connected:          c.Connected,
	}

	if len(c.UserRelayListRead) > 0 || len(c.UserRelayListWrite) > 0 {
		conn.UserRelayList = &RelayList{
			Read:  c.UserRelayListRead,
			Write: c.UserRelayListWrite,
		}
	}

	return conn, nil
}

// MemoryRateLimitStore implements RateLimitStore using in-memory storage
type MemoryRateLimitStore struct {
	buckets sync.Map // map[string]*rateLimitBucket
	stopCh  chan struct{}
}

type rateLimitBucket struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// NewMemoryRateLimitStore creates a new in-memory rate limit store
func NewMemoryRateLimitStore() *MemoryRateLimitStore {
	store := &MemoryRateLimitStore{
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryRateLimitStore) cleanupLoop() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryRateLimitStore) Close() {
	close(s.stopCh)
}

func (s *MemoryRateLimitStore) Check(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	bucket := s.getBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Remove expired entries
	valid := make([]time.Time, 0, len(bucket.timestamps))
	for _, t := range bucket.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	bucket.timestamps = valid

	remaining := limit - len(bucket.timestamps)
	if remaining < 0 {
		remaining = 0
	}
	return len(bucket.timestamps) < limit, remaining, nil
}

func (s *MemoryRateLimitStore) Increment(ctx context.Context, key string, window time.Duration) error {
	bucket := s.getBucket(key)
	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Remove expired entries and add new one
	valid := make([]time.Time, 0, len(bucket.timestamps)+1)
	for _, t := range bucket.timestamps {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	valid = append(valid, now)
	bucket.timestamps = valid
	return nil
}

func (s *MemoryRateLimitStore) getBucket(key string) *rateLimitBucket {
	val, _ := s.buckets.LoadOrStore(key, &rateLimitBucket{})
	return val.(*rateLimitBucket)
}

func (s *MemoryRateLimitStore) cleanup() {
	cutoff := time.Now().Add(-1 * time.Hour) // Clean buckets with no recent activity
	s.buckets.Range(func(key, value interface{}) bool {
		bucket := value.(*rateLimitBucket)
		bucket.mu.Lock()
		// Remove bucket if all timestamps are old or bucket is empty
		allOld := true
		for _, t := range bucket.timestamps {
			if t.After(cutoff) {
				allOld = false
				break
			}
		}
		isEmpty := len(bucket.timestamps) == 0
		bucket.mu.Unlock()
		if allOld || isEmpty {
			s.buckets.Delete(key)
		}
		return true
	})
}

// MemorySearchCacheStore implements SearchCacheStore using in-memory storage
type MemorySearchCacheStore struct {
	cache   sync.Map // map[string]*searchCacheEntry
	count   int64    // Atomic counter for entry count
	maxSize int64    // Maximum number of entries
	stopCh  chan struct{}
	mu      sync.Mutex // Protects eviction
}

type searchCacheEntry struct {
	events    []Event
	expiresAt time.Time
}

// NewMemorySearchCacheStore creates a new in-memory search cache store
func NewMemorySearchCacheStore() *MemorySearchCacheStore {
	store := &MemorySearchCacheStore{
		maxSize: 1000, // Limit to 1000 search results
		stopCh:  make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemorySearchCacheStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemorySearchCacheStore) Close() {
	close(s.stopCh)
}

func (s *MemorySearchCacheStore) makeKey(query string, kinds []int, limit int) string {
	// Create a deterministic key from query params
	kindsStr := ""
	if len(kinds) > 0 {
		sortedKinds := make([]int, len(kinds))
		copy(sortedKinds, kinds)
		sort.Ints(sortedKinds)
		for i, k := range sortedKinds {
			if i > 0 {
				kindsStr += ","
			}
			kindsStr += strconv.Itoa(k)
		}
	}
	return query + "|" + kindsStr + "|" + strconv.Itoa(limit)
}

func (s *MemorySearchCacheStore) Get(ctx context.Context, query string, kinds []int, limit int) ([]Event, bool, error) {
	key := s.makeKey(query, kinds, limit)
	val, ok := s.cache.Load(key)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*searchCacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(key)
		return nil, false, nil
	}
	return entry.events, true, nil
}

func (s *MemorySearchCacheStore) Set(ctx context.Context, query string, kinds []int, limit int, events []Event, ttl time.Duration) error {
	key := s.makeKey(query, kinds, limit)

	// Check if we're updating an existing entry (doesn't change count)
	_, existed := s.cache.Load(key)

	s.cache.Store(key, &searchCacheEntry{
		events:    events,
		expiresAt: time.Now().Add(ttl),
	})

	if !existed {
		atomic.AddInt64(&s.count, 1)
		// Evict if over limit
		if atomic.LoadInt64(&s.count) > s.maxSize {
			s.evictOldest()
		}
	}
	return nil
}

// evictOldest removes expired and oldest entries to get back under maxSize
func (s *MemorySearchCacheStore) evictOldest() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// First pass: collect all entries with timestamps
	type entryInfo struct {
		key       string
		expiresAt time.Time
	}
	var entries []entryInfo
	now := time.Now()

	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*searchCacheEntry)
		// Delete expired entries immediately
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
			atomic.AddInt64(&s.count, -1)
		} else {
			entries = append(entries, entryInfo{key: key.(string), expiresAt: entry.expiresAt})
		}
		return true
	})

	// If still over limit, remove oldest 10%
	currentCount := atomic.LoadInt64(&s.count)
	if currentCount > s.maxSize {
		toRemove := int(s.maxSize / 10)
		if toRemove < 1 {
			toRemove = 1
		}

		// Sort by expiration (soonest to expire first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].expiresAt.Before(entries[j].expiresAt)
		})

		for i := 0; i < toRemove && i < len(entries); i++ {
			s.cache.Delete(entries[i].key)
			atomic.AddInt64(&s.count, -1)
		}
	}
}

func (s *MemorySearchCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*searchCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
			atomic.AddInt64(&s.count, -1)
		}
		return true
	})
}

// MemoryThreadCacheStore implements ThreadCacheStore using in-memory storage
type MemoryThreadCacheStore struct {
	cache  sync.Map // map[string]*threadCacheEntry
	stopCh chan struct{}
}

type threadCacheEntry struct {
	thread    *CachedThread
	expiresAt time.Time
}

// NewMemoryThreadCacheStore creates a new in-memory thread cache store
func NewMemoryThreadCacheStore() *MemoryThreadCacheStore {
	store := &MemoryThreadCacheStore{
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryThreadCacheStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryThreadCacheStore) Close() {
	close(s.stopCh)
}

func (s *MemoryThreadCacheStore) Get(ctx context.Context, rootEventID string) (*CachedThread, bool, error) {
	val, ok := s.cache.Load(rootEventID)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*threadCacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(rootEventID)
		return nil, false, nil
	}
	return entry.thread, true, nil
}

func (s *MemoryThreadCacheStore) Set(ctx context.Context, rootEventID string, thread *CachedThread, ttl time.Duration) error {
	s.cache.Store(rootEventID, &threadCacheEntry{
		thread:    thread,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (s *MemoryThreadCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*threadCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
		}
		return true
	})
}

// MemoryNotificationReadStore implements NotificationReadStore using in-memory storage
type MemoryNotificationReadStore struct {
	cache  sync.Map // map[string]*notificationReadEntry
	ttl    time.Duration
	stopCh chan struct{}
}

type notificationReadEntry struct {
	timestamp int64
	expiresAt time.Time
}

// NewMemoryNotificationReadStore creates a new in-memory notification read store
func NewMemoryNotificationReadStore(ttl time.Duration) *MemoryNotificationReadStore {
	store := &MemoryNotificationReadStore{
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryNotificationReadStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryNotificationReadStore) Close() {
	close(s.stopCh)
}

func (s *MemoryNotificationReadStore) GetLastRead(ctx context.Context, pubkey string) (int64, bool, error) {
	val, ok := s.cache.Load(pubkey)
	if !ok {
		return 0, false, nil
	}
	entry := val.(*notificationReadEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(pubkey)
		return 0, false, nil
	}
	return entry.timestamp, true, nil
}

func (s *MemoryNotificationReadStore) SetLastRead(ctx context.Context, pubkey string, timestamp int64) error {
	s.cache.Store(pubkey, &notificationReadEntry{
		timestamp: timestamp,
		expiresAt: time.Now().Add(s.ttl),
	})
	return nil
}

func (s *MemoryNotificationReadStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*notificationReadEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
		}
		return true
	})
}

// MemoryNotificationCacheStore implements NotificationCacheStore using in-memory storage
type MemoryNotificationCacheStore struct {
	cache  sync.Map // map[string]*notificationCacheEntry
	stopCh chan struct{}
}

type notificationCacheEntry struct {
	cached    *CachedNotifications
	expiresAt time.Time
}

// NewMemoryNotificationCacheStore creates a new in-memory notification cache store
func NewMemoryNotificationCacheStore() *MemoryNotificationCacheStore {
	store := &MemoryNotificationCacheStore{
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryNotificationCacheStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryNotificationCacheStore) Close() {
	close(s.stopCh)
}

func (s *MemoryNotificationCacheStore) Get(ctx context.Context, pubkey string) (*CachedNotifications, bool, error) {
	val, ok := s.cache.Load(pubkey)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*notificationCacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(pubkey)
		return nil, false, nil
	}
	return entry.cached, true, nil
}

func (s *MemoryNotificationCacheStore) Set(ctx context.Context, pubkey string, cached *CachedNotifications, ttl time.Duration) error {
	s.cache.Store(pubkey, &notificationCacheEntry{
		cached:    cached,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (s *MemoryNotificationCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*notificationCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
		}
		return true
	})
}

// MemoryDVMCacheStore implements DVMCacheStore using in-memory storage
type MemoryDVMCacheStore struct {
	cache  sync.Map // map[string]*dvmCacheEntry
	stopCh chan struct{}
}

type dvmCacheEntry struct {
	result    *CachedDVMResult
	expiresAt time.Time
}

// NewMemoryDVMCacheStore creates a new in-memory DVM cache store
func NewMemoryDVMCacheStore() *MemoryDVMCacheStore {
	store := &MemoryDVMCacheStore{
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryDVMCacheStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryDVMCacheStore) Close() {
	close(s.stopCh)
}

func (s *MemoryDVMCacheStore) Get(ctx context.Context, key string) (*CachedDVMResult, bool, error) {
	val, ok := s.cache.Load(key)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*dvmCacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(key)
		return nil, false, nil
	}
	return entry.result, true, nil
}

func (s *MemoryDVMCacheStore) Set(ctx context.Context, key string, result *CachedDVMResult, ttl time.Duration) error {
	s.cache.Store(key, &dvmCacheEntry{
		result:    result,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (s *MemoryDVMCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*dvmCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
		}
		return true
	})
}

// MemoryDVMMetaCacheStore implements DVMMetaCacheStore using in-memory storage
type MemoryDVMMetaCacheStore struct {
	cache  sync.Map // map[string]*dvmMetaCacheEntry
	stopCh chan struct{}
}

type dvmMetaCacheEntry struct {
	meta      *DVMMetadata
	expiresAt time.Time
}

// NewMemoryDVMMetaCacheStore creates a new in-memory DVM metadata cache store
func NewMemoryDVMMetaCacheStore() *MemoryDVMMetaCacheStore {
	store := &MemoryDVMMetaCacheStore{
		stopCh: make(chan struct{}),
	}
	go store.cleanupLoop()
	return store
}

func (s *MemoryDVMMetaCacheStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// Close stops the cleanup goroutine
func (s *MemoryDVMMetaCacheStore) Close() {
	close(s.stopCh)
}

func (s *MemoryDVMMetaCacheStore) Get(ctx context.Context, key string) (*DVMMetadata, bool, error) {
	val, ok := s.cache.Load(key)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*dvmMetaCacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(key)
		return nil, false, nil
	}
	return entry.meta, true, nil
}

func (s *MemoryDVMMetaCacheStore) Set(ctx context.Context, key string, meta *DVMMetadata, ttl time.Duration) error {
	s.cache.Store(key, &dvmMetaCacheEntry{
		meta:      meta,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (s *MemoryDVMMetaCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*dvmMetaCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
		}
		return true
	})
}

// MemoryNIP05Cache implements NIP05CacheStore using in-memory storage
type MemoryNIP05Cache struct {
	cache  sync.Map // map[string]*nip05CacheEntry
	ttl    time.Duration
	stopCh chan struct{}
}

type nip05CacheEntry struct {
	result    *NIP05Result
	expiresAt time.Time
}

// NewMemoryNIP05Cache creates a new in-memory NIP-05 cache
func NewMemoryNIP05Cache(ttl time.Duration) *MemoryNIP05Cache {
	cache := &MemoryNIP05Cache{
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go cache.cleanupLoop()
	return cache
}

func (c *MemoryNIP05Cache) Get(identifier string) (*NIP05Result, bool) {
	val, ok := c.cache.Load(identifier)
	if !ok {
		return nil, false
	}
	entry := val.(*nip05CacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.cache.Delete(identifier)
		return nil, false
	}
	return entry.result, true
}

func (c *MemoryNIP05Cache) Set(identifier string, result *NIP05Result) {
	c.cache.Store(identifier, &nip05CacheEntry{
		result:    result,
		expiresAt: time.Now().Add(c.ttl),
	})
}

func (c *MemoryNIP05Cache) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
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

func (c *MemoryNIP05Cache) cleanup() {
	now := time.Now()
	c.cache.Range(func(key, value interface{}) bool {
		entry := value.(*nip05CacheEntry)
		if now.After(entry.expiresAt) {
			c.cache.Delete(key)
		}
		return true
	})
}

func (c *MemoryNIP05Cache) Close() {
	close(c.stopCh)
}

// MemoryRelayHealth implements RelayHealthStore using in-memory storage
type MemoryRelayHealth struct {
	mu       sync.RWMutex
	failures map[string]*relayFailure
	stats    map[string]*relayStats
}

// NewMemoryRelayHealth creates a new in-memory relay health tracker
func NewMemoryRelayHealth() *MemoryRelayHealth {
	return &MemoryRelayHealth{
		failures: make(map[string]*relayFailure),
		stats:    make(map[string]*relayStats),
	}
}

func (h *MemoryRelayHealth) shouldSkip(relayURL string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	f := h.failures[relayURL]
	if f == nil {
		return false
	}
	return time.Now().Before(f.backoffUntil)
}

func (h *MemoryRelayHealth) recordFailure(relayURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	f := h.failures[relayURL]
	if f == nil {
		f = &relayFailure{}
		h.failures[relayURL] = f
	}

	f.lastFailure = time.Now()
	f.failureCount++

	var backoff time.Duration // Exponential: 30s, 60s, 2m, 5m max
	switch {
	case f.failureCount <= 1:
		backoff = 30 * time.Second
	case f.failureCount == 2:
		backoff = 60 * time.Second
	case f.failureCount == 3:
		backoff = 2 * time.Minute
	default:
		backoff = 5 * time.Minute
	}

	f.backoffUntil = time.Now().Add(backoff)
	slog.Warn("relay connection failed",
		"relay", relayURL,
		"failure_count", f.failureCount,
		"backoff_until", f.backoffUntil.Format("15:04:05"))
}

func (h *MemoryRelayHealth) recordSuccess(relayURL string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.failures, relayURL)
}

func (h *MemoryRelayHealth) recordResponseTime(relayURL string, duration time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.stats[relayURL]
	if s == nil {
		s = &relayStats{}
		h.stats[relayURL] = s
	}

	// Exponential moving average (alpha=0.3)
	if s.responseCount == 0 {
		s.avgResponseTime = duration
	} else {
		alpha := 0.3
		s.avgResponseTime = time.Duration(alpha*float64(duration) + (1-alpha)*float64(s.avgResponseTime))
	}

	s.responseCount++
	s.lastResponse = time.Now()
}

func (h *MemoryRelayHealth) getAverageResponseTime(relayURL string) time.Duration {
	h.mu.RLock()
	defer h.mu.RUnlock()

	s := h.stats[relayURL]
	if s == nil || s.responseCount == 0 {
		return 1 * time.Second
	}
	return s.avgResponseTime
}

func (h *MemoryRelayHealth) getRelayScore(relayURL string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	score := 50

	if s := h.stats[relayURL]; s != nil && s.responseCount > 0 {
		avgMs := s.avgResponseTime.Milliseconds()
		switch {
		case avgMs < 200:
			score = 50
		case avgMs < 500:
			score = 40
		case avgMs < 1000:
			score = 25
		default:
			score = 10
		}

		bonus := s.responseCount
		if bonus > 10 {
			bonus = 10
		}
		score += bonus
	}

	if f := h.failures[relayURL]; f != nil {
		penalty := f.failureCount * 10
		if penalty > 30 {
			penalty = 30
		}
		score -= penalty

		if time.Now().Before(f.backoffUntil) {
			score -= 20
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func (h *MemoryRelayHealth) SortRelaysByScore(relays []string) []string {
	if len(relays) <= 1 {
		return relays
	}

	scores := make(map[string]int, len(relays))
	for _, relay := range relays {
		scores[relay] = h.getRelayScore(relay)
	}

	sorted := make([]string, len(relays))
	copy(sorted, relays)

	sort.Slice(sorted, func(i, j int) bool {
		return scores[sorted[i]] > scores[sorted[j]]
	})

	return sorted
}

func (h *MemoryRelayHealth) GetExpectedResponseTime(relays []string, minRelays int) time.Duration {
	if len(relays) == 0 {
		return 500 * time.Millisecond
	}

	times := make([]time.Duration, 0, len(relays))
	for _, relay := range relays {
		times = append(times, h.getAverageResponseTime(relay))
	}

	sort.Slice(times, func(i, j int) bool {
		return times[i] < times[j]
	})

	idx := minRelays - 1
	if idx >= len(times) {
		idx = len(times) - 1
	}
	if idx < 0 {
		idx = 0
	}

	expected := times[idx] + times[idx]/2 // +50% buffer
	if expected < 200*time.Millisecond {
		expected = 200 * time.Millisecond
	}
	if expected > 2*time.Second {
		expected = 2 * time.Second
	}

	return expected
}

func (h *MemoryRelayHealth) GetRelayHealthStats() (healthy int, unhealthy int, avgResponseMs int64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var totalMs int64
	var count int

	for relay, stats := range h.stats {
		if stats.responseCount > 0 {
			// Check if this relay is in backoff
			if f := h.failures[relay]; f != nil && f.failureCount > 0 {
				unhealthy++
			} else {
				healthy++
			}
			totalMs += stats.avgResponseTime.Milliseconds()
			count++
		}
	}

	if count > 0 {
		avgResponseMs = totalMs / int64(count)
	}

	return healthy, unhealthy, avgResponseMs
}

func (h *MemoryRelayHealth) GetRelayHealthDetails() []RelayHealthDetail {
	h.mu.RLock()
	defer h.mu.RUnlock()

	details := make([]RelayHealthDetail, 0, len(h.stats))
	for relay, stats := range h.stats {
		if stats.responseCount > 0 {
			status := "healthy"
			if f := h.failures[relay]; f != nil && f.failureCount > 0 {
				status = "unhealthy"
			}
			details = append(details, RelayHealthDetail{
				URL:           relay,
				Status:        status,
				AvgResponseMs: stats.avgResponseTime.Milliseconds(),
				RequestCount:  int64(stats.responseCount),
			})
		}
	}
	return details
}

// MemoryWavlakeCache implements WavlakeCacheStore using in-memory storage
type MemoryWavlakeCache struct {
	cache sync.Map // map[string]*wavlakeCacheEntry
}

type wavlakeCacheEntry struct {
	track     *WavlakeTrack
	expiresAt time.Time
}

// NewMemoryWavlakeCache creates a new in-memory Wavlake cache
func NewMemoryWavlakeCache() *MemoryWavlakeCache {
	return &MemoryWavlakeCache{}
}

// Get retrieves a cached Wavlake track if available and not expired
func (c *MemoryWavlakeCache) Get(trackID string) (*WavlakeTrack, bool) {
	val, ok := c.cache.Load(trackID)
	if !ok {
		return nil, false
	}

	entry := val.(*wavlakeCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.cache.Delete(trackID)
		return nil, false
	}

	return entry.track, true
}

// Set stores a Wavlake track in the cache with the given TTL
func (c *MemoryWavlakeCache) Set(trackID string, track *WavlakeTrack, ttl time.Duration) {
	c.cache.Store(trackID, &wavlakeCacheEntry{
		track:     track,
		expiresAt: time.Now().Add(ttl),
	})
}

// --- LNURL Pay Info Cache ---

// MemoryLNURLCache caches LNURL pay info for faster zap interactions
type MemoryLNURLCache struct {
	cache sync.Map // map[string]*lnurlCacheEntry
	ttl   time.Duration
}

type lnurlCacheEntry struct {
	info      *LNURLPayInfo
	notFound  bool
	expiresAt time.Time
}

// NewMemoryLNURLCache creates a new in-memory LNURL cache
func NewMemoryLNURLCache(ttl time.Duration) *MemoryLNURLCache {
	return &MemoryLNURLCache{ttl: ttl}
}

// Get retrieves cached LNURL pay info if available and not expired
func (c *MemoryLNURLCache) Get(pubkey string) (*LNURLPayInfo, bool) {
	val, ok := c.cache.Load(pubkey)
	if !ok {
		return nil, false
	}

	entry := val.(*lnurlCacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.cache.Delete(pubkey)
		return nil, false
	}

	// Return nil for "not found" entries but still return true (was cached)
	if entry.notFound {
		return nil, true
	}

	return entry.info, true
}

// Set stores LNURL pay info in the cache
func (c *MemoryLNURLCache) Set(pubkey string, info *LNURLPayInfo) {
	c.cache.Store(pubkey, &lnurlCacheEntry{
		info:      info,
		notFound:  false,
		expiresAt: time.Now().Add(c.ttl),
	})
}

// SetNotFound marks a pubkey as having no LNURL (no lud16/lud06 or failed resolution)
func (c *MemoryLNURLCache) SetNotFound(pubkey string) {
	c.cache.Store(pubkey, &lnurlCacheEntry{
		info:      nil,
		notFound:  true,
		expiresAt: time.Now().Add(c.ttl),
	})
}
