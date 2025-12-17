package main

import (
	"context"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// MemoryCache implements CacheBackend using sync.Map
type MemoryCache struct {
	data            sync.Map
	maxSize         int
	cleanupInterval time.Duration
	stopCh          chan struct{}
}

type memoryCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(maxSize int, cleanupInterval time.Duration) *MemoryCache {
	mc := &MemoryCache{
		maxSize:         maxSize,
		cleanupInterval: cleanupInterval,
		stopCh:          make(chan struct{}),
	}
	go mc.cleanupLoop()
	return mc
}

func (m *MemoryCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, ok := m.data.Load(key)
	if !ok {
		return nil, false, nil
	}
	entry := val.(*memoryCacheEntry)
	if time.Now().After(entry.expiresAt) {
		m.data.Delete(key)
		return nil, false, nil
	}
	return entry.value, true, nil
}

func (m *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	m.data.Store(key, &memoryCacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (m *MemoryCache) Delete(ctx context.Context, key string) error {
	m.data.Delete(key)
	return nil
}

func (m *MemoryCache) GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error) {
	result := make(map[string][]byte)
	now := time.Now()
	for _, key := range keys {
		val, ok := m.data.Load(key)
		if !ok {
			continue
		}
		entry := val.(*memoryCacheEntry)
		if now.After(entry.expiresAt) {
			m.data.Delete(key)
			continue
		}
		result[key] = entry.value
	}
	return result, nil
}

func (m *MemoryCache) SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	expiresAt := time.Now().Add(ttl)
	for key, value := range items {
		m.data.Store(key, &memoryCacheEntry{
			value:     value,
			expiresAt: expiresAt,
		})
	}
	return nil
}

func (m *MemoryCache) Close() error {
	close(m.stopCh)
	return nil
}

func (m *MemoryCache) cleanupLoop() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *MemoryCache) cleanup() {
	now := time.Now()
	var entries []struct {
		key       string
		expiresAt time.Time
	}

	// Remove expired entries and collect remaining
	m.data.Range(func(key, value interface{}) bool {
		k := key.(string)
		entry := value.(*memoryCacheEntry)
		if now.After(entry.expiresAt) {
			m.data.Delete(k)
		} else {
			entries = append(entries, struct {
				key       string
				expiresAt time.Time
			}{k, entry.expiresAt})
		}
		return true
	})

	// Enforce max size by removing oldest entries
	if len(entries) > m.maxSize {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].expiresAt.Before(entries[j].expiresAt)
		})
		toRemove := len(entries) - m.maxSize
		for i := 0; i < toRemove; i++ {
			m.data.Delete(entries[i].key)
		}
	}
}

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
		// Remove bucket if all timestamps are old
		allOld := true
		for _, t := range bucket.timestamps {
			if t.After(cutoff) {
				allOld = false
				break
			}
		}
		bucket.mu.Unlock()
		if allOld && len(bucket.timestamps) == 0 {
			s.buckets.Delete(key)
		}
		return true
	})
}

// MemorySearchCacheStore implements SearchCacheStore using in-memory storage
type MemorySearchCacheStore struct {
	cache  sync.Map // map[string]*searchCacheEntry
	stopCh chan struct{}
}

type searchCacheEntry struct {
	events    []Event
	expiresAt time.Time
}

// NewMemorySearchCacheStore creates a new in-memory search cache store
func NewMemorySearchCacheStore() *MemorySearchCacheStore {
	store := &MemorySearchCacheStore{
		stopCh: make(chan struct{}),
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
			kindsStr += string(rune('0' + k%10)) // Simple int to string
		}
	}
	return query + "|" + kindsStr + "|" + string(rune('0'+limit%10))
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
	s.cache.Store(key, &searchCacheEntry{
		events:    events,
		expiresAt: time.Now().Add(ttl),
	})
	return nil
}

func (s *MemorySearchCacheStore) cleanup() {
	now := time.Now()
	s.cache.Range(func(key, value interface{}) bool {
		entry := value.(*searchCacheEntry)
		if now.After(entry.expiresAt) {
			s.cache.Delete(key)
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
