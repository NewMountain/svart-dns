package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
	dto "github.com/prometheus/client_model/go"
)

// A durable acknowledgement survives immediate process death. An ordinary DNS
// response only acknowledges bounded volatile enqueue and is not this boundary.
func TestDurableAcknowledgementProcessCrash(t *testing.T) {
	if path := os.Getenv("SVART_CRASH_JOURNAL"); path != "" {
		initLogging()
		s, err := openLogSpool(path)
		if err != nil {
			t.Fatal(err)
		}
		e := makeEntry(7)
		e.ts = 1720000000123456789
		s.spill(&e)
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "queries.spool")
	// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
	cmd := exec.Command(os.Args[0], "-test.run=^TestDurableAcknowledgementProcessCrash$")
	cmd.Env = append(os.Environ(), "SVART_CRASH_JOURNAL="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	s, err := openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	entries, _, err := s.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	expected := makeEntry(7)
	expected.ts = 1720000000123456789
	if !reflect.DeepEqual(entries, []queryLogEntry{expected}) {
		t.Fatalf("after process death: %#v; want exact admitted event %#v", entries, expected)
	}
}

func TestDurableDoomLoopRawEventsRetained(t *testing.T) {
	d := newSpoolTestDB(t)
	lw := d.writer(t, 8)
	expected := make([]queryLogEntry, 60)
	for i := range expected {
		e := makeBlockedEntry("192.0.2.18", "telemetry.tv.example.")
		e.ts = 1720000000000000000 + int64(i)
		e.latencyMicroseconds = int64(i + 100)
		if i%2 == 0 {
			e.queryType = "AAAA"
		}
		e.resultRule = "telemetry.tv.example"
		expected[i] = e
		lw.log(e)
	}
	stopWriter(lw)
	s, err := openLogSpool(d.path + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	// Raw history is retained independently of the presentation cursor.
	s.read.Store(0)
	actual, _, err := s.readBatch(100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("raw journal retained %d events, want all 60 with original time/type/latency/policy", len(actual))
	}
}

func TestDurableLegacyTruncateCrashStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.spool")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".offset", []byte("127\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openLogSpool(path)
	if err != nil {
		t.Fatalf("legacy fully processed journal must recover truncate-before-offset-reset crash: %v", err)
	}
	defer s.close()
	entries, _, err := s.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries=%d, want 0", len(entries))
	}
}

func TestDurableReplayCommitProgressCrash(t *testing.T) {
	if path := os.Getenv("SVART_CURSOR_CRASH"); path != "" {
		initLogging()
		journal, err := openLogSpool(path + ".spool")
		if err != nil {
			t.Fatal(err)
		}
		writerDB, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=FULL")
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := writerDB.Prepare(queryLogInsertSQL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = writerDB.Exec("CREATE TABLE query_journal_progress(source TEXT PRIMARY KEY, sequence INTEGER NOT NULL)"); err != nil {
			t.Fatal(err)
		}
		lw := &logWriter{db: writerDB, stmt: stmt, spool: journal}
		entries, next, err := journal.readBatch(100)
		if err != nil {
			t.Fatal(err)
		}
		if !lw.flushWithCursor(entries, next) {
			t.Fatal("transaction failed")
		}
		// Die after destination commit, before updating any in-memory cursor
		// or closing/checkpointing either SQLite database.
		os.Exit(0)
	}
	d := newSpoolTestDB(t)
	journal, err := openLogSpool(d.path + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		e := makeEntry(i)
		e.ts = 1720000000000000000 + int64(i)
		journal.spill(&e)
	}
	journal.close()
	// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
	cmd := exec.Command(os.Args[0], "-test.run=^TestDurableReplayCommitProgressCrash$")
	cmd.Env = append(os.Environ(), "SVART_CURSOR_CRASH="+d.path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	lw := d.writer(t, 8)
	stopWriter(lw)
	var count, distinct int
	if err = d.verify.QueryRow("SELECT COUNT(*),COUNT(DISTINCT query_name) FROM query_logs").Scan(&count, &distinct); err != nil {
		t.Fatal(err)
	}
	if count != 12 || distinct != 12 {
		t.Fatalf("restart rows=%d distinct=%d; want 12 each", count, distinct)
	}
}

func TestDurableLegacyPartialRecordPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queries.spool")
	e := makeEntry(3)
	e.ts = 1720000000123456789
	r := toSpoolRecord(&e)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	data = append(data, []byte(`{"ts":1720000000`)...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	got, _, err := s.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []queryLogEntry{e}) {
		t.Fatalf("complete legacy records=%#v, want exact one", got)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	retained, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(retained) != string(data) {
		t.Fatal("legacy partial append bytes were modified")
	}
	next := makeEntry(4)
	next.ts = time.Now().UnixNano()
	s.spill(&next)
}

func waitLogCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("log state condition did not become true within 20s")
		case <-tick.C:
		}
	}
}

func TestJournalDiskFullRetainsPendingAdmission(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "full.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	var pages int
	if err = j.db.QueryRow("PRAGMA page_count").Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec("PRAGMA max_page_count=" + fmt.Sprint(pages)); err != nil {
		t.Fatal(err)
	}
	// Force real SQLite SQLITE_FULL, rather than mocking the error return.
	payload := bytes.Repeat([]byte("full event payload\n"), 10000)
	finished := make(chan error, 1)
	go func() { _, err := j.append(payload); finished <- err }()
	waitLogCondition(t, func() bool { return j.failures.Load() > 0 })
	select {
	case err := <-finished:
		t.Fatalf("unpersisted admission returned: %v", err)
	default:
	}
	rows, err := j.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("partial records=%d, want 0", len(rows))
	}
	closed := make(chan error, 1)
	go func() { closed <- j.close() }()
	waitLogCondition(t, func() bool { j.mu.RLock(); defer j.mu.RUnlock(); return j.closed })
	select {
	case err := <-closed:
		t.Fatalf("shutdown abandoned pending admission: %v", err)
	default:
	}
	if _, err = j.db.Exec("PRAGMA max_page_count=1073741823"); err != nil {
		t.Fatal(err)
	}
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	if err = <-closed; err != nil {
		t.Fatal(err)
	}
	recovered, err := openDurableJournal(j.path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := recovered.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	rows, err = recovered.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].payload, payload) {
		t.Fatal("recovery did not preserve exactly one full payload")
	}
}

func TestJournalRetryTokenExactlyOnce(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "retry.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	request := &journalRequest{token: "replayed-commit", payload: []byte(`{"domain":"example.org","latency_us":27}`)}
	if err = j.writeBatch([]*journalRequest{request}); err != nil {
		t.Fatal(err)
	}
	if err = j.writeBatch([]*journalRequest{request}); err != nil {
		t.Fatal(err)
	}
	rows, err := j.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].payload) != `{"domain":"example.org","latency_us":27}` {
		t.Fatalf("retry duplicated or altered event: %#v", rows)
	}
}

func TestJournalUncommittedProcessCrash(t *testing.T) {
	if path := os.Getenv("SVART_UNCOMMITTED_JOURNAL"); path != "" {
		j, err := openDurableJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = j.append([]byte("accepted first event")); err != nil {
			t.Fatal(err)
		}
		j.db.SetMaxOpenConns(1)
		if _, err = j.db.Exec("PRAGMA cache_size=8"); err != nil {
			t.Fatal(err)
		}
		// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
		before, err := os.Stat(path + "-wal")
		if err != nil {
			t.Fatal(err)
		}
		tx, err := j.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO journal_records(token,payload) VALUES('unfinished',?)", bytes.Repeat([]byte("partial disk write"), 100000)); err != nil {
			t.Fatal(err)
		}
		// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
		after, err := os.Stat(path + "-wal")
		if err != nil {
			t.Fatal(err)
		}
		if after.Size() <= before.Size() {
			t.Fatal("fixture did not spill uncommitted pages to disk")
		}
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "crash.sqlite")
	// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
	cmd := exec.Command(os.Args[0], "-test.run=^TestJournalUncommittedProcessCrash$")
	cmd.Env = append(os.Environ(), "SVART_UNCOMMITTED_JOURNAL="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child %v: %s", err, out)
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
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || string(rows[0].payload) != "accepted first event" {
		t.Fatalf("crash recovery=%#v; want exactly accepted first event", rows)
	}
}

func TestJournalClosedRejectsAdmission(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "closed.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = j.close(); err != nil {
		t.Fatal(err)
	}
	_, err = j.append([]byte("after shutdown"))
	if err == nil || err.Error() != "journal closed: event not admitted" {
		t.Fatalf("closed admission error=%v", err)
	}
}

func TestAsyncDNSUDPAnswersDrainToRawEvents(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	previous := loggingEnabled.Load()
	loggingEnabled.Store(true)
	defer loggingEnabled.Store(previous)
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))
	addr, _ := secdnsServe(t)
	var group sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			client := &dns.Client{Net: "udp", Timeout: 10 * time.Second}
			for i := worker; i < 256; i += 16 {
				name := fmt.Sprintf("burst-%03d.example.org.", i)
				request := new(dns.Msg)
				request.SetQuestion(name, dns.TypeA)
				response, _, err := client.Exchange(request, addr)
				if err != nil {
					t.Error(err)
					return
				}
				if response.Rcode != dns.RcodeSuccess {
					t.Errorf("rcode %d", response.Rcode)
					return
				}
			}
		}(worker)
	}
	group.Wait()
	waitLogCondition(t, func() bool { return queryLogWriter.queued.Load() == 0 })
	rows, err := queryLogWriter.spool.journal.batch(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 256 {
		t.Fatalf("UDP burst raw records=%d, want 256", len(rows))
	}
}

func TestDNSJournalAdmissionBoundDuringTransientFailure(t *testing.T) {
	if len(dnsLogAdmission) != 0 {
		t.Fatalf("initial admissions=%d, want 0", len(dnsLogAdmission))
	}
	for i := 0; i < cap(dnsLogAdmission); i++ {
		if !admitDNSLog() {
			t.Fatalf("slot %d refused early", i)
		}
	}
	if admitDNSLog() {
		t.Fatal("saturated journal admitted another handler")
	}
	for i := 0; i < cap(dnsLogAdmission); i++ {
		<-dnsLogAdmission
	}
	if !admitDNSLog() {
		t.Fatal("recovered journal failed admission")
	}
	<-dnsLogAdmission
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "unavailable.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	saved := queryLogWriter
	queryLogWriter = &logWriter{spool: &logSpool{journal: j}}
	defer func() { queryLogWriter = saved }()
	j.unavailable.Store(true)
	if !admitDNSLog() {
		t.Fatal("available volatile capacity refused during transient storage failure")
	}
	<-dnsLogAdmission
}

func TestDNSJournalOutageBuffersAndRecoversOverUDP(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	previous := loggingEnabled.Load()
	loggingEnabled.Store(true)
	defer loggingEnabled.Store(previous)
	addr, _ := secdnsServe(t)
	journal := queryLogWriter.spool.journal
	unlock := (&spoolTestDB{path: journal.path}).holdWriteLock(t)
	released := false
	defer func() {
		if !released {
			unlock()
		}
	}()
	before := dnsGuardStats.overloadShed.Load()
	client := &dns.Client{Net: "udp", Timeout: 500 * time.Millisecond}
	for i := 0; i < 64; i++ {
		request := new(dns.Msg)
		request.SetQuestion(fmt.Sprintf("outage-%d.example.org.", i), dns.TypeANY)
		response, _, err := client.Exchange(request, addr)
		if err != nil || response.Rcode != dns.RcodeSuccess {
			t.Fatalf("reply %d while storage unavailable: response=%v err=%v", i, response, err)
		}
		if i == 0 {
			waitLogCondition(t, func() bool { return journal.failures.Load() > 0 })
		}
	}
	if delta := dnsGuardStats.overloadShed.Load() - before; delta != 0 {
		t.Fatalf("transient outage rejected %d requests despite capacity", delta)
	}
	if got := queryLogWriter.queued.Load(); got != 64 {
		t.Fatalf("retained volatile events=%d want64", got)
	}
	rows, err := journal.batch(0, 100)
	if err != nil || len(rows) != 0 {
		t.Fatalf("uncommitted records=%d err=%v", len(rows), err)
	}
	unlock()
	released = true
	waitLogCondition(t, func() bool { return queryLogWriter.queued.Load() == 0 })
	rows, err = journal.batch(0, 100)
	if err != nil || len(rows) != 64 {
		t.Fatalf("recovered records=%d err=%v", len(rows), err)
	}
	seen := map[string]bool{}
	for _, row := range rows {
		var raw spoolRecord
		if err = json.Unmarshal(row.payload, &raw); err != nil {
			t.Fatal(err)
		}
		seen[raw.QueryName] = true
	}
	for i := 0; i < 64; i++ {
		if !seen[fmt.Sprintf("outage-%d.example.org.", i)] {
			t.Fatalf("missing event %d", i)
		}
	}
}

func TestJournalSaturatedAdmissionBackpressuresWithoutLoss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saturated.sqlite")
	journal, err := openDurableJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	lock, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, lock) }()
	lock.SetMaxOpenConns(1)
	if _, err = lock.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	var completed atomic.Int64
	var group sync.WaitGroup
	for i := 0; i < 10000; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			_, err := journal.append([]byte(fmt.Sprintf("record-%04d", i)))
			if err != nil {
				t.Error(err)
				return
			}
			completed.Add(1)
		}(i)
	}
	waitLogCondition(t, func() bool { return len(journal.requests) == cap(journal.requests) && journal.failures.Load() > 0 })
	if completed.Load() != 0 {
		t.Fatalf("storage locked but %d admissions completed", completed.Load())
	}
	if _, err = lock.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if completed.Load() != 10000 {
		t.Fatalf("completed=%d, want 10000", completed.Load())
	}
	rows, err := journal.batch(0, 11000)
	if err != nil {
		t.Fatal(err)
	}
	actual := make(map[string]int)
	for _, row := range rows {
		actual[string(row.payload)]++
	}
	if len(rows) != 10000 || len(actual) != 10000 {
		t.Fatalf("rows=%d distinct=%d, want 10000 each", len(rows), len(actual))
	}
	for i := 0; i < 10000; i++ {
		if actual[fmt.Sprintf("record-%04d", i)] != 1 {
			t.Fatalf("record %d missing or duplicated", i)
		}
	}
}

func TestJournalMetricsSeparateBacklogFromRetainedHistory(t *testing.T) {
	saved := queryLogWriter
	spool, err := openLogSpool(filepath.Join(t.TempDir(), "metrics.spool"))
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	queryLogWriter = &logWriter{spool: spool}
	defer func() { queryLogWriter = saved }()
	entry := makeEntry(8)
	entry.ts = 1720000000000000000
	spool.spill(&entry)
	metric := new(dto.Metric)
	if err = logSpoolPendingRecords.Write(metric); err != nil {
		t.Fatal(err)
	}
	if got := metric.GetGauge().GetValue(); got != 1 {
		t.Fatalf("pending records=%v, want 1", got)
	}
	spool.read.Store(spool.written.Load())
	if err = logSpoolPendingBytes.Write(metric); err != nil {
		t.Fatal(err)
	}
	if got := metric.GetGauge().GetValue(); got != 0 {
		t.Fatalf("caught-up pending bytes=%v, want 0", got)
	}
	if err = logSpoolRetainedBytes.Write(metric); err != nil {
		t.Fatal(err)
	}
	if got := metric.GetGauge().GetValue(); got <= 0 {
		t.Fatalf("retained storage=%v, want positive bytes", got)
	}
	if err = spool.journal.db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = logSpoolPendingBytes.Write(metric); err != nil {
		t.Fatal(err)
	}
	if got := metric.GetGauge().GetValue(); !math.IsNaN(got) {
		t.Fatalf("unavailable pending bytes=%v, want NaN", got)
	}
}

func TestJournalBatchPersistenceTimingPreservesRawProcessingTime(t *testing.T) {
	d := newSpoolTestDB(t)
	writer := d.writer(t, logChannelSize)
	before := new(dto.Metric)
	if err := logWriterBatchDuration.Write(before); err != nil {
		t.Fatal(err)
	}
	event := makeEntry(7)
	event.latencyMicroseconds = 23
	event.ts = 1720000000000000000
	writer.log(event)
	stopWriter(writer)
	after := new(dto.Metric)
	if err := logWriterBatchDuration.Write(after); err != nil {
		t.Fatal(err)
	}
	if got := after.GetHistogram().GetSampleCount() - before.GetHistogram().GetSampleCount(); got != 1 {
		t.Fatalf("batch samples=%d, want 1", got)
	}
	spool, err := openLogSpool(d.path + ".spool")
	if err != nil {
		t.Fatal(err)
	}
	defer spool.close()
	rows, _, err := spool.readBatch(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].latencyMicroseconds != 23 || rows[0].ts != 1720000000000000000 {
		t.Fatalf("raw processing time/timestamp changed: %+v", rows)
	}
}

// A real SQLite writer lock must not delay ordinary UDP replies while memory
// has room. Clean shutdown must then wait, retaining that volatile event.
func TestAsyncDNSReplyWhileJournalBlockedAndCloseDrains(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	previous := loggingEnabled.Load()
	loggingEnabled.Store(true)
	defer loggingEnabled.Store(previous)
	journal := queryLogWriter.spool.journal
	unlock := (&spoolTestDB{path: journal.path}).holdWriteLock(t)
	released := false
	defer func() {
		if !released {
			unlock()
		}
	}()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan struct{})
	started := make(chan struct{})
	server := &dns.Server{PacketConn: pc, Net: "udp", Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) { handleDNSRequest(w, r); close(handled) })}
	server.NotifyStartedFunc = func() { close(started) }
	go func() {
		if err := server.ActivateAndServe(); err != nil {
			t.Errorf("DNS fixture server failed: %v", err)
		}
	}()
	<-started
	defer func() {
		if err := server.Shutdown(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	addr := pc.LocalAddr().String()
	request := new(dns.Msg)
	request.SetQuestion("async-blocked.example.org.", dns.TypeANY)
	client := &dns.Client{Net: "udp", Timeout: 500 * time.Millisecond}
	response, _, err := client.Exchange(request, addr)
	if err != nil || response.Rcode != dns.RcodeSuccess {
		t.Fatalf("reply while journal locked: response=%v err=%v", response, err)
	}
	if queryLogWriter.queued.Load() != 1 {
		t.Fatalf("volatile events=%d, want 1", queryLogWriter.queued.Load())
	}
	<-handled
	closed := make(chan struct{})
	close(queryLogWriter.ch)
	go func() { <-queryLogWriter.done; close(closed) }()
	waitLogCondition(t, func() bool { return journal.failures.Load() > 0 })
	select {
	case <-closed:
		t.Fatal("shutdown succeeded while its event was not durable")
	default:
	}
	unlock()
	released = true
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not recover")
	}
	rows, err := journal.batch(0, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("drained records=%d err=%v", len(rows), err)
	}
	var raw spoolRecord
	if err = json.Unmarshal(rows[0].payload, &raw); err != nil || raw.QueryName != "async-blocked.example.org." {
		t.Fatalf("drained payload=%s err=%v", rows[0].payload, err)
	}
	// The standard cleanup closes the global writer; it is already drained here.
	queryLogWriter.spool.close()
	checkTestClose(t, queryLogWriter.stmt)
	checkTestClose(t, queryLogWriter.db)
	queryLogWriter = nil
}

func TestAsyncQueueBackpressureRetainsAllEvents(t *testing.T) {
	lw, _, cleanup := newTestLogWriter(t, true)
	defer cleanup()
	lw.ch = make(chan *queryLogEntry, 8)
	for i := 0; i < 8; i++ {
		lw.log(makeEntry(i))
	}
	ninth := make(chan struct{})
	go func() { lw.log(makeEntry(8)); close(ninth) }()
	waitLogCondition(t, func() bool { return lw.queued.Load() == 9 })
	select {
	case <-ninth:
		t.Fatal("full queue accepted another event without backpressure")
	default:
	}
	if len(lw.ch) != 8 {
		t.Fatalf("queue=%d want bounded 8", len(lw.ch))
	}
	go lw.run()
	<-ninth
	close(lw.ch)
	<-lw.done
	rows, err := lw.spool.journal.batch(0, 20)
	if err != nil || len(rows) != 9 {
		t.Fatalf("records=%d err=%v", len(rows), err)
	}
	for i, row := range rows {
		var raw spoolRecord
		if err = json.Unmarshal(row.payload, &raw); err != nil || raw.QueryName != makeEntry(i).queryName {
			t.Fatalf("record %d=%s err=%v", i, row.payload, err)
		}
	}
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)
}

func TestJournalPreservesLogicalBatchCommit(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "batch.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	payloads := make([][]byte, 5000)
	for i := range payloads {
		payloads[i] = []byte(fmt.Sprintf("event-%d", i))
	}
	if _, err = j.appendBatch(payloads); err != nil {
		t.Fatal(err)
	}
	if got := j.commits.Load(); got != 1 {
		t.Fatalf("logical batch used %d commits, want 1", got)
	}
	rows, err := j.batch(0, 5001)
	if err != nil || len(rows) != 5000 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	for i, row := range rows {
		if !bytes.Equal(row.payload, payloads[i]) {
			t.Fatalf("row %d changed", i)
		}
	}
}

// A stalled persistence worker plus a full queue may retain at most the DNS
// admission limit of waiting handlers. Excess requests fail before acceptance.
func TestAsyncDNSSaturationBoundsWaitingHandlers(t *testing.T) {
	cleanup := setupTestDBTB(t)
	defer cleanup()
	closeLogWriter()
	lw, _, writerCleanup := newTestLogWriter(t, true)
	defer writerCleanup()
	lw.ch = make(chan *queryLogEntry, 8)
	queryLogWriter = lw
	previous := loggingEnabled.Load()
	loggingEnabled.Store(true)
	defer loggingEnabled.Store(previous)
	for i := 0; i < 8; i++ {
		lw.log(makeEntry(i))
	}
	var group sync.WaitGroup
	for i := 0; i < cap(dnsLogAdmission); i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			request := new(dns.Msg)
			request.SetQuestion(fmt.Sprintf("waiting-%d.example.org.", i), dns.TypeANY)
			writer := &mockDNSWriter{addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10000 + i}}
			handleDNSRequest(writer, request)
			if writer.written == nil || writer.written.Rcode != dns.RcodeSuccess {
				t.Errorf("accepted handler %d response=%v", i, writer.written)
			}
		}(i)
	}
	waitLogCondition(t, func() bool { return lw.queued.Load() == int64(8+cap(dnsLogAdmission)) })
	before := dnsGuardStats.overloadShed.Load()
	for i := 0; i < 64; i++ {
		request := new(dns.Msg)
		request.SetQuestion("rejected.example.org.", dns.TypeANY)
		writer := &mockDNSWriter{addr: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 30000 + i}}
		handleDNSRequest(writer, request)
		if writer.written == nil || writer.written.Rcode != dns.RcodeServerFailure {
			t.Fatalf("overload response=%v", writer.written)
		}
	}
	if delta := dnsGuardStats.overloadShed.Load() - before; delta != 64 {
		t.Fatalf("rejections=%d want64", delta)
	}
	if len(lw.ch) != 8 || len(dnsLogAdmission) != 1024 || lw.queued.Load() != 1032 {
		t.Fatalf("unbounded admission: channel=%d handlers=%d queued=%d", len(lw.ch), len(dnsLogAdmission), lw.queued.Load())
	}
	go lw.run()
	group.Wait()
	close(lw.ch)
	<-lw.done
	rows, err := lw.spool.journal.batch(0, 2000)
	if err != nil || len(rows) != 1032 {
		t.Fatalf("recovered originals=%d err=%v", len(rows), err)
	}
	checkTestClose(t, lw.stmt)
	checkTestClose(t, lw.db)
	queryLogWriter = nil
}

func TestJournalMixedLogicalBatchStatistics(t *testing.T) {
	j, err := openDurableJournal(filepath.Join(t.TempDir(), "mixed.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := j.close(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	// A 4,999-record tail may collect the following 5,000-record logical batch.
	batch := make([]*journalRequest, 9999)
	for i := range batch {
		batch[i] = &journalRequest{token: journalToken(), payload: []byte(fmt.Sprintf("mixed-%d", i))}
	}
	if err = j.writeBatch(batch); err != nil {
		t.Fatal(err)
	}
	if j.batchSizes[14].Load() != 1 {
		t.Fatal("mixed batch not recorded in its bounded histogram bucket")
	}
	rows, err := j.batch(0, 10000)
	if err != nil || len(rows) != 9999 {
		t.Fatalf("records=%d err=%v", len(rows), err)
	}
}
