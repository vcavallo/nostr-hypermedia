package main

import (
	"nostr-server/internal/util"
	"sync"
)

// =============================================================================
// Outbox Relay Helpers
// =============================================================================

// buildOutboxRelays fetches relay lists for the given pubkeys and returns
// a combined list of write relays, limited to maxPerAuthor relays per author.
// Relays are sorted by health score before limiting.
func buildOutboxRelays(pubkeys []string, maxPerAuthor int) []string {
	if len(pubkeys) == 0 || maxPerAuthor <= 0 {
		return nil
	}

	relayLists := fetchRelayLists(pubkeys)
	var outboxRelays []string

	for _, rl := range relayLists {
		if rl != nil && len(rl.Write) > 0 {
			writeRelays := relayHealthStore.SortRelaysByScore(rl.Write)
			writeRelays = util.LimitSlice(writeRelays, maxPerAuthor)
			outboxRelays = append(outboxRelays, writeRelays...)
		}
	}

	return outboxRelays
}

// buildOutboxRelaysForPubkey fetches the relay list for a single pubkey and
// returns its write relays, limited to maxRelays and sorted by health score.
func buildOutboxRelaysForPubkey(pubkey string, maxRelays int) []string {
	if pubkey == "" || maxRelays <= 0 {
		return nil
	}

	relayList := fetchRelayList(pubkey)
	if relayList == nil || len(relayList.Write) == 0 {
		return nil
	}

	writeRelays := relayHealthStore.SortRelaysByScore(relayList.Write)
	return util.LimitSlice(writeRelays, maxRelays)
}

// =============================================================================
// Pubkey Collection Helpers
// =============================================================================

// addMentionedPubkeysToSet extracts pubkeys mentioned in the content strings
// (nostr: references) and adds them to the pubkey set.
func addMentionedPubkeysToSet(contents []string, pubkeySet map[string]bool) {
	mentionedPubkeys := ExtractMentionedPubkeys(contents)
	for _, pk := range mentionedPubkeys {
		pubkeySet[pk] = true
	}
}

// collectEventPubkeys extracts the author pubkey from an event and adds it
// to the pubkey set. Also extracts content for mention extraction if the
// contents slice is provided.
func collectEventPubkeys(evt *Event, pubkeySet map[string]bool, contents *[]string) {
	pubkeySet[evt.PubKey] = true
	if contents != nil {
		*contents = append(*contents, evt.Content)
	}
}

// =============================================================================
// Event Collection Helpers
// =============================================================================

// CollectEventData extracts pubkeys (including mentioned pubkeys) and event IDs from events.
// Returns pubkeys slice and eventIDs slice. Use for timeline/search result processing.
func CollectEventData(events []Event) (pubkeys []string, eventIDs []string) {
	pubkeySet := make(map[string]bool, len(events))
	eventIDs = make([]string, 0, len(events))
	contents := make([]string, 0, len(events))

	for i := range events {
		collectEventPubkeys(&events[i], pubkeySet, &contents)
		eventIDs = append(eventIDs, events[i].ID)
	}

	addMentionedPubkeysToSet(contents, pubkeySet)
	pubkeys = util.MapKeys(pubkeySet)
	return pubkeys, eventIDs
}

// CollectEventPubkeys extracts only pubkeys (including mentioned) from events.
// Use when you don't need event IDs.
func CollectEventPubkeys(events []Event) []string {
	pubkeySet := make(map[string]bool, len(events))
	contents := make([]string, 0, len(events))

	for i := range events {
		collectEventPubkeys(&events[i], pubkeySet, &contents)
	}

	addMentionedPubkeysToSet(contents, pubkeySet)
	return util.MapKeys(pubkeySet)
}

// =============================================================================
// Event Conversion Helpers
// =============================================================================

// EventsToItems converts Event slice to EventItem slice with enrichment data.
// Use for preparing events for template rendering.
func EventsToItems(events []Event, profiles map[string]*ProfileInfo,
	reactions map[string]*ReactionsSummary, replyCounts map[string]int) []EventItem {
	items := make([]EventItem, len(events))
	for i, evt := range events {
		items[i] = EventItem{
			ID:            evt.ID,
			Kind:          evt.Kind,
			Pubkey:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Content:       evt.Content,
			Tags:          evt.Tags,
			Sig:           evt.Sig,
			RelaysSeen:    evt.RelaysSeen,
			AuthorProfile: profiles[evt.PubKey],
			Reactions:     reactions[evt.ID],
			ReplyCount:    replyCounts[evt.ID],
		}
	}
	return items
}

// EventsToItemsWithProfile converts Event slice to EventItem slice using a single profile.
// Use for profile pages where all events are from the same author.
func EventsToItemsWithProfile(events []Event, profile *ProfileInfo) []EventItem {
	items := make([]EventItem, len(events))
	for i, evt := range events {
		items[i] = EventItem{
			ID:            evt.ID,
			Kind:          evt.Kind,
			Pubkey:        evt.PubKey,
			CreatedAt:     evt.CreatedAt,
			Content:       evt.Content,
			Tags:          evt.Tags,
			Sig:           evt.Sig,
			RelaysSeen:    evt.RelaysSeen,
			AuthorProfile: profile,
		}
	}
	return items
}

// =============================================================================
// Event Enrichment Helpers
// =============================================================================

// EventEnrichment holds fetched enrichment data for events.
type EventEnrichment struct {
	Profiles    map[string]*ProfileInfo
	Reactions   map[string]*ReactionsSummary
	ReplyCounts map[string]int
}

// FetchEventEnrichment fetches profiles, reactions, and reply counts in parallel.
// Use cacheOnly=true to skip network requests (for append operations).
func FetchEventEnrichment(relays, pubkeys, eventIDs []string, cacheOnly bool) EventEnrichment {
	result := EventEnrichment{
		Profiles:    make(map[string]*ProfileInfo),
		Reactions:   make(map[string]*ReactionsSummary),
		ReplyCounts: make(map[string]int),
	}

	var wg sync.WaitGroup

	if len(pubkeys) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result.Profiles = fetchProfilesWithOptions(relays, pubkeys, cacheOnly)
		}()
	}

	if len(eventIDs) > 0 && !cacheOnly {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result.Reactions = fetchReactions(relays, eventIDs)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			result.ReplyCounts = fetchReplyCounts(relays, eventIDs)
		}()
	}

	wg.Wait()

	// Ensure maps are non-nil
	if result.Profiles == nil {
		result.Profiles = make(map[string]*ProfileInfo)
	}
	if result.Reactions == nil {
		result.Reactions = make(map[string]*ReactionsSummary)
	}
	if result.ReplyCounts == nil {
		result.ReplyCounts = make(map[string]int)
	}

	return result
}

// FetchProfilesOnly fetches only profiles in parallel.
// Wrapper for fetchProfilesWithOptions with a cleaner API.
func FetchProfilesOnly(relays, pubkeys []string, cacheOnly bool) map[string]*ProfileInfo {
	if len(pubkeys) == 0 {
		return make(map[string]*ProfileInfo)
	}
	profiles := fetchProfilesWithOptions(relays, pubkeys, cacheOnly)
	if profiles == nil {
		return make(map[string]*ProfileInfo)
	}
	return profiles
}
