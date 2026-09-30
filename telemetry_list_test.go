package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestListRefreshTracesRealDownloadAndStorage(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() {
		otel.SetTracerProvider(old)
		fixtureErr597 := provider.Shutdown(context.Background())
		if fixtureErr597 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr597)
		}
	}()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintln(w, "0.0.0.0 ads.example.test"); err != nil {
			t.Errorf("list fixture response: %v", err)
		}
	}))
	defer server.Close()
	result, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES(?,?,?)", server.URL, "Telemetry fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err = refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	var domain string
	if err = db.QueryRow("SELECT domain FROM blocked_domains WHERE blocklist_id=?", id).Scan(&domain); err != nil {
		t.Fatal(err)
	}
	if domain != "ads.example.test" {
		t.Fatalf("domain=%q", domain)
	}
	spans := recorder.Ended()
	if len(spans) != 4 {
		t.Fatalf("spans=%d want 4 (storage read, outbound HTTP, storage write, refresh)", len(spans))
	}
	want := []string{"storage_read", "list_download HTTP", "storage_write", "list_download"}
	for i, span := range spans {
		if span.Name() != want[i] {
			t.Fatalf("span %d=%s want %s", i, span.Name(), want[i])
		}
		if i < 3 && span.Parent().SpanID() != spans[3].SpanContext().SpanID() {
			t.Fatalf("span %d has wrong parent", i)
		}
	}
}
