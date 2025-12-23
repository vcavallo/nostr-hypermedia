package main

import (
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

// HTTP metrics
var (
	httpRequestsTotal atomic.Int64
	httpErrorsTotal   atomic.Int64
)

// Relay metrics
var (
	droppedEventCount atomic.Int64
)

// Cache metrics
var (
	cacheHitsTotal   atomic.Int64
	cacheMissesTotal atomic.Int64
)

// SSE connection metrics
var (
	sseConnectionsActive atomic.Int64
)

// SSE connection tracking
func IncrementSSEConnections() {
	sseConnectionsActive.Add(1)
}

func DecrementSSEConnections() {
	sseConnectionsActive.Add(-1)
}

// IncrementCacheHit increments the cache hit counter
func IncrementCacheHit() {
	cacheHitsTotal.Add(1)
}

// IncrementCacheMiss increments the cache miss counter
func IncrementCacheMiss() {
	cacheMissesTotal.Add(1)
}

// GetConnectionStats returns current connection pool statistics
func (p *RelayPool) GetConnectionStats() (active int, max int) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.connections), maxTotalConnections
}

// RelayHealthDetail holds per-relay health information
type RelayHealthDetail struct {
	URL           string `json:"url"`
	Status        string `json:"status"` // "healthy" or "unhealthy"
	AvgResponseMs int64  `json:"avg_response_ms"`
	RequestCount  int64  `json:"request_count"`
}

// metricsHandler serves Prometheus-compatible metrics
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	// Build info metric
	fmt.Fprintf(w, "# HELP nostr_build_info Build and configuration information\n")
	fmt.Fprintf(w, "# TYPE nostr_build_info gauge\n")
	fmt.Fprintf(w, "nostr_build_info{cache_backend=%q,go_version=%q} 1\n\n", cacheBackendType, runtime.Version())

	// Process metrics
	fmt.Fprintf(w, "# HELP process_start_time_seconds Unix timestamp of process start\n")
	fmt.Fprintf(w, "# TYPE process_start_time_seconds gauge\n")
	fmt.Fprintf(w, "process_start_time_seconds %d\n\n", serverStartTime.Unix())

	fmt.Fprintf(w, "# HELP process_uptime_seconds Time since process started\n")
	fmt.Fprintf(w, "# TYPE process_uptime_seconds gauge\n")
	fmt.Fprintf(w, "process_uptime_seconds %.0f\n\n", time.Since(serverStartTime).Seconds())

	// Go runtime metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	fmt.Fprintf(w, "# HELP go_goroutines Number of active goroutines\n")
	fmt.Fprintf(w, "# TYPE go_goroutines gauge\n")
	fmt.Fprintf(w, "go_goroutines %d\n\n", runtime.NumGoroutine())

	fmt.Fprintf(w, "# HELP go_memstats_alloc_bytes Currently allocated memory in bytes\n")
	fmt.Fprintf(w, "# TYPE go_memstats_alloc_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_alloc_bytes %d\n\n", memStats.Alloc)

	fmt.Fprintf(w, "# HELP go_memstats_sys_bytes Total memory obtained from the OS\n")
	fmt.Fprintf(w, "# TYPE go_memstats_sys_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_sys_bytes %d\n\n", memStats.Sys)

	fmt.Fprintf(w, "# HELP go_memstats_heap_inuse_bytes Heap memory in use\n")
	fmt.Fprintf(w, "# TYPE go_memstats_heap_inuse_bytes gauge\n")
	fmt.Fprintf(w, "go_memstats_heap_inuse_bytes %d\n\n", memStats.HeapInuse)

	fmt.Fprintf(w, "# HELP go_gc_duration_seconds_total Total GC pause duration\n")
	fmt.Fprintf(w, "# TYPE go_gc_duration_seconds_total counter\n")
	fmt.Fprintf(w, "go_gc_duration_seconds_total %.6f\n\n", float64(memStats.PauseTotalNs)/1e9)

	fmt.Fprintf(w, "# HELP go_gc_cycles_total Number of completed GC cycles\n")
	fmt.Fprintf(w, "# TYPE go_gc_cycles_total counter\n")
	fmt.Fprintf(w, "go_gc_cycles_total %d\n\n", memStats.NumGC)

	// HTTP metrics
	fmt.Fprintf(w, "# HELP http_requests_total Total number of HTTP requests\n")
	fmt.Fprintf(w, "# TYPE http_requests_total counter\n")
	fmt.Fprintf(w, "http_requests_total %d\n\n", httpRequestsTotal.Load())

	fmt.Fprintf(w, "# HELP http_errors_total Total number of HTTP 5xx errors\n")
	fmt.Fprintf(w, "# TYPE http_errors_total counter\n")
	fmt.Fprintf(w, "http_errors_total %d\n\n", httpErrorsTotal.Load())

	// SSE metrics
	fmt.Fprintf(w, "# HELP sse_connections_active Number of active SSE connections\n")
	fmt.Fprintf(w, "# TYPE sse_connections_active gauge\n")
	fmt.Fprintf(w, "sse_connections_active %d\n\n", sseConnectionsActive.Load())

	// Connection pool metrics
	activeConns, maxConns := relayPool.GetConnectionStats()
	fmt.Fprintf(w, "# HELP nostr_relay_connections_active Number of active relay connections\n")
	fmt.Fprintf(w, "# TYPE nostr_relay_connections_active gauge\n")
	fmt.Fprintf(w, "nostr_relay_connections_active %d\n\n", activeConns)

	fmt.Fprintf(w, "# HELP nostr_relay_connections_max Maximum relay connections allowed\n")
	fmt.Fprintf(w, "# TYPE nostr_relay_connections_max gauge\n")
	fmt.Fprintf(w, "nostr_relay_connections_max %d\n\n", maxConns)

	// Relay health summary
	healthy, unhealthy, avgMs := relayHealthStore.GetRelayHealthStats()
	fmt.Fprintf(w, "# HELP nostr_relays_healthy Number of healthy relays\n")
	fmt.Fprintf(w, "# TYPE nostr_relays_healthy gauge\n")
	fmt.Fprintf(w, "nostr_relays_healthy %d\n\n", healthy)

	fmt.Fprintf(w, "# HELP nostr_relays_unhealthy Number of unhealthy relays\n")
	fmt.Fprintf(w, "# TYPE nostr_relays_unhealthy gauge\n")
	fmt.Fprintf(w, "nostr_relays_unhealthy %d\n\n", unhealthy)

	fmt.Fprintf(w, "# HELP nostr_relay_avg_response_ms Average relay response time in milliseconds\n")
	fmt.Fprintf(w, "# TYPE nostr_relay_avg_response_ms gauge\n")
	fmt.Fprintf(w, "nostr_relay_avg_response_ms %d\n\n", avgMs)

	// Per-relay metrics with labels
	relayDetails := relayHealthStore.GetRelayHealthDetails()
	if len(relayDetails) > 0 {
		fmt.Fprintf(w, "# HELP nostr_relay_response_ms Response time per relay in milliseconds\n")
		fmt.Fprintf(w, "# TYPE nostr_relay_response_ms gauge\n")
		for _, detail := range relayDetails {
			fmt.Fprintf(w, "nostr_relay_response_ms{relay=%q} %d\n", detail.URL, detail.AvgResponseMs)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP nostr_relay_requests_total Total requests per relay\n")
		fmt.Fprintf(w, "# TYPE nostr_relay_requests_total counter\n")
		for _, detail := range relayDetails {
			fmt.Fprintf(w, "nostr_relay_requests_total{relay=%q} %d\n", detail.URL, detail.RequestCount)
		}
		fmt.Fprintf(w, "\n")

		fmt.Fprintf(w, "# HELP nostr_relay_healthy Whether relay is healthy (1) or not (0)\n")
		fmt.Fprintf(w, "# TYPE nostr_relay_healthy gauge\n")
		for _, detail := range relayDetails {
			healthyVal := 0
			if detail.Status == "healthy" {
				healthyVal = 1
			}
			fmt.Fprintf(w, "nostr_relay_healthy{relay=%q} %d\n", detail.URL, healthyVal)
		}
		fmt.Fprintf(w, "\n")
	}

	// Event metrics
	fmt.Fprintf(w, "# HELP nostr_events_dropped_total Events dropped due to full channels\n")
	fmt.Fprintf(w, "# TYPE nostr_events_dropped_total counter\n")
	fmt.Fprintf(w, "nostr_events_dropped_total %d\n\n", droppedEventCount.Load())

	// Cache metrics
	cacheHits := cacheHitsTotal.Load()
	cacheMisses := cacheMissesTotal.Load()

	fmt.Fprintf(w, "# HELP cache_hits_total Total cache hits\n")
	fmt.Fprintf(w, "# TYPE cache_hits_total counter\n")
	fmt.Fprintf(w, "cache_hits_total %d\n\n", cacheHits)

	fmt.Fprintf(w, "# HELP cache_misses_total Total cache misses\n")
	fmt.Fprintf(w, "# TYPE cache_misses_total counter\n")
	fmt.Fprintf(w, "cache_misses_total %d\n\n", cacheMisses)

	// Cache hit ratio (useful for alerting)
	var hitRatio float64
	if total := cacheHits + cacheMisses; total > 0 {
		hitRatio = float64(cacheHits) / float64(total)
	}
	fmt.Fprintf(w, "# HELP cache_hit_ratio Cache hit ratio (0-1)\n")
	fmt.Fprintf(w, "# TYPE cache_hit_ratio gauge\n")
	fmt.Fprintf(w, "cache_hit_ratio %.4f\n", hitRatio)
}
