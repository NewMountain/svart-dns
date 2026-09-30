package svart

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func resetDashboardStatsStateForTest() func() {
	oldNow := dashboardStatsNow
	dashboardStatsCache.clear()
	return func() {
		dashboardStatsNow = oldNow
		dashboardStatsCache.clear()
	}
}

func TestWrapDashboardStatsEndpointCachesSuccessfulGETs(t *testing.T) {
	defer resetDashboardStatsStateForTest()()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	dashboardStatsNow = func() time.Time { return now }

	callCount := 0
	handler := wrapDashboardStatsEndpoint("timeseries", true, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeJSON(w, http.StatusOK, map[string]int{"call": callCount})
	})

	req1 := httptest.NewRequest(http.MethodGet, "/api/stats/timeseries?window=24h&buckets=24", nil)
	rr1 := httptest.NewRecorder()
	handler(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d", rr1.Code)
	}
	if got := rr1.Header().Get("X-Svart-Stats-Cache"); got != "miss" {
		t.Fatalf("expected first request cache miss, got %q", got)
	}
	if got := rr1.Header().Get("X-Svart-Stats-Endpoint"); got != "timeseries" {
		t.Fatalf("expected endpoint header timeseries, got %q", got)
	}
	if got := rr1.Header().Get("X-Svart-Stats-Compute-Ms"); got == "" {
		t.Fatal("expected compute time header on first request")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/stats/timeseries?buckets=24&window=24h", nil)
	rr2 := httptest.NewRecorder()
	handler(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected second request 200, got %d", rr2.Code)
	}
	if got := rr2.Header().Get("X-Svart-Stats-Cache"); got != "hit" {
		t.Fatalf("expected second request cache hit, got %q", got)
	}
	if callCount != 1 {
		t.Fatalf("expected wrapped handler to run once, got %d", callCount)
	}
	if rr1.Body.String() != rr2.Body.String() {
		t.Fatalf("expected cached body %q to match first response %q", rr2.Body.String(), rr1.Body.String())
	}
}

func TestWrapDashboardStatsEndpointExpiresEntries(t *testing.T) {
	defer resetDashboardStatsStateForTest()()

	now := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	dashboardStatsNow = func() time.Time { return now }

	callCount := 0
	handler := wrapDashboardStatsEndpoint("top-clients", true, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeJSON(w, http.StatusOK, map[string]int{"call": callCount})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/top-clients?window=24h&limit=8", nil)
	rr1 := httptest.NewRecorder()
	handler(rr1, req)

	now = now.Add(dashboardStatsCacheTTL + time.Second)

	rr2 := httptest.NewRecorder()
	handler(rr2, req)

	if callCount != 2 {
		t.Fatalf("expected cache expiry to re-run handler, got %d calls", callCount)
	}
	if got := rr2.Header().Get("X-Svart-Stats-Cache"); got != "miss" {
		t.Fatalf("expected expired entry to produce miss, got %q", got)
	}
}

func TestWrapDashboardStatsEndpointDoesNotCacheErrors(t *testing.T) {
	defer resetDashboardStatsStateForTest()()

	callCount := 0
	handler := wrapDashboardStatsEndpoint("servfails", true, func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		writeError(w, http.StatusInternalServerError, "boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/stats/servfails?window=24h", nil)
	rr1 := httptest.NewRecorder()
	handler(rr1, req)
	rr2 := httptest.NewRecorder()
	handler(rr2, req)

	if callCount != 2 {
		t.Fatalf("expected error responses to bypass cache, got %d calls", callCount)
	}
	if got := rr1.Header().Get("X-Svart-Stats-Cache"); got != "bypass" {
		t.Fatalf("expected first error response bypass, got %q", got)
	}
	if got := rr2.Header().Get("X-Svart-Stats-Cache"); got != "bypass" {
		t.Fatalf("expected second error response bypass, got %q", got)
	}
}

func TestWrapDashboardStatsEndpointCachesActualStatsResponse(t *testing.T) {
	defer resetDashboardStatsStateForTest()()

	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec(`INSERT INTO query_logs (
		client_ip, query_name, query_type, response_code, blocked, upstream,
		latency_microseconds, result_reason, block_rule, block_list_id, coalesced_count
	) VALUES ('10.0.0.1', 'cached.example.', 'A', 'NOERROR', 0, 'cache', 1000, '', '', 0, 3)`); err != nil {
		t.Fatalf("failed to seed query log: %v", err)
	}

	handler := wrapDashboardStatsEndpoint("timeseries", true, handleAPIStatsTimeseries)

	req := httptest.NewRequest(http.MethodGet, "/api/stats/timeseries?window=24h&buckets=1", nil)
	rr1 := httptest.NewRecorder()
	handler(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request 200, got %d body=%s", rr1.Code, rr1.Body.String())
	}

	var first apiResponse
	if err := json.Unmarshal(rr1.Body.Bytes(), &first); err != nil {
		t.Fatalf("failed to decode first response: %v", err)
	}

	rr2 := httptest.NewRecorder()
	handler(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected second request 200, got %d body=%s", rr2.Code, rr2.Body.String())
	}
	if got := rr2.Header().Get("X-Svart-Stats-Cache"); got != "hit" {
		t.Fatalf("expected cached second response, got %q", got)
	}
	if rr1.Body.String() != rr2.Body.String() {
		t.Fatalf("expected cached timeseries body to match original response")
	}
}
