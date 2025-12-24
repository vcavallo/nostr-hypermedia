package main

import (
	"log/slog"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"nostr-server/internal/types"
	"nostr-server/internal/util"
)

// Singleflight groups for deduplicating concurrent requests.
// When multiple goroutines request the same data simultaneously,
// only one actually fetches while others wait and share the result.
var (
	relayListGroup   singleflight.Group
	reactionsGroup   singleflight.Group
	replyCountsGroup singleflight.Group
)

// buildBatchKey creates a stable key for singleflight deduplication.
// Sorts both slices to ensure identical batches produce identical keys.
func buildBatchKey(prefix string, relays, ids []string) string {
	sortedRelays := util.SortedCopy(relays)
	sortedIDs := util.SortedCopy(ids)
	return prefix + ":" + strings.Join(sortedRelays, "|") + ":" + strings.Join(sortedIDs, ",")
}

// fetchRelayList fetches a relay list with singleflight deduplication.
// If multiple goroutines request the same pubkey simultaneously,
// only one fetch occurs and all share the result.
func fetchRelayList(pubkey string) *types.RelayList {
	// Check cache first (avoid singleflight overhead for cache hits)
	if relayList, notFound, ok := relayListCache.Get(pubkey); ok {
		IncrementCacheHit()
		if notFound {
			return nil
		}
		return relayList
	}

	// Use singleflight for cache misses
	result, _, shared := relayListGroup.Do(pubkey, func() (interface{}, error) {
		return fetchRelayListDirect(pubkey), nil
	})

	if shared {
		slog.Debug("singleflight: shared relay list fetch", "pubkey", shortID(pubkey))
	}

	if result == nil {
		return nil
	}
	return result.(*types.RelayList)
}

// fetchRelayLists fetches relay lists for multiple pubkeys with singleflight.
// Uses per-pubkey singleflight to deduplicate overlapping concurrent requests.
func fetchRelayLists(pubkeys []string) map[string]*types.RelayList {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := relayListCache.GetMultiple(pubkeys)
	if len(missing) == 0 {
		IncrementCacheHit()
		return cached
	}

	IncrementCacheMiss()

	// Fetch missing pubkeys with per-pubkey singleflight (parallel)
	var wg sync.WaitGroup
	var mu sync.Mutex
	freshLists := make(map[string]*types.RelayList)

	for _, pk := range missing {
		wg.Add(1)
		go func(pubkey string) {
			defer wg.Done()
			relayList := fetchRelayList(pubkey)
			if relayList != nil {
				mu.Lock()
				freshLists[pubkey] = relayList
				mu.Unlock()
			}
		}(pk)
	}
	wg.Wait()

	// Merge results
	result := make(map[string]*types.RelayList, len(cached)+len(freshLists))
	for pk, rl := range cached {
		result[pk] = rl
	}
	for pk, rl := range freshLists {
		result[pk] = rl
	}

	return result
}

// fetchProfiles fetches kind 0 profiles with singleflight deduplication.
func fetchProfiles(relays []string, pubkeys []string) map[string]*ProfileInfo {
	return fetchProfilesWithOptions(relays, pubkeys, false)
}

// fetchProfilesWithOptions fetches profiles with request coalescing.
// Uses a batcher to collect requests over a time window and merge overlapping keys.
// This is more efficient than singleflight for overlapping (not just identical) requests.
func fetchProfilesWithOptions(relays []string, pubkeys []string, cacheOnly bool) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := profileCache.GetMultiple(pubkeys)
	if len(missing) == 0 {
		IncrementCacheHit()
		return cached
	}

	if cacheOnly {
		IncrementCacheMiss()
		return cached
	}

	IncrementCacheMiss()

	// Use batcher for cache misses - it will collect and merge with other concurrent requests
	var freshProfiles map[string]*ProfileInfo
	if profileBatcher != nil {
		freshProfiles = profileBatcher.GetMultiple(missing)
	} else {
		// Fallback if batcher not initialized
		freshProfiles = fetchProfilesWithOptionsDirect(relays, missing, false)
	}

	// Merge cached and fresh
	finalResult := make(map[string]*ProfileInfo, len(cached)+len(freshProfiles))
	for pk, p := range cached {
		finalResult[pk] = p
	}
	for pk, p := range freshProfiles {
		finalResult[pk] = p
	}

	return finalResult
}

// fetchReactions fetches reactions with singleflight deduplication.
func fetchReactions(relays []string, eventIDs []string) map[string]*ReactionsSummary {
	if len(eventIDs) == 0 {
		return nil
	}

	batchKey := buildBatchKey("reactions", relays, eventIDs)

	result, _, shared := reactionsGroup.Do(batchKey, func() (interface{}, error) {
		return fetchReactionsDirect(relays, eventIDs), nil
	})

	if shared {
		slog.Debug("singleflight: shared reactions fetch", "count", len(eventIDs))
	}

	return result.(map[string]*ReactionsSummary)
}

// fetchReplyCounts fetches reply counts with singleflight deduplication.
func fetchReplyCounts(relays []string, eventIDs []string) map[string]int {
	if len(eventIDs) == 0 {
		return nil
	}

	batchKey := buildBatchKey("replies", relays, eventIDs)

	result, _, shared := replyCountsGroup.Do(batchKey, func() (interface{}, error) {
		return fetchReplyCountsDirect(relays, eventIDs), nil
	})

	if shared {
		slog.Debug("singleflight: shared reply counts fetch", "count", len(eventIDs))
	}

	return result.(map[string]int)
}
