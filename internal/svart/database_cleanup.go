package svart

import (
	"database/sql"
	"errors"
	"log/slog"
)

// closeQueryRows releases read cursors. Query owners still inspect rows.Err()
// before returning data; an additional cleanup failure must remain observable.
func closeQueryRows(rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		slog.Error("database query cursor cleanup failed", "error", err)
	}
}

// rollbackTransaction also runs after successful commits. ErrTxDone is the
// expected terminal state; any other failure to release a transaction is logged.
func rollbackTransaction(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Error("database transaction rollback failed", "error", err)
	}
}
