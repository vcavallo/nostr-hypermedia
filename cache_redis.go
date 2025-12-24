package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements CacheBackend using Redis
type RedisCache struct {
	client *redis.Client
	prefix string
}

// NewRedisCache creates a new Redis cache from URL
// URL format: redis://[:password@]host:port/db
func NewRedisCache(redisURL string, prefix string) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	// Connection pool settings
	opts.PoolSize = 10
	opts.MinIdleConns = 2
	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 3 * time.Second
	opts.WriteTimeout = 3 * time.Second

	client := redis.NewClient(opts)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connection failed: %w", err)
	}

	return &RedisCache{
		client: client,
		prefix: prefix,
	}, nil
}

func (r *RedisCache) key(k string) string {
	return r.prefix + k
}

func (r *RedisCache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	data, err := r.client.Get(ctx, r.key(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, r.key(key), value, ttl).Err()
}

func (r *RedisCache) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, r.key(key)).Err()
}

func (r *RedisCache) GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	prefixedKeys := make([]string, len(keys))
	for i, k := range keys {
		prefixedKeys[i] = r.key(k)
	}

	values, err := r.client.MGet(ctx, prefixedKeys...).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string][]byte)
	for i, v := range values {
		if v != nil {
			if str, ok := v.(string); ok {
				result[keys[i]] = []byte(str)
			}
		}
	}

	return result, nil
}

func (r *RedisCache) SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if len(items) == 0 {
		return nil
	}

	pipe := r.client.Pipeline()
	for key, value := range items {
		pipe.Set(ctx, r.key(key), value, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisCache) Close() error {
	return r.client.Close()
}

// GetClient returns the underlying Redis client for use by other stores
func (r *RedisCache) GetClient() *redis.Client {
	return r.client
}

// RedisSessionStore implements SessionStore using Redis
type RedisSessionStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisSessionStore creates a new Redis session store
func NewRedisSessionStore(client *redis.Client, prefix string, ttl time.Duration) *RedisSessionStore {
	return &RedisSessionStore{
		client: client,
		prefix: prefix + "session:",
		ttl:    ttl,
	}
}

func (s *RedisSessionStore) Get(ctx context.Context, sessionID string) (*BunkerSession, error) {
	data, err := s.client.Get(ctx, s.prefix+sessionID).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		slog.Error("Redis session get error", "error", err)
		return nil, nil // Return nil instead of error to allow graceful degradation
	}

	var cached CachedSession
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Error("Redis session unmarshal error", "error", err)
		return nil, nil
	}

	return cachedSessionToBunkerSession(&cached)
}

func (s *RedisSessionStore) Set(ctx context.Context, session *BunkerSession) error {
	cached := bunkerSessionToCached(session)
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}

	err = s.client.Set(ctx, s.prefix+session.ID, data, s.ttl).Err()
	if err != nil {
		slog.Error("Redis session set error", "error", err)
	}
	return err
}

func (s *RedisSessionStore) Delete(ctx context.Context, sessionID string) error {
	err := s.client.Del(ctx, s.prefix+sessionID).Err()
	if err != nil {
		slog.Error("Redis session delete error", "error", err)
	}
	return err
}

// RedisPendingConnStore implements PendingConnStore using Redis
type RedisPendingConnStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisPendingConnStore creates a new Redis pending connection store
func NewRedisPendingConnStore(client *redis.Client, prefix string, ttl time.Duration) *RedisPendingConnStore {
	return &RedisPendingConnStore{
		client: client,
		prefix: prefix + "pending:",
		ttl:    ttl,
	}
}

func (s *RedisPendingConnStore) Get(ctx context.Context, secret string) (*PendingConnection, error) {
	data, err := s.client.Get(ctx, s.prefix+secret).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		slog.Error("Redis pending conn get error", "error", err)
		return nil, nil
	}

	var cached CachedPendingConnection
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Error("Redis pending conn unmarshal error", "error", err)
		return nil, nil
	}

	return cachedToPendingConn(&cached)
}

func (s *RedisPendingConnStore) Set(ctx context.Context, secret string, conn *PendingConnection) error {
	cached := pendingConnToCached(conn)
	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}

	err = s.client.Set(ctx, s.prefix+secret, data, s.ttl).Err()
	if err != nil {
		slog.Error("Redis pending conn set error", "error", err)
	}
	return err
}

func (s *RedisPendingConnStore) Delete(ctx context.Context, secret string) error {
	err := s.client.Del(ctx, s.prefix+secret).Err()
	if err != nil {
		slog.Error("Redis pending conn delete error", "error", err)
	}
	return err
}

// RedisRateLimitStore implements RateLimitStore using Redis
type RedisRateLimitStore struct {
	client *redis.Client
	prefix string
}

// NewRedisRateLimitStore creates a new Redis rate limit store
func NewRedisRateLimitStore(client *redis.Client, prefix string) *RedisRateLimitStore {
	return &RedisRateLimitStore{
		client: client,
		prefix: prefix + "ratelimit:",
	}
}

func (s *RedisRateLimitStore) Check(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	fullKey := s.prefix + key
	now := time.Now()
	windowStart := now.Add(-window)

	// Use a Lua script for atomic check-and-count
	// This removes old entries and counts remaining in one operation
	script := redis.NewScript(`
		local key = KEYS[1]
		local window_start = tonumber(ARGV[1])
		local limit = tonumber(ARGV[2])

		-- Remove old entries
		redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)

		-- Count current entries
		local count = redis.call('ZCARD', key)

		return count
	`)

	count, err := script.Run(ctx, s.client, []string{fullKey}, windowStart.UnixNano(), limit).Int()
	if err != nil {
		slog.Error("Redis rate limit check error", "error", err)
		// On error, allow the request (fail open)
		return true, limit, nil
	}

	remaining := limit - count
	if remaining < 0 {
		remaining = 0
	}

	return count < limit, remaining, nil
}

func (s *RedisRateLimitStore) Increment(ctx context.Context, key string, window time.Duration) error {
	fullKey := s.prefix + key
	now := time.Now()

	// Add current timestamp and set expiry
	pipe := s.client.Pipeline()
	pipe.ZAdd(ctx, fullKey, redis.Z{
		Score:  float64(now.UnixNano()),
		Member: now.UnixNano(),
	})
	pipe.Expire(ctx, fullKey, window+time.Minute) // Extra minute buffer for safety

	_, err := pipe.Exec(ctx)
	if err != nil {
		slog.Error("Redis rate limit increment error", "error", err)
	}
	return err
}

// RedisSearchCacheStore implements SearchCacheStore using Redis
type RedisSearchCacheStore struct {
	client *redis.Client
	prefix string
}

// NewRedisSearchCacheStore creates a new Redis search cache store
func NewRedisSearchCacheStore(client *redis.Client, prefix string) *RedisSearchCacheStore {
	return &RedisSearchCacheStore{
		client: client,
		prefix: prefix + "search:",
	}
}

func (s *RedisSearchCacheStore) makeKey(query string, kinds []int, limit int) string {
	kindsStr := ""
	for i, k := range kinds {
		if i > 0 {
			kindsStr += ","
		}
		kindsStr += fmt.Sprintf("%d", k)
	}
	return fmt.Sprintf("%s%s|%s|%d", s.prefix, query, kindsStr, limit)
}

func (s *RedisSearchCacheStore) Get(ctx context.Context, query string, kinds []int, limit int) ([]Event, bool, error) {
	key := s.makeKey(query, kinds, limit)

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		slog.Error("Redis search cache get error", "error", err)
		return nil, false, nil
	}

	var cached CachedSearchResult
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Error("Redis search cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return cached.Events, true, nil
}

func (s *RedisSearchCacheStore) Set(ctx context.Context, query string, kinds []int, limit int, events []Event, ttl time.Duration) error {
	key := s.makeKey(query, kinds, limit)

	cached := CachedSearchResult{
		Query:    query,
		Kinds:    kinds,
		Events:   events,
		CachedAt: time.Now().Unix(),
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}

	err = s.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		slog.Error("Redis search cache set error", "error", err)
	}
	return err
}

// RedisThreadCacheStore implements ThreadCacheStore using Redis
type RedisThreadCacheStore struct {
	client *redis.Client
	prefix string
}

// NewRedisThreadCacheStore creates a new Redis thread cache store
func NewRedisThreadCacheStore(client *redis.Client, prefix string) *RedisThreadCacheStore {
	return &RedisThreadCacheStore{
		client: client,
		prefix: prefix + "thread:",
	}
}

func (s *RedisThreadCacheStore) Get(ctx context.Context, rootEventID string) (*CachedThread, bool, error) {
	key := s.prefix + rootEventID

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		slog.Error("Redis thread cache get error", "error", err)
		return nil, false, nil
	}

	var cached CachedThread
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Error("Redis thread cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return &cached, true, nil
}

func (s *RedisThreadCacheStore) Set(ctx context.Context, rootEventID string, thread *CachedThread, ttl time.Duration) error {
	key := s.prefix + rootEventID

	data, err := json.Marshal(thread)
	if err != nil {
		return err
	}

	err = s.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		slog.Error("Redis thread cache set error", "error", err)
	}
	return err
}

// RedisNotificationReadStore implements NotificationReadStore using Redis
type RedisNotificationReadStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisNotificationReadStore creates a new Redis notification read store
func NewRedisNotificationReadStore(client *redis.Client, prefix string, ttl time.Duration) *RedisNotificationReadStore {
	return &RedisNotificationReadStore{
		client: client,
		prefix: prefix + "notif_read:",
		ttl:    ttl,
	}
}

func (s *RedisNotificationReadStore) GetLastRead(ctx context.Context, pubkey string) (int64, bool, error) {
	key := s.prefix + pubkey

	timestamp, err := s.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		slog.Error("Redis notification read get error", "error", err)
		return 0, false, nil // Graceful degradation
	}

	return timestamp, true, nil
}

func (s *RedisNotificationReadStore) SetLastRead(ctx context.Context, pubkey string, timestamp int64) error {
	key := s.prefix + pubkey

	err := s.client.Set(ctx, key, timestamp, s.ttl).Err()
	if err != nil {
		slog.Error("Redis notification read set error", "error", err)
	}
	return err
}

// RedisNotificationCacheStore implements NotificationCacheStore using Redis
type RedisNotificationCacheStore struct {
	client *redis.Client
	prefix string
}

// NewRedisNotificationCacheStore creates a new Redis notification cache store
func NewRedisNotificationCacheStore(client *redis.Client, prefix string) *RedisNotificationCacheStore {
	return &RedisNotificationCacheStore{
		client: client,
		prefix: prefix + "notif_cache:",
	}
}

func (s *RedisNotificationCacheStore) Get(ctx context.Context, pubkey string) (*CachedNotifications, bool, error) {
	key := s.prefix + pubkey

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		slog.Error("Redis notification cache get error", "error", err)
		return nil, false, nil
	}

	var cached CachedNotifications
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Error("Redis notification cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return &cached, true, nil
}

func (s *RedisNotificationCacheStore) Set(ctx context.Context, pubkey string, cached *CachedNotifications, ttl time.Duration) error {
	key := s.prefix + pubkey

	data, err := json.Marshal(cached)
	if err != nil {
		return err
	}

	err = s.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		slog.Error("Redis notification cache set error", "error", err)
	}
	return err
}

// RedisEventCache implements EventCacheStore using Redis
type RedisEventCache struct {
	client *redis.Client
	prefix string
}

// NewRedisEventCache creates a new Redis event cache
func NewRedisEventCache(client *redis.Client, prefix string) *RedisEventCache {
	return &RedisEventCache{
		client: client,
		prefix: prefix + "events:",
	}
}

func (c *RedisEventCache) Get(relays []string, filter Filter) ([]Event, bool, bool) {
	key := c.prefix + buildEventCacheKey(relays, filter)
	ctx := context.Background()

	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, false, false
	}
	if err != nil {
		slog.Debug("Redis event cache get error", "error", err)
		return nil, false, false
	}

	var cached CachedEventResult
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Debug("Redis event cache unmarshal error", "error", err)
		return nil, false, false
	}

	return cached.Events, cached.EOSE, true
}

func (c *RedisEventCache) Set(relays []string, filter Filter, events []Event, eose bool) {
	key := c.prefix + buildEventCacheKey(relays, filter)
	ttl := getEventTTL(filter)
	ctx := context.Background()

	cached := CachedEventResult{
		Events:   events,
		EOSE:     eose,
		CachedAt: time.Now().Unix(),
	}

	data, err := json.Marshal(cached)
	if err != nil {
		slog.Debug("Redis event cache marshal error", "error", err)
		return
	}

	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		slog.Debug("Redis event cache set error", "error", err)
	}
}

func (c *RedisEventCache) Close() {
	// Redis client is shared, don't close it here
}

// RedisDVMCacheStore implements DVMCacheStore using Redis
type RedisDVMCacheStore struct {
	client *redis.Client
	prefix string
}

// NewRedisDVMCacheStore creates a new Redis DVM cache store
func NewRedisDVMCacheStore(client *redis.Client, prefix string) *RedisDVMCacheStore {
	return &RedisDVMCacheStore{
		client: client,
		prefix: prefix + "dvm:",
	}
}

func (s *RedisDVMCacheStore) Get(ctx context.Context, key string) (*CachedDVMResult, bool, error) {
	data, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		slog.Debug("Redis DVM cache get error", "error", err)
		return nil, false, nil
	}

	var cached CachedDVMResult
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Debug("Redis DVM cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return &cached, true, nil
}

func (s *RedisDVMCacheStore) Set(ctx context.Context, key string, result *CachedDVMResult, ttl time.Duration) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}

	if err := s.client.Set(ctx, s.prefix+key, data, ttl).Err(); err != nil {
		slog.Debug("Redis DVM cache set error", "error", err)
		return err
	}
	return nil
}

// RedisDVMMetaCacheStore implements DVMMetaCacheStore using Redis
type RedisDVMMetaCacheStore struct {
	client *redis.Client
	prefix string
}

// NewRedisDVMMetaCacheStore creates a new Redis DVM metadata cache store
func NewRedisDVMMetaCacheStore(client *redis.Client, prefix string) *RedisDVMMetaCacheStore {
	return &RedisDVMMetaCacheStore{
		client: client,
		prefix: prefix + "dvm_meta:",
	}
}

func (s *RedisDVMMetaCacheStore) Get(ctx context.Context, key string) (*DVMMetadata, bool, error) {
	data, err := s.client.Get(ctx, s.prefix+key).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		slog.Debug("Redis DVM meta cache get error", "error", err)
		return nil, false, nil
	}

	var cached DVMMetadata
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Debug("Redis DVM meta cache unmarshal error", "error", err)
		return nil, false, nil
	}

	return &cached, true, nil
}

func (s *RedisDVMMetaCacheStore) Set(ctx context.Context, key string, meta *DVMMetadata, ttl time.Duration) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	if err := s.client.Set(ctx, s.prefix+key, data, ttl).Err(); err != nil {
		slog.Debug("Redis DVM meta cache set error", "error", err)
		return err
	}
	return nil
}

// RedisNIP05Cache implements NIP-05 caching using Redis
type RedisNIP05Cache struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewRedisNIP05Cache creates a new Redis NIP-05 cache
func NewRedisNIP05Cache(client *redis.Client, prefix string, ttl time.Duration) *RedisNIP05Cache {
	return &RedisNIP05Cache{
		client: client,
		prefix: prefix + "nip05:",
		ttl:    ttl,
	}
}

func (c *RedisNIP05Cache) Get(identifier string) (*NIP05Result, bool) {
	ctx := context.Background()
	data, err := c.client.Get(ctx, c.prefix+identifier).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		slog.Debug("Redis NIP-05 cache get error", "error", err)
		return nil, false
	}

	var cached NIP05Result
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Debug("Redis NIP-05 cache unmarshal error", "error", err)
		return nil, false
	}

	return &cached, true
}

func (c *RedisNIP05Cache) Set(identifier string, result *NIP05Result) {
	ctx := context.Background()
	data, err := json.Marshal(result)
	if err != nil {
		slog.Debug("Redis NIP-05 cache marshal error", "error", err)
		return
	}

	if err := c.client.Set(ctx, c.prefix+identifier, data, c.ttl).Err(); err != nil {
		slog.Debug("Redis NIP-05 cache set error", "error", err)
	}
}

// RedisRelayHealth implements relay health tracking using Redis
type RedisRelayHealth struct {
	client *redis.Client
	prefix string
}

// NewRedisRelayHealth creates a new Redis relay health tracker
func NewRedisRelayHealth(client *redis.Client, prefix string) *RedisRelayHealth {
	return &RedisRelayHealth{
		client: client,
		prefix: prefix + "relay_health:",
	}
}

type redisRelayStats struct {
	AvgResponseTimeMs int64 `json:"avg_ms"`
	ResponseCount     int   `json:"count"`
	LastResponse      int64 `json:"last"`
	FailureCount      int   `json:"failures"`
	BackoffUntil      int64 `json:"backoff_until"`
}

func (h *RedisRelayHealth) getStats(relayURL string) *redisRelayStats {
	ctx := context.Background()
	data, err := h.client.Get(ctx, h.prefix+relayURL).Bytes()
	if err != nil {
		return &redisRelayStats{}
	}

	var stats redisRelayStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return &redisRelayStats{}
	}
	return &stats
}

func (h *RedisRelayHealth) setStats(relayURL string, stats *redisRelayStats) {
	ctx := context.Background()
	data, err := json.Marshal(stats)
	if err != nil {
		return
	}
	// Keep stats for 1 hour
	h.client.Set(ctx, h.prefix+relayURL, data, 1*time.Hour)
}

func (h *RedisRelayHealth) shouldSkip(relayURL string) bool {
	stats := h.getStats(relayURL)
	return stats.BackoffUntil > 0 && time.Now().Unix() < stats.BackoffUntil
}

func (h *RedisRelayHealth) recordFailure(relayURL string) {
	stats := h.getStats(relayURL)
	stats.FailureCount++

	var backoff time.Duration
	switch {
	case stats.FailureCount <= 1:
		backoff = 30 * time.Second
	case stats.FailureCount == 2:
		backoff = 60 * time.Second
	case stats.FailureCount == 3:
		backoff = 2 * time.Minute
	default:
		backoff = 5 * time.Minute
	}

	stats.BackoffUntil = time.Now().Add(backoff).Unix()
	h.setStats(relayURL, stats)

	slog.Warn("relay connection failed",
		"relay", relayURL,
		"failure_count", stats.FailureCount,
		"backoff_until", time.Unix(stats.BackoffUntil, 0).Format("15:04:05"))
}

func (h *RedisRelayHealth) recordSuccess(relayURL string) {
	stats := h.getStats(relayURL)
	stats.FailureCount = 0
	stats.BackoffUntil = 0
	h.setStats(relayURL, stats)
}

func (h *RedisRelayHealth) recordResponseTime(relayURL string, duration time.Duration) {
	stats := h.getStats(relayURL)

	// Exponential moving average (alpha=0.3)
	durationMs := duration.Milliseconds()
	if stats.ResponseCount == 0 {
		stats.AvgResponseTimeMs = durationMs
	} else {
		alpha := 0.3
		stats.AvgResponseTimeMs = int64(alpha*float64(durationMs) + (1-alpha)*float64(stats.AvgResponseTimeMs))
	}

	stats.ResponseCount++
	stats.LastResponse = time.Now().Unix()
	h.setStats(relayURL, stats)
}

func (h *RedisRelayHealth) getAverageResponseTime(relayURL string) time.Duration {
	stats := h.getStats(relayURL)
	if stats.ResponseCount == 0 {
		return 1 * time.Second
	}
	return time.Duration(stats.AvgResponseTimeMs) * time.Millisecond
}

func (h *RedisRelayHealth) getRelayScore(relayURL string) int {
	stats := h.getStats(relayURL)
	score := 50

	if stats.ResponseCount > 0 {
		switch {
		case stats.AvgResponseTimeMs < 200:
			score = 50
		case stats.AvgResponseTimeMs < 500:
			score = 40
		case stats.AvgResponseTimeMs < 1000:
			score = 25
		default:
			score = 10
		}

		bonus := stats.ResponseCount
		if bonus > 10 {
			bonus = 10
		}
		score += bonus
	}

	penalty := stats.FailureCount * 10
	if penalty > 30 {
		penalty = 30
	}
	score -= penalty

	if stats.BackoffUntil > 0 && time.Now().Unix() < stats.BackoffUntil {
		score -= 20
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

func (h *RedisRelayHealth) SortRelaysByScore(relays []string) []string {
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

func (h *RedisRelayHealth) GetExpectedResponseTime(relays []string, minRelays int) time.Duration {
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

func (h *RedisRelayHealth) GetRelayHealthStats() (healthy int, unhealthy int, avgResponseMs int64) {
	ctx := context.Background()
	// Get all relay health keys
	keys, err := h.client.Keys(ctx, h.prefix+"*").Result()
	if err != nil {
		return 0, 0, 0
	}

	var totalMs int64
	var count int

	for _, key := range keys {
		relayURL := key[len(h.prefix):]
		stats := h.getStats(relayURL)
		if stats.ResponseCount > 0 {
			if stats.FailureCount > 0 {
				unhealthy++
			} else {
				healthy++
			}
			totalMs += stats.AvgResponseTimeMs
			count++
		}
	}

	if count > 0 {
		avgResponseMs = totalMs / int64(count)
	}

	return healthy, unhealthy, avgResponseMs
}

func (h *RedisRelayHealth) GetRelayHealthDetails() []RelayHealthDetail {
	ctx := context.Background()
	// Get all relay health keys
	keys, err := h.client.Keys(ctx, h.prefix+"*").Result()
	if err != nil {
		return nil
	}

	details := make([]RelayHealthDetail, 0, len(keys))
	for _, key := range keys {
		relayURL := key[len(h.prefix):]
		stats := h.getStats(relayURL)
		if stats.ResponseCount > 0 {
			status := "healthy"
			if stats.FailureCount > 0 {
				status = "unhealthy"
			}
			details = append(details, RelayHealthDetail{
				URL:           relayURL,
				Status:        status,
				AvgResponseMs: stats.AvgResponseTimeMs,
				RequestCount:  int64(stats.ResponseCount),
			})
		}
	}
	return details
}

// --- Redis LNURL Cache ---

// RedisLNURLCache implements LNURLCacheStore using Redis
type RedisLNURLCache struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// cachedLNURLInfo stores LNURL pay info with not-found state
type cachedLNURLInfo struct {
	Info     *LNURLPayInfo `json:"info"`
	NotFound bool          `json:"not_found"`
}

// NewRedisLNURLCache creates a new Redis LNURL cache
func NewRedisLNURLCache(client *redis.Client, prefix string, ttl time.Duration) *RedisLNURLCache {
	return &RedisLNURLCache{
		client: client,
		prefix: prefix + "lnurl:",
		ttl:    ttl,
	}
}

// Get retrieves cached LNURL pay info
func (c *RedisLNURLCache) Get(pubkey string) (*LNURLPayInfo, bool) {
	ctx := context.Background()
	data, err := c.client.Get(ctx, c.prefix+pubkey).Bytes()
	if err == redis.Nil {
		return nil, false
	}
	if err != nil {
		slog.Debug("Redis LNURL cache get error", "error", err)
		return nil, false
	}

	var cached cachedLNURLInfo
	if err := json.Unmarshal(data, &cached); err != nil {
		slog.Debug("Redis LNURL cache unmarshal error", "error", err)
		return nil, false
	}

	// Return nil for "not found" entries but still return true (was cached)
	if cached.NotFound {
		return nil, true
	}

	return cached.Info, true
}

// Set stores LNURL pay info in the cache
func (c *RedisLNURLCache) Set(pubkey string, info *LNURLPayInfo) {
	ctx := context.Background()
	cached := cachedLNURLInfo{
		Info:     info,
		NotFound: false,
	}
	data, err := json.Marshal(cached)
	if err != nil {
		slog.Debug("Redis LNURL cache marshal error", "error", err)
		return
	}

	if err := c.client.Set(ctx, c.prefix+pubkey, data, c.ttl).Err(); err != nil {
		slog.Debug("Redis LNURL cache set error", "error", err)
	}
}

// SetNotFound marks a pubkey as having no LNURL
func (c *RedisLNURLCache) SetNotFound(pubkey string) {
	ctx := context.Background()
	cached := cachedLNURLInfo{
		Info:     nil,
		NotFound: true,
	}
	data, err := json.Marshal(cached)
	if err != nil {
		slog.Debug("Redis LNURL cache marshal error", "error", err)
		return
	}

	if err := c.client.Set(ctx, c.prefix+pubkey, data, c.ttl).Err(); err != nil {
		slog.Debug("Redis LNURL cache set error", "error", err)
	}
}
