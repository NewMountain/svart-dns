package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

// Persist actual database and file fixtures before the standard test cleanup.
// SVART_HISTORY_RECHECK_EVIDENCE is optional so the regressions remain hermetic.
func preserveHistoryRecheck(t *testing.T) string {
	t.Helper()
	root := os.Getenv("SVART_HISTORY_RECHECK_EVIDENCE")
	if root == "" {
		return t.TempDir()
	}
	path, err := os.MkdirTemp(root, "fixture-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("preserved fixture=%s", path)
	database, archives := db, archivePath
	t.Cleanup(func() {
		if _, err := database.Exec("VACUUM INTO ?", filepath.Join(path, "main.sqlite")); err != nil {
			t.Error(err)
		}
		// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
		if output, err := exec.Command("cp", "-a", archives, filepath.Join(path, "archive-final")).CombinedOutput(); err != nil {
			t.Errorf("preserve archives: %v %s", err, output)
		}
	})
	return path
}

func TestRecheckReadySummaryRejectsValidReplacement(t *testing.T) {
	for _, mutation := range []string{"in-place-partial", "renamed-same-count"} {
		t.Run(mutation, func(t *testing.T) {
			day := independentArchiveFixture(t)
			saved := preserveHistoryRecheck(t)
			for _, name := range []string{"first-original.example.", "second-original.example."} {
				independentArchiveInsert(t, "2026-09-01 12:00:00", name)
			}
			if count, err := archiveDay(day); err != nil || count != 2 {
				t.Fatalf("archive=%d,%v", count, err)
			}
			path := filepath.Join(archivePath, archiveDayFileName(day))
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
			if err := os.WriteFile(filepath.Join(saved, "original.parquet"), original, 0600); err != nil {
				t.Fatal(err)
			}
			all, err := parquet.ReadFile[QueryLogRow](path)
			if err != nil {
				t.Fatal(err)
			}
			if len(all) != 2 || independentHotCount(t) != 0 {
				t.Fatal("fixture did not retire two originals")
			}
			if err := initDuckDB(testDBPath(t), archivePath); err != nil {
				t.Fatal(err)
			}
			defer closeDuckDB()
			assertInvestigationArchiveCount(t, 2)
			badRows := []QueryLogRow{all[0]}
			if mutation == "renamed-same-count" {
				badRows = append([]QueryLogRow(nil), all...)
				badRows[1].CoalescedCount = 700
			}
			badPath := filepath.Join(saved, "damaged-valid.parquet")
			if err := parquet.WriteFile(badPath, badRows); err != nil {
				t.Fatal(err)
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			damaged, err := os.ReadFile(badPath)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "in-place-partial" {
				// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
				if err := os.WriteFile(path, damaged, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				replacement := path + ".replacement"
				// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
				if err := os.WriteFile(replacement, damaged, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			status, _, body := postInvestigate(t, "SELECT count(*),sum(coalesced_count) FROM query_logs")
			if err := os.WriteFile(filepath.Join(saved, "damaged-query-response.json"), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			t.Logf("ready manifest count=2; query HTTP%d %s; want unavailable", status, body)
			if status != http.StatusServiceUnavailable {
				t.Errorf("damaged ready owner query HTTP%d; expected503", status)
			}
			for _, surface := range []struct {
				name    string
				handler http.HandlerFunc
			}{{"schema", handleAPIInvestigateSchema}, {"status", handleAPIArchiveStatus}} {
				rec := httptest.NewRecorder()
				surface.handler(rec, httptest.NewRequest(http.MethodGet, "/api/"+surface.name, nil))
				if err := os.WriteFile(filepath.Join(saved, "damaged-"+surface.name+"-response.json"), rec.Body.Bytes(), 0600); err != nil {
					t.Fatal(err)
				}
				t.Logf("damaged %s HTTP%d", surface.name, rec.Code)
				if rec.Code >= 200 && rec.Code < 300 {
					t.Errorf("damaged ready owner %s returned success HTTP%d", surface.name, rec.Code)
				}
			}
			// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 5)
			if err != nil || fmt.Sprint(rows) != "[[2 14]]" {
				t.Fatalf("restored complete history=%v,%v", rows, err)
			}
			assertInvestigationArchiveCount(t, 2)
		})
	}
}

func TestRecheckOutboxExactBytesAndAtomicRollback(t *testing.T) {
	independentArchiveFixture(t)
	preserveHistoryRecheck(t)
	if _, err := db.Exec(queryLokiOutboxSchema); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\"line\":\"lookup example.org\",\"unknown\":\"雪\"}\n")
	for _, trial := range []struct {
		name     string
		payload  []byte
		conflict bool
	}{
		{"initial", original, false}, {"identical", original, false}, {"different-newline", bytes.TrimSuffix(original, []byte("\n")), true},
	} {
		t.Run(trial.name, func(t *testing.T) {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { checkTestRollback(t, tx) }()
			if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES('recheck-side-effect','pending') ON CONFLICT(key) DO UPDATE SET value='pending'"); err != nil {
				t.Fatal(err)
			}
			err = insertExactOutbox(tx, "stable-recheck-identity", trial.payload)
			if trial.conflict {
				if err == nil {
					t.Fatal("conflicting exact bytes accepted")
				}
				if err := tx.Rollback(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if trial.conflict {
				var effects int
				if err := db.QueryRow("SELECT count(*) FROM settings WHERE key='recheck-side-effect'").Scan(&effects); err != nil {
					t.Fatal(err)
				}
				if effects != 0 {
					t.Fatalf("conflict committed side effect=%d", effects)
				}
			}
			var got []byte
			if err := db.QueryRow("SELECT payload FROM query_loki_outbox WHERE id='stable-recheck-identity'").Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, original) {
				t.Fatalf("payload=%q want%q", got, original)
			}
			if _, err := db.Exec("DELETE FROM settings WHERE key='recheck-side-effect'"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecheckSummaryManifestInsertIgnoredRetainsSource(t *testing.T) {
	day := independentArchiveFixture(t)
	preserveHistoryRecheck(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "ignore-intent.example.")
	store, err := openSummaryStore()
	if err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, store)
	if _, err := db.Exec("CREATE TRIGGER recheck_ignore_intent BEFORE INSERT ON summary_archive_files BEGIN SELECT RAISE(IGNORE); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveDay(day); err == nil {
		t.Fatal("ignored intent acknowledged")
	}
	if independentHotCount(t) != 1 {
		t.Fatal("ignored intent retired source")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM summary_archive_files").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("manifest count=%d want0", count)
	}
	if _, err := db.Exec("DROP TRIGGER recheck_ignore_intent"); err != nil {
		t.Fatal(err)
	}
	if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	if independentHotCount(t) != 0 {
		t.Fatal("retry did not retire source")
	}
}

func TestRecheckLegacyCommitFailureRetainsExactTail(t *testing.T) {
	independentArchiveFixture(t)
	saved := preserveHistoryRecheck(t)
	path := filepath.Join(saved, "legacy.spool")
	first := []byte("{\"ts\":123,\"query_name\":\"commit-original.example.\",\"future\":\"雪\"}\n")
	tail := []byte("{\"ts\":456,\"query_name\":\"commit-tail.example.\",\"future\":\"exact\\nbytes\"}\n")
	if err := os.WriteFile(path, first, 0600); err != nil {
		t.Fatal(err)
	}
	spool, err := openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	spool.close()
	original := append(append([]byte(nil), first...), tail...)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	journal, err := openDurableJournal(path + ".sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	journal.db.SetMaxOpenConns(1)
	if _, err := journal.db.Exec("PRAGMA foreign_keys=ON; CREATE TABLE recheck_parent(id INTEGER PRIMARY KEY); CREATE TABLE recheck_child(id INTEGER REFERENCES recheck_parent(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER recheck_commit_fault AFTER UPDATE ON journal_meta WHEN NEW.key='legacy_provenance_v1' BEGIN INSERT INTO recheck_child VALUES(999); END"); err != nil {
		t.Fatal(err)
	}
	importTarget := &logSpool{path: path, offsetPath: path + ".offset", journal: journal}
	if err := importTarget.importLegacy(); err == nil {
		t.Fatal("deferred constraint did not fail migration commit")
	}
	rows, err := journal.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].payload, first) {
		t.Fatalf("failed migration committed tail: %+v", rows)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, original) {
		t.Fatal("failed commit altered original bytes")
	}
	if _, err := journal.db.Exec("DROP TRIGGER recheck_commit_fault"); err != nil {
		t.Fatal(err)
	}
	if err := importTarget.importLegacy(); err != nil {
		t.Fatal(err)
	}
	if err := journal.close(); err != nil {
		t.Fatal(err)
	}
	spool, err = openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	rows, err = spool.journal.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !bytes.Equal(rows[0].payload, first) || !bytes.Equal(rows[1].payload, tail) {
		t.Fatalf("reopened exact originals=%+v", rows)
	}
}
