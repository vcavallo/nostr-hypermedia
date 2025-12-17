package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// SiteConfig represents the site.json configuration for head tags and site identity
type SiteConfig struct {
	Site      SiteIdentity   `json:"site"`
	Meta      MetaConfig     `json:"meta"`
	OpenGraph OpenGraphConfig `json:"openGraph"`
	Links     LinksConfig    `json:"links"`
	Scripts   []ScriptConfig `json:"scripts"`
}

// SiteIdentity contains site-wide identity information
type SiteIdentity struct {
	Name        string `json:"name"`
	TitleFormat string `json:"titleFormat"` // e.g., "{title} - {siteName}"
	Description string `json:"description"`
}

// MetaConfig contains meta tag configurations
type MetaConfig struct {
	ThemeColor ThemeColorConfig `json:"themeColor"`
}

// ThemeColorConfig contains theme color for light/dark modes
type ThemeColorConfig struct {
	Light string `json:"light"`
	Dark  string `json:"dark"`
}

// OpenGraphConfig contains Open Graph defaults
type OpenGraphConfig struct {
	Type  string `json:"type"`
	Image string `json:"image"`
}

// LinksConfig contains link tags (favicon, stylesheets, preconnect)
type LinksConfig struct {
	Favicon    string   `json:"favicon"`
	Stylesheet string   `json:"stylesheet"`
	Preconnect []string `json:"preconnect"`
}

// ScriptConfig represents a script tag
type ScriptConfig struct {
	Src   string `json:"src"`
	Defer bool   `json:"defer,omitempty"`
	Async bool   `json:"async,omitempty"`
}

var (
	siteConfig     *SiteConfig
	siteConfigMu   sync.RWMutex
	siteConfigOnce sync.Once
)

// GetSiteConfig returns the current site configuration (thread-safe)
func GetSiteConfig() *SiteConfig {
	siteConfigOnce.Do(func() {
		siteConfigMu.Lock()
		defer siteConfigMu.Unlock()
		if siteConfig == nil {
			siteConfig = loadSiteConfigFromFile()
		}
	})

	siteConfigMu.RLock()
	defer siteConfigMu.RUnlock()
	return siteConfig
}

// ReloadSiteConfig reloads the configuration from file
func ReloadSiteConfig() error {
	newConfig := loadSiteConfigFromFile()
	siteConfigMu.Lock()
	defer siteConfigMu.Unlock()
	siteConfig = newConfig
	slog.Info("site configuration reloaded")
	return nil
}

func loadSiteConfigFromFile() *SiteConfig {
	configPath := os.Getenv("SITE_CONFIG")
	if configPath == "" {
		configPath = "config/site.json"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Debug("site config file not found, using defaults", "path", configPath)
		} else {
			slog.Warn("could not read site config, using defaults", "path", configPath, "error", err)
		}
		return getDefaultSiteConfig()
	}

	var config SiteConfig
	if err := json.Unmarshal(data, &config); err != nil {
		slog.Error("invalid JSON in site config, using defaults", "path", configPath, "error", err)
		return getDefaultSiteConfig()
	}

	slog.Info("loaded site configuration",
		"name", config.Site.Name,
		"preconnect", len(config.Links.Preconnect),
		"scripts", len(config.Scripts))
	return &config
}

// getDefaultSiteConfig returns the embedded default configuration
func getDefaultSiteConfig() *SiteConfig {
	return &SiteConfig{
		Site: SiteIdentity{
			Name:        "Nostr Hypermedia",
			TitleFormat: "{title} - {siteName}",
			Description: "A hypermedia Nostr client for the decentralized web",
		},
		Meta: MetaConfig{
			ThemeColor: ThemeColorConfig{
				Light: "#f5f5f5",
				Dark:  "#121212",
			},
		},
		OpenGraph: OpenGraphConfig{
			Type:  "website",
			Image: "/static/og-image.png",
		},
		Links: LinksConfig{
			Favicon:    "/static/favicon.ico",
			Stylesheet: "/static/style.css",
			Preconnect: []string{
				"https://nostr.build",
				"https://image.nostr.build",
				"https://void.cat",
				"https://blob.satellite.earth",
			},
		},
		Scripts: []ScriptConfig{
			{Src: "/static/helm.js", Defer: true},
		},
	}
}

// FormatTitle formats a page title using the configured format
func (c *SiteConfig) FormatTitle(title string) string {
	result := c.Site.TitleFormat
	result = strings.ReplaceAll(result, "{title}", title)
	result = strings.ReplaceAll(result, "{siteName}", c.Site.Name)
	return result
}

// GetDescription returns the page description, using override if provided
func (c *SiteConfig) GetDescription(override string) string {
	if override != "" {
		return override
	}
	return c.Site.Description
}

// GetOGImage returns the OG image URL, using override if provided
func (c *SiteConfig) GetOGImage(override string) string {
	if override != "" {
		return override
	}
	return c.OpenGraph.Image
}
