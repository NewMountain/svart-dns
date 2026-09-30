package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInvestigateResourceRejectsCompleteOversizedResults(t *testing.T) {
	setupInvestigateSandbox(t)
	for _, query := range []string{
		`SELECT repeat('x', 8388608) FROM query_logs LIMIT 1`,
		`SELECT repeat('<', 1048576) FROM query_logs LIMIT 1`,
		`SELECT repeat(query_name, 100000) FROM query_logs`,
	} {
		status, response, _ := postInvestigate(t, query)
		if status != http.StatusUnprocessableEntity {
			t.Errorf("status=%d want=422", status)
		}
		if response.Error == nil || *response.Error != "investigation resource limit exceeded; select fewer columns, aggregate, or page a smaller result" {
			t.Errorf("error=%v", response.Error)
		}
		if response.Data != nil {
			t.Fatal("rejection returned partial data")
		}
	}
	_, rows, _, err := investigateQuery(`SELECT count(*), sum(coalesced_count)::BIGINT, repeat('dns', 2) FROM query_logs`, 5)
	if err != nil || len(rows) != 1 || rows[0][0] != int64(5) || rows[0][1] != int64(12) || rows[0][2] != "dnsdns" {
		t.Fatalf("recovery rows=%v err=%v", rows, err)
	}
}

func TestInvestigateResourceHonorsCanceledRequest(t *testing.T) {
	setupInvestigateSandbox(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(`{"sql":"SELECT count(*) FROM query_logs","timeout":5}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	start := time.Now()
	handleAPIInvestigate(response, req)
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status=%d want=408", response.Code)
	}
	if time.Since(start) > time.Second {
		t.Fatal("canceled request waited over one second")
	}
}

func TestInvestigateResourceRowBoundaryAndCompleteScalar(t *testing.T) {
	setupInvestigateSandbox(t)
	_, rows, _, err := investigateQuery(`SELECT unnest(range(10000)) AS n FROM query_logs LIMIT 10000`, 5)
	if err != nil || len(rows) != 10000 || rows[0][0] != int64(0) || rows[9999][0] != int64(9999) {
		t.Fatalf("row boundary len=%d err=%v", len(rows), err)
	}
	code, response, _ := postInvestigate(t, `SELECT unnest(range(10001)) AS n FROM query_logs LIMIT 10001`)
	if code != 422 || response.Data != nil {
		t.Fatalf("oversized rows status=%d data=%v", code, response.Data)
	}
	_, rows, _, err = investigateQuery(`SELECT repeat('dns', 100000) FROM query_logs LIMIT 1`, 5)
	if err != nil || len(rows) != 1 || rows[0][0] != strings.Repeat("dns", 100000) {
		t.Fatalf("complete scalar not preserved: err=%v", err)
	}
}
