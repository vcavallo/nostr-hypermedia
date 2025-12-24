package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"

	cfgpkg "nostr-server/internal/config"
)

// ActionsConfig represents the JSON configuration for actions
type ActionsConfig struct {
	Actions       map[string]ActionConfig `json:"actions"`
	DisplayOrder  []string                `json:"displayOrder"`
	KindOverrides map[string]KindOverride `json:"kindOverrides"`
	FieldDefaults map[string]string       `json:"fieldDefaults"`
}

// ActionConfig represents a single action's configuration
type ActionConfig struct {
	TitleKey       string   `json:"titleKey,omitempty"`       // i18n key (defaults to "action.{name}")
	Method         string   `json:"method"`
	Href           string   `json:"href"`
	Class          string   `json:"class"`
	Rel            string   `json:"rel,omitempty"`
	Icon           string   `json:"icon,omitempty"`
	IconOnly       string   `json:"iconOnly,omitempty"`       // "always", "mobile", "desktop", or "" (never)
	AppliesTo      []int    `json:"appliesTo"`
	Fields         []string `json:"fields,omitempty"`
	HasCount       bool     `json:"hasCount,omitempty"`       // Show count (inferred from action name: reply→replyCount)
	Toggleable     bool     `json:"toggleable,omitempty"`     // Can toggle off on re-click (bookmark, mute)
	GroupWith      string   `json:"groupWith,omitempty"`      // Appears in another action's dropdown
	RequiresWallet bool     `json:"requiresWallet,omitempty"` // Requires wallet connection (zap)
	Amounts        []int    `json:"amounts,omitempty"`        // Preset amounts for zap action (in sats)
}

// GetTitleKey returns the i18n key, deriving from action name if not explicitly set
func (a ActionConfig) GetTitleKey(actionName string) string {
	if a.TitleKey != "" {
		return a.TitleKey
	}
	return "action." + actionName
}

// KindOverride allows specific kinds to have custom action sets
type KindOverride struct {
	Actions []string `json:"actions"`
	Comment string   `json:"comment,omitempty"`
}

var (
	actionsConfig     *ActionsConfig
	actionsConfigMu   sync.RWMutex
	actionsConfigOnce sync.Once
)

// GetActionsConfig returns the current actions configuration (thread-safe)
func GetActionsConfig() *ActionsConfig {
	// Use sync.Once for initial load (most common case after startup)
	actionsConfigOnce.Do(func() {
		actionsConfigMu.Lock()
		defer actionsConfigMu.Unlock()
		if actionsConfig == nil {
			actionsConfig = loadActionsConfigFromFile()
		}
	})

	actionsConfigMu.RLock()
	defer actionsConfigMu.RUnlock()
	return actionsConfig
}

// ReloadActionsConfig reloads the configuration from file
func ReloadActionsConfig() error {
	newConfig := loadActionsConfigFromFile()
	actionsConfigMu.Lock()
	defer actionsConfigMu.Unlock()
	actionsConfig = newConfig
	slog.Info("actions configuration reloaded")
	return nil
}

func loadActionsConfigFromFile() *ActionsConfig {
	// Try to load from file
	configPath := os.Getenv("ACTIONS_CONFIG")
	if configPath == "" {
		configPath = "config/actions.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("config file not found, using defaults", "path", configPath)
		} else {
			slog.Warn("could not read config, using defaults", "path", configPath, "error", err)
		}
		return getDefaultActionsConfig()
	}

	var config ActionsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		slog.Error("invalid JSON in config, using defaults", "path", configPath, "error", err)
		return getDefaultActionsConfig()
	}

	slog.Info("loaded actions configuration", "path", configPath)
	return &config
}

// getDefaultActionsConfig returns the embedded default configuration
func getDefaultActionsConfig() *ActionsConfig {
	return &ActionsConfig{
		Actions: map[string]ActionConfig{
			"reply": {
				Method:    "GET",
				Href:      "/thread/{event_id}",
				Class:     "action-reply",
				AppliesTo: []int{1, 20, 9735, 30311},
			},
			"repost": {
				Method:    "POST",
				Href:      "/repost",
				Class:     "action-repost",
				AppliesTo: []int{1, 20},
				Fields:    []string{"csrf_token", "event_id", "event_pubkey", "return_url"},
			},
			"quote": {
				Method:    "GET",
				Href:      "/quote/{event_id}",
				Class:     "action-quote",
				AppliesTo: []int{1, 20},
			},
			"react": {
				Method:    "POST",
				Href:      "/react",
				Class:     "action-react",
				AppliesTo: []int{1, 20, 9735, 30311},
				Fields:    []string{"csrf_token", "event_id", "event_pubkey", "return_url", "reaction"},
			},
			"bookmark": {
				Method:    "POST",
				Href:      "/bookmark",
				Class:     "action-bookmark",
				AppliesTo: []int{1, 20, 30023},
				Fields:    []string{"csrf_token", "event_id", "return_url", "action"},
			},
			"read": {
				Method:    "GET",
				Href:      "/thread/{event_id}",
				Class:     "action-read",
				AppliesTo: []int{30023},
			},
		},
		DisplayOrder: []string{"reply", "repost", "react", "quote", "bookmark"},
		KindOverrides: map[string]KindOverride{
			"30023": {
				Actions: []string{"read", "bookmark"},
			},
		},
		FieldDefaults: map[string]string{
			"reaction": "❤️",
		},
	}
}

// ConfigActionAppliesTo checks if an action applies to a kind using the config
func ConfigActionAppliesTo(actionName string, kind int) bool {
	config := GetActionsConfig()
	action, exists := config.Actions[actionName]
	if !exists {
		return false
	}
	for _, k := range action.AppliesTo {
		if k == kind {
			return true
		}
	}
	return false
}

// ConfigGetDisplayOrder returns the display order for actions
func ConfigGetDisplayOrder() []string {
	config := GetActionsConfig()
	return config.DisplayOrder
}

// ConfigGetKindOverride returns the action override for a specific kind, if any
func ConfigGetKindOverride(kind int) ([]string, bool) {
	config := GetActionsConfig()
	override, exists := config.KindOverrides[fmt.Sprintf("%d", kind)]
	if !exists {
		return nil, false
	}
	return override.Actions, true
}

// ConfigActionHasGroupWith returns true if the action has groupWith set
func ConfigActionHasGroupWith(actionName string) bool {
	config := GetActionsConfig()
	actionCfg, exists := config.Actions[actionName]
	return exists && actionCfg.GroupWith != ""
}

// ConfigGetGroupedActions returns actions that have groupWith set and apply to the kind
// These are actions that appear in dropdowns under their parent action
func ConfigGetGroupedActions(kind int) []string {
	config := GetActionsConfig()
	var grouped []string
	for name, actionCfg := range config.Actions {
		if actionCfg.GroupWith == "" {
			continue
		}
		// Check if this action applies to the kind
		for _, k := range actionCfg.AppliesTo {
			if k == kind {
				grouped = append(grouped, name)
				break
			}
		}
	}
	return grouped
}

// ConfigBuildAction creates an ActionDefinition from config and context
func ConfigBuildAction(actionName string, ctx ActionContext) ActionDefinition {
	config := GetActionsConfig()
	actionCfg, exists := config.Actions[actionName]
	if !exists {
		return ActionDefinition{}
	}

	// Build href with placeholders replaced
	href := replaceActionPlaceholders(actionCfg.Href, ctx)

	// Build title (translate from i18n key, then apply dynamic modifications for toggleable actions)
	title := cfgpkg.I18n(actionCfg.GetTitleKey(actionName))
	if actionCfg.Toggleable && actionName == "bookmark" && ctx.IsBookmarked {
		title = cfgpkg.I18n("action.unbookmark")
	}
	if actionCfg.Toggleable && actionName == "mute" && ctx.IsMuted {
		title = cfgpkg.I18n("action.unmute")
	}

	// Don't show mute action for user's own events
	if actionName == "mute" && ctx.IsAuthor {
		return ActionDefinition{}
	}

	// Note: requiresWallet actions (zap) are shown to all logged-in users.
	// The handler redirects to wallet setup if no wallet is connected.

	// Determine completed state based on action type
	var completed bool
	switch actionName {
	case "react":
		completed = ctx.IsReacted
	case "repost":
		completed = ctx.IsReposted
	case "zap":
		completed = ctx.IsZapped
	case "bookmark":
		completed = ctx.IsBookmarked
	case "mute":
		completed = ctx.IsMuted
	}

	// Get count value based on action type (if hasCount is enabled)
	var count int
	if actionCfg.HasCount {
		switch actionName {
		case "reply":
			count = ctx.ReplyCount
		case "repost":
			count = ctx.RepostCount
		case "react":
			count = ctx.ReactionCount
		case "zap":
			count = int(ctx.ZapTotal)
		}
	}

	// Build fields using shared field value resolution
	var fields []FieldDefinition
	for _, fieldName := range actionCfg.Fields {
		field := FieldDefinition{
			Name:  fieldName,
			Type:  "hidden",
			Value: getFieldValue(fieldName, actionName, ctx),
		}
		fields = append(fields, field)
	}

	return ActionDefinition{
		Name:      actionName,
		Title:     title,
		Method:    actionCfg.Method,
		Href:      href,
		Class:     actionCfg.Class,
		Rel:       actionCfg.Rel,
		Icon:      actionCfg.Icon,
		IconOnly:  actionCfg.IconOnly,
		Fields:    fields,
		Completed: completed,
		Count:     count,
		HasCount:  actionCfg.HasCount,
		GroupWith: actionCfg.GroupWith,
		Amounts:   actionCfg.Amounts,
	}
}

// ConfigBuildLoggedOutAction creates a disabled action linking to login
func ConfigBuildLoggedOutAction(actionName string, ctx ActionContext) ActionDefinition {
	config := GetActionsConfig()
	actionCfg, exists := config.Actions[actionName]
	if !exists {
		return ActionDefinition{}
	}

	// Don't show wallet-requiring actions to logged-out users
	if actionCfg.RequiresWallet {
		return ActionDefinition{}
	}

	title := cfgpkg.I18n(actionCfg.GetTitleKey(actionName))

	return ActionDefinition{
		Name:      actionName,
		Title:     title,
		Method:    "GET",
		Href:      ctx.LoginURL,
		Class:     fmt.Sprintf("%s action-disabled", actionCfg.Class),
		Rel:       actionCfg.Rel,
		Icon:      actionCfg.Icon,
		IconOnly:  actionCfg.IconOnly,
		GroupWith: actionCfg.GroupWith, // Preserve grouping for proper dropdown display
	}
}
