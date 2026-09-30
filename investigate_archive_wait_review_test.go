package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Hold a real durable publication boundary, rather than manually taking the
// mutex, and drive both HTTP handlers plus a disconnected HTTP client.
func TestReviewInvestigationCancellationDuringArchivePublication(t *testing.T) {
	day := independentArchiveFixture(t)
	independentArchiveInsert(t, "2026-09-01 12:00:00", "archive-wait.example.")
	if err := initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	defer closeDuckDB()
	published, release, archived := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	summaryArchiveCheckpoint = func(stage string) error {
		if stage == "published" {
			close(published)
			<-release
		}
		return nil
	}
	released := false
	defer func() {
		if !released {
			close(release)
			<-archived
		}
		summaryArchiveCheckpoint = nil
	}()
	go func() { _, err := archiveDay(day); archived <- err }()
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("publication boundary not reached")
	}
	if investigateMu.TryLock() {
		investigateMu.Unlock()
		t.Fatal("publication does not hold serialization")
	}
	for _, surface := range []string{"query", "schema", "disconnect"} {
		t.Run(surface, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := &observedSummaryContext{Context: ctx, observed: make(chan struct{})}
			done := make(chan struct{})
			var clientDone chan error
			rec := httptest.NewRecorder()
			if surface == "disconnect" {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestCtx := &observedSummaryContext{Context: r.Context(), observed: observed.observed}
					handleAPIInvestigate(w, r.WithContext(requestCtx))
					close(done)
				}))
				defer server.Close()
				req, err := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(`{"sql":"SELECT count(*),sum(coalesced_count) FROM query_logs"}`))
				if err != nil {
					t.Fatal(err)
				}
				clientDone = make(chan error, 1)
				go func() {
					response, err := server.Client().Do(req)
					if response != nil {
						checkTestClose(t, response.Body)
					}
					clientDone <- err
				}()
			} else {
				req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(`{"sql":"SELECT count(*),sum(coalesced_count) FROM query_logs"}`)).WithContext(observed)
				if surface == "schema" {
					req.Method = "GET"
				}
				go func() {
					if surface == "schema" {
						handleAPIInvestigateSchema(rec, req)
					} else {
						handleAPIInvestigate(rec, req)
					}
					close(done)
				}()
			}
			select {
			case <-observed.observed:
			case <-time.After(time.Second):
				t.Fatal("handler context not reached")
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("handler retained behind archive publication")
			}
			if clientDone != nil {
				if err := <-clientDone; !errors.Is(err, context.Canceled) {
					t.Fatalf("disconnect=%v", err)
				}
			} else {
				want := http.StatusRequestTimeout
				if surface == "schema" {
					want = http.StatusServiceUnavailable
				}
				if rec.Code != want {
					t.Fatalf("canceled request HTTP%d want%d: %s", rec.Code, want, rec.Body.String())
				}
			}
			if pids := investigationChildPIDs(t); len(pids) != 0 || investigateBusy.Load() {
				t.Fatalf("workers=%v busy=%v", pids, investigateBusy.Load())
			}
			if investigateMu.TryLock() {
				investigateMu.Unlock()
				t.Fatal("publication lock released prematurely")
			}
			t.Logf("%s canceled while durable archive publication remains held; no worker/admission", surface)
		})
	}
	close(release)
	released = true
	if err := <-archived; err != nil {
		t.Fatal(err)
	}
	summaryArchiveCheckpoint = nil
	_, rows, _, err := investigateQuery("SELECT count(*),sum(coalesced_count) FROM query_logs", 5)
	if err != nil || fmt.Sprint(rows) != "[[1 7]]" {
		t.Fatalf("completed publication result=%v error=%v", rows, err)
	}
	t.Logf("completed publication exact result=%v", rows)
}
