//go:build linux

package svart

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"

	"github.com/mattn/go-sqlite3"
)

func TestCheckpointTemporaryDiskFailure(t *testing.T) {
	if os.Getenv("SVART_CHECKPOINT_DISK_FAILURE_CHILD") != "1" {
		// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
		cmd := exec.Command(os.Args[0], "-test.run=^TestCheckpointTemporaryDiskFailure$", "-test.v")
		cmd.Env = append(os.Environ(), "SVART_CHECKPOINT_DISK_FAILURE_CHILD=1")
		output, err := cmd.CombinedOutput()
		t.Logf("isolated SQLite temp-file failure:\n%s", output)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	if _, err := db.Exec(`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<199999) INSERT INTO blocked_domains(blocklist_id,domain) SELECT 900,printf('temporary-storage-rule-%07d.example',x) FROM n`); err != nil {
		t.Fatal(err)
	}
	readDB.SetMaxOpenConns(1)
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	limited.Cur = 4096
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		t.Fatal(err)
	}
	_, readErr := queryCheckpointPage(context.Background(), 901, "", 3, 0)
	// Restore before any further storage operations; the limit is confined to
	// this child and only applied after the real SQLite fixture is committed.
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	var sqliteErr sqlite3.Error
	if !errors.As(readErr, &sqliteErr) || (sqliteErr.Code != sqlite3.ErrIoErr && sqliteErr.Code != sqlite3.ErrFull) {
		t.Fatalf("expected real SQLite temp-file write failure, got %v", readErr)
	}
	t.Logf("temp-file write failed with SQLite code %d extended %d", sqliteErr.Code, sqliteErr.ExtendedCode)
	var scratch int
	if err := readDB.QueryRow(`SELECT count(*) FROM sqlite_temp_master WHERE name LIKE 'checkpoint_%'`).Scan(&scratch); err != nil {
		t.Fatal(err)
	}
	if scratch != 0 {
		t.Fatalf("failed read leaked %d scratch tables", scratch)
	}
	page, err := queryCheckpointPage(context.Background(), 901, "temporary-storage-rule", 3, 0)
	if err != nil || page.Total != 200000 || len(page.Domains) != 3 || page.Domains[0] != "temporary-storage-rule-0000000.example" {
		t.Fatalf("retry after disk recovery: page=%+v err=%v", page, err)
	}
}
