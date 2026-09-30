package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func withSystemStatsSamples(t *testing.T, samples []SystemSample) func() {
	t.Helper()

	systemStatsMu.Lock()
	oldBuffer := systemStatsBuffer
	oldHead := systemStatsHead
	oldCount := systemStatsCount
	oldStarted := systemStatsStarted

	systemStatsBuffer = [systemStatsBufferSize]SystemSample{}
	for i, sample := range samples {
		if i >= systemStatsBufferSize {
			break
		}
		systemStatsBuffer[i] = sample
	}
	systemStatsHead = len(samples) % systemStatsBufferSize
	if len(samples) > systemStatsBufferSize {
		systemStatsCount = systemStatsBufferSize
	} else {
		systemStatsCount = len(samples)
	}
	systemStatsStarted = false
	systemStatsMu.Unlock()

	return func() {
		systemStatsMu.Lock()
		systemStatsBuffer = oldBuffer
		systemStatsHead = oldHead
		systemStatsCount = oldCount
		systemStatsStarted = oldStarted
		systemStatsMu.Unlock()
	}
}

func TestHandleAPIStatsTimeseriesAggregate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	rows := []struct {
		clientIP            string
		queryName           string
		responseCode        string
		blocked             bool
		upstream            string
		latencyMicroseconds int
		resultReason        string
		blockRule           string
		blockListID         int
		coalescedCount      int
	}{
		{"10.0.0.1", "cached.example.", "NOERROR", false, "cache", 1500, "", "", 0, 3},
		{"10.0.0.1", "ads.example.", "NXDOMAIN", true, "", 0, "custom_block", "ads.example", 0, 2},
		{"10.0.0.2", "allow.example.", "NOERROR", false, "9.9.9.9:53", 2000, "custom_allow", "allow.example", 0, 4},
		{"10.0.0.2", "rewrite.example.", "NOERROR", false, "rewrite", 100, "rewrite", "", 0, 1},
		{"10.0.0.3", "fail.example.", "SERVFAIL", false, "9.9.9.9:53", 0, "", "", 0, 5},
	}

	for _, row := range rows {
		_, err := db.Exec(`INSERT INTO query_logs (
			client_ip, query_name, query_type, response_code, blocked, upstream,
			latency_microseconds, result_reason, block_rule, block_list_id, coalesced_count
		) VALUES (?, ?, 'A', ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.clientIP, row.queryName, row.responseCode, row.blocked, row.upstream,
			row.latencyMicroseconds, row.resultReason, row.blockRule, row.blockListID, row.coalescedCount,
		)
		if err != nil {
			t.Fatalf("failed to seed query log: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/timeseries?buckets=1", nil)
	rr := httptest.NewRecorder()
	handleAPIStatsTimeseries(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	type aggregate struct {
		TotalQueries           int64 `json:"total_queries"`
		BlockedQueries         int64 `json:"blocked_queries"`
		AllowedQueries         int64 `json:"allowed_queries"`
		AvgLatencyMicroseconds int64 `json:"avg_latency_microseconds"`
		ActiveClients          int64 `json:"active_clients"`
		CacheHits              int64 `json:"cache_hits"`
		ServfailCount          int64 `json:"servfail_count"`
		RewriteCount           int64 `json:"rewrite_count"`
		CustomBlocks           int64 `json:"custom_blocks"`
		CustomAllows           int64 `json:"custom_allows"`
		RewriteHits            int64 `json:"rewrite_hits"`
	}

	got := decodeAPIData[aggregate](t, rr)
	if got.TotalQueries != 15 {
		t.Fatalf("expected total queries 15, got %d", got.TotalQueries)
	}
	if got.BlockedQueries != 2 {
		t.Fatalf("expected blocked queries 2, got %d", got.BlockedQueries)
	}
	if got.AllowedQueries != 13 {
		t.Fatalf("expected allowed queries 13, got %d", got.AllowedQueries)
	}
	if got.AvgLatencyMicroseconds != 1200 {
		t.Fatalf("expected avg latency 1200us, got %d", got.AvgLatencyMicroseconds)
	}
	if got.ActiveClients != 3 {
		t.Fatalf("expected 3 active clients, got %d", got.ActiveClients)
	}
	if got.CacheHits != 3 {
		t.Fatalf("expected 3 cache hits, got %d", got.CacheHits)
	}
	if got.ServfailCount != 5 {
		t.Fatalf("expected 5 servfails, got %d", got.ServfailCount)
	}
	if got.RewriteCount != 1 || got.RewriteHits != 1 {
		t.Fatalf("expected rewrite count/hits to be 1, got count=%d hits=%d", got.RewriteCount, got.RewriteHits)
	}
	if got.CustomBlocks != 2 {
		t.Fatalf("expected custom blocks 2, got %d", got.CustomBlocks)
	}
	if got.CustomAllows != 4 {
		t.Fatalf("expected custom allows 4, got %d", got.CustomAllows)
	}
}

func TestHandleAPIStatsDashboardSnapshot(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES ('10.0.0.2', 'Kitchen Tablet')"); err != nil {
		t.Fatalf("failed to seed alias: %v", err)
	}

	rows := []struct {
		clientIP            string
		queryName           string
		responseCode        string
		blocked             bool
		upstream            string
		latencyMicroseconds int
		resultReason        string
		blockRule           string
		blockListID         int
		coalescedCount      int
	}{
		{"10.0.0.1", "cached.example.", "NOERROR", false, "cache", 1500, "", "", 0, 3},
		{"10.0.0.1", "ads.example.", "NXDOMAIN", true, "", 0, "custom_block", "ads.example", 0, 2},
		{"10.0.0.2", "allow.example.", "NOERROR", false, "9.9.9.9:53", 2000, "custom_allow", "allow.example", 0, 4},
		{"10.0.0.2", "rewrite.example.", "NOERROR", false, "rewrite", 100, "rewrite", "", 0, 1},
		{"10.0.0.3", "fail.example.", "SERVFAIL", false, "", 0, "", "", 0, 5},
	}

	for _, row := range rows {
		_, err := db.Exec(`INSERT INTO query_logs (
			client_ip, query_name, query_type, response_code, blocked, upstream,
			latency_microseconds, result_reason, block_rule, block_list_id, coalesced_count
		) VALUES (?, ?, 'A', ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.clientIP, row.queryName, row.responseCode, row.blocked, row.upstream,
			row.latencyMicroseconds, row.resultReason, row.blockRule, row.blockListID, row.coalescedCount,
		)
		if err != nil {
			t.Fatalf("failed to seed query log: %v", err)
		}
	}

	restoreSystemStats := withSystemStatsSamples(t, []SystemSample{
		{
			Timestamp:   time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC),
			CPUPercent:  10.5,
			RSSBytes:    512 * 1024 * 1024,
			HeapAlloc:   128 * 1024 * 1024,
			DBSizeBytes: 64 * 1024 * 1024,
		},
		{
			Timestamp:   time.Date(2026, 3, 29, 10, 0, 30, 0, time.UTC),
			CPUPercent:  18.4,
			RSSBytes:    576 * 1024 * 1024,
			HeapAlloc:   150 * 1024 * 1024,
			DBSizeBytes: 65 * 1024 * 1024,
		},
	})
	defer restoreSystemStats()

	req := httptest.NewRequest(http.MethodGet, "/api/stats/dashboard?window=24h&buckets=24&client_limit=8&domain_limit=10", nil)
	rr := httptest.NewRecorder()
	handleAPIStatsDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	got := decodeAPIData[DashboardSnapshot](t, rr)
	if got.Summary.TotalQueries != 15 {
		t.Fatalf("expected total queries 15, got %d", got.Summary.TotalQueries)
	}
	if got.Summary.BlockedQueries != 2 || got.Summary.AllowedQueries != 13 {
		t.Fatalf("unexpected blocked/allowed totals: %+v", got.Summary)
	}
	if len(got.Timeseries) == 0 {
		t.Fatal("expected dashboard timeseries data")
	}
	if len(got.Latency) == 0 {
		t.Fatal("expected dashboard latency data")
	}
	if len(got.TopClients) == 0 {
		t.Fatal("expected top client data")
	}

	foundAlias := false
	for _, client := range got.TopClients {
		if client.ClientIP == "10.0.0.2" && client.Alias == "Kitchen Tablet" {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Fatalf("expected aliased top client in snapshot, got %+v", got.TopClients)
	}

	if len(got.BlockSources) != 1 || got.BlockSources[0].ListName != "Custom Rule" || got.BlockSources[0].Count != 2 {
		t.Fatalf("unexpected block sources: %+v", got.BlockSources)
	}
	if len(got.UpstreamUsage) != 2 {
		t.Fatalf("expected two upstream usage entries, got %+v", got.UpstreamUsage)
	}
	if got.TopBlocked.Total != 2 || len(got.TopBlocked.Domains) != 1 || got.TopBlocked.Domains[0].Domain != "ads.example." {
		t.Fatalf("unexpected blocked top domains: %+v", got.TopBlocked)
	}
	if got.TopPermitted.Total != 13 {
		t.Fatalf("expected permitted total 13, got %+v", got.TopPermitted)
	}
	if len(got.Servfails) != 1 || got.Servfails[0].Count != 5 || got.Servfails[0].TopDomains[0].Domain != "fail.example." {
		t.Fatalf("unexpected servfail snapshot: %+v", got.Servfails)
	}
	if len(got.System) != 2 || got.System[1].RSSBytes != 576*1024*1024 {
		t.Fatalf("unexpected system samples: %+v", got.System)
	}
}

func TestHandleAPIBlocklistsUniqueDomainsLoadsRawDomainsOnDemand(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", "https://example.com/a.txt", "Small", true)
	if err != nil {
		t.Fatalf("failed to insert blocklist 1: %v", err)
	}
	bl1, fixtureErr8735 := result.LastInsertId()
	if fixtureErr8735 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr8735)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", bl1, "alpha.example.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	result, err = db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", "https://example.com/b.txt", "Large", true)
	if err != nil {
		t.Fatalf("failed to insert blocklist 2: %v", err)
	}
	bl2, fixtureErr9154 := result.LastInsertId()
	if fixtureErr9154 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr9154)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", bl2, "alpha.example.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", bl2, "beta.example.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	req := httptest.NewRequest(http.MethodGet, "/api/blocklists/unique-domains?ids="+strconv.FormatInt(bl1, 10)+","+strconv.FormatInt(bl2, 10), nil)
	rr := httptest.NewRecorder()
	handleAPIBlocklistsUniqueDomains(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	got := decodeAPIData[BlocklistUniqueStats](t, rr)
	if got.UniqueTotal != 2 {
		t.Fatalf("expected unique total 2, got %d", got.UniqueTotal)
	}
	if len(got.Lists) != 2 {
		t.Fatalf("expected 2 lists, got %d", len(got.Lists))
	}
	for _, list := range got.Lists {
		switch list.ID {
		case int(bl1):
			if list.Total != 1 || list.UniqueToSet != 1 {
				t.Fatalf("unexpected stats for list 1: %+v", list)
			}
		case int(bl2):
			if list.Total != 2 || list.UniqueToSet != 1 {
				t.Fatalf("unexpected stats for list 2: %+v", list)
			}
		default:
			t.Fatalf("unexpected list id in response: %+v", list)
		}
	}
}

func TestHandleAPIStatsTopDomainsExcludesUpstreamHostnames(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	_, err := db.Exec(`INSERT INTO query_logs (
		client_ip, query_name, query_type, response_code, blocked, upstream, coalesced_count
	) VALUES
		('10.42.1.42', 'dns.quad9.net.', 'A', 'NOERROR', 0, '9.9.9.9:53', 500),
		('10.42.1.42', 'real-user-domain.example.', 'A', 'NOERROR', 0, '9.9.9.9:53', 25)`)
	if err != nil {
		t.Fatalf("failed to seed query logs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/top-domains?blocked=false&limit=10", nil)
	rr := httptest.NewRecorder()
	handleAPIStatsTopDomains(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	type topDomain struct {
		Domain string `json:"domain"`
		Count  int64  `json:"count"`
	}
	type topDomainsResponse struct {
		Domains []topDomain `json:"domains"`
		Total   int64       `json:"total"`
	}

	got := decodeAPIData[topDomainsResponse](t, rr)
	if len(got.Domains) == 0 {
		t.Fatal("expected at least one top domain")
	}
	for _, domain := range got.Domains {
		if domain.Domain == "dns.quad9.net." {
			t.Fatalf("expected upstream hostname lookup to be excluded, got %+v", got.Domains)
		}
	}
	if got.Domains[0].Domain != "real-user-domain.example." {
		t.Fatalf("expected real user domain to lead results, got %+v", got.Domains[0])
	}
}

func TestHandleAPIStatsServfailsIncludesTopDomains(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES ('10.0.0.9', 'Kitchen Tablet')"); err != nil {
		t.Fatalf("failed to seed alias: %v", err)
	}

	rows := []struct {
		clientIP       string
		queryName      string
		coalescedCount int
	}{
		{"10.0.0.9", "api.example.", 4},
		{"10.0.0.9", "api.example.", 3},
		{"10.0.0.9", "cdn.example.", 2},
		{"10.0.0.10", "auth.example.", 5},
	}
	for _, row := range rows {
		_, err := db.Exec(`INSERT INTO query_logs (
			client_ip, query_name, query_type, response_code, blocked, coalesced_count
		) VALUES (?, ?, 'A', 'SERVFAIL', 0, ?)`, row.clientIP, row.queryName, row.coalescedCount)
		if err != nil {
			t.Fatalf("failed to seed servfail row: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/servfails?window=24h", nil)
	rr := httptest.NewRecorder()
	handleAPIStatsServfails(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	type domainCount struct {
		Domain string `json:"domain"`
		Count  int64  `json:"count"`
	}
	type servfailClient struct {
		ClientIP   string        `json:"client_ip"`
		Alias      string        `json:"alias"`
		Count      int64         `json:"count"`
		TopDomains []domainCount `json:"top_domains"`
	}

	got := decodeAPIData[[]servfailClient](t, rr)
	if len(got) != 2 {
		t.Fatalf("expected 2 client entries, got %d", len(got))
	}
	if got[0].ClientIP != "10.0.0.9" {
		t.Fatalf("expected busiest client first, got %+v", got[0])
	}
	if got[0].Alias != "Kitchen Tablet" {
		t.Fatalf("expected alias to be populated, got %+v", got[0])
	}
	if got[0].Count != 9 {
		t.Fatalf("expected client count 9, got %d", got[0].Count)
	}
	if len(got[0].TopDomains) < 2 {
		t.Fatalf("expected top domains for client, got %+v", got[0])
	}
	if got[0].TopDomains[0].Domain != "api.example." || got[0].TopDomains[0].Count != 7 {
		t.Fatalf("expected api.example. to lead with 7, got %+v", got[0].TopDomains[0])
	}
}

func TestHandleAPIStatsSystemReturnsChronologicalSamples(t *testing.T) {
	systemStatsMu.Lock()
	oldBuffer := systemStatsBuffer
	oldHead := systemStatsHead
	oldCount := systemStatsCount
	oldStarted := systemStatsStarted

	first := SystemSample{
		Timestamp:   time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC),
		CPUPercent:  10.5,
		RSSBytes:    512 * 1024 * 1024,
		HeapAlloc:   128 * 1024 * 1024,
		DBSizeBytes: 64 * 1024 * 1024,
	}
	second := SystemSample{
		Timestamp:   time.Date(2026, 3, 29, 10, 0, 30, 0, time.UTC),
		CPUPercent:  22.0,
		RSSBytes:    768 * 1024 * 1024,
		HeapAlloc:   196 * 1024 * 1024,
		DBSizeBytes: 65 * 1024 * 1024,
	}
	systemStatsBuffer = [systemStatsBufferSize]SystemSample{}
	systemStatsBuffer[0] = first
	systemStatsBuffer[1] = second
	systemStatsHead = 2
	systemStatsCount = 2
	systemStatsStarted = false
	systemStatsMu.Unlock()

	defer func() {
		systemStatsMu.Lock()
		systemStatsBuffer = oldBuffer
		systemStatsHead = oldHead
		systemStatsCount = oldCount
		systemStatsStarted = oldStarted
		systemStatsMu.Unlock()
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/stats/system", nil)
	rr := httptest.NewRecorder()
	handleAPIStatsSystem(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	type systemSampleResponse struct {
		Timestamp   string  `json:"timestamp"`
		CPUPercent  float64 `json:"cpu_percent"`
		RSSBytes    uint64  `json:"rss_bytes"`
		HeapAlloc   uint64  `json:"heap_alloc"`
		DBSizeBytes int64   `json:"db_size_bytes"`
	}

	got := decodeAPIData[[]systemSampleResponse](t, rr)
	if len(got) != 2 {
		t.Fatalf("expected 2 system samples, got %d", len(got))
	}
	if got[0].Timestamp >= got[1].Timestamp {
		t.Fatalf("expected chronological order, got %+v", got)
	}
	if got[1].RSSBytes != second.RSSBytes || got[1].HeapAlloc != second.HeapAlloc {
		t.Fatalf("expected latest sample fields to match, got %+v", got[1])
	}
}
