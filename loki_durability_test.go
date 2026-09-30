package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLokiHandlerPreservesNestedFields(t *testing.T) {
	var output bytes.Buffer
	journal, err := openDurableJournal(filepath.Join(t.TempDir(), "loki.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	sender := &lokiSender{journal: journal, wake: make(chan struct{}, 1)}
	h := newLokiHandler(slog.NewJSONHandler(&output, nil), sender)
	nested := h.WithAttrs([]slog.Attr{slog.String("service", "svart")}).WithGroup("request").WithAttrs([]slog.Attr{slog.String("id", "req-17")})
	r := slog.NewRecord(time.Unix(1720000000, 0), slog.LevelInfo, "accepted", 0)
	if err := nested.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	rows, err := journal.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("journal records=%d, want 1", len(rows))
	}
	var record lokiRecord
	if err = json.Unmarshal(rows[0].payload, &record); err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err = json.Unmarshal([]byte(record.Line), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["service"]) != `"svart"` || string(got["request"]) != `{"id":"req-17"}` {
		t.Fatalf("nested log = %s; want service svart and request.id req-17", record.Line)
	}
}

func TestLokiReceiverFailureSaturationRestart(t *testing.T) {
	var mu sync.Mutex
	received := make(map[string]int)
	var healthy atomic.Bool
	attempts := make(chan struct{}, 10000)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			select {
			case attempts <- struct{}{}:
			default:
			}
			return
		}
		var body struct {
			Streams []struct {
				Values [][2]string `json:"values"`
			} `json:"streams"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("receiver decode: %v", err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		for _, stream := range body.Streams {
			for _, v := range stream.Values {
				received[v[1]]++
			}
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	path := filepath.Join(t.TempDir(), "loki.sqlite")
	sender, err := newLokiSender(path, receiver.URL, "fixture-node", receiver.Client())
	if err != nil {
		t.Fatal(err)
	}
	// The receiver backlog exceeds a batch and the in-memory admission queue;
	// every accepted event must reside on disk throughout the outage.
	handler := newLokiHandler(slog.NewJSONHandler(io.Discard, nil), sender)
	expected := make(map[string]bool)
	var group sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for i := worker; i < 5200; i += 16 {
				record := slog.NewRecord(time.Unix(1720000000, int64(i)), slog.LevelInfo, fmt.Sprintf("event-%04d", i), 0)
				if err := handler.Handle(context.Background(), record); err != nil {
					t.Errorf("Handle: %v", err)
				}
			}
		}(worker)
	}
	group.Wait()
	<-attempts
	rows, err := sender.journal.batch(0, 6000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5200 {
		t.Fatalf("failed receiver retained %d, want 5200", len(rows))
	}
	for _, row := range rows {
		var record lokiRecord
		if err = json.Unmarshal(row.payload, &record); err != nil {
			t.Fatal(err)
		}
		expected[record.Line] = true
	}
	if len(expected) != 5200 {
		t.Fatalf("distinct records=%d, want 5200", len(expected))
	}
	if err = sender.shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	healthy.Store(true)
	sender, err = newLokiSender(path, receiver.URL, "fixture-node", receiver.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sender.shutdown(context.Background()); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	sender.wake <- struct{}{}
	waitLogCondition(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(received) == 5200 })
	waitLogCondition(t, func() bool { rows, err := sender.pending(1); return err == nil && len(rows) == 0 })
	mu.Lock()
	defer mu.Unlock()
	for line := range expected {
		if received[line] != 1 {
			t.Fatalf("record %q received %d times, want 1", line, received[line])
		}
	}
	remaining, err := sender.journal.batch(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("acknowledged retained rows=%d, want 0", len(remaining))
	}
}

func TestLokiCanceledAcknowledgementRetainsOriginal(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) }))
	defer receiver.Close()
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "retry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	payload := []byte(`{"ts":1720000000000000007,"line":"original detail","component":"dns"}`)
	if _, err = j.append(payload); err != nil {
		t.Fatal(err)
	}
	s := &lokiSender{journal: j, client: receiver.Client(), endpoint: receiver.URL, instance: "fixture", ctx: context.Background()}
	if err = s.deliver(); err == nil || err.Error() != "loki receiver status 400" {
		t.Fatalf("delivery error=%v", err)
	}
	rows, err := s.pending(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].payload, payload) {
		t.Fatal("permanent receiver rejection lost or changed original record")
	}
}

func TestLokiBatchAdmissionRetainsEveryRecordAndStdout(t *testing.T) {
	journal, err := openDurableJournal(filepath.Join(t.TempDir(), "batch.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	var output bytes.Buffer
	handler := newLokiHandler(slog.NewJSONHandler(&output, nil), &lokiSender{journal: journal, wake: make(chan struct{}, 1)})
	records := make([]slog.Record, 500)
	for i := range records {
		records[i] = slog.NewRecord(time.Unix(1720000000, int64(i)), slog.LevelInfo, fmt.Sprintf("query-%03d", i), 0)
	}
	if err = handler.HandleBatch(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	rows, err := journal.batch(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 500 || strings.Count(output.String(), "\n") != 500 {
		t.Fatalf("journal/stdout rows=%d/%d, want 500 each", len(rows), strings.Count(output.String(), "\n"))
	}
	if journal.commits.Load() >= 500 {
		t.Fatalf("batch required %d commits; expected shared commits", journal.commits.Load())
	}
	for i, row := range rows {
		var record lokiRecord
		if err = json.Unmarshal(row.payload, &record); err != nil {
			t.Fatal(err)
		}
		if record.TS != 1720000000000000000+int64(i) || !strings.Contains(record.Line, fmt.Sprintf(`"msg":"query-%03d"`, i)) {
			t.Fatalf("record %d changed: %+v", i, record)
		}
	}
}

type crashAfterQueryCommitWriter struct{}

func (crashAfterQueryCommitWriter) Write(p []byte) (int, error) { os.Exit(0); return len(p), nil }

func TestLokiQueryOutboxSurvivesPresentationCommitCrash(t *testing.T) {
	if path := os.Getenv("SVART_LOKI_OUTBOX_CRASH"); path != "" {
		initLogging()
		spool, err := openLogSpool(path + ".spool")
		if err != nil {
			t.Fatal(err)
		}
		destination, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=FULL")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = destination.Exec(queryJournalProgressSchema); err != nil {
			t.Fatal(err)
		}
		stmt, err := destination.Prepare(queryLogInsertSQL)
		if err != nil {
			t.Fatal(err)
		}
		logDNS = slog.New(newLokiHandler(slog.NewJSONHandler(crashAfterQueryCommitWriter{}, nil), nil).WithAttrs([]slog.Attr{slog.String("component", "dns")}))
		logQueryLines.Store(true)
		entries, next, err := spool.readBatch(100)
		if err != nil {
			t.Fatal(err)
		}
		lw := &logWriter{db: destination, stmt: stmt, spool: spool}
		lw.flushWithCursor(entries, next)
		t.Fatal("child did not exit after commit")
	}
	d := newSpoolTestDB(t)
	spool, err := openLogSpool(d.path + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		e := makeEntry(i)
		e.ts = 1720000000000000000 + int64(i)
		spool.spill(&e)
	}
	spool.close()
	// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
	cmd := exec.Command(os.Args[0], "-test.run=^TestLokiQueryOutboxSurvivesPresentationCommitCrash$")
	cmd.Env = append(os.Environ(), "SVART_LOKI_OUTBOX_CRASH="+d.path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	var outbox, presented int
	if err = d.verify.QueryRow("SELECT COUNT(*) FROM query_loki_outbox").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err = d.verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&presented); err != nil {
		t.Fatal(err)
	}
	if outbox != 12 || presented != 12 {
		t.Fatalf("crash left outbox=%d presented=%d, want12 each", outbox, presented)
	}
	var mu sync.Mutex
	received := map[string]int{}
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Streams []struct {
				Values [][2]string `json:"values"`
			} `json:"streams"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		for _, stream := range body.Streams {
			for _, value := range stream.Values {
				var line struct {
					Domain string `json:"domain"`
				}
				if err := json.Unmarshal([]byte(value[1]), &line); err != nil {
					t.Error(err)
				}
				received[line.Domain]++
			}
		}
		w.WriteHeader(204)
	}))
	defer receiver.Close()
	sender, err := newLokiSender(d.path+".loki.sqlite", receiver.URL, "fixture", receiver.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := sender.shutdown(context.Background()); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	oldLogger, oldLines := logDNS, logQueryLines.Load()
	logDNS = slog.New(newLokiHandler(slog.NewJSONHandler(io.Discard, nil), sender).WithAttrs([]slog.Attr{slog.String("component", "dns")}))
	logQueryLines.Store(true)
	defer func() { logDNS = oldLogger; logQueryLines.Store(oldLines) }()
	lw := d.writer(t, 8)
	waitLogCondition(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(received) == 12 })
	stopWriter(lw)
	if err = d.verify.QueryRow("SELECT COUNT(*) FROM query_loki_outbox").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err = d.verify.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&presented); err != nil {
		t.Fatal(err)
	}
	if outbox != 0 || presented != 12 {
		t.Fatalf("recovery outbox=%d presented=%d", outbox, presented)
	}
	mu.Lock()
	defer mu.Unlock()
	for i := 0; i < 12; i++ {
		if received[makeEntry(i).queryName] != 1 {
			t.Fatalf("receiver=%v", received)
		}
	}
}

func TestLokiDNSAdmissionAsyncAndShutdownDrains(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer receiver.Close()
	path := filepath.Join(t.TempDir(), "dns-loki.sqlite")
	sender, err := newLokiSender(path, receiver.URL, "fixture", receiver.Client())
	if err != nil {
		t.Fatal(err)
	}
	unlock := (&spoolTestDB{path: path}).holdWriteLock(t)
	var output bytes.Buffer
	h := newLokiHandler(slog.NewJSONHandler(&output, nil), sender).WithAttrs([]slog.Attr{slog.String("component", "dns")})
	admitted := make(chan error, 1)
	go func() {
		admitted <- h.Handle(context.Background(), slog.NewRecord(time.Unix(1720000000, 0), slog.LevelWarn, "resolution failed", 0))
	}()
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("DNS warning waited for disk")
	}
	closed := make(chan error, 1)
	go func() { closed <- sender.shutdown(context.Background()) }()
	waitLogCondition(t, func() bool { return sender.journal.failures.Load() > 0 })
	select {
	case err := <-closed:
		t.Fatalf("shutdown succeeded early: %v", err)
	default:
	}
	unlock()
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	j, err := openDurableJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	rows, err := j.batch(0, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("records=%d err=%v", len(rows), err)
	}
	var record lokiRecord
	if err = json.Unmarshal(rows[0].payload, &record); err != nil {
		t.Fatal(err)
	}
	if record.Line != output.String() || !strings.Contains(record.Line, "resolution failed") {
		t.Fatalf("preserved line=%s stdout=%s", record.Line, output.String())
	}
}
