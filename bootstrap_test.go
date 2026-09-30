package main

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func nxdomain(r *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetRcode(r, dns.RcodeNameError)
	return m
}

func silent(*dns.Msg) *dns.Msg { return nil }

// hostUpstream returns "udp://<host>:<port of stub>" so the upstream must be
// found through the bootstrap servers.
func hostUpstream(host string, stub *upstreamStub) string {
	_, port, err := net.SplitHostPort(stub.addr)
	if err != nil {
		panic(err)
	}
	return "udp://" + net.JoinHostPort(host, port)
}

// TestBootstrapResolvesAndCachesUpstreamHostname pins the happy path: one
// bootstrap lookup serves every later query to that upstream.
func TestBootstrapResolvesAndCachesUpstreamHostname(t *testing.T) {
	defer setupTestDB(t)()
	bootstrap := startUpstreamStub(t, "udp", answerA("127.0.0.1"))
	useBootstrap(t, bootstrap.addr)
	upstream := startUpstreamStub(t, "udp", answerA("93.184.216.34"))

	for i := 0; i < 3; i++ {
		resp, err := queryA(t, hostUpstream("cached.example.com", upstream), "www.example.org.")
		if err != nil || len(resp.Answer) != 1 {
			t.Fatalf("query %d: resp=%v err=%v", i, resp, err)
		}
	}
	if got := len(bootstrap.queries()); got != 1 {
		t.Errorf("bootstrap servers queried %d times for one hostname, want 1", got)
	}
	if got := len(upstream.queries()); got != 3 {
		t.Errorf("upstream received %d queries, want 3", got)
	}
}

// TestBootstrapCacheEntriesExpire replaces the tests of the old
// bootstrapCache: a live entry (positive or negative) answers without a
// bootstrap query, an expired one of either kind is looked up again.
func TestBootstrapCacheEntriesExpire(t *testing.T) {
	defer setupTestDB(t)()
	bootstrap := startUpstreamStub(t, "udp", answerA("127.0.0.1"))
	useBootstrap(t, bootstrap.addr)
	now := clock.Now()

	bootstrapHosts.Store("live.example.com", bootstrapEntry{ip: "192.0.2.10", expiresAt: now.Add(time.Hour)})
	bootstrapHosts.Store("live-failure.example.com", bootstrapEntry{err: errNoBootstrapServers, expiresAt: now.Add(time.Hour)})
	bootstrapHosts.Store("expired.example.com", bootstrapEntry{ip: "192.0.2.10", expiresAt: now.Add(-time.Second)})
	bootstrapHosts.Store("expired-failure.example.com", bootstrapEntry{err: errNoBootstrapServers, expiresAt: now.Add(-time.Second)})
	t.Cleanup(func() {
		for _, h := range []string{"live.example.com", "live-failure.example.com", "expired.example.com", "expired-failure.example.com"} {
			bootstrapHosts.Delete(h)
		}
	})

	tests := []struct {
		host    string
		wantIP  string
		wantErr bool
		queries int
	}{
		{"live.example.com", "192.0.2.10", false, 0},
		{"LIVE.example.com.", "192.0.2.10", false, 0},
		{"live-failure.example.com", "", true, 0},
		{"expired.example.com", "127.0.0.1", false, 1},
		{"expired-failure.example.com", "127.0.0.1", false, 1},
	}
	for _, tt := range tests {
		before := len(bootstrap.queries())
		ip, err := bootstrapLookup(context.Background(), tt.host)
		if (err != nil) != tt.wantErr || ip != tt.wantIP {
			t.Errorf("bootstrapLookup(%q) = %q, %v; want %q, error=%v", tt.host, ip, err, tt.wantIP, tt.wantErr)
		}
		if got := len(bootstrap.queries()) - before; got != tt.queries {
			t.Errorf("bootstrapLookup(%q) sent %d bootstrap queries, want %d", tt.host, got, tt.queries)
		}
	}
}

// TestBootstrapFailureIsNegativelyCached is the D17 regression test for
// uncached failures: every query to an upstream whose hostname does not
// resolve used to re-query every bootstrap server.
func TestBootstrapFailureIsNegativelyCached(t *testing.T) {
	defer setupTestDB(t)()
	bootstrap := startUpstreamStub(t, "udp", nxdomain)
	useBootstrap(t, bootstrap.addr)
	upstream := startUpstreamStub(t, "udp", answerA("93.184.216.34"))

	for i := 0; i < 3; i++ {
		if resp, err := queryA(t, hostUpstream("gone.example.com", upstream), "www.example.org."); err == nil {
			t.Fatalf("query %d succeeded through an unresolvable upstream hostname: %v", i, resp)
		}
	}
	if got := len(bootstrap.queries()); got != 1 {
		t.Errorf("bootstrap servers queried %d times for one failing hostname, want 1 (negative cache)", got)
	}
	if got := len(upstream.queries()); got != 0 {
		t.Errorf("upstream received %d queries, want 0", got)
	}
}

// TestBootstrapHonorsRequestContext: a silent bootstrap server used to hold
// the request for a fixed 5 s per server, ignoring the caller's deadline.
func TestBootstrapHonorsRequestContext(t *testing.T) {
	defer setupTestDB(t)()
	bootstrap := startUpstreamStub(t, "udp", silent)
	useBootstrap(t, bootstrap.addr)
	upstream := startUpstreamStub(t, "udp", answerA("93.184.216.34"))

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := queryUpstream(ctx, clientQuery("www.example.org.", dns.TypeA, 1, false, false), hostUpstream("slow.example.com", upstream))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("query succeeded although the bootstrap server never answered")
	}
	if elapsed > 1500*time.Millisecond {
		t.Errorf("bootstrap lookup took %v with a 300ms request deadline", elapsed)
	}
}

// TestBootstrapDoesNotUseWritePool: bootstrap lookups run on the DNS path and
// used to read bootstrap_servers through the single-connection write pool,
// so any long config write stalled upstream resolution.
func TestBootstrapDoesNotUseWritePool(t *testing.T) {
	defer setupTestDB(t)()
	bootstrap := startUpstreamStub(t, "udp", answerA("127.0.0.1"))
	useBootstrap(t, bootstrap.addr)
	upstream := startUpstreamStub(t, "udp", answerA("93.184.216.34"))

	tx, err := db.Begin() // holds the write pool's only connection
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := queryA(t, hostUpstream("writepool.example.com", upstream), "www.example.org.")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("query while the write pool is busy: %v", err)
		}
		checkTestRollback(t, tx)
	case <-time.After(3 * time.Second):
		t.Errorf("bootstrap lookup blocked on the busy write pool")
		checkTestRollback(t, tx)
		<-done // let the blocked lookup finish before the DB is torn down
	}
}

// TestBootstrapValidatesAnswer: the bootstrap reply is plaintext UDP, so it
// gets the same question check as any upstream reply, and only addresses
// owned by the asked name (directly or through its CNAME chain) count.
func TestBootstrapValidatesAnswer(t *testing.T) {
	defer setupTestDB(t)()
	upstream := startUpstreamStub(t, "udp", answerA("93.184.216.34"))

	rr := func(s string) dns.RR {
		r, err := dns.NewRR(s)
		if err != nil {
			t.Fatalf("NewRR(%q): %v", s, err)
		}
		return r
	}
	tests := []struct {
		name   string
		answer func(r *dns.Msg) *dns.Msg
		wantOK bool
	}{
		{"CNAME chain to an A record", func(r *dns.Msg) *dns.Msg {
			m := new(dns.Msg)
			m.SetReply(r)
			m.Answer = []dns.RR{
				rr(r.Question[0].Name + " 300 IN CNAME edge.cdn.example.net."),
				rr("edge.cdn.example.net. 300 IN A 127.0.0.1"),
			}
			return m
		}, true},
		{"A record for a different owner", func(r *dns.Msg) *dns.Msg {
			m := new(dns.Msg)
			m.SetReply(r)
			m.Answer = []dns.RR{rr("attacker.example.net. 300 IN A 127.0.0.1")}
			return m
		}, false},
		{"reply for a different question", func(r *dns.Msg) *dns.Msg {
			m := new(dns.Msg)
			m.SetReply(r)
			m.Question[0].Name = "attacker.example.net."
			m.Answer = []dns.RR{rr(r.Question[0].Name + " 300 IN A 127.0.0.1")}
			return m
		}, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bootstrap := startUpstreamStub(t, "udp", tt.answer)
			useBootstrap(t, bootstrap.addr)
			host := []string{"chain.example.com", "owner.example.com", "question.example.com"}[i]
			resp, err := queryA(t, hostUpstream(host, upstream), "www.example.org.")
			if tt.wantOK && (err != nil || len(resp.Answer) != 1) {
				t.Fatalf("resp=%v err=%v, want the upstream answer", resp, err)
			}
			if !tt.wantOK && err == nil {
				t.Fatalf("upstream reached through a bootstrap answer that does not belong to %s: %v", host, resp)
			}
		})
	}
}
