package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"unsafe"

	_ "github.com/mattn/go-sqlite3"
)

// newTestLogWriter creates a temporary DB with the query_logs table and
// initializes a logWriter against it. Returns the writer, the DB (for
// verification queries), and a cleanup function.
func newTestLogWriter(t *testing.T, pauseReplay ...bool) (*logWriter, *sql.DB, func()) {
	t.Helper()

	initLogging()
	previousAliases := clientAliasCache.Load()
	clientAliasCache.Store(map[string]string{
		"10.42.1.42": "Chris Macbook",
		"10.42.1.50": "Qwen AI Device",
	})

	tmpFile, err := os.CreateTemp("", "svart-logwriter-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp db: %v", err)
	}
	checkTestClose(t, tmpFile)

	dsn := tmpFile.Name() + "?_journal_mode=WAL&_synchronous=NORMAL"

	// Verification DB (for SELECT queries)
	verifyDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		if err := os.Remove(tmpFile.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		t.Fatalf("failed to open verify db: %v", err)
	}

	_, err = verifyDB.Exec(`CREATE TABLE IF NOT EXISTS query_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		client_ip TEXT NOT NULL,
		query_name TEXT NOT NULL,
		query_type TEXT NOT NULL,
		response_code TEXT,
		blocked BOOLEAN DEFAULT 0,
		upstream TEXT,
		latency_microseconds INTEGER,
		block_tier TEXT,
		block_rule TEXT,
		block_source TEXT,
		block_list_id INTEGER,
		block_list_name TEXT,
		result TEXT DEFAULT '',
		result_reason TEXT DEFAULT '',
		client_name TEXT DEFAULT '',
		policy_json TEXT DEFAULT '',
		result_tier TEXT DEFAULT '',
		result_entity TEXT DEFAULT '',
		result_is_published BOOLEAN DEFAULT 0,
		result_rule TEXT DEFAULT '',
		result_list_id INTEGER DEFAULT 0,
		result_list_name TEXT DEFAULT '',
		range_result TEXT DEFAULT '',
		range_entity TEXT DEFAULT '',
		range_is_published BOOLEAN DEFAULT 0,
		range_rule TEXT DEFAULT '',
		range_list_id INTEGER DEFAULT 0,
		range_list_name TEXT DEFAULT '',
		group_result TEXT DEFAULT '',
		group_entity TEXT DEFAULT '',
		group_is_published BOOLEAN DEFAULT 0,
		group_rule TEXT DEFAULT '',
		group_list_id INTEGER DEFAULT 0,
		group_list_name TEXT DEFAULT '',
		ip_result TEXT DEFAULT '',
		ip_entity TEXT DEFAULT '',
		ip_is_published BOOLEAN DEFAULT 0,
		ip_rule TEXT DEFAULT '',
		ip_list_id INTEGER DEFAULT 0,
		ip_list_name TEXT DEFAULT '',
		coalesced_count INTEGER DEFAULT 1
	)`)
	if err != nil {
		checkTestClose(t, verifyDB)
		if err := os.Remove(tmpFile.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		t.Fatalf("failed to create table: %v", err)
	}

	// Writer DB
	writerDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		checkTestClose(t, verifyDB)
		if err := os.Remove(tmpFile.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		t.Fatalf("failed to open writer db: %v", err)
	}
	writerDB.SetMaxOpenConns(1)

	stmt, err := writerDB.Prepare(queryLogInsertSQL)
	if err != nil {
		checkTestClose(t, writerDB)
		checkTestClose(t, verifyDB)
		if err := os.Remove(tmpFile.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		t.Fatalf("failed to prepare stmt: %v", err)
	}

	spool, err := openLogSpool(tmpFile.Name() + ".spool")
	if err != nil {
		t.Fatalf("open spool: %v", err)
	}
	var lw *logWriter
	if len(pauseReplay) > 0 && pauseReplay[0] {
		lw = &logWriter{ch: make(chan *queryLogEntry, logChannelSize), db: writerDB, stmt: stmt, done: make(chan struct{}), spool: spool}
	} else {
		lw = newLogWriter(writerDB, stmt, spool)
	}

	cleanup := func() {
		spool.close()
		if err := os.Remove(tmpFile.Name() + ".spool"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Remove(tmpFile.Name() + ".spool.offset"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		clientAliasCache.Store(previousAliases)
		checkTestClose(t, verifyDB)
		if err := os.Remove(tmpFile.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Remove(tmpFile.Name() + "-wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Remove(tmpFile.Name() + "-shm"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
	}

	return lw, verifyDB, cleanup
}

func makeEntry(i int) queryLogEntry {
	return queryLogEntry{
		clientIP:            "10.42.1.42",
		queryName:           fmt.Sprintf("example-%d.com.", i), // unique domain per entry to avoid doom loop dedup
		queryType:           "A",
		responseCode:        "NOERROR",
		blocked:             false,
		upstream:            "9.9.9.9:53",
		latencyMicroseconds: int64(i % 100),
		coalescedCount:      1,
		result:              "allow",
		resultReason:        "default_allow",
		resultTier:          "default",
	}
}

func TestLogWriterRichColumnsRoundTrip(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	entry := queryLogEntry{
		clientIP:            "10.42.1.42",
		queryName:           "ads.google.com.",
		queryType:           "A",
		responseCode:        "NXDOMAIN",
		blocked:             true,
		latencyMicroseconds: 5,
		coalescedCount:      1,
		result:              "block",
		resultReason:        "published_block",
		resultTier:          "group",
		resultEntity:        "Kids",
		resultIsPublished:   true,
		resultRule:          "ads.google.com",
		resultListID:        1,
		resultListName:      "Hagezi Pro",
		groupResult:         "block",
		groupEntity:         "Kids",
		groupIsPublished:    true,
		groupRule:           "ads.google.com",
		groupListID:         1,
		groupListName:       "Hagezi Pro",
	}
	lw.log(entry)
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	var result, resultReason, clientName, policyJSON, resultTier, resultEntity string
	var resultIsPublished bool
	var resultListID, coalescedCount int
	var groupResult, groupEntity string
	err := verifyDB.QueryRow(`SELECT result, result_reason, client_name, policy_json,
		result_tier, result_entity, result_is_published, result_list_id,
		group_result, group_entity, coalesced_count
		FROM query_logs WHERE query_name = 'ads.google.com.'`).Scan(
		&result, &resultReason, &clientName, &policyJSON,
		&resultTier, &resultEntity, &resultIsPublished, &resultListID,
		&groupResult, &groupEntity, &coalescedCount,
	)
	if err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if result != "block" {
		t.Errorf("expected result 'block', got %q", result)
	}
	if resultReason != "published_block" {
		t.Errorf("expected reason 'published_block', got %q", resultReason)
	}
	if clientName != "Chris Macbook" {
		t.Errorf("expected clientName 'Chris Macbook', got %q", clientName)
	}
	if !strings.Contains(policyJSON, `"result":"block"`) {
		t.Errorf("expected block policyJSON, got %q", policyJSON)
	}
	if resultTier != "group" {
		t.Errorf("expected tier 'group', got %q", resultTier)
	}
	if resultEntity != "Kids" {
		t.Errorf("expected entity 'Kids', got %q", resultEntity)
	}
	if !resultIsPublished {
		t.Error("expected resultIsPublished=true")
	}
	if resultListID != 1 {
		t.Errorf("expected listID 1, got %d", resultListID)
	}
	if groupResult != "block" {
		t.Errorf("expected groupResult 'block', got %q", groupResult)
	}
	if groupEntity != "Kids" {
		t.Errorf("expected groupEntity 'Kids', got %q", groupEntity)
	}
	if coalescedCount != 1 {
		t.Errorf("expected coalescedCount 1, got %d", coalescedCount)
	}
}

func TestLogWriterDefaultAllowSkipsPolicyJSON(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	lw.log(makeEntry(1))
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	var policyJSON string
	if err := verifyDB.QueryRow(`SELECT COALESCE(policy_json, '') FROM query_logs WHERE query_name = 'example-1.com.'`).Scan(&policyJSON); err != nil {
		t.Fatalf("failed to read policy_json: %v", err)
	}
	if policyJSON != "" {
		t.Fatalf("expected empty policy_json for default allow, got %q", policyJSON)
	}
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	return count
}

func sumCoalesced(t *testing.T, db *sql.DB) int {
	t.Helper()
	var sum int
	if err := db.QueryRow("SELECT COALESCE(SUM(coalesced_count), 0) FROM query_logs").Scan(&sum); err != nil {
		t.Fatalf("failed to sum coalesced_count: %v", err)
	}
	return sum
}

func TestLogWriterBasic(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	for i := 0; i < 100; i++ {
		lw.log(makeEntry(i))
	}

	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	got := countRows(t, verifyDB)
	if got != 100 {
		t.Errorf("expected 100 rows, got %d", got)
	}
}

func TestLogWriterBatchFlush(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	// Send exactly one full batch + 1 to trigger a batch flush mid-stream
	logTestBurst(lw, 5001, makeEntry)

	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	got := countRows(t, verifyDB)
	if got != 5001 {
		t.Errorf("expected 5001 rows, got %d", got)
	}
}

func TestLogWriterTimerFlush(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	for i := 0; i < 10; i++ {
		lw.log(makeEntry(i))
	}

	// Wait for the 100ms timer to fire (give it 300ms margin)
	waitLogCondition(t, func() bool { return countRows(t, verifyDB) == 10 })

	got := countRows(t, verifyDB)
	if got != 10 {
		t.Errorf("expected 10 rows after timer flush, got %d", got)
	}

	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)
}

func TestLogWriterGracefulShutdown(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	// Blast 50k entries as fast as possible (unique domains to avoid dedup)
	var admitted sync.WaitGroup
	for worker := 0; worker < 64; worker++ {
		admitted.Add(1)
		go func(worker int) {
			defer admitted.Done()
			for i := worker; i < 50000; i += 64 {
				lw.log(makeEntry(i))
			}
		}(worker)
	}
	admitted.Wait()

	// Close immediately — writer must drain and persist everything
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	got := countRows(t, verifyDB)
	if got != 50_000 {
		t.Errorf("expected 50000 rows after graceful shutdown, got %d", got)
	}
}

// TestLogWriterFlushRetriesOnTransientBusyAndDoesNotLoseRows simulates the A1
// production scenario: another connection holds the SQLite write lock (as the
// nightly archiver's VACUUM does, DD-013/DD-024) while the log writer tries to
// flush. Before the fix, flush() would log-and-return on the first error,
// silently discarding the whole batch. After the fix, the batch is retained
// and retried (never cleared until a commit actually succeeds), so every row
// is eventually persisted once the lock clears — house rule: never lose rows.
func TestLogWriterFlushRetriesOnTransientBusyAndDoesNotLoseRows(t *testing.T) {
	initLogging()

	tmpFile, err := os.CreateTemp("", "svart-logwriter-busy-test-*.db")
	if err != nil {
		t.Fatalf("failed to create temp db: %v", err)
	}
	tmpPath := tmpFile.Name()
	checkTestClose(t, tmpFile)
	defer func() {
		if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Remove(tmpPath + "-wal"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Remove(tmpPath + "-shm"); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()

	// Create the schema via a setup connection.
	setupDB, err := sql.Open("sqlite3", tmpPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("failed to open setup db: %v", err)
	}
	if _, err := setupDB.Exec(testQueryLogsSchema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	checkTestClose(t, setupDB)

	// Verification DB (separate connection, for read-back after the test).
	verifyDB, err := sql.Open("sqlite3", tmpPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("failed to open verify db: %v", err)
	}
	defer func() { checkTestClose(t, verifyDB) }()

	// Writer DB with a short busy_timeout so a held write lock produces a fast,
	// deterministic SQLITE_BUSY instead of the production 5000ms.
	writerDB, err := sql.Open("sqlite3", tmpPath+"?_journal_mode=WAL&_busy_timeout=150")
	if err != nil {
		t.Fatalf("failed to open writer db: %v", err)
	}
	writerDB.SetMaxOpenConns(1)

	stmt, err := writerDB.Prepare(queryLogInsertSQL)
	if err != nil {
		checkTestClose(t, writerDB)
		t.Fatalf("failed to prepare stmt: %v", err)
	}

	spool, err := openLogSpool(tmpPath + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	lw := newLogWriter(writerDB, stmt, spool)

	// Lock holder: a separate connection that grabs the SQLite write lock
	// (BEGIN IMMEDIATE, held via manual transaction control since MaxOpenConns(1)
	// pins it to a single underlying connection) and holds it long enough to
	// force at least one transient flush failure in the writer above.
	lockDB, err := sql.Open("sqlite3", tmpPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("failed to open lock db: %v", err)
	}
	lockDB.SetMaxOpenConns(1)
	if _, err := lockDB.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("failed to acquire write lock: %v", err)
	}

	const totalEntries = 20
	for i := 0; i < totalEntries; i++ {
		lw.log(makeEntry(i))
	}

	// Give the writer a few 100ms ticks to attempt (and fail) flushing while
	// the lock is held, proving the failure path is actually exercised.
	waitLogCondition(t, func() bool { return lw.flushFailed.Load() > 0 })

	if got := lw.flushFailed.Load(); got == 0 {
		t.Fatal("expected at least one transient flush failure to be recorded while the lock was held, got 0 — test isn't exercising the busy path")
	}
	if got := countRows(t, verifyDB); got != 0 {
		t.Fatalf("expected 0 rows committed while the lock was held, got %d", got)
	}

	// Release the lock — the writer's next retry should succeed.
	if _, err := lockDB.Exec("COMMIT"); err != nil {
		t.Fatalf("failed to release write lock: %v", err)
	}
	checkTestClose(t, lockDB)

	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	got := countRows(t, verifyDB)
	if got != totalEntries {
		t.Errorf("expected all %d rows to eventually be persisted despite the transient busy error, got %d — rows were lost", totalEntries, got)
	}
}

func TestLogWriterWithoutJournalRejectsAdmission(t *testing.T) {
	lw := &logWriter{ch: make(chan *queryLogEntry, logChannelSize)}
	defer func() {
		if got := recover(); got != "query log journal unavailable: event not admitted" {
			t.Fatalf("admission panic=%v", got)
		}
		if len(lw.ch) != 0 || lw.dropped.Load() != 0 {
			t.Fatal("unadmitted event entered a lossy queue")
		}
	}()
	lw.log(makeEntry(1))
}

// --- Doom loop deduplication tests ---

func makeBlockedEntry(clientIP, domain string) queryLogEntry {
	return queryLogEntry{
		ts:                  1720000000000000000,
		clientIP:            clientIP,
		queryName:           domain,
		queryType:           "A",
		responseCode:        "NXDOMAIN",
		blocked:             true,
		upstream:            "",
		latencyMicroseconds: 5,
		coalescedCount:      1,
		result:              "block",
		resultReason:        "published_block",
		resultTier:          "group",
		resultEntity:        "Kids",
		resultIsPublished:   true,
		resultRule:          domain,
		resultListID:        1,
		resultListName:      "Hagezi Pro",
	}
}

func makeAllowedEntry(clientIP, domain string) queryLogEntry {
	return queryLogEntry{
		ts:                  1720000000000000000,
		clientIP:            clientIP,
		queryName:           domain,
		queryType:           "A",
		responseCode:        "NOERROR",
		blocked:             false,
		upstream:            "9.9.9.9:53",
		latencyMicroseconds: 20,
		coalescedCount:      1,
		result:              "allow",
		resultReason:        "default_allow",
		resultTier:          "default",
	}
}

func TestQueryLogEntryHotStructSize(t *testing.T) {
	if got := unsafe.Sizeof(queryLogEntry{}); got > 448 {
		t.Fatalf("queryLogEntry grew too large for the hot queue: %d bytes", got)
	}
}

func TestDoomLoopDeduplication(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t, true)
	defer cleanup()

	// Send 100 blocked queries for the same client+domain rapidly.
	// Threshold for blocked is 25/sec. All sent in same goroutine time second,
	// so entries 1-25 are written individually, entries 26-100 are coalesced.
	logTestBurst(lw, 100, func(int) queryLogEntry { return makeBlockedEntry("10.42.1.50", "qwen-telemetry.alibaba.com.") })

	// Pin one presentation batch; disk latency must not change the traffic fixture.
	go lw.run()
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	rows := countRows(t, verifyDB)
	total := sumCoalesced(t, verifyDB)

	// Fewer rows than 100 were written (dedup happened)
	if rows >= 100 {
		t.Errorf("expected fewer than 100 rows due to doom loop dedup, got %d", rows)
	}
	// But SUM(coalesced_count) must account for all 100 queries
	if total != 100 {
		t.Errorf("expected SUM(coalesced_count) = 100, got %d", total)
	}
	// At most 25 individual rows (threshold) + 1 summary row
	if rows > 26 {
		t.Errorf("expected at most 26 rows (25 individual + 1 summary), got %d", rows)
	}

	t.Logf("doom loop dedup: %d rows written, SUM(coalesced_count) = %d (100 queries)", rows, total)
}

func TestAllowedTrafficNotCoalescedBelowThreshold(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t, true)
	defer cleanup()

	// Send 50 allowed queries — all under the 100/sec threshold
	logTestBurst(lw, 50, func(int) queryLogEntry { return makeAllowedEntry("10.42.1.42", "github.com.") })

	// Pin one presentation batch; disk latency must not change the traffic fixture.
	go lw.run()
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	rows := countRows(t, verifyDB)
	if rows != 50 {
		t.Errorf("expected exactly 50 rows (below allowed threshold), got %d", rows)
	}
}

func TestAllowedTrafficCoalescedAboveThreshold(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t, true)
	defer cleanup()

	// Send 200 allowed queries rapidly — threshold is 100/sec
	logTestBurst(lw, 200, func(int) queryLogEntry { return makeAllowedEntry("10.42.1.42", "chatgpt.com.") })

	// Pin one presentation batch; disk latency must not change the traffic fixture.
	go lw.run()
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	rows := countRows(t, verifyDB)
	total := sumCoalesced(t, verifyDB)

	if rows >= 200 {
		t.Errorf("expected fewer than 200 rows due to doom loop dedup, got %d", rows)
	}
	if total != 200 {
		t.Errorf("expected SUM(coalesced_count) = 200, got %d", total)
	}
	// At most 100 individual rows + 1 summary row
	if rows > 101 {
		t.Errorf("expected at most 101 rows (100 individual + 1 summary), got %d", rows)
	}

	t.Logf("allowed doom loop dedup: %d rows written, SUM(coalesced_count) = %d (200 queries)", rows, total)
}

func TestDoomLoopCoalescedCountColumn(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t)
	defer cleanup()

	// Send a single normal entry — should have coalesced_count = 1
	lw.log(makeBlockedEntry("10.42.1.77", "single-query.example.com."))

	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	var cc int
	err := verifyDB.QueryRow("SELECT coalesced_count FROM query_logs WHERE query_name = 'single-query.example.com.'").Scan(&cc)
	if err != nil {
		t.Fatalf("failed to read coalesced_count: %v", err)
	}
	if cc != 1 {
		t.Errorf("expected coalesced_count = 1 for normal query, got %d", cc)
	}
}

func TestDoomLoopSummaryRowHasCorrectCount(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t, true)
	defer cleanup()

	// Send 60 blocked queries (threshold=25) — 25 individual + 1 summary with count=35
	logTestBurst(lw, 60, func(int) queryLogEntry { return makeBlockedEntry("10.42.1.88", "summary-test.example.com.") })

	// Pin one presentation batch; disk latency must not change the traffic fixture.
	go lw.run()
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	// Find the summary row (coalesced_count > 1)
	var summaryCount int
	err := verifyDB.QueryRow("SELECT coalesced_count FROM query_logs WHERE query_name = 'summary-test.example.com.' AND coalesced_count > 1").Scan(&summaryCount)
	if err != nil {
		t.Fatalf("failed to find summary row: %v", err)
	}
	if summaryCount != 35 {
		t.Errorf("expected summary row coalesced_count = 35, got %d", summaryCount)
	}

	// Filter to just this domain
	var domainTotal int
	if err := verifyDB.QueryRow("SELECT COALESCE(SUM(coalesced_count), 0) FROM query_logs WHERE query_name = 'summary-test.example.com.'").Scan(&domainTotal); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if domainTotal != 60 {
		t.Errorf("expected SUM(coalesced_count) = 60 for domain, got %d", domainTotal)
	}

	t.Logf("summary row has coalesced_count = %d, total for domain = %d", summaryCount, domainTotal)
}

func TestDoomLoopDifferentClientsSameDomainsAreIndependent(t *testing.T) {
	lw, verifyDB, cleanup := newTestLogWriter(t, true)
	defer cleanup()

	// Two different clients querying the same blocked domain
	// Each sends 30 queries — should independently trigger at threshold 25
	for i := 0; i < 30; i++ {
		lw.log(makeBlockedEntry("10.42.1.10", "shared-blocked.example.com."))
		lw.log(makeBlockedEntry("10.42.1.20", "shared-blocked.example.com."))
	}

	// Pin one presentation batch; disk latency must not change the traffic fixture.
	go lw.run()
	close(lw.ch)
	<-lw.done
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)

	// Each client should have its dedup tracked independently
	var client1Rows, client2Rows int
	var client1Total, client2Total int
	if err := verifyDB.QueryRow("SELECT COUNT(*), COALESCE(SUM(coalesced_count), 0) FROM query_logs WHERE client_ip = '10.42.1.10'").Scan(&client1Rows, &client1Total); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := verifyDB.QueryRow("SELECT COUNT(*), COALESCE(SUM(coalesced_count), 0) FROM query_logs WHERE client_ip = '10.42.1.20'").Scan(&client2Rows, &client2Total); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if client1Total != 30 {
		t.Errorf("client1: expected SUM(coalesced_count) = 30, got %d", client1Total)
	}
	if client2Total != 30 {
		t.Errorf("client2: expected SUM(coalesced_count) = 30, got %d", client2Total)
	}
	// Both should have had some dedup
	if client1Rows >= 30 {
		t.Errorf("client1: expected fewer than 30 rows, got %d", client1Rows)
	}
	if client2Rows >= 30 {
		t.Errorf("client2: expected fewer than 30 rows, got %d", client2Rows)
	}

	t.Logf("client1: %d rows, total=%d; client2: %d rows, total=%d", client1Rows, client1Total, client2Rows, client2Total)
}

// testQueryLogsSchema is query_logs as a migrated production database has it.
const testQueryLogsSchema = `CREATE TABLE IF NOT EXISTS query_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
	client_ip TEXT NOT NULL,
	query_name TEXT NOT NULL,
	query_type TEXT NOT NULL,
	response_code TEXT,
	blocked BOOLEAN DEFAULT 0,
	upstream TEXT,
	latency_microseconds INTEGER,
	block_tier TEXT,
	block_rule TEXT,
	block_source TEXT,
	block_list_id INTEGER,
	block_list_name TEXT,
	result TEXT DEFAULT '',
	result_reason TEXT DEFAULT '',
	client_name TEXT DEFAULT '',
	policy_json TEXT DEFAULT '',
	result_tier TEXT DEFAULT '',
	result_entity TEXT DEFAULT '',
	result_is_published BOOLEAN DEFAULT 0,
	result_rule TEXT DEFAULT '',
	result_list_id INTEGER DEFAULT 0,
	result_list_name TEXT DEFAULT '',
	range_result TEXT DEFAULT '',
	range_entity TEXT DEFAULT '',
	range_is_published BOOLEAN DEFAULT 0,
	range_rule TEXT DEFAULT '',
	range_list_id INTEGER DEFAULT 0,
	range_list_name TEXT DEFAULT '',
	group_result TEXT DEFAULT '',
	group_entity TEXT DEFAULT '',
	group_is_published BOOLEAN DEFAULT 0,
	group_rule TEXT DEFAULT '',
	group_list_id INTEGER DEFAULT 0,
	group_list_name TEXT DEFAULT '',
	ip_result TEXT DEFAULT '',
	ip_entity TEXT DEFAULT '',
	ip_is_published BOOLEAN DEFAULT 0,
	ip_rule TEXT DEFAULT '',
	ip_list_id INTEGER DEFAULT 0,
	ip_list_name TEXT DEFAULT '',
	coalesced_count INTEGER DEFAULT 1
)`

// Concurrent callers represent an actual subsecond DNS burst even when each
// successful call must wait for durable storage.
func logTestBurst(lw *logWriter, count int, entry func(int) queryLogEntry) {
	var group sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < min(count, 64); worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			<-start
			for i := worker; i < count; i += 64 {
				lw.log(entry(i))
			}
		}(worker)
	}
	close(start)
	group.Wait()
}
