package svart

import (
	"database/sql"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

// Cleanup may follow an explicit close in a failure-path test. That already
// closed state is successful cleanup; unrelated close failures fail the test.
func checkTestClose(t testing.TB, resource io.Closer) {
	t.Helper()
	if err := resource.Close(); err != nil && !errors.Is(err, os.ErrClosed) && !errors.Is(err, net.ErrClosed) {
		t.Errorf("fixture close failed: %v", err)
	}
}

func checkTestRollback(t testing.TB, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Errorf("fixture rollback failed: %v", err)
	}
}

func requireFixtureType[T any](t testing.TB, value any) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("fixture type mismatch: got %T, want %T", value, typed)
	}
	return typed
}

// Oversized-response tests deliberately make the client close its socket while
// the fixture is still writing. Preserve all other transport failures as failures.
func checkRejectedResponseWrite(t testing.TB, err error) {
	t.Helper()
	if err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, syscall.EPIPE) && !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("oversized response fixture write failed: %v", err)
	}
}

// Small pure fixture builders cannot report through a testing.T. A failed input
// conversion must stop the fixture, rather than silently constructing zero data.
func mustFixture[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
