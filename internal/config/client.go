package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

// ClientConfig represents the client.json configuration for NIP-89 client identification
type ClientConfig struct {
	Enabled    bool   `json:"enabled"`
	Name       string `json:"name"`
	Pubkey     string `json:"pubkey"`     // Hex pubkey for the client
	Dtag       string `json:"dtag"`       // d-tag value for 31990 event
	RelayHint  string `json:"relayHint"`  // Optional relay hint
	TagKinds   []int  `json:"tagKinds"`   // Which kinds get the client tag
	Version    string `json:"version"`    // Client version (for reference)
	NIP05Badge string `json:"nip05Badge"` // Badge/icon for verified NIP-05 identities
}

var (
	clientConfig     *ClientConfig
	clientConfigMu   sync.RWMutex
	clientConfigOnce sync.Once
)

// GetClientConfig returns the current client configuration (thread-safe)
func GetClientConfig() *ClientConfig {
	clientConfigOnce.Do(func() {
		clientConfigMu.Lock()
		defer clientConfigMu.Unlock()
		if clientConfig == nil {
			clientConfig = loadClientConfigFromFile()
		}
	})

	clientConfigMu.RLock()
	defer clientConfigMu.RUnlock()
	return clientConfig
}

// ReloadClientConfig reloads the configuration from file
func ReloadClientConfig() error {
	newConfig := loadClientConfigFromFile()
	clientConfigMu.Lock()
	defer clientConfigMu.Unlock()
	clientConfig = newConfig
	slog.Info("client configuration reloaded", "enabled", newConfig.Enabled)
	return nil
}

func loadClientConfigFromFile() *ClientConfig {
	configPath := os.Getenv("CLIENT_CONFIG")
	if configPath == "" {
		configPath = "config/client.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("client config file not found, using defaults", "path", configPath)
		} else {
			slog.Warn("could not read client config, using defaults", "path", configPath, "error", err)
		}
		return getDefaultClientConfig()
	}

	var config ClientConfig
	if err := json.Unmarshal(data, &config); err != nil {
		slog.Error("invalid JSON in client config, using defaults", "path", configPath, "error", err)
		return getDefaultClientConfig()
	}

	if config.Enabled {
		if config.Pubkey == "" || config.Pubkey == "hex-placeholder" {
			slog.Warn("client identification enabled but pubkey not configured")
		} else {
			slog.Info("loaded client configuration",
				"name", config.Name,
				"tagKinds", config.TagKinds)
		}
	} else {
		slog.Debug("client identification disabled")
	}

	return &config
}

// getDefaultClientConfig returns the embedded default configuration
func getDefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Enabled:    false,
		Name:       "nostr-hypermedia",
		Pubkey:     "",
		Dtag:       "nostr-hypermedia",
		RelayHint:  "",
		TagKinds:   []int{1, 6, 7},
		Version:    "0.1.0",
		NIP05Badge: "🛡️",
	}
}

// ShouldTagKind returns true if the given kind should have a client tag added
func (c *ClientConfig) ShouldTagKind(kind int) bool {
	if !c.Enabled || c.Pubkey == "" {
		return false
	}
	for _, k := range c.TagKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// GetClientTag returns the client tag to add to events, or nil if disabled
// Format: ["client", "31990:<pubkey>:<dtag>", "<relay-hint>"]
func (c *ClientConfig) GetClientTag() []string {
	if !c.Enabled || c.Pubkey == "" {
		return nil
	}

	reference := fmt.Sprintf("31990:%s:%s", c.Pubkey, c.Dtag)

	if c.RelayHint != "" {
		return []string{"client", reference, c.RelayHint}
	}
	return []string{"client", reference}
}

// GetNIP05Badge returns the badge/icon for verified NIP-05 identities
func (c *ClientConfig) GetNIP05Badge() string {
	if c.NIP05Badge != "" {
		return c.NIP05Badge
	}
	return "🛡️" // Default fallback
}

// GetNIP05Badge is a convenience function to get the NIP-05 badge from the global config
func GetNIP05Badge() string {
	return GetClientConfig().GetNIP05Badge()
}
