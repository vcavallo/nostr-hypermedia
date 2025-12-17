package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxBodySize = 32 * 1024 // 32KB max for POST requests

// CSP header (computed once at startup)
var precomputedCSP = "default-src 'self'; " +
	"img-src * data:; " +
	"media-src *; " +
	"frame-src https://www.youtube.com https://www.youtube-nocookie.com; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'"

// HSTS configuration (loaded from environment)
var (
	hstsEnabled bool
	hstsMaxAge  int
	hstsHeader  string
)

// Secure cookies configuration
// Priority: SECURE_COOKIES env > HSTS_ENABLED > auto-detect via localhost
var (
	secureCookiesSet   bool // whether SECURE_COOKIES was explicitly set
	secureCookiesValue bool // the explicit value if set
)

// Server runtime info for health checks
var serverStartTime = time.Now()

func init() {
	hstsEnabled = os.Getenv("HSTS_ENABLED") == "1"
	hstsMaxAge = 31536000 // Default: 1 year
	if maxAgeStr := os.Getenv("HSTS_MAX_AGE"); maxAgeStr != "" {
		if n, err := strconv.Atoi(maxAgeStr); err == nil && n > 0 {
			hstsMaxAge = n
		}
	}
	if hstsEnabled {
		hstsHeader = fmt.Sprintf("max-age=%d; includeSubDomains", hstsMaxAge)
		// Note: logged after InitLogger() in main()
	}

	// SECURE_COOKIES: explicit override for cookie Secure flag
	if v := os.Getenv("SECURE_COOKIES"); v != "" {
		secureCookiesSet = true
		secureCookiesValue = v == "1"
	}
}

// shouldSecureCookie determines if cookies should have the Secure flag.
// Priority: SECURE_COOKIES env var > HSTS_ENABLED > auto-detect via localhost
func shouldSecureCookie(r *http.Request) bool {
	if secureCookiesSet {
		return secureCookiesValue
	}
	if hstsEnabled {
		return true
	}
	return !isLocalhost(r)
}

// isLocalhost checks if the request is to localhost (for cookie Secure flag auto-detection)
func isLocalhost(r *http.Request) bool {
	host := r.Host
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

var (
	gzipEnabled = os.Getenv("GZIP_ENABLED") != ""
	gzipPool    = sync.Pool{
		New: func() interface{} {
			w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
			return w
		},
	}
)

const minGzipSize = 1024 // Skip gzip for small responses

// gzipResponseWriter wraps ResponseWriter to compress responses
type gzipResponseWriter struct {
	http.ResponseWriter
	gzipWriter *gzip.Writer
	buf        []byte
	statusCode int
	written    bool
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	if code >= 300 && code < 400 { // Redirects: write immediately
		w.ResponseWriter.WriteHeader(code)
		w.written = true
	}
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.buf = append(w.buf, b...) // Buffer to check size before compressing
		if len(w.buf) < minGzipSize {
			return len(b), nil
		}
		w.written = true
		w.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		w.ResponseWriter.Header().Del("Content-Length")
		w.ResponseWriter.WriteHeader(w.statusCode)
		if _, err := w.gzipWriter.Write(w.buf); err != nil {
			return 0, err
		}
		w.buf = nil
		return len(b), nil
	}
	return w.gzipWriter.Write(b)
}

func (w *gzipResponseWriter) finish() error {
	if !w.written && len(w.buf) > 0 { // Small response: skip compression
		w.ResponseWriter.WriteHeader(w.statusCode)
		_, err := w.ResponseWriter.Write(w.buf)
		return err
	}
	if w.written {
		return w.gzipWriter.Close()
	}
	return nil
}

// gzipMiddleware compresses responses when GZIP_ENABLED=1
func gzipMiddleware(next http.HandlerFunc) http.HandlerFunc {
	if !gzipEnabled {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next(w, r)
			return
		}

		w.Header().Set("Vary", "Accept-Encoding")
		gz := gzipPool.Get().(*gzip.Writer)
		gz.Reset(w)
		defer gzipPool.Put(gz)

		gzw := &gzipResponseWriter{
			ResponseWriter: w,
			gzipWriter:     gz,
			statusCode:     http.StatusOK,
		}

		next(gzw, r)
		gzw.finish()
	}
}

// limitBody limits request body size
func limitBody(next http.HandlerFunc, maxBytes int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		}
		next(w, r)
	}
}

// securityHeaders adds security headers to responses
func securityHeaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Panic recovery - log server-side, return generic error to client
		defer func() {
			if err := recover(); err != nil {
				slog.Error("panic recovered",
					"error", err,
					"method", r.Method,
					"path", r.URL.Path,
				)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		w.Header().Set("Content-Security-Policy", precomputedCSP)
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// HSTS - only set when explicitly enabled (requires HTTPS deployment)
		if hstsEnabled {
			w.Header().Set("Strict-Transport-Security", hstsHeader)
		}

		next(w, r)
	}
}

func main() {
	// Initialize structured logging first
	InitLogger()

	// Log HSTS config if enabled (deferred from init())
	if hstsEnabled {
		slog.Info("HSTS enabled", "max_age", hstsMaxAge)
	}

	// Initialize caches (Redis if REDIS_URL set, else memory)
	if err := InitCaches(); err != nil {
		slog.Error("failed to initialize caches", "error", err)
		os.Exit(1)
	}

	initTemplates()
	initAuthTemplates()
	initGiphy()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Static files (serves pre-compressed .gz when available)
	http.HandleFunc("/static/", staticFileHandler)

	// JSON API
	http.HandleFunc("/timeline", timelineHandler)
	http.HandleFunc("/thread/", threadHandler)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			session := getSessionFromRequest(r)
			if session != nil && session.Connected {
				http.Redirect(w, r, DefaultTimelineURLLoggedIn(), http.StatusFound)
			} else {
				http.Redirect(w, r, DefaultTimelineURL(), http.StatusFound)
			}
		} else {
			http.NotFound(w, r)
		}
	})

	// HTML pages
	http.HandleFunc("/html/timeline", gzipMiddleware(securityHeaders(htmlTimelineHandler)))
	http.HandleFunc("/html/thread/", gzipMiddleware(securityHeaders(htmlThreadHandler)))
	http.HandleFunc("/html/profile/edit", gzipMiddleware(securityHeaders(limitBody(htmlProfileEditHandler, maxBodySize))))
	http.HandleFunc("/html/profile/", gzipMiddleware(securityHeaders(htmlProfileHandler)))
	http.HandleFunc("/html/login", gzipMiddleware(securityHeaders(limitBody(htmlLoginHandler, maxBodySize))))
	http.HandleFunc("/html/logout", securityHeaders(htmlLogoutHandler))
	http.HandleFunc("/html/post", securityHeaders(limitBody(htmlPostNoteHandler, maxBodySize)))
	http.HandleFunc("/html/reply", securityHeaders(limitBody(htmlReplyHandler, maxBodySize)))
	http.HandleFunc("/html/react", securityHeaders(limitBody(htmlReactHandler, maxBodySize)))
	http.HandleFunc("/html/zap", securityHeaders(limitBody(htmlZapHandler, maxBodySize)))
	http.HandleFunc("/html/bookmark", securityHeaders(limitBody(htmlBookmarkHandler, maxBodySize)))
	http.HandleFunc("/html/mute", securityHeaders(limitBody(htmlMuteHandler, maxBodySize)))
	http.HandleFunc("/html/repost", securityHeaders(limitBody(htmlRepostHandler, maxBodySize)))
	http.HandleFunc("/html/follow", securityHeaders(limitBody(htmlFollowHandler, maxBodySize)))
	http.HandleFunc("/html/quote/", gzipMiddleware(securityHeaders(htmlQuoteHandler)))
	http.HandleFunc("/html/check-connection", securityHeaders(htmlCheckConnectionHandler))
	http.HandleFunc("/html/reconnect", securityHeaders(htmlReconnectHandler))
	http.HandleFunc("/html/theme", securityHeaders(htmlThemeHandler))
	http.HandleFunc("/html/notifications", gzipMiddleware(securityHeaders(htmlNotificationsHandler)))
	http.HandleFunc("/html/mutes", gzipMiddleware(securityHeaders(htmlMutesHandler)))
	http.HandleFunc("/html/wallet", gzipMiddleware(securityHeaders(htmlWalletHandler)))
	http.HandleFunc("/html/wallet/connect", securityHeaders(limitBody(htmlWalletConnectHandler, maxBodySize)))
	http.HandleFunc("/html/wallet/disconnect", securityHeaders(limitBody(htmlWalletDisconnectHandler, maxBodySize)))
	http.HandleFunc("/html/wallet/info", securityHeaders(htmlWalletInfoHandler))
	http.HandleFunc("/html/search", gzipMiddleware(securityHeaders(htmlSearchHandler)))
	http.HandleFunc("/html/timeline/check-new", securityHeaders(htmlCheckNewNotesHandler))
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/health/live", healthLiveHandler)
	http.HandleFunc("/health/ready", healthReadyHandler)
	http.HandleFunc("/metrics", metricsHandler)

	// GIF picker endpoints
	http.HandleFunc("/html/gifs", gzipMiddleware(securityHeaders(htmlGifsHandler)))
	http.HandleFunc("/html/gifs/search", gzipMiddleware(securityHeaders(htmlGifsSearchHandler)))
	http.HandleFunc("/html/gifs/select", securityHeaders(htmlGifsSelectHandler))
	http.HandleFunc("/html/gifs/clear", securityHeaders(htmlGifsClearHandler))
	http.HandleFunc("/html/gifs/close", securityHeaders(htmlGifsCloseHandler))
	http.HandleFunc("/html/compose", gzipMiddleware(securityHeaders(htmlComposeHandler)))

	// SSE
	http.HandleFunc("/stream/timeline", streamTimelineHandler)
	http.HandleFunc("/stream/notifications", securityHeaders(streamNotificationsHandler))
	http.HandleFunc("/stream/config", securityHeaders(streamConfigHandler))

	StartConnectionListener(defaultNostrConnectRelays()) // NIP-46 listener
	go WarmupConnections()                               // Warm up relays

	// SIGHUP reloads config
	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		for range sighup {
			slog.Info("received SIGHUP, reloading configuration")
			if err := ReloadActionsConfig(); err != nil {
				slog.Error("failed to reload actions config", "error", err)
			}
			if err := ReloadKindsConfig(); err != nil {
				slog.Error("failed to reload kinds config", "error", err)
			}
			if err := ReloadFeedsConfig(); err != nil {
				slog.Error("failed to reload feeds config", "error", err)
			}
			if err := ReloadRelaysConfig(); err != nil {
				slog.Error("failed to reload relays config", "error", err)
			}
			if err := ReloadNavigationConfig(); err != nil {
				slog.Error("failed to reload navigation config", "error", err)
			}
			if err := ReloadI18nConfig(); err != nil {
				slog.Error("failed to reload i18n config", "error", err)
			}
			if err := ReloadSiteConfig(); err != nil {
				slog.Error("failed to reload site config", "error", err)
			}
			// Notify connected clients to refresh
			BroadcastConfigReload()
		}
	}()

	slog.Info("starting server", "port", port, "gzip", gzipEnabled)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           RequestLoggingMiddleware(http.DefaultServeMux),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      60 * time.Second, // Higher for SSE connections
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1MB
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	healthy, unhealthy, avgMs := relayHealth.GetRelayHealthStats()
	activeConns, maxConns := relayPool.GetConnectionStats()
	cacheHits := cacheHitsTotal.Load()
	cacheMisses := cacheMissesTotal.Load()

	// Calculate cache hit rate
	var cacheHitRate float64
	if total := cacheHits + cacheMisses; total > 0 {
		cacheHitRate = float64(cacheHits) / float64(total) * 100
	}

	response := map[string]interface{}{
		"status": "ok",
		"server": map[string]interface{}{
			"uptime_seconds": int64(time.Since(serverStartTime).Seconds()),
			"started_at":     serverStartTime.Unix(),
			"timestamp":      time.Now().Unix(),
		},
		"relays": map[string]interface{}{
			"healthy":         healthy,
			"unhealthy":       unhealthy,
			"avg_response_ms": avgMs,
		},
		"connections": map[string]interface{}{
			"active": activeConns,
			"max":    maxConns,
		},
		"cache": map[string]interface{}{
			"backend":  cacheBackendType,
			"hits":     cacheHits,
			"misses":   cacheMisses,
			"hit_rate": fmt.Sprintf("%.1f%%", cacheHitRate),
		},
		"http": map[string]interface{}{
			"requests_total": httpRequestsTotal.Load(),
			"errors_total":   httpErrorsTotal.Load(),
		},
		"metrics": map[string]interface{}{
			"dropped_events": droppedEventCount.Load(),
		},
	}

	// Add verbose relay details if requested
	if r.URL.Query().Get("verbose") == "1" {
		response["relay_details"] = relayHealth.GetRelayHealthDetails()
	}

	// Determine status
	if healthy == 0 && unhealthy == 0 {
		// No stats yet - might be starting up, still OK
		response["status"] = "ok"
	} else if healthy == 0 {
		// All relays are in backoff - degraded
		response["status"] = "degraded"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("failed to encode health response", "error", err)
	}
}

// healthLiveHandler returns 200 if the server is running (liveness probe)
func healthLiveHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "ok",
		"uptime_seconds": int64(time.Since(serverStartTime).Seconds()),
	}); err != nil {
		slog.Error("failed to encode liveness response", "error", err)
	}
}

// healthReadyHandler returns 200 if ready to serve traffic (readiness probe)
func healthReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	healthy, unhealthy, _ := relayHealth.GetRelayHealthStats()

	response := map[string]interface{}{
		"status": "ready",
	}

	// Not ready if no healthy relays (after initial warmup)
	if healthy == 0 && unhealthy > 0 {
		response["status"] = "not_ready"
		response["reason"] = "no healthy relays"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("failed to encode readiness response", "error", err)
	}
}

// staticFileHandler serves static files (serves .gz versions when available)
func staticFileHandler(w http.ResponseWriter, r *http.Request) {
	filePath := strings.TrimPrefix(r.URL.Path, "/static/")
	if filePath == "" || strings.Contains(filePath, "..") {
		http.NotFound(w, r)
		return
	}

	fullPath := "./static/" + filePath

	// Serve pre-compressed .gz if available
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		gzPath := fullPath + ".gz"
		if _, err := os.Stat(gzPath); err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			setContentType(w, filePath)
			http.ServeFile(w, r, gzPath)
			return
		}
	}

	http.ServeFile(w, r, fullPath)
}

// setContentType sets Content-Type (needed because .gz files confuse http.ServeFile)
func setContentType(w http.ResponseWriter, originalPath string) {
	switch {
	case strings.HasSuffix(originalPath, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(originalPath, ".js"):
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case strings.HasSuffix(originalPath, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(originalPath, ".json"):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case strings.HasSuffix(originalPath, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	}
}
