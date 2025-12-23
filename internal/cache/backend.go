package cache

import (
	"context"
	"time"
)

// CacheBackend defines the interface for cache implementations
type CacheBackend interface {
	// Get retrieves a value from the cache
	// Returns (value, found, error)
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// Set stores a value in the cache with the given TTL
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete removes a value from the cache
	Delete(ctx context.Context, key string) error

	// GetMultiple retrieves multiple values from the cache
	// Returns a map of found keys to values
	GetMultiple(ctx context.Context, keys []string) (map[string][]byte, error)

	// SetMultiple stores multiple values with the given TTL
	SetMultiple(ctx context.Context, items map[string][]byte, ttl time.Duration) error

	// Close closes the cache connection
	Close() error
}
