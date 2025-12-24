package main

import (
	"log/slog"
	"os"

	"nostr-server/internal/services"
)

// Type aliases for backward compatibility
type GiphyClient = services.GiphyClient
type GifResult = services.GifResult

// Function alias
var NewGiphyClient = services.NewGiphyClient

var giphyClient *GiphyClient

// initGiphy initializes the Giphy client if API key is available
func initGiphy() {
	apiKey := os.Getenv("GIPHY_API_KEY")
	if apiKey != "" {
		giphyClient = NewGiphyClient(apiKey)
		slog.Info("Giphy client initialized")
	} else {
		slog.Debug("Giphy disabled (no API key)")
	}
}

// GiphyEnabled returns true if Giphy API is configured
func GiphyEnabled() bool {
	return giphyClient != nil
}

// GetGiphyClient returns the global Giphy client (may be nil if not configured)
func GetGiphyClient() *GiphyClient {
	return giphyClient
}
