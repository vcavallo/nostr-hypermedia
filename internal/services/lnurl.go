package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nostr-server/internal/nips"
	"nostr-server/internal/util"
)

// LNURL-pay handling for Lightning payments

const (
	LNURLHTTPTimeout = 10 * time.Second
)

// lnurlHTTPClient is a dedicated HTTP client for LNURL requests with proper timeouts
var lnurlHTTPClient = &http.Client{
	Timeout: LNURLHTTPTimeout,
	Transport: &http.Transport{
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// ValidateExternalURL validates that a URL is safe to fetch (SSRF prevention)
func ValidateExternalURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}

	// Only allow HTTPS (or HTTP for testing, but prefer HTTPS)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("invalid scheme: %s (expected https)", parsed.Scheme)
	}

	// Block localhost and common internal hostnames
	host := parsed.Hostname()
	if util.IsPrivateHost(host) || host == "0.0.0.0" {
		return errors.New("internal hosts not allowed")
	}

	// Block private IP ranges using proper IP parsing
	if ip := net.ParseIP(host); ip != nil {
		// Check standard private ranges
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return errors.New("private IP ranges not allowed")
		}
		// Block cloud metadata endpoint
		if ip.Equal(net.ParseIP("169.254.169.254")) {
			return errors.New("metadata endpoint not allowed")
		}
	} else {
		// Hostname-based checks for non-IP hosts
		if strings.HasPrefix(host, "10.") ||
			strings.HasPrefix(host, "192.168.") ||
			strings.HasPrefix(host, "169.254.") {
			return errors.New("private IP ranges not allowed")
		}
		// Check 172.16.0.0/12 range (172.16.0.0 - 172.31.255.255)
		if strings.HasPrefix(host, "172.") {
			parts := strings.Split(host, ".")
			if len(parts) >= 2 {
				if second := parts[1]; len(second) > 0 {
					// Parse second octet
					var octet int
					if _, err := fmt.Sscanf(second, "%d", &octet); err == nil {
						if octet >= 16 && octet <= 31 {
							return errors.New("private IP ranges not allowed")
						}
					}
				}
			}
		}
	}

	return nil
}

// LNURLPayInfo contains the payment endpoint info from initial LNURL fetch
type LNURLPayInfo struct {
	Callback       string `json:"callback"`
	MinSendable    int64  `json:"minSendable"`    // millisats
	MaxSendable    int64  `json:"maxSendable"`    // millisats
	Metadata       string `json:"metadata"`       // JSON stringified metadata
	Tag            string `json:"tag"`            // should be "payRequest"
	AllowsNostr    bool   `json:"allowsNostr"`    // supports NIP-57 zaps
	NostrPubkey    string `json:"nostrPubkey"`    // pubkey for zap receipts
	CommentAllowed int    `json:"commentAllowed"` // max comment length, 0 = no comments
}

// LNURLPayResponse contains the invoice from callback
type LNURLPayResponse struct {
	PR     string `json:"pr"`     // BOLT11 invoice
	Routes []any  `json:"routes"` // ignored
}

// LNURLError is returned on LNURL errors
type LNURLError struct {
	Status string `json:"status"` // "ERROR"
	Reason string `json:"reason"`
}

// ResolveLud16 resolves a Lightning address (user@domain.com) to LNURL pay info
func ResolveLud16(lud16 string) (*LNURLPayInfo, error) {
	// Parse email-like format
	parts := strings.SplitN(lud16, "@", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid lud16 format: expected user@domain")
	}
	username := parts[0]
	domain := parts[1]

	if username == "" || domain == "" {
		return nil, errors.New("invalid lud16: empty username or domain")
	}

	// Construct LNURL-pay URL
	// https://domain.com/.well-known/lnurlp/username
	lnurlURL := fmt.Sprintf("https://%s/.well-known/lnurlp/%s", domain, strings.ToLower(username))

	return FetchLNURLPayInfo(lnurlURL)
}

// ResolveLud06 decodes a bech32 LNURL and fetches the pay info
func ResolveLud06(lud06 string) (*LNURLPayInfo, error) {
	if !strings.HasPrefix(strings.ToLower(lud06), "lnurl1") {
		return nil, errors.New("invalid lud06: must start with lnurl1")
	}

	// Decode bech32
	hrp, data, err := nips.Bech32Decode(strings.ToLower(lud06))
	if err != nil {
		return nil, fmt.Errorf("failed to decode lnurl: %v", err)
	}
	if hrp != "lnurl" {
		return nil, errors.New("invalid lnurl hrp")
	}

	// Convert 5-bit to 8-bit
	urlBytes, err := nips.Bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert lnurl bits: %v", err)
	}

	lnurlURL := string(urlBytes)
	return FetchLNURLPayInfo(lnurlURL)
}

// FetchLNURLPayInfo fetches the LNURL-pay info from the endpoint
func FetchLNURLPayInfo(lnurlURL string) (*LNURLPayInfo, error) {
	// Validate URL to prevent SSRF
	if err := ValidateExternalURL(lnurlURL); err != nil {
		return nil, fmt.Errorf("invalid lnurl: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), LNURLHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", lnurlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := lnurlHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch lnurl: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("lnurl returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Check for error response
	var lnurlErr LNURLError
	if err := json.Unmarshal(body, &lnurlErr); err == nil && lnurlErr.Status == "ERROR" {
		return nil, fmt.Errorf("lnurl error: %s", lnurlErr.Reason)
	}

	var info LNURLPayInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse lnurl response: %v", err)
	}

	// Validate response
	if info.Tag != "payRequest" {
		return nil, fmt.Errorf("unexpected lnurl tag: %s (expected payRequest)", info.Tag)
	}
	if info.Callback == "" {
		return nil, errors.New("lnurl missing callback")
	}
	if info.MinSendable <= 0 || info.MaxSendable <= 0 {
		return nil, errors.New("lnurl missing amount limits")
	}

	return &info, nil
}

// RequestInvoice requests a BOLT11 invoice from the LNURL callback
// amountMsats is the payment amount in millisatoshis
// zapRequestJSON is an optional signed kind 9734 event JSON for NIP-57 zaps
// lnurl is the original LNURL (required for zap verification)
func RequestInvoice(info *LNURLPayInfo, amountMsats int64, zapRequestJSON string, lnurl string) (string, error) {
	// Validate callback URL to prevent SSRF
	if err := ValidateExternalURL(info.Callback); err != nil {
		return "", fmt.Errorf("invalid callback URL: %v", err)
	}

	// Validate amount
	if amountMsats < info.MinSendable {
		return "", fmt.Errorf("amount %d msats below minimum %d", amountMsats, info.MinSendable)
	}
	if amountMsats > info.MaxSendable {
		return "", fmt.Errorf("amount %d msats above maximum %d", amountMsats, info.MaxSendable)
	}

	// Build callback URL with amount
	callbackURL, err := url.Parse(info.Callback)
	if err != nil {
		return "", fmt.Errorf("invalid callback URL: %v", err)
	}

	query := callbackURL.Query()
	query.Set("amount", fmt.Sprintf("%d", amountMsats))

	// Add zap request if provided (NIP-57)
	// Note: caller should verify AllowsNostr before calling
	if zapRequestJSON != "" {
		query.Set("nostr", zapRequestJSON)
		if lnurl != "" {
			query.Set("lnurl", lnurl)
		}
	}

	callbackURL.RawQuery = query.Encode()

	// Fetch invoice
	ctx, cancel := context.WithTimeout(context.Background(), LNURLHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", callbackURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create callback request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := lnurlHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch invoice: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("callback returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read callback response: %v", err)
	}

	// Check for error response
	var lnurlErr LNURLError
	if err := json.Unmarshal(body, &lnurlErr); err == nil && lnurlErr.Status == "ERROR" {
		return "", fmt.Errorf("callback error: %s", lnurlErr.Reason)
	}

	var payResp LNURLPayResponse
	if err := json.Unmarshal(body, &payResp); err != nil {
		return "", fmt.Errorf("failed to parse callback response: %v", err)
	}

	if payResp.PR == "" {
		return "", errors.New("callback returned empty invoice")
	}

	return payResp.PR, nil
}

// MsatsToSats converts millisatoshis to satoshis (rounds down)
func MsatsToSats(msats int64) int64 {
	return msats / 1000
}
