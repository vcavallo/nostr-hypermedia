package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

// NavigationConfig represents the unified JSON configuration for all navigation elements
type NavigationConfig struct {
	Feeds       []FeedConfig       `json:"feeds"`
	Utility     []UtilityConfig    `json:"utility"`
	Settings    []SettingsConfig   `json:"settings"`
	KindFilters []KindFilterConfig `json:"kindFilters"`
	Defaults    DefaultsConfig     `json:"defaults"`
}

// FeedConfig represents a feed tab (Follows, Global, Me)
type FeedConfig struct {
	Name          string `json:"name"`
	TitleKey      string `json:"titleKey,omitempty"` // i18n key (defaults to "feed.{name}")
	Icon          string `json:"icon,omitempty"`
	IconOnly      string `json:"iconOnly,omitempty"` // "always", "mobile", or "" (never)
	RequiresLogin bool   `json:"requiresLogin"`
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
	Name        string         `json:"name"`
	TitleKey    string         `json:"titleKey,omitempty"`    // i18n key (defaults to "kind.{name}")
	Kinds       []int          `json:"kinds,omitempty"`       // Empty if using custom href
	KindsByFeed map[string][]int `json:"kindsByFeed,omitempty"` // Per-feed kind overrides (for "all" filter)
	Href        string         `json:"href,omitempty"`        // Custom href (overrides kinds-based URL)
	Only        []string       `json:"only,omitempty"`        // Feeds where this filter appears
}

// GetTitleKey returns the i18n key, deriving from name if not explicitly set
func (k KindFilterConfig) GetTitleKey() string {
	if k.TitleKey != "" {
		return k.TitleKey
	}
	return "kind." + k.Name
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
			{Name: "search", Href: "/html/search", RequiresLogin: false, Icon: "🔍", IconOnly: "always"},
			{Name: "notifications", Href: "/html/notifications", RequiresLogin: true, Icon: "🔔", IconOnly: "always"},
		},
		Settings: []SettingsConfig{
			{Name: "theme", Href: "/html/theme", Method: "POST", Dynamic: "theme"},
			{Name: "relays", Dynamic: "relays", RequiresLogout: true},
			{Name: "edit_profile", Href: "/html/profile/edit", RequiresLogin: true},
			{Name: "bookmarks", Href: "/html/bookmarks", Icon: "🔖", RequiresLogin: true},
			{Name: "mutes", Href: "/html/mutes", Icon: "🔇", RequiresLogin: true},
			{Name: "logout", Href: "/html/logout", RequiresLogin: true, DividerBefore: true},
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
			Title:         I18n(item.GetTitleKey()),
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
			href = "/html/timeline?kinds=" + intsToString(item.Kinds) + "&limit=10&feed=" + feed
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
			titleTemplate := I18n(item.GetTitleKey())
			settingsItem.Title = strings.Replace(titleTemplate, "{value}", themeValue, 1)
		} else if item.Dynamic == "relays" {
			settingsItem.Title = I18n(item.GetTitleKey())
			settingsItem.IsDynamic = true
		} else {
			settingsItem.Title = I18n(item.GetTitleKey())
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
		Title:   I18n("nav.settings"),
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

		// Get default kinds for this specific feed
		defaultKinds := ConfigGetDefaultKinds(feedCfg.Name)

		// Build timeline URL
		href := "/html/timeline?kinds=" + defaultKinds + "&limit=10&feed=" + feedCfg.Name

		// If only one kind filter, use that kind directly in the URL
		if len(config.KindFilters) == 1 {
			href = "/html/timeline?kinds=" + intsToString(config.KindFilters[0].Kinds) + "&limit=10&feed=" + feedCfg.Name
		}

		// Determine active state
		active := feedCfg.Name == ctx.ActiveFeed || feedCfg.Name == ctx.CurrentPage

		modes = append(modes, FeedMode{
			Name:          feedCfg.Name,
			Title:         I18n(feedCfg.GetTitleKey()),
			Href:          href,
			Icon:          feedCfg.Icon,
			IconOnly:      feedCfg.IconOnly,
			Active:        active,
			RequiresLogin: feedCfg.RequiresLogin,
		})
	}

	return modes
}

// ============================================
// Kind Filters
// ============================================

// ConfigGetKindFilters returns kind filters from config for navigation
func ConfigGetKindFilters(ctx KindFilterContext) []KindFilter {
	config := GetNavigationConfig()

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
		Title:  I18n("kind.all"),
		Href:   buildKindFilterHref(allKindsStr, ctx),
		Active: allActive,
	})

	// Add individual filters
	for _, filterCfg := range config.KindFilters {
		// Skip "all" entry - we already added it above
		if filterCfg.Name == "all" {
			continue
		}

		// Check "only" filter - if set, item only appears on specific feeds
		if len(filterCfg.Only) > 0 {
			found := false
			for _, only := range filterCfg.Only {
				if only == ctx.ActiveFeed {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Use custom href if provided, otherwise build from kinds
		var href string
		var active bool
		if filterCfg.Href != "" {
			href = filterCfg.Href
			active = ctx.ActivePage == filterCfg.Name // Match by page name
		} else {
			kindsStr := intsToString(filterCfg.Kinds)
			href = buildKindFilterHref(kindsStr, ctx)
			active = ctx.ActiveKinds == kindsStr
		}

		filters = append(filters, KindFilter{
			Name:   filterCfg.Name,
			Title:  I18n(filterCfg.GetTitleKey()),
			Href:   href,
			Active: active,
		})
	}

	return filters
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
func buildKindFilterHref(kindsStr string, ctx KindFilterContext) string {
	href := "/html/timeline?kinds=" + kindsStr + "&limit=10"

	if ctx.ActiveFeed != "" {
		href += "&feed=" + ctx.ActiveFeed
	}

	return href
}

// ============================================
// Default URLs
// ============================================

// Route constants - centralized URLs for HATEOAS compliance
const (
	RouteLogin = "/html/login"
)

// DefaultTimelineURL returns the default timeline URL based on configuration
func DefaultTimelineURL() string {
	config := GetNavigationConfig()
	defaultFeed := config.Defaults.LoggedOutFeed
	if defaultFeed == "" {
		defaultFeed = "global"
	}
	defaultKinds := ConfigGetDefaultKinds(defaultFeed)
	return "/html/timeline?kinds=" + defaultKinds + "&limit=10&feed=" + defaultFeed
}

// DefaultTimelineURLWithFeed returns the timeline URL with a specific feed
func DefaultTimelineURLWithFeed(feed string) string {
	defaultKinds := ConfigGetDefaultKinds(feed)
	return "/html/timeline?kinds=" + defaultKinds + "&limit=10&feed=" + feed
}

// DefaultTimelineURLLoggedIn returns the default timeline URL for logged-in users
func DefaultTimelineURLLoggedIn() string {
	config := GetNavigationConfig()
	defaultFeed := config.Defaults.Feed
	if defaultFeed == "" {
		defaultFeed = "follows"
	}
	defaultKinds := ConfigGetDefaultKinds(defaultFeed)
	return "/html/timeline?kinds=" + defaultKinds + "&limit=10&feed=" + defaultFeed
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
