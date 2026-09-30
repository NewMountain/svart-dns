package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestBridgeHistoryPreservesFrozenReaderUntilActivation(t *testing.T) {
	bridgeWriterTestDB(t)
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, "old.example", "prior.example")
	res, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES (?,'History bridge',1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if err := refreshBlocklistByID(int(id)); err != nil {
			t.Fatal(err)
		}
	}
	samples := func() (int, string, string) {
		t.Helper()
		var historyID int
		var added, removed string
		if err := db.QueryRow("SELECT id,sample_added,sample_removed FROM blocklist_history WHERE blocklist_id=? ORDER BY id DESC LIMIT 1", id).Scan(&historyID, &added, &removed); err != nil {
			t.Fatal(err)
		}
		return historyID, added, removed
	}
	refresh()
	_, added, removed := samples()
	if added != "old.example,prior.example" || removed != "" {
		t.Errorf("inactive initial samples=(%q,%q), want legacy comma text and empty removal", added, removed)
	}
	// Frozen a918 uses splitDomains directly, without versioned decoding.
	if got := splitDomains(added); !reflect.DeepEqual(got, []string{"old.example", "prior.example"}) {
		t.Errorf("frozen reader samples=%q, want [old.example prior.example]", got)
	}
	server.serve("a.example", "b.example", "c.example", "d.example", "e.example", "f.example", "g.example", "h.example", "i.example", "j.example", "k.example", "l.example")
	refresh()
	historyID, added, removed := samples()
	if added != "a.example,b.example,c.example,d.example,e.example,f.example,g.example,h.example,i.example,j.example" || removed != "old.example,prior.example" {
		t.Errorf("inactive replacement samples=(%q,%q), want exact legacy first-ten samples", added, removed)
	}
	var changes int
	if err := db.QueryRow("SELECT count(*) FROM blocklist_changelog WHERE history_id=?", historyID).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if changes != 14 {
		t.Fatalf("complete changelog has %d rows, want 12 added and 2 removed", changes)
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, []string{"a.example", "b.example", "c.example", "d.example", "e.example", "f.example", "g.example", "h.example", "i.example", "j.example", "k.example", "l.example"}) {
		t.Fatalf("complete stored rules=%q", got)
	}
	listWriterRole = "active"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	listWriterRole = "bridge"
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	const rule = `/^ad-[a-z]{2,4}\.example$/$dnstype=TXT,important`
	server.serve(rule)
	refresh()
	_, added, removed = samples()
	if !strings.HasPrefix(added, "!svart-sample-v1:") || !strings.HasPrefix(removed, "!svart-sample-v1:") {
		t.Fatalf("activated bridge samples are not versioned JSON: (%q,%q)", added, removed)
	}
	decoded, err := decodeHistorySample(added)
	if err != nil || !reflect.DeepEqual(decoded, []string{rule}) {
		t.Fatalf("activated bridge comma-bearing sample=%q err=%v, want complete original rule", decoded, err)
	}
	refresh()
	_, added, removed = samples()
	if added != "!svart-sample-v1:[]" || removed != "!svart-sample-v1:[]" {
		t.Fatalf("activated unchanged samples=(%q,%q), want JSON empty arrays", added, removed)
	}
}
