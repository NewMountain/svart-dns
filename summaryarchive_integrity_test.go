package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func writeIntegrityFixture(path string, rows []QueryLogRow) (resultErr error) {
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	writer := parquet.NewGenericWriter[QueryLogRow](f, parquet.Compression(&parquet.Snappy), parquet.MaxRowsPerRowGroup(summaryRowGroupRows))
	n, err := writer.Write(rows)
	if err != nil {
		return err
	}
	if n != len(rows) {
		return io.ErrShortWrite
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return nil
}

func TestSummaryIntegrityRejectsSameSizeSameTimeReplacements(t *testing.T) {
	for _, mode := range []string{"in-place", "renamed"} {
		t.Run(mode, func(t *testing.T) {
			day := independentArchiveFixture(t)
			independentArchiveInsert(t, "2026-09-01 12:00:00", "first-original.example.")
			independentArchiveInsert(t, "2026-09-01 12:00:00", "second-original.example.")
			if _, err := archiveDay(day); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(archivePath, archiveDayFileName(day))
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			cold := independentArchiveRows(t, day)
			candidate := path + ".replacement"
			var replacement []byte
			// The serializer can change compression lengths. Select a genuinely equal-
			// length, valid replacement, rather than padding an invalid file fixture.
			for value := int64(1); value < 256; value++ {
				if value == 7 {
					continue
				}
				for i := range cold {
					cold[i].CoalescedCount = value
				}
				if err := writeIntegrityFixture(candidate, cold); err != nil {
					t.Fatal(err)
				}
				// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
				contents, err := os.ReadFile(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if len(contents) == len(original) {
					replacement = contents
					break
				}
			}
			if replacement == nil {
				t.Fatal("could not construct equal-size valid Parquet replacement")
			}
			if bytes.Equal(replacement, original) {
				t.Fatal("fixture bytes did not change")
			}
			if err := initDuckDB(testDBPath(t), archivePath); err != nil {
				t.Fatal(err)
			}
			defer closeDuckDB()
			_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 5)
			if err != nil || fmt.Sprint(rows) != "[[2 14]]" {
				t.Fatalf("warm query=%v,%v", rows, err)
			}
			if _, err := investigateSchema(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := getArchiveStatus(t.Context()); err != nil {
				t.Fatal(err)
			}
			if mode == "in-place" {
				// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
				if err := os.WriteFile(path, replacement, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(candidate, path); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				t.Fatal("fixture changed size or modification time")
			}
			if os.SameFile(before, after) != (mode == "in-place") {
				t.Fatal("fixture inode mutation differs from requested mode")
			}
			parsed, err := parquet.ReadFile[QueryLogRow](path)
			if err != nil {
				t.Fatal(err)
			}
			if len(parsed) != 2 || parsed[0].CoalescedCount+parsed[1].CoalescedCount == 14 {
				t.Fatal("replacement must be valid but have different event totals")
			}
			code, _, body := postInvestigate(t, "SELECT count(*),sum(coalesced_count) FROM query_logs")
			if code != http.StatusServiceUnavailable {
				t.Fatalf("query=%d %s", code, body)
			}
			if strings.Contains(body, path) || strings.Contains(body, "digest") {
				t.Fatalf("private integrity diagnostic leaked: %s", body)
			}
			for _, surface := range []struct {
				name    string
				handler http.HandlerFunc
			}{{"schema", handleAPIInvestigateSchema}, {"status", handleAPIArchiveStatus}} {
				rec := httptest.NewRecorder()
				surface.handler(rec, httptest.NewRequest(http.MethodGet, "/api/"+surface.name, nil))
				if rec.Code != http.StatusServiceUnavailable {
					t.Fatalf("%s=%d %s", surface.name, rec.Code, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), path) {
					t.Fatalf("%s leaked path", surface.name)
				}
			}
			// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			_, rows, _, err = investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 5)
			if err != nil || fmt.Sprint(rows) != "[[2 14]]" {
				t.Fatalf("restored=%v,%v", rows, err)
			}
			if _, err := investigateSchema(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := getArchiveStatus(t.Context()); err != nil {
				t.Fatal(err)
			}
			t.Logf("mode=%s equal_bytes=%d equal_mtime=true all_surfaces=503 restored=[[2 14]]", mode, len(original))
		})
	}
}

type integrityObservedReader struct {
	source         io.Reader
	reads, largest int
	bytes          int64
	afterRead      func()
}

func (r *integrityObservedReader) Read(p []byte) (int, error) {
	r.reads++
	if len(p) > r.largest {
		r.largest = len(p)
	}
	n, err := r.source.Read(p)
	r.bytes += int64(n)
	if r.afterRead != nil {
		r.afterRead()
	}
	return n, err
}

func TestSummaryIntegrityStreamingReadAndCancellation(t *testing.T) {
	payload := bytes.Repeat([]byte("complete-opaque-original\x00\n"), 20000)
	path := filepath.Join(t.TempDir(), "archive-content")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, f) }()
	observed := &integrityObservedReader{source: f}
	got, err := summaryContentDigest(context.Background(), observed)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256(payload)
	if got != hex.EncodeToString(expected[:]) || observed.bytes != int64(len(payload)) || observed.largest != summaryIntegrityBufferBytes {
		t.Fatalf("digest=%s bytes=%d largest=%d", got, observed.bytes, observed.largest)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed = &integrityObservedReader{source: f, afterRead: cancel}
	got, err = summaryContentDigest(ctx, observed)
	if !errors.Is(err, context.Canceled) || got != "" || observed.bytes != summaryIntegrityBufferBytes || observed.reads != 1 {
		t.Fatalf("cancel=%v digest=%s bytes=%d reads=%d", err, got, observed.bytes, observed.reads)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, retained) {
		t.Fatal("cancellation changed original")
	}
	t.Logf("complete_bytes=%d max_read_buffer=%d canceled_after_bytes=%d without_digest", len(payload), summaryIntegrityBufferBytes, observed.bytes)
}

func TestSummaryIntegrityParentRequestsHonorCancellation(t *testing.T) {
	independentArchiveFixture(t)
	if err := initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	defer closeDuckDB()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := getArchiveStatus(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("status=%v", err)
	}
	if _, err := investigateSchema(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("schema=%v", err)
	}
	stop := make(chan struct{})
	close(stop)
	if digest, err := summaryContentDigest(summaryReadContext{stop: stop}, strings.NewReader("original")); !errors.Is(err, context.Canceled) || digest != "" {
		t.Fatalf("archive stop=%v digest=%s", err, digest)
	}
}

func TestSummaryIntegrityMetadataPoolWaitCancellation(t *testing.T) {
	for _, surface := range []string{"status", "schema"} {
		t.Run(surface, func(t *testing.T) {
			independentArchiveFixture(t)
			if err := initDuckDB(testDBPath(t), archivePath); err != nil {
				t.Fatal(err)
			}
			defer closeDuckDB()
			db.SetMaxOpenConns(1)
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { checkTestRollback(t, tx) }()
			before := db.Stats().WaitCount
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				if surface == "status" {
					_, err := getArchiveStatus(ctx)
					result <- err
				} else {
					_, err := investigateSchema(ctx)
					result <- err
				}
			}()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for db.Stats().WaitCount == before {
				select {
				case err := <-result:
					t.Fatalf("request returned before held connection: %v", err)
				case <-ticker.C:
				case <-deadline.C:
					cancel()
					checkTestRollback(t, tx)
					<-result
					t.Fatal("request did not reach held connection")
				}
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("held connection cancellation: %v", err)
				}
			case <-deadline.C:
				checkTestRollback(t, tx)
				<-result
				t.Fatal("request ignored cancellation while connection remained held")
			}
		})
	}
}

func TestSummaryIntegrityReadCost(t *testing.T) {
	day := independentArchiveFixture(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestRollback(t, tx) }()
	statement, err := tx.Prepare("INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,policy_json,coalesced_count) VALUES('2026-09-01 12:00:00','192.0.2.40',?,'A',?,1)")
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic diverse input gives a useful compressed-file read volume.
	var random uint64 = 0x927abc2137
	const records = 8192
	for i := 0; i < records; i++ {
		var value [1024]byte
		for k := range value {
			random = random*6364136223846793005 + 1
			value[k] = "0123456789abcdef"[random>>60]
		}
		if _, err := statement.Exec(fmt.Sprintf("event-%d.example.", i), string(value[:])); err != nil {
			t.Fatal(err)
		}
	}
	if err := statement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n, err := archiveDay(day); err != nil || n != records {
		t.Fatalf("archive=%d,%v", n, err)
	}
	path := filepath.Join(archivePath, archiveDayFileName(day))
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, file) }()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	observed := &integrityObservedReader{source: file}
	begin := time.Now()
	if _, err := summaryContentDigest(context.Background(), observed); err != nil {
		t.Fatal(err)
	}
	hashElapsed := time.Since(begin)
	if observed.bytes != info.Size() {
		t.Fatalf("hash bytes=%d file=%d", observed.bytes, info.Size())
	}
	if err := initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	defer closeDuckDB()
	begin = time.Now()
	_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 10)
	queryElapsed := time.Since(begin)
	if err != nil || fmt.Sprint(rows) != fmt.Sprintf("[[%d %d]]", records, records) {
		t.Fatalf("query=%v,%v", rows, err)
	}
	begin = time.Now()
	if _, err := investigateSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	schemaElapsed := time.Since(begin)
	begin = time.Now()
	if _, err := getArchiveStatus(t.Context()); err != nil {
		t.Fatal(err)
	}
	statusElapsed := time.Since(begin)
	t.Logf("warm-local fixture rows=%d files=1 file_bytes=%d actual_hash_reads=%d actual_hash_bytes=%d max_read_buffer=%d hash=%v query=%v schema=%v status=%v; configured integrity passes query=2 schema=2 status=1 (excludes native query I/O)", records, info.Size(), observed.reads, observed.bytes, observed.largest, hashElapsed, queryElapsed, schemaElapsed, statusElapsed)
}
