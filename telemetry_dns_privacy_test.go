package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestDNSAnomalyLogsHonorQueryPrivacyAndRetainRows(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	previousDNS, previousLW := logDNS, logLW
	previousFlag := logQueryLines.Load()
	defer func() { logDNS, logLW = previousDNS, previousLW; logQueryLines.Store(previousFlag) }()
	for _, enabled := range []bool{false, true} {
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		logDNS, logLW = logger, logger
		logQueryLines.Store(enabled)
		e := makeEntry(0)
		e.clientIP = "192.0.2.42"
		e.queryName = "private.example.test"
		e.coalescedCount = doomLoopThresholdAllowed + 1
		tracker := newDoomLoopTracker()
		for i := 0; i <= doomLoopThresholdAllowed; i++ {
			tracker.observe(e)
		}
		if !queryLogWriter.flush([]queryLogEntry{e}) {
			t.Fatal("flush failed")
		}
		if strings.Count(output.String(), `"level":"WARN"`) != 2 {
			t.Fatalf("expected both aggregate anomaly warnings: %s", output.String())
		}
		for _, private := range []string{"192.0.2.42", "private.example.test"} {
			if strings.Contains(output.String(), private) != enabled {
				t.Fatalf("LOG_QUERIES=%t private value disclosed=%t", enabled, strings.Contains(output.String(), private))
			}
		}
		if !strings.Contains(output.String(), `"coalesced_count":101`) {
			t.Fatalf("aggregate count missing: %s", output.String())
		}
	}
	var rows int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs WHERE query_name='private.example.test' AND client_ip='192.0.2.42' AND coalesced_count=101").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("private persisted rows=%d want 2", rows)
	}
}
