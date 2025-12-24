package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// CSRFTokenMaxAge is the maximum age of a CSRF token before it expires
	// 30 minutes provides good security while allowing reasonable form completion time
	CSRFTokenMaxAge = 30 * time.Minute
)

// CSRFManager handles CSRF token generation and validation
type CSRFManager struct {
	secret []byte
}

// NewCSRFManager creates a new CSRF manager with the given secret
func NewCSRFManager(secret []byte) *CSRFManager {
	return &CSRFManager{secret: secret}
}

// NewCSRFManagerWithRandomSecret creates a new CSRF manager with a random secret
func NewCSRFManagerWithRandomSecret() (*CSRFManager, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate CSRF secret: %w", err)
	}
	return &CSRFManager{secret: secret}, nil
}

// GenerateToken creates a signed CSRF token for the given session ID
// Format: timestamp.signature (base64 encoded)
func (m *CSRFManager) GenerateToken(sessionID string) string {
	timestamp := time.Now().Unix()
	signature := m.computeSignature(sessionID, timestamp)
	return fmt.Sprintf("%d.%s", timestamp, signature)
}

// ValidateToken checks if a CSRF token is valid for the given session ID
func (m *CSRFManager) ValidateToken(sessionID string, token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	// Check if token has expired
	if time.Now().Unix()-timestamp > int64(CSRFTokenMaxAge.Seconds()) {
		return false
	}

	// Verify signature
	expectedSignature := m.computeSignature(sessionID, timestamp)
	return hmac.Equal([]byte(parts[1]), []byte(expectedSignature))
}

// computeSignature generates the HMAC signature for a session ID and timestamp
func (m *CSRFManager) computeSignature(sessionID string, timestamp int64) string {
	data := fmt.Sprintf("%s.%d", sessionID, timestamp)

	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}
