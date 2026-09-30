package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/policycore"
)

// Opt-in: the regular test suite must not seed millions of rows. Retain the
// same fixture and executable toolchain on both sides of a performance change.
func TestListPerformanceThreeMillion(t *testing.T) {
	if os.Getenv("SVART_CORE_LARGE_BENCH") != "1" {
		t.Skip("set SVART_CORE_LARGE_BENCH=1 for the 3M-rule measurement")
	}
	defer setupTestDB(t)()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestRollback(t, tx) }()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	stmt, err := tx.Prepare("INSERT INTO blocked_domains(blocklist_id,domain) VALUES(?,?)")
	if err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 3; id++ {
		exec("INSERT INTO blocklists(id,url,alias,enabled,domain_count) VALUES(?,? ,?,1,1000000)", id, fmt.Sprintf("https://lists.example/%d.txt", id), fmt.Sprintf("Published %d", id))
		for n := 0; n < 1000000; n++ {
			if _, err := stmt.Exec(id, largeFixtureRule(id, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 8; id++ {
		exec("INSERT INTO ip_ranges(id,name,cidr) VALUES(?,?,?)", id, fmt.Sprintf("Network %d", id), fmt.Sprintf("10.%d.0.0/16", id))
		exec("INSERT INTO range_blocklists(range_id,blocklist_id) VALUES(?,?)", id, (id-1)%3+1)
		exec("INSERT INTO client_groups(id,name) VALUES(?,?)", id, fmt.Sprintf("Group %d", id))
		exec("INSERT INTO group_blocklists(group_id,blocklist_id) VALUES(?,?)", id, id%3+1)
		for client := 1; client <= 8; client++ {
			ip := fmt.Sprintf("10.%d.0.%d", id, client)
			exec("INSERT INTO client_group_members(client_ip,group_id) VALUES(?,?)", ip, id)
			exec("INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES(?,?)", ip, (id+1)%3+1)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	mustReloadPolicy(t)
	t.Logf("fixture rules=3000000 ranges=8 groups=8 clients=64 toolchain=%s gomaxprocs=%d memory=%s", runtime.Version(), runtime.GOMAXPROCS(0), listPerformanceMemory())
	queries := make([]string, 3072)
	for n := 0; n < 1024; n++ {
		queries[n*3] = largeFixtureRule(n%3+1, n)
		queries[n*3+1] = "cdn." + queries[n*3]
		queries[n*3+2] = fmt.Sprintf("miss-%d.unlisted.example", n)
	}
	index := policyState.Load().Index
	assigned := index.SetFor([]int{1, 2, 3}, false)
	for sample := 0; sample < 3; sample++ {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		cpu := processCPU()
		hits := 0
		var probe policycore.Probe
		const count = 1000000
		for i := 0; i < count; i++ {
			index.ProbeDomain(&probe, queries[i%len(queries)], "", 1)
			if _, ok := index.FirstMatch(&probe, assigned, true); ok {
				hits++
			}
		}
		elapsed := time.Since(start)
		cpuElapsed := processCPU() - cpu
		runtime.ReadMemStats(&after)
		if hits != 666667 {
			t.Fatalf("lookup corpus matches=%d, want666667", hits)
		}
		t.Logf("index_lookup sample=%d queries=%d hits=%d ns_per_query=%.3f cpu=%s allocated_bytes=%d allocations=%d", sample, count, hits, float64(elapsed.Nanoseconds())/count, cpuElapsed, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs)
	}

	// Optional diagnostics profile only the manual-edit phase; exclude fixture
	// seeding and refresh. Keep this disabled for the timing evidence above.
	var profile *os.File
	if name := os.Getenv("SVART_CORE_MANUAL_CPU_PROFILE"); name != "" {
		// #nosec G304 G703 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		profile, err = os.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { checkTestClose(t, profile) }()
		if err := pprof.StartCPUProfile(profile); err != nil {
			t.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}
	for n := 0; n < 3; n++ {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		domain := fmt.Sprintf("manual-%d.tracker.example", n)
		request := httptest.NewRequest(http.MethodPost, "/api/clients/10.1.0.1/block", strings.NewReader(`{"domain":"`+domain+`"}`))
		response := httptest.NewRecorder()
		start := time.Now()
		cpu := processCPU()
		handleAPIClientBlockDomain(response, request, "10.1.0.1", nil)
		elapsed := time.Since(start)
		cpuElapsed := processCPU() - cpu
		runtime.ReadMemStats(&after)
		if response.Code != http.StatusOK {
			t.Fatalf("manual edit status=%d body=%s", response.Code, response.Body.String())
		}
		if got := evaluatePolicyFull("10.1.0.1", domain, 1); got.Result != "block" || got.ResultSource == nil || got.ResultSource.CustomRule == nil || got.ResultSource.CustomRule.Rule != domain {
			t.Fatalf("manual edit decision=%+v", got)
		}
		t.Logf("manual_edit iteration=%d wall=%s cpu=%s allocated_bytes=%d allocations=%d memory=%s", n, elapsed, cpuElapsed, after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, listPerformanceMemory())
	}
	if profile != nil {
		pprof.StopCPUProfile()
	}
	// Change one rule of a real million-row published list. Once the transaction
	// owns the writer, probe both real admin handlers and retain every sample.
	rules := make([]string, 1000000)
	for n := range rules {
		rules[n] = largeFixtureRule(1, n)
	}
	rules[len(rules)-1] = "changed.ads.example"
	finished := make(chan error, 1)
	start := time.Now()
	go func() {
		finished <- storeListGeneration(blockListStore, 1, rules, len(rules), "https://lists.example/1.txt", nil)
	}()
	for db.Stats().InUse == 0 {
		select {
		case err := <-finished:
			t.Fatalf("refresh finished before contention sample (invalid run): %v", err)
		default:
			runtime.Gosched()
		}
	}
	for n := 0; n < 10; n++ {
		for _, endpoint := range []struct {
			name, path string
			handler    http.HandlerFunc
		}{
			{"metadata", "/api/blocklists", handleAPIGetBlocklists},
			{"domains", "/api/blocklists/1/domains?limit=10", handleAPIBlocklistAction},
		} {
			active := db.Stats().InUse > 0
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, endpoint.path, nil)
			sampleStart := time.Now()
			endpoint.handler(response, request)
			latency := time.Since(sampleStart)
			if response.Code != http.StatusOK {
				t.Fatalf("admin %s status=%d body=%s", endpoint.name, response.Code, response.Body.String())
			}
			var envelope struct {
				Data  json.RawMessage `json:"data"`
				Error *string         `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Error != nil {
				t.Fatalf("admin %s invalid response: %v %+v", endpoint.name, err, envelope)
			}
			if endpoint.name == "domains" {
				var page struct {
					Total   int      `json:"total"`
					Domains []string `json:"domains"`
				}
				if err := json.Unmarshal(envelope.Data, &page); err != nil {
					t.Fatal(err)
				}
				if page.Total != 1000000 || len(page.Domains) != 10 {
					t.Fatalf("admin page incomplete: total=%d domains=%d", page.Total, len(page.Domains))
				}
			}
			t.Logf("admin_read iteration=%d endpoint=%s writer_active_at_start=%t wall=%s", n, endpoint.name, active, latency)
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	t.Logf("published_refresh wall=%s memory=%s", time.Since(start), listPerformanceMemory())
}

func largeFixtureRule(list, n int) string {
	return fmt.Sprintf("host-%07d.list-%d.ads.example", n, list)
}

func listPerformanceMemory() string {
	contents, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err.Error()
	}
	var fields []string
	for _, line := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmHWM:") {
			fields = append(fields, line)
		}
	}
	return strings.Join(fields, "; ")
}
