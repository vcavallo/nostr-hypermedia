package util

import "net/http"

// =============================================================================
// HTTP Response Helpers
// =============================================================================

// SetHTMLHeaders sets standard headers for HTML responses.
// maxAge is the Cache-Control max-age value in seconds (as string).
func SetHTMLHeaders(w http.ResponseWriter, maxAge string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age="+maxAge)
}

// WriteHTML writes an HTML string to the response writer.
// Returns any write error (usually safe to ignore for HTTP handlers).
func WriteHTML(w http.ResponseWriter, html string) error {
	_, err := w.Write([]byte(html))
	return err
}

// =============================================================================
// HTTP Error Helpers
// =============================================================================

// RespondBadRequest sends a 400 Bad Request error response.
func RespondBadRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}

// RespondUnauthorized sends a 401 Unauthorized error response.
func RespondUnauthorized(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusUnauthorized)
}

// RespondForbidden sends a 403 Forbidden error response.
func RespondForbidden(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusForbidden)
}

// RespondNotFound sends a 404 Not Found error response.
func RespondNotFound(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusNotFound)
}

// RespondMethodNotAllowed sends a 405 Method Not Allowed error response.
func RespondMethodNotAllowed(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusMethodNotAllowed)
}

// RespondInternalError sends a 500 Internal Server Error response.
func RespondInternalError(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusInternalServerError)
}

// RespondServiceUnavailable sends a 503 Service Unavailable error response.
func RespondServiceUnavailable(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusServiceUnavailable)
}
