package main

import (
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// OG tag parsing regexes
var (
	ogTitleRegex       = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:title["'][^>]*content=["']([^"']+)["']`)
	ogTitleRegexAlt    = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:title["']`)
	ogDescRegex        = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:description["'][^>]*content=["']([^"']+)["']`)
	ogDescRegexAlt     = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:description["']`)
	ogImageRegex       = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:image["'][^>]*content=["']([^"']+)["']`)
	ogImageRegexAlt    = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:image["']`)
	ogSiteNameRegex    = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:site_name["'][^>]*content=["']([^"']+)["']`)
	ogSiteNameRegexAlt = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*property=["']og:site_name["']`)
	// Fallback to regular title tag
	titleTagRegex = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	// Fallback to meta description
	metaDescRegex    = regexp.MustCompile(`(?i)<meta[^>]*name=["']description["'][^>]*content=["']([^"']+)["']`)
	metaDescRegexAlt = regexp.MustCompile(`(?i)<meta[^>]*content=["']([^"']+)["'][^>]*name=["']description["']`)
)

// HTTP client with timeout for fetching previews
var previewHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// FetchLinkPreview fetches OG metadata from a URL
func FetchLinkPreview(url string) *LinkPreview {
	preview := &LinkPreview{
		URL:       url,
		FetchedAt: time.Now(),
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		preview.Failed = true
		return preview
	}

	// Set a reasonable User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NostrPreviewBot/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := previewHTTPClient.Do(req)
	if err != nil {
		log.Printf("Link preview fetch failed for %s: %v", url, err)
		preview.Failed = true
		return preview
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Link preview got status %d for %s", resp.StatusCode, url)
		preview.Failed = true
		return preview
	}

	// Only read first 50KB to find meta tags (they should be in <head>)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024))
	if err != nil {
		preview.Failed = true
		return preview
	}

	html := string(body)

	// Extract OG tags
	preview.Title = extractMeta(html, ogTitleRegex, ogTitleRegexAlt)
	preview.Description = extractMeta(html, ogDescRegex, ogDescRegexAlt)
	preview.Image = extractMeta(html, ogImageRegex, ogImageRegexAlt)
	preview.SiteName = extractMeta(html, ogSiteNameRegex, ogSiteNameRegexAlt)

	// Fallbacks
	if preview.Title == "" {
		if match := titleTagRegex.FindStringSubmatch(html); len(match) > 1 {
			preview.Title = strings.TrimSpace(match[1])
		}
	}
	if preview.Description == "" {
		preview.Description = extractMeta(html, metaDescRegex, metaDescRegexAlt)
	}

	// If we got nothing useful, mark as failed
	if preview.Title == "" && preview.Description == "" {
		preview.Failed = true
	}

	return preview
}

// extractMeta tries multiple regex patterns to extract a meta tag value
func extractMeta(html string, patterns ...*regexp.Regexp) string {
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(html); len(match) > 1 {
			return decodeHTMLEntities(strings.TrimSpace(match[1]))
		}
	}
	return ""
}

// decodeHTMLEntities decodes common HTML entities
func decodeHTMLEntities(s string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&apos;", "'",
		"&#x27;", "'",
		"&nbsp;", " ",
	)
	return replacer.Replace(s)
}

// FetchLinkPreviews fetches multiple link previews in parallel
func FetchLinkPreviews(urls []string) map[string]*LinkPreview {
	if len(urls) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := linkPreviewCache.GetMultiple(urls)
	if len(missing) == 0 {
		return cached
	}

	// Fetch missing previews in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]*LinkPreview)

	// Copy cached results
	for url, preview := range cached {
		results[url] = preview
	}

	// Limit concurrent fetches
	semaphore := make(chan struct{}, 5)

	for _, url := range missing {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			preview := FetchLinkPreview(u)
			linkPreviewCache.Set(u, preview)

			mu.Lock()
			results[u] = preview
			mu.Unlock()
		}(url)
	}

	wg.Wait()
	return results
}

// ExtractPreviewableURLs extracts URLs that should get link previews
// (excludes images, videos, YouTube, and nostr references)
func ExtractPreviewableURLs(content string) []string {
	var urls []string
	seen := make(map[string]bool)

	matches := urlRegex.FindAllString(content, -1)
	for _, url := range matches {
		// Skip if already seen
		if seen[url] {
			continue
		}
		seen[url] = true

		// Skip images
		if imageExtRegex.MatchString(url) {
			continue
		}
		// Skip videos
		if videoExtRegex.MatchString(url) {
			continue
		}
		// Skip YouTube (already embedded)
		if youtubeRegex.MatchString(url) {
			continue
		}
		// Skip audio files (already embedded)
		if audioExtRegex.MatchString(url) {
			continue
		}

		urls = append(urls, url)
	}

	return urls
}
