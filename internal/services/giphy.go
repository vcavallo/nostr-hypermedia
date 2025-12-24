package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"nostr-server/internal/util"
)

// GiphyClient handles communication with the Giphy API
type GiphyClient struct {
	apiKey string
	client *http.Client
}

// GifResult represents a single GIF from Giphy
type GifResult struct {
	ID       string
	Title    string
	URL      string // Full size URL for posting
	ThumbURL string // Thumbnail for picker grid
	Width    int
	Height   int
}

// giphyResponse represents the JSON response from Giphy API
type giphyResponse struct {
	Data []giphyGif `json:"data"`
}

type giphyGif struct {
	ID     string      `json:"id"`
	Title  string      `json:"title"`
	Images giphyImages `json:"images"`
}

type giphyImages struct {
	Original        giphyImage `json:"original"`
	FixedWidthSmall giphyImage `json:"fixed_width_small"`
	FixedWidth      giphyImage `json:"fixed_width"`
}

type giphyImage struct {
	URL    string `json:"url"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

// NewGiphyClient creates a new Giphy API client
func NewGiphyClient(apiKey string) *GiphyClient {
	return &GiphyClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 5 * time.Second,
			},
		},
	}
}

// Search searches for GIFs matching the query
func (c *GiphyClient) Search(query string, limit int) ([]GifResult, error) {
	if c == nil {
		return nil, fmt.Errorf("giphy client not initialized")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("q", query)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("rating", "g") // Safe for all audiences

	apiURL := fmt.Sprintf("%s/search?%s", util.GiphyAPIBaseURL, params.Encode())

	resp, err := c.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("giphy API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("giphy API returned status %d", resp.StatusCode)
	}

	var giphyResp giphyResponse
	if err := json.NewDecoder(resp.Body).Decode(&giphyResp); err != nil {
		return nil, fmt.Errorf("failed to decode giphy response: %w", err)
	}

	results := make([]GifResult, 0, len(giphyResp.Data))
	for _, gif := range giphyResp.Data {
		results = append(results, GifResult{
			ID:       gif.ID,
			Title:    gif.Title,
			URL:      gif.Images.Original.URL,
			ThumbURL: gif.Images.FixedWidthSmall.URL,
		})
	}

	return results, nil
}
