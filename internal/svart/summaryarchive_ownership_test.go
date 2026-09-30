package svart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestSummaryArchiveCrashChild(t *testing.T) {
	path := os.Getenv("SVART_SUMMARY_CRASH_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	var err error
	db, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	readDB, err = sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	archivePath = os.Getenv("SVART_SUMMARY_CRASH_ARCHIVE")
	summaryArchiveCheckpoint = func(stage string) error {
		if stage == os.Getenv("SVART_SUMMARY_CRASH_STAGE") {
			os.Exit(79)
		}
		return nil
	}
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if strings.HasPrefix(os.Getenv("SVART_SUMMARY_CRASH_STAGE"), "expir") {
		if err := expireSummaryFiles(archivePath, 1); err != nil {
			t.Fatal(err)
		}
	} else if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash boundary was not reached")
}

func TestSummaryArchiveAbruptCrashMatrix(t *testing.T) {
	for _, stage := range []string{"staged", "intent", "renamed", "published", "deleted", "ready", "retired", "expire-intent", "expire-unlinked", "expired"} {
		t.Run(stage, func(t *testing.T) {
			day := independentArchiveFixture(t)
			independentArchiveInsert(t, "2026-09-01 12:00:00", "crash-original.example.")
			expiry := strings.HasPrefix(stage, "expir")
			if expiry {
				if _, err := archiveDay(day); err != nil {
					t.Fatal(err)
				}
			}
			// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
			cmd := exec.Command(os.Args[0], "-test.run=^TestSummaryArchiveCrashChild$")
			cmd.Env = append(os.Environ(), "SVART_SUMMARY_CRASH_DB="+testDBPath(t), "SVART_SUMMARY_CRASH_ARCHIVE="+archivePath, "SVART_SUMMARY_CRASH_STAGE="+stage)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 79 {
				t.Fatalf("child=%v output=%s", err, output)
			}
			if expiry {
				if err := expireSummaryFiles(archivePath, 1); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(archivePath, archiveDayFileName(day))); !os.IsNotExist(err) {
					t.Fatalf("expired file err=%v", err)
				}
				if err := checkSummaryAvailability(t.Context(), testDBPath(t), archivePath); err != nil {
					t.Fatal(err)
				}
				return
			}
			independentArchiveInsert(t, "2026-09-01 13:00:00", "late-after-crash.example.")
			if _, err := archiveDay(day); err != nil {
				t.Fatal(err)
			}
			if got := independentHotCount(t); got != 0 {
				t.Fatalf("hot=%d want0", got)
			}
			names, err := filepath.Glob(filepath.Join(archivePath, "*.parquet"))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]int{}
			for _, name := range names {
				rows, err := parquet.ReadFile[QueryLogRow](name)
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					got[row.QueryName]++
					if row.CoalescedCount != 7 {
						t.Fatal(row)
					}
				}
			}
			if len(got) != 2 || got["crash-original.example."] != 1 || got["late-after-crash.example."] != 1 {
				t.Fatalf("cold=%v want each original exactly once", got)
			}
			if err := initDuckDB(testDBPath(t), archivePath); err != nil {
				t.Fatal(err)
			}
			defer closeDuckDB()
			_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 5)
			if err != nil || fmt.Sprint(rows) != "[[2 14]]" {
				t.Fatalf("investigation=%v err=%v", rows, err)
			}
		})
	}
}

func TestSummaryArchiveOwnershipFailuresAndRetry(t *testing.T) {
	for _, mutation := range []string{"delete-ignore", "lock-ignore", "ready-ignore", "commit-failure", "source-change", "corrupt-published", "missing-published"} {
		t.Run(mutation, func(t *testing.T) {
			day := independentArchiveFixture(t)
			independentArchiveInsert(t, "2026-09-01 12:00:00", "owned.example.")
			summaryArchiveCheckpoint = func(stage string) error {
				if stage == "published" {
					return errors.New("publication interruption")
				}
				return nil
			}
			t.Cleanup(func() { summaryArchiveCheckpoint = nil })
			if _, err := archiveDay(day); err == nil {
				t.Fatal("expected interrupted publication")
			}
			summaryArchiveCheckpoint = nil
			if _, err := investigateViewSQL(t.Context(), testDBPath(t), archivePath); !errors.Is(err, errInvestigateArchiveUnavailable) {
				t.Fatalf("pending should be unavailable: %v", err)
			}
			if _, err := getArchiveStatus(t.Context()); err == nil {
				t.Fatal("pending status falsely successful")
			}
			path := filepath.Join(archivePath, archiveDayFileName(day))
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var oldPolicy sql.NullString
			if err := db.QueryRow("SELECT policy_json FROM query_logs WHERE query_name='owned.example.'").Scan(&oldPolicy); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "delete-ignore":
				_, err = db.Exec("CREATE TRIGGER summary_fault BEFORE DELETE ON query_logs BEGIN SELECT RAISE(IGNORE); END")
			case "lock-ignore":
				_, err = db.Exec("CREATE TRIGGER summary_fault BEFORE UPDATE ON summary_archive_files BEGIN SELECT RAISE(IGNORE); END")
			case "ready-ignore":
				_, err = db.Exec("CREATE TRIGGER summary_fault BEFORE UPDATE OF state ON summary_archive_files WHEN NEW.state='ready' BEGIN SELECT RAISE(IGNORE); END")
			case "commit-failure":
				_, err = db.Exec("CREATE TABLE summary_parent(id INTEGER PRIMARY KEY); CREATE TABLE summary_child(id INTEGER REFERENCES summary_parent(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER summary_fault AFTER DELETE ON query_logs BEGIN INSERT INTO summary_child VALUES(999); END")
			case "source-change":
				_, err = db.Exec("UPDATE query_logs SET policy_json='changed' WHERE query_name='owned.example.'")
			case "corrupt-published":
				err = os.WriteFile(path, []byte("corrupted archive"), 0600)
			case "missing-published":
				err = os.Rename(path, path+".saved")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := archiveDay(day); err == nil {
				t.Fatal("failed ownership accepted")
			}
			if got := independentHotCount(t); got != 1 {
				t.Fatalf("hot=%d want1", got)
			}
			switch mutation {
			case "delete-ignore", "lock-ignore", "ready-ignore", "commit-failure":
				_, err = db.Exec("DROP TRIGGER summary_fault")
			case "source-change":
				_, err = db.Exec("UPDATE query_logs SET policy_json=? WHERE query_name='owned.example.'", oldPolicy)
			case "corrupt-published":
				// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
				err = os.WriteFile(path, original, 0600)
			case "missing-published":
				err = os.Rename(path+".saved", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := archiveDay(day); err != nil {
				t.Fatal(err)
			}
			if got := independentHotCount(t); got != 0 {
				t.Fatalf("retry hot=%d want0", got)
			}
		})
	}
}

func TestSummaryArchiveMissingExpectedAndLegacyOverlap(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "expected.example.")
	if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	moved := archivePath + "-saved"
	if err := os.Rename(archivePath, moved); err != nil {
		t.Fatal(err)
	}
	restored := false
	defer func() {
		if restored {
			return
		}
		if err := os.Rename(moved, archivePath); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	if _, err := investigateViewSQL(t.Context(), testDBPath(t), archivePath); !errors.Is(err, errInvestigateArchiveUnavailable) {
		t.Fatalf("missing expected storage=%v", err)
	}
	if _, err := getArchiveStatus(t.Context()); err == nil {
		t.Fatal("missing expected status successful")
	}
	if err := os.Rename(moved, archivePath); err != nil {
		t.Fatal(err)
	}
	restored = true
	if _, err := db.Exec("DROP TABLE summary_archive_files"); err != nil {
		t.Fatal(err)
	}
	independentArchiveInsert(t, "2026-09-01 13:00:00", "ambiguous-late.example.")
	if _, err := archiveDay(day); err == nil {
		t.Fatal("legacy overlap falsely acknowledged")
	}
	if _, err := investigateViewSQL(t.Context(), testDBPath(t), archivePath); !errors.Is(err, errInvestigateArchiveUnavailable) {
		t.Fatalf("legacy overlap=%v", err)
	}
	if got := independentHotCount(t); got != 1 {
		t.Fatalf("legacy hot=%d want1", got)
	}
	if got := independentArchiveRows(t, day); len(got) != 1 || got[0].QueryName != "expected.example." {
		t.Fatalf("legacy changed=%v", got)
	}
}

func TestSummaryArchiveBoundedSegmentsAndLateArrival(t *testing.T) {
	day := independentArchiveFixture(t)
	if _, err := db.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?) INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,coalesced_count) SELECT '2026-09-01 12:00:00','192.0.2.40','segment-'||x||'.example.','A',3 FROM n`, summarySegmentRows+3); err != nil {
		t.Fatal(err)
	}
	n, err := archiveDay(day)
	if err != nil || n != summarySegmentRows+3 {
		t.Fatalf("archive=%d,%v", n, err)
	}
	independentArchiveInsert(t, "2026-09-01 13:00:00", "late-segment.example.")
	if n, err := archiveDay(day); err != nil || n != 1 {
		t.Fatalf("late=%d,%v", n, err)
	}
	rows, err := db.Query("SELECT count FROM summary_archive_files ORDER BY count DESC")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, rows) }()
	var counts []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts = append(counts, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("initial rows=%d, initial files=2, late rows=1, final files=%d, segment counts=%v, row-group target=%d", summarySegmentRows+3, len(counts), counts, summaryRowGroupRows)
	if fmt.Sprint(counts) != fmt.Sprintf("[%d 3 1]", summarySegmentRows) {
		t.Fatalf("segments=%v", counts)
	}
}

func TestSummaryArchiveLateArrivalAfterExpiry(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "expired-original.example.")
	if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	if err := expireSummaryFiles(archivePath, 1); err != nil {
		t.Fatal(err)
	}
	independentArchiveInsert(t, "2026-09-01 13:00:00", "late-after-expiry.example.")
	if n, err := archiveDay(day); err != nil || n != 1 {
		t.Fatalf("late archive=%d,%v", n, err)
	}
	if err := checkSummaryAvailability(t.Context(), testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(archivePath, "*.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) == archiveDayFileName(day) {
		t.Fatalf("late files=%v", paths)
	}
	got, err := parquet.ReadFile[QueryLogRow](paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].QueryName != "late-after-expiry.example." {
		t.Fatalf("late rows=%v", got)
	}
}

func TestSummaryArchiveStopAndRetentionPreservePendingOwner(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "pending-retention.example.")
	oldStop := archiveStop
	archiveStop = make(chan struct{})
	t.Cleanup(func() { archiveStop = oldStop; summaryArchiveCheckpoint = nil })
	summaryArchiveCheckpoint = func(stage string) error {
		if stage == "published" {
			close(archiveStop)
		}
		return nil
	}
	if _, err := archiveDay(day); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("stop=%v", err)
	}
	summaryArchiveCheckpoint = nil
	if _, err := db.Exec("UPDATE settings SET value='1' WHERE key='log_retention_days'"); err != nil {
		t.Fatal(err)
	}
	cleanupOldLogs()
	if err := expireSummaryFiles(archivePath, 1); err != nil {
		t.Fatal(err)
	}
	if got := independentHotCount(t); got != 1 {
		t.Fatalf("pending source=%d want1", got)
	}
	if got := independentArchiveRows(t, day); len(got) != 1 {
		t.Fatal(got)
	}
	archiveStop = make(chan struct{})
	if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	if got := independentHotCount(t); got != 0 {
		t.Fatalf("recovered source=%d want0", got)
	}
}

type observedSummaryContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *observedSummaryContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}
func TestSummaryArchiveSerializationWaitsAreCancelable(t *testing.T) {
	for _, kind := range []string{"query-cancel", "archive-stop"} {
		t.Run(kind, func(t *testing.T) {
			investigateMu.Lock()
			defer investigateMu.Unlock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := &observedSummaryContext{Context: ctx, observed: make(chan struct{})}
			stop := make(chan struct{})
			done := make(chan error, 1)
			go func() { done <- lockInvestigation(observed, stop) }()
			<-observed.observed
			if kind == "query-cancel" {
				cancel()
			} else {
				close(stop)
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("canceled serialized wait succeeded")
				}
				if kind == "query-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if kind == "archive-stop" && !strings.Contains(err.Error(), "stopped") {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("serialized wait ignored cancellation")
			}
		})
	}
}
