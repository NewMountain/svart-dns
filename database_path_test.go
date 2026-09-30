package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabasePoolsUseLiteralFilesystemNames(t *testing.T) {
	for _, name := range []string{"state#revision.sqlite", "state?literal.sqlite", "state%2Fencoded.sqlite"} {
		t.Run(name, func(t *testing.T) {
			defer setupTestDB(t)()
			if err := closeDatabase(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), name)
			if err := initDatabase(path); err != nil {
				t.Fatalf("initialize literal database filename: %v", err)
			}
			if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('literal-path-proof','same-file')"); err != nil {
				t.Fatal(err)
			}
			var value string
			if err := readDB.QueryRow("SELECT value FROM settings WHERE key='literal-path-proof'").Scan(&value); err != nil || value != "same-file" {
				t.Fatalf("read pool opened a different database: value=%q err=%v", value, err)
			}
			if _, err := queryLogWriter.db.Exec("INSERT INTO query_logs(client_ip,query_name,query_type,response_code) VALUES('192.0.2.10','literal.example.','A','NOERROR')"); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := readDB.QueryRow("SELECT COUNT(*) FROM query_logs WHERE query_name='literal.example.'").Scan(&count); err != nil || count != 1 {
				t.Fatalf("log writer opened a different database: count=%d err=%v", count, err)
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("literal database file absent: %v", err)
			}
			if err := queryLogWriter.spool.journal.db.QueryRow("SELECT COUNT(*) FROM journal_records").Scan(&count); err != nil {
				t.Fatalf("literal journal file unavailable: %v", err)
			}
		})
	}
}
