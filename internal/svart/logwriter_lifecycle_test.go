package svart

import (
	"testing"
)

func TestLogWriterNestedDatabaseSetupDrainsPreviousOwner(t *testing.T) {
	outerCleanup := setupTestDB(t)
	defer outerCleanup()
	outer := queryLogWriter
	outer.log(queryLogEntry{ts: 1790424000123456789, clientIP: "192.0.2.10", queryName: "lifecycle.example.", queryType: "AAAA", responseCode: "NOERROR", latencyMicroseconds: 17})
	innerCleanup := setupTestDB(t)
	defer innerCleanup()
	select {
	case <-outer.done:
	default:
		// Keep the failing regression from leaking the very worker it detects.
		inner := queryLogWriter
		queryLogWriter = outer
		closeLogWriter()
		queryLogWriter = inner
		t.Fatal("previous database writer is still running after replacement")
	}
	reopened, err := openDurableJournal(outer.spool.journal.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	rows, err := reopened.batch(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("retained raw records=%d want1", len(rows))
	}
	if string(rows[0].payload) != `{"ts":1790424000123456789,"client_ip":"192.0.2.10","query_name":"lifecycle.example.","query_type":"AAAA","response_code":"NOERROR","latency_us":17,"coalesced_count":0}` {
		t.Fatalf("unexpected retained raw event: %s", rows[0].payload)
	}
}
