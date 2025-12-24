package main

import (
	"crypto/rand"
	"net/http"
	"os"
	"sync"

	"nostr-server/internal/auth"
	"nostr-server/internal/util"
)

var (
	csrfManager     *auth.CSRFManager
	csrfManagerOnce sync.Once
)

// getCSRFManager returns the global CSRF manager, creating it if necessary
func getCSRFManager() *auth.CSRFManager {
	csrfManagerOnce.Do(func() {
		if secret := os.Getenv("CSRF_SECRET"); secret != "" {
			csrfManager = auth.NewCSRFManager([]byte(secret))
		} else {
			// Generate random secret for local/dev use
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				panic("failed to generate CSRF secret: " + err.Error())
			}
			csrfManager = auth.NewCSRFManager(secret)
		}
	})
	return csrfManager
}

// generateCSRFToken creates a signed CSRF token for the given session ID
func generateCSRFToken(sessionID string) string {
	return getCSRFManager().GenerateToken(sessionID)
}

// validateCSRFToken checks if a CSRF token is valid for the given session ID
func validateCSRFToken(sessionID string, token string) bool {
	return getCSRFManager().ValidateToken(sessionID, token)
}

// requireCSRF validates the CSRF token and writes an error response if invalid.
// Returns true if valid, false if invalid (and response already written).
func requireCSRF(w http.ResponseWriter, sessionID, token string) bool {
	if !validateCSRFToken(sessionID, token) {
		util.RespondForbidden(w, "Invalid or expired CSRF token")
		return false
	}
	return true
}
