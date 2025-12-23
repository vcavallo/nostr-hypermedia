package nostr

import (
	"net/url"
	"strings"

	"nostr-server/internal/util"
)

// NormalizeRelayURL validates and normalizes a relay URL from NIP-65 events
// Returns empty string if URL is invalid/malformed
func NormalizeRelayURL(relayURL string) string {
	// Trim whitespace
	relayURL = strings.TrimSpace(relayURL)
	if relayURL == "" {
		return ""
	}

	// Quick reject for obviously bad URLs (no colon = no protocol)
	if !strings.Contains(relayURL, "://") {
		return ""
	}

	// Reject URL-encoded spaces (indicates garbage text as URL)
	if strings.Contains(relayURL, "%20") || strings.Contains(relayURL, "+") {
		return ""
	}

	// Reject double protocols (wss://https://...)
	if strings.Count(relayURL, "://") > 1 {
		return ""
	}

	// Parse URL
	parsed, err := url.Parse(relayURL)
	if err != nil {
		return ""
	}

	// Must be ws:// or wss:// (not ww://, http://, etc)
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return ""
	}

	// Must have a valid hostname
	host := parsed.Hostname()
	if host == "" {
		return ""
	}

	// Reject hostnames that are clearly not relay URLs
	if len(host) < 3 {
		return ""
	}
	if !strings.Contains(host, ".") && host != "localhost" {
		return ""
	}
	if strings.Contains(host, " ") {
		return ""
	}
	// Block internal/unreachable hosts (.onion, .local, .internal)
	if util.IsInternalHost(host) {
		return ""
	}

	// Allow localhost for development
	if util.IsLoopbackHost(host) {
		// Normalize: strip trailing slash, lowercase
		result := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(host)
		if parsed.Port() != "" {
			result += ":" + parsed.Port()
		}
		if parsed.Path != "" && parsed.Path != "/" {
			result += parsed.Path
		}
		return result
	}

	// Normalize: strip trailing slash, lowercase
	result := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(host)
	if parsed.Port() != "" {
		result += ":" + parsed.Port()
	}
	if parsed.Path != "" && parsed.Path != "/" {
		result += parsed.Path
	}
	return result
}
