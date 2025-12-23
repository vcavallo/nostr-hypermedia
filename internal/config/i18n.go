package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// I18nStrings holds all localized strings
type I18nStrings map[string]string

var (
	i18nStrings   I18nStrings
	i18nMu        sync.RWMutex
	i18nConfigDir = getEnvOrDefault("I18N_CONFIG_DIR", "config/i18n")
	defaultLang   = getEnvOrDefault("I18N_DEFAULT_LANG", "en")
	i18nLoaded    bool
)

// InitI18n initializes the i18n system. Call this during startup.
func InitI18n() {
	if err := loadI18nConfig(); err != nil {
		slog.Warn("could not load i18n config", "error", err)
		i18nStrings = make(I18nStrings)
	}
}

func loadI18nConfig() error {
	i18nMu.Lock()
	defer i18nMu.Unlock()

	configPath := filepath.Join(i18nConfigDir, defaultLang+".json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", configPath)
		}
		return fmt.Errorf("could not read %s: %w", configPath, err)
	}

	var strings I18nStrings
	if err := json.Unmarshal(data, &strings); err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", configPath, err)
	}

	i18nStrings = strings
	i18nLoaded = true
	slog.Info("loaded i18n strings", "count", len(strings), "path", configPath)
	return nil
}

// ReloadI18nConfig reloads the i18n configuration from disk
func ReloadI18nConfig() error {
	return loadI18nConfig()
}

// I18n looks up a localized string by key
// Returns the key itself if not found (fallback behavior)
func I18n(key string) string {
	i18nMu.RLock()
	defer i18nMu.RUnlock()

	if val, ok := i18nStrings[key]; ok {
		return val
	}
	// Return key as fallback - makes missing translations visible
	return key
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
