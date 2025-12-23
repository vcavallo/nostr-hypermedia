package main

// FeedMode represents a timeline feed mode (follows, global, me) or any nav item
type FeedMode struct {
	Name          string     // "follows", "global", "me", "notifications", "search", etc.
	Title         string     // Display text
	Href          string     // URL
	Icon          string     // Optional icon
	IconOnly      string     // "always", "mobile", or "" (never) - controls icon-only display
	Active        bool       // Is this the current mode
	RequiresLogin bool       // Only show when logged in
	Group         string     // "feed", "utility", or empty for contextual
	Children      []FeedMode // Nested items (for DVM dropdown)
	IsDropdown    bool       // True if this has 2+ children (render as dropdown)
	IsDVM         bool       // True if this is a DVM feed
}

// FeedModeContext provides context for building feed modes
type FeedModeContext struct {
	LoggedIn    bool
	ActiveFeed  string // Current feed mode (for timeline)
	ActiveKinds string // Current kinds parameter (preserved when switching feeds)
	CurrentPage string // Current page type: "timeline", "profile", "thread", "search", "notifications"
}

// GetFeedModes returns the list of feed modes for the current context
// Uses navigation.json feeds section
func GetFeedModes(ctx FeedModeContext) []FeedMode {
	return ConfigGetFeedModes(ctx)
}

// KindFilter represents a content type filter (notes, photos, longform, etc.)
type KindFilter struct {
	Name       string       // "all", "notes", "photos", "longform", "highlights", "live"
	Title      string       // Display text
	Href       string       // URL
	Active     bool         // Is this the current filter
	IsDropdown bool         // True if this filter has children (renders as dropdown)
	Children   []KindFilter // Nested filters for dropdown groups
}

// KindFilterContext provides context for building kind filters
type KindFilterContext struct {
	LoggedIn    bool
	ActiveFeed  string // Current feed mode
	ActiveKinds string // Current kinds parameter
	ActivePage  string // Current page name (for custom href items like "mutes")
}

// GetKindFilters returns the list of kind filters for the current context
// Uses navigation.json kindFilters section
// Returns nil if only one kind filter is configured (no submenu needed)
func GetKindFilters(ctx KindFilterContext) []KindFilter {
	return ConfigGetKindFilters(ctx)
}

// NavItem represents a navigation destination (search, notifications)
type NavItem struct {
	Name          string // "search", "notifications"
	Title         string // Display text
	Href          string // URL
	Active        bool   // Is this the current page
	RequiresLogin bool   // Only show when logged in
	Icon          string // Optional icon
	IconOnly      string // "always", "mobile", or "" (never) - controls icon-only display
	HasBadge      bool   // Show notification badge
}

// NavContext provides context for building nav items
type NavContext struct {
	LoggedIn      bool
	ActivePage    string // Current page name
	HasUnread     bool   // Has unread notifications
}

// GetNavItems returns the list of nav items for the current context
// Uses navigation.json utility section
func GetNavItems(ctx NavContext) []NavItem {
	return ConfigGetNavItems(ctx)
}

// SettingsItem represents a settings dropdown item
type SettingsItem struct {
	Name          string // "theme", "edit_profile", "logout", "bookmarks", "mutes", etc.
	Title         string // Display text
	Href          string // URL (for links)
	Icon          string // Optional icon
	Method        string // "GET" or "POST"
	RequiresLogin bool   // Only show when logged in
	IsForm        bool   // Render as form instead of link
	DividerBefore bool   // Render a divider above this item
	IsDynamic     bool   // Dynamic item (relays, reactions)
	DynamicType   string // "theme", "relays", or "reactions"
	Active        bool   // Is this the current page
	IsChecked     bool   // For checkbox-style items (reactions toggle)
}

// SettingsContext provides context for building settings items
type SettingsContext struct {
	LoggedIn      bool
	ThemeLabel    string // Current theme label for display
	CurrentPage   string // Current page name (for active state)
	FeedMode      string // Current feed mode
	KindFilter    string // Current kinds filter
	UserAvatarURL string // User's profile picture URL (for avatar icon)
	UserNpub      string // User's npub for dynamic profile link
}

// GetSettingsItems returns the list of settings items for the current context
// Uses navigation.json settings section
func GetSettingsItems(ctx SettingsContext) []SettingsItem {
	return ConfigGetSettingsItems(ctx)
}

// SettingsToggle represents the settings dropdown toggle button
type SettingsToggle struct {
	Icon    string // Emoji or image URL
	IsImage bool   // True if Icon is an image URL (starts with / or http)
	Title   string // Tooltip/accessibility title
}

// GetSettingsToggle returns the settings toggle configuration
func GetSettingsToggle(ctx SettingsContext) SettingsToggle {
	return ConfigGetSettingsToggle(ctx)
}
