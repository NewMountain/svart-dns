package svart

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/parquet-go/parquet-go"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRawArchiveRealDiskFullPreservesSourceAndRetries(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	p := rawArchiveAppend(t, j, now.UnixNano(), "disk full exact original")
	rawArchiveProgress(t, source, j, 1)
	a.createTemp = func(string) (*os.File, error) { return os.OpenFile("/dev/full", os.O_WRONLY, 0) }
	if err := a.cycle(now); !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("error=%v wantENOSPC", err)
	}
	rawArchiveCount(t, j, 1)
	a.createTemp = nil
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	count := 0
	if err := a.verify(func(r rawArchiveRow) error {
		count++
		if !bytes.Equal(r.Payload, p) {
			return errors.New("raw payload changed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("verified=%d want1", count)
	}
}
func TestRawArchiveCrashChild(t *testing.T) {
	dir := os.Getenv("SVART_RAW_CRASH_DIR")
	if dir == "" {
		t.Skip("subprocess only")
	}
	j, err := openDurableJournal(filepath.Join(dir, "query.spool.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := sql.Open("sqlite3", filepath.Join(dir, "main.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	a, err := newRawArchiver(j, source, filepath.Join(dir, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	a.checkpoint = func(stage string) error {
		if stage == os.Getenv("SVART_RAW_CRASH_STAGE") {
			os.Exit(79)
		}
		return nil
	}
	if err = a.cycle(time.Date(2026, 9, 26, 12, 0, 0, 123, time.UTC)); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash boundary not reached")
}
func TestRawArchiveAbruptCrashMatrix(t *testing.T) {
	for _, stage := range []string{"planned", "temp-synced", "renamed", "published", "verified", "before-retire", "retire-deleted", "retired", "before-expire-commit", "expired", "obsolete-removed"} {
		t.Run(stage, func(t *testing.T) {
			a, j, source := rawArchiveFixture(t)
			now := time.Date(2026, 9, 26, 12, 0, 0, 123, time.UTC)
			p1 := rawArchiveAppend(t, j, now.AddDate(0, 0, -2).UnixNano(), "old event")
			p2 := rawArchiveAppend(t, j, now.UnixNano(), "current event")
			rawArchiveProgress(t, source, j, 2)
			retention := stage == "before-expire-commit" || stage == "expired" || stage == "obsolete-removed"
			if retention {
				if err := a.cycle(now); err != nil {
					t.Fatal(err)
				}
				if _, err := source.Exec("UPDATE settings SET value='1'"); err != nil {
					t.Fatal(err)
				}
			}
			// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
			cmd := exec.Command(os.Args[0], "-test.run=^TestRawArchiveCrashChild$")
			cmd.Env = append(os.Environ(), "SVART_RAW_CRASH_DIR="+filepath.Dir(j.path), "SVART_RAW_CRASH_STAGE="+stage)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 79 {
				t.Fatalf("child=%v output=%s", err, output)
			}
			if err = a.cycle(now); err != nil {
				t.Fatal(err)
			}
			rawArchiveCount(t, j, 0)
			var got []rawArchiveRow
			if err = a.verify(func(r rawArchiveRow) error { got = append(got, r); return nil }); err != nil {
				t.Fatal(err)
			}
			if retention {
				if len(got) != 1 || got[0].Sequence != 2 || !bytes.Equal(got[0].Payload, p2) {
					t.Fatalf("retained=%+v want exact sequence2", got)
				}
			} else {
				if len(got) != 2 || !bytes.Equal(got[0].Payload, p1) || !bytes.Equal(got[1].Payload, p2) {
					t.Fatalf("retained=%+v want both originals", got)
				}
			}
			paths, err := filepath.Glob(filepath.Join(a.dir, "*", "*", "*", "*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(paths) != 1 {
				t.Fatalf("files=%v want one live segment without abandoned derived files", paths)
			}
			var report bytes.Buffer
			if err = verifyRawArchives(j.path, filepath.Join(filepath.Dir(j.path), "archives"), &report); err != nil {
				t.Fatal(err)
			}
			want := 2
			if retention {
				want = 1
			}
			if !strings.Contains(report.String(), fmt.Sprintf("archived_records=%d journal_records=0 quarantine_records=0", want)) {
				t.Fatalf("verification report=%s", report.String())
			}
		})
	}
}
func TestRawArchiveBoundedSegmentsAndJournalPageReuse(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	var maxPages int64
	for round := 0; round < 3; round++ {
		payloads := make([][]byte, 2048)
		for i := range payloads {
			payloads[i] = []byte(fmt.Sprintf(`{"ts":%d,"query_name":"reuse.example.","metadata":%q}`, now.UnixNano(), strings.Repeat("r", 2048)))
		}
		last, err := j.appendBatch(payloads)
		if err != nil {
			t.Fatal(err)
		}
		rawArchiveProgress(t, source, j, last)
		var pages int64
		if err = j.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			maxPages = pages + 32
		} else if pages > maxPages {
			t.Fatalf("journal grew despite reusable free pages: round=%d pages=%d first+32=%d", round, pages, maxPages)
		}
		if err = a.cycle(now); err != nil {
			t.Fatal(err)
		}
		rawArchiveCount(t, j, 0)
		var free int64
		if err = j.db.QueryRow("PRAGMA freelist_count").Scan(&free); err != nil {
			t.Fatal(err)
		}
		if free < 1000 {
			t.Fatalf("freed pages=%d want>=1000", free)
		}
		t.Logf("round=%d journal_pages=%d reusable_pages=%d", round, pages, free)
	}
	var files, count, maxCount int
	if err := j.db.QueryRow("SELECT COUNT(*),SUM(count),MAX(count) FROM raw_archive_files WHERE state='ready'").Scan(&files, &count, &maxCount); err != nil {
		t.Fatal(err)
	}
	if files != 3 || count != 6144 || maxCount != 2048 {
		t.Fatalf("files=%d records=%d max_segment_records=%d want3,6144,2048", files, count, maxCount)
	}
}
func TestRawArchivePreservesLegacyQuarantineAndReadFailures(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "valid")
	rawArchiveProgress(t, source, j, 1)
	original := filepath.Join(filepath.Dir(j.path), "legacy.spool")
	if err := os.WriteFile(original, []byte("{incomplete legacy bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec("INSERT INTO journal_quarantine VALUES(?,0,?,'incomplete')", original, []byte("{incomplete legacy bytes")); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Exec("DROP TABLE query_journal_progress"); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err == nil {
		t.Fatal("expected failed cursor read")
	}
	rawArchiveCount(t, j, 1)
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	raw, err := os.ReadFile(original)
	if err != nil || string(raw) != "{incomplete legacy bytes" {
		t.Fatalf("legacy=%q err=%v", raw, err)
	}
	var quarantined string
	if err = j.db.QueryRow("SELECT payload FROM journal_quarantine").Scan(&quarantined); err != nil || quarantined != "{incomplete legacy bytes" {
		t.Fatalf("quarantine=%q err=%v", quarantined, err)
	}
}
func TestRawArchiveWorkerStartupShutdownAndRestart(t *testing.T) {
	defer setupTestDB(t)()
	root := t.TempDir()
	j := queryLogWriter.spool.journal
	queryLogWriter.log(queryLogEntry{ts: time.Now().UnixNano(), clientIP: "192.0.2.10", queryName: "worker.example.", queryType: "A", responseCode: "NOERROR"})
	w := newRawArchiveWorker(j, readDB, root, 10*time.Millisecond)
	t.Cleanup(w.close)
	waitLogCondition(t, func() bool {
		var n int
		err := j.db.QueryRow("SELECT COALESCE(SUM(count),0) FROM raw_archive_files WHERE state='ready'").Scan(&n)
		return err == nil && n == 1
	})
	w.close()
	w.close()
	rawArchiveCount(t, j, 0)
	// Events admitted after archive shutdown remain durable and are resumed by
	// the next startup; shutdown never closes the journal beneath this worker.
	queryLogWriter.log(queryLogEntry{ts: time.Now().UnixNano(), clientIP: "192.0.2.11", queryName: "restart.example.", queryType: "AAAA", responseCode: "NOERROR"})
	waitLogCondition(t, func() bool { return queryLogWriter.spool.read.Load() == 2 })
	if _, err := j.db.Exec("UPDATE journal_meta SET value='0' WHERE key='raw_archive_flush_ns'"); err != nil {
		t.Fatal(err)
	}
	w = newRawArchiveWorker(j, readDB, root, 10*time.Millisecond)
	t.Cleanup(w.close)
	waitLogCondition(t, func() bool {
		var n int
		err := j.db.QueryRow("SELECT SUM(count) FROM raw_archive_files WHERE state='ready'").Scan(&n)
		return err == nil && n == 2
	})
	w.close()
	rawArchiveCount(t, j, 0)
	a, err := newRawArchiver(j, readDB, root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	if err = a.verify(func(r rawArchiveRow) error {
		var v struct {
			QueryName string `json:"query_name"`
		}
		if err := json.Unmarshal(r.Payload, &v); err != nil {
			return err
		}
		names = append(names, v.QueryName)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(names) != "[worker.example. restart.example.]" {
		t.Fatalf("archived names=%v", names)
	}
}

func TestRawArchiveValidParquetWrongPayloadCannotRetire(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "exact original")
	rawArchiveProgress(t, source, j, 1)
	a.checkpoint = func(stage string) error {
		if stage != "published" {
			return nil
		}
		m, err := a.nextManifest("", "pending")
		if err != nil {
			return err
		}
		rows, err := a.readFile(m)
		if err != nil {
			return err
		}
		rows[0].Payload = append(rows[0].Payload, ' ')
		f, err := os.Create(filepath.Join(a.dir, m.name))
		if err != nil {
			return err
		}
		w := parquet.NewGenericWriter[rawArchiveRow](f)
		_, err = w.Write(rows)
		if err == nil {
			err = w.Close()
		}
		checkTestClose(t, f)
		return err
	}
	if err := a.cycle(now); err == nil || (!strings.Contains(err.Error(), "content mismatch") && !strings.Contains(err.Error(), "equality failed")) {
		t.Fatalf("error=%v want content mismatch", err)
	}
	rawArchiveCount(t, j, 1)
}
func TestRawArchiveMaximumRetentionDoesNotOverflow(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, 1, "near Unix epoch")
	rawArchiveProgress(t, source, j, 1)
	if _, err := source.Exec("UPDATE settings SET value='3650000'"); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	n := 0
	if err := a.verify(func(rawArchiveRow) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("retained=%d want1", n)
	}
}
func TestRawArchiveVerifierRejectsMissingAndCorruptOwners(t *testing.T) {
	for _, mode := range []string{"missing", "corrupt", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			a, j, source := rawArchiveFixture(t)
			now := time.Now()
			rawArchiveAppend(t, j, now.UnixNano(), "owned")
			rawArchiveProgress(t, source, j, 1)
			if err := a.cycle(now); err != nil {
				t.Fatal(err)
			}
			m, err := a.nextManifest("", "ready")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(a.dir, m.name)
			switch mode {
			case "missing":
				err = os.Remove(path)
			case "corrupt":
				err = os.WriteFile(path, []byte("corrupt footer"), 0600)
			case "unknown":
				err = os.WriteFile(filepath.Join(a.dir, "unrecognized.partial"), []byte("recoverable bytes"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err = verifyRawArchives(j.path, filepath.Join(filepath.Dir(j.path), "archives"), &output); err == nil {
				t.Fatal("expected failed operator verification")
			}
			if output.Len() != 0 {
				t.Fatalf("unexpected success output=%q", output.String())
			}
		})
	}
}

func TestRawArchiveOperatorFixture(t *testing.T) {
	destination := os.Getenv("SVART_RAW_OPERATOR_FIXTURE")
	if destination == "" {
		t.Skip("explicit scratch-only operator fixture")
	}
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "operator full-payload verification")
	rawArchiveProgress(t, source, j, 1)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	// SQLite's VACUUM INTO is a consistent backup on this disposable, idle test
	// database only. Production archival never uses VACUUM.
	// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
	if err := os.MkdirAll(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec("VACUUM INTO ?", filepath.Join(destination, "query.spool.sqlite")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(destination, "archives", "raw-v1", j.identity)
	// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	m, err := a.nextManifest("", "ready")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(a.dir, m.name))
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
	if err = os.MkdirAll(filepath.Dir(filepath.Join(target, m.name)), 0700); err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
	if err = os.WriteFile(filepath.Join(target, m.name), data, 0600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	if err = verifyRawArchives(filepath.Join(destination, "query.spool.sqlite"), filepath.Join(destination, "archives"), &report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report.String(), "archived_records=1 journal_records=0 quarantine_records=0") {
		t.Fatalf("report=%s", report.String())
	}
}

// readFile is only a bounded fixture helper; production streams via fileSource.
func (a *rawArchiver) readFile(m rawArchiveManifest) ([]rawArchiveRow, error) {
	if m.count > rawArchiveBatchRows {
		return nil, errors.New("use streaming raw archive reader for a complete segment")
	}
	var result []rawArchiveRow
	err := walkRawArchive(a.fileSource(m), func(rows []rawArchiveRow) error { result = append(result, rows...); return nil })
	return result, err
}
