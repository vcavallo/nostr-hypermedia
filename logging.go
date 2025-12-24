package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Context key for request ID
type contextKey string

const requestIDKey contextKey = "request_id"

// InitLogger initializes the structured logger with JSON output
// Log level is controlled by LOG_LEVEL env var (debug/info/warn/error)
func InitLogger() {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	var level slog.Level
	switch levelStr {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})

	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Info("logger initialized", "level", level.String())
}

// generateRequestID creates a short random ID for request tracing
func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// RequestIDFromContext extracts request ID from context
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// LoggerFromContext returns a logger with request ID attached
func LoggerFromContext(ctx context.Context) *slog.Logger {
	reqID := RequestIDFromContext(ctx)
	if reqID != "" {
		return slog.Default().With("request_id", reqID)
	}
	return slog.Default()
}

// RequestLoggingMiddleware adds request ID and logs request/response
func RequestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip logging for health/metrics/static endpoints
		if r.URL.Path == "/health" || strings.HasPrefix(r.URL.Path, "/health/") || r.URL.Path == "/metrics" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		requestID := generateRequestID()

		// Add request ID to context
		ctx := context.WithValue(r.Context(), requestIDKey, requestID)
		r = r.WithContext(ctx)

		// Add request ID to response header
		w.Header().Set("X-Request-ID", requestID)

		// Wrap response writer to capture status code
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Log request start at debug level
		slog.Debug("request started",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
		)

		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)

		// Log at appropriate level based on status code
		attrs := []any{
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", duration.Milliseconds(),
		}

		if wrapped.statusCode >= 500 {
			httpErrorsTotal.Add(1)
			slog.Error("request failed", attrs...)
		} else if wrapped.statusCode >= 400 {
			slog.Warn("request error", attrs...)
		} else {
			slog.Debug("request completed", attrs...)
		}

		httpRequestsTotal.Add(1)
	})
}

// statusResponseWriter wraps http.ResponseWriter to capture status code
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher to support SSE streaming
func (w *statusResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
