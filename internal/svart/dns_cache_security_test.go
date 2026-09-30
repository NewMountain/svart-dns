package svart

// Regression tests for the response cache, miss coalescing, the doom-loop
// throttle, CNAME cloaking and DNS rebinding (review items D2 part, D3, D4,
// D8, D12, D13, D15). Loopback stub upstreams only.

import (
	"fmt"
	"net"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func secdnsNXUpstream(t testing.TB, soaTTL uint32, calls *atomic.Int32) string {
	return secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeNameError
		m.Ns = append(m.Ns, &dns.SOA{
			Hdr: dns.RR_Header{Name: "example.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: soaTTL},
			Ns:  "ns.example.", Mbox: "hostmaster.example.", Serial: 2026092501, Minttl: soaTTL,
		})
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
}

func secdnsAsk(t testing.TB, addr, name string, qtype uint16) *dns.Msg {
	t.Helper()
	q := new(dns.Msg)
	q.SetQuestion(name, qtype)
	return secdnsExchange(t, "udp", addr, q)
}

// --- D3: cache correctness --------------------------------------------------

// TestCacheHitPreservesNXDOMAIN ports PoC 3a: SetReply on a cache hit used
// to rewrite a cached NXDOMAIN into NOERROR.
func TestCacheHitPreservesNXDOMAIN(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsNXUpstream(t, 900, &calls))
	udpAddr, _ := secdnsServe(t)

	miss := secdnsAsk(t, udpAddr, "nx.example.", dns.TypeA)
	hit := secdnsAsk(t, udpAddr, "nx.example.", dns.TypeA)
	if miss.Rcode != dns.RcodeNameError {
		t.Fatalf("miss: want NXDOMAIN, got %s", dns.RcodeToString[miss.Rcode])
	}
	if hit.Rcode != dns.RcodeNameError {
		t.Errorf("cache hit: want NXDOMAIN, got %s", dns.RcodeToString[hit.Rcode])
	}
	if len(hit.Ns) != 1 {
		t.Errorf("cache hit: want the SOA in the authority section, got %d records", len(hit.Ns))
	}
	if calls.Load() != 1 {
		t.Errorf("NXDOMAIN should be cached: upstream called %d times, want 1", calls.Load())
	}
}

// TestNegativeCacheTTLCappedAt300 (RFC 2308): the negative TTL comes from the
// SOA and is capped, both in the cache and in what the client is told.
func TestNegativeCacheTTLCappedAt300(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsNXUpstream(t, 86400, &calls))
	udpAddr, _ := secdnsServe(t)

	secdnsAsk(t, udpAddr, "nx.example.", dns.TypeA)
	hit := secdnsAsk(t, udpAddr, "nx.example.", dns.TypeA)
	if len(hit.Ns) != 1 {
		t.Fatalf("want SOA in authority, got %d records", len(hit.Ns))
	}
	if ttl := hit.Ns[0].Header().Ttl; ttl > 300 {
		t.Errorf("negative answer served with SOA TTL %d; want <= 300 (negative cache cap)", ttl)
	}
}

// TestTransientServfailAndRefusedAreNotCached ports PoC 3b.
func TestTransientServfailAndRefusedAreNotCached(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		n := calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		switch n {
		case 1:
			m.Rcode = dns.RcodeServerFailure
		case 2:
			m.Rcode = dns.RcodeRefused
		default:
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("192.0.2.10")})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	first := secdnsAsk(t, udpAddr, "flaky.example.", dns.TypeA)
	second := secdnsAsk(t, udpAddr, "flaky.example.", dns.TypeA)
	third := secdnsAsk(t, udpAddr, "flaky.example.", dns.TypeA)
	if first.Rcode != dns.RcodeServerFailure {
		t.Errorf("first: want the upstream SERVFAIL, got %s", dns.RcodeToString[first.Rcode])
	}
	if second.Rcode != dns.RcodeRefused {
		t.Errorf("second: SERVFAIL must not be cached; want the upstream REFUSED, got %s", dns.RcodeToString[second.Rcode])
	}
	if third.Rcode != dns.RcodeSuccess || len(third.Answer) != 1 {
		t.Errorf("third: REFUSED must not be cached; want the A record, got %s answers=%d", dns.RcodeToString[third.Rcode], len(third.Answer))
	}
}

// TestTruncatedUpstreamAnswerNotCached ports PoC 5: a TC=1 UDP answer was
// cached and replayed to the client's TCP retry for the whole cache TTL.
func TestTruncatedUpstreamAnswerNotCached(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Truncated = true
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, tcpAddr := secdnsServe(t)

	q := new(dns.Msg)
	q.SetQuestion("bigtxt.example.", dns.TypeTXT)
	secdnsExchange(t, "udp", udpAddr, q)
	secdnsExchange(t, "tcp", tcpAddr, q)
	if got := calls.Load(); got != 2 {
		t.Errorf("truncated answer was cached: upstream saw %d queries, want 2", got)
	}
}

// TestCacheKeyIsCaseInsensitive ports PoC 7a: case variants of one name used
// to create separate entries (and upstream queries).
func TestCacheKeyIsCaseInsensitive(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))
	udpAddr, _ := secdnsServe(t)

	for _, n := range []string{"www.example.com.", "WWW.example.com.", "wWw.ExAmPlE.CoM."} {
		r := secdnsAsk(t, udpAddr, n, dns.TypeA)
		if len(r.Answer) != 1 {
			t.Fatalf("%s: want 1 answer, got %d", n, len(r.Answer))
		}
		if r.Question[0].Name != n {
			t.Errorf("question must echo the client's spelling %q (0x20), got %q", n, r.Question[0].Name)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("3 case variants of one name caused %d upstream queries, want 1", got)
	}
}

// TestCacheKeySeparatesDOAndCD: answers fetched with DNSSEC OK or Checking
// Disabled are different answers and must not be shared.
func TestCacheKeySeparatesDOAndCD(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))
	udpAddr, _ := secdnsServe(t)

	plain := new(dns.Msg)
	plain.SetQuestion("dnssec.example.", dns.TypeA)
	do := plain.Copy()
	do.SetEdns0(1232, true)
	cd := plain.Copy()
	cd.CheckingDisabled = true
	for _, q := range []*dns.Msg{plain, do, cd, plain} {
		secdnsExchange(t, "udp", udpAddr, q)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("plain, DO, CD and plain again caused %d upstream queries, want 3", got)
	}
}

// TestUnknownQtypesHaveDistinctCacheKeys ports PoC 12 (D15).
func TestUnknownQtypesHaveDistinctCacheKeys(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = append(m.Answer, &dns.RFC3597{
			Hdr:   dns.RR_Header{Name: r.Question[0].Name, Rrtype: r.Question[0].Qtype, Class: dns.ClassINET, Ttl: 300},
			Rdata: fmt.Sprintf("%04x", r.Question[0].Qtype),
		})
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	a := secdnsAsk(t, udpAddr, "x.example.", 65400)
	b := secdnsAsk(t, udpAddr, "x.example.", 65401)
	if calls.Load() != 2 {
		t.Errorf("TYPE65400 and TYPE65401 caused %d upstream queries, want 2", calls.Load())
	}
	if len(b.Answer) != 1 || b.Answer[0].Header().Rrtype != 65401 {
		t.Errorf("TYPE65401 was answered with %v (TYPE65400 answer was %v)", b.Answer, a.Answer)
	}
}

// TestCachedTTLCountsDown: a cache hit must serve the remaining TTL, not the
// TTL the upstream sent when the entry was stored.
func TestCachedTTLCountsDown(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))
	udpAddr, _ := secdnsServe(t)

	first := secdnsAsk(t, udpAddr, "countdown.example.", dns.TypeA)
	if first.Answer[0].Header().Ttl != 300 {
		t.Fatalf("miss: want TTL 300, got %d", first.Answer[0].Header().Ttl)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		hit := secdnsAsk(t, udpAddr, "countdown.example.", dns.TypeA)
		if ttl := hit.Answer[0].Header().Ttl; ttl < 300 {
			if ttl < 296 {
				t.Errorf("TTL dropped to %d within 3 s", ttl)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache hit still serves TTL 300 after 3 s; TTLs must count down")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Errorf("hits went upstream: %d calls", calls.Load())
	}
}

// --- D4: bounded cache ------------------------------------------------------

// TestCacheMemoryIsBounded ports PoC 7c: the cache used to grow without
// limit (~500 B per random-subdomain entry, hourly sweep). The default budget
// is 64 MiB; 300k negative answers must not retain more than that.
func TestCacheMemoryIsBounded(t *testing.T) {
	cache.clear()
	defer cache.clear()
	sample := new(dns.Msg)
	sample.SetQuestion("r00000000.attacker.example.", dns.TypeA)
	sample.Response = true
	sample.Rcode = dns.RcodeNameError
	sample.Ns = []dns.RR{&dns.SOA{Hdr: dns.RR_Header{Name: "attacker.example.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600}, Ns: "ns1.attacker.example.", Mbox: "hostmaster.attacker.example.", Serial: 1, Minttl: 3600}}

	before := secdnsHeap()
	const n = 300000
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("r%08x.attacker.example.", i)
		sample.Question[0].Name = name
		cache.set(dnsCacheKey{name: name, qtype: dns.TypeA}, sample, 3600)
	}
	after := secdnsHeap()
	grown := float64(after) - float64(before)
	t.Logf("%d entries inserted: heap grew %.1f MiB", n, float64(grown)/(1<<20))
	if grown > 72<<20 {
		t.Errorf("cache retained %.1f MiB after a 300k-name flood; want it bounded by the 64 MiB default budget", float64(grown)/(1<<20))
	}
}

func secdnsHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// --- D2 (part): coalescing ----------------------------------------------------

// TestConcurrentIdenticalMissesAreCoalesced: N concurrent queries for one
// uncached name must produce one upstream query (also removes the birthday
// attack's many-outstanding-queries precondition).
func TestConcurrentIdenticalMissesAreCoalesced(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	release := make(chan struct{})
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		<-release
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("192.0.2.44")})
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	const clients = 40
	var wg sync.WaitGroup
	var answered atomic.Int32
	for i := 0; i < clients; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			q := new(dns.Msg)
			q.SetQuestion("popular.example.", dns.TypeA)
			c := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
			r, _, err := c.Exchange(q, udpAddr)
			if err == nil && len(r.Answer) == 1 && r.Id == q.Id {
				answered.Add(1)
			}
		}(i)
	}
	// Hold the upstream until every client has joined the same miss. Observing
	// the pending count proves contention; elapsed time cannot prove arrival.
	deadline := time.Now().Add(2 * time.Second)
	for pendingResolutions.Load() != clients && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	pending := pendingResolutions.Load()
	close(release)
	wg.Wait()
	if pending != clients {
		t.Fatalf("pending clients before upstream release = %d, want %d", pending, clients)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("%d concurrent identical misses caused %d upstream queries, want 1", clients, got)
	}
	if answered.Load() != clients {
		t.Errorf("%d of %d clients got a correct answer (own ID, 1 record)", answered.Load(), clients)
	}
}

// --- D8: doom-loop throttle ---------------------------------------------------

// TestDoomLoopThrottleNeverParksGoroutines ports PoC 8: the throttle slept
// 1 s inside the handler, parking one goroutine per query.
func TestDoomLoopThrottleNeverParksGoroutines(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	res, fixtureErr13074 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'doom', 1)")
	if fixtureErr13074 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr13074)
	}
	blID, fixtureErr13164 := res.LastInsertId()
	if fixtureErr13164 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr13164)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'blocked.example')", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.42.1.77', ?)", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES ('10.42.1.77','x.','A','NOERROR',0,1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	mustReloadPolicy(t)

	req := new(dns.Msg)
	req.SetQuestion("blocked.example.", dns.TypeA)
	w := newMockWriter("10.42.1.77")
	start := time.Now()
	handleDNSRequest(w, req)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("doom-looping client waited %v in the handler; the answer must be immediate", elapsed)
	}
	if w.written == nil || w.written.Rcode != dns.RcodeNameError {
		t.Fatalf("want the blocked NXDOMAIN, got %+v", w.written)
	}
}

// TestQueryNameLoggedLowercase: the doom-loop key and all analytics use the
// logged name, so case variants (0x20, "BLOCKED.example") must collapse to
// one lowercase name instead of bypassing coalescing.
func TestQueryNameLoggedLowercase(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	oldLogging := loggingEnabled.Load()
	loggingEnabled.Store(true)
	defer loggingEnabled.Store(oldLogging)
	rewritesCache.Load().Store("nas.home.arpa", Rewrite{Domain: "nas.home.arpa", IPAddresses: []string{"192.168.1.20"}})

	req := new(dns.Msg)
	req.SetQuestion("NaS.Home.ARPA.", dns.TypeA)
	handleDNSRequest(newMockWriter("10.42.1.42"), req)

	deadline := time.Now().Add(3 * time.Second)
	var name string
	for time.Now().Before(deadline) {
		if err := db.QueryRow("SELECT query_name FROM query_logs WHERE client_ip = '10.42.1.42' ORDER BY id DESC LIMIT 1").Scan(&name); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if name != "nas.home.arpa." {
		t.Errorf("logged query name = %q, want %q", name, "nas.home.arpa.")
	}
}

// --- D12: CNAME cloaking ------------------------------------------------------

func secdnsBlockFor(t testing.TB, clientIP string, domains ...string) {
	t.Helper()
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('https://lists.example/cloak.txt', 'cloak-list', 1)")
	if err != nil {
		t.Fatal(err)
	}
	blID, fixtureErr15695 := res.LastInsertId()
	if fixtureErr15695 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr15695)
	}
	for _, d := range domains {
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", clientIP, blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?,'x.','A','NOERROR',0,1)", clientIP); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	mustReloadPolicy(t)
}

// TestCNAMECloakingIsBlocked ports PoC 6: a first-party name that CNAMEs to
// a blocked tracker used to resolve. Every CNAME/DNAME target and SVCB/HTTPS
// TargetName in the answer is checked, on misses and on cache hits.
func TestCNAMECloakingIsBlocked(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	// Queries arrive from 127.0.0.1 (remapped to externalIP when set).
	client := queryClientIP(netip.MustParseAddr("127.0.0.1"))
	secdnsBlockFor(t, client, "tracker.adtech.example", "svcb-target.adtech.example", "dname-target.adtech.example")

	var calls atomic.Int32
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		name := r.Question[0].Name
		switch name {
		case "metrics.news.example.":
			m.Answer = append(m.Answer,
				&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "Tracker.AdTech.example."},
				&dns.A{Hdr: dns.RR_Header{Name: "tracker.adtech.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("203.0.113.9")})
		case "www.shop.example.":
			m.Answer = append(m.Answer, &dns.HTTPS{SVCB: dns.SVCB{
				Hdr:      dns.RR_Header{Name: name, Rrtype: dns.TypeHTTPS, Class: dns.ClassINET, Ttl: 300},
				Priority: 1, Target: "svcb-target.adtech.example."}})
		case "a.sub.cloak.example.":
			m.Answer = append(m.Answer,
				&dns.DNAME{Hdr: dns.RR_Header{Name: "sub.cloak.example.", Rrtype: dns.TypeDNAME, Class: dns.ClassINET, Ttl: 300}, Target: "dname-target.adtech.example."},
				&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "a.dname-target.adtech.example."})
		default:
			m.Answer = append(m.Answer,
				&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "cdn.clean.example."},
				&dns.A{Hdr: dns.RR_Header{Name: "cdn.clean.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("198.51.100.1")})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	for round := 0; round < 2; round++ { // round 1 comes from the cache
		for _, tc := range []struct {
			name  string
			qtype uint16
		}{
			{"metrics.news.example.", dns.TypeA},
			{"www.shop.example.", dns.TypeHTTPS},
			{"a.sub.cloak.example.", dns.TypeA},
		} {
			r := secdnsAsk(t, udpAddr, tc.name, tc.qtype)
			if r.Rcode != dns.RcodeNameError || len(r.Answer) != 0 {
				t.Errorf("round %d: %s cloaks a blocked target; want blocked NXDOMAIN, got %s with %d answers", round, tc.name, dns.RcodeToString[r.Rcode], len(r.Answer))
			}
		}
		clean := secdnsAsk(t, udpAddr, "www.news.example.", dns.TypeA)
		if clean.Rcode != dns.RcodeSuccess || len(clean.Answer) != 2 {
			t.Errorf("round %d: clean CNAME chain must resolve, got %s answers=%d", round, dns.RcodeToString[clean.Rcode], len(clean.Answer))
		}
	}
}

// --- D13: DNS rebinding -------------------------------------------------------

// TestDNSRebindingFilter: with DNS_REBIND_PROTECTION=true, a public name that
// resolves to a private/loopback address is blocked, except for names under
// DNS_REBIND_ALLOW_DOMAINS and local rewrites. Off by default.
func TestDNSRebindingFilter(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		ip := "192.168.1.1"
		if r.Question[0].Name == "public.example." {
			ip = "93.184.216.34"
		}
		if r.Question[0].Qtype == dns.TypeAAAA {
			m.Answer = append(m.Answer, &dns.AAAA{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300}, AAAA: net.ParseIP("::1")})
		} else {
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP(ip)})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	if r := secdnsAsk(t, udpAddr, "rebind.attacker.example.", dns.TypeA); len(r.Answer) != 1 {
		t.Fatalf("filter off by default: want the private answer passed through, got %s", dns.RcodeToString[r.Rcode])
	}

	cfg, err := loadDNSGuardConfig(envMap(map[string]string{
		"DNS_REBIND_PROTECTION":    "true",
		"DNS_REBIND_ALLOW_DOMAINS": "corp.example, plex.direct",
	}))
	if err != nil {
		t.Fatal(err)
	}
	old := dnsGuard.Load()
	dnsGuard.Store(cfg)
	defer dnsGuard.Store(old)
	cache.clear()
	rewritesCache.Load().Store("nas.home.arpa", Rewrite{Domain: "nas.home.arpa", IPAddresses: []string{"192.168.1.20"}})

	for _, tc := range []struct {
		name    string
		qtype   uint16
		blocked bool
	}{
		{"rebind.attacker.example.", dns.TypeA, true},
		{"rebind.attacker.example.", dns.TypeA, true}, // cache hit
		{"loop6.attacker.example.", dns.TypeAAAA, true},
		{"public.example.", dns.TypeA, false},
		{"intranet.corp.example.", dns.TypeA, false},
		{"192-168-1-1.abc123.plex.direct.", dns.TypeA, false},
		{"nas.home.arpa.", dns.TypeA, false},
	} {
		r := secdnsAsk(t, udpAddr, tc.name, tc.qtype)
		gotBlocked := r.Rcode == dns.RcodeNameError && len(r.Answer) == 0
		if gotBlocked != tc.blocked {
			t.Errorf("%s: blocked=%v (rcode %s, %d answers), want blocked=%v", tc.name, gotBlocked, dns.RcodeToString[r.Rcode], len(r.Answer), tc.blocked)
		}
	}
}

// TestCNAMECloakingRespectsExplicitAllow: an explicit allow for the queried
// name also covers what it aliases to, so an allowlisted site behind a
// blocked CDN keeps working. Default-allow names get no such exemption.
func TestCNAMECloakingRespectsExplicitAllow(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	client := queryClientIP(netip.MustParseAddr("127.0.0.1"))
	res, err := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'manual-allow', 1)")
	if err != nil {
		t.Fatal(err)
	}
	alID, fixtureErr22546 := res.LastInsertId()
	if fixtureErr22546 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr22546)
	}
	if _, err := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, 'shop.example')", alID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", client, alID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	secdnsBlockFor(t, client, "cdn.adtech.example")
	mustReloadPolicy(t)

	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		name := r.Question[0].Name
		m.Answer = append(m.Answer,
			&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "cdn.adtech.example."},
			&dns.A{Hdr: dns.RR_Header{Name: "cdn.adtech.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("203.0.113.50")})
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, _ := secdnsServe(t)

	if r := secdnsAsk(t, udpAddr, "www.shop.example.", dns.TypeA); r.Rcode != dns.RcodeSuccess || len(r.Answer) != 2 {
		t.Errorf("allowlisted www.shop.example behind a blocked CDN: want the answer, got %s answers=%d", dns.RcodeToString[r.Rcode], len(r.Answer))
	}
	if r := secdnsAsk(t, udpAddr, "www.other.example.", dns.TypeA); r.Rcode != dns.RcodeNameError {
		t.Errorf("default-allowed www.other.example behind a blocked CDN: want blocked NXDOMAIN, got %s", dns.RcodeToString[r.Rcode])
	}
}

// TestPendingResolutionCapShedsLoad (D11): miekg starts a goroutine per UDP
// packet, and a flood of unique names against a stalled upstream used to
// park one per query for the whole timeout. Past the cap a miss gets
// SERVFAIL at once.
func TestPendingResolutionCapShedsLoad(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	oldCap := maxPendingResolutions
	maxPendingResolutions = 2
	defer func() { maxPendingResolutions = oldCap }()

	var calls atomic.Int32
	release := make(chan struct{})
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		<-release
		m := new(dns.Msg)
		m.SetReply(r)
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, up)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := new(dns.Msg)
			req.SetQuestion(fmt.Sprintf("slow%d.example.", i), dns.TypeA)
			handleDNSRequest(newMockWriter("10.42.1.42"), req)
		}(i)
	}
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if calls.Load() != 2 {
		t.Fatalf("the two leaders never reached the upstream")
	}

	before := dnsGuardStats.overloadShed.Load()
	for i := 0; i < 3; i++ {
		req := new(dns.Msg)
		req.SetQuestion(fmt.Sprintf("flood%d.example.", i), dns.TypeA)
		w := newMockWriter("10.42.1.42")
		start := time.Now()
		handleDNSRequest(w, req)
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Errorf("query past the cap waited %v; it must be shed immediately", elapsed)
		}
		if w.written == nil || w.written.Rcode != dns.RcodeServerFailure {
			t.Errorf("query past the cap: want SERVFAIL, got %+v", w.written)
		}
	}
	if got := dnsGuardStats.overloadShed.Load() - before; got != 3 {
		t.Errorf("overload counter moved by %d, want 3", got)
	}
	close(release)
	wg.Wait()
	if n := pendingResolutions.Load(); n != 0 {
		t.Errorf("pending resolutions = %d after all queries finished, want 0", n)
	}
}
