package main

// KindDataApplier is a function that parses kind-specific data from event tags
// and applies it directly to an HTMLEventItem. This keeps type safety while
// allowing extensible kind-specific processing.
// The ctx parameter provides additional context needed for processing (profiles, relays, etc.)
type KindDataApplier func(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext)

// KindDefinition describes how to process and render a specific Nostr event kind.
// This is the single source of truth for kind-specific behavior.
type KindDefinition struct {
	Kind         int    // Nostr event kind number
	Name         string // Machine name: "note", "longform", "picture", etc.
	LabelKey     string // i18n key for human label: "kind.note.label", etc.
	TemplateName string // Template to use for rendering: "note", "longform", etc.

	// Content processing hints (used by Go-side processing)
	ExtractTitle    bool // Extract title from tags
	ExtractSummary  bool // Extract summary from tags
	ExtractImages   bool // Process imeta tags for images
	IsRepost        bool // This kind wraps another event (kind 6)
	IsAddressable   bool // Uses d-tag for addressing (30xxx kinds)
	IsReplaceable   bool // Can be replaced by newer events
	SkipContent     bool // Don't render .Content (e.g., reposts, zaps)
	RenderMarkdown  bool // Render content as markdown
	ShowInTimeline  bool // Show in main timeline feeds
	ShowReplyCount  bool // Show reply count badge

	// Behavioral flags (for protocol-agnostic processing)
	ExcludeFromReplyFilter bool     // Don't filter this kind when removing replies (e.g., reposts)
	SupportsQuotePosts     bool     // Can have q tags for quote posts (kind 1)
	RequiredTags           []string // Must have at least one of these tags to be valid
	RequireAnyTag          bool     // If true, any one of RequiredTags is sufficient

	// DataApplier for kind-specific tag extraction (registered at init)
	// This function parses tags and applies data directly to HTMLEventItem
	DataApplier KindDataApplier
}

// Label returns the localized label for this kind
func (k *KindDefinition) Label() string {
	if k.LabelKey == "" {
		return "Event"
	}
	return I18n(k.LabelKey)
}

// KindRegistry maps kind numbers to their definitions.
// Add new kinds here to support them throughout the application.
var KindRegistry = map[int]*KindDefinition{
	// Kind 1: Short text note (standard Nostr note)
	1: {
		Kind:               1,
		Name:               "note",
		LabelKey:           "kind.note.label",
		TemplateName:       "note",
		SupportsQuotePosts: true,
		ShowInTimeline:     true,
		ShowReplyCount:     true,
	},

	// Kind 6: Repost (retweet-like)
	6: {
		Kind:                   6,
		Name:                   "repost",
		LabelKey:               "kind.repost.label",
		TemplateName:           "repost",
		IsRepost:               true,
		ExcludeFromReplyFilter: true,
		SkipContent:            true, // Content is the reposted event, not text
		ShowInTimeline:         true,
	},

	// Kind 20: Picture (image post)
	20: {
		Kind:           20,
		Name:           "picture",
		LabelKey:       "kind.photo.label",
		TemplateName:   "picture",
		ExtractTitle:   true,
		ExtractImages:  true,
		ShowInTimeline: true,
		ShowReplyCount: true,
	},

	// Kind 22: Short-form vertical video (NIP-71)
	22: {
		Kind:           22,
		Name:           "shortvideo",
		LabelKey:       "kind.video.label",
		TemplateName:   "shortvideo",
		ExtractTitle:   true,
		ExtractImages:  true, // For thumbnail extraction from imeta
		ShowInTimeline: true,
		ShowReplyCount: true,
	},

	// Kind 9735: Zap receipt
	9735: {
		Kind:           9735,
		Name:           "zap",
		LabelKey:       "kind.zap.label",
		TemplateName:   "zap",
		SkipContent:    true,
		ShowInTimeline: true,
	},

	// Kind 9802: Highlight
	9802: {
		Kind:           9802,
		Name:           "highlight",
		LabelKey:       "kind.highlight.label",
		TemplateName:   "highlight",
		ShowInTimeline: true,
	},

	// Kind 10003: Bookmark list
	10003: {
		Kind:           10003,
		Name:           "bookmarks",
		LabelKey:       "kind.bookmarks.label",
		TemplateName:   "bookmarks",
		IsReplaceable:  true,
		SkipContent:    true,
		ShowInTimeline: true,
	},

	// Kind 30023: Long-form content (article)
	30023: {
		Kind:           30023,
		Name:           "longform",
		LabelKey:       "kind.article.label",
		TemplateName:   "longform",
		ExtractTitle:   true,
		ExtractSummary: true,
		IsAddressable:  true,
		RenderMarkdown: true,
		ShowInTimeline: true,
		ShowReplyCount: true,
	},

	// Kind 30311: Live event/stream
	30311: {
		Kind:           30311,
		Name:           "livestream",
		LabelKey:       "kind.live.label",
		TemplateName:   "livestream",
		IsAddressable:  true,
		SkipContent:    true, // Uses structured tags instead
		ShowInTimeline: true,
		RequiredTags:   []string{"streaming", "recording"},
		RequireAnyTag:  true, // Must have streaming OR recording
	},

	// Kind 30402: Classified listing (NIP-99)
	30402: {
		Kind:           30402,
		Name:           "classified",
		LabelKey:       "kind.classified.label",
		TemplateName:   "classified",
		ExtractTitle:   true,
		ExtractSummary: true,
		ExtractImages:  true,
		IsAddressable:  true,
		RenderMarkdown: true,
		ShowInTimeline: true,
	},
}

// DefaultKind is used for unknown kinds
var DefaultKind = &KindDefinition{
	Kind:           0,
	Name:           "unknown",
	LabelKey:       "kind.event.label",
	TemplateName:   "note", // Fall back to note template
	ShowInTimeline: true,
}

// HasRequiredTags checks if an event has the required tags for this kind.
// Returns true if no required tags are defined, or if the event has at least one.
func (k *KindDefinition) HasRequiredTags(tags [][]string) bool {
	if len(k.RequiredTags) == 0 {
		return true
	}

	for _, tag := range tags {
		if len(tag) < 2 || tag[1] == "" {
			continue
		}
		for _, required := range k.RequiredTags {
			if tag[0] == required {
				return true // Found at least one required tag
			}
		}
	}
	return false
}

// KindProcessingContext provides additional context needed by DataAppliers
type KindProcessingContext struct {
	Profiles map[string]*ProfileInfo // Pre-fetched profiles (e.g., for live event participants)
	Relays   []string                // Available relays for fetching additional data
}

// RegisterKindDataApplier registers a data applier function for a specific kind.
// Call this at init time to set up kind-specific tag parsing.
func RegisterKindDataApplier(kind int, applier KindDataApplier) {
	if def, ok := KindRegistry[kind]; ok {
		def.DataApplier = applier
	}
}

// ApplyKindData calls the registered DataApplier for this kind if one exists.
// Returns true if a DataApplier was called.
func (k *KindDefinition) ApplyKindData(item *HTMLEventItem, tags [][]string, ctx *KindProcessingContext) bool {
	if k.DataApplier == nil {
		return false
	}
	k.DataApplier(item, tags, ctx)
	return true
}

// GetKindDefinition returns the definition for a kind, or DefaultKind if not found.
func GetKindDefinition(kind int) *KindDefinition {
	if def, ok := KindRegistry[kind]; ok {
		return def
	}
	return DefaultKind
}
