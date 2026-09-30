package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/parquet-go/parquet-go"
)

func independentArchiveFixture(t *testing.T) time.Time {
	t.Helper()
	t.Cleanup(setupTestDB(t))
	old := archivePath
	archivePath = t.TempDir()
	t.Cleanup(func() { archivePath = old })
	return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
}

func independentArchiveInsert(t *testing.T, stamp, name string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,response_code,coalesced_count) VALUES(?, '192.0.2.40', ?, 'AAAA', 'NOERROR', 7)`, stamp, name); err != nil {
		t.Fatal(err)
	}
}

func independentArchiveRows(t *testing.T, day time.Time) []QueryLogRow {
	t.Helper()
	rows, err := parquet.ReadFile[QueryLogRow](filepath.Join(archivePath, archiveDayFileName(day)))
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func independentHotCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIndependentSummaryArchiveRetriesPublishedDeletion(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "recovery.example.")
	if _, err := db.Exec(`CREATE TRIGGER reject_archive_delete BEFORE DELETE ON query_logs BEGIN SELECT RAISE(ABORT,'injected delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	n, err := archiveDay(day)
	if err == nil || !strings.Contains(err.Error(), "injected delete failure") || n != 1 {
		t.Fatalf("first archive=%d,%v; want published row plus injected delete error", n, err)
	}
	if got := independentHotCount(t); got != 1 {
		t.Fatalf("hot count=%d want1", got)
	}
	if len(independentArchiveRows(t, day)) != 1 {
		t.Fatal("published row missing")
	}
	if _, err := db.Exec("DROP TRIGGER reject_archive_delete"); err != nil {
		t.Fatal(err)
	}
	// No in-memory recovery state exists: retry is also the process-restart path.
	if _, err := archiveDay(day); err != nil {
		t.Fatal(err)
	}
	if err := initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	defer closeDuckDB()
	_, rows, _, err := investigateQuery("SELECT count(*) AS rows, sum(coalesced_count) AS events FROM query_logs", 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("combined Investigation result=%v; want rows=1 events=7", rows)
	if got := independentHotCount(t); got != 0 {
		t.Errorf("published recovery left %d hot rows plus 1 cold row; retry must converge to one owner", got)
	}
	if len(rows) != 1 || fmt.Sprint(rows[0][0]) != "1" || fmt.Sprint(rows[0][1]) != "7" {
		t.Errorf("combined Investigation result=%v; want [[1 7]]", rows)
	}
}

func TestIndependentSummaryArchiveRejectsInvalidTimestamp(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 99:99:99", "invalid-time.example.")
	n, err := archiveDay(day)
	hot := independentHotCount(t)
	if err == nil {
		t.Errorf("invalid timestamp archive returned success count=%d hot=%d cold=%+v; must fail and preserve source", n, hot, independentArchiveRows(t, day))
	}
	if hot != 1 {
		t.Errorf("invalid timestamp source removed; hot=%d want1", hot)
	}
}

var independentArchiveDriverID atomic.Uint64

// The reader's private view injects faults at the real SQLite row-read boundary.
// The writer still sees the unmodified persistent query_logs table.
func independentArchiveReader(t *testing.T, expression string, hook func(string) (string, error)) {
	t.Helper()
	name := fmt.Sprintf("independent-archive-%d", independentArchiveDriverID.Add(1))
	driver := &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		if hook != nil {
			return conn.RegisterFunc("review_query_name", hook, false)
		}
		return nil
	}}
	sql.Register(name, driver)
	reader, err := sql.Open(name, testDBPath(t)+"?_journal_mode=WAL&_busy_timeout=1000")
	if err != nil {
		t.Fatal(err)
	}
	reader.SetMaxOpenConns(1)
	rows, err := db.Query("PRAGMA table_info(query_logs)")
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for rows.Next() {
		var cid, notnull, pk int
		var col, kind string
		var def sql.NullString
		if err := rows.Scan(&cid, &col, &kind, &notnull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		if col == "query_name" {
			cols = append(cols, expression+" AS query_name")
		} else {
			cols = append(cols, col)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, rows)
	// #nosec G202 -- Fixture SQL identifiers come only from the local schema or fixed test-case table list; values are bound.
	if _, err := reader.Exec("CREATE TEMP VIEW query_logs AS SELECT " + strings.Join(cols, ",") + " FROM main.query_logs"); err != nil {
		t.Fatal(err)
	}
	old := readDB
	readDB = reader
	t.Cleanup(func() { readDB = old; checkTestClose(t, reader) })
}

func TestIndependentSummaryArchivePreservesLateReplay(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "first.example.")
	inserted := false
	independentArchiveReader(t, "review_query_name(query_name)", func(name string) (string, error) {
		if !inserted {
			inserted = true
			_, err := db.Exec(`INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,coalesced_count) VALUES('2026-09-01 13:00:00','192.0.2.40','late-replayed.example.','A',3)`)
			if err != nil {
				return "", err
			}
		}
		return name, nil
	})
	n, err := archiveDay(day)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("fixture did not insert late row during snapshot read")
	}
	cold := independentArchiveRows(t, day)
	hot := independentHotCount(t)
	t.Logf("archive returned=%d, hot=%d cold=%+v", n, hot, cold)
	if hot+len(cold) != 2 {
		t.Fatalf("late replay lost from presentation history: hot=%d cold=%d; want two exact original rows", hot, len(cold))
	}
}

func TestIndependentSummaryArchiveRejectsIterationError(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "read-error.example.")
	independentArchiveReader(t, "CASE WHEN query_name='read-error.example.' THEN abs(-9223372036854775808) ELSE query_name END", nil)
	n, err := archiveDay(day)
	hot := independentHotCount(t)
	if err == nil {
		t.Errorf("SQLite iteration overflow ignored: returned count=%d hot=%d; must report unavailable and retain row", n, hot)
	}
	if hot != 1 {
		t.Errorf("failed iteration deleted source: hot=%d want1", hot)
	}
	if _, statErr := os.Stat(filepath.Join(archivePath, archiveDayFileName(day))); statErr == nil {
		t.Error("failed iteration published incomplete archive")
	}
}

func TestIndependentReplayIgnoredMutationRollsBack(t *testing.T) {
	for _, table := range []string{"query_logs", "query_journal_progress", "query_loki_outbox"} {
		t.Run(table, func(t *testing.T) {
			lw, verify, cleanup := newTestLogWriter(t, true)
			defer cleanup()
			defer func() { checkTestClose(t, lw.db) }()
			defer func() { checkTestClose(t, lw.stmt) }()
			if table == "query_loki_outbox" {
				oldLogger, oldLines := logDNS, logQueryLines.Load()
				logDNS = slog.New(newLokiHandler(slog.NewJSONHandler(io.Discard, nil), nil))
				logQueryLines.Store(true)
				defer func() { logDNS = oldLogger; logQueryLines.Store(oldLines) }()
				if _, err := lw.db.Exec(queryLokiOutboxSchema); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := lw.db.Exec(queryJournalProgressSchema); err != nil {
				t.Fatal(err)
			}
			// #nosec G202 -- Fixture SQL identifiers come only from the local schema or fixed test-case table list; values are bound.
			if _, err := lw.db.Exec("CREATE TRIGGER ignore_review_write BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(IGNORE); END"); err != nil {
				t.Fatal(err)
			}
			e := makeEntry(909)
			e.ts = 1788264000123456789
			lw.spool.spill(&e)
			entries, next, err := lw.spool.readBatch(10)
			if err != nil {
				t.Fatal(err)
			}
			ok := lw.flushWithCursor(entries, next)
			var count, progress int
			if err := verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if err := verify.QueryRow("SELECT COUNT(*) FROM query_journal_progress").Scan(&progress); err != nil {
				t.Fatal(err)
			}
			raw, _, err := lw.spool.readBatch(10)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(raw, []queryLogEntry{e}) {
				t.Fatal("immutable original changed")
			}
			if ok || count != 0 || progress != 0 {
				t.Fatalf("ignored %s mutation acknowledged=%t presentation=%d cursorRows=%d; want failed atomic transaction and all zeros", table, ok, count, progress)
			}
		})
	}
}

func TestIndependentRawOriginalsSurviveCoalescingArchiveReopen(t *testing.T) {
	lw, verify, cleanup := newTestLogWriter(t, true)
	defer cleanup()
	defer func() { checkTestClose(t, lw.db) }()
	defer func() { checkTestClose(t, lw.stmt) }()
	if _, err := lw.db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT);INSERT INTO settings VALUES('log_retention_days','1095')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	expected := make([][]byte, 60)
	for i := range expected {
		record := spoolRecord{TS: now.UnixNano() + int64(i), ClientIP: "192.0.2.40", QueryName: "telemetry.example.", QueryType: "AAAA", ResponseCode: "NXDOMAIN", Upstream: "tls://dns.example:853", Result: "block", ResultReason: "policy_block", ResultTier: "client", ResultEntity: "media-player", ResultRule: "telemetry.example", ResultListName: "Privacy ☃", RangeResult: "block", RangeEntity: "192.0.2.0/24", RangeRule: "*.example", RangeListName: "Network", GroupResult: "allow", GroupEntity: "Living room", GroupRule: "telemetry.example", GroupListName: "Exceptions", IPResult: "block", IPEntity: "192.0.2.40", IPRule: "telemetry.example", IPListName: "Device", LatencyMicroseconds: int64(100 + i), CoalescedCount: 1, ResultListID: 11, RangeListID: 12, GroupListID: 13, IPListID: 14, Blocked: true, ResultIsPublished: true, RangeIsPublished: true, GroupIsPublished: true, IPIsPublished: true}
		if i%2 == 0 {
			record.QueryType = "A"
		}
		payload, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(bytes.TrimSuffix(payload, []byte("}")), []byte(fmt.Sprintf(",\"future_field\":\"retained-%d-☃\"}\n", i))...)
		expected[i] = payload
		if _, err := lw.spool.journal.append(payload); err != nil {
			t.Fatal(err)
		}
	}
	lw.replaySpool()
	var rows, events int
	if err := verify.QueryRow("SELECT COUNT(*),SUM(coalesced_count) FROM query_logs").Scan(&rows, &events); err != nil {
		t.Fatal(err)
	}
	if rows >= 60 || events != 60 {
		t.Fatalf("coalescing rows=%d events=%d; want fewer rows and exact60 events", rows, events)
	}
	root := t.TempDir()
	a, err := newRawArchiver(lw.spool.journal, lw.db, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, lw.spool.journal, 0)
	path := lw.spool.journal.path
	if err := lw.spool.journal.close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openDurableJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	lw.spool.journal = reopened
	recovered, err := newRawArchiver(reopened, lw.db, root)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	if err := recovered.verify(func(row rawArchiveRow) error {
		if row.Sequence != int64(seen+1) || row.Journal != reopened.identity || row.TimestampNS != now.UnixNano()+int64(seen) || !bytes.Equal(row.Payload, expected[seen]) {
			return fmt.Errorf("original identity/bytes changed at sequence%d", row.Sequence)
		}
		seen++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 60 {
		t.Fatalf("verified%d originals want60", seen)
	}
	t.Logf("coalesced presentation rows=%d, events=%d; reopened archive retains all60 exact original payloads, unknown fields, newline bytes, nanosecond timestamps, sequence and identity", rows, events)
}

func TestIndependentJournalRejectsConflictingRetryToken(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "retry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	original := &journalRequest{token: "same-stable-token", payload: []byte(`{"line":"first original"}`)}
	if err := j.writeBatch([]*journalRequest{original}); err != nil {
		t.Fatal(err)
	}
	retry := &journalRequest{token: original.token, payload: append([]byte(nil), original.payload...)}
	if err := j.writeBatch([]*journalRequest{retry}); err != nil {
		t.Fatal(err)
	}
	if retry.id != original.id {
		t.Fatal("identical retry changed identity")
	}
	conflict := &journalRequest{token: original.token, payload: []byte(`{"line":"different original"}`)}
	err = j.writeBatch([]*journalRequest{conflict})
	rows, readErr := j.batch(0, 10)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].payload, original.payload) {
		t.Fatal("retry mutated prior original")
	}
	if err == nil {
		t.Fatalf("conflicting token acknowledged id=%d while only original bytes survive; require explicit error, never false durable admission", conflict.id)
	}
}

func TestIndependentJournalIgnoredInsertAndFailedCommitDoNotAcknowledge(t *testing.T) {
	for _, fault := range []string{"ignored insert", "deferred constraint commit"} {
		t.Run(fault, func(t *testing.T) {
			j, err := openDurableJournal(filepath.Join(t.TempDir(), "mutation.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := j.close(); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
			}()
			j.db.SetMaxOpenConns(1)
			sqlFault := `CREATE TRIGGER ignored BEFORE INSERT ON journal_records BEGIN SELECT RAISE(IGNORE); END`
			if fault == "deferred constraint commit" {
				sqlFault = `PRAGMA foreign_keys=ON; CREATE TABLE required_parent(id INTEGER PRIMARY KEY); CREATE TABLE commit_check(id INTEGER REFERENCES required_parent(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER rejected_commit AFTER INSERT ON journal_records BEGIN INSERT INTO commit_check VALUES(999); END`
			}
			if _, err := j.db.Exec(sqlFault); err != nil {
				t.Fatal(err)
			}
			request := &journalRequest{token: "mutation-fixture", payload: []byte(`{"line":"must remain pending"}`)}
			if err := j.writeBatch([]*journalRequest{request}); err == nil {
				t.Fatal("uncommitted record acknowledged")
			}
			rows, err := j.batch(0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) != 0 {
				t.Fatalf("failed transaction retained%d partial rows", len(rows))
			}
			if j.committedRecords.Load() != 0 {
				t.Fatal("failed transaction counted as durable")
			}
		})
	}
}

func TestIndependentReplayFailedCommitRollsBack(t *testing.T) {
	lw, verify, cleanup := newTestLogWriter(t, true)
	defer cleanup()
	defer func() { checkTestClose(t, lw.db) }()
	defer func() { checkTestClose(t, lw.stmt) }()
	if _, err := lw.db.Exec(queryJournalProgressSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := lw.db.Exec(`PRAGMA foreign_keys=ON; CREATE TABLE required_parent(id INTEGER PRIMARY KEY); CREATE TABLE commit_check(id INTEGER REFERENCES required_parent(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER rejected_commit AFTER INSERT ON query_logs BEGIN INSERT INTO commit_check VALUES(999); END`); err != nil {
		t.Fatal(err)
	}
	e := makeEntry(818)
	e.ts = 1788264000123456789
	lw.spool.spill(&e)
	entries, next, err := lw.spool.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if lw.flushWithCursor(entries, next) {
		t.Fatal("failed commit acknowledged")
	}
	var rows, cursors int
	if err := verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := verify.QueryRow("SELECT COUNT(*) FROM query_journal_progress").Scan(&cursors); err != nil {
		t.Fatal(err)
	}
	if rows != 0 || cursors != 0 {
		t.Fatalf("failed commit left rows=%d cursors=%d", rows, cursors)
	}
	raw, _, err := lw.spool.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, []queryLogEntry{e}) {
		t.Fatal("failed commit changed raw original")
	}
}
