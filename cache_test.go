package main

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func newTestDNSMsg(name string, qtype uint16) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(name), qtype)
	msg.Response = true
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn(name),
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		A: []byte{93, 184, 216, 34}, // 93.184.216.34
	})
	return msg
}

func testCacheKey(name string, qtype uint16) dnsCacheKey {
	return dnsCacheKey{name: lowerASCII(dns.Fqdn(name)), qtype: qtype}
}

// servedMsg renders a cached answer for a plain query and unpacks it, the way
// a client would see it.
func servedMsg(t testing.TB, e *cachedAnswer, now int64) *dns.Msg {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(e.key.name, e.key.qtype)
	m := new(dns.Msg)
	if err := m.Unpack(e.appendReply(nil, req, now)); err != nil {
		t.Fatalf("served answer does not unpack: %v", err)
	}
	return m
}

func TestCacheSetGet(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("example.com", dns.TypeA)
	c.set(key, newTestDNSMsg("example.com", dns.TypeA), 3600)

	e, ok := c.get(key, clock.NowUnixNano())
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	got := servedMsg(t, e, e.storedAt)
	if got.Question[0].Name != "example.com." {
		t.Errorf("expected question name 'example.com.', got %q", got.Question[0].Name)
	}
	if len(got.Answer) != 1 || requireFixtureType[*dns.A](t, got.Answer[0]).A.String() != "93.184.216.34" {
		t.Errorf("expected the A 93.184.216.34 answer, got %v", got.Answer)
	}
}

func TestCacheMiss(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)

	if _, ok := c.get(testCacheKey("nonexistent.com", dns.TypeA), clock.NowUnixNano()); ok {
		t.Error("expected cache miss for nonexistent domain")
	}
	if stats := c.stats(); stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("shortlived.com", dns.TypeA)
	c.set(key, newTestDNSMsg("shortlived.com", dns.TypeA), 1) // 1-second configured cap

	now := clock.NowUnixNano()
	if _, ok := c.get(key, now); !ok {
		t.Fatal("expected cache hit immediately after set")
	}
	if _, ok := c.get(key, now+int64(1100*time.Millisecond)); ok {
		t.Error("expected cache miss after TTL expiry")
	}
}

func TestCacheZeroTTL(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("notcached.com", dns.TypeA)
	if e := c.set(key, newTestDNSMsg("notcached.com", dns.TypeA), 0); e == nil {
		t.Fatal("caching disabled must still return a servable answer")
	}
	if _, ok := c.get(key, clock.NowUnixNano()); ok {
		t.Error("expected cache miss for zero-TTL entry")
	}
}

func TestCacheNegativeTTL(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("negativettl.com", dns.TypeA)
	c.set(key, newTestDNSMsg("negativettl.com", dns.TypeA), -1)
	if _, ok := c.get(key, clock.NowUnixNano()); ok {
		t.Error("expected cache miss for negative-TTL entry")
	}
}

func TestCacheCleanup(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	c.set(testCacheKey("short.com", dns.TypeA), newTestDNSMsg("short.com", dns.TypeA), 1)
	c.set(testCacheKey("long.com", dns.TypeA), newTestDNSMsg("long.com", dns.TypeA), 3600)

	if stats := c.stats(); stats.Entries != 2 {
		t.Fatalf("expected 2 entries before cleanup, got %d", stats.Entries)
	}
	later := clock.NowUnixNano() + int64(1100*time.Millisecond)
	c.sweep(later)

	stats := c.stats()
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry after cleanup, got %d", stats.Entries)
	}
	if stats.Expired != 1 {
		t.Errorf("expected expired counter 1, got %d", stats.Expired)
	}
	if _, ok := c.get(testCacheKey("long.com", dns.TypeA), later); !ok {
		t.Error("expected long-TTL entry to survive cleanup")
	}
}

func TestCacheClear(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("domain%d.com", i)
		c.set(testCacheKey(name, dns.TypeA), newTestDNSMsg(name, dns.TypeA), 3600)
	}
	now := clock.NowUnixNano()
	c.get(testCacheKey("domain0.com", dns.TypeA), now) // hit
	c.get(testCacheKey("missing.com", dns.TypeA), now) // miss

	if stats := c.stats(); stats.Entries != 10 {
		t.Fatalf("expected 10 entries, got %d", stats.Entries)
	}
	c.clear()

	stats := c.stats()
	if stats.Entries != 0 || stats.EstimatedBytes != 0 {
		t.Errorf("expected an empty cache after clear, got %d entries / %d bytes", stats.Entries, stats.EstimatedBytes)
	}
	if stats.Hits != 0 || stats.Misses != 0 {
		t.Errorf("expected 0 hits/misses after clear, got %d/%d", stats.Hits, stats.Misses)
	}
	if _, ok := c.get(testCacheKey("domain0.com", dns.TypeA), now); ok {
		t.Error("cleared entry still served")
	}
}

func TestCacheStats(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("stats-test.com", dns.TypeA)
	c.set(key, newTestDNSMsg("stats-test.com", dns.TypeA), 3600)

	now := clock.NowUnixNano()
	for i := 0; i < 3; i++ {
		c.get(key, now)
	}
	for i := 0; i < 2; i++ {
		c.get(testCacheKey("missing.com", dns.TypeA), now)
	}

	stats := c.stats()
	if stats.Hits != 3 {
		t.Errorf("expected 3 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("expected 2 misses, got %d", stats.Misses)
	}
	if stats.HitRate != 60 {
		t.Errorf("expected 60%% hit rate, got %d%%", stats.HitRate)
	}
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}
	if stats.EstimatedBytes <= cacheEntryOverhead {
		t.Errorf("expected accounted bytes > per-entry overhead %d, got %d", cacheEntryOverhead, stats.EstimatedBytes)
	}
	if stats.PeakEntries != 1 {
		t.Errorf("expected peak entries 1, got %d", stats.PeakEntries)
	}
	if stats.PeakBytes < stats.EstimatedBytes {
		t.Errorf("expected peak bytes >= current bytes, got peak=%d current=%d", stats.PeakBytes, stats.EstimatedBytes)
	}
	if stats.MaxBytes != defaultCacheMaxBytes {
		t.Errorf("expected MaxBytes %d, got %d", defaultCacheMaxBytes, stats.MaxBytes)
	}
}

// TestCacheDifferentQueryTypes: A and AAAA are separate entries. (The AAAA
// fixture carries a real AAAA answer: an empty NOERROR without an SOA is a
// NODATA that RFC 2308 says not to cache.)
func TestCacheDifferentQueryTypes(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	c.set(testCacheKey("dual.com", dns.TypeA), newTestDNSMsg("dual.com", dns.TypeA), 3600)

	aaaaMsg := new(dns.Msg)
	aaaaMsg.SetQuestion("dual.com.", dns.TypeAAAA)
	aaaaMsg.Answer = append(aaaaMsg.Answer, &dns.AAAA{
		Hdr:  dns.RR_Header{Name: "dual.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
		AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
	})
	c.set(testCacheKey("dual.com", dns.TypeAAAA), aaaaMsg, 3600)

	if stats := c.stats(); stats.Entries != 2 {
		t.Errorf("expected 2 entries (A + AAAA), got %d", stats.Entries)
	}
	now := clock.NowUnixNano()
	a, ok := c.get(testCacheKey("dual.com", dns.TypeA), now)
	if !ok {
		t.Fatal("expected A record cache hit")
	}
	if got := servedMsg(t, a, now); len(got.Answer) != 1 || got.Answer[0].Header().Rrtype != dns.TypeA {
		t.Errorf("expected the A answer, got %v", got.Answer)
	}
	aaaa, ok := c.get(testCacheKey("dual.com", dns.TypeAAAA), now)
	if !ok {
		t.Fatal("expected AAAA record cache hit")
	}
	if got := servedMsg(t, aaaa, now); len(got.Answer) != 1 || got.Answer[0].Header().Rrtype != dns.TypeAAAA {
		t.Errorf("expected the AAAA answer, got %v", got.Answer)
	}
}

// TestCacheServesIndependentCopies: a served reply is a fresh buffer;
// mutating it must not affect the stored answer or another reply.
func TestCacheServesIndependentCopies(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("noclobber.com", dns.TypeA)
	c.set(key, newTestDNSMsg("noclobber.com", dns.TypeA), 3600)

	e, _ := c.get(key, clock.NowUnixNano())
	req := new(dns.Msg)
	req.SetQuestion("noclobber.com.", dns.TypeA)
	first := e.appendReply(nil, req, e.storedAt)
	for i := range first {
		first[i] = 0xff
	}
	second := servedMsg(t, e, e.storedAt)
	if len(second.Answer) != 1 {
		t.Error("mutating one served reply affected another — the stored wire form is shared")
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		domain := "concurrent" + string(rune('a'+(i%26))) + ".com"

		go func(d string) {
			defer wg.Done()
			c.set(testCacheKey(d, dns.TypeA), newTestDNSMsg(d, dns.TypeA), 3600)
		}(domain)

		go func(d string) {
			defer wg.Done()
			c.get(testCacheKey(d, dns.TypeA), clock.NowUnixNano())
		}(domain)
	}
	wg.Wait()

	if stats := c.stats(); stats.Entries != 26 {
		t.Errorf("expected 26 distinct entries after concurrent access, got %d", stats.Entries)
	}
}

func TestCacheOverwriteUpdatesEstimatedBytesWithoutAddingEntries(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("same.example.com", dns.TypeA)

	small := newTestDNSMsg("same.example.com", dns.TypeA)
	large := newTestDNSMsg("same.example.com", dns.TypeA)
	large.Extra = append(large.Extra, &dns.TXT{
		Hdr: dns.RR_Header{
			Name:   dns.Fqdn("same.example.com"),
			Rrtype: dns.TypeTXT,
			Class:  dns.ClassINET,
			Ttl:    300,
		},
		Txt: []string{"this makes the response larger for accounting"},
	})

	c.set(key, small, 3600)
	before := c.stats()
	c.set(key, large, 3600)
	after := c.stats()

	if after.Entries != 1 {
		t.Fatalf("expected overwrite to keep 1 entry, got %d", after.Entries)
	}
	if after.EstimatedBytes <= before.EstimatedBytes {
		t.Fatalf("expected overwrite to increase estimated bytes, before=%d after=%d", before.EstimatedBytes, after.EstimatedBytes)
	}
}

func TestCacheExpiredMissUpdatesEntryAndExpiryCounts(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("expire.example.com", dns.TypeA)
	c.set(key, newTestDNSMsg("expire.example.com", dns.TypeA), 1)

	later := clock.NowUnixNano() + int64(1100*time.Millisecond)
	if _, ok := c.get(key, later); ok {
		t.Fatal("expected expired cache miss")
	}
	c.sweep(later)

	stats := c.stats()
	if stats.Entries != 0 {
		t.Fatalf("expected 0 entries after expired sweep, got %d", stats.Entries)
	}
	if stats.Expired != 1 {
		t.Fatalf("expected expired counter 1, got %d", stats.Expired)
	}
	if stats.EstimatedBytes != 0 {
		t.Fatalf("expected estimated bytes 0 after expired removal, got %d", stats.EstimatedBytes)
	}
}

// TestCacheStaysWithinByteBudget (D4): inserting far more than the budget
// holds evicts instead of growing.
func TestCacheStaysWithinByteBudget(t *testing.T) {
	const budget = 256 << 10
	c := newDNSCache(budget)
	msg := newTestDNSMsg("seed.example.com", dns.TypeA)
	for i := 0; i < 20000; i++ {
		name := fmt.Sprintf("r%08x.attacker.example.", i)
		msg.Question[0].Name = name
		c.set(testCacheKey(name, dns.TypeA), msg, 3600)
	}
	stats := c.stats()
	if stats.EstimatedBytes > budget {
		t.Errorf("accounted bytes %d exceed the %d budget", stats.EstimatedBytes, budget)
	}
	if stats.Entries == 0 || stats.Entries >= 20000 {
		t.Errorf("expected a bounded, non-empty cache, got %d entries", stats.Entries)
	}
	if stats.Evicted != int64(20000-stats.Entries) {
		t.Errorf("evicted %d, want %d (inserted - resident)", stats.Evicted, 20000-stats.Entries)
	}
	// Every resident entry is reachable and the map holds nothing else.
	mapped := 0
	c.entries.Range(func(_, _ any) bool { mapped++; return true })
	if mapped != stats.Entries {
		t.Errorf("map holds %d entries but the rings account for %d", mapped, stats.Entries)
	}
}

// TestCacheKeepsHotEntryUnderFlood: CLOCK second chance keeps a name that is
// read between inserts alive through a flood of one-shot names.
func TestCacheKeepsHotEntryUnderFlood(t *testing.T) {
	c := newDNSCache(256 << 10)
	hot := testCacheKey("www.bank.example", dns.TypeA)
	c.set(hot, newTestDNSMsg("www.bank.example", dns.TypeA), 3600)
	msg := newTestDNSMsg("seed.example.com", dns.TypeA)
	for i := 0; i < 50000; i++ {
		name := fmt.Sprintf("r%08x.attacker.example.", i)
		msg.Question[0].Name = name
		c.set(testCacheKey(name, dns.TypeA), msg, 3600)
		if i%100 == 0 {
			if _, ok := c.get(hot, clock.NowUnixNano()); !ok {
				t.Fatalf("hot entry evicted after %d flood inserts", i)
			}
		}
	}
}

// TestCacheSetRejectsUnservableAnswers (D2, D3): transient rcodes, TC=1 and
// answers to a different question are never stored or served from cache.
func TestCacheSetRejectsUnservableAnswers(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)
	key := testCacheKey("login.victimbank.example", dns.TypeA)

	servfail := newTestDNSMsg("login.victimbank.example", dns.TypeA)
	servfail.Rcode = dns.RcodeServerFailure
	refused := newTestDNSMsg("login.victimbank.example", dns.TypeA)
	refused.Rcode = dns.RcodeRefused
	truncated := newTestDNSMsg("login.victimbank.example", dns.TypeA)
	truncated.Truncated = true
	otherQuestion := newTestDNSMsg("unrelated.other", dns.TypeA)
	otherType := newTestDNSMsg("login.victimbank.example", dns.TypeA)
	otherType.Question[0].Qtype = dns.TypeAAAA

	for name, m := range map[string]*dns.Msg{
		"SERVFAIL": servfail, "REFUSED": refused, "TC=1": truncated,
		"other question": otherQuestion, "other qtype": otherType,
	} {
		if e := c.set(key, m, 3600); e != nil {
			t.Errorf("%s: set returned a servable answer", name)
		}
	}
	if stats := c.stats(); stats.Entries != 0 {
		t.Errorf("expected nothing cached, got %d entries", stats.Entries)
	}

	// The upstream may echo 0x20 case; that is still the same question.
	mixed := newTestDNSMsg("LOGIN.VictimBank.example", dns.TypeA)
	if e := c.set(key, mixed, 3600); e == nil {
		t.Errorf("a case-variant echo of the question must be accepted")
	}
}

// TestAppendReplyPatchesPerClientFields: the served bytes carry the client's
// ID, RD/CD bits, 0x20 spelling and EDNS, and TTLs that count down from the
// capped stored values.
func TestAppendReplyPatchesPerClientFields(t *testing.T) {
	resp := new(dns.Msg)
	resp.SetQuestion("www.example.com.", dns.TypeA)
	resp.Response = true
	resp.RecursionAvailable = true
	resp.AuthenticatedData = true
	resp.Answer = []dns.RR{
		&dns.CNAME{Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 3600}, Target: "edge.example.net."},
		&dns.A{Hdr: dns.RR_Header{Name: "edge.example.net.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("192.0.2.80")},
	}
	resp.SetEdns0(4096, true) // upstream OPT is dropped from the stored form
	key := dnsCacheKey{name: "www.example.com.", qtype: dns.TypeA, do: true}
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC).UnixNano()
	e, err := newCachedAnswer(key, resp, 120, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := []responseTarget{{name: "edge.example.net", rrtype: dns.TypeCNAME}}; fmt.Sprint(e.targets) != fmt.Sprint(want) {
		t.Errorf("targets = %v, want %v", e.targets, want)
	}
	if want := []string{"192.0.2.80"}; fmt.Sprint(e.ips) != fmt.Sprint(want) {
		t.Errorf("ips = %v, want %v", e.ips, want)
	}

	req := new(dns.Msg)
	req.SetQuestion("WwW.ExAmple.COM.", dns.TypeA)
	req.Id = 0xBEEF
	req.RecursionDesired = true
	req.CheckingDisabled = true
	req.SetEdns0(1232, true)
	got := new(dns.Msg)
	if err := got.Unpack(e.appendReply(nil, req, now+int64(10*time.Second))); err != nil {
		t.Fatal(err)
	}
	if got.Id != 0xBEEF || !got.Response || !got.RecursionDesired || !got.RecursionAvailable || !got.CheckingDisabled || !got.AuthenticatedData {
		t.Errorf("header = %+v", got.MsgHdr)
	}
	if got.Question[0].Name != "WwW.ExAmple.COM." {
		t.Errorf("question = %q, want the client's spelling", got.Question[0].Name)
	}
	if ttl := got.Answer[0].Header().Ttl; ttl != 110 {
		t.Errorf("CNAME TTL = %d, want 110 (3600 capped to the 120 s lifetime, minus 10 s)", ttl)
	}
	if ttl := got.Answer[1].Header().Ttl; ttl != 50 {
		t.Errorf("A TTL = %d, want 50", ttl)
	}
	opt := got.IsEdns0()
	if opt == nil || opt.UDPSize() != ednsAdvertisedSize || !opt.Do() || len(got.Extra) != 1 {
		t.Errorf("want exactly our OPT (1232, DO=1), got extra=%v", got.Extra)
	}

	// A plain client: no OPT, no AD (it asked for neither), TTLs floor at 0.
	plain := new(dns.Msg)
	plain.SetQuestion("www.example.com.", dns.TypeA)
	plain.RecursionDesired = false
	got = new(dns.Msg)
	if err := got.Unpack(e.appendReply(nil, plain, now+int64(500*time.Second))); err != nil {
		t.Fatal(err)
	}
	if got.IsEdns0() != nil || got.AuthenticatedData || got.RecursionDesired || got.CheckingDisabled {
		t.Errorf("plain client header/extra = %+v %v", got.MsgHdr, got.Extra)
	}
	if got.Answer[0].Header().Ttl != 0 || got.Answer[1].Header().Ttl != 0 {
		t.Errorf("TTLs past expiry must floor at 0, got %d/%d", got.Answer[0].Header().Ttl, got.Answer[1].Header().Ttl)
	}
}

func TestTTLOffsetsRejectsMalformedWire(t *testing.T) {
	for name, wire := range map[string][]byte{
		"short header":   {0, 1, 2},
		"truncated name": {0, 0, 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x'},
		"bad label type": {0, 0, 0x81, 0x80, 0, 1, 0, 0, 0, 0, 0, 0, 0x40, 0},
		"rr past end":    {0, 0, 0x81, 0x80, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 1, 0, 1},
	} {
		if _, err := ttlOffsets(wire); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLowerASCII(t *testing.T) {
	for in, want := range map[string]string{
		"www.example.com.":  "www.example.com.",
		"WWW.Example.COM.":  "www.example.com.",
		`a\046B.example.`:   `a\046b.example.`,
		"xn--bcher-KVA.de.": "xn--bcher-kva.de.",
	} {
		if got := lowerASCII(in); got != want {
			t.Errorf("lowerASCII(%q) = %q, want %q", in, got, want)
		}
	}
	s := "already.lower.example."
	if allocs := testing.AllocsPerRun(100, func() { _ = lowerASCII(s) }); allocs != 0 {
		t.Errorf("lowercase input allocated %.0f times", allocs)
	}
}

// --- effectiveCacheTTL tests ---
//
// Before A5b, cache.set always cached for the fixed configured cacheTTL
// regardless of what the upstream answer actually said, so fast-failover/
// CDN/GSLB answers with short TTLs (and negative NXDOMAIN/NODATA answers
// with a short SOA negative-cache TTL, RFC 2308) got over-cached — serving
// stale results long after the upstream intended them to expire.

func msgWithAnswerTTL(ttl uint32) *dns.Msg {
	msg := new(dns.Msg)
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
		A:   []byte{93, 184, 216, 34},
	})
	return msg
}

func msgWithMultipleAnswerTTLs(ttls ...uint32) *dns.Msg {
	msg := new(dns.Msg)
	for _, ttl := range ttls {
		msg.Answer = append(msg.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
			A:   []byte{93, 184, 216, 34},
		})
	}
	return msg
}

func msgNegativeWithSOA(minttl uint32) *dns.Msg {
	msg := new(dns.Msg)
	msg.Rcode = dns.RcodeNameError
	msg.Ns = append(msg.Ns, &dns.SOA{
		Hdr:    dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: minttl},
		Ns:     "ns.example.com.",
		Mbox:   "admin.example.com.",
		Minttl: minttl,
	})
	return msg
}

func TestEffectiveCacheTTL_UsesSmallerAnswerTTL(t *testing.T) {
	// CDN/fast-failover answer: 30s TTL, but configured cap is 3600s — must
	// respect the smaller, upstream-intended TTL.
	got := effectiveCacheTTL(msgWithAnswerTTL(30), 3600)
	if got != 30 {
		t.Errorf("expected effective TTL 30 (answer TTL), got %d", got)
	}
}

func TestEffectiveCacheTTL_CapsAtConfiguredMax(t *testing.T) {
	// Answer TTL larger than the configured cap — never cache longer than
	// the admin's configured maximum.
	got := effectiveCacheTTL(msgWithAnswerTTL(86400), 3600)
	if got != 3600 {
		t.Errorf("expected effective TTL capped at 3600, got %d", got)
	}
}

func TestEffectiveCacheTTL_UsesSmallestAmongMultipleAnswers(t *testing.T) {
	got := effectiveCacheTTL(msgWithMultipleAnswerTTLs(300, 60, 120), 3600)
	if got != 60 {
		t.Errorf("expected effective TTL 60 (smallest among answers), got %d", got)
	}
}

func TestEffectiveCacheTTL_NegativeResponseUsesSOAMinttl(t *testing.T) {
	// NXDOMAIN with a short SOA negative-cache TTL — must not be cached for
	// the full configured duration.
	got := effectiveCacheTTL(msgNegativeWithSOA(45), 3600)
	if got != 45 {
		t.Errorf("expected effective TTL 45 (SOA minttl), got %d", got)
	}
}

// TestEffectiveCacheTTL_NegativeTTLIsMinOfSOATTLAndMinimum (RFC 2308
// section 5) and is capped at maxNegativeCacheTTLSeconds.
func TestEffectiveCacheTTL_NegativeTTLIsMinOfSOATTLAndMinimum(t *testing.T) {
	m := msgNegativeWithSOA(900)
	m.Ns[0].Header().Ttl = 120
	if got := effectiveCacheTTL(m, 3600); got != 120 {
		t.Errorf("SOA TTL 120, MINIMUM 900: expected 120, got %d", got)
	}
	if got := effectiveCacheTTL(msgNegativeWithSOA(86400), 3600); got != maxNegativeCacheTTLSeconds {
		t.Errorf("SOA 86400: expected the %d s negative cap, got %d", maxNegativeCacheTTLSeconds, got)
	}
}

// TestEffectiveCacheTTL_NegativeResponseWithoutSOAIsNotCached replaces the
// old "falls back to the configured TTL" behavior: a NODATA/NXDOMAIN without
// an SOA gives no negative TTL, and RFC 2308 section 5 says such answers
// SHOULD NOT be cached (the old code held them for the full hour).
func TestEffectiveCacheTTL_NegativeResponseWithoutSOAIsNotCached(t *testing.T) {
	if got := effectiveCacheTTL(new(dns.Msg), 3600); got != 0 {
		t.Errorf("expected 0 (not cached) with no SOA, got %d", got)
	}
}

func TestEffectiveCacheTTL_TransientAnswersAreNotCached(t *testing.T) {
	for name, m := range map[string]*dns.Msg{
		"SERVFAIL": {MsgHdr: dns.MsgHdr{Rcode: dns.RcodeServerFailure}},
		"REFUSED":  {MsgHdr: dns.MsgHdr{Rcode: dns.RcodeRefused}},
		"TC=1":     {MsgHdr: dns.MsgHdr{Truncated: true}, Answer: msgWithAnswerTTL(300).Answer},
	} {
		if got := effectiveCacheTTL(m, 3600); got != 0 {
			t.Errorf("%s: expected 0, got %d", name, got)
		}
	}
}

func TestEffectiveCacheTTL_FloorsNearZeroAnswerTTL(t *testing.T) {
	// A near-zero answer TTL (some fast-failover setups do this) must not
	// thrash the cache on every single query — floored at minCacheTTLSeconds.
	got := effectiveCacheTTL(msgWithAnswerTTL(0), 3600)
	if got != minCacheTTLSeconds {
		t.Errorf("expected floor of %d for near-zero answer TTL, got %d", minCacheTTLSeconds, got)
	}
}

func TestEffectiveCacheTTL_FloorNeverExceedsExplicitLowConfiguredCap(t *testing.T) {
	// An operator who deliberately configures a cacheTTL below the floor
	// (e.g. 1s, for testing) must NOT be overridden upward by the floor.
	got := effectiveCacheTTL(msgWithAnswerTTL(300), 1)
	if got != 1 {
		t.Errorf("expected the explicit low configured cap (1) to win over the floor, got %d", got)
	}
}

func TestCacheSet_RespectsAnswerTTLNotJustConfiguredCap(t *testing.T) {
	c := newDNSCache(defaultCacheMaxBytes)

	// Configured cap is huge (3600s), but the answer says a TTL just above
	// the thrash-prevention floor (minCacheTTLSeconds) — the entry must
	// expire around that TTL, not the 3600s configured cap. (An answer TTL
	// below the floor would get floored — see
	// TestEffectiveCacheTTL_FloorsNearZeroAnswerTTL for that case — so this
	// uses a TTL just above it to isolate the "respects the smaller answer
	// TTL" behavior from the floor.)
	shortTTL := uint32(minCacheTTLSeconds + 1)
	msg := msgWithAnswerTTL(shortTTL)
	msg.SetQuestion("shortlived-answer.com.", dns.TypeA)
	key := testCacheKey("shortlived-answer.com", dns.TypeA)
	c.set(key, msg, 3600)

	now := clock.NowUnixNano()
	if _, ok := c.get(key, now); !ok {
		t.Fatal("expected cache hit immediately after set")
	}
	if _, ok := c.get(key, now+int64(time.Duration(shortTTL)*time.Second+300*time.Millisecond)); ok {
		t.Errorf("expected cache entry to have expired around the answer's own TTL (%ds), not the 3600s configured cap", shortTTL)
	}
}
