package main

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"nostr-server/internal/config"
)

// SubscriptionAggregator maintains persistent subscriptions to aggregator relays
// and streams events into a local cache. This reduces per-request relay queries
// by keeping a warm cache of recent global timeline events.
type SubscriptionAggregator struct {
	mu            sync.RWMutex
	events        []Event          // Recent events (sorted by CreatedAt desc)
	eventIndex    map[string]bool  // Event ID -> exists (for deduplication)
	maxEvents     int              // Maximum events to keep
	lastEventTime int64            // Timestamp of most recent event

	ctx        context.Context
	cancel     context.CancelFunc
	running    bool
	eventChan  chan Event
}

// AggregatorConfig holds configuration for the subscription aggregator
type AggregatorConfig struct {
	MaxEvents      int           // Maximum events to keep in memory (default: 500)
	Kinds          []int         // Event kinds to subscribe to (default: [1])
	ReconnectDelay time.Duration // Delay before reconnecting on disconnect (default: 5s)
}

// DefaultAggregatorConfig returns sensible defaults
func DefaultAggregatorConfig() AggregatorConfig {
	return AggregatorConfig{
		MaxEvents:      500,
		Kinds:          []int{1}, // Notes only for global timeline
		ReconnectDelay: 5 * time.Second,
	}
}

// Global aggregator instance
var (
	globalAggregator     *SubscriptionAggregator
	aggregatorOnce       sync.Once
	aggregatorConfig     AggregatorConfig
)

// InitAggregator initializes the global subscription aggregator.
// Call this after relay pool is initialized.
func InitAggregator() {
	aggregatorOnce.Do(func() {
		aggregatorConfig = DefaultAggregatorConfig()
		globalAggregator = NewSubscriptionAggregator(aggregatorConfig)
		globalAggregator.Start()
		slog.Info("subscription aggregator initialized",
			"max_events", aggregatorConfig.MaxEvents,
			"kinds", aggregatorConfig.Kinds)
	})
}

// StopAggregator stops the global subscription aggregator.
// Call this during server shutdown.
func StopAggregator() {
	if globalAggregator != nil {
		globalAggregator.Stop()
	}
}

// GetAggregatedEvents returns recent events from the aggregator cache.
// Returns events matching the filter, up to the specified limit.
// If no events match, returns nil (caller should fall back to relay fetch).
func GetAggregatedEvents(filter Filter) []Event {
	if globalAggregator == nil {
		return nil
	}
	return globalAggregator.GetEvents(filter)
}

// GetAggregatorStats returns statistics about the aggregator
func GetAggregatorStats() (eventCount int, lastEventTime int64, running bool) {
	if globalAggregator == nil {
		return 0, 0, false
	}
	return globalAggregator.Stats()
}

// NewSubscriptionAggregator creates a new aggregator with the given config
func NewSubscriptionAggregator(cfg AggregatorConfig) *SubscriptionAggregator {
	return &SubscriptionAggregator{
		events:     make([]Event, 0, cfg.MaxEvents),
		eventIndex: make(map[string]bool),
		maxEvents:  cfg.MaxEvents,
		eventChan:  make(chan Event, 100),
	}
}

// Start begins the aggregator's background subscription loop
func (a *SubscriptionAggregator) Start() {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.running = true
	a.mu.Unlock()

	go a.processEvents()
	go a.subscriptionLoop()
}

// Stop halts the aggregator
func (a *SubscriptionAggregator) Stop() {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return
	}
	a.running = false
	a.cancel()
	a.mu.Unlock()

	slog.Info("subscription aggregator stopped")
}

// Stats returns aggregator statistics
func (a *SubscriptionAggregator) Stats() (eventCount int, lastEventTime int64, running bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.events), a.lastEventTime, a.running
}

// GetEvents returns events matching the filter from the aggregator cache
func (a *SubscriptionAggregator) GetEvents(filter Filter) []Event {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.events) == 0 {
		return nil
	}

	// Check if filter is compatible with aggregator
	// Aggregator only handles global timeline (no authors filter)
	if len(filter.Authors) > 0 {
		return nil // Not a global timeline request
	}

	// Filter kinds
	kindSet := make(map[int]bool)
	for _, k := range aggregatorConfig.Kinds {
		kindSet[k] = true
	}

	// Check if requested kinds are subset of aggregator kinds
	if len(filter.Kinds) > 0 {
		for _, k := range filter.Kinds {
			if !kindSet[k] {
				return nil // Requested kind not aggregated
			}
		}
		// Use requested kinds as filter
		kindSet = make(map[int]bool)
		for _, k := range filter.Kinds {
			kindSet[k] = true
		}
	}

	// Apply limit
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}

	// Filter and collect events
	result := make([]Event, 0, limit)
	for _, evt := range a.events {
		// Kind filter
		if !kindSet[evt.Kind] {
			continue
		}

		// Since filter
		if filter.Since != nil && evt.CreatedAt < *filter.Since {
			continue
		}

		// Until filter
		if filter.Until != nil && evt.CreatedAt > *filter.Until {
			continue
		}

		// Skip replies (same as timeline logic)
		if isReply(evt) && evt.Kind != 6 {
			continue
		}

		result = append(result, evt)
		if len(result) >= limit {
			break
		}
	}

	if len(result) == 0 {
		return nil
	}

	slog.Debug("aggregator: serving from cache", "count", len(result), "limit", limit)
	return result
}

// processEvents processes incoming events from the subscription
func (a *SubscriptionAggregator) processEvents() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case evt := <-a.eventChan:
			a.addEvent(evt)
		}
	}
}

// addEvent adds an event to the cache, maintaining sort order and size limits
func (a *SubscriptionAggregator) addEvent(evt Event) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Deduplicate
	if a.eventIndex[evt.ID] {
		return
	}

	// Add to index
	a.eventIndex[evt.ID] = true

	// Insert in sorted order (newest first)
	idx := sort.Search(len(a.events), func(i int) bool {
		return a.events[i].CreatedAt < evt.CreatedAt
	})

	// Insert at idx
	a.events = append(a.events, Event{})
	copy(a.events[idx+1:], a.events[idx:])
	a.events[idx] = evt

	// Update last event time
	if evt.CreatedAt > a.lastEventTime {
		a.lastEventTime = evt.CreatedAt
	}

	// Enforce max size
	if len(a.events) > a.maxEvents {
		// Remove oldest (last element)
		oldest := a.events[len(a.events)-1]
		a.events = a.events[:len(a.events)-1]
		delete(a.eventIndex, oldest.ID)
	}
}

// subscriptionLoop maintains persistent subscriptions to aggregator relays
func (a *SubscriptionAggregator) subscriptionLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		default:
			a.runSubscription()
			// Wait before reconnecting
			select {
			case <-a.ctx.Done():
				return
			case <-time.After(aggregatorConfig.ReconnectDelay):
			}
		}
	}
}

// runSubscription creates and maintains subscriptions to default relays
func (a *SubscriptionAggregator) runSubscription() {
	relays := config.GetDefaultRelays()
	if len(relays) == 0 {
		slog.Warn("aggregator: no default relays configured")
		return
	}

	// Create subscription context
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	// Track seen events to avoid duplicates across relays
	seenIDs := make(map[string]bool)
	seenMu := sync.Mutex{}

	// Channel for events from relays
	eventChan := make(chan Event, 100)
	eoseChan := make(chan string, len(relays))

	// Subscribe to each relay
	var wg sync.WaitGroup
	for _, relay := range relays {
		wg.Add(1)
		go func(relayURL string) {
			defer wg.Done()
			a.subscribeToRelay(ctx, relayURL, eventChan, eoseChan)
		}(relay)
	}

	// Close channels when all relay goroutines complete
	go func() {
		wg.Wait()
		close(eventChan)
		close(eoseChan)
	}()

	eoseCount := 0
	slog.Debug("aggregator: starting subscriptions", "relays", len(relays))

	// Event loop
	for {
		select {
		case <-ctx.Done():
			return

		case evt, ok := <-eventChan:
			if !ok {
				slog.Debug("aggregator: event channel closed, reconnecting")
				return
			}

			// Deduplicate across relays
			seenMu.Lock()
			if seenIDs[evt.ID] {
				seenMu.Unlock()
				continue
			}
			seenIDs[evt.ID] = true
			seenMu.Unlock()

			// Send to processing goroutine
			select {
			case a.eventChan <- evt:
			default:
				// Channel full, skip event
			}

		case relayURL := <-eoseChan:
			eoseCount++
			slog.Debug("aggregator: EOSE received", "relay", relayURL, "count", eoseCount, "total", len(relays))
		}
	}
}

// subscribeToRelay creates a subscription to a single relay
func (a *SubscriptionAggregator) subscribeToRelay(ctx context.Context, relayURL string, eventChan chan<- Event, eoseChan chan<- string) {
	subID := "agg-" + randomString(8)
	reqFilter := map[string]interface{}{
		"kinds": aggregatorConfig.Kinds,
		"limit": 100, // Initial batch
	}

	sub, err := relayPool.Subscribe(ctx, relayURL, subID, reqFilter)
	if err != nil {
		slog.Debug("aggregator: failed to subscribe", "relay", relayURL, "error", err)
		return
	}
	defer relayPool.Unsubscribe(relayURL, sub)

	slog.Debug("aggregator: subscribed", "relay", relayURL, "sub_id", subID)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.Done:
			slog.Debug("aggregator: subscription closed", "relay", relayURL)
			return
		case evt := <-sub.EventChan:
			select {
			case eventChan <- evt:
			case <-ctx.Done():
				return
			}
		case <-sub.EOSEChan:
			eoseChan <- relayURL
			// Continue listening after EOSE
		}
	}
}
