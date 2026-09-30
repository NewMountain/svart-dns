package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

// --- Helper: setup policy test environment with seed data ---

func setupAnalysisTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupTestDB(t)

	adsID, trackingID, _ := seedBlocklists(t)
	seedClientsAndGroups(t, adsID, trackingID)
	seedQueryLogs(t)

	mustReloadPolicy(t)

	return cleanup
}

// --- 3.2: Blocklist comparison tests ---

func TestCompareOverlap(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	var ids []int
	for _, l := range policyState.Load().Index.Lists {
		if !l.Allow {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatal("need at least 2 blocklists for comparison")
	}

	body, fixtureErr781 := json.Marshal(compareRequest{BlocklistIDs: ids[:2]})
	if fixtureErr781 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr781)
	}
	req := httptest.NewRequest("POST", "/api/analysis/compare", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisCompare(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	data, fixtureErr1228 := json.Marshal(resp.Data)
	if fixtureErr1228 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1228)
	}
	var result compareResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if len(result.Lists) != 2 {
		t.Fatalf("expected 2 lists, got %d", len(result.Lists))
	}
	if len(result.Overlap) != 2 {
		t.Fatalf("expected 2x2 overlap matrix, got %d rows", len(result.Overlap))
	}

	// Diagonal should be domain count of each list
	for i, li := range result.Lists {
		if result.Overlap[i][i] != li.DomainCount {
			t.Errorf("diagonal overlap[%d][%d] = %d, expected %d (domain_count)", i, i, result.Overlap[i][i], li.DomainCount)
		}
	}

	// Overlap should be symmetric
	if result.Overlap[0][1] != result.Overlap[1][0] {
		t.Errorf("overlap not symmetric: [0][1]=%d, [1][0]=%d", result.Overlap[0][1], result.Overlap[1][0])
	}
}

func TestCompareUnique(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	var ids []int
	for _, l := range policyState.Load().Index.Lists {
		if !l.Allow {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatal("need at least 2 blocklists")
	}

	body, fixtureErr2319 := json.Marshal(compareRequest{BlocklistIDs: ids[:2]})
	if fixtureErr2319 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr2319)
	}
	req := httptest.NewRequest("POST", "/api/analysis/compare", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisCompare(w, req)

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr2667 := json.Marshal(resp.Data)
	if fixtureErr2667 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr2667)
	}
	var result compareResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Unique count for each list should be >= 0 and <= domain count
	for i, li := range result.Lists {
		if result.UniqueCount[i] < 0 || result.UniqueCount[i] > li.DomainCount {
			t.Errorf("unique count for list %d (%s) out of range: %d (domain_count=%d)",
				li.ID, li.Alias, result.UniqueCount[i], li.DomainCount)
		}
	}
}

func TestCompareUnionSize(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	var ids []int
	for _, l := range policyState.Load().Index.Lists {
		if !l.Allow {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) < 2 {
		t.Fatal("need at least 2 blocklists")
	}

	body, fixtureErr3440 := json.Marshal(compareRequest{BlocklistIDs: ids[:2]})
	if fixtureErr3440 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3440)
	}
	req := httptest.NewRequest("POST", "/api/analysis/compare", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisCompare(w, req)

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr3788 := json.Marshal(resp.Data)
	if fixtureErr3788 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3788)
	}
	var result compareResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Union size should be >= max(list domain counts) and <= sum of all
	maxCount := 0
	sumCount := 0
	for _, li := range result.Lists {
		if li.DomainCount > maxCount {
			maxCount = li.DomainCount
		}
		sumCount += li.DomainCount
	}
	if result.UnionSize < maxCount || result.UnionSize > sumCount {
		t.Errorf("union_size %d out of range [%d, %d]", result.UnionSize, maxCount, sumCount)
	}
}

func TestCompareRequiresTwo(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr4449 := json.Marshal(compareRequest{BlocklistIDs: []int{1}})
	if fixtureErr4449 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr4449)
	}
	req := httptest.NewRequest("POST", "/api/analysis/compare", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisCompare(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for < 2 lists, got %d", w.Code)
	}
}

func TestCompareUnknownList(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr4861 := json.Marshal(compareRequest{BlocklistIDs: []int{99999, 99998}})
	if fixtureErr4861 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr4861)
	}
	req := httptest.NewRequest("POST", "/api/analysis/compare", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisCompare(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown list IDs, got %d", w.Code)
	}
}

// --- 3.1: Domain × List matrix tests ---

func TestMatrixBasic(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	domains := []string{
		"google.com",               // not blocked
		"ads.google.com",           // blocked by Hagezi Pro
		"segment.io",               // blocked by OISD Big
		"unknown-domain.xyz",       // not blocked
		"tracking.ads.example.com", // blocked by Hagezi Pro
	}

	body, fixtureErr5608 := json.Marshal(matrixRequest{Domains: domains})
	if fixtureErr5608 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr5608)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr6046 := json.Marshal(resp.Data)
	if fixtureErr6046 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr6046)
	}
	var result matrixResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if len(result.Lists) == 0 {
		t.Fatal("expected at least one list")
	}
	if len(result.Domains) != len(domains) {
		t.Fatalf("expected %d domains, got %d", len(domains), len(result.Domains))
	}
	if len(result.Matrix) != len(domains) {
		t.Fatalf("expected %d matrix rows, got %d", len(domains), len(result.Matrix))
	}

	// Verify at least some entries are blocked
	anyBlocked := false
	for _, row := range result.Matrix {
		for _, cell := range row {
			if cell {
				anyBlocked = true
				break
			}
		}
	}
	if !anyBlocked {
		t.Error("expected at least some domains to be blocked in the matrix")
	}
}

func TestMatrixDefaultsToAllLists(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	// Omit blocklist_ids — should use all enabled lists
	body, fixtureErr6980 := json.Marshal(matrixRequest{Domains: []string{"google.com", "ads.google.com"}})
	if fixtureErr6980 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr6980)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr7451 := json.Marshal(resp.Data)
	if fixtureErr7451 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr7451)
	}
	var result matrixResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Should include both enabled lists (Hagezi Pro + OISD Big) but not disabled (Steven Black)
	enabled := 0
	for _, l := range policyState.Load().Index.Lists {
		if !l.Allow {
			enabled++
		}
	}
	if len(result.Lists) != enabled {
		t.Errorf("expected %d lists (all enabled), got %d", enabled, len(result.Lists))
	}
}

func TestMatrixSortedBySize(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr8038 := json.Marshal(matrixRequest{Domains: []string{"google.com"}})
	if fixtureErr8038 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr8038)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr8393 := json.Marshal(resp.Data)
	if fixtureErr8393 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr8393)
	}
	var result matrixResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Lists should be sorted by domain_count ascending
	for i := 1; i < len(result.Lists); i++ {
		if result.Lists[i].DomainCount < result.Lists[i-1].DomainCount {
			t.Errorf("lists not sorted by size: list %d (%d domains) before list %d (%d domains)",
				i-1, result.Lists[i-1].DomainCount, i, result.Lists[i].DomainCount)
		}
	}
}

func TestMatrixSubdomainMatch(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	// "tracker.ads.google.com" should match "ads.google.com" in Hagezi Pro via subdomain walk
	body, fixtureErr9089 := json.Marshal(matrixRequest{Domains: []string{"tracker.ads.google.com"}})
	if fixtureErr9089 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr9089)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr9456 := json.Marshal(resp.Data)
	if fixtureErr9456 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr9456)
	}
	var result matrixResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Should be blocked by at least one list
	anyBlocked := false
	for _, cell := range result.Matrix[0] {
		if cell {
			anyBlocked = true
			break
		}
	}
	if !anyBlocked {
		t.Error("expected tracker.ads.google.com to be blocked via subdomain match on ads.google.com")
	}

	// matched_rules should show the parent domain
	anyRuleMatch := false
	for _, rule := range result.MatchedRules[0] {
		if rule != nil && *rule == "ads.google.com" {
			anyRuleMatch = true
			break
		}
	}
	if !anyRuleMatch {
		t.Error("expected matched_rules to contain 'ads.google.com' for subdomain match")
	}
}

func TestMatrixEmptyDomains(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr10312 := json.Marshal(matrixRequest{Domains: []string{}})
	if fixtureErr10312 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr10312)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty domains, got %d", w.Code)
	}
}

func TestMatrixCap(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	// Build a request with > 10k domains
	domains := make([]string, 10001)
	for i := range domains {
		domains[i] = "domain-" + intToStr(i) + ".com"
	}

	body, fixtureErr10864 := json.Marshal(matrixRequest{Domains: domains})
	if fixtureErr10864 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr10864)
	}
	req := httptest.NewRequest("POST", "/api/analysis/matrix", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisMatrix(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for >10k domains, got %d", w.Code)
	}
}

// --- 3.3: Policy simulator tests ---

func TestSimulateBasic(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	domains := []string{
		"google.com",     // not blocked for Chris
		"ads.google.com", // blocked for Chris (direct IP assignment)
		"segment.io",     // blocked for Chris (direct IP assignment)
	}

	body, fixtureErr11504 := json.Marshal(simulateRequest{ClientIP: "10.42.1.42", Domains: domains})
	if fixtureErr11504 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr11504)
	}
	req := httptest.NewRequest("POST", "/api/analysis/simulate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr11972 := json.Marshal(resp.Data)
	if fixtureErr11972 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr11972)
	}
	var result simulateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if result.ClientIP != "10.42.1.42" {
		t.Errorf("expected client_ip 10.42.1.42, got %s", result.ClientIP)
	}
	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	// google.com should not be blocked
	if result.Results[0].Result == "block" {
		t.Error("expected google.com to not be blocked for Chris")
	}

	// ads.google.com should be blocked
	if result.Results[1].Result != "block" {
		t.Errorf("expected ads.google.com to be blocked, got %q", result.Results[1].Result)
	}

	// segment.io should be blocked
	if result.Results[2].Result != "block" {
		t.Errorf("expected segment.io to be blocked, got %q", result.Results[2].Result)
	}
}

func TestSimulateTierBreakdown(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	// Sam's iPhone (10.42.1.43) is in the Sam group which has Hagezi Pro
	body, fixtureErr12999 := json.Marshal(simulateRequest{
		ClientIP: "10.42.1.43",
		Domains:  []string{"ads.google.com"},
	})
	if fixtureErr12999 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr12999)
	}
	req := httptest.NewRequest("POST", "/api/analysis/simulate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req)

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr13397 := json.Marshal(resp.Data)
	if fixtureErr13397 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr13397)
	}
	var result simulateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	r := result.Results[0]
	if r.Result != "block" {
		t.Errorf("expected block, got %q", r.Result)
	}
	if r.ResultSource == nil {
		t.Fatal("expected result_source to be set")
	}
	// Sam group should be the source (group tier)
	if r.ResultSource.Tier != "group" {
		t.Errorf("expected group tier, got %q", r.ResultSource.Tier)
	}
}

func TestSimulateUnknownClient(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr14097 := json.Marshal(simulateRequest{
		ClientIP: "192.168.99.99",
		Domains:  []string{"ads.google.com"},
	})
	if fixtureErr14097 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr14097)
	}
	req := httptest.NewRequest("POST", "/api/analysis/simulate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr14575 := json.Marshal(resp.Data)
	if fixtureErr14575 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr14575)
	}
	var result simulateResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Unknown client with no policy should not block
	if result.Results[0].Result == "block" {
		t.Error("expected unknown client to not block anything")
	}
}

func TestSimulateMissingIP(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr15002 := json.Marshal(simulateRequest{Domains: []string{"google.com"}})
	if fixtureErr15002 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr15002)
	}
	req := httptest.NewRequest("POST", "/api/analysis/simulate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing client_ip, got %d", w.Code)
	}
}

// --- 3.4: Domain prefill sources tests ---

func TestDomainsTopQueried(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/analysis/domains?source=top-queried&limit=5", nil)
	w := httptest.NewRecorder()
	handleAPIAnalysisDomains(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr15869 := json.Marshal(resp.Data)
	if fixtureErr15869 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr15869)
	}
	var result domainsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if result.Source != "top-queried" {
		t.Errorf("expected source=top-queried, got %s", result.Source)
	}
	if result.Count == 0 {
		t.Error("expected some queried domains from seed data")
	}

	// Domains should not have trailing dots
	for _, d := range result.Domains {
		if d[len(d)-1] == '.' {
			t.Errorf("domain %q has trailing dot", d)
		}
	}
}

func TestDomainsTopBlocked(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/analysis/domains?source=top-blocked&limit=10", nil)
	w := httptest.NewRecorder()
	handleAPIAnalysisDomains(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	data, fixtureErr16878 := json.Marshal(resp.Data)
	if fixtureErr16878 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr16878)
	}
	var result domainsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if result.Count == 0 {
		t.Error("expected some blocked domains from seed data")
	}

	// All returned domains should have been blocked in seed data
	// (ads.google.com, segment.io, ads.facebook.com, newrelic.com, sentry.io)
	blockedSet := map[string]bool{
		"ads.google.com":   true,
		"segment.io":       true,
		"ads.facebook.com": true,
		"newrelic.com":     true,
		"sentry.io":        true,
	}
	for _, d := range result.Domains {
		if !blockedSet[d] {
			t.Errorf("unexpected blocked domain: %s", d)
		}
	}
}

func TestDomainsInvalidSource(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/analysis/domains?source=invalid", nil)
	w := httptest.NewRecorder()
	handleAPIAnalysisDomains(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid source, got %d", w.Code)
	}
}

// --- Matrix rule lookup (ruleIn) ---

// analysisRule is the rule a one-list index reports for domain in the matrix.
func analysisRule(rules []string, domain string) string {
	b := mustFixture(policycore.NewIndexBuilder([]policycore.List{{ID: 1, Name: "Hagezi Pro"}}))
	for _, r := range rules {
		b.Add(0, r)
	}
	ix := mustFixture(b.Build())
	var p policycore.Probe
	ix.ProbeDomain(&p, domain, "", 1)
	return ix.RuleIn(&p, 0)
}

func TestAnalysisRuleIn(t *testing.T) {
	cases := []struct {
		name   string
		rules  []string
		domain string
		want   string
	}{
		{"exact", []string{"ads.google.com", "tracker.fb.com"}, "ads.google.com", "ads.google.com"},
		{"exact rule does not match its parent", []string{"ads.google.com", "tracker.fb.com"}, "google.com", ""},
		{"subdomain", []string{"doubleclick.net"}, "ad.doubleclick.net", "doubleclick.net"},
		{"deep subdomain", []string{"doubleclick.net"}, "deep.sub.doubleclick.net", "doubleclick.net"},
		{"wildcard suffix", []string{"*.adserver.com"}, "cdn.adserver.com", "*.adserver.com"},
		{"wildcard does not match its root", []string{"*.adserver.com"}, "adserver.com", ""},
		{"complex wildcard", []string{"ad*tracker*.com"}, "ad-tracker-v2.com", "ad*tracker*.com"},
		{"complex wildcard miss", []string{"ad*tracker*.com"}, "google.com", ""},
		{"empty list", nil, "anything.com", ""},
		{"reverse lookups are not matched by IP rules", []string{"193.200.64.30"}, "30.64.200.193.in-addr.arpa", ""},
	}
	for _, c := range cases {
		if got := analysisRule(c.rules, c.domain); got != c.want {
			t.Errorf("%s: rule for %q = %q, want %q", c.name, c.domain, got, c.want)
		}
	}
}
