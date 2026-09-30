package svart

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestIndependentExportAtomicSnapshot(t *testing.T) {
	defer setupTestDB(t)()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		res, err := tx.Exec("INSERT INTO blocklists(alias,url,enabled) VALUES(?, '', 1)", fmt.Sprintf("list-%03d-a", i))
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO blocked_domains(blocklist_id,domain) VALUES(?,?)", id, fmt.Sprintf("rule-%03d-a.example", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		from, to := "a", "b"
		for {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			tx, err := db.Begin()
			if err != nil {
				done <- err
				return
			}
			if _, err = tx.Exec("UPDATE blocklists SET alias=replace(alias,?,?)", "-"+from, "-"+to); err != nil {
				checkTestRollback(t, tx)
				done <- err
				return
			}
			if _, err = tx.Exec("UPDATE blocked_domains SET domain=replace(domain,?,?)", "-"+from+".", "-"+to+"."); err != nil {
				checkTestRollback(t, tx)
				done <- err
				return
			}
			if err = tx.Commit(); err != nil {
				done <- err
				return
			}
			from, to = to, from
		}
	}()
	defer func() {
		close(stop)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for n := 0; n < 100; n++ {
		exp, err := buildConfigExport()
		if err != nil {
			t.Fatal(err)
		}
		if len(exp.Blocklists) != 100 {
			t.Fatalf("export %d returned %d lists; want all 100", n, len(exp.Blocklists))
		}
		generation := exp.Blocklists[0].Alias[len(exp.Blocklists[0].Alias)-1:]
		for _, list := range exp.Blocklists {
			if !strings.HasSuffix(list.Alias, "-"+generation) {
				t.Fatalf("export %d mixed list generations: first=%q current=%q", n, exp.Blocklists[0].Alias, list.Alias)
			}
		}
		for _, list := range exp.Blocklists {
			want := "rule-" + strings.TrimPrefix(list.Alias, "list-") + ".example"
			if len(list.Domains) != 1 || list.Domains[0] != want {
				t.Fatalf("export %d returned successful but inconsistent backup: alias=%q rules=%v want exactly %q", n, list.Alias, list.Domains, want)
			}
		}
	}
}

func TestIndependentBootstrapDeadlineDoesNotPoisonNextCaller(t *testing.T) {
	defer setupTestDB(t)()
	invalidateBootstrapCache()
	defer invalidateBootstrapCache()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	stub := startUpstreamStub(t, "udp", func(q *dns.Msg) *dns.Msg {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return answerA("192.0.2.10")(q)
	})
	useBootstrap(t, stub.addr)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := bootstrapLookup(ctx, "deadline-independent.example"); first <- err }()
	<-entered
	err := <-first
	close(release)
	if err == nil {
		t.Fatal("first short request unexpectedly succeeded")
	}
	fresh, cancelFresh := context.WithTimeout(context.Background(), time.Second)
	defer cancelFresh()
	ip, err := bootstrapLookup(fresh, "deadline-independent.example")
	if err != nil || ip != "192.0.2.10" {
		t.Fatalf("fresh caller inherited earlier request failure: ip=%q err=%v stub_requests=%d", ip, err, calls.Load())
	}
}

func TestIndependentBootstrapImportRecordsRemoval(t *testing.T) {
	defer setupTestDB(t)()
	useBootstrap(t, "192.0.2.53:53")
	if _, err := db.Exec("UPDATE bootstrap_servers SET updated_at=?,node_id='peer-before-import'", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	oldPeer, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewBufferString(`{"bootstrap_servers":["192.0.2.54:53"]}`)))
	if w.Code != 200 {
		t.Fatalf("import=%d %s", w.Code, w.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name='bootstrap_servers' AND natural_key='192.0.2.53:53'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := mergeSyncResponse(oldPeer); err != nil {
		t.Fatal(err)
	}
	var resurrected int
	if err := db.QueryRow("SELECT COUNT(*) FROM bootstrap_servers WHERE server='192.0.2.53:53'").Scan(&resurrected); err != nil {
		t.Fatal(err)
	}
	if count != 1 || resurrected != 0 {
		t.Fatalf("successful replacement generated %d removal tombstones; old peer sync resurrected %d removed servers", count, resurrected)
	}
}

type independentObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *independentObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestIndependentBootstrapLeaderCancellationKeepsWaiterAlive(t *testing.T) {
	defer setupTestDB(t)()
	invalidateBootstrapCache()
	defer invalidateBootstrapCache()
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	stub := startUpstreamStub(t, "udp", func(q *dns.Msg) *dns.Msg {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return answerA("192.0.2.10")(q)
	})
	useBootstrap(t, stub.addr)
	leader, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	leaderDone := make(chan error, 1)
	go func() { _, err := bootstrapLookup(leader, "leader-cancel.example"); leaderDone <- err }()
	<-entered
	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	waiter := &independentObservedContext{Context: waitCtx, observed: make(chan struct{})}
	waiterDone := make(chan error, 1)
	go func() {
		ip, err := bootstrapLookup(waiter, "leader-cancel.example")
		if err == nil && ip != "192.0.2.10" {
			err = fmt.Errorf("wrong IP %q", ip)
		}
		waiterDone <- err
	}()
	<-waiter.observed
	cancel()
	<-leaderDone
	close(release)
	if err := <-waiterDone; err != nil {
		t.Fatalf("live coalesced caller inherited canceled leader error: %v; its own context=%v", err, waitCtx.Err())
	}
}

func TestIndependentSyncBootstrapAdditionInvalidatesNegativeCache(t *testing.T) {
	defer setupTestDB(t)()
	invalidateBootstrapCache()
	defer invalidateBootstrapCache()
	if _, err := db.Exec("DELETE FROM bootstrap_servers"); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapLookup(context.Background(), "added-bootstrap.example"); err == nil {
		t.Fatal("empty server config unexpectedly resolved")
	}
	stub := startUpstreamStub(t, "udp", answerA("192.0.2.10"))
	response := &SyncResponse{Changes: SyncChanges{BootstrapSvrs: []SyncBootstrap{{Server: stub.addr, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}}
	if err := mergeSyncResponse(response); err != nil {
		t.Fatal(err)
	}
	ip, err := bootstrapLookup(context.Background(), "added-bootstrap.example")
	if err != nil || ip != "192.0.2.10" {
		t.Fatalf("successful sync addition left stale negative bootstrap result: ip=%q err=%v stub_requests=%d", ip, err, len(stub.queries()))
	}
}

func TestIndependentTokenStorageFailureIsUnavailable(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("DROP TABLE api_tokens"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/export", nil)
	r.Header.Set("X-Api-Key", "sv_deadbeef00000000000000000000000000000000000000000000000000000000")
	_, ok := requireAuth(w, r)
	if ok || w.Code != 503 || w.Body.String() != `{"data":null,"error":"API token verification unavailable; retry later","error_code":"unavailable"}`+"\n" {
		t.Fatalf("DB failure auth response: accepted=%v status=%d body=%s", ok, w.Code, w.Body.String())
	}
}
