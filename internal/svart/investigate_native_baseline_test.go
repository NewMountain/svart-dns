package svart

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// The direct native test oracle reads a consistent disposable snapshot, never
// the WAL/SHM owned by the fixture's active Go SQLite writer. VACUUM INTO copies
// the fixture to disk without opening a second Go connection to the snapshot.
// investigateViewSQL's catalog precheck closes before the native scan begins.
func prepareInvestigateView(ctx context.Context, t *testing.T, conn *sql.Conn) error {
	t.Helper()
	snapshot := filepath.Join(t.TempDir(), "baseline.sqlite")
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("snapshot native test baseline: %w", err)
	}
	viewSQL, err := investigateViewSQL(ctx, snapshot, duckDBArchive)
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, viewSQL); err != nil {
		return fmt.Errorf("create unified view: %w", err)
	}
	return nil
}
