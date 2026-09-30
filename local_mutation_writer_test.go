package main

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/yeti/svart-dns/internal/policycore"
)

func TestManualMutationReservesWriterBeforeReadingSnapshot(t *testing.T) {
	defer setupTestDB(t)()
	const client = "192.0.2.10"
	const domain = "manual-backup.example"
	if err := mutateManualRule(blockListStore, manualClient, client, domain, false); err != nil {
		t.Fatal(err)
	}
	var listID int64
	if err := db.QueryRow("SELECT blocklist_id FROM client_blocklists WHERE client_ip=?", client).Scan(&listID); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var name, path string
	if err := db.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Error(err)
		}
	})
	var competitorErr error
	err = localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		// DELETE first reads assigned lists and counts before writing its tombstone.
		// A separate logging connection must not commit between that snapshot and
		// its first write, which SQLite cannot upgrade even with busy_timeout.
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id=?", listID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("manual rules=%d", count)
		}
		_, competitorErr = writer.Exec("INSERT INTO query_logs(client_ip,query_name,query_type,response_code) VALUES(?,?,?,?)", client, "other.example.", "A", "NOERROR")
		return writeManualRule(tx, blockListStore, listID, domain, true)
	}, policycore.ListKey{ID: int(listID)})
	if err != nil {
		t.Fatalf("manual delete failed during independent query-log write: %v", err)
	}
	var busy sqlite3.Error
	if !errors.As(competitorErr, &busy) || busy.Code != sqlite3.ErrBusy {
		t.Fatalf("independent writer entered reserved snapshot: %v", competitorErr)
	}
	domains, err := readManualDomains(db, blockListStore, manualClient, client)
	if err != nil || len(domains) != 0 {
		t.Fatalf("deleted rules=%v err=%v", domains, err)
	}
	if evaluatePolicy(client, domain, 1).Result == "block" {
		t.Fatal("deleted manual rule remains published")
	}
	if _, err := writer.Exec("INSERT INTO query_logs(client_ip,query_name,query_type,response_code) VALUES(?,?,?,?)", client, "after.example.", "A", "NOERROR"); err != nil {
		t.Fatalf("writer reservation leaked after commit: %v", err)
	}
	var logged int
	if err := writer.QueryRow("SELECT COUNT(*) FROM query_logs WHERE query_name='after.example.'").Scan(&logged); err != nil || logged != 1 {
		t.Fatalf("post-mutation durable query logs=%d err=%v", logged, err)
	}
}
