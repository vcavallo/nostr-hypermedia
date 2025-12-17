package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
