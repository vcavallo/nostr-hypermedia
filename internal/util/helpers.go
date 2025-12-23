package util

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Template Compilation Helpers
// =============================================================================

// MustCompileTemplate compiles a template with the given name and content.
// Panics with a fatal error if compilation fails.
// This is used during initialization when template failures are unrecoverable.
func MustCompileTemplate(name string, funcs template.FuncMap, content string) *template.Template {
	t, err := template.New(name).Funcs(funcs).Parse(content)
	if err != nil {
		slog.Error("failed to compile template", "template", name, "error", err)
		os.Exit(1)
	}
	return t
}

// =============================================================================
// Host Validation Helpers
// =============================================================================

// IsInternalHost checks if a hostname is internal/private and should not be accessed.
// Used to prevent SSRF attacks by blocking requests to internal networks.
func IsInternalHost(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".onion") ||
		strings.HasSuffix(host, ".localhost")
}

// IsLoopbackHost checks if a hostname resolves to localhost.
func IsLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1" ||
		strings.HasPrefix(host, "127.") ||
		host == "[::1]"
}

// IsPrivateHost checks if a host should be blocked for security reasons.
// Combines internal host and loopback checks.
func IsPrivateHost(host string) bool {
	return IsInternalHost(host) || IsLoopbackHost(host)
}

// =============================================================================
// Tag Extraction Helpers
// =============================================================================

// GetTagValue returns the first value for the given tag name, or empty string if not found.
// Example: GetTagValue(tags, "e") returns the first event ID tag value.
func GetTagValue(tags [][]string, tagName string) string {
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == tagName {
			return tag[1]
		}
	}
	return ""
}

// GetLastTagValue returns the last value for the given tag name, or empty string if not found.
// Useful for "e" tags in replies where the last e-tag is typically the direct parent.
func GetLastTagValue(tags [][]string, tagName string) string {
	var result string
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == tagName {
			result = tag[1]
		}
	}
	return result
}

// GetTagValues returns all values for the given tag name.
// Example: GetTagValues(tags, "p") returns all mentioned pubkeys.
func GetTagValues(tags [][]string, tagName string) []string {
	var results []string
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == tagName {
			results = append(results, tag[1])
		}
	}
	return results
}

// HasTag returns true if the given tag name exists (even with empty value).
// Example: HasTag(tags, "content-warning") checks for NIP-36 content warning.
func HasTag(tags [][]string, tagName string) bool {
	for _, tag := range tags {
		if len(tag) >= 1 && tag[0] == tagName {
			return true
		}
	}
	return false
}

// =============================================================================
// Generic Map Utilities
// =============================================================================

// MapKeys returns all keys from a map as a slice.
// Order is not guaranteed (map iteration order).
func MapKeys[K comparable, V any](m map[K]V) []K {
	result := make([]K, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// =============================================================================
// Content Helpers
// =============================================================================

// AppendGifToContent appends a GIF URL to content with proper spacing.
// If content is empty, returns just the GIF URL.
// If gifURL is empty, returns the original content unchanged.
func AppendGifToContent(content, gifURL string) string {
	if gifURL == "" {
		return content
	}
	if content != "" {
		return content + "\n\n" + gifURL
	}
	return gifURL
}

// ExtractEmbeddedEventContent extracts the content field from embedded event JSON.
// Used for reposts (kind 6) where the actual text content is embedded as JSON.
// Returns empty string if parsing fails or content field is missing.
func ExtractEmbeddedEventContent(jsonContent string) string {
	var embedded struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(jsonContent), &embedded); err == nil {
		return embedded.Content
	}
	return ""
}

// ExtractEmbeddedEventTags extracts the tags field from embedded event JSON.
// Used for reposts (kind 6) to get tags like q (quote) from embedded events.
// Returns nil if parsing fails or tags field is missing.
func ExtractEmbeddedEventTags(jsonContent string) [][]string {
	var embedded struct {
		Tags [][]string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(jsonContent), &embedded); err == nil {
		return embedded.Tags
	}
	return nil
}

// ExtractEmbeddedEventPubkey extracts the pubkey field from embedded event JSON.
// Used for reposts (kind 6) to get the original author's pubkey for profile lookup.
// Returns empty string if parsing fails or pubkey field is missing.
func ExtractEmbeddedEventPubkey(jsonContent string) string {
	var embedded struct {
		Pubkey string `json:"pubkey"`
	}
	if err := json.Unmarshal([]byte(jsonContent), &embedded); err == nil {
		return embedded.Pubkey
	}
	return ""
}

// =============================================================================
// Binary Content Detection
// =============================================================================

// IsBinaryContent detects if content appears to be base64-encoded binary data
// rather than human-readable text. Used to filter out malformed content in
// file metadata events (kind 1063) where someone incorrectly put file data
// in the content field instead of a description.
func IsBinaryContent(content string) bool {
	if len(content) < 20 {
		return false
	}

	// Common base64 binary file headers
	binaryPrefixes := []string{
		"SUQz",     // ID3 (MP3)
		"AAAA",     // Many binary formats (MP4, etc.)
		"UklGR",    // RIFF (WAV, AVI)
		"iVBORw",   // PNG
		"/9j/",     // JPEG
		"R0lGOD",   // GIF
		"UEsDB",    // ZIP/DOCX/XLSX
		"JVBERi",   // PDF
		"H4sI",     // GZIP
		"d09HR",    // OGG
		"GkXfo",    // WebM/MKV
		"AAAAHGZ0", // ftyp (MP4/MOV)
	}

	for _, prefix := range binaryPrefixes {
		if strings.HasPrefix(content, prefix) {
			return true
		}
	}

	// Heuristic: base64 content has no spaces, mostly alphanumeric, and is long
	if len(content) > 100 && !strings.Contains(content[:100], " ") {
		alphanumCount := 0
		for _, r := range content[:100] {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '+' || r == '/' || r == '=' {
				alphanumCount++
			}
		}
		// If >95% of first 100 chars are base64 alphabet, likely binary
		if alphanumCount > 95 {
			return true
		}
	}

	return false
}

// =============================================================================
// URL Building Helpers
// =============================================================================

// URLParamOrder defines the canonical order for URL query parameters.
// Parameters are grouped semantically: resource -> filters -> pagination -> flags -> media -> auth
var URLParamOrder = []string{
	// Resource/feed type
	"feed", "pubkey", "target",
	// Content filters
	"kinds", "authors", "q", "filter", "relays",
	// Pagination
	"limit", "until", "since", "page", "offset",
	// Behavior flags
	"append", "refresh", "cache_only",
	// Media (GIF picker)
	"url", "thumb", "media_url", "media_thumb",
	// Auth/redirect
	"return_url", "return", "secret",
	// SSE/polling
	"format",
}

// urlParamIndex maps parameter names to their canonical position for O(1) lookup.
var urlParamIndex = func() map[string]int {
	idx := make(map[string]int, len(URLParamOrder))
	for i, name := range URLParamOrder {
		idx[name] = i
	}
	return idx
}()

// queryEscape encodes a string for use in a URL query parameter.
// Unlike url.QueryEscape, it leaves commas unencoded since they're
// safe in query strings per RFC 3986 (sub-delimiters).
func queryEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "%2C", ",")
}

// BuildURL constructs a URL with query parameters in canonical order.
// Empty values are omitted. Parameters not in the canonical order are appended alphabetically.
func BuildURL(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}

	var parts []string
	used := make(map[string]bool, len(params))

	// Add parameters in canonical order
	for _, key := range URLParamOrder {
		if val, ok := params[key]; ok && val != "" {
			parts = append(parts, url.QueryEscape(key)+"="+queryEscape(val))
			used[key] = true
		}
	}

	// Collect any remaining parameters not in canonical order
	var remaining []string
	for key := range params {
		if !used[key] && params[key] != "" {
			remaining = append(remaining, key)
		}
	}

	// Add remaining parameters in alphabetical order
	sort.Strings(remaining)
	for _, key := range remaining {
		parts = append(parts, url.QueryEscape(key)+"="+queryEscape(params[key]))
	}

	if len(parts) == 0 {
		return path
	}
	return path + "?" + strings.Join(parts, "&")
}

// IntsToParam converts an int slice to a comma-separated string for URL params.
// Example: []int{1, 6, 30023} -> "1,6,30023"
func IntsToParam(ints []int) string {
	if len(ints) == 0 {
		return ""
	}
	strs := make([]string, len(ints))
	for i, v := range ints {
		strs[i] = strconv.Itoa(v)
	}
	return strings.Join(strs, ",")
}

// IntsToStrings converts an int slice to a string slice.
// Example: []int{1, 6, 30023} -> []string{"1", "6", "30023"}
func IntsToStrings(ints []int) []string {
	strs := make([]string, len(ints))
	for i, v := range ints {
		strs[i] = strconv.Itoa(v)
	}
	return strs
}

// =============================================================================
// Slice Utilities
// =============================================================================

// LimitSlice returns the first n elements of a slice, or the entire slice if
// it has fewer than n elements. Safe to call with n <= 0 (returns empty slice).
func LimitSlice[T any](slice []T, n int) []T {
	if n <= 0 {
		return nil
	}
	if len(slice) <= n {
		return slice
	}
	return slice[:n]
}

// SortedCopy returns a sorted copy of a string slice.
// The original slice is not modified.
// Useful for building stable cache keys from unordered inputs.
func SortedCopy(slice []string) []string {
	if len(slice) == 0 {
		return nil
	}
	sorted := make([]string, len(slice))
	copy(sorted, slice)
	sort.Strings(sorted)
	return sorted
}

// =============================================================================
// String Utilities
// =============================================================================

// TruncateString truncates a string to maxLen characters, adding "..." suffix
// if truncation occurs. Returns original string if shorter than maxLen.
func TruncateString(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// TruncateStringRunes truncates a string to maxLen runes (Unicode-aware),
// adding "..." suffix if truncation occurs.
func TruncateStringRunes(s string, maxLen int) string {
	if maxLen <= 3 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// =============================================================================
// Filtering Helpers
// =============================================================================

// FilterSlice returns a new slice containing only elements that satisfy the predicate.
// The original slice is not modified.
func FilterSlice[T any](items []T, predicate func(T) bool) []T {
	result := make([]T, 0, len(items))
	for _, item := range items {
		if predicate(item) {
			result = append(result, item)
		}
	}
	return result
}

// FilterSliceInPlace filters a slice in place, returning the filtered slice.
// More memory efficient than FilterSlice for large slices where many items pass.
func FilterSliceInPlace[T any](items []T, predicate func(T) bool) []T {
	n := 0
	for _, item := range items {
		if predicate(item) {
			items[n] = item
			n++
		}
	}
	return items[:n]
}

// =============================================================================
// Concurrent Execution Helpers
// =============================================================================

// RunWithTimeout executes a function with a timeout.
// Returns true if the function completed, false if it timed out.
func RunWithTimeout(timeout time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() {
		fn()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// RunWithTimeoutCtx executes a function with a context that has a timeout.
// Returns true if completed, false if timed out. The function receives the context.
func RunWithTimeoutCtx(timeout time.Duration, fn func(ctx context.Context)) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		fn(ctx)
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
