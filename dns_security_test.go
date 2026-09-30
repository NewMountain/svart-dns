package main

// Regression tests for the September 2026 DNS security review
// (design/SECURITY-REVIEW-2026-09.md, D-series). Every test runs against
// loopback stub servers only; nothing leaves the host.

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// --- helpers (prefixed secdns to stay clear of other test files) -----------

// secdnsUpstream starts a loopback stub DNS server on the given network and
// returns its address. The handler sees exactly what svart sends upstream.
func secdnsUpstream(t testing.TB, network string, h dns.HandlerFunc) string {
	t.Helper()
	srv := &dns.Server{Net: network, Handler: h}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	var addr string
	if network == "udp" {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv.PacketConn = pc
		addr = pc.LocalAddr().String()
	} else {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		srv.Listener = l
		addr = l.Addr().String()
	}
	go func() {
		if err := srv.ActivateAndServe(); err != nil {
			t.Errorf("DNS fixture server failed: %v", err)
		}
	}()
	<-started
	t.Cleanup(func() {
		if err := srv.Shutdown(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	return addr
}

// secdnsServe starts svart's real handler on loopback UDP and TCP.
func secdnsServe(t testing.TB) (udpAddr, tcpAddr string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u := &dns.Server{PacketConn: pc, Net: "udp", Handler: dns.HandlerFunc(handleDNSRequest)}
	tc := &dns.Server{Listener: l, Net: "tcp", Handler: dns.HandlerFunc(handleDNSRequest)}
	s1, s2 := make(chan struct{}), make(chan struct{})
	u.NotifyStartedFunc = func() { close(s1) }
	tc.NotifyStartedFunc = func() { close(s2) }
	go func() {
		if err := u.ActivateAndServe(); err != nil {
			t.Errorf("DNS fixture server failed: %v", err)
		}
	}()
	go func() {
		if err := tc.ActivateAndServe(); err != nil {
			t.Errorf("DNS fixture server failed: %v", err)
		}
	}()
	<-s1
	<-s2
	t.Cleanup(func() {
		if err := u.Shutdown(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := tc.Shutdown(); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	return pc.LocalAddr().String(), l.Addr().String()
}

// secdnsSetUpstream makes addr the only enabled upstream and empties the cache.
func secdnsSetUpstream(t testing.TB, addr string) {
	t.Helper()
	if _, err := db.Exec("DELETE FROM upstreams"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, 1)", addr); err != nil {
		t.Fatal(err)
	}
	if err := loadUpstreamsFromDB(); err != nil {
		t.Fatal(err)
	}
	upstreamStatsMap = sync.Map{}
	cache.clear()
}

func secdnsExchange(t testing.TB, network, addr string, q *dns.Msg) *dns.Msg {
	t.Helper()
	c := &dns.Client{Net: network, Timeout: 3 * time.Second, UDPSize: 65535}
	r, _, err := c.Exchange(q, addr)
	if err != nil {
		t.Fatalf("%s exchange with %s: %v", network, addr, err)
	}
	return r
}

// secdnsARecordUpstream answers every IN A query with 93.184.216.34 and
// counts the queries it received.
func secdnsARecordUpstream(t testing.TB, calls *atomic.Int32) string {
	return secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		if r.Question[0].Qclass == dns.ClassINET && r.Question[0].Qtype == dns.TypeA {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   net.ParseIP("93.184.216.34"),
			})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
}

// --- D1: open resolver ------------------------------------------------------

// TestDNSClientACLDefaultRefusesPublicSources: by default only loopback,
// RFC 1918, CGNAT, link-local and ULA sources are served (plus the
// IPv4-mapped forms). Everyone else gets REFUSED and never reaches the
// rewrite, policy, cache or upstream paths.
func TestDNSClientACLDefaultRefusesPublicSources(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})

	cases := []struct {
		ip      string
		allowed bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.42.1.42", true},
		{"172.20.3.4", true},
		{"192.168.1.5", true},
		{"100.64.12.9", true},
		{"169.254.10.1", true},
		{"fe80::1", true},
		{"fd12:3456:789a::5", true},
		{"::ffff:192.168.1.5", true},
		{"203.0.113.7", false},
		{"8.8.8.8", false},
		{"172.32.0.1", false},
		{"100.128.0.1", false},
		{"2001:db8::1", false},
		{"::ffff:203.0.113.7", false},
	}
	for _, tc := range cases {
		req := new(dns.Msg)
		req.SetQuestion("printer.home.arpa.", dns.TypeA)
		w := newMockWriter(tc.ip)
		handleDNSRequest(w, req)
		if w.written == nil {
			t.Fatalf("%s: no response written", tc.ip)
		}
		if tc.allowed {
			if w.written.Rcode != dns.RcodeSuccess || len(w.written.Answer) != 1 {
				t.Errorf("%s: want NOERROR with the rewrite answer, got %s answers=%d", tc.ip, dns.RcodeToString[w.written.Rcode], len(w.written.Answer))
			}
			continue
		}
		if w.written.Rcode != dns.RcodeRefused {
			t.Errorf("%s: want REFUSED for a client outside the default ACL, got %s", tc.ip, dns.RcodeToString[w.written.Rcode])
		}
		if len(w.written.Answer) != 0 {
			t.Errorf("%s: refused response leaked %d answer records", tc.ip, len(w.written.Answer))
		}
	}
}

// txtFloodUpstream answers every query with ~30 KB of TXT records.
func txtFloodUpstream(t testing.TB, network string, calls *atomic.Int32) string {
	return secdnsUpstream(t, network, func(w dns.ResponseWriter, r *dns.Msg) {
		calls.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		for i := 0; i < 120; i++ {
			m.Answer = append(m.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 3600},
				Txt: []string{strings.Repeat("x", 250)},
			})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
}

// TestUDPResponsesRespectClientSizeLimit ports PoC 1: a 38-byte query used to
// return a 33,998-byte UDP reply with TC=0 (895x amplification), also from
// cache. UDP replies must fit 512 bytes without EDNS, min(advertised, 1232)
// with EDNS, and set TC when records were dropped. TCP gets everything.
func TestUDPResponsesRespectClientSizeLimit(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, "tcp://"+txtFloodUpstream(t, "tcp", &calls))
	udpAddr, tcpAddr := secdnsServe(t)

	for round := 0; round < 2; round++ { // round 1 is served from cache
		plain := new(dns.Msg)
		plain.SetQuestion("amp.attacker.example.", dns.TypeTXT)
		conn, err := net.Dial("udp", udpAddr)
		if err != nil {
			t.Fatal(err)
		}
		wire, fixtureErr6994 := plain.Pack()
		if fixtureErr6994 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr6994)
		}
		if _, err := conn.Write(wire); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		buf := make([]byte, 65535)
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		n, err := conn.Read(buf)
		checkTestClose(t, conn)
		if err != nil {
			t.Fatalf("round %d: read: %v", round, err)
		}
		resp := new(dns.Msg)
		if err := resp.Unpack(buf[:n]); err != nil {
			t.Fatalf("round %d: unpack: %v", round, err)
		}
		if n > 512 {
			t.Errorf("round %d: %d-byte UDP reply to a non-EDNS client (limit 512)", round, n)
		}
		if !resp.Truncated {
			t.Errorf("round %d: non-EDNS reply dropped records but TC=0", round)
		}

		edns := new(dns.Msg)
		edns.SetQuestion("amp.attacker.example.", dns.TypeTXT)
		edns.SetEdns0(4096, false)
		ewire, fixtureErr7838 := edns.Pack()
		if fixtureErr7838 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr7838)
		}
		econn, err := net.Dial("udp", udpAddr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := econn.Write(ewire); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := econn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		en, err := econn.Read(buf)
		checkTestClose(t, econn)
		if err != nil {
			t.Fatalf("round %d: edns read: %v", round, err)
		}
		eresp := new(dns.Msg)
		if err := eresp.Unpack(buf[:en]); err != nil {
			t.Fatalf("round %d: edns unpack: %v", round, err)
		}
		if en > 1232 {
			t.Errorf("round %d: %d-byte UDP reply to an EDNS client advertising 4096 (cap 1232, DNS flag day 2020)", round, en)
		}
		if !eresp.Truncated {
			t.Errorf("round %d: EDNS reply dropped records but TC=0", round)
		}
		if eresp.IsEdns0() == nil {
			t.Errorf("round %d: EDNS query answered without an OPT record", round)
		}

		full := secdnsExchange(t, "tcp", tcpAddr, plain)
		if full.Truncated || len(full.Answer) != 120 {
			t.Errorf("round %d: TCP answer must be complete: TC=%v answers=%d (want 120)", round, full.Truncated, len(full.Answer))
		}
	}
}

// TestANYQueryAnsweredMinimallyPerRFC8482: ANY is the classic amplification
// query. It is answered locally with a single synthesized HINFO and never
// forwarded.
func TestANYQueryAnsweredMinimallyPerRFC8482(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, txtFloodUpstream(t, "udp", &calls))
	udpAddr, tcpAddr := secdnsServe(t)

	q := new(dns.Msg)
	q.SetQuestion("isc.org.", dns.TypeANY)
	for _, tr := range []struct{ network, addr string }{{"udp", udpAddr}, {"tcp", tcpAddr}} {
		r := secdnsExchange(t, tr.network, tr.addr, q)
		if r.Rcode != dns.RcodeSuccess {
			t.Fatalf("%s: want NOERROR, got %s", tr.network, dns.RcodeToString[r.Rcode])
		}
		if len(r.Answer) != 1 {
			t.Fatalf("%s: want exactly one synthesized record, got %d", tr.network, len(r.Answer))
		}
		hinfo, ok := r.Answer[0].(*dns.HINFO)
		if !ok {
			t.Fatalf("%s: want HINFO, got %T", tr.network, r.Answer[0])
		}
		if hinfo.Cpu != "RFC8482" || hinfo.Os != "" || hinfo.Hdr.Name != "isc.org." {
			t.Errorf("%s: want HINFO \"RFC8482\" \"\" for isc.org., got %q %q for %s", tr.network, hinfo.Cpu, hinfo.Os, hinfo.Hdr.Name)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("ANY was forwarded upstream %d times; want 0", got)
	}
}

// TestNonINClassRefusedAndNeverPoisonsIN ports PoC 2: one CHAOS-class query
// used to be cached under the IN key and blank the name for ~1 h.
func TestNonINClassRefusedAndNeverPoisonsIN(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))
	udpAddr, _ := secdnsServe(t)

	evil := new(dns.Msg)
	evil.SetQuestion("www.bank.example.", dns.TypeA)
	evil.Question[0].Qclass = dns.ClassCHAOS
	r1 := secdnsExchange(t, "udp", udpAddr, evil)
	if r1.Rcode != dns.RcodeRefused {
		t.Errorf("CHAOS-class query: want REFUSED, got %s", dns.RcodeToString[r1.Rcode])
	}
	if calls.Load() != 0 {
		t.Errorf("CHAOS-class query was forwarded upstream")
	}

	good := new(dns.Msg)
	good.SetQuestion("www.bank.example.", dns.TypeA)
	r2 := secdnsExchange(t, "udp", udpAddr, good)
	if r2.Rcode != dns.RcodeSuccess || len(r2.Answer) != 1 {
		t.Fatalf("IN A after a CHAOS query: want NOERROR with 1 answer, got %s answers=%d", dns.RcodeToString[r2.Rcode], len(r2.Answer))
	}
}

// TestNonQueryOpcodeNotImplemented: NOTIFY/UPDATE/etc. are not forwarded.
func TestNonQueryOpcodeNotImplemented(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	var calls atomic.Int32
	secdnsSetUpstream(t, secdnsARecordUpstream(t, &calls))

	req := new(dns.Msg)
	req.SetQuestion("www.example.com.", dns.TypeA)
	req.Opcode = dns.OpcodeNotify
	w := newMockWriter("10.42.1.42")
	handleDNSRequest(w, req)
	if w.written == nil || w.written.Rcode != dns.RcodeNotImplemented {
		t.Fatalf("NOTIFY: want NOTIMP, got %+v", w.written)
	}
	if calls.Load() != 0 {
		t.Errorf("NOTIFY was forwarded upstream")
	}
}

// --- D11: handler robustness ------------------------------------------------

// TestHandlerPanicAnswersServfailInsteadOfCrashing: a bug anywhere in the
// handler must not take DNS down for the whole network. A corrupt rewrite
// cache value (not a Rewrite) reliably panics the type assertion.
func TestHandlerPanicAnswersServfailInsteadOfCrashing(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	rewritesCache.Load().Store("corrupt.home.arpa", "not-a-rewrite")

	req := new(dns.Msg)
	req.SetQuestion("corrupt.home.arpa.", dns.TypeA)
	w := newMockWriter("10.42.1.42")

	escaped := func() (p any) {
		defer func() { p = recover() }()
		handleDNSRequest(w, req)
		return nil
	}()
	if escaped != nil {
		t.Fatalf("panic escaped the DNS handler (would crash the server): %v", escaped)
	}
	if w.written == nil || w.written.Rcode != dns.RcodeServerFailure {
		t.Fatalf("want SERVFAIL after a handler panic, got %+v", w.written)
	}
	if w.written.Id != req.Id {
		t.Errorf("SERVFAIL id %d does not match request id %d", w.written.Id, req.Id)
	}
}

// TestTCPPerIPConnectionCap: the TCP listener used to accept unlimited
// connections (each one a goroutine held for the read/idle timeouts).
// Connections past the per-IP cap are closed immediately.
func TestTCPPerIPConnectionCap(t *testing.T) {
	t.Cleanup(setupTestDB(t)) // LIFO: servers stop (draining handlers) before the DB and log writer close
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	checkTestClose(t, l)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- startDNSServer(ctx, "tcp", addr) }()
	t.Cleanup(func() { cancel(); <-errCh })

	var probe net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if probe, err = net.Dial("tcp", addr); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if probe == nil {
		t.Fatalf("TCP DNS server never came up on %s: %v", addr, err)
	}
	checkTestClose(t, probe)

	const attempts = 200
	conns := make([]net.Conn, 0, attempts)
	defer func() {
		for _, c := range conns {
			checkTestClose(t, c)
		}
	}()
	for i := 0; i < attempts; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			continue // refused at the kernel backlog is also fine
		}
		conns = append(conns, c)
	}

	// Probe every connection concurrently against one absolute deadline that
	// is well inside the server's 2 s first-read timeout, so only an explicit
	// cap (not the read timeout) can account for closed connections.
	var closed atomic.Int32
	var wg sync.WaitGroup
	probeDeadline := time.Now().Add(500 * time.Millisecond)
	for _, c := range conns {
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			if err := c.SetReadDeadline(probeDeadline); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			buf := make([]byte, 1)
			if _, err := c.Read(buf); err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					return // still open and idle
				}
				closed.Add(1)
			}
		}(c)
	}
	wg.Wait()
	open := len(conns) - int(closed.Load())
	if open > 64 {
		t.Errorf("%d of %d idle TCP connections from one IP were kept open; want the per-IP cap to close the rest", open, len(conns))
	}
}
