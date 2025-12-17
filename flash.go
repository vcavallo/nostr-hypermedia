package main

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// Flash message cookie names
const (
	flashSuccessCookie = "flash_success"
	flashErrorCookie   = "flash_error"
)

// setFlashSuccess sets a success flash message cookie
func setFlashSuccess(w http.ResponseWriter, r *http.Request, message string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashSuccessCookie,
		Value:    url.QueryEscape(message),
		Path:     "/",
		MaxAge:   60, // 1 minute - plenty of time for redirect
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// setFlashError sets an error flash message cookie
func setFlashError(w http.ResponseWriter, r *http.Request, message string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashErrorCookie,
		Value:    url.QueryEscape(message),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		Secure:   shouldSecureCookie(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// FlashMessages holds success and error messages read from cookies
type FlashMessages struct {
	Success string
	Error   string
}

// getFlashMessages reads and clears flash message cookies
// Call this once per request, early in the handler
func getFlashMessages(w http.ResponseWriter, r *http.Request) FlashMessages {
	var messages FlashMessages

	// Read success cookie
	if cookie, err := r.Cookie(flashSuccessCookie); err == nil {
		if decoded, err := url.QueryUnescape(cookie.Value); err == nil {
			messages.Success = decoded
		}
		// Clear the cookie
		http.SetCookie(w, &http.Cookie{
			Name:     flashSuccessCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1, // Delete immediately
			HttpOnly: true,
		})
	}

	// Read error cookie
	if cookie, err := r.Cookie(flashErrorCookie); err == nil {
		if decoded, err := url.QueryUnescape(cookie.Value); err == nil {
			messages.Error = decoded
		}
		// Clear the cookie
		http.SetCookie(w, &http.Cookie{
			Name:     flashErrorCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})
	}

	return messages
}

// redirectWithSuccess redirects to a URL and sets a success flash message
func redirectWithSuccess(w http.ResponseWriter, r *http.Request, url string, message string) {
	setFlashSuccess(w, r, message)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// redirectWithError redirects to a URL and sets an error flash message
func redirectWithError(w http.ResponseWriter, r *http.Request, url string, message string) {
	setFlashError(w, r, message)
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// OOBFlashData holds data for the OOB flash template.
type OOBFlashData struct {
	Message string
	Type    string // "error" or "success"
}

// renderOOBFlash renders an OOB flash message using the compiled template
func renderOOBFlash(message, flashType string) string {
	data := OOBFlashData{Message: message, Type: flashType}
	var buf strings.Builder
	if err := cachedOOBFlash.ExecuteTemplate(&buf, "oob-flash", data); err != nil {
		slog.Error("failed to render OOB flash", "error", err)
		return ""
	}
	return buf.String()
}

// respondWithError handles errors for both HelmJS and regular requests.
// For HelmJS requests: returns OOB flash message HTML (no page duplication)
// For regular requests: redirects with flash cookie
//
// NOTE: This returns ONLY the OOB flash message. If the form targets a specific
// element (like #footer-{id}), use respondWithErrorAndFragment instead to avoid
// the target element disappearing.
func respondWithError(w http.ResponseWriter, r *http.Request, returnURL string, message string) {
	if isHelmRequest(r) {
		// Return OOB flash message update - HelmJS will swap it in
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(renderOOBFlash(message, "error")))
		return
	}
	// Regular request - redirect with flash cookie
	redirectWithError(w, r, returnURL, message)
}

// respondWithErrorAndFragment handles errors while preserving the target element.
// For HelmJS requests: returns the fragment HTML plus OOB error flash
// For regular requests: redirects with flash cookie
//
// Use this when the form targets a specific element that shouldn't disappear on error.
func respondWithErrorAndFragment(w http.ResponseWriter, r *http.Request, returnURL string, message string, fragmentHTML string) {
	if isHelmRequest(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fragmentHTML + renderOOBFlash(message, "error")))
		return
	}
	redirectWithError(w, r, returnURL, message)
}
