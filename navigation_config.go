package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	cfgpkg "nostr-server/internal/config"
	"nostr-server/internal/util"
)

// NavigationConfig represents the unified JSON configuration for all navigation elements
type NavigationConfig struct {
	Feeds       []FeedConfig       `json:"feeds"`
	Utility     []UtilityConfig    `json:"utility"`
	Settings    []SettingsConfig   `json:"settings"`
	KindFilters []KindFilterConfig `json:"kindFilters"`
	Defaults    DefaultsConfig     `json:"defaults"`
}

// FeedConfig represents a feed tab (Follows, Global, Me) or a DVM feed group
type FeedConfig struct {
	Name          string       `json:"name"`
	TitleKey      string       `json:"titleKey,omitempty"` // i18n key (defaults to "feed.{name}")
	Icon          string       `json:"icon,omitempty"`
	IconOnly      string       `json:"iconOnly,omitempty"` // "always", "mobile", or "" (never)
	RequiresLogin bool         `json:"requiresLogin"`
	Feeds         []FeedConfig `json:"feeds,omitempty"` // Nested feeds (for DVM groups)
	DVM           *DVMConfig   `json:"dvm,omitempty"`   // DVM configuration (if this is a DVM feed)
}

// DVMConfig represents Data Vending Machine configuration
type DVMConfig struct {
	Kind         int               `json:"kind"`                   // DVM job request kind (e.g., 5300)
	Pubkey       string            `json:"pubkey"`                 // DVM's hex pubkey
	Relays       []string          `json:"relays,omitempty"`       // Relay URLs where this DVM listens (falls back to defaultRelays)
	CacheTTL     int               `json:"cacheTTL,omitempty"`     // Cache duration in seconds (default 300)
	PageSize     int               `json:"pageSize,omitempty"`     // Items per page (default 10)
	Personalized bool              `json:"personalized,omitempty"` // Include user pubkey for personalized results
	Params       map[string]string `json:"params,omitempty"`       // Additional params (e.g., max_results, topic)
	Name         string            `json:"name,omitempty"`         // Display name override (fetched from kind 31990 if not set)
	Image        string            `json:"image,omitempty"`        // Image URL override (fetched from kind 31990 if not set)
	Description  string            `json:"description,omitempty"`  // Description override (fetched from kind 31990 if not set)
}

// GetCacheTTL returns cache TTL with default of 300 seconds
func (d *DVMConfig) GetCacheTTL() int {
	if d.CacheTTL > 0 {
		return d.CacheTTL
	}
	return 300
}

// GetPageSize returns page size with default of 10
func (d *DVMConfig) GetPageSize() int {
	if d.PageSize > 0 {
		return d.PageSize
	}
	return 10
}

// GetRelays returns relay URLs for this DVM, falling back to default relays
func (d *DVMConfig) GetRelays() []string {
	if len(d.Relays) > 0 {
		return d.Relays
	}
	return cfgpkg.GetDefaultRelays()
}

// IsDVMFeed returns true if this feed has DVM configuration
func (f FeedConfig) IsDVMFeed() bool {
	return f.DVM != nil
}

// HasNestedFeeds returns true if this feed has nested children
func (f FeedConfig) HasNestedFeeds() bool {
	return len(f.Feeds) > 0
}

// GetVisibleFeeds returns nested feeds that are visible based on login state
func (f FeedConfig) GetVisibleFeeds(loggedIn bool) []FeedConfig {
	var visible []FeedConfig
	for _, child := range f.Feeds {
		if child.RequiresLogin && !loggedIn {
			continue
		}
		visible = append(visible, child)
	}
	return visible
}

// GetTitleKey returns the i18n key, deriving from name if not explicitly set
func (f FeedConfig) GetTitleKey() string {
	if f.TitleKey != "" {
		return f.TitleKey
	}
	return "feed." + f.Name
}

// UtilityConfig represents a utility nav item (Search, Notifications)
type UtilityConfig struct {
	Name          string `json:"name"`
	TitleKey      string `json:"titleKey,omitempty"` // i18n key (defaults to "nav.{name}")
	Href          string `json:"href"`
	RequiresLogin bool   `json:"requiresLogin"`
	Icon          string `json:"icon"`
	IconOnly      string `json:"iconOnly,omitempty"` // "always", "mobile", or "" (never)
}

// GetTitleKey returns the i18n key, deriving from name if not explicitly set
func (u UtilityConfig) GetTitleKey() string {
	if u.TitleKey != "" {
		return u.TitleKey
	}
	return "nav." + u.Name
}

// KindFilterConfig represents a kind filter in the submenu
type KindFilterConfig struct {
	Name        string             `json:"name"`
	TitleKey    string             `json:"titleKey,omitempty"`    // i18n key (defaults to "kind.{name}")
	Kinds       []int              `json:"kinds,omitempty"`       // Empty if using custom href or group
	Tags        []string           `json:"tags,omitempty"`        // #t tag filter (e.g., ["nostrcooking"] for recipes)
	KindsByFeed map[string][]int   `json:"kindsByFeed,omitempty"` // Per-feed kind overrides (for "all" filter)
	Href        string             `json:"href,omitempty"`        // Custom href (overrides kinds-based URL)
	Limit       int                `json:"limit,omitempty"`       // Custom limit (defaults to 10 if not set)
	Only        []string           `json:"only,omitempty"`        // Feeds where this filter appears
	Children    []KindFilterConfig `json:"children,omitempty"`    // Nested filters (renders as dropdown)
}

// GetTitleKey returns the i18n key, deriving from name if not explicitly set
func (k KindFilterConfig) GetTitleKey() string {
	if k.TitleKey != "" {
		return k.TitleKey
	}
	return "kind." + k.Name
}

// IsGroup returns true if this filter has children (renders as dropdown)
func (k KindFilterConfig) IsGroup() bool {
	return len(k.Children) > 0
}

// GetKindsForFeed returns the kinds for a specific feed, with fallback to default kinds
func (k KindFilterConfig) GetKindsForFeed(feed string) []int {
	if k.KindsByFeed != nil {
		if kinds, ok := k.KindsByFeed[feed]; ok {
			return kinds
		}
	}
	return k.Kinds
}

// SettingsConfig represents a settings dropdown item
type SettingsConfig struct {
	Name           string `json:"name"`
	TitleKey       string `json:"titleKey,omitempty"` // i18n key (defaults to "settings.{name}")
	Href           string `json:"href,omitempty"`
	Icon           string `json:"icon,omitempty"`
	Method         string `json:"method,omitempty"` // "GET" or "POST", defaults to "GET"
	RequiresLogin  bool   `json:"requiresLogin,omitempty"`
	RequiresLogout bool   `json:"requiresLogout,omitempty"`
	DividerBefore  bool   `json:"dividerBefore,omitempty"`
	Dynamic        string `json:"dynamic,omitempty"` // "theme" or "relays"
	Kinds          []int  `json:"kinds,omitempty"`   // Shortcut: navigate to timeline with these kinds
	Feed           string `json:"feed,omitempty"`    // Feed to use with kinds shortcut (e.g., "me")
}

// GetTitleKey returns the i18n key, deriving from name if not explicitly set
func (s SettingsConfig) GetTitleKey() string {
	if s.TitleKey != "" {
		return s.TitleKey
	}
	return "settings." + s.Name
}

// GetMethod returns the HTTP method, defaulting to GET
func (s SettingsConfig) GetMethod() string {
	if s.Method != "" {
		return s.Method
	}
	return "GET"
}

// DefaultsConfig holds default values for navigation
type DefaultsConfig struct {
	Feed                 string `json:"feed"`
	LoggedOutFeed        string `json:"loggedOutFeed"`
	SettingsIcon         string `json:"settingsIcon,omitempty"`         // Icon for settings toggle ("avatar" for user pic, or emoji/image path)
	SettingsIconFallback string `json:"settingsIconFallback,omitempty"` // Fallback when avatar not available
}

var (
	navigationConfig     *NavigationConfig
	navigationConfigMu   sync.RWMutex
	navigationConfigOnce sync.Once
)

// GetNavigationConfig returns the current navigation configuration (thread-safe)
func GetNavigationConfig() *NavigationConfig {
	// Use sync.Once for initial load (most common case after startup)
	navigationConfigOnce.Do(func() {
		navigationConfigMu.Lock()
		defer navigationConfigMu.Unlock()
		if navigationConfig == nil {
			navigationConfig = loadNavigationConfigFromFile()
		}
	})

	navigationConfigMu.RLock()
	defer navigationConfigMu.RUnlock()
	return navigationConfig
}

// ReloadNavigationConfig reloads the configuration from file
func ReloadNavigationConfig() error {
	newConfig := loadNavigationConfigFromFile()
	navigationConfigMu.Lock()
	defer navigationConfigMu.Unlock()
	navigationConfig = newConfig
	slog.Info("navigation configuration reloaded")
	return nil
}

func loadNavigationConfigFromFile() *NavigationConfig {
	configPath := os.Getenv("NAVIGATION_CONFIG")
	if configPath == "" {
		configPath = "config/navigation.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("config file not found, using defaults", "path", configPath)
		} else {
			slog.Warn("could not read config, using defaults", "path", configPath, "error", err)
		}
		return getDefaultNavigationConfig()
	}

	var config NavigationConfig
	if err := json.Unmarshal(data, &config); err != nil {
		slog.Error("invalid JSON in config, using defaults", "path", configPath, "error", err)
		return getDefaultNavigationConfig()
	}

	slog.Info("loaded navigation configuration",
		"feeds", len(config.Feeds),
		"utility", len(config.Utility),
		"settings", len(config.Settings),
		"kind_filters", len(config.KindFilters))
	return &config
}

// getDefaultNavigationConfig returns the embedded default configuration
func getDefaultNavigationConfig() *NavigationConfig {
	return &NavigationConfig{
		Feeds: []FeedConfig{
			{Name: "follows", Icon: "👥", RequiresLogin: true},
			{Name: "global", Icon: "🌐", RequiresLogin: false},
			{Name: "me", Icon: "👤", RequiresLogin: true},
		},
		Utility: []UtilityConfig{
			{Name: "search", Href: "/search", RequiresLogin: false, Icon: "🔍", IconOnly: "always"},
			{Name: "notifications", Href: "/notifications", RequiresLogin: true, Icon: "🔔", IconOnly: "always"},
		},
		Settings: []SettingsConfig{
			{Name: "theme", Href: "/theme", Method: "POST", Dynamic: "theme"},
			{Name: "relays", Dynamic: "relays", RequiresLogout: true},
			{Name: "edit_profile", Href: "/profile/edit", RequiresLogin: true},
			{Name: "bookmarks", Href: "/bookmarks", Icon: "🔖", RequiresLogin: true},
			{Name: "mutes", Href: "/mutes", Icon: "🔇", RequiresLogin: true},
			{Name: "logout", Href: "/logout", RequiresLogin: true, DividerBefore: true},
		},
		KindFilters: []KindFilterConfig{
			{Name: "notes", Kinds: []int{1}},
			{Name: "photos", Kinds: []int{20}},
			{Name: "longform", Kinds: []int{30023}},
			{Name: "highlights", Kinds: []int{9802}},
			{Name: "live", Kinds: []int{30311}},
		},
		Defaults: DefaultsConfig{
			Feed:          "follows",
			LoggedOutFeed: "global",
		},
	}
}

// ============================================
// Utility Nav Items
// ============================================

// ConfigGetNavItems returns the utility nav items from config for the given context
func ConfigGetNavItems(ctx NavContext) []NavItem {
	config := GetNavigationConfig()
	var items []NavItem

	for _, item := range config.Utility {
		// Skip login-required items if not logged in
		if item.RequiresLogin && !ctx.LoggedIn {
			continue
		}

		navItem := NavItem{
			Name:          item.Name,
			Title:         cfgpkg.I18n(item.GetTitleKey()),
			Href:          item.Href,
			Active:        item.Name == ctx.ActivePage,
			RequiresLogin: item.RequiresLogin,
			Icon:          item.Icon,
			IconOnly:      item.IconOnly,
		}

		// Always add badge element for notifications (when logged in) so polling works
		if item.Name == "notifications" && ctx.LoggedIn {
			navItem.HasBadge = true
		}

		items = append(items, navItem)
	}

	return items
}

// ============================================
// Settings Items
// ============================================

// ConfigGetSettingsItems returns the settings items from config for the given context
func ConfigGetSettingsItems(ctx SettingsContext) []SettingsItem {
	config := GetNavigationConfig()
	var items []SettingsItem

	for _, item := range config.Settings {
		// Skip login-required items if not logged in
		if item.RequiresLogin && !ctx.LoggedIn {
			continue
		}
		// Skip logout-required items if logged in
		if item.RequiresLogout && ctx.LoggedIn {
			continue
		}

		// Build href - use kinds shortcut if specified, otherwise use explicit href
		href := item.Href
		if len(item.Kinds) > 0 {
			feed := item.Feed
			if feed == "" {
				feed = "follows"
			}
			href = util.BuildURL("/timeline", map[string]string{
				"feed":  feed,
				"kinds": intsToString(item.Kinds),
				"limit": "10",
			})
		} else if href == "" && item.Name == "profile" && ctx.UserNpub != "" {
			// Dynamic profile link - auto-generate from logged-in user's npub
			href = "/profile/" + ctx.UserNpub
		}

		settingsItem := SettingsItem{
			Name:          item.Name,
			Href:          href,
			Icon:          item.Icon,
			Method:        item.GetMethod(),
			RequiresLogin: item.RequiresLogin,
			IsForm:        item.GetMethod() == "POST",
			DividerBefore: item.DividerBefore,
			DynamicType:   item.Dynamic,
			Active:        item.Name == ctx.CurrentPage,
		}

		// Handle dynamic items
		if item.Dynamic == "theme" {
			// Build theme title with interpolation: "Theme: {value}"
			themeValue := ctx.ThemeLabel
			if themeValue == "" {
				themeValue = "Light"
			}
			// Get the template and interpolate
			titleTemplate := cfgpkg.I18n(item.GetTitleKey())
			settingsItem.Title = strings.Replace(titleTemplate, "{value}", themeValue, 1)
		} else if item.Dynamic == "relays" {
			settingsItem.Title = cfgpkg.I18n(item.GetTitleKey())
			settingsItem.IsDynamic = true
		} else {
			settingsItem.Title = cfgpkg.I18n(item.GetTitleKey())
		}

		items = append(items, settingsItem)
	}

	return items
}

// ConfigGetSettingsToggle returns the settings toggle configuration
func ConfigGetSettingsToggle(ctx SettingsContext) SettingsToggle {
	config := GetNavigationConfig()

	icon := config.Defaults.SettingsIcon
	fallback := config.Defaults.SettingsIconFallback

	// Default fallback if not specified
	if fallback == "" {
		fallback = "⚙️"
	}

	// Default icon if not specified
	if icon == "" {
		icon = "⚙️"
	}

	// Resolve "avatar" special value
	if icon == "avatar" {
		if ctx.LoggedIn && ctx.UserAvatarURL != "" {
			icon = ctx.UserAvatarURL
		} else {
			icon = fallback
		}
	}

	// Determine if icon is an image (starts with / or http)
	isImage := strings.HasPrefix(icon, "/") || strings.HasPrefix(icon, "http")

	return SettingsToggle{
		Icon:    icon,
		IsImage: isImage,
		Title:   cfgpkg.I18n("nav.settings"),
	}
}

// ============================================
// Feed Modes
// ============================================

// ConfigGetFeedModes returns feed modes from config for navigation
func ConfigGetFeedModes(ctx FeedModeContext) []FeedMode {
	config := GetNavigationConfig()

	var modes []FeedMode

	for _, feedCfg := range config.Feeds {
		// Skip login-required modes if not logged in
		if feedCfg.RequiresLogin && !ctx.LoggedIn {
			continue
		}

		// Handle nested feeds (DVM groups)
		if feedCfg.HasNestedFeeds() {
			mode := buildNestedFeedMode(feedCfg, ctx, config)
			if mode != nil {
				modes = append(modes, *mode)
			}
			continue
		}

		// Handle DVM feed (single DVM without parent group)
		if feedCfg.IsDVMFeed() {
			mode := buildDVMFeedMode(feedCfg, ctx)
			modes = append(modes, mode)
			continue
		}

		// Regular feed (follows, global, me)
		// Preserve current kinds if set, otherwise use defaults
		kinds := ctx.ActiveKinds
		if kinds == "" {
			kinds = ConfigGetDefaultKinds(feedCfg.Name)
			// If only one kind filter, use that kind directly
			if len(config.KindFilters) == 1 {
				kinds = intsToString(config.KindFilters[0].Kinds)
			}
		}

		// Build timeline URL
		href := util.BuildURL("/timeline", map[string]string{
			"feed":  feedCfg.Name,
			"kinds": kinds,
			"limit": "10",
		})

		// Determine active state
		active := feedCfg.Name == ctx.ActiveFeed || feedCfg.Name == ctx.CurrentPage

		modes = append(modes, FeedMode{
			Name:          feedCfg.Name,
			Title:         cfgpkg.I18n(feedCfg.GetTitleKey()),
			Href:          href,
			Icon:          feedCfg.Icon,
			IconOnly:      feedCfg.IconOnly,
			Active:        active,
			RequiresLogin: feedCfg.RequiresLogin,
		})
	}

	return modes
}

// buildNestedFeedMode builds a FeedMode for a feed with nested children (DVM group)
func buildNestedFeedMode(feedCfg FeedConfig, ctx FeedModeContext, config *NavigationConfig) *FeedMode {
	visibleFeeds := feedCfg.GetVisibleFeeds(ctx.LoggedIn)

	// No visible children - hide the parent
	if len(visibleFeeds) == 0 {
		return nil
	}

	// Build children
	var children []FeedMode
	anyChildActive := false
	for _, child := range visibleFeeds {
		childMode := buildDVMFeedMode(child, ctx)
		children = append(children, childMode)
		if childMode.Active {
			anyChildActive = true
		}
	}

	// 1 child: show as direct tab with parent's name and icon
	if len(children) == 1 {
		return &FeedMode{
			Name:          children[0].Name,
			Title:         cfgpkg.I18n(feedCfg.GetTitleKey()), // Use parent's title
			Href:          children[0].Href,
			Icon:          feedCfg.Icon,
			IconOnly:      feedCfg.IconOnly,
			Active:        children[0].Active,
			RequiresLogin: feedCfg.RequiresLogin,
			IsDVM:         true,
		}
	}

	// 2+ children: show as dropdown
	return &FeedMode{
		Name:          feedCfg.Name,
		Title:         cfgpkg.I18n(feedCfg.GetTitleKey()),
		Icon:          feedCfg.Icon,
		IconOnly:      feedCfg.IconOnly,
		Active:        anyChildActive, // Parent active if any child is active
		RequiresLogin: feedCfg.RequiresLogin,
		Children:      children,
		IsDropdown:    true,
	}
}

// buildDVMFeedMode builds a FeedMode for a DVM feed
func buildDVMFeedMode(feedCfg FeedConfig, ctx FeedModeContext) FeedMode {
	// DVM feeds use timeline route with feed parameter
	href := util.BuildURL("/timeline", map[string]string{
		"feed":  feedCfg.Name,
		"limit": "10",
	})

	// Determine active state
	active := feedCfg.Name == ctx.ActiveFeed

	return FeedMode{
		Name:          feedCfg.Name,
		Title:         cfgpkg.I18n(feedCfg.GetTitleKey()),
		Href:          href,
		Icon:          feedCfg.Icon,
		IconOnly:      feedCfg.IconOnly,
		Active:        active,
		RequiresLogin: feedCfg.RequiresLogin,
		IsDVM:         true,
	}
}

// GetDVMConfig looks up DVM configuration by feed name
// Returns nil if the feed is not a DVM feed
func GetDVMConfig(feedName string) *DVMConfig {
	config := GetNavigationConfig()
	return findDVMConfig(config.Feeds, feedName)
}

// findDVMConfig recursively searches for a DVM config by feed name
func findDVMConfig(feeds []FeedConfig, feedName string) *DVMConfig {
	for _, feed := range feeds {
		// Check if this feed matches
		if feed.Name == feedName && feed.DVM != nil {
			return feed.DVM
		}
		// Check nested feeds
		if len(feed.Feeds) > 0 {
			if found := findDVMConfig(feed.Feeds, feedName); found != nil {
				return found
			}
		}
	}
	return nil
}

// IsDVMFeedName returns true if the given feed name corresponds to a DVM feed
func IsDVMFeedName(feedName string) bool {
	return GetDVMConfig(feedName) != nil
}

// ============================================
// Kind Filters
// ============================================

// ConfigGetKindFilters returns kind filters from config for navigation
func ConfigGetKindFilters(ctx KindFilterContext) []KindFilter {
	config := GetNavigationConfig()

	// DVM feeds don't show kind filters - they have their own content curation
	if IsDVMFeedName(ctx.ActiveFeed) {
		return nil
	}

	// If only one filter configured, return empty (no submenu needed)
	if len(config.KindFilters) <= 1 {
		return nil
	}

	var filters []KindFilter

	// Build "All" filter from all configured kinds (feed-aware)
	allKinds := ConfigGetAllKinds(ctx.ActiveFeed)
	allKindsStr := intsToString(allKinds)

	// "All" is active when no specific kinds or custom page is selected
	allActive := (ctx.ActiveKinds == allKindsStr || ctx.ActiveKinds == "") && ctx.ActivePage == ""
	filters = append(filters, KindFilter{
		Name:   "all",
		Title:  cfgpkg.I18n("kind.all"),
		Href:   buildKindFilterHref(allKindsStr, nil, 0, ctx),
		Active: allActive,
	})

	// Add individual filters
	for _, filterCfg := range config.KindFilters {
		// Skip "all" entry - we already added it above
		if filterCfg.Name == "all" {
			continue
		}

		// Handle grouped filters (dropdown)
		if filterCfg.IsGroup() {
			groupFilter := buildKindFilterGroup(filterCfg, ctx)
			if groupFilter != nil {
				filters = append(filters, *groupFilter)
			}
			continue
		}

		// Check "only" filter - if set, item only appears on specific feeds
		if !isKindFilterVisibleForFeed(filterCfg, ctx.ActiveFeed) {
			continue
		}

		filter := buildSingleKindFilter(filterCfg, ctx)
		filters = append(filters, filter)
	}

	// Check if any filter is active
	anyActive := false
	for _, f := range filters {
		if f.Active {
			anyActive = true
			break
		}
		// Also check children for dropdown filters
		for _, c := range f.Children {
			if c.Active {
				anyActive = true
				break
			}
		}
	}

	// If no filter is active and we have kinds, add ephemeral filter
	if !anyActive && ctx.ActiveKinds != "" {
		ephemeral := buildEphemeralKindFilter(ctx)
		if ephemeral != nil {
			filters = append(filters, *ephemeral)
		}
	}

	return filters
}

// buildEphemeralKindFilter creates a temporary filter for kinds not matching any defined filter
func buildEphemeralKindFilter(ctx KindFilterContext) *KindFilter {
	// Parse kinds string into ints
	kindStrs := strings.Split(ctx.ActiveKinds, ",")
	var kinds []int
	for _, s := range kindStrs {
		s = strings.TrimSpace(s)
		if k, err := strconv.Atoi(s); err == nil {
			kinds = append(kinds, k)
		}
	}

	if len(kinds) == 0 {
		return nil
	}

	// Build title from kind names
	var names []string
	for _, k := range kinds {
		def := GetKindDefinition(k)
		if def != nil && def.LabelKey != "" {
			name := cfgpkg.I18n(def.LabelKey)
			names = append(names, name)
		} else {
			// Fallback to kind number if no label
			names = append(names, fmt.Sprintf("Kind %d", k))
		}
	}

	// Build title: single kind shows name, multiple shows "Name +N"
	var title string
	if len(names) == 1 {
		title = names[0]
	} else if len(names) > 1 {
		title = fmt.Sprintf("%s +%d", names[0], len(names)-1)
	}

	// Build href - same URL (no-op when clicked)
	href := buildKindFilterHref(ctx.ActiveKinds, nil, 0, ctx)

	return &KindFilter{
		Name:   "ephemeral",
		Title:  title,
		Href:   href,
		Active: true,
	}
}

// isKindFilterVisibleForFeed checks if a filter should be visible on the given feed
func isKindFilterVisibleForFeed(filterCfg KindFilterConfig, feed string) bool {
	if len(filterCfg.Only) == 0 {
		return true
	}
	for _, only := range filterCfg.Only {
		if only == feed {
			return true
		}
	}
	return false
}

// buildSingleKindFilter builds a KindFilter from a KindFilterConfig
func buildSingleKindFilter(filterCfg KindFilterConfig, ctx KindFilterContext) KindFilter {
	var href string
	var active bool

	if filterCfg.Href != "" {
		href = filterCfg.Href
		active = ctx.ActivePage == filterCfg.Name
	} else {
		kindsStr := intsToString(filterCfg.Kinds)
		href = buildKindFilterHref(kindsStr, filterCfg.Tags, filterCfg.Limit, ctx)
		// Active when kinds match AND tags match (or filter has no tags)
		tagsMatch := len(filterCfg.Tags) == 0 || ctx.ActiveTags == strings.Join(filterCfg.Tags, ",")
		active = ctx.ActiveKinds == kindsStr && tagsMatch
	}

	return KindFilter{
		Name:   filterCfg.Name,
		Title:  cfgpkg.I18n(filterCfg.GetTitleKey()),
		Href:   href,
		Active: active,
	}
}

// buildKindFilterGroup builds a dropdown KindFilter from a grouped KindFilterConfig
func buildKindFilterGroup(filterCfg KindFilterConfig, ctx KindFilterContext) *KindFilter {
	var children []KindFilter
	anyChildActive := false

	for _, childCfg := range filterCfg.Children {
		// Check if child is visible for this feed
		if !isKindFilterVisibleForFeed(childCfg, ctx.ActiveFeed) {
			continue
		}

		child := buildSingleKindFilter(childCfg, ctx)
		children = append(children, child)
		if child.Active {
			anyChildActive = true
		}
	}

	// No visible children - skip the group
	if len(children) == 0 {
		return nil
	}

	return &KindFilter{
		Name:       filterCfg.Name,
		Title:      cfgpkg.I18n(filterCfg.GetTitleKey()),
		Active:     anyChildActive,
		IsDropdown: true,
		Children:   children,
	}
}

// ConfigGetAllKinds returns all enabled kinds from config for a specific feed.
// If an explicit "all" entry exists in kindFilters, uses its kinds (with feed override).
// Otherwise, collects kinds from all configured filters.
func ConfigGetAllKinds(feed string) []int {
	config := GetNavigationConfig()

	// Check for explicit "all" entry
	for _, filter := range config.KindFilters {
		if filter.Name == "all" {
			kinds := filter.GetKindsForFeed(feed)
			if len(kinds) > 0 {
				return kinds
			}
			break
		}
	}

	// Fallback: collect from all filters
	kindSet := make(map[int]bool)
	var kinds []int

	for _, filter := range config.KindFilters {
		if filter.Name == "all" {
			continue // Skip "all" entry in collection
		}
		for _, k := range filter.Kinds {
			if !kindSet[k] {
				kindSet[k] = true
				kinds = append(kinds, k)
			}
		}
	}

	return kinds
}

// ConfigGetDefaultKinds returns the default kinds string for timeline
func ConfigGetDefaultKinds(feed string) string {
	return intsToString(ConfigGetAllKinds(feed))
}

// buildKindFilterHref builds the URL for a kind filter
func buildKindFilterHref(kindsStr string, tags []string, limit int, ctx KindFilterContext) string {
	limitStr := "10"
	if limit > 0 {
		limitStr = strconv.Itoa(limit)
	}
	params := map[string]string{
		"kinds": kindsStr,
		"limit": limitStr,
	}
	if len(tags) > 0 {
		params["t"] = strings.Join(tags, ",")
	}
	if ctx.ActiveFeed != "" {
		params["feed"] = ctx.ActiveFeed
	}
	return util.BuildURL("/timeline", params)
}

// ============================================
// Default URLs
// ============================================

// Route constants - centralized URLs for HATEOAS compliance
const (
	RouteLogin = "/login"
)

// DefaultTimelineURL returns the default timeline URL based on configuration
func DefaultTimelineURL() string {
	config := GetNavigationConfig()
	defaultFeed := config.Defaults.LoggedOutFeed
	if defaultFeed == "" {
		defaultFeed = "global"
	}
	return util.BuildURL("/timeline", map[string]string{
		"feed":  defaultFeed,
		"kinds": ConfigGetDefaultKinds(defaultFeed),
		"limit": "10",
	})
}

// DefaultTimelineURLWithFeed returns the timeline URL with a specific feed
func DefaultTimelineURLWithFeed(feed string) string {
	return util.BuildURL("/timeline", map[string]string{
		"feed":  feed,
		"kinds": ConfigGetDefaultKinds(feed),
		"limit": "10",
	})
}

// DefaultTimelineURLLoggedIn returns the default timeline URL for logged-in users
func DefaultTimelineURLLoggedIn() string {
	config := GetNavigationConfig()
	defaultFeed := config.Defaults.Feed
	if defaultFeed == "" {
		defaultFeed = "follows"
	}
	return util.BuildURL("/timeline", map[string]string{
		"feed":  defaultFeed,
		"kinds": ConfigGetDefaultKinds(defaultFeed),
		"limit": "10",
	})
}

// ============================================
// Helper functions
// ============================================

// intsToString converts a slice of ints to comma-separated string
func intsToString(ints []int) string {
	strs := make([]string, len(ints))
	for i, v := range ints {
		strs[i] = strconv.Itoa(v)
	}
	return strings.Join(strs, ",")
}

// ============================================
// Backward compatibility aliases
// ============================================

// These types and functions maintain backward compatibility with code
// that was using the old separate config files

// FeedsConfig is an alias for backward compatibility
type FeedsConfig = NavigationConfig

// KindsConfig is an alias for backward compatibility
type KindsConfig = NavigationConfig

// FeedModeConfig is an alias for FeedConfig
type FeedModeConfig = FeedConfig

// GetFeedsConfig returns the navigation config (backward compatibility)
func GetFeedsConfig() *NavigationConfig {
	return GetNavigationConfig()
}

// ReloadFeedsConfig reloads navigation config (backward compatibility)
func ReloadFeedsConfig() error {
	return ReloadNavigationConfig()
}

// ReloadKindsConfig reloads navigation config (backward compatibility)
func ReloadKindsConfig() error {
	return ReloadNavigationConfig()
}

// Ensure FeedsConfig has the expected fields for backward compatibility
func (n *NavigationConfig) GetDefaultFeed() string {
	return n.Defaults.Feed
}

func (n *NavigationConfig) GetDefaultLoggedOutFeed() string {
	return n.Defaults.LoggedOutFeed
}

// For templates that access .FeedModes directly
func (n *NavigationConfig) GetFeedModes() []FeedConfig {
	return n.Feeds
}

// For code that accesses .KindFilters
func (n *NavigationConfig) GetKindFilters() []KindFilterConfig {
	return n.KindFilters
}

// Compatibility: some code checks len(kindsConfig.KindFilters)
// This is handled by KindFilters field directly

// fmt is imported but we need to use it somewhere to avoid unused import error
var _ = fmt.Sprint
