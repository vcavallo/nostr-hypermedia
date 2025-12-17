package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LNURL-pay handling for Lightning payments

const (
	lnurlHTTPTimeout = 10 * time.Second
)

// validateExternalURL validates that a URL is safe to fetch (SSRF prevention)
func validateExternalURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %v", err)
	}

	// Only allow HTTPS (or HTTP for testing, but prefer HTTPS)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("invalid scheme: %s (expected https)", parsed.Scheme)
	}

	// Block localhost and common internal hostnames
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" ||
		host == "0.0.0.0" || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") {
		return errors.New("internal hosts not allowed")
	}

	// Block private IP ranges (basic check)
	if strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.2") ||
		strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.") ||
		strings.HasPrefix(host, "169.254.") {
		return errors.New("private IP ranges not allowed")
	}

	return nil
}

// LNURLPayInfo contains the payment endpoint info from initial LNURL fetch
type LNURLPayInfo struct {
	Callback       string   `json:"callback"`
	MinSendable    int64    `json:"minSendable"`    // millisats
	MaxSendable    int64    `json:"maxSendable"`    // millisats
	Metadata       string   `json:"metadata"`       // JSON stringified metadata
	Tag            string   `json:"tag"`            // should be "payRequest"
	AllowsNostr    bool     `json:"allowsNostr"`    // supports NIP-57 zaps
	NostrPubkey    string   `json:"nostrPubkey"`    // pubkey for zap receipts
	CommentAllowed int      `json:"commentAllowed"` // max comment length, 0 = no comments
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

// ResolveLNURLFromProfile extracts and resolves LNURL info from a profile's lud16/lud06
// Returns nil if no Lightning address is configured
func ResolveLNURLFromProfile(profile *ProfileInfo) (*LNURLPayInfo, error) {
	if profile == nil {
		return nil, errors.New("profile is nil")
	}

	// Try lud16 first (more common)
	if profile.Lud16 != "" {
		return ResolveLud16(profile.Lud16)
	}

	// Fall back to lud06
	if profile.Lud06 != "" {
		return ResolveLud06(profile.Lud06)
	}

	return nil, errors.New("no Lightning address configured")
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

	return fetchLNURLPayInfo(lnurlURL)
}

// ResolveLud06 decodes a bech32 LNURL and fetches the pay info
func ResolveLud06(lud06 string) (*LNURLPayInfo, error) {
	if !strings.HasPrefix(strings.ToLower(lud06), "lnurl1") {
		return nil, errors.New("invalid lud06: must start with lnurl1")
	}

	// Decode bech32
	hrp, data, err := bech32Decode(strings.ToLower(lud06))
	if err != nil {
		return nil, fmt.Errorf("failed to decode lnurl: %v", err)
	}
	if hrp != "lnurl" {
		return nil, errors.New("invalid lnurl hrp")
	}

	// Convert 5-bit to 8-bit
	urlBytes, err := bech32ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, fmt.Errorf("failed to convert lnurl bits: %v", err)
	}

	lnurlURL := string(urlBytes)
	return fetchLNURLPayInfo(lnurlURL)
}

// fetchLNURLPayInfo fetches the LNURL-pay info from the endpoint
func fetchLNURLPayInfo(lnurlURL string) (*LNURLPayInfo, error) {
	// Validate URL to prevent SSRF
	if err := validateExternalURL(lnurlURL); err != nil {
		return nil, fmt.Errorf("invalid lnurl: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), lnurlHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", lnurlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
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
	if err := validateExternalURL(info.Callback); err != nil {
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
	ctx, cancel := context.WithTimeout(context.Background(), lnurlHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", callbackURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create callback request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
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

// CanReceiveZaps checks if the profile can receive zaps (has lud16 or lud06)
func CanReceiveZaps(profile *ProfileInfo) bool {
	if profile == nil {
		return false
	}
	return profile.Lud16 != "" || profile.Lud06 != ""
}

// SatsToMsats converts satoshis to millisatoshis
func SatsToMsats(sats int64) int64 {
	return sats * 1000
}

// MsatsToSats converts millisatoshis to satoshis (rounds down)
func MsatsToSats(msats int64) int64 {
	return msats / 1000
}
