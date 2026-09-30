package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

func bridgeWriterTestDB(t *testing.T) {
	t.Helper()
	old := listWriterRole
	listWriterRole = "bridge"
	t.Cleanup(func() { listWriterRole = old })
	setupTestDB(t)
}

func TestListWriterBridgeKeepsLegacyUntilStickyActivation(t *testing.T) {
	bridgeWriterTestDB(t)
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	const body = "||ads.example^$dnstype=TXT\n||*.top^$dnstype=~CNAME\n@@|www.example.com|\n0.0.0.0 ordinary.example\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	defer server.Close()
	before, err := fetchList(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Domains, []string{"*.top", "ordinary.example"}) || !reflect.DeepEqual(before.Exceptions, []string{"www.example.com"}) || len(before.Rules) != 0 || before.Unsupported != 1 || before.ModifiersIgnored != 1 {
		t.Fatalf("bridge changed legacy parse: %+v", before)
	}
	listWriterRole = "active"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	listWriterRole = "bridge"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	after, err := fetchList(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Rules) != 3 || !reflect.DeepEqual(after.Domains, []string{"ordinary.example"}) {
		t.Fatalf("fallback lost activated parser: %+v", after)
	}
}

func TestListWriterGateReadErrorsNeverSelectLegacy(t *testing.T) {
	bridgeWriterTestDB(t)
	if _, err := db.Exec("DROP TABLE local_list_storage_capabilities"); err != nil {
		t.Fatal(err)
	}
	_, err := parseDownloadedList(strings.NewReader("ads.example"), 1<<20, 10)
	if err == nil || !strings.Contains(err.Error(), "list writer capability") {
		t.Fatalf("read error silently parsed: %v", err)
	}
}

func TestListWriterActivationTransactionFailureKeepsBridge(t *testing.T) {
	bridgeWriterTestDB(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_writer_activation BEFORE UPDATE ON local_list_storage_capabilities BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	listWriterRole = "active"
	if err := initializeSchema(); err == nil {
		t.Fatal("ignored activation reported success")
	}
	var active bool
	if err := db.QueryRow("SELECT typed_rules_v1 FROM local_list_storage_capabilities WHERE singleton=1").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("failed activation committed")
	}
}

func TestListWriterActivationRejectsStaleLegacyRefresh(t *testing.T) {
	bridgeWriterTestDB(t)
	if _, err := db.Exec("INSERT INTO blocklists(id,url,alias,enabled) VALUES(9001,'https://example.com/list','gate-test',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains(blocklist_id,domain) VALUES(9001,'original.example')"); err != nil {
		t.Fatal(err)
	}
	listWriterRole = "active"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	err := storeListGeneration(blockListStore, 9001, []string{"stale.example"}, 1, "https://example.com/list", nil, false)
	if err == nil {
		t.Fatal("legacy refresh committed after activation")
	}
	var got string
	if err := db.QueryRow("SELECT domain FROM blocked_domains WHERE blocklist_id=9001").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "original.example" {
		t.Fatalf("previous policy replaced: %q", got)
	}
}

func TestListWriterBridgeRejectsTypedRowsBeforeActivation(t *testing.T) {
	bridgeWriterTestDB(t)
	if _, err := db.Exec("INSERT INTO blocklists(id,url,alias,enabled) VALUES(9002,'https://example.com/list','gate-typed',1)"); err != nil {
		t.Fatal(err)
	}
	row, err := listparse.EncodeStored(listparse.Rule{Version: 1, Text: "|ads.example|", Pattern: "ads.example", Kind: listparse.KindExact})
	if err != nil {
		t.Fatal(err)
	}
	if err := storeListGeneration(blockListStore, 9002, []string{row}, 1, "https://example.com/list", nil); err == nil {
		t.Fatal("inactive bridge stored typed row")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM blocked_domains WHERE blocklist_id=9002").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("rejected row committed")
	}
}

func TestListWriterUnknownBuildRoleRefusesStartup(t *testing.T) {
	bridgeWriterTestDB(t)
	listWriterRole = "typo"
	if err := initializeSchema(); err == nil {
		t.Fatal("unknown writer role started")
	}
}

func TestListWriterBridgeRefreshRemainsUnassessedUntilActivation(t *testing.T) {
	bridgeWriterTestDB(t)
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ordinary.example\n||ads.example^$dnstype=TXT\n")); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	defer server.Close()
	if _, err := db.Exec("INSERT INTO blocklists(id,url,alias,enabled) VALUES(9003,?,'gate-report',1)", server.URL); err != nil {
		t.Fatal(err)
	}
	if err := refreshListByID(blockListStore, 9003); err != nil {
		t.Fatal(err)
	}
	var report string
	if err := db.QueryRow("SELECT compatibility_report FROM local_blocklist_generations WHERE list_id=9003").Scan(&report); err != nil {
		t.Fatal(err)
	}
	if report != "" {
		t.Fatalf("bridge emitted typed assessment: %s", report)
	}
	listWriterRole = "active"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	listWriterRole = "bridge"
	if err := refreshListByID(blockListStore, 9003); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT compatibility_report FROM local_blocklist_generations WHERE list_id=9003").Scan(&report); err != nil {
		t.Fatal(err)
	}
	if report == "" {
		t.Fatal("activated fallback lost typed assessment")
	}
	var advanced int
	if err := db.QueryRow("SELECT count(*) FROM blocked_domains WHERE blocklist_id=9003 AND domain LIKE '!svart-rule-v1:%'").Scan(&advanced); err != nil {
		t.Fatal(err)
	}
	if advanced != 1 {
		t.Fatalf("activated bridge refresh stored %d advanced rows", advanced)
	}
}
