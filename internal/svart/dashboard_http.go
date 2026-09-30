package svart

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	dashboardStatsCacheTTL     = 30 * time.Second
	slowDashboardStatThreshold = 500 * time.Millisecond
)

var (
	dashboardStatsNow   = time.Now
	dashboardStatsCache = newDashboardStatsResponseCache(dashboardStatsCacheTTL)
)

type cachedDashboardResponse struct {
	status          int
	header          http.Header
	body            []byte
	expiresAt       time.Time
	computeDuration time.Duration
}

type dashboardStatsResponseCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	entries map[string]cachedDashboardResponse
}

func newDashboardStatsResponseCache(ttl time.Duration) *dashboardStatsResponseCache {
	return &dashboardStatsResponseCache{
		ttl:     ttl,
		entries: make(map[string]cachedDashboardResponse),
	}
}

func (c *dashboardStatsResponseCache) get(key string) (cachedDashboardResponse, bool) {
	now := dashboardStatsNow()

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return cachedDashboardResponse{}, false
	}
	if !entry.expiresAt.After(now) {
		c.mu.Lock()
		entry, ok = c.entries[key]
		if ok && !entry.expiresAt.After(now) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return cachedDashboardResponse{}, false
	}
	return entry, true
}

func (c *dashboardStatsResponseCache) put(key string, entry cachedDashboardResponse) {
	c.mu.Lock()
	c.entries[key] = entry
	if len(c.entries)%32 == 0 {
		c.pruneLocked(dashboardStatsNow())
	}
	c.mu.Unlock()
}

func (c *dashboardStatsResponseCache) clear() {
	c.mu.Lock()
	c.entries = make(map[string]cachedDashboardResponse)
	c.mu.Unlock()
}

func (c *dashboardStatsResponseCache) stats() (entries int, approxBytes int64) {
	now := dashboardStatsNow()
	c.mu.RLock()
	defer c.mu.RUnlock()

	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			continue
		}
		entries++
		approxBytes += int64(len(key))
		for name, values := range entry.header {
			approxBytes += int64(len(name))
			for _, value := range values {
				approxBytes += int64(len(value))
			}
		}
		approxBytes += int64(len(entry.body))
	}
	return entries, approxBytes
}

func (c *dashboardStatsResponseCache) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			delete(c.entries, key)
		}
	}
}

type dashboardResponseCapture struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newDashboardResponseCapture() *dashboardResponseCapture {
	return &dashboardResponseCapture{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (c *dashboardResponseCapture) Header() http.Header {
	return c.header
}

func (c *dashboardResponseCapture) Write(p []byte) (int, error) {
	return c.body.Write(p)
}

func (c *dashboardResponseCapture) WriteHeader(status int) {
	c.status = status
}

func dashboardStatsCacheKey(r *http.Request) string {
	if raw := r.URL.Query().Encode(); raw != "" {
		return r.URL.Path + "?" + raw
	}
	return r.URL.Path
}

func applyDashboardResponse(w http.ResponseWriter, endpoint, cacheState string, resp cachedDashboardResponse, includeBody bool) {
	for key, values := range resp.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	ms := float64(resp.computeDuration) / float64(time.Millisecond)
	w.Header().Set("X-Svart-Stats-Endpoint", endpoint)
	w.Header().Set("X-Svart-Stats-Cache", cacheState)
	w.Header().Set("X-Svart-Stats-Compute-Ms", fmt.Sprintf("%.3f", ms))
	w.Header().Set("Server-Timing", fmt.Sprintf("svart_stats;dur=%.3f;desc=\"%s %s\"", ms, endpoint, cacheState))
	w.WriteHeader(resp.status)
	if includeBody && len(resp.body) > 0 {
		if _, err := w.Write(resp.body); err != nil {
			logAdmin.Warn("write dashboard response failed", "error", err)
		}
	}
}

func observeDashboardStatRequest(endpoint, cacheState string, status int, duration time.Duration) {
	dashboardStatsRequestDuration.WithLabelValues(endpoint, cacheState).Observe(duration.Seconds())
	dashboardStatsRequestsTotal.WithLabelValues(endpoint, cacheState, strconv.Itoa(status)).Inc()
}

func wrapDashboardStatsEndpoint(endpoint string, cacheable bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			rec := newDashboardResponseCapture()
			start := time.Now()
			next(rec, r)
			duration := time.Since(start)
			resp := cachedDashboardResponse{
				status:          rec.status,
				header:          rec.header.Clone(),
				body:            append([]byte(nil), rec.body.Bytes()...),
				computeDuration: duration,
			}
			observeDashboardStatRequest(endpoint, "bypass", resp.status, duration)
			applyDashboardResponse(w, endpoint, "bypass", resp, r.Method != http.MethodHead)
			return
		}

		cacheKey := dashboardStatsCacheKey(r)
		if cacheable {
			if cached, ok := dashboardStatsCache.get(cacheKey); ok {
				observeDashboardStatRequest(endpoint, "hit", cached.status, 0)
				applyDashboardResponse(w, endpoint, "hit", cached, r.Method != http.MethodHead)
				return
			}
		}

		rec := newDashboardResponseCapture()
		start := time.Now()
		next(rec, r)
		duration := time.Since(start)

		cacheState := "bypass"
		resp := cachedDashboardResponse{
			status:          rec.status,
			header:          rec.header.Clone(),
			body:            append([]byte(nil), rec.body.Bytes()...),
			computeDuration: duration,
		}
		if cacheable && resp.status == http.StatusOK {
			resp.expiresAt = dashboardStatsNow().Add(dashboardStatsCache.ttl)
			dashboardStatsCache.put(cacheKey, resp)
			cacheState = "miss"
		}

		observeDashboardStatRequest(endpoint, cacheState, resp.status, duration)
		if resp.status == http.StatusOK && cacheState != "hit" && duration >= slowDashboardStatThreshold {
			logAdmin.Warn("slow dashboard stat",
				"endpoint", endpoint,
				"cache", cacheState,
				"duration_ms", duration.Milliseconds(),
				"query", r.URL.RawQuery,
			)
		}

		applyDashboardResponse(w, endpoint, cacheState, resp, r.Method != http.MethodHead)
	}
}
