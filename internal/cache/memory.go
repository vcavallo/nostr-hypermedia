package cache

import (
	"context"
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
