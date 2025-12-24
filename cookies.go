package main

import (
	"net/http"
)

// =============================================================================
// Cookie Helpers
// =============================================================================

// SetCookie sets an HTTP cookie with standard security defaults.
// Uses the request to determine if the Secure flag should be set.
// Parameters:
//   - name: cookie name
//   - value: cookie value
//   - path: cookie path (use "/" for site-wide)
//   - maxAge: cookie lifetime in seconds (-1 to delete)
//   - sameSite: SameSite policy (http.SameSiteLaxMode or http.SameSiteStrictMode)
func SetCookie(w http.ResponseWriter, r *http.Request, name, value, path string, maxAge int, sameSite http.SameSite) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: sameSite,
	})
}

// SetSessionCookie sets a session cookie with strict security defaults.
// Uses SameSiteStrictMode and "/" path.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	SetCookie(w, r, name, value, "/", maxAge, http.SameSiteStrictMode)
}

// SetLaxCookie sets a cookie with lax security (allows cross-site top-level navigation).
// Uses SameSiteLaxMode and "/" path. Suitable for flash messages and preferences.
func SetLaxCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	SetCookie(w, r, name, value, "/", maxAge, http.SameSiteLaxMode)
}

// DeleteCookie deletes a cookie by setting MaxAge to -1.
func DeleteCookie(w http.ResponseWriter, r *http.Request, name, path string) {
	SetCookie(w, r, name, "", path, -1, http.SameSiteStrictMode)
}
