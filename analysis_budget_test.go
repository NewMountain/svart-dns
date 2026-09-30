package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestResolveAnalysisLists(t *testing.T) {
	loaded := buildFixtureIndex(t, []fixtureList{
		{id: 1, rules: []string{"telemetry.001-studio.com", "ad.001zb.com"}}, // Hagezi Pro
		{id: 2, rules: []string{"0--foodwarez.da.ru"}},                       // OISD Big
		{id: 3, rules: []string{"ad-assets.futurecdn.net"}},                  // StevenBlack
	}, false)
	got, err := resolveAnalysisLists([]int{2, 1, 2, 3, 1, 2}, loaded)
	if err != nil || !reflect.DeepEqual(got, []int{2, 1, 3}) {
		t.Fatalf("got %v, %v; want [2 1 3] in first-seen order", got, err)
	}
	if _, err := resolveAnalysisLists([]int{1, 900001}, loaded); err == nil ||
		err.Error() != "unknown blocklist_id: 900001 (not an enabled blocklist)" {
		t.Fatalf("unknown id error = %v", err)
	}
	allowlists := buildFixtureIndex(t, []fixtureList{{id: 1, rules: []string{"ad.001zb.com"}}}, true)
	if _, err := resolveAnalysisLists([]int{1}, allowlists); err == nil ||
		err.Error() != "unknown blocklist_id: 1 (not an enabled blocklist)" {
		t.Fatalf("allowlist id accepted as a blocklist: %v", err)
	}

	var many []fixtureList
	var ids []int
	for i := 1; i <= 65; i++ {
		many = append(many, fixtureList{id: i, rules: []string{fmt.Sprintf("ads%d.example.com", i)}})
		ids = append(ids, i)
	}
	manyIndex := buildFixtureIndex(t, many, false)
	if _, err := resolveAnalysisLists(ids, manyIndex); err == nil ||
		err.Error() != "too many blocklists: 65 distinct lists requested, at most 64 per request" {
		t.Fatalf("65 lists error = %v", err)
	}
	if got, err := resolveAnalysisLists(ids[:64], manyIndex); err != nil || len(got) != 64 {
		t.Fatalf("64 lists: got %d, %v", len(got), err)
	}
}

func TestNormalizeAnalysisDomains(t *testing.T) {
	got, err := normalizeAnalysisDomains([]string{" Ads.Google.COM. ", "pixel.facebook.com"})
	if err != nil || !reflect.DeepEqual(got, []string{"ads.google.com", "pixel.facebook.com"}) {
		t.Fatalf("got %v, %v", got, err)
	}
	cases := map[string][]string{
		"domains list required":                              {},
		"domains[1] is empty":                                {"ads.google.com", " . "},
		"domains[0] is 254 bytes; DNS names are at most 253": {strings.Repeat("a", 250) + ".com"},
	}
	for want, in := range cases {
		if _, err := normalizeAnalysisDomains(in); err == nil || err.Error() != want {
			t.Errorf("normalize(%d items) err = %v, want %q", len(in), err, want)
		}
	}
	tooMany := make([]string, maxAnalysisDomains+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("host%d.example.com", i)
	}
	if _, err := normalizeAnalysisDomains(tooMany); err == nil ||
		err.Error() != "too many domains: 10001 requested, maximum 10000 per request" {
		t.Fatalf("10001 domains err = %v", err)
	}
}

func TestAnalysisWorkEstimates(t *testing.T) {
	// Two lists, the second with two complex patterns (ad*.example.com,
	// track*.example.net).
	if got := matrixWorkUnits(10000, []int{0, 2}); got != 40000 {
		t.Fatalf("matrixWorkUnits = %d, want 10000 x (1 + 1+2) = 40000", got)
	}
	loaded, pairs := compareWork([]int{350000, 250000, 130000})
	if loaded != 730000 || pairs != 250000+130000+130000 {
		t.Fatalf("compareWork = %d, %d; want 730000, 510000", loaded, pairs)
	}
}

func TestAnalysisRejectsWhenSlotsAreBusy(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	// Occupy every slot the way two in-flight requests would.
	for i := 0; i < analysisConcurrency; i++ {
		analysisSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < analysisConcurrency; i++ {
			<-analysisSlots
		}
	}()

	body, fixtureErr3656 := json.Marshal(simulateRequest{ClientIP: "192.168.1.10", Domains: []string{"ads.google.com"}})
	if fixtureErr3656 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3656)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/analysis/simulate", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("status %d Retry-After %q, want 429 and 1", w.Code, w.Header().Get("Retry-After"))
	}
	if got := secErrorOf(w.Body.String()); got != "error=analysis capacity busy: 2 analysis requests are already running; retry in a second" {
		t.Fatalf("error = %s", got)
	}
}

func TestAnalysisStopsWorkWhenClientDisconnects(t *testing.T) {
	cleanup := setupAnalysisTestEnv(t)
	defer cleanup()

	body, fixtureErr4393 := json.Marshal(simulateRequest{ClientIP: "192.168.1.10", Domains: []string{"ads.google.com", "pixel.facebook.com"}})
	if fixtureErr4393 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr4393)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/analysis/simulate", bytes.NewReader(body))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	handleAPIAnalysisSimulate(w, req.WithContext(ctx))
	if w.Body.Len() != 0 {
		t.Fatalf("cancelled request still produced a %d-byte response", w.Body.Len())
	}
	if len(analysisSlots) != 0 {
		t.Fatalf("cancelled request leaked %d analysis slots", len(analysisSlots))
	}
}
