package main

import (
	"fmt"
	"strings"
	"sync"
)

// ActionBuilder is a function that builds an ActionDefinition from context.
// This allows custom actions to have complex logic for field values, titles, etc.
type ActionBuilder func(ctx ActionContext) ActionDefinition

// RegisteredAction represents an action registered via the extension system
type RegisteredAction struct {
	Name          string        // Action identifier (e.g., "zap", "highlight")
	Config        ActionConfig  // Base configuration (title, method, href, class, fields)
	Builder       ActionBuilder // Optional custom builder (overrides config-based building)
	Priority      int           // Display priority (lower = earlier, default 100)
	RequiresLogin bool          // Whether action requires login (default true)
}

var (
	// registeredActions holds programmatically registered actions
	registeredActions   = make(map[string]*RegisteredAction)
	registeredActionsMu sync.RWMutex

	// kindActions maps kinds to additional registered action names
	kindActions   = make(map[int][]string)
	kindActionsMu sync.RWMutex
)

// GetRegisteredAction returns a registered action by name, if it exists
func GetRegisteredAction(name string) (*RegisteredAction, bool) {
	registeredActionsMu.RLock()
	defer registeredActionsMu.RUnlock()
	action, exists := registeredActions[name]
	return action, exists
}

// GetKindActions returns additional actions registered for a kind
func GetKindActions(kind int) []string {
	kindActionsMu.RLock()
	defer kindActionsMu.RUnlock()
	return kindActions[kind]
}

// RegisterKindAction registers a custom action for specific event kinds.
// This allows plugins/extensions to add actions without modifying config files.
// Example usage:
//
//	RegisterKindAction(&RegisteredAction{
//	    Name: "zap",
//	    Config: ActionConfig{TitleKey: "action.zap", Method: "POST", Href: "/html/zap"},
//	}, 1, 30023) // Register for notes and long-form
func RegisterKindAction(action *RegisteredAction, kinds ...int) {
	registeredActionsMu.Lock()
	registeredActions[action.Name] = action
	registeredActionsMu.Unlock()

	kindActionsMu.Lock()
	for _, kind := range kinds {
		kindActions[kind] = append(kindActions[kind], action.Name)
	}
	kindActionsMu.Unlock()
}

// BuildRegisteredAction builds an ActionDefinition from a registered action
func BuildRegisteredAction(action *RegisteredAction, ctx ActionContext) ActionDefinition {
	// If custom builder provided, use it
	if action.Builder != nil {
		return action.Builder(ctx)
	}

	// Otherwise, build from config similar to ConfigBuildAction
	return buildActionFromConfig(action.Name, action.Config, ctx)
}

// buildActionFromConfig creates an ActionDefinition from an ActionConfig
func buildActionFromConfig(name string, cfg ActionConfig, ctx ActionContext) ActionDefinition {
	// Build href with placeholders replaced
	href := cfg.Href
	href = replaceActionPlaceholders(href, ctx)

	// Build fields
	var fields []FieldDefinition
	for _, fieldName := range cfg.Fields {
		field := FieldDefinition{
			Name: fieldName,
			Type: "hidden",
		}
		field.Value = getFieldValue(fieldName, name, ctx)
		fields = append(fields, field)
	}

	return ActionDefinition{
		Name:   name,
		Title:  I18n(cfg.TitleKey),
		Method: cfg.Method,
		Href:   href,
		Class:  cfg.Class,
		Fields: fields,
	}
}

// replaceActionPlaceholders replaces {placeholders} in href with actual values
func replaceActionPlaceholders(href string, ctx ActionContext) string {
	href = strings.ReplaceAll(href, "{event_id}", ctx.EventID)
	href = strings.ReplaceAll(href, "{event_pubkey}", ctx.EventPubkey)
	return href
}

// getFieldValue returns the value for a field based on context.
// This is the canonical function for resolving field values - used by both
// config-based and registry-based action building.
func getFieldValue(fieldName string, actionName string, ctx ActionContext) string {
	config := GetActionsConfig()
	switch fieldName {
	case "csrf_token":
		return ctx.CSRFToken
	case "event_id":
		return ctx.EventID
	case "event_pubkey":
		return ctx.EventPubkey
	case "return_url":
		return ctx.ReturnURL
	case "reaction":
		if val, ok := config.FieldDefaults["reaction"]; ok && val != "" {
			return val
		}
		return "❤️"
	case "pubkey":
		return ctx.EventPubkey
	case "action":
		// For bookmark action
		if actionName == "bookmark" {
			if ctx.IsBookmarked {
				return "remove"
			}
			return "add"
		}
		// For mute action
		if actionName == "mute" {
			if ctx.IsMuted {
				return "unmute"
			}
			return "mute"
		}
		return ""
	case "kind":
		return fmt.Sprintf("%d", ctx.Kind)
	default:
		// Check field defaults in config
		if val, ok := config.FieldDefaults[fieldName]; ok {
			return val
		}
		return ""
	}
}
