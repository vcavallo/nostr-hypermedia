package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
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

	// Block localhost variations before DNS lookup
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return nil, errors.New("connection to localhost blocked")
	}

	// Block obvious internal names
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return nil, errors.New("connection to internal host blocked")
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

	// Block localhost variations
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}

	// Resolve the hostname to check actual IPs
	ips, err := net.LookupIP(host)
	if err != nil {
		// If we can't resolve, allow it (might be valid external host)
		// but block obvious internal names
		if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
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

// FetchLinkPreview fetches OG metadata from a URL
func FetchLinkPreview(targetURL string) *LinkPreview {
	preview := &LinkPreview{
		URL:       targetURL,
		FetchedAt: time.Now(),
	}

	// SSRF protection: validate URL before fetching
	if !isURLSafeForSSRF(targetURL) {
		log.Printf("Link preview blocked for SSRF risk: %s", targetURL)
		preview.Failed = true
		return preview
	}

	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		preview.Failed = true
		return preview
	}

	// Set a reasonable User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NostrPreviewBot/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := previewHTTPClient.Do(req)
	if err != nil {
		log.Printf("Link preview fetch failed for %s: %v", targetURL, err)
		preview.Failed = true
		return preview
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Link preview got status %d for %s", resp.StatusCode, targetURL)
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
