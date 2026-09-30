package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

func insertTestBlocklist(t *testing.T, alias string, enabled bool, rules ...string) int {
	t.Helper()
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", "https://lists.example.net/"+alias, alias, enabled)
	if err != nil {
		t.Fatal(err)
	}
	id, fixtureErr414 := res.LastInsertId()
	if fixtureErr414 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr414)
	}
	for _, r := range rules {
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, r); err != nil {
			t.Fatal(err)
		}
	}
	return int(id)
}

// TestReloadReusesIndexForEntityChanges pins when the list index is rebuilt:
// entity-only changes reuse it as is, a rename reuses its rule tables, and
// enabling or disabling a list rebuilds it.
func TestReloadReusesIndexForEntityChanges(t *testing.T) {
	defer setupTestDB(t)()
	hagezi := insertTestBlocklist(t, "Hagezi Pro", true, "doubleclick.net", "*.adnxs.com")
	oisd := insertTestBlocklist(t, "OISD Big", true, "pixel.facebook.com")
	mustReloadPolicy(t)
	first := policyState.Load().Index

	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('IoT VLAN', '10.42.3.0/24')"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?)", hagezi); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := reloadPolicyState("range added", reloadEntities); err != nil {
		t.Fatal(err)
	}
	if got := policyState.Load().Index; got != first {
		t.Fatal("an entity-only reload rebuilt the list index")
	}
	if got := isBlockedForClient("10.42.3.9", "stats.g.doubleclick.net"); !got {
		t.Fatal("range assigned by an entity-only reload does not block")
	}

	if _, err := db.Exec("UPDATE blocklists SET alias = 'Hagezi Pro++' WHERE id = ?", hagezi); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := reloadPolicyState("renamed", reloadEntities); err != nil {
		t.Fatal(err)
	}
	renamed := policyState.Load().Index
	if renamed == first || &renamed.Exact.Shards[0].Arena[0] != &first.Exact.Shards[0].Arena[0] {
		t.Fatal("a rename must copy the index header and share its rule tables")
	}
	r := evaluatePolicyFull("10.42.3.9", "doubleclick.net", 1)
	if got := r.ResultSource.PublishedList; got == nil || got.ListName != "Hagezi Pro++" || got.ListID != hagezi {
		t.Fatalf("after rename the decision is attributed to %+v, want Hagezi Pro++ (%d)", got, hagezi)
	}

	if _, err := db.Exec("UPDATE blocklists SET enabled = 0 WHERE id = ?", oisd); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := reloadPolicyState("disabled", reloadEntities); err != nil {
		t.Fatal(err)
	}
	if ix := policyState.Load().Index; ix.SlotOf(oisd, false) >= 0 || len(ix.Lists) != 1 {
		t.Fatalf("disabling a list did not rebuild the index without it: %+v", ix.Lists)
	}
}

// TestFailedReloadKeepsPreviousPolicy: a snapshot that cannot be built is
// never published; the old one keeps serving and the error names the cause.
func TestFailedReloadKeepsPreviousPolicy(t *testing.T) {
	defer setupTestDB(t)()
	tracker := insertTestBlocklist(t, "Tracker List", true, "tracker.example.com")
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.42.1.42', ?)", tracker); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	spare := insertTestBlocklist(t, "Spare List", false, "ads.example.net")
	mustReloadPolicy(t)
	before := policyState.Load()
	failures := counterValue(t, policyReloadErrors)

	// One more enabled list than the index can hold.
	tx, fixtureErr3657 := db.Begin()
	if fixtureErr3657 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3657)
	}
	for i := 0; i < policycore.MaxIndexLists; i++ {
		if _, err := tx.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', ?, 1)", fmt.Sprintf("Manual Block (10.99.%d.%d)", i/256, i%256)); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	err := reloadPolicyState("too many lists", reloadEntities)
	want := "policy reload (too many lists) failed, previous policy stays active: too many enabled lists: 2049 enabled blocklists and allowlists, the list index holds at most 2048; disable or delete lists (each client, group or policy with custom rules has its own manual list)"
	if err == nil || err.Error() != want {
		t.Fatalf("reload error = %v, want %q", err, want)
	}
	if policyState.Load() != before {
		t.Fatal("a failed reload replaced the published snapshot")
	}
	if got := counterValue(t, policyReloadErrors) - failures; got != 1 {
		t.Fatalf("svart_dns_policy_reload_errors_total grew by %v, want 1", got)
	}
	if !isBlockedForClient("10.42.1.42", "tracker.example.com") {
		t.Fatal("the previous policy stopped serving after a failed reload")
	}

	// The API rejects a mutation whose resulting runtime snapshot cannot be built.
	req := httptest.NewRequest("POST", fmt.Sprintf("/api/blocklists/%d/toggle", spare), nil)
	w := httptest.NewRecorder()
	handleAPIBlocklistAction(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"error_code":"unavailable"`) {
		t.Fatalf("toggle during overload: %d %s", w.Code, w.Body.String())
	}
	var enabled bool
	if err := db.QueryRow("SELECT enabled FROM blocklists WHERE id=?", spare).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled || policyState.Load() != before {
		t.Fatal("failed toggle changed stored or active policy")
	}

}

func TestPolicyEvaluateRejectsOverlongNames(t *testing.T) {
	defer setupTestDB(t)()
	label := strings.Repeat("a", 63)
	name := strings.Join([]string{label, label, label, label}, ".") + ".example" // 263 bytes
	req := httptest.NewRequest("GET", "/api/policy/evaluate?client_ip=10.42.1.42&domain="+name, nil)
	w := httptest.NewRecorder()
	handleAPIPolicyEvaluate(w, req)
	if want := `"error":"domain is 263 bytes; DNS names are at most 253"`; w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) {
		t.Fatalf("overlong name: %d %s, want 400 with %s", w.Code, w.Body.String(), want)
	}
}

func TestSyncTouchesListContent(t *testing.T) {
	cases := []struct {
		name string
		resp SyncResponse
		want bool
	}{
		{"client alias only", SyncResponse{Changes: SyncChanges{ClientAliases: []SyncClientAlias{{IPAddress: "10.42.1.42", Alias: "Sam laptop"}}}}, false},
		{"list metadata only", SyncResponse{Changes: SyncChanges{Blocklists: []SyncBlocklist{{Alias: "Hagezi Pro"}}}}, false},
		{"custom block rule", SyncResponse{Changes: SyncChanges{BlockedDomains: []SyncManualDomain{{ListAlias: "Manual Block (10.42.1.42)", Domain: "tiktok.com"}}}}, true},
		{"custom allow rule", SyncResponse{Changes: SyncChanges{AllowedDomains: []SyncManualDomain{{ListAlias: "Manual (10.42.1.42)", Domain: "cdn.example.com"}}}}, true},
		{"custom rule deleted", SyncResponse{Tombstones: []Tombstone{{TableName: "blocked_domains", NaturalKey: "Manual Block (10.42.1.42)|tiktok.com"}}}, true},
		{"group deleted", SyncResponse{Tombstones: []Tombstone{{TableName: "client_groups", NaturalKey: "Kids"}}}, false},
	}
	for _, c := range cases {
		if got := syncTouchesListContent(&c.resp); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
