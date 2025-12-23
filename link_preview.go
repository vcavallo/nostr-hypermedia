package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"nostr-server/internal/util"
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
	// Next.js RSC streaming format (OG tags delivered as JSON in script chunks)
	// Format: "property":"og:title","content":"Title Here"
	ogTitleRSC    = regexp.MustCompile(`"property"\s*:\s*"og:title"\s*,\s*"content"\s*:\s*"([^"]+)"`)
	ogDescRSC     = regexp.MustCompile(`"property"\s*:\s*"og:description"\s*,\s*"content"\s*:\s*"([^"]+)"`)
	ogImageRSC    = regexp.MustCompile(`"property"\s*:\s*"og:image"\s*,\s*"content"\s*:\s*"([^"]+)"`)
	ogSiteNameRSC = regexp.MustCompile(`"property"\s*:\s*"og:site_name"\s*,\s*"content"\s*:\s*"([^"]+)"`)
)

// ssrfSafeDialer creates connections only to public IPs, preventing DNS rebinding attacks
// by validating the IP at connection time rather than before the request.
var ssrfSafeDialer = &net.Dialer{
	Timeout:   5 * time.Second,
	KeepAlive: 30 * time.Second,
}

// ssrfSafeDialContext resolves DNS and validates the IP is public before connecting.
// This prevents DNS rebinding attacks by checking the IP at connection time.
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	// Block localhost and internal hosts before DNS lookup
	if util.IsPrivateHost(host) {
		return nil, errors.New("connection to private/internal host blocked")
	}

	// Resolve DNS
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("DNS lookup failed: %w", err)
	}

	if len(ips) == 0 {
		return nil, errors.New("no IP addresses found")
	}

	// Find a public IP to connect to
	for _, ip := range ips {
		if isPublicIP(ip) {
			// Connect using the validated IP directly (no second DNS lookup)
			return ssrfSafeDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
	}

	return nil, errors.New("all resolved IPs are private/internal")
}

// HTTP client with timeout for fetching previews
// Uses custom DialContext to prevent SSRF via DNS rebinding
var previewHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DialContext:           ssrfSafeDialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		// Note: redirect targets will be validated by ssrfSafeDialContext
		// when the new connection is established
		return nil
	},
}

// isURLSafeForSSRF checks if a URL is safe to fetch (not pointing to private/internal IPs)
func isURLSafeForSSRF(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Only allow http and https
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}

	host := parsed.Hostname()
	if host == "" {
		return false
	}

	// Block localhost and internal hosts
	if util.IsPrivateHost(host) {
		return false
	}

	// Resolve the hostname to check actual IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve, allow it (might be valid external host)
		// but block obvious internal names
		if util.IsInternalHost(host) {
			return false
		}
		return true
	}

	for _, ip := range ips {
		if !isPublicIP(ip) {
			return false
		}
	}

	return true
}

// isPublicIP returns true if the IP is a public (non-private, non-reserved) address
func isPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// Check for loopback
	if ip.IsLoopback() {
		return false
	}

	// Check for private networks
	if ip.IsPrivate() {
		return false
	}

	// Check for link-local (169.254.x.x for IPv4, fe80::/10 for IPv6)
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}

	// Check for unspecified (0.0.0.0 or ::)
	if ip.IsUnspecified() {
		return false
	}

	// Block cloud metadata IPs explicitly
	// AWS/GCP/Azure metadata: 169.254.169.254
	metadataIP := net.ParseIP("169.254.169.254")
	if ip.Equal(metadataIP) {
		return false
	}

	// Block multicast
	if ip.IsMulticast() {
		return false
	}

	return true
}

// FetchLinkPreview fetches OG metadata from a URL (uses default 5s timeout)
func FetchLinkPreview(targetURL string) *LinkPreview {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return FetchLinkPreviewWithContext(ctx, targetURL)
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

// htmlEntityReplacer is created once at startup to avoid allocation per call
var htmlEntityReplacer = strings.NewReplacer(
	"&amp;", "&",
	"&lt;", "<",
	"&gt;", ">",
	"&quot;", "\"",
	"&#39;", "'",
	"&apos;", "'",
	"&#x27;", "'",
	"&nbsp;", " ",
)

// decodeHTMLEntities decodes common HTML entities
func decodeHTMLEntities(s string) string {
	return htmlEntityReplacer.Replace(s)
}

// FetchLinkPreviews fetches multiple link previews in parallel with a global timeout.
// Returns available previews within timeout; slow URLs are skipped to avoid blocking.
func FetchLinkPreviews(urls []string) map[string]*LinkPreview {
	if len(urls) == 0 {
		return nil
	}

	// Check cache first
	cached, missing := linkPreviewCache.GetMultiple(urls)
	if len(missing) == 0 {
		return cached
	}

	// Global timeout for all fetches - don't let slow servers block the page
	const fetchTimeout = 5 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	// Fetch missing previews in parallel
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(map[string]*LinkPreview, len(urls))

	// Copy cached results
	for url, preview := range cached {
		results[url] = preview
	}

	// Limit concurrent fetches - balance between speed and not overwhelming targets
	semaphore := make(chan struct{}, 15)

	for _, url := range missing {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()

			// Acquire semaphore with context
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return // Timeout reached, skip this URL
			}

			// Check if we still have time
			if ctx.Err() != nil {
				return
			}

			preview := FetchLinkPreviewWithContext(ctx, u)
			if preview != nil {
				linkPreviewCache.Set(u, preview)
				mu.Lock()
				results[u] = preview
				mu.Unlock()
			}
		}(url)
	}

	// Wait for either all goroutines to finish or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All fetches completed
	case <-ctx.Done():
		// Timeout - return what we have
		slog.Debug("link preview fetch timeout", "completed", len(results)-len(cached), "total", len(missing))
	}

	return results
}

// FetchLinkPreviewWithContext fetches OG metadata from a URL with context support
func FetchLinkPreviewWithContext(ctx context.Context, targetURL string) *LinkPreview {
	preview := &LinkPreview{
		URL:       targetURL,
		FetchedAt: time.Now(),
	}

	// SSRF protection: validate URL before fetching
	if !isURLSafeForSSRF(targetURL) {
		slog.Warn("link preview blocked for SSRF risk", "url", targetURL)
		preview.Failed = true
		return preview
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		preview.Failed = true
		return preview
	}

	// Set a reasonable User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NostrPreviewBot/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := previewHTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			// Context cancelled/timeout - don't log as error
			return nil
		}
		slog.Debug("link preview fetch failed", "url", targetURL, "error", err)
		preview.Failed = true
		return preview
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("link preview got non-OK status", "url", targetURL, "status", resp.StatusCode)
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

	// Extract OG tags (standard HTML meta tags)
	preview.Title = extractMeta(html, ogTitleRegex, ogTitleRegexAlt)
	preview.Description = extractMeta(html, ogDescRegex, ogDescRegexAlt)
	preview.Image = extractMeta(html, ogImageRegex, ogImageRegexAlt)
	preview.SiteName = extractMeta(html, ogSiteNameRegex, ogSiteNameRegexAlt)

	// Fallback to Next.js RSC streaming format (JSON in script chunks)
	if preview.Title == "" {
		preview.Title = extractMeta(html, ogTitleRSC)
	}
	if preview.Description == "" {
		preview.Description = extractMeta(html, ogDescRSC)
	}
	if preview.Image == "" {
		preview.Image = extractMeta(html, ogImageRSC)
	}
	if preview.SiteName == "" {
		preview.SiteName = extractMeta(html, ogSiteNameRSC)
	}

	// Fallback to regular title tag
	if preview.Title == "" {
		if match := titleTagRegex.FindStringSubmatch(html); len(match) > 1 {
			preview.Title = strings.TrimSpace(match[1])
		}
	}
	// Fallback to meta description
	if preview.Description == "" {
		preview.Description = extractMeta(html, metaDescRegex, metaDescRegexAlt)
	}

	// If we got nothing useful, mark as failed
	if preview.Title == "" && preview.Description == "" {
		preview.Failed = true
	}

	return preview
}

// ExtractPreviewableURLs extracts URLs that should get link previews
// (excludes images, videos, YouTube, and nostr references)
func ExtractPreviewableURLs(content string) []string {
	matches := urlRegex.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}

	urls := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))

	for _, rawURL := range matches {
		// Clean trailing punctuation (e.g., from markdown links or prose)
		url := cleanURLTrailing(rawURL)

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
		// Skip YouTube videos (already embedded)
		if youtubeRegex.MatchString(url) {
			continue
		}
		// Skip YouTube playlists (already embedded)
		if youtubePlaylistRegex.MatchString(url) {
			continue
		}
		// Skip audio files (already embedded)
		if audioExtRegex.MatchString(url) {
			continue
		}
		// Note: We fetch link previews for all Wavlake URLs as fallback
		// Tracks will use NOM audio player if available, otherwise link preview

		urls = append(urls, url)
	}

	return urls
}
