package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// realisticLogBatch is one flush worth of entries shaped like production
// traffic: mostly default-allow answers from an upstream or the cache, a
// fifth blocked by a range-level published list, a few custom-rule allows.
func realisticLogBatch(n int) []queryLogEntry {
	domains := []string{"www.google.com.", "i.ytimg.com.", "api.github.com.", "graph.facebook.com.",
		"ocsp.digicert.com.", "time.apple.com.", "connectivitycheck.gstatic.com.", "api.spotify.com."}
	blocked := []string{"ads.doubleclick.net.", "app-measurement.com.", "telemetry.tv.example.",
		"pagead2.googlesyndication.com.", "metrics.icloud.com."}
	batch := make([]queryLogEntry, n)
	for i := range batch {
		ip := fmt.Sprintf("192.168.1.%d", 10+i%40)
		e := queryLogEntry{clientIP: ip, queryType: "A", responseCode: "NOERROR", coalescedCount: 1,
			latencyMicroseconds: int64(300 + i%900)}
		switch {
		case i%5 == 0:
			e.queryName = blocked[i%len(blocked)]
			e.responseCode, e.blocked = "NXDOMAIN", true
			e.result, e.resultReason, e.resultTier, e.resultEntity = "block", "published_block", "range", "IoT VLAN"
			e.resultRule, e.resultListName, e.resultListID, e.resultIsPublished = e.queryName[:len(e.queryName)-1], "Hagezi Pro", 3, true
			e.rangeResult, e.rangeEntity, e.rangeRule, e.rangeListName, e.rangeListID, e.rangeIsPublished =
				"block", "IoT VLAN", e.resultRule, "Hagezi Pro", 3, true
		case i%37 == 0:
			e.queryName = "cdn.jsdelivr.net."
			e.upstream = "https://dns.quad9.net/dns-query"
			e.result, e.resultReason, e.resultTier, e.resultEntity, e.resultRule = "allow", "custom_allow", "ip", ip, "cdn.jsdelivr.net"
			e.ipResult, e.ipEntity, e.ipRule = "allow", ip, "cdn.jsdelivr.net"
		default:
			e.queryName = domains[i%len(domains)]
			e.upstream = "cache"
			if i%3 == 0 {
				e.upstream = "https://dns.mullvad.net/dns-query"
			}
			e.result, e.resultReason, e.resultTier = "allow", "default_allow", "default"
		}
		batch[i] = e
	}
	return batch
}

// BenchmarkLogWriterFlush measures committed query-log rows per second through
// the real flush path (SQLite insert, metrics, structured log lines).
func BenchmarkLogWriterFlush(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()
	if queryLogWriter == nil {
		b.Fatal("log writer not initialized")
	}
	batch := realisticLogBatch(logBatchSize)
	b.ResetTimer()
	for b.Loop() {
		if !queryLogWriter.flush(batch) {
			b.Fatal("flush failed")
		}
	}
	b.ReportMetric(float64(b.N*len(batch))/b.Elapsed().Seconds(), "rows/s")
}

// BenchmarkLogWriterFlushNoLowCardIndexes is the same flush without the three
// low-cardinality indexes, to price what they cost the write path.
func BenchmarkLogWriterFlushNoLowCardIndexes(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()
	for _, ix := range []string{"idx_query_logs_blocked", "idx_query_logs_result", "idx_query_logs_result_reason"} {
		if _, err := db.Exec("DROP INDEX IF EXISTS " + ix); err != nil {
			b.Fatal(err)
		}
	}
	batch := realisticLogBatch(logBatchSize)
	b.ResetTimer()
	for b.Loop() {
		if !queryLogWriter.flush(batch) {
			b.Fatal("flush failed")
		}
	}
	b.ReportMetric(float64(b.N*len(batch))/b.Elapsed().Seconds(), "rows/s")
}

// BenchmarkLogWriterFlushWithQueryLines prices the per-query structured log
// line emitted at LOG_LEVEL=info (the production default), written as JSON
// to a discarded stdout.
func BenchmarkLogWriterFlushWithQueryLines(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { checkTestClose(b, devnull) }()
	stdout := os.Stdout
	os.Stdout = devnull
	b.Setenv("LOG_LEVEL", "info")
	b.Setenv("LOG_FORMAT", "json")
	initLogging()
	defer func() {
		os.Stdout = stdout
		if err := os.Setenv("LOG_LEVEL", "error"); err != nil {
			b.Errorf("fixture operation failed: %v", err)
		}
		initLogging()
	}()
	batch := realisticLogBatch(logBatchSize)
	b.ResetTimer()
	for b.Loop() {
		if !queryLogWriter.flush(batch) {
			b.Fatal("flush failed")
		}
	}
	b.ReportMetric(float64(b.N*len(batch))/b.Elapsed().Seconds(), "rows/s")
}

// TestQueryLinesAreOptIn: by default a flushed batch reaches SQLite and
// metrics but not the structured log; LOG_QUERIES=true restores the lines.
func TestQueryLinesAreOptIn(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	var buf bytes.Buffer
	saved := logDNS
	logDNS = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { logDNS = saved }()
	defer logQueryLines.Store(logQueryLines.Load())

	batch := realisticLogBatch(20)
	logQueryLines.Store(false)
	if !queryLogWriter.flush(batch) {
		t.Fatal("flush failed")
	}
	if n := strings.Count(buf.String(), `"msg":"query"`); n != 0 {
		t.Fatalf("default flush wrote %d query log lines, want 0", n)
	}
	logQueryLines.Store(true)
	if !queryLogWriter.flush(batch) {
		t.Fatal("flush failed")
	}
	if n := strings.Count(buf.String(), `"msg":"query"`); n != len(batch) {
		t.Fatalf("LOG_QUERIES flush wrote %d query log lines, want %d", n, len(batch))
	}
}
