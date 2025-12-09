package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	csrfTokenMaxAge = 1 * time.Hour
)

var (
	csrfSecret     []byte
	csrfSecretOnce sync.Once
)

// getCSRFSecret returns the CSRF secret, loading from env var or generating randomly
func getCSRFSecret() []byte {
	csrfSecretOnce.Do(func() {
		if secret := os.Getenv("CSRF_SECRET"); secret != "" {
			csrfSecret = []byte(secret)
		} else {
			// Generate random secret for local/dev use
			csrfSecret = make([]byte, 32)
			if _, err := rand.Read(csrfSecret); err != nil {
				panic("failed to generate CSRF secret: " + err.Error())
			}
		}
	})
	return csrfSecret
}

// generateCSRFToken creates a signed CSRF token for the given session ID
// Format: timestamp.signature (base64 encoded)
func generateCSRFToken(sessionID string) string {
	timestamp := time.Now().Unix()
	signature := computeCSRFSignature(sessionID, timestamp)
	return fmt.Sprintf("%d.%s", timestamp, signature)
}

// validateCSRFToken checks if a CSRF token is valid for the given session ID
func validateCSRFToken(sessionID string, token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}

	timestamp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	// Check if token has expired
	if time.Now().Unix()-timestamp > int64(csrfTokenMaxAge.Seconds()) {
		return false
	}

	// Verify signature
	expectedSignature := computeCSRFSignature(sessionID, timestamp)
	return hmac.Equal([]byte(parts[1]), []byte(expectedSignature))
}

// computeCSRFSignature generates the HMAC signature for a session ID and timestamp
func computeCSRFSignature(sessionID string, timestamp int64) string {
	secret := getCSRFSecret()
	data := fmt.Sprintf("%s.%d", sessionID, timestamp)

	h := hmac.New(sha256.New, secret)
	h.Write([]byte(data))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}
