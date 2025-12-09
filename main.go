package main

import (
	"log"
	"net/http"
	"os"
)

// Request body size limits
const (
	maxBodySize = 32 * 1024 // 32KB for POST requests
)

// limitBody wraps an HTTP handler to limit request body size
func limitBody(next http.HandlerFunc, maxBytes int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next(w, r)
	}
}

// securityHeaders wraps an HTTP handler to add security headers
func securityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy - defense in depth against XSS
		// - default-src 'self': only load resources from same origin by default
		// - img-src * data:: allow images from anywhere (user avatars, embedded images)
		// - media-src *: allow audio/video from anywhere
		// - frame-src youtube.com youtube-nocookie.com: allow YouTube embeds
		// - style-src 'self' 'unsafe-inline': allow inline styles for theming
		// - script-src 'self': only allow scripts from same origin
		csp := "default-src 'self'; " +
			"img-src * data:; " +
			"media-src *; " +
			"frame-src https://www.youtube.com https://www.youtube-nocookie.com; " +
			"style-src 'self' 'unsafe-inline'; " +
			"script-src 'self'"
		w.Header().Set("Content-Security-Policy", csp)

		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")

		// Prevent MIME type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Enable XSS filter in older browsers
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		// Referrer policy - don't leak full URLs to external sites
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		next(w, r)
	}
}

func main() {
	// Initialize templates at startup for better performance
	initTemplates()
	initAuthTemplates()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Serve static files
	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// API endpoints (these handle content negotiation internally)
	http.HandleFunc("/timeline", timelineHandler)
	http.HandleFunc("/thread/", threadHandler)

	// Root path redirects to HTML timeline, everything else 404
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/html/timeline?kinds=1&limit=20", http.StatusFound)
		} else {
			http.NotFound(w, r)
		}
	})
	// HTML handlers wrapped with security headers
	http.HandleFunc("/html/timeline", securityHeaders(htmlTimelineHandler))
	http.HandleFunc("/html/thread/", securityHeaders(htmlThreadHandler))
	http.HandleFunc("/html/profile/edit", securityHeaders(limitBody(htmlProfileEditHandler, maxBodySize)))
	http.HandleFunc("/html/profile/", securityHeaders(htmlProfileHandler))
	http.HandleFunc("/html/login", securityHeaders(limitBody(htmlLoginHandler, maxBodySize)))
	http.HandleFunc("/html/logout", securityHeaders(htmlLogoutHandler))
	http.HandleFunc("/html/post", securityHeaders(limitBody(htmlPostNoteHandler, maxBodySize)))
	http.HandleFunc("/html/reply", securityHeaders(limitBody(htmlReplyHandler, maxBodySize)))
	http.HandleFunc("/html/react", securityHeaders(limitBody(htmlReactHandler, maxBodySize)))
	http.HandleFunc("/html/bookmark", securityHeaders(limitBody(htmlBookmarkHandler, maxBodySize)))
	http.HandleFunc("/html/repost", securityHeaders(limitBody(htmlRepostHandler, maxBodySize)))
	http.HandleFunc("/html/follow", securityHeaders(limitBody(htmlFollowHandler, maxBodySize)))
	http.HandleFunc("/html/quote/", securityHeaders(htmlQuoteHandler))
	http.HandleFunc("/html/check-connection", securityHeaders(htmlCheckConnectionHandler))
	http.HandleFunc("/html/reconnect", securityHeaders(htmlReconnectHandler))
	http.HandleFunc("/html/theme", securityHeaders(htmlThemeHandler))
	http.HandleFunc("/html/notifications", securityHeaders(htmlNotificationsHandler))
	http.HandleFunc("/health", healthHandler)

	// Start NIP-46 connection listener for nostrconnect:// flow
	StartConnectionListener(defaultNostrConnectRelays)

	log.Printf("Starting server on :%s", port)
	log.Printf("Open http://localhost:%s in your browser", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
