package svart

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// storedRules returns a list's stored rules, sorted.
func storedRules(t *testing.T, table, idCol string, id int) []string {
	t.Helper()
	rows, err := db.Query("SELECT domain FROM "+table+" WHERE "+idCol+" = ? ORDER BY domain", id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, rows) }()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// TestListRefreshIsAllOrNothing: when storing a downloaded list fails part
// way (here a row the database refuses), the refresh fails and the previous
// version stays stored and in effect. It used to ignore the failed insert and
// commit whatever rows had been written.
func TestListRefreshIsAllOrNothing(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	for _, kind := range []struct {
		name, lists, rows, idCol string
		refresh                  func(int) error
	}{
		{"blocklist", "blocklists", "blocked_domains", "blocklist_id", refreshBlocklistByID},
		{"allowlist", "allowlists", "allowed_domains", "allowlist_id", refreshAllowlistByID},
	} {
		t.Run(kind.name, func(t *testing.T) {
			defer setupTestDB(t)()
			v1 := []string{"ads.google.com", "pixel.facebook.com"}
			server := newGatedListServer(t, v1...)
			res, err := db.Exec("INSERT INTO "+kind.lists+" (url, alias, enabled) VALUES (?, 'Hagezi Pro', 1)", server.srv.URL+"/pro.txt")
			if err != nil {
				t.Fatal(err)
			}
			id64, fixtureErr1578 := res.LastInsertId()
			if fixtureErr1578 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr1578)
			}
			id := int(id64)
			if err := kind.refresh(id); err != nil {
				t.Fatalf("first refresh: %v", err)
			}

			// The database rejects one row of the next version.
			if _, err := db.Exec(`CREATE TRIGGER reject_row BEFORE INSERT ON ` + kind.rows + `
				WHEN NEW.domain = 'metrics.poisoned.example' BEGIN SELECT RAISE(ABORT, 'database or disk is full'); END`); err != nil {
				t.Fatal(err)
			}
			server.serve("ads.google.com", "metrics.poisoned.example", "telemetry.microsoft.com")
			if err := kind.refresh(id); err == nil {
				t.Fatal("refresh succeeded although a row could not be stored")
			}

			if got := storedRules(t, kind.rows, kind.idCol, id); !reflect.DeepEqual(got, v1) {
				t.Errorf("stored rules after the failed refresh = %v, want the previous version %v", got, v1)
			}
			var count int
			if err := db.QueryRow("SELECT domain_count FROM "+kind.lists+" WHERE id = ?", id).Scan(&count); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			if count != len(v1) {
				t.Errorf("domain_count = %d, want %d", count, len(v1))
			}
			ix := policyState.Load().Index
			var rules []string
			ix.Rules(ix.SlotOf(id, kind.name == "allowlist"), func(r string) { rules = append(rules, r) })
			slices.Sort(rules)
			if !reflect.DeepEqual(rules, v1) {
				t.Errorf("rules in effect = %v, want %v", rules, v1)
			}
		})
	}
}

// A checkpoint cannot be reconstructed after even one missing diff row.
func TestBlocklistRefreshChangelogFailureRollsBackGeneration(t *testing.T) {
	defer setupTestDB(t)()
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, "ads.google.com", "pixel.facebook.com")
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Atomic history', 1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	before := policyState.Load()
	if _, err := db.Exec(`CREATE TRIGGER reject_diff BEFORE INSERT ON blocklist_changelog BEGIN SELECT RAISE(ABORT, 'history storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	server.serve("ads.google.com", "telemetry.microsoft.com")
	if err := refreshBlocklistByID(int(id)); err == nil {
		t.Error("refresh succeeded although changelog could not be stored")
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, []string{"ads.google.com", "pixel.facebook.com"}) {
		t.Errorf("rules changed on failed history write: %v", got)
	}
	var histories int
	if err := db.QueryRow("SELECT COUNT(*) FROM blocklist_history WHERE blocklist_id = ?", id).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if histories != 1 {
		t.Errorf("history count = %d, want 1", histories)
	}
	if policyState.Load() != before {
		t.Error("failed refresh published a policy generation")
	}
}

func TestBlocklistRefreshHistoryFailureRollsBackGeneration(t *testing.T) {
	defer setupTestDB(t)()
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Atomic history', 1)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	original := []string{"@@safe.ads.example", "ads.example"}
	if err := storeListGeneration(blockListStore, int(id), original, 1, "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_history BEFORE INSERT ON blocklist_history BEGIN SELECT RAISE(ABORT, 'history storage unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := storeListGeneration(blockListStore, int(id), []string{"tracker.example"}, 1, "", nil); err == nil {
		t.Fatal("history failure was ignored")
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, original) {
		t.Fatalf("rules changed: %v", got)
	}
}

func TestBlocklistRefreshCheckpointAcrossEmptyGeneration(t *testing.T) {
	defer setupTestDB(t)()
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Atomic history', 1)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	original := []string{"@@safe.ads.example", "ads.example"}
	if err := storeListGeneration(blockListStore, int(id), original, 1, "", nil); err != nil {
		t.Fatal(err)
	}
	var historyID int
	if err := db.QueryRow("SELECT max(id) FROM blocklist_history WHERE blocklist_id=?", id).Scan(&historyID); err != nil {
		t.Fatal(err)
	}
	for _, rules := range [][]string{nil, {"other.example"}, {"@@safe.ads.example", "last.example"}} {
		count := len(rules)
		if count == 2 {
			count--
		}
		if err := storeListGeneration(blockListStore, int(id), rules, count, "", nil); err != nil {
			t.Fatal(err)
		}
		page, err := queryCheckpointPage(context.Background(), historyID, "", 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(page.Domains, original) || page.Total != len(original) {
			t.Fatalf("checkpoint after %v = %+v, want %v", rules, page, original)
		}
	}
}

func TestBlocklistRefreshConcurrentCheckpointIsComplete(t *testing.T) {
	defer setupTestDB(t)()
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Atomic history', 1)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	original := []string{"@@safe.ads.example", "ads.example"}
	if err := storeListGeneration(blockListStore, int(id), original, 1, "", nil); err != nil {
		t.Fatal(err)
	}
	var historyID int
	if err := db.QueryRow("SELECT max(id) FROM blocklist_history WHERE blocklist_id=?", id).Scan(&historyID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			rules := []string{fmt.Sprintf("ads%d.example", i), fmt.Sprintf("@@safe.ads%d.example", i)}
			if err := storeListGeneration(blockListStore, int(id), rules, 1, "", nil); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		page, err := queryCheckpointPage(context.Background(), historyID, "", 100, 0)
		if err != nil {
			t.Error(err)
			break
		}
		if !reflect.DeepEqual(page.Domains, original) || page.Total != 2 {
			t.Errorf("inconsistent checkpoint: %+v", page)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestListGenerationRejectsIgnoredWrites(t *testing.T) {
	for _, test := range []struct{ name, table, operation string }{
		{"history", "blocklist_history", "INSERT"},
		{"changelog", "blocklist_changelog", "INSERT"},
		{"rule insertion", "blocked_domains", "INSERT"},
		{"rule deletion", "blocked_domains", "DELETE"},
		{"metadata update", "blocklists", "UPDATE"},
		{"local generation insert", "local_blocklist_generations", "INSERT"},
		{"local generation update", "local_blocklist_generations", "UPDATE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer setupTestDB(t)()
			result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Ignored write', 1)")
			if err != nil {
				t.Fatal(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			original := []string{"ads.example"}
			if err := storeListGeneration(blockListStore, int(id), original, 1, "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("CREATE TRIGGER ignore_write BEFORE " + test.operation + " ON " + test.table + " BEGIN SELECT RAISE(IGNORE); END"); err != nil {
				t.Fatal(err)
			}
			if err := storeListGeneration(blockListStore, int(id), []string{"tracker.example"}, 1, "", nil); err == nil {
				t.Error("ignored write accepted")
			}
			if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, original) {
				t.Errorf("stored rules = %v, want %v", got, original)
			}
			var histories int
			if err := db.QueryRow("SELECT count(*) FROM blocklist_history WHERE blocklist_id=?", id).Scan(&histories); err != nil {
				t.Fatal(err)
			}
			if histories != 1 {
				t.Errorf("history count = %d, want 1", histories)
			}
		})
	}
}
