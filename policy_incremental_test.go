package main

import (
	"database/sql"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

type recordingListQueries struct {
	queryer
	reads []int
}

func (q *recordingListQueries) Query(query string, args ...any) (*sql.Rows, error) {
	if strings.Contains(query, "SELECT domain FROM blocked_domains") {
		id, ok := args[0].(int)
		if !ok {
			return nil, fmt.Errorf("recording fixture expected integer list ID, got %T", args[0])
		}
		q.reads = append(q.reads, id)
	}
	return q.queryer.Query(query, args...)
}

func TestIncrementalReloadReadsOnlyChangedList(t *testing.T) {
	defer setupTestDB(t)()
	published := insertTestBlocklist(t, "Published", true, "ads.example", "*.tracker.example")
	manual := insertTestBlocklist(t, "Manual", true, "old.example")
	if _, err := db.Exec("UPDATE blocklists SET url='' WHERE id=?", manual); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES('192.0.2.10',?),('192.0.2.10',?)", published, manual); err != nil {
		t.Fatal(err)
	}
	mustReloadPolicy(t)
	previous := policyState.Load()
	if _, err := db.Exec("UPDATE blocked_domains SET domain='new.example' WHERE blocklist_id=?", manual); err != nil {
		t.Fatal(err)
	}
	tx, err := readDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestRollback(t, tx) }()
	cfg, err := readPolicyConfig(tx)
	if err != nil {
		t.Fatal(err)
	}
	queries := &recordingListQueries{queryer: tx}
	ix, eligible, err := readIncrementalIndex(queries, previous.Index, cfg.Lists, []policycore.ListKey{{ID: manual}})
	if err != nil || !eligible {
		t.Fatalf("incremental build: eligible=%t err=%v", eligible, err)
	}
	if !reflect.DeepEqual(queries.reads, []int{manual}) {
		t.Fatalf("rule reads=%v, want only%d", queries.reads, manual)
	}
	next := policycore.BuildSnapshot(cfg, ix)
	full, err := policycore.BuildIndex(cfg.Lists, listRuleFeed(tx, cfg.Lists))
	if err != nil {
		t.Fatal(err)
	}
	expected := policycore.BuildSnapshot(cfg, full)
	for _, name := range []string{"old.example", "new.example", "ads.example", "sub.tracker.example"} {
		got, want := next.Evaluate("192.0.2.10", netip.MustParseAddr("192.0.2.10"), name, 1), expected.Evaluate("192.0.2.10", netip.MustParseAddr("192.0.2.10"), name, 1)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s incremental=%+v full=%+v", name, got, want)
		}
	}
	if !previous.Index.Contains(previous.Index.SlotOf(manual, false), "old.example") {
		t.Fatal("old snapshot modified")
	}
}

func TestFailedIncrementalReloadKeepsPublishedSnapshot(t *testing.T) {
	defer setupTestDB(t)()
	id := insertTestBlocklist(t, "Ads", true, "ads.example")
	mustReloadPolicy(t)
	before := policyState.Load()
	if _, err := db.Exec("DROP TABLE blocked_domains"); err != nil {
		t.Fatal(err)
	}
	if err := reloadPolicyState("failed changed-list read", reloadListContent, policycore.ListKey{ID: id}); err == nil {
		t.Fatal("failed rule read accepted")
	}
	if policyState.Load() != before {
		t.Fatal("failed update replaced published policy")
	}
}

func TestIncrementalReloadConcurrentConfigAndEvaluations(t *testing.T) {
	defer setupTestDB(t)()
	id := insertTestBlocklist(t, "Ads", true, "ads.example")
	if _, err := db.Exec("INSERT INTO ip_ranges(id,name,cidr) VALUES(1,'Network','192.0.2.0/24')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO range_blocklists(range_id,blocklist_id) VALUES(1,?)", id); err != nil {
		t.Fatal(err)
	}
	mustReloadPolicy(t)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for i := 0; i < 4; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
					evaluatePolicyFull("192.0.2.10", "ads.example", 1)
				}
			}
		}()
	}
	defer func() { close(stop); readers.Wait() }()
	for i := 0; i < 20; i++ {
		alias := fmt.Sprintf("Generation%d", i)
		if _, err := db.Exec("UPDATE blocklists SET alias=? WHERE id=?", alias, id); err != nil {
			t.Fatal(err)
		}
		if err := reloadPolicyState("concurrent changed-list read", reloadListContent, policycore.ListKey{ID: id}); err != nil {
			t.Fatal(err)
		}
		result := evaluatePolicyFull("192.0.2.10", "ads.example", 1)
		if result.Result != "block" || result.ResultSource.PublishedList.ListName != alias {
			t.Fatalf("stale attribution: %+v", result.ResultSource)
		}
	}
}

// A failed reload can leave saved content absent from the live snapshot. A
// later unrelated hint must recover all saved changes, not just its own list.
func TestIncrementalReloadRecoversEarlierFailedList(t *testing.T) {
	defer setupTestDB(t)()
	a := insertTestBlocklist(t, "First", true, "old-a.example")
	b := insertTestBlocklist(t, "Second", true, "old-b.example")
	mustReloadPolicy(t)
	before := policyState.Load()
	if _, err := db.Exec("UPDATE blocked_domains SET domain='new-a.example' WHERE blocklist_id=?", a); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("ALTER TABLE blocked_domains RENAME TO unavailable_rules"); err != nil {
		t.Fatal(err)
	}
	err := reloadPolicyState("failed first list", reloadListContent, policycore.ListKey{ID: a})
	if _, restoreErr := db.Exec("ALTER TABLE unavailable_rules RENAME TO blocked_domains"); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil || policyState.Load() != before {
		t.Fatalf("failed reload err=%v replaced=%t", err, policyState.Load() != before)
	}
	if _, err := db.Exec("UPDATE blocked_domains SET domain='new-b.example' WHERE blocklist_id=?", b); err != nil {
		t.Fatal(err)
	}
	if err := reloadPolicyState("second list recovers pending content", reloadListContent, policycore.ListKey{ID: b}); err != nil {
		t.Fatal(err)
	}
	ix := policyState.Load().Index
	for _, c := range []struct {
		id        int
		old, next string
	}{{a, "old-a.example", "new-a.example"}, {b, "old-b.example", "new-b.example"}} {
		slot := ix.SlotOf(c.id, false)
		if ix.Contains(slot, c.old) || !ix.Contains(slot, c.next) {
			t.Errorf("list%d remains stale after successful recovery", c.id)
		}
	}
}
