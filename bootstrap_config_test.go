package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/miekg/dns"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestBootstrapReplacementInvalidatesCachedAnswer(t *testing.T) {
	bootstrapHosts.Delete("changing.example")
	t.Cleanup(func() { bootstrapHosts.Delete("changing.example") })
	defer setupTestDB(t)()
	first := startUpstreamStub(t, "udp", answerA("192.0.2.10"))
	second := startUpstreamStub(t, "udp", answerA("192.0.2.20"))
	useBootstrap(t, first.addr)
	ip, err := bootstrapLookup(context.Background(), "changing.example.")
	if err != nil || ip != "192.0.2.10" {
		t.Fatalf("first lookup=%s %v", ip, err)
	}
	body, fixtureErr644 := json.Marshal(map[string][]string{"servers": {second.addr}})
	if fixtureErr644 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr644)
	}
	w := httptest.NewRecorder()
	handleAPIBootstrap(w, httptest.NewRequest("PUT", "/api/bootstrap", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("replace status=%d", w.Code)
	}
	ip, err = bootstrapLookup(context.Background(), "changing.example.")
	if err != nil || ip != "192.0.2.20" {
		t.Fatalf("new config lookup=%s %v, want 192.0.2.20", ip, err)
	}
}

func TestBootstrapReplacementRollsBackAllSQLAndTombstones(t *testing.T) {
	defer setupTestDB(t)()
	useBootstrap(t, "192.0.2.53:53")
	if _, err := db.Exec(`CREATE TRIGGER fail_bootstrap BEFORE INSERT ON bootstrap_servers WHEN NEW.server='192.0.2.54:53' BEGIN SELECT RAISE(ABORT, 'injected bootstrap failure'); END`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleAPIBootstrap(w, httptest.NewRequest("PUT", "/api/bootstrap", bytes.NewBufferString(`{"servers":["192.0.2.55:53","192.0.2.54:53"]}`)))
	if w.Code != 503 {
		t.Errorf("status=%d, want 503", w.Code)
	}
	if w.Body.String() != `{"data":null,"error":"Data unavailable; retry the request","error_code":"unavailable"}`+"\n" {
		t.Errorf("unsafe or incomplete error: %s", w.Body.String())
	}
	var server string
	if err := db.QueryRow("SELECT server FROM bootstrap_servers").Scan(&server); err != nil {
		t.Fatal(err)
	}
	if server != "192.0.2.53:53" {
		t.Errorf("server=%q, want old server", server)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name='bootstrap_servers'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("rollback tombstones=%d, want 0", count)
	}
}

func TestBootstrapConcurrentColdLookupCoalesces(t *testing.T) {
	bootstrapHosts.Delete("coalesced.example")
	t.Cleanup(func() { bootstrapHosts.Delete("coalesced.example") })
	defer setupTestDB(t)()
	started := make(chan struct{}, 32)
	release := make(chan struct{})
	bootstrap := startUpstreamStub(t, "udp", func(q *dns.Msg) *dns.Msg { started <- struct{}{}; <-release; return answerA("192.0.2.10")(q) })
	useBootstrap(t, bootstrap.addr)
	var wg sync.WaitGroup
	ready := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-ready
			ip, err := bootstrapLookup(context.Background(), "coalesced.example.")
			if err != nil || ip != "192.0.2.10" {
				t.Errorf("lookup=%s %v", ip, err)
			}
		}()
	}
	close(ready)
	<-started
	close(release)
	wg.Wait()
	if count := len(bootstrap.queries()); count != 1 {
		t.Fatalf("cold requests=%d, want 1", count)
	}
}

func BenchmarkBootstrapCacheHit(b *testing.B) {
	bootstrapHosts.Store("benchmark.example", bootstrapEntry{ip: "192.0.2.10", expiresAt: clock.Now().Add(time.Hour)})
	defer bootstrapHosts.Delete("benchmark.example")
	b.ReportAllocs()
	for b.Loop() {
		ip, err := bootstrapLookup(context.Background(), "benchmark.example")
		if err != nil || ip != "192.0.2.10" {
			b.Fatal(ip, err)
		}
	}
}

func TestBootstrapInvalidationDiscardsInFlightOldAnswer(t *testing.T) {
	defer setupTestDB(t)()
	bootstrapHosts.Delete("inflight.example")
	t.Cleanup(func() { bootstrapHosts.Delete("inflight.example") })
	started, release := make(chan struct{}), make(chan struct{})
	first := startUpstreamStub(t, "udp", func(q *dns.Msg) *dns.Msg { close(started); <-release; return answerA("192.0.2.10")(q) })
	second := startUpstreamStub(t, "udp", answerA("192.0.2.20"))
	useBootstrap(t, first.addr)
	done := make(chan string, 1)
	go func() {
		ip, err := bootstrapLookup(context.Background(), "inflight.example")
		if err != nil {
			done <- err.Error()
			return
		}
		done <- ip
	}()
	<-started
	body, fixtureErr4262 := json.Marshal(map[string][]string{"servers": {second.addr}})
	if fixtureErr4262 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr4262)
	}
	w := httptest.NewRecorder()
	handleAPIBootstrap(w, httptest.NewRequest("PUT", "/api/bootstrap", bytes.NewReader(body)))
	if w.Code != 200 {
		close(release)
		t.Fatalf("replace=%d", w.Code)
	}
	ip, err := bootstrapLookup(context.Background(), "inflight.example")
	if err != nil || ip != "192.0.2.20" {
		close(release)
		t.Fatalf("new lookup=%s %v", ip, err)
	}
	close(release)
	if got := <-done; got != "192.0.2.10" {
		t.Fatalf("old in-flight lookup=%q", got)
	}
	ip, err = bootstrapLookup(context.Background(), "inflight.example")
	if err != nil || ip != "192.0.2.20" {
		t.Fatalf("old request repopulated cache=%s %v", ip, err)
	}
}
