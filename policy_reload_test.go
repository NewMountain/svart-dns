package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/policycore"
)

// gatedListServer serves a published list whose content the test swaps
// between refreshes. After hold, every request waits until the returned
// channel is closed, which keeps a list in the "still loading" state.
type gatedListServer struct {
	mu      sync.Mutex
	content []string
	gate    chan struct{}
	srv     *httptest.Server
}

func newGatedListServer(t *testing.T, content ...string) *gatedListServer {
	t.Helper()
	g := &gatedListServer{content: content}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		g.mu.Lock()
		gate, body := g.gate, strings.Join(g.content, "\n")
		g.mu.Unlock()
		if gate != nil {
			<-gate
		}
		if _, err := fmt.Fprintln(w, body); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *gatedListServer) hold() chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gate = make(chan struct{})
	return g.gate
}

func (g *gatedListServer) serve(content ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gate = nil
	g.content = content
}

// callAPI runs one admin API handler and fails the test unless it returns want.
func callAPI(t *testing.T, handler http.HandlerFunc, method, path, body string, want int) map[string]any {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d; body %s", method, path, w.Code, want, w.Body.String())
	}
	return decodeAPIData[map[string]any](t, w)
}

// rangeDecision is the range tier's deciding entity for domain, or nil.
func rangeDecision(clientIP, domain string) *policycore.EntityResult {
	r := evaluatePolicyFull(clientIP, domain, 1)
	if r.RangeEvaluation == nil {
		return nil
	}
	return r.RangeEvaluation.ResultSource
}

func publishedBlock(rangeName, rule string, listID int, listName string) *policycore.EntityResult {
	return &policycore.EntityResult{Tier: "range", Name: rangeName, Result: "block",
		PublishedList: &policycore.PublishedHit{Action: "block", Rule: rule, ListID: listID, ListName: listName}}
}

func describe(e *policycore.EntityResult) string {
	if e == nil {
		return "no decision"
	}
	b := mustFixture(json.Marshal(e))
	return string(b)
}

// waitForRangeDecision polls until the range tier decides domain as want, or
// fails after the deadline with the last decision seen.
func waitForRangeDecision(t *testing.T, clientIP, domain string, want *policycore.EntityResult) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var got *policycore.EntityResult
	for time.Now().Before(deadline) {
		if got = rangeDecision(clientIP, domain); reflect.DeepEqual(got, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("range decision for %s from %s: got %s, want %s", domain, clientIP, describe(got), describe(want))
}

func checkRangeDecision(t *testing.T, what, clientIP, domain string, want *policycore.EntityResult) {
	t.Helper()
	if got := rangeDecision(clientIP, domain); !reflect.DeepEqual(got, want) {
		t.Errorf("%s: range decision for %s: got %s, want %s", what, domain, describe(got), describe(want))
	}
}

// TestRangeServesListLoadedAfterAssignment is the B1 regression
// (design/SECURITY-REVIEW-2026-09.md): a range assigned a blocklist while
// that list is still downloading must block once the download lands, and
// must follow every later refresh, manual or automatic.
func TestRangeServesListLoadedAfterAssignment(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	const client = "10.20.30.40"
	lists := newGatedListServer(t, "sst.2checkout.com", "||pixel.facebook.com^")
	release := lists.hold()

	created := callAPI(t, handleAPIBlocklists, "POST", "/api/blocklists",
		`{"url":"`+lists.srv.URL+`/hagezi-pro.txt","alias":"Hagezi Pro","enabled":true}`, http.StatusCreated)
	listID := int(requireFixtureType[float64](t, created["id"]))
	rng := callAPI(t, handleAPIRangesRouter, "POST", "/api/ranges", `{"name":"everyone","cidr":"0.0.0.0/0"}`, http.StatusCreated)
	rangeID := int(requireFixtureType[float64](t, rng["id"]))
	callAPI(t, handleAPIRangeAction, "POST", fmt.Sprintf("/api/ranges/%d/blocklists/%d", rangeID, listID), "", http.StatusOK)

	checkRangeDecision(t, "while loading", client, "sst.2checkout.com", nil)
	close(release)
	waitForRangeDecision(t, client, "sst.2checkout.com", publishedBlock("everyone", "sst.2checkout.com", listID, "Hagezi Pro"))
	checkRangeDecision(t, "loaded", client, "cdn.pixel.facebook.com", publishedBlock("everyone", "pixel.facebook.com", listID, "Hagezi Pro"))

	// Manual refresh with changed content: the range serves the new version.
	lists.serve("sst.2checkout.com", "||analytics.tiktok.com^")
	if err := refreshBlocklistByID(listID); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	checkRangeDecision(t, "after refresh, added", client, "analytics.tiktok.com", publishedBlock("everyone", "analytics.tiktok.com", listID, "Hagezi Pro"))
	checkRangeDecision(t, "after refresh, removed", client, "pixel.facebook.com", nil)

	// Auto-refresh of a stale list: same guarantee.
	lists.serve("||analytics.tiktok.com^", "telemetry.microsoft.com")
	if _, err := db.Exec("UPDATE blocklists SET refresh_interval = 3600, last_updated = datetime('now', '-2 hours') WHERE id = ?", listID); err != nil {
		t.Fatal(err)
	}
	runAutoRefreshCycle(make(refreshRetries), time.Now)
	checkRangeDecision(t, "after auto-refresh, added", client, "telemetry.microsoft.com", publishedBlock("everyone", "telemetry.microsoft.com", listID, "Hagezi Pro"))
	checkRangeDecision(t, "after auto-refresh, removed", client, "sst.2checkout.com", nil)
}

// TestRangeServesRefreshedAllowlist is B1 for allowlists: a range's
// published allowlist carve-out follows the allowlist's refreshes.
func TestRangeServesRefreshedAllowlist(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	const client = "192.168.40.12"
	blocks := newGatedListServer(t, "||doubleclick.net^", "||googlesyndication.com^")
	allows := newGatedListServer(t, "ad.doubleclick.net")

	insertList := func(table, url, alias string) int {
		t.Helper()
		res, err := db.Exec("INSERT INTO "+table+" (url, alias, enabled) VALUES (?, ?, 1)", url, alias)
		if err != nil {
			t.Fatal(err)
		}
		id, fixtureErr6480 := res.LastInsertId()
		if fixtureErr6480 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr6480)
		}
		return int(id)
	}
	blID := insertList("blocklists", blocks.srv.URL+"/ads.txt", "OISD Big")
	alID := insertList("allowlists", allows.srv.URL+"/allow.txt", "Hagezi Allow")
	if err := refreshBlocklistByID(blID); err != nil {
		t.Fatalf("blocklist refresh: %v", err)
	}
	if err := refreshAllowlistByID(alID); err != nil {
		t.Fatalf("allowlist refresh: %v", err)
	}

	rng := callAPI(t, handleAPIRangesRouter, "POST", "/api/ranges", `{"name":"lan","cidr":"192.168.40.0/24"}`, http.StatusCreated)
	rangeID := int(requireFixtureType[float64](t, rng["id"]))
	callAPI(t, handleAPIRangeAction, "POST", fmt.Sprintf("/api/ranges/%d/blocklists/%d", rangeID, blID), "", http.StatusOK)
	callAPI(t, handleAPIRangeAction, "POST", fmt.Sprintf("/api/ranges/%d/allowlists/%d", rangeID, alID), "", http.StatusOK)

	// A published block and a published allow both match: the block is the
	// published hit and the allow is reported as the overriding rule.
	overridden := func(blockRule, allowRule string) *policycore.EntityResult {
		return &policycore.EntityResult{Tier: "range", Name: "lan", Result: "allow",
			PublishedList: &policycore.PublishedHit{Action: "block", Rule: blockRule, ListID: blID, ListName: "OISD Big"},
			CustomRule:    &policycore.CustomHit{Action: "allow", Rule: allowRule}}
	}
	checkRangeDecision(t, "before refresh", client, "ad.doubleclick.net", overridden("doubleclick.net", "ad.doubleclick.net"))

	allows.serve("pagead2.googlesyndication.com")
	if err := refreshAllowlistByID(alID); err != nil {
		t.Fatalf("allowlist refresh: %v", err)
	}
	checkRangeDecision(t, "after refresh, newly allowed", client, "pagead2.googlesyndication.com",
		overridden("googlesyndication.com", "pagead2.googlesyndication.com"))
	checkRangeDecision(t, "after refresh, no longer allowed", client, "ad.doubleclick.net",
		&policycore.EntityResult{Tier: "range", Name: "lan", Result: "block",
			PublishedList: &policycore.PublishedHit{Action: "block", Rule: "doubleclick.net", ListID: blID, ListName: "OISD Big"}})
}
