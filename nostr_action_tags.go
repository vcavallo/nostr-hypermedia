package main

import (
	"strings"
	"sync"
	"time"
)

// ParsedActionTemplate represents an action definition parsed from Nostr event tags.
// This follows the NATEOAS tag format: ["action", name, method, href, field_spec...]
type ParsedActionTemplate struct {
	Name   string
	Title  string // Derived from name if not specified
	Method string
	Href   string
	Fields []ParsedFieldSpec
}

// ParsedFieldSpec represents a field definition parsed from a field spec string.
// Format: type:name:placeholder:required
type ParsedFieldSpec struct {
	Type        string
	Name        string
	Placeholder string
	Required    bool
}

// parseActionTags extracts action definitions from Nostr event tags.
// It looks for tags in the format: ["action", name, method, href, field_spec...]
// Returns a slice of ParsedActionTemplate for all valid action tags found.
func parseActionTags(tags [][]string) []ParsedActionTemplate {
	var actions []ParsedActionTemplate

	for _, tag := range tags {
		if len(tag) < 4 {
			continue
		}
		if tag[0] != "action" {
			continue
		}

		action := ParsedActionTemplate{
			Name:   tag[1],
			Method: strings.ToUpper(tag[2]),
			Href:   tag[3],
			Title:  formatActionTitle(tag[1]), // Default title from name
		}

		// Parse field specs (elements 4+)
		for i := 4; i < len(tag); i++ {
			if field := parseFieldSpec(tag[i]); field != nil {
				action.Fields = append(action.Fields, *field)
			}
		}

		actions = append(actions, action)
	}

	return actions
}

// parseFieldSpec parses a field specification string.
// Format: type:name:placeholder:required
// Examples:
//   - "hidden:event_id" -> hidden field named event_id
//   - "textarea:content:Write a comment..." -> textarea with placeholder
//   - "text:amount::true" -> required text field
func parseFieldSpec(spec string) *ParsedFieldSpec {
	parts := strings.Split(spec, ":")
	if len(parts) < 2 {
		return nil
	}

	field := &ParsedFieldSpec{
		Type: parts[0],
		Name: parts[1],
	}

	if len(parts) > 2 {
		field.Placeholder = parts[2]
	}

	if len(parts) > 3 {
		field.Required = parts[3] == "true"
	}

	return field
}

// formatActionTitle converts an action name to a display title.
// Examples: "reply" -> "Reply", "quote_repost" -> "Quote Repost"
func formatActionTitle(name string) string {
	if name == "" {
		return ""
	}
	// Replace underscores with spaces and capitalize each word
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// ToActionDefinition converts a ParsedActionTemplate to an ActionDefinition.
// This bridges the parsed Nostr format to the internal action system.
func (p ParsedActionTemplate) ToActionDefinition() ActionDefinition {
	var fields []FieldDefinition
	for _, f := range p.Fields {
		fields = append(fields, FieldDefinition{
			Name:  f.Name,
			Type:  f.Type,
			Value: "", // Value is populated at render time from context
		})
	}

	return ActionDefinition{
		Name:   p.Name,
		Title:  p.Title,
		Method: p.Method,
		Href:   p.Href,
		Fields: fields,
	}
}

// ParseActionRegistryFromTags extracts the action-registry naddr from event tags.
// Looks for ["action-registry", "naddr1..."] tag format.
// This is the Phase 4 NATEOAS pattern where events reference their own action definitions.
func ParseActionRegistryFromTags(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "action-registry" {
			return tag[1]
		}
	}
	return ""
}

// ParseKindFromTags extracts the kind number from event tags.
// Looks for ["k", "kind_number"] tag format used in kind definition events.
func ParseKindFromTags(tags [][]string) int {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "k" {
			var kind int
			for _, c := range tag[1] {
				if c >= '0' && c <= '9' {
					kind = kind*10 + int(c-'0')
				} else {
					return 0 // Invalid kind
				}
			}
			return kind
		}
	}
	return 0
}

// ParseRenderHintFromTags extracts the render hint from event tags.
// Looks for ["render", "hint_value"] tag format.
func ParseRenderHintFromTags(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "render" {
			return tag[1]
		}
	}
	return ""
}

// ParseKindNameFromTags extracts the kind name from event tags.
// Looks for ["name", "Kind Name"] tag format.
func ParseKindNameFromTags(tags [][]string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "name" {
			return tag[1]
		}
	}
	return ""
}

// HypermediaEntity represents an event with its discovered metadata.
// This is the Phase 4 NATEOAS pattern where all capabilities are discovered from event data.
type HypermediaEntity struct {
	ActionRegistryID string // naddr pointing to action definitions
	RenderHint       string // How to render this event
	Actions          []ActionDefinition
	Source           string // "local", "nostr-cache", "action-registry", "default"
}

// BuildHypermediaEntity constructs a HypermediaEntity from an event context.
// It discovers actions using the local-first priority:
//  1. Local config (actions.json) - O(1), always works
//  2. Nostr cache (kind 39001 events) - O(1), no network
//  3. Event's action-registry tag - fetches and caches naddr
//  4. Default actions - fallback
//
// Parameters:
//   - ctx: ActionContext with event metadata (Kind, EventID, etc.)
//   - tags: Event tags for parsing action-registry and render-hint
//   - relays: Relay URLs for fetching action-registry (can be nil to skip fetch)
func BuildHypermediaEntity(ctx ActionContext, tags [][]string, relays []string) *HypermediaEntity {
	entity := &HypermediaEntity{
		ActionRegistryID: ParseActionRegistryFromTags(tags),
		RenderHint:       ParseRenderHintFromTags(tags),
	}

	// Priority 1: Local config (always checked first - fast and reliable)
	if actions := GetActionsForEvent(ctx); len(actions) > 0 {
		entity.Actions = actions
		entity.Source = "local"
		return entity
	}

	// Priority 2: Nostr cache (kind metadata fetched from relays)
	if IsNostrActionsEnabled() {
		if meta := GetCachedKindMetadata(ctx.Kind); meta != nil {
			var actions []ActionDefinition
			for _, parsed := range meta.Actions {
				actions = append(actions, parsed.ToActionDefinition())
			}
			if len(actions) > 0 {
				entity.Actions = actions
				entity.Source = "nostr-cache"
				return entity
			}
		}
	}

	// Priority 3: Event's action-registry tag - fetch and cache the naddr
	if entity.ActionRegistryID != "" && len(relays) > 0 {
		parsedActions := FetchActionRegistry(entity.ActionRegistryID, relays)
		if len(parsedActions) > 0 {
			var actions []ActionDefinition
			for _, parsed := range parsedActions {
				actions = append(actions, parsed.ToActionDefinition())
			}
			entity.Actions = actions
			entity.Source = "action-registry"
			return entity
		}
		// Fetch attempted but no actions found - fall through to defaults
		entity.Source = "action-registry-empty"
	} else if entity.ActionRegistryID != "" {
		// Has action-registry but no relays provided - mark as pending
		entity.Source = "action-registry-pending"
	}

	// Priority 4: Default actions (empty for now - local config should handle this)
	if entity.Source == "" {
		entity.Source = "default"
	}

	return entity
}

// ActionRegistryCache holds cached action registries fetched from Nostr.
// Keyed by naddr string for fast lookup.
type ActionRegistryCache struct {
	mu      sync.RWMutex
	entries map[string]*ActionRegistryEntry
}

// ActionRegistryEntry represents a cached action registry with TTL.
type ActionRegistryEntry struct {
	Actions   []ParsedActionTemplate
	FetchedAt time.Time
	NotFound  bool // True if fetch was attempted but event wasn't found
}

// Global cache for action registries
var actionRegistryCache = &ActionRegistryCache{
	entries: make(map[string]*ActionRegistryEntry),
}

// Action registry cache TTL (1 hour)
const actionRegistryCacheTTL = 1 * time.Hour

// GetCachedActionRegistry returns cached actions for an naddr, if available and fresh.
func GetCachedActionRegistry(naddr string) ([]ParsedActionTemplate, bool) {
	actionRegistryCache.mu.RLock()
	defer actionRegistryCache.mu.RUnlock()

	entry, exists := actionRegistryCache.entries[naddr]
	if !exists {
		return nil, false
	}

	// Check if cache is stale
	if time.Since(entry.FetchedAt) > actionRegistryCacheTTL {
		return nil, false
	}

	// Return nil if previously not found (negative cache)
	if entry.NotFound {
		return nil, true
	}

	return entry.Actions, true
}

// CacheActionRegistry stores actions for an naddr in the cache.
func CacheActionRegistry(naddr string, actions []ParsedActionTemplate) {
	actionRegistryCache.mu.Lock()
	defer actionRegistryCache.mu.Unlock()

	actionRegistryCache.entries[naddr] = &ActionRegistryEntry{
		Actions:   actions,
		FetchedAt: time.Now(),
		NotFound:  false,
	}
}

// CacheActionRegistryNotFound marks an naddr as not found (negative cache).
func CacheActionRegistryNotFound(naddr string) {
	actionRegistryCache.mu.Lock()
	defer actionRegistryCache.mu.Unlock()

	actionRegistryCache.entries[naddr] = &ActionRegistryEntry{
		FetchedAt: time.Now(),
		NotFound:  true,
	}
}

// FetchActionRegistry fetches action definitions from an naddr.
// Returns parsed action templates or nil if not found.
// Uses cache to avoid redundant fetches.
func FetchActionRegistry(naddr string, relays []string) []ParsedActionTemplate {
	// Check cache first
	if actions, found := GetCachedActionRegistry(naddr); found {
		return actions
	}

	// Decode the naddr
	na, err := DecodeNAddr(naddr)
	if err != nil {
		CacheActionRegistryNotFound(naddr)
		return nil
	}

	// Fetch the addressable event
	events := fetchAddressableEvent(relays, na.Kind, na.Author, na.DTag)
	if len(events) == 0 {
		CacheActionRegistryNotFound(naddr)
		return nil
	}

	// Parse action tags from the event
	actions := parseActionTags(events[0].Tags)

	// Cache the result
	if len(actions) > 0 {
		CacheActionRegistry(naddr, actions)
	} else {
		CacheActionRegistryNotFound(naddr)
	}

	return actions
}

