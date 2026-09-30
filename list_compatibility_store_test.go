package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCompatibilityOldWriterInvalidatesAssessment(t *testing.T) {
	defer setupTestDB(t)()
	id := compatibilityList(t, "||ads.example^$dnstype=TXT")
	before, err := readListCompatibilityReport(readDB, "blocklist", id)
	if err != nil || before == nil {
		t.Fatalf("initial report=%+v err=%v", before, err)
	}
	// The prior binary updates last_updated but knows nothing about reports.
	// Even two refreshes in one timestamp tick must invalidate the assessment.
	if _, err := db.Exec("UPDATE blocklists SET last_updated=last_updated WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	after, err := readListCompatibilityReport(readDB, "blocklist", id)
	if err != nil || after != nil {
		t.Fatalf("old writer retained stale report=%+v err=%v", after, err)
	}
}

func TestCompatibilityHistoryShowsCompleteRuleText(t *testing.T) {
	defer setupTestDB(t)()
	rule := `/^ad-[a-z]{2,4}\.example$/$dnstype=TXT,important`
	compatibilityList(t, rule)
	rr := httptest.NewRecorder()
	handleAPIBlocklistHistory(rr, httptest.NewRequest(http.MethodGet, "/api/blocklist-history", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", rr.Code, rr.Body.String())
	}
	var response struct {
		Data []BlocklistHistoryView `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Data) != 1 || !reflect.DeepEqual(response.Data[0].SampleAdded, []string{rule}) {
		t.Fatalf("history=%+v, want original unsplit rule %q", response.Data, rule)
	}
}

func TestCompatibilityReportRefreshTransaction(t *testing.T) {
	defer setupTestDB(t)()
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, "ads.example", "||tracking.example^$client=~192.0.2.1")
	res, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES (?,'Atomic compatibility',1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	before, err := readListCompatibilityReport(readDB, "blocklist", int(id))
	if err != nil {
		t.Fatal(err)
	}
	if before == nil || before.Applied != 1 || before.Unsupported != 1 || before.Invalid != 0 || len(before.Diagnostics) != 1 || before.Diagnostics[0].Rule != "||tracking.example^$client=~192.0.2.1" {
		t.Fatalf("report=%+v, want one applied and one complete unsupported rule", before)
	}
	allBefore := migrationSnapshot(t)
	if _, err := db.Exec("CREATE TRIGGER reject_compatibility BEFORE UPDATE ON local_blocklist_generations BEGIN SELECT RAISE(IGNORE); END"); err != nil {
		t.Fatal(err)
	}
	server.serve("tracker.example")
	if err := refreshBlocklistByID(int(id)); err == nil {
		t.Fatal("ignored report write reported successful refresh")
	}
	if _, err := db.Exec("DROP TRIGGER reject_compatibility"); err != nil {
		t.Fatal(err)
	}
	if got := migrationSnapshot(t); !reflect.DeepEqual(got, allBefore) {
		t.Fatal("failed report storage changed rules, history, or generation state")
	}
	after, err := readListCompatibilityReport(readDB, "blocklist", int(id))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("report changed: got=%+v want=%+v", after, before)
	}
	if _, err := db.Exec("UPDATE blocklists SET url='https://lists.example/changed.txt' WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	stale, err := readListCompatibilityReport(readDB, "blocklist", int(id))
	if err != nil {
		t.Fatal(err)
	}
	if stale != nil {
		t.Fatalf("changed source reported old assessment: %+v", stale)
	}
}

func TestCompatibilityReportMigrationPreservesLegacyRows(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("INSERT INTO blocklists(id,url,alias,enabled) VALUES(773,'https://lists.example/legacy','Legacy',1); INSERT INTO blocked_domains(blocklist_id,domain) VALUES(773,'ads.example'); INSERT INTO local_blocklist_generations(list_id,url) VALUES(773,'https://lists.example/legacy'); DROP TRIGGER invalidate_blocklist_compatibility; DROP TRIGGER invalidate_allowlist_compatibility; ALTER TABLE local_blocklist_generations DROP COLUMN compatibility_report; ALTER TABLE local_allowlist_generations DROP COLUMN compatibility_report"); err != nil {
		t.Fatal(err)
	}
	if err := schemaTransaction(initLocalListGenerations); err != nil {
		t.Fatal(err)
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", 773); !reflect.DeepEqual(got, []string{"ads.example"}) {
		t.Fatalf("legacy rules=%v", got)
	}
	report, err := readListCompatibilityReport(readDB, "blocklist", 773)
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Fatalf("legacy generation fabricated an assessment: %+v", report)
	}
	before := migrationSnapshot(t)
	if err := schemaTransaction(initLocalListGenerations); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(migrationSnapshot(t), before) {
		t.Fatal("repeated compatibility migration changed state")
	}
}
