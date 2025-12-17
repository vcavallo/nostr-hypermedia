package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
)

// RelaysConfig represents the JSON configuration for relay lists
type RelaysConfig struct {
	DefaultRelays      []string `json:"defaultRelays"`
	NostrConnectRelays []string `json:"nostrConnectRelays"`
	SearchRelays       []string `json:"searchRelays"`
	PublishRelays      []string `json:"publishRelays"`
	ProfileRelays      []string `json:"profileRelays"`
}

var (
	relaysConfig     *RelaysConfig
	relaysConfigMu   sync.RWMutex
	relaysConfigOnce sync.Once
)

// GetRelaysConfig returns the current relays configuration (thread-safe)
func GetRelaysConfig() *RelaysConfig {
	// Use sync.Once for initial load (most common case after startup)
	relaysConfigOnce.Do(func() {
		relaysConfigMu.Lock()
		defer relaysConfigMu.Unlock()
		if relaysConfig == nil {
			relaysConfig = loadRelaysConfigFromFile()
		}
	})

	relaysConfigMu.RLock()
	defer relaysConfigMu.RUnlock()
	return relaysConfig
}

// ReloadRelaysConfig reloads the configuration from file
func ReloadRelaysConfig() error {
	newConfig := loadRelaysConfigFromFile()
	relaysConfigMu.Lock()
	defer relaysConfigMu.Unlock()
	relaysConfig = newConfig
	slog.Info("relays configuration reloaded")
	return nil
}

func loadRelaysConfigFromFile() *RelaysConfig {
	// Try to load from file
	configPath := os.Getenv("RELAYS_CONFIG")
	if configPath == "" {
		configPath = "config/relays.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("config file not found, using defaults", "path", configPath)
		} else {
			slog.Warn("could not read config, using defaults", "path", configPath, "error", err)
		}
		return getDefaultRelaysConfig()
	}

	var config RelaysConfig
	if err := json.Unmarshal(data, &config); err != nil {
		slog.Error("invalid JSON in config, using defaults", "path", configPath, "error", err)
		return getDefaultRelaysConfig()
	}

	slog.Info("loaded relays configuration",
		"path", configPath,
		"default", len(config.DefaultRelays),
		"nostrconnect", len(config.NostrConnectRelays),
		"search", len(config.SearchRelays),
		"publish", len(config.PublishRelays),
		"profile", len(config.ProfileRelays))
	return &config
}

// getDefaultRelaysConfig returns the embedded default configuration
func getDefaultRelaysConfig() *RelaysConfig {
	return &RelaysConfig{
		DefaultRelays: []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
			"wss://nos.lol",
			"wss://nostr.mom",
		},
		NostrConnectRelays: []string{
			"wss://relay.nsec.app",
			"wss://relay.damus.io",
		},
		SearchRelays: []string{
			"wss://relay.nostr.band",
		},
		PublishRelays: []string{
			"wss://relay.damus.io",
			"wss://relay.nostr.band",
			"wss://relay.primal.net",
		},
		ProfileRelays: []string{
			"wss://relay.nostr.band",
		},
	}
}

// ConfigGetDefaultRelays returns the default relay list for general queries
func ConfigGetDefaultRelays() []string {
	config := GetRelaysConfig()
	if len(config.DefaultRelays) > 0 {
		return config.DefaultRelays
	}
	return getDefaultRelaysConfig().DefaultRelays
}

// ConfigGetNostrConnectRelays returns the relay list for NIP-46 NostrConnect
func ConfigGetNostrConnectRelays() []string {
	config := GetRelaysConfig()
	if len(config.NostrConnectRelays) > 0 {
		return config.NostrConnectRelays
	}
	return getDefaultRelaysConfig().NostrConnectRelays
}

// ConfigGetSearchRelays returns the relay list for search queries
func ConfigGetSearchRelays() []string {
	config := GetRelaysConfig()
	if len(config.SearchRelays) > 0 {
		return config.SearchRelays
	}
	return getDefaultRelaysConfig().SearchRelays
}

// ConfigGetPublishRelays returns the relay list for publishing events
func ConfigGetPublishRelays() []string {
	config := GetRelaysConfig()
	if len(config.PublishRelays) > 0 {
		return config.PublishRelays
	}
	return getDefaultRelaysConfig().PublishRelays
}

// ConfigGetProfileRelays returns the relay list for profile lookups
func ConfigGetProfileRelays() []string {
	config := GetRelaysConfig()
	if len(config.ProfileRelays) > 0 {
		return config.ProfileRelays
	}
	return getDefaultRelaysConfig().ProfileRelays
}
