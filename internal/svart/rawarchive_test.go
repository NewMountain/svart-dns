package svart

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rawArchiveFixture(t *testing.T) (*rawArchiver, *durableJournal, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	j, err := openDurableJournal(filepath.Join(dir, "query.spool.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	source, err := sql.Open("sqlite3", filepath.Join(dir, "main.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { checkTestClose(t, source) })
	if _, err = source.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT); INSERT INTO settings VALUES('log_retention_days','1095'); CREATE TABLE query_journal_progress(source TEXT PRIMARY KEY,sequence INTEGER); CREATE TABLE query_loki_outbox(id TEXT PRIMARY KEY,payload BLOB)`); err != nil {
		t.Fatal(err)
	}
	a, err := newRawArchiver(j, source, filepath.Join(dir, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	return a, j, source
}
func rawArchiveAppend(t *testing.T, j *durableJournal, ts int64, extra string) []byte {
	t.Helper()
	p := []byte(fmt.Sprintf("{\"ts\":%d,\"query_name\":\"archive.example.\",\"query_type\":\"AAAA\",\"latency_us\":17,\"unknown\":%q}\n", ts, extra))
	if _, err := j.append(p); err != nil {
		t.Fatal(err)
	}
	return p
}
func rawArchiveProgress(t *testing.T, db *sql.DB, j *durableJournal, n int64) {
	t.Helper()
	if _, err := db.Exec("INSERT OR REPLACE INTO query_journal_progress VALUES(?,?)", j.identity, n); err != nil {
		t.Fatal(err)
	}
}
func rawArchiveCount(t *testing.T, j *durableJournal, want int) {
	t.Helper()
	var n int
	if err := j.db.QueryRow("SELECT COUNT(*) FROM journal_records").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Fatalf("raw journal rows=%d want%d", n, want)
	}
}
func TestRawArchiveExactOwnershipLateEventsAndLokiIndependence(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	p1 := rawArchiveAppend(t, j, now.UnixNano()-123, "original Unicode ☃")
	p2 := rawArchiveAppend(t, j, now.UnixNano()-456, "same-day late event")
	rawArchiveProgress(t, source, j, 1)
	if _, err := source.Exec("INSERT INTO query_loki_outbox VALUES('pending',?)", []byte("independent delivery")); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 1)
	rawArchiveProgress(t, source, j, 2)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	var got []rawArchiveRow
	if err := a.verify(func(r rawArchiveRow) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("archived=%d want2", len(got))
	}
	for i, p := range [][]byte{p1, p2} {
		if got[i].Sequence != int64(i+1) || got[i].Journal != j.identity || !bytes.Equal(got[i].Payload, p) {
			t.Fatalf("row %d differs: %+v", i, got[i])
		}
		var ts struct {
			TS int64 `json:"ts"`
		}
		if err := json.Unmarshal(p, &ts); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if got[i].TimestampNS != ts.TS {
			t.Fatalf("nanoseconds=%d want%d", got[i].TimestampNS, ts.TS)
		}
	}
	var outbox string
	if err := source.QueryRow("SELECT payload FROM query_loki_outbox WHERE id='pending'").Scan(&outbox); err != nil || outbox != "independent delivery" {
		t.Fatalf("outbox=%q err=%v", outbox, err)
	}
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
}
func TestRawArchiveFailuresKeepOriginals(t *testing.T) {
	for _, stage := range []string{"planned", "temp-synced", "published", "verified", "before-retire"} {
		t.Run(stage, func(t *testing.T) {
			a, j, source := rawArchiveFixture(t)
			now := time.Now().UTC()
			p := rawArchiveAppend(t, j, now.UnixNano(), "recoverable")
			rawArchiveProgress(t, source, j, 1)
			a.checkpoint = func(s string) error {
				if s == stage {
					return errors.New("injected archive failure")
				}
				return nil
			}
			if err := a.cycle(now); err == nil {
				t.Fatal("expected injected archive failure")
			}
			rawArchiveCount(t, j, 1)
			rows, err := j.batch(0, 2)
			if err != nil || !bytes.Equal(rows[0].payload, p) {
				t.Fatalf("original changed: %v", err)
			}
			a.checkpoint = nil
			if err := a.cycle(now); err != nil {
				t.Fatal(err)
			}
			rawArchiveCount(t, j, 0)
			n := 0
			if err := a.verify(func(r rawArchiveRow) error {
				n++
				if !bytes.Equal(r.Payload, p) {
					return errors.New("changed payload")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("archive records=%d want1", n)
			}
		})
	}
}
func TestRawArchiveInvalidRetentionAndReadFailurePreserveSources(t *testing.T) {
	for _, setting := range []string{"0", "-1", "3650001", "missing", "read-error"} {
		t.Run(setting, func(t *testing.T) {
			a, j, source := rawArchiveFixture(t)
			rawArchiveAppend(t, j, 1, "preserve")
			rawArchiveProgress(t, source, j, 1)
			switch setting {
			case "missing":
				if _, err := source.Exec("DELETE FROM settings"); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
			case "read-error":
				if _, err := source.Exec("DROP TABLE settings"); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
			default:
				if _, err := source.Exec("UPDATE settings SET value=?", setting); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
			}
			if err := a.cycle(time.Now()); err == nil {
				t.Fatal("expected unavailable retention")
			}
			rawArchiveCount(t, j, 1)
		})
	}
}
func TestRawArchiveRetentionExactNanosecondBoundary(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Date(2026, 9, 26, 12, 0, 0, 123, time.UTC)
	cutoff := now.AddDate(0, 0, -1)
	if _, err := source.Exec("UPDATE settings SET value='1'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	rawArchiveAppend(t, j, cutoff.UnixNano()-1, "expired")
	keep := rawArchiveAppend(t, j, cutoff.UnixNano(), "boundary retained")
	rawArchiveAppend(t, j, now.UnixNano(), "current")
	rawArchiveProgress(t, source, j, 3)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	var got []rawArchiveRow
	if err := a.verify(func(r rawArchiveRow) error { got = append(got, r); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Sequence != 2 || !bytes.Equal(got[0].Payload, keep) || got[1].Sequence != 3 {
		t.Fatalf("retained rows=%+v", got)
	}
}
func TestRawArchiveCorruptPublishedFileRetainsJournal(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "original")
	rawArchiveProgress(t, source, j, 1)
	a.checkpoint = func(stage string) error {
		if stage == "published" {
			paths, err := filepath.Glob(filepath.Join(a.dir, "*", "*", "*", "*.parquet"))
			if err != nil {
				return err
			}
			return os.WriteFile(paths[0], []byte("corrupt archive fixture"), 0600)
		}
		return nil
	}
	if err := a.cycle(now); err == nil {
		t.Fatal("expected verification failure")
	}
	rawArchiveCount(t, j, 1)
	a.checkpoint = nil
	if err := a.cycle(now); err == nil {
		t.Fatal("corrupt published archive must require recovery")
	}
	rawArchiveCount(t, j, 1)
}
