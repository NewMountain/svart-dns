//go:build linux

package svart

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestFinalInvestigationCompleteWireTypes(t *testing.T) {
	setupInvestigateSandbox(t)
	query := `SELECT 18446744073709551615::UBIGINT AS unsigned, 123456789012345678901234567890::HUGEINT AS huge, 12.34::DECIMAL(10,2) AS amount, DATE '2025-03-14' AS day, TIMESTAMP '2025-03-14 22:05:00' AS moment, [1::BIGINT, NULL, 3::BIGINT] AS numbers, {'a': 'dns', 'b': 2::BIGINT} AS record FROM query_logs LIMIT 1`
	cols, rows, _, err := investigateQuery(query, 5)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	status, _, raw := postInvestigate(t, query)
	var wire struct {
		Data struct {
			Columns  []string        `json:"columns"`
			Rows     json.RawMessage `json:"rows"`
			RowCount int             `json:"row_count"`
			Duration int64           `json:"duration_ms"`
		} `json:"data"`
		Error *string `json:"error"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		t.Fatal(err)
	}
	if status != 200 || wire.Error != nil || !reflect.DeepEqual(cols, wire.Data.Columns) || string(wire.Data.Rows) != string(expected) || wire.Data.RowCount != 1 || wire.Data.Duration < 0 {
		t.Fatalf("whole wire mismatch HTTP%d %s expected rows=%s columns=%v", status, raw, expected, cols)
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		t.Fatalf("trailing wire content: %v", err)
	}
	t.Logf("complete typed HTTP response=%s", raw)
}

func TestFinalInvestigationCorruptLegacyDiagnosticsPrivate(t *testing.T) {
	dir := setupInvestigateSandbox(t)
	marker := "final-private-customer-volume-9b61.parquet"
	path := filepath.Join(dir, marker)
	original := []byte("opaque-original-not-parquet\x00private-customer-9b61\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	status, response, body := postInvestigate(t, `SELECT count(*) FROM query_logs`)
	if status != 500 || response.Data != nil || response.Error == nil || *response.Error != "investigation data unavailable; check server storage and retry" {
		t.Fatalf("query HTTP%d %s", status, body)
	}
	rec := httptest.NewRecorder()
	handleAPIInvestigateSchema(rec, httptest.NewRequest("GET", "/api/investigate/schema", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), marker) || strings.Contains(body, dir) || strings.Contains(rec.Body.String(), dir) {
		t.Fatalf("unsafe diagnostics query=%s schema=%d %s", body, rec.Code, rec.Body.String())
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	retained, err := os.ReadFile(path)
	if err != nil || string(retained) != string(original) {
		t.Fatalf("original changed: %v", err)
	}
	t.Logf("query HTTP%d %s schema HTTP%d %s", status, body, rec.Code, rec.Body.String())
}

// Parquet permits unreferenced bytes before its footer. Padding preserves every
// original byte and every row-group offset, while making the actual digest phase
// observable without replacing production file I/O with a mock or test hook.
func finalDigestArchive(t *testing.T) (string, int64) {
	t.Helper()
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "digest-original.example.")
	if n, err := archiveDay(day); err != nil || n != 1 {
		t.Fatalf("archive=%d,%v", n, err)
	}
	path := filepath.Join(archivePath, archiveDayFileName(day))
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	footerStart := len(original) - 8 - int(binary.LittleEndian.Uint32(original[len(original)-8:len(original)-4]))
	replacement := path + ".padded"
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.Create(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(original[:footerStart]); err != nil {
		t.Fatal(err)
	}
	const padding = 512 << 20
	if _, err = f.Seek(padding, io.SeekCurrent); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(original[footerStart:]); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	parsed, err := parquet.ReadFile[QueryLogRow](path)
	if err != nil || len(parsed) != 1 || parsed[0].CoalescedCount != 7 {
		t.Fatalf("padded valid parquet=%v,%v", parsed, err)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := summaryContentDigest(context.Background(), file)
	checkTestClose(t, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE summary_archive_files SET digest=? WHERE name=?", digest, filepath.Base(path)); err != nil {
		t.Fatal(err)
	}
	if err = initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDuckDB)
	return path, int64(len(original)) + padding
}

func finalDigestReadPosition(t *testing.T, pid, path string) int64 {
	t.Helper()
	files, err := filepath.Glob("/proc/" + pid + "/fd/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range files {
		target, err := os.Readlink(fd)
		if err != nil || target != path {
			continue
		}
		// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		info, err := os.ReadFile("/proc/" + pid + "/fdinfo/" + filepath.Base(fd))
		if err != nil {
			continue
		}
		var pos int64
		if _, err = fmt.Sscanf(string(info), "pos:\t%d", &pos); err == nil {
			return pos
		}
	}
	return 0
}

func TestFinalInvestigationActiveDigestCancellation(t *testing.T) {
	path, size := finalDigestArchive(t)
	for _, surface := range []string{"query", "disconnect", "schema", "status"} {
		t.Run(surface, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			clientDone := make(chan error, 1)
			if surface == "disconnect" {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handleAPIInvestigate(w, r); done <- nil }))
				defer srv.Close()
				req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, strings.NewReader(`{"sql":"SELECT count(*),sum(coalesced_count) FROM query_logs","timeout":30}`))
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					resp, err := srv.Client().Do(req)
					if resp != nil {
						checkTestClose(t, resp.Body)
					}
					clientDone <- err
				}()
			} else {
				go func() {
					var err error
					switch surface {
					case "query":
						_, _, _, err = investigateQueryContext(ctx, "SELECT count(*),sum(coalesced_count) FROM query_logs", 30)
					case "schema":
						_, err = investigateSchema(ctx)
					case "status":
						_, err = getArchiveStatus(ctx)
					}
					done <- err
				}()
			}
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			var observed int64
			for observed == 0 {
				select {
				case err := <-done:
					t.Fatalf("request returned before observing digest read: %v", err)
				case <-timer.C:
					t.Fatal("did not observe active digest file read")
				case <-ticker.C:
					pids := []string{"self"}
					if surface == "query" || surface == "disconnect" || surface == "schema" {
						pids = investigationChildPIDs(t)
					}
					for _, pid := range pids {
						if pos := finalDigestReadPosition(t, pid, path); pos >= 4<<20 && pos < size/2 {
							observed = pos
							break
						}
					}
				}
			}
			begin := time.Now()
			cancel()
			select {
			case err := <-done:
				if surface != "disconnect" && !errors.Is(err, context.Canceled) {
					t.Fatalf("cancel error=%v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("digest cancellation exceeded 1s")
			}
			if surface == "disconnect" {
				if err := <-clientDone; !errors.Is(err, context.Canceled) {
					t.Fatalf("HTTP disconnect=%v", err)
				}
			}
			if pids := investigationChildPIDs(t); len(pids) != 0 || investigateBusy.Load() {
				t.Fatalf("retained workers=%v admission=%v", pids, investigateBusy.Load())
			}
			t.Logf("surface=%s active_digest_position=%d file_bytes=%d cancellation=%v live_children=0", surface, observed, size, time.Since(begin))
		})
	}
	_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 10)
	if err != nil || fmt.Sprint(rows) != "[[1 7]]" {
		t.Fatalf("digest recovery=%v,%v", rows, err)
	}
	t.Logf("complete padded-Parquet recovery=%v file_bytes=%d", rows, size)
}
