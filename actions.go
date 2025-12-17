package main

import (
	"os"
	"strings"
)

// disabledActions holds globally disabled actions (parsed from ACTIONS_DISABLE env var)
var disabledActions map[string]bool

func init() {
	disabledActions = make(map[string]bool)
	if disabled := os.Getenv("ACTIONS_DISABLE"); disabled != "" {
		for _, action := range strings.Split(disabled, ",") {
			action = strings.TrimSpace(strings.ToLower(action))
			if action != "" {
				disabledActions[action] = true
			}
		}
	}
}

// IsActionDisabled returns true if an action is globally disabled via ACTIONS_DISABLE
func IsActionDisabled(actionName string) bool {
	return disabledActions[actionName]
}

// ActionDefinition defines a possible action on an event.
// This is the canonical source of truth for both HTML and Siren rendering.
type ActionDefinition struct {
	Name      string            // Identifier: "reply", "react", "repost", "quote", "bookmark"
	Title     string            // Display text
	Method    string            // "GET" or "POST"
	Href      string            // URL pattern (populated per-event)
	Class     string            // CSS class for styling
	Rel       string            // Link relation for semantic meaning (e.g., "reply", "bookmark", "author")
	Icon      string            // Optional icon
	IconOnly  string            // "always", "mobile", "desktop", or "" (never) - controls icon-only display
	Fields    []FieldDefinition // Form fields (for POST actions)
	Disabled  bool              // If true, render as non-interactive text (deprecated, use Completed)
	Completed bool              // If true, action already performed (filled pill style, no-op on click)
	Count     int               // Count to display (if HasCount is true in config)
	HasCount  bool              // Whether to show count
	GroupWith string            // If set, this action appears in another action's dropdown
}

// FieldDefinition defines a form field for POST actions
type FieldDefinition struct {
	Name  string // Field name
	Type  string // "hidden", "text", "textarea"
	Value string // Field value
}

// ActionContext provides context for determining which actions apply
type ActionContext struct {
	EventID        string
	EventPubkey    string
	Kind           int
	IsBookmarked   bool
	IsReacted      bool  // Whether user has already reacted to this event
	IsReposted     bool  // Whether user has already reposted this event
	IsZapped       bool  // Whether user has already zapped this event
	IsMuted        bool  // Whether the event's author is in user's mute list
	ReplyCount     int   // Number of replies
	RepostCount    int   // Number of reposts
	ReactionCount  int   // Total reactions (consolidated, not by emoji)
	ZapTotal       int64 // Total zap amount in sats
	LoggedIn       bool
	HasWallet      bool  // Whether user has a wallet connected
	IsAuthor       bool
	CSRFToken      string
	ReturnURL      string
	LoginURL       string // URL to redirect to for login
}

// StandardActions returns the display order for actions (from config)
func StandardActions() []string {
	return ConfigGetDisplayOrder()
}

// ActionAppliesTo determines if an action applies to a given event kind
// Returns true if the action should be shown for this kind
func ActionAppliesTo(actionName string, kind int) bool {
	return ConfigActionAppliesTo(actionName, kind)
}

// buildAction creates an ActionDefinition for a specific action and context
func buildAction(actionName string, ctx ActionContext) ActionDefinition {
	return ConfigBuildAction(actionName, ctx)
}

// buildLoggedOutAction creates a disabled action that links to login
func buildLoggedOutAction(actionName string, ctx ActionContext) ActionDefinition {
	return ConfigBuildLoggedOutAction(actionName, ctx)
}

// GetActionsForEvent returns the list of actions available for an event
// based on its kind, login state, and other context
func GetActionsForEvent(ctx ActionContext) []ActionDefinition {
	var actions []ActionDefinition

	// Check for kind-specific overrides (e.g., articles only show read + bookmark)
	if overrideActions, hasOverride := ConfigGetKindOverride(ctx.Kind); hasOverride {
		for _, actionName := range overrideActions {
			if IsActionDisabled(actionName) {
				continue
			}
			// For "read" action, always show it
			// For other actions, require login
			if actionName == "read" {
				actions = append(actions, buildAction(actionName, ctx))
			} else if ctx.LoggedIn {
				actions = append(actions, buildAction(actionName, ctx))
			}
		}
		// Also add registered actions for this kind
		actions = append(actions, getRegisteredActionsForKind(ctx)...)
		return actions
	}

	// For other kinds, iterate through standard actions
	for _, actionName := range StandardActions() {
		// Skip globally disabled actions
		if IsActionDisabled(actionName) {
			continue
		}

		// Skip actions with groupWith - they're added via ConfigGetGroupedActions
		if ConfigActionHasGroupWith(actionName) {
			continue
		}

		if !ActionAppliesTo(actionName, ctx.Kind) {
			continue
		}

		if ctx.LoggedIn {
			actions = append(actions, buildAction(actionName, ctx))
		} else {
			// Show logged-out version (links to login)
			actions = append(actions, buildLoggedOutAction(actionName, ctx))
		}
	}

	// Add grouped actions (those with groupWith set, not in displayOrder)
	for _, actionName := range ConfigGetGroupedActions(ctx.Kind) {
		if IsActionDisabled(actionName) {
			continue
		}
		if ctx.LoggedIn {
			actions = append(actions, buildAction(actionName, ctx))
		} else {
			actions = append(actions, buildLoggedOutAction(actionName, ctx))
		}
	}

	// Add programmatically registered actions for this kind
	actions = append(actions, getRegisteredActionsForKind(ctx)...)

	return actions
}

// getRegisteredActionsForKind returns actions registered via RegisterKindAction
func getRegisteredActionsForKind(ctx ActionContext) []ActionDefinition {
	var actions []ActionDefinition

	// Get actions registered for this specific kind
	kindActionNames := GetKindActions(ctx.Kind)
	for _, actionName := range kindActionNames {
		if IsActionDisabled(actionName) {
			continue
		}

		// Check if this action is already in config (avoid duplicates)
		if ConfigActionAppliesTo(actionName, ctx.Kind) {
			continue
		}

		// Get the registered action
		regAction, exists := GetRegisteredAction(actionName)
		if !exists {
			continue
		}

		// Check login requirement
		if regAction.RequiresLogin && !ctx.LoggedIn {
			// Build logged-out version
			actions = append(actions, buildLoggedOutRegisteredAction(regAction, ctx))
		} else {
			actions = append(actions, BuildRegisteredAction(regAction, ctx))
		}
	}

	return actions
}

// buildLoggedOutRegisteredAction creates a disabled action linking to login
func buildLoggedOutRegisteredAction(action *RegisteredAction, ctx ActionContext) ActionDefinition {
	return ActionDefinition{
		Name:   action.Name,
		Title:  I18n(action.Config.TitleKey),
		Method: "GET",
		Href:   ctx.LoginURL,
		Class:  action.Config.Class + " action-disabled",
	}
}

// ToSirenAction converts an ActionDefinition to a SirenAction
func (a ActionDefinition) ToSirenAction() SirenAction {
	var fields []SirenField
	for _, f := range a.Fields {
		fields = append(fields, SirenField{
			Name:  f.Name,
			Type:  f.Type,
			Value: f.Value,
		})
	}

	return SirenAction{
		Name:   a.Name,
		Title:  a.Title,
		Method: a.Method,
		Href:   a.Href,
		Type:   "application/x-www-form-urlencoded",
		Fields: fields,
	}
}

// ToHTMLAction converts an ActionDefinition to an HTMLAction
func (a ActionDefinition) ToHTMLAction() HTMLAction {
	var csrfToken string
	var fields []HTMLField
	for _, f := range a.Fields {
		if f.Name == "csrf_token" {
			csrfToken = f.Value
			continue // Don't include in fields - rendered explicitly in template
		}
		fields = append(fields, HTMLField{
			Name:  f.Name,
			Value: f.Value,
		})
	}

	return HTMLAction{
		Name:      a.Name,
		Title:     a.Title,
		Href:      a.Href,
		Method:    a.Method,
		Class:     a.Class,
		Rel:       a.Rel,
		Icon:      a.Icon,
		IconOnly:  a.IconOnly,
		CSRFToken: csrfToken,
		Fields:    fields,
		Disabled:  a.Disabled,
		Completed: a.Completed,
		Count:     a.Count,
		HasCount:  a.HasCount,
		GroupWith: a.GroupWith,
	}
}

// HTMLActionGroup represents a primary action with optional grouped children
type HTMLActionGroup struct {
	Primary  HTMLAction   // The primary action
	Children []HTMLAction // Grouped actions that appear in dropdown
	HasGroup bool         // Whether this action has grouped children
}

// GroupActionsForKind organizes actions into groups based on GroupWith config.
// Actions with GroupWith are placed under their parent action's dropdown.
// Returns a list of action groups (primary actions with optional children).
func GroupActionsForKind(actions []ActionDefinition, kind int) []HTMLActionGroup {
	// First pass: collect all actions and identify groupings
	actionMap := make(map[string]HTMLAction)
	groupChildren := make(map[string][]HTMLAction) // parent name -> children

	for _, action := range actions {
		htmlAction := action.ToHTMLAction()
		if htmlAction.Name == "" {
			continue
		}

		if htmlAction.GroupWith != "" {
			// This action is grouped under another
			groupChildren[htmlAction.GroupWith] = append(groupChildren[htmlAction.GroupWith], htmlAction)
		} else {
			// This is a primary action
			actionMap[htmlAction.Name] = htmlAction
		}
	}

	// Second pass: build groups in display order
	displayOrder := ConfigGetDisplayOrder()
	var groups []HTMLActionGroup
	processedActions := make(map[string]bool)

	for _, name := range displayOrder {
		action, exists := actionMap[name]
		if !exists {
			continue
		}

		group := HTMLActionGroup{
			Primary:  action,
			Children: groupChildren[name],
			HasGroup: len(groupChildren[name]) > 0,
		}
		groups = append(groups, group)
		processedActions[name] = true
	}

	// Third pass: add any remaining actions not in displayOrder (e.g., kindOverride-specific actions)
	// These are added at the beginning since they're typically kind-specific primary actions like "read"
	var extraGroups []HTMLActionGroup
	for name, action := range actionMap {
		if processedActions[name] {
			continue
		}
		group := HTMLActionGroup{
			Primary:  action,
			Children: groupChildren[name],
			HasGroup: len(groupChildren[name]) > 0,
		}
		extraGroups = append(extraGroups, group)
	}
	if len(extraGroups) > 0 {
		groups = append(extraGroups, groups...)
	}

	return groups
}
