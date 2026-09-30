package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// spoolTestDB is a query_logs database plus a writer whose SQLite busy
// timeout is short, so tests can hold the write lock to simulate a stalled or
// unavailable database deterministically.
type spoolTestDB struct {
	path   string
	verify *sql.DB
}

func newSpoolTestDB(t *testing.T) *spoolTestDB {
	t.Helper()
	t.Setenv("LOG_LEVEL", "error")
	initLogging()
	path := filepath.Join(t.TempDir(), "svart.db")
	setup, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec(testQueryLogsSchema); err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, setup)
	verify, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { checkTestClose(t, verify) })
	return &spoolTestDB{path: path, verify: verify}
}

func (d *spoolTestDB) writer(t *testing.T, _ int) *logWriter {
	t.Helper()
	wdb, err := sql.Open("sqlite3", d.path+"?_journal_mode=WAL&_busy_timeout=50")
	if err != nil {
		t.Fatal(err)
	}
	wdb.SetMaxOpenConns(1)
	stmt, err := wdb.Prepare(queryLogInsertSQL)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := openLogSpool(d.path + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	return newLogWriter(wdb, stmt, spool)
}

func stopWriter(lw *logWriter) {
	close(lw.ch)
	<-lw.done
	lw.spool.close()
	if err := lw.stmt.Close(); err != nil {
		panic(err)
	}
	if err := lw.db.Close(); err != nil {
		panic(err)
	}
}

// holdWriteLock takes SQLite's write lock until the returned func is called.
func (d *spoolTestDB) holdWriteLock(t *testing.T) func() {
	t.Helper()
	lock, err := sql.Open("sqlite3", d.path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	lock.SetMaxOpenConns(1)
	if _, err := lock.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := lock.Exec("ROLLBACK"); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		checkTestClose(t, lock)
	}
}

func (d *spoolTestDB) waitRows(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		if err := d.verify.QueryRow("SELECT COALESCE(SUM(coalesced_count), 0) FROM query_logs").Scan(&got); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("query_logs holds %d queries, want %d", got, want)
}

// TestLogWriterKeepsQueryTime: a row's timestamp is when the query happened,
// not when a backlogged writer finally inserted it.
func TestLogWriterKeepsQueryTime(t *testing.T) {
	d := newSpoolTestDB(t)
	lw := d.writer(t, logChannelSize)
	release := d.holdWriteLock(t)
	before := time.Now().UTC()
	lw.log(makeEntry(1))
	waitLogCondition(t, func() bool { return lw.flushFailed.Load() > 0 })
	release()
	d.waitRows(t, 1)
	stopWriter(lw)
	var ts string
	if err := d.verify.QueryRow("SELECT timestamp || '' FROM query_logs").Scan(&ts); err != nil {
		t.Fatal(err)
	}
	got, err := time.Parse("2006-01-02 15:04:05.000", ts)
	if err != nil {
		t.Fatalf("timestamp %q not in YYYY-MM-DD HH:MM:SS.mmm UTC form: %v", ts, err)
	}
	if skew := got.Sub(before); skew < -time.Second || skew > time.Second {
		t.Fatalf("row timestamp %s is %s after the query was logged at %s; want within 1s",
			ts, skew, before.Format(time.RFC3339Nano))
	}
}

// TestLogWriterOverflowSpillsInsteadOfDropping: with SQLite stalled and the
// queue full, every entry still reaches query_logs once SQLite recovers.
func TestLogWriterOverflowSpillsInsteadOfDropping(t *testing.T) {
	d := newSpoolTestDB(t)
	lw := d.writer(t, 8)
	release := d.holdWriteLock(t)
	const n = 3000
	logTestBurst(lw, n, makeEntry)
	waitLogCondition(t, func() bool { return lw.flushFailed.Load() > 0 })
	release()
	d.waitRows(t, n)
	if got := lw.dropped.Load(); got != 0 {
		t.Fatalf("dropped %d entries, want 0", got)
	}
	if got := lw.spool.spilled.Load(); got == 0 {
		t.Fatalf("expected overflow to go through the spool, spilled=0")
	}
	stopWriter(lw)
	var distinct int
	if err := d.verify.QueryRow("SELECT COUNT(DISTINCT query_name) FROM query_logs").Scan(&distinct); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if distinct != n {
		t.Fatalf("%d distinct names persisted, want %d (duplicates or losses)", distinct, n)
	}
}

// TestLogWriterSpoolSurvivesRestart: rows that could not be written before
// shutdown are replayed by the next process, exactly once, with their times.
func TestLogWriterSpoolSurvivesRestart(t *testing.T) {
	d := newSpoolTestDB(t)
	lw := d.writer(t, logChannelSize)
	release := d.holdWriteLock(t)
	past := time.Now().Add(-90 * time.Minute).UnixNano()
	const n = 250
	for i := 0; i < n; i++ {
		e := makeEntry(i)
		e.ts = past + int64(i)*int64(time.Millisecond)
		lw.log(e)
	}
	stopWriter(lw) // SQLite still locked: shutdown must spool, not drop
	release()
	if info, err := os.Stat(d.path + ".spool.sqlite"); err != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty spool after failed shutdown flush (err=%v)", err)
	}

	lw2 := d.writer(t, logChannelSize)
	d.waitRows(t, n)
	stopWriter(lw2)

	var rows int
	var first string
	if err := d.verify.QueryRow("SELECT COUNT(*), MIN(timestamp) || '' FROM query_logs").Scan(&rows, &first); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if rows != n {
		t.Fatalf("%d rows after replay, want exactly %d", rows, n)
	}
	if want := queryLogTimestamp(past); first != want {
		t.Fatalf("first replayed timestamp %s, want original %s", first, want)
	}
	lw3 := d.writer(t, logChannelSize) // a third start must not replay again
	d.waitRows(t, n)
	stopWriter(lw3)
	if err := d.verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&rows); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if rows != n {
		t.Fatalf("%d rows after a second restart, want %d (spool replayed twice)", rows, n)
	}
}

// TestLogWriterReplayCoalescesFlood: a doom loop that overflowed the queue is
// still coalesced when replayed, with every query counted.
func TestLogWriterReplayCoalescesFlood(t *testing.T) {
	d := newSpoolTestDB(t)
	lw := d.writer(t, 4)
	release := d.holdWriteLock(t)
	start := time.Now().Truncate(time.Second).UnixNano()
	const n = 2000
	logTestBurst(lw, n, func(i int) queryLogEntry {
		e := makeBlockedEntry("192.168.1.77", "telemetry.tv.example.")
		e.ts = start + int64(i)*int64(time.Millisecond)/4
		return e
	})
	waitLogCondition(t, func() bool { return lw.flushFailed.Load() > 0 })
	release()
	d.waitRows(t, n)
	stopWriter(lw)
	var rows int
	if err := d.verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&rows); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if rows > doomLoopThresholdBlocked+50 {
		t.Fatalf("%d rows for a 2000-query doom loop; replay did not coalesce", rows)
	}
}

func TestDoomLoopTrackerForgetsQuietPairs(t *testing.T) {
	d := newDoomLoopTracker()
	d.advance(1000, func(queryLogEntry) {})
	for i := 0; i < 10000; i++ {
		d.observe(makeAllowedEntry("192.168.1.9", fmt.Sprintf("r%d.cdn.example.", i)))
	}
	for i := 0; i < doomLoopThresholdBlocked+5; i++ {
		d.observe(makeBlockedEntry("192.168.1.9", "ads.example."))
	}
	var summaries []queryLogEntry
	d.advance(1001, func(e queryLogEntry) { summaries = append(summaries, e) })
	if len(d.counts) != 0 {
		t.Fatalf("per-second counts kept %d pairs after the second ended", len(d.counts))
	}
	if len(summaries) != 1 || summaries[0].coalescedCount != 5 {
		t.Fatalf("summaries = %+v, want one row absorbing 5 queries", summaries)
	}
	d.advance(1002, func(queryLogEntry) {})
	if len(d.coalescing) != 0 {
		t.Fatalf("pair still coalescing after a quiet second")
	}
}
