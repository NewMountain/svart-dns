package svart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
)

// Count the complete HTTP response without retaining a second giant fixture in
// the verifier. The handler and JSON encoder still execute their real paths.
type independentCountingWriter struct {
	header http.Header
	status int
	bytes  int64
}

func (w *independentCountingWriter) Header() http.Header    { return w.header }
func (w *independentCountingWriter) WriteHeader(status int) { w.status = status }
func (w *independentCountingWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.bytes += int64(len(p))
	return len(p), nil
}

// An authorized investigation must not consume an unbounded result allocation.
// This bounded adversarial fixture uses an allowed scalar function and existing
// traffic. It does not require forbidden table functions or recursive SQL.
func TestIndependentInvestigationOversizedScalarIsRejected(t *testing.T) {
	setupInvestigateSandbox(t)
	var before, after syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &before); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/investigate", strings.NewReader(`{"sql":"SELECT repeat('x', 300000000) FROM query_logs LIMIT 1","timeout":5}`))
	w := &independentCountingWriter{header: make(http.Header)}
	handleAPIInvestigate(w, req)
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &after); err != nil {
		t.Fatal(err)
	}
	t.Logf("HTTP status=%d complete response bytes=%d peak RSS before=%d KiB after=%d KiB", w.status, w.bytes, before.Maxrss, after.Maxrss)
	if w.status == http.StatusOK {
		t.Fatalf("query returned HTTP 200 with %d response bytes despite configured 256 MB engine memory budget", w.bytes)
	}
	if w.bytes > 10000 {
		t.Fatalf("rejection returned an unexpectedly large %d-byte response", w.bytes)
	}
}
