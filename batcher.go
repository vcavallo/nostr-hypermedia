package main

import (
	"log/slog"
	"sync"
	"time"
)

// Batcher collects requests over a time window and executes them in batches.
// This provides better deduplication than singleflight for overlapping (not just identical) requests.
//
// Example: Three concurrent requests for profiles [a,b,c], [a,d], [b,e]
// - Singleflight: 3 separate relay queries (different batch keys)
// - Batcher: 1 relay query for [a,b,c,d,e] (merged and deduplicated)
type Batcher[V any] struct {
	name      string
	batchFn   func(keys []string) map[string]V
	window    time.Duration
	maxBatch  int

	mu       sync.Mutex
	pending  map[string][]*batchWaiter[V]
	timer    *time.Timer
	timerSet bool
}

// batchWaiter represents a caller waiting for results
type batchWaiter[V any] struct {
	keys   []string
	result chan map[string]V
}

// NewBatcher creates a new batcher with the given configuration.
//
// Parameters:
//   - name: Identifier for logging
//   - batchFn: Function to fetch values for a batch of keys
//   - window: Time to wait before executing a batch (e.g., 50ms)
//   - maxBatch: Maximum keys per batch (0 = unlimited)
func NewBatcher[V any](name string, batchFn func(keys []string) map[string]V, window time.Duration, maxBatch int) *Batcher[V] {
	return &Batcher[V]{
		name:     name,
		batchFn:  batchFn,
		window:   window,
		maxBatch: maxBatch,
		pending:  make(map[string][]*batchWaiter[V]),
	}
}

// Get fetches a single value, batching with other concurrent requests.
func (b *Batcher[V]) Get(key string) V {
	result := b.GetMultiple([]string{key})
	return result[key]
}

// GetMultiple fetches multiple values, batching with other concurrent requests.
// Returns a map of key -> value for all requested keys.
func (b *Batcher[V]) GetMultiple(keys []string) map[string]V {
	if len(keys) == 0 {
		return nil
	}

	// Create waiter for this request
	waiter := &batchWaiter[V]{
		keys:   keys,
		result: make(chan map[string]V, 1),
	}

	b.mu.Lock()

	// Add keys to pending map
	for _, key := range keys {
		b.pending[key] = append(b.pending[key], waiter)
	}

	// Start timer if not already running
	if !b.timerSet {
		b.timerSet = true
		b.timer = time.AfterFunc(b.window, b.executeBatch)
	}

	// Check if we've hit max batch size
	if b.maxBatch > 0 && len(b.pending) >= b.maxBatch {
		b.timer.Stop()
		b.mu.Unlock()
		b.executeBatch()
	} else {
		b.mu.Unlock()
	}

	// Wait for result
	result := <-waiter.result
	return result
}

// executeBatch runs the batch function and distributes results to waiters.
func (b *Batcher[V]) executeBatch() {
	b.mu.Lock()

	// Collect all pending keys
	keys := make([]string, 0, len(b.pending))
	for key := range b.pending {
		keys = append(keys, key)
	}

	// Collect all waiters (deduplicated)
	waiterSet := make(map[*batchWaiter[V]]bool)
	for _, waiters := range b.pending {
		for _, w := range waiters {
			waiterSet[w] = true
		}
	}

	// Reset state
	b.pending = make(map[string][]*batchWaiter[V])
	b.timerSet = false

	b.mu.Unlock()

	if len(keys) == 0 {
		return
	}

	// Execute batch function
	slog.Debug("batcher: executing batch",
		"name", b.name,
		"keys", len(keys),
		"waiters", len(waiterSet))

	results := b.batchFn(keys)

	// Distribute results to each waiter
	for waiter := range waiterSet {
		// Build result map for this specific waiter (only their requested keys)
		waiterResult := make(map[string]V, len(waiter.keys))
		for _, key := range waiter.keys {
			if val, ok := results[key]; ok {
				waiterResult[key] = val
			}
		}
		waiter.result <- waiterResult
	}
}

// Stats returns current batcher statistics
func (b *Batcher[V]) Stats() (pendingKeys int, pendingWaiters int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	waiterSet := make(map[*batchWaiter[V]]bool)
	for _, waiters := range b.pending {
		for _, w := range waiters {
			waiterSet[w] = true
		}
	}

	return len(b.pending), len(waiterSet)
}

// BatcherConfig holds configuration for creating batchers
type BatcherConfig struct {
	Window   time.Duration
	MaxBatch int
}

// DefaultBatcherConfig returns sensible defaults for batching
func DefaultBatcherConfig() BatcherConfig {
	return BatcherConfig{
		Window:   50 * time.Millisecond, // Collect requests for 50ms
		MaxBatch: 100,                   // Max 100 keys per batch
	}
}

// Global batchers for different data types
var (
	profileBatcher     *Batcher[*ProfileInfo]
	reactionsBatcher   *Batcher[*ReactionsSummary]
	replyCountsBatcher *Batcher[int]

	batchersInitOnce sync.Once
)

// InitBatchers initializes the global batchers.
// Must be called after cache is initialized.
func InitBatchers() {
	batchersInitOnce.Do(func() {
		config := DefaultBatcherConfig()

		// Profile batcher - wraps the direct fetch function
		profileBatcher = NewBatcher(
			"profiles",
			func(keys []string) map[string]*ProfileInfo {
				// Use empty relays - the direct function will use profileRelays as fallback
				return fetchProfilesWithOptionsDirect(nil, keys, false)
			},
			config.Window,
			config.MaxBatch,
		)

		slog.Info("batchers initialized",
			"window_ms", config.Window.Milliseconds(),
			"max_batch", config.MaxBatch)
	})
}

// GetProfilesBatched fetches profiles using the batcher.
// This should be called instead of fetchProfilesWithOptions for optimal batching.
func GetProfilesBatched(keys []string) map[string]*ProfileInfo {
	if profileBatcher == nil {
		// Fallback if batchers not initialized
		return fetchProfilesWithOptionsDirect(nil, keys, false)
	}
	return profileBatcher.GetMultiple(keys)
}
