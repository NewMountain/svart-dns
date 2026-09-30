package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
)

// closeReadResource handles deferred release of read handles and statements.
// Write owners still explicitly check sync/close before publishing ownership.
// Cleanup may follow that explicit close; only the already-closed state is benign.
func closeReadResource(resource io.Closer) {
	if err := resource.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		slog.Error("resource cleanup failed", "error", err)
	}
}

// internalValue checks process-owned caches and pools. A mismatched value is an
// invariant violation, just as a failed direct type assertion was previously.
func internalValue[T any](value any) T {
	typed, ok := value.(T)
	if !ok {
		panic("internal cache value has an unexpected type")
	}
	return typed
}
