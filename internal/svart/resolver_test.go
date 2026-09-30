package svart

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestParseUpstreamAddress(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		expectedProtocol string
		expectedAddress  string
	}{
		{
			name:             "bare IP with port",
			input:            "9.9.9.9:53",
			expectedProtocol: "udp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "bare IP without port",
			input:            "9.9.9.9",
			expectedProtocol: "udp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "udp explicit",
			input:            "udp://9.9.9.9:53",
			expectedProtocol: "udp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "tcp explicit",
			input:            "tcp://9.9.9.9:53",
			expectedProtocol: "tcp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "tls with hostname",
			input:            "tls://dns.quad9.net",
			expectedProtocol: "tls",
			expectedAddress:  "dns.quad9.net",
		},
		{
			name:             "tls with hostname and port",
			input:            "tls://dns.quad9.net:853",
			expectedProtocol: "tls",
			expectedAddress:  "dns.quad9.net:853",
		},
		{
			name:             "https DoH",
			input:            "https://dns.mullvad.net/dns-query",
			expectedProtocol: "https",
			expectedAddress:  "dns.mullvad.net/dns-query",
		},
		{
			name:             "https cloudflare",
			input:            "https://cloudflare-dns.com/dns-query",
			expectedProtocol: "https",
			expectedAddress:  "cloudflare-dns.com/dns-query",
		},
		{
			name:             "comment line",
			input:            "# this is a comment",
			expectedProtocol: "",
			expectedAddress:  "",
		},
		{
			name:             "domain-specific prefix",
			input:            "[/example.com/]9.9.9.9:53",
			expectedProtocol: "udp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "whitespace padding",
			input:            "  9.9.9.9:53  ",
			expectedProtocol: "udp",
			expectedAddress:  "9.9.9.9:53",
		},
		{
			name:             "cloudflare 1.1.1.1",
			input:            "1.1.1.1:53",
			expectedProtocol: "udp",
			expectedAddress:  "1.1.1.1:53",
		},
		{
			name:             "mullvad DoH full URL",
			input:            "https://doh.mullvad.net/dns-query",
			expectedProtocol: "https",
			expectedAddress:  "doh.mullvad.net/dns-query",
		},
		{
			name:             "bare IPv6 without port",
			input:            "2620:fe::fe",
			expectedProtocol: "udp",
			expectedAddress:  "[2620:fe::fe]:53",
		},
		{
			name:             "bracketed IPv6 with port",
			input:            "[2620:fe::fe]:53",
			expectedProtocol: "udp",
			expectedAddress:  "[2620:fe::fe]:53",
		},
		{
			name:             "explicit udp without port",
			input:            "udp://1.1.1.1",
			expectedProtocol: "udp",
			expectedAddress:  "1.1.1.1:53",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			protocol, address := parseUpstreamAddress(tt.input)
			if protocol != tt.expectedProtocol {
				t.Errorf("parseUpstreamAddress(%q) protocol = %q, want %q", tt.input, protocol, tt.expectedProtocol)
			}
			if address != tt.expectedAddress {
				t.Errorf("parseUpstreamAddress(%q) address = %q, want %q", tt.input, address, tt.expectedAddress)
			}
		})
	}
}

func TestCalculateWeight(t *testing.T) {
	// Clear any existing stats
	upstreamStatsMap = sync.Map{}

	// Unknown upstream should get weight 1.0
	w := calculateWeight("unknown.server:53")
	if w != 1.0 {
		t.Errorf("expected weight 1.0 for unknown upstream, got %f", w)
	}

	// Record some successes — should keep weight near 1.0
	for i := 0; i < 10; i++ {
		recordSuccess("fast.server:53", 15) // 15ms average
	}
	w = calculateWeight("fast.server:53")
	if w < 0.5 {
		t.Errorf("expected high weight for fast server with no failures, got %f", w)
	}

	// Record failures — weight should drop
	for i := 0; i < 10; i++ {
		recordFailure("flaky.server:53")
	}
	for i := 0; i < 10; i++ {
		recordSuccess("flaky.server:53", 200) // also slow
	}
	w = calculateWeight("flaky.server:53")
	// With 50% failure rate and 200ms avg: 1/(1 + 0.5*10 + 200/1000) ≈ 0.16
	// Should be significantly lower than the fast server
	fastWeight := calculateWeight("fast.server:53")
	if w >= fastWeight {
		t.Errorf("expected flaky server weight (%f) to be lower than fast server (%f)", w, fastWeight)
	}

	// Weight should never go below 0.01
	for i := 0; i < 100; i++ {
		recordFailure("dead.server:53")
	}
	w = calculateWeight("dead.server:53")
	if w < 0.01 {
		t.Errorf("expected minimum weight 0.01, got %f", w)
	}
}

func TestWeightedRandomSelectDistribution(t *testing.T) {
	upstreams := []Upstream{
		{ID: 1, Upstream: "heavy.server:53", Enabled: true},
		{ID: 2, Upstream: "light.server:53", Enabled: true},
	}

	// Heavily skewed weights: 0.99 vs 0.01
	weights := []float64{0.99, 0.01}
	totalWeight := 1.0

	heavyCount := 0
	trials := 1000
	for i := 0; i < trials; i++ {
		selected := weightedRandomSelect(upstreams, weights, totalWeight)
		if selected.ID == 1 {
			heavyCount++
		}
	}

	// With 99% weight, we'd expect ~990 selections. Allow wide margin
	// because the RNG uses time-based seeding which isn't perfectly distributed
	if heavyCount < 800 {
		t.Errorf("expected heavy server to be selected >800 times out of %d, got %d", trials, heavyCount)
	}
}

func TestRecordSuccessAndFailure(t *testing.T) {
	upstreamStatsMap = sync.Map{}

	recordSuccess("test.server:53", 25)
	recordSuccess("test.server:53", 35)
	recordFailure("test.server:53")

	val, ok := upstreamStatsMap.Load("test.server:53")
	if !ok {
		t.Fatal("expected stats to be recorded")
	}

	stats := requireFixtureType[*upstreamStats](t, val)
	if got := stats.queryCount.Load(); got != 3 {
		t.Errorf("expected 3 queries, got %d", got)
	}
	if got := stats.failures.Load(); got != 1 {
		t.Errorf("expected 1 failure, got %d", got)
	}
	if got := stats.totalTime.Load(); got != 60 {
		t.Errorf("expected total time 60ms, got %d", got)
	}
}

// TestUpstreamStatsConcurrentReadWriteIsRaceFree is the A5(a) regression
// test. upstreamStats fields were plain int64 written via atomic.AddInt64 in
// recordSuccess/recordFailure but read as bare fields (no atomic load) in
// calculateWeight — a genuine data race under `go test -race`, and not
// guaranteed by the memory model to observe the latest value even when the
// race detector is off. This hammers recordSuccess/recordFailure and
// calculateWeight concurrently for the same upstream; it must be clean
// under -race.
func TestUpstreamStatsConcurrentReadWriteIsRaceFree(t *testing.T) {
	upstreamStatsMap = sync.Map{}
	const upstream = "race.server:53"

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				if (id+i)%5 == 0 {
					recordFailure(upstream)
				} else {
					recordSuccess(upstream, int64(10+i%50))
				}
				calculateWeight(upstream)
			}
		}(g)
	}
	wg.Wait()

	val, ok := upstreamStatsMap.Load(upstream)
	if !ok {
		t.Fatal("expected stats to be recorded")
	}
	stats := requireFixtureType[*upstreamStats](t, val)
	if stats.queryCount.Load() != 8*500 {
		t.Errorf("expected %d total queries, got %d", 8*500, stats.queryCount.Load())
	}
}

// clientQuery builds a client request the way a stub resolver or an attacker
// would: arbitrary ID, arbitrary casing, optional EDNS with options.
func clientQuery(name string, qtype uint16, id uint16, edns bool, do bool, opts ...dns.EDNS0) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(name, qtype)
	m.Id = id
	if edns {
		m.SetEdns0(4096, do)
		opt := m.IsEdns0()
		opt.Option = append(opt.Option, opts...)
	}
	return m
}

func clientSubnetOption() *dns.EDNS0_SUBNET {
	return &dns.EDNS0_SUBNET{
		Code:          dns.EDNS0SUBNET,
		Family:        1,
		SourceNetmask: 24,
		Address:       net.ParseIP("203.0.113.0").To4(),
	}
}

// TestQueryUpstreamBuildsFreshMessage is the D2 regression test for message
// construction. svart used to forward the client's own message: its ID (an
// attacker-chosen ID removes 16 bits of spoofing entropy), its casing, its
// EDNS options (ECS leaks the client subnet to the upstream) and its UDP size.
// The upstream must instead see a message svart built itself.
func TestQueryUpstreamBuildsFreshMessage(t *testing.T) {
	defer setupTestDB(t)()

	tests := []struct {
		name   string
		req    func() *dns.Msg
		wantDO bool
		wantCD bool
		wantAD bool
		wantQ  dns.Question
	}{
		{
			name: "EDNS client with DO, CD, ECS and cookie",
			req: func() *dns.Msg {
				m := clientQuery("Login.VictimBank.Example.", dns.TypeA, 0x1337, true, true,
					clientSubnetOption(),
					&dns.EDNS0_COOKIE{Code: dns.EDNS0COOKIE, Cookie: "24a5ac1223ad1c6f"})
				m.CheckingDisabled = true
				return m
			},
			wantDO: true,
			wantCD: true,
			wantQ:  dns.Question{Name: "login.victimbank.example.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
		},
		{
			name: "plain client without EDNS and RD=0",
			req: func() *dns.Msg {
				m := clientQuery("WWW.EXAMPLE.COM.", dns.TypeAAAA, 0x1337, false, false)
				m.RecursionDesired = false
				return m
			},
			wantQ: dns.Question{Name: "www.example.com.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
		},
		{
			name: "EDNS client without DO but with AD (trust-ad stub)",
			req: func() *dns.Msg {
				m := clientQuery("mail.example.org.", dns.TypeMX, 0x1337, true, false)
				m.AuthenticatedData = true
				return m
			},
			wantAD: true,
			wantQ:  dns.Question{Name: "mail.example.org.", Qtype: dns.TypeMX, Qclass: dns.ClassINET},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := startUpstreamStub(t, "udp", answerA("93.184.216.34"))
			const rounds = 8
			for i := 0; i < rounds; i++ {
				if _, err := queryUpstream(context.Background(), tt.req(), stub.addr); err != nil {
					t.Fatalf("queryUpstream: %v", err)
				}
			}

			seen := stub.queries()
			if len(seen) != rounds {
				t.Fatalf("upstream received %d queries, want %d", len(seen), rounds)
			}
			ids := make(map[uint16]bool)
			for _, got := range seen {
				ids[got.Id] = true
				if len(got.Question) != 1 || got.Question[0] != tt.wantQ {
					t.Fatalf("upstream question = %v, want exactly %v", got.Question, tt.wantQ)
				}
				if !got.RecursionDesired {
					t.Errorf("upstream RD = false, want true")
				}
				if got.CheckingDisabled != tt.wantCD {
					t.Errorf("upstream CD = %v, want %v", got.CheckingDisabled, tt.wantCD)
				}
				if got.AuthenticatedData != tt.wantAD {
					t.Errorf("upstream AD = %v, want %v", got.AuthenticatedData, tt.wantAD)
				}
				opt := got.IsEdns0()
				if opt == nil {
					t.Fatalf("upstream query has no OPT record; want svart's own EDNS0")
				}
				if opt.UDPSize() != 1232 {
					t.Errorf("upstream EDNS UDP size = %d, want 1232", opt.UDPSize())
				}
				if opt.Do() != tt.wantDO {
					t.Errorf("upstream DO = %v, want %v", opt.Do(), tt.wantDO)
				}
				if len(opt.Option) != 0 {
					t.Errorf("upstream OPT carries client options %v; ECS/cookies must not be forwarded", opt.Option)
				}
				if len(got.Extra) != 1 {
					t.Errorf("upstream additional section = %v, want only svart's OPT", got.Extra)
				}
			}
			// The client always sends 0x1337. A fresh random 16-bit ID per
			// exchange repeats across all 8 exchanges with probability 2^-112.
			if len(ids) == 1 {
				t.Errorf("all %d upstream exchanges used ID %#04x; want a fresh random ID per exchange", rounds, seen[0].Id)
			}
		})
	}
}

// TestQueryUpstreamMapsReplyOntoClientRequest pins the reply the client sees:
// its own ID, its own question casing, and no upstream EDNS options.
func TestQueryUpstreamMapsReplyOntoClientRequest(t *testing.T) {
	defer setupTestDB(t)()

	stub := startUpstreamStub(t, "udp", func(r *dns.Msg) *dns.Msg {
		m := answerA("93.184.216.34")(r)
		m.SetEdns0(4096, false)
		opt := m.IsEdns0()
		opt.Option = append(opt.Option, &dns.EDNS0_NSID{Code: dns.EDNS0NSID, Nsid: "7265736f6c7665722d3031"})
		return m
	})

	tests := []struct {
		name   string
		req    *dns.Msg
		wantDO bool
	}{
		{name: "plain client", req: clientQuery("WwW.ExAmPlE.CoM.", dns.TypeA, 0xbeef, false, false)},
		{name: "EDNS client with DO", req: clientQuery("WwW.ExAmPlE.CoM.", dns.TypeA, 0x0042, true, true, clientSubnetOption()), wantDO: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := queryUpstream(context.Background(), tt.req, stub.addr)
			if err != nil {
				t.Fatalf("queryUpstream: %v", err)
			}
			if resp.Id != tt.req.Id {
				t.Errorf("reply ID = %#04x, want client ID %#04x", resp.Id, tt.req.Id)
			}
			want := dns.Question{Name: "WwW.ExAmPlE.CoM.", Qtype: dns.TypeA, Qclass: dns.ClassINET}
			if len(resp.Question) != 1 || resp.Question[0] != want {
				t.Errorf("reply question = %v, want client's exact question %v", resp.Question, want)
			}
			if !resp.Response || resp.Rcode != dns.RcodeSuccess {
				t.Errorf("reply QR=%v rcode=%s, want QR=true NOERROR", resp.Response, dns.RcodeToString[resp.Rcode])
			}
			if len(resp.Answer) != 1 || requireFixtureType[*dns.A](t, resp.Answer[0]).A.String() != "93.184.216.34" {
				t.Errorf("reply answer = %v, want one A 93.184.216.34", resp.Answer)
			}
			opt := resp.IsEdns0()
			if tt.req.IsEdns0() == nil {
				if opt != nil {
					t.Errorf("non-EDNS client got an OPT record %v", opt)
				}
				return
			}
			if opt == nil {
				t.Fatalf("EDNS client got no OPT record")
			}
			if opt.Do() != tt.wantDO {
				t.Errorf("reply DO = %v, want %v", opt.Do(), tt.wantDO)
			}
			if len(opt.Option) != 0 {
				t.Errorf("reply OPT carries upstream options %v; they describe the upstream hop, not ours", opt.Option)
			}
		})
	}
}

// forgedA answers with an A record for the asked name, then lets mutate
// break one property of the reply the way a spoofer or broken upstream would.
func forgedA(mutate func(m *dns.Msg)) func(*dns.Msg) *dns.Msg {
	return func(r *dns.Msg) *dns.Msg {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Answer = append(m.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 3600},
			A:   net.ParseIP("6.6.6.6"),
		})
		mutate(m)
		return m
	}
}

// TestQueryUpstreamRejectsMismatchedReplies is the D2 regression test for
// reply validation. A reply for "unrelated.other." used to be accepted (and
// then cached) as the answer for "login.victimbank.example.".
func TestQueryUpstreamRejectsMismatchedReplies(t *testing.T) {
	defer setupTestDB(t)()

	tests := []struct {
		name   string
		mutate func(m *dns.Msg)
	}{
		{"different question name", func(m *dns.Msg) {
			m.Question = []dns.Question{{Name: "unrelated.other.", Qtype: dns.TypeA, Qclass: dns.ClassINET}}
		}},
		{"different question type", func(m *dns.Msg) { m.Question[0].Qtype = dns.TypeAAAA }},
		{"different question class", func(m *dns.Msg) { m.Question[0].Qclass = dns.ClassCHAOS }},
		{"no question", func(m *dns.Msg) { m.Question = nil }},
		{"two questions", func(m *dns.Msg) {
			m.Question = append(m.Question, dns.Question{Name: "unrelated.other.", Qtype: dns.TypeA, Qclass: dns.ClassINET})
		}},
		{"QR bit clear", func(m *dns.Msg) { m.Response = false }},
		{"opcode NOTIFY", func(m *dns.Msg) { m.Opcode = dns.OpcodeNotify }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := startUpstreamStub(t, "udp", forgedA(tt.mutate))
			mismatches := dnsUpstreamMismatchedReplies.WithLabelValues(stub.addr)
			before := counterValue(t, mismatches)
			req := clientQuery("login.victimbank.example.", dns.TypeA, 0x1337, false, false)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resp, err := queryUpstream(ctx, req, stub.addr)
			if !errors.Is(err, errUpstreamReplyMismatch) {
				t.Fatalf("queryUpstream error = %v, want errUpstreamReplyMismatch (reply %v)", err, resp)
			}
			if resp != nil {
				t.Errorf("queryUpstream returned a message alongside error %v", err)
			}
			if got := counterValue(t, mismatches) - before; got != 1 {
				t.Errorf("svart_dns_upstream_mismatched_replies_total{upstream=%q} rose by %v, want 1", stub.addr, got)
			}
		})
	}
}

// TestResolveQueryDoesNotCacheMismatchedReply is the end-to-end form of the
// D2 PoC: the forged answer must never be served, now or on the next query.
func TestResolveQueryDoesNotCacheMismatchedReply(t *testing.T) {
	defer setupTestDB(t)()

	stub := startUpstreamStub(t, "udp", forgedA(func(m *dns.Msg) {
		m.Question = []dns.Question{{Name: "unrelated.other.", Qtype: dns.TypeA, Qclass: dns.ClassINET}}
		m.Answer[0].Header().Name = "unrelated.other."
	}))
	useUpstreams(t, stub.addr)

	for i := 0; i < 2; i++ {
		req := clientQuery("login.victimbank.example.", dns.TypeA, uint16(0x1337+i), false, false)
		resp, _, err := resolveQuery(context.Background(), req)
		if err == nil {
			t.Fatalf("round %d: resolveQuery served a forged answer: %v", i, resp.Answer)
		}
	}
	if got := len(stub.queries()); got != 2 {
		t.Errorf("upstream saw %d queries, want 2 (the forged reply must not be cached)", got)
	}
}

// TestResolveQueryNonINClassNotForwarded covers the forwarding half of the
// CHAOS-class PoC: svart only forwards class IN (the question it builds is
// always IN), so a CH query must fail locally instead of reaching the
// upstream and having the upstream's REFUSED stored under the IN name.
func TestResolveQueryNonINClassNotForwarded(t *testing.T) {
	defer setupTestDB(t)()

	stub := startUpstreamStub(t, "udp", func(r *dns.Msg) *dns.Msg {
		if r.Question[0].Qclass != dns.ClassINET {
			m := new(dns.Msg)
			m.SetRcode(r, dns.RcodeRefused)
			return m
		}
		return answerA("93.184.216.34")(r)
	})
	useUpstreams(t, stub.addr)

	chaos := clientQuery("www.bank.example.", dns.TypeA, 0x0101, false, false)
	chaos.Question[0].Qclass = dns.ClassCHAOS
	if resp, _, err := resolveQuery(context.Background(), chaos); !errors.Is(err, errQueryNotForwardable) {
		t.Fatalf("CH-class query: err=%v resp=%v, want errQueryNotForwardable", err, resp)
	}
	if val, ok := upstreamStatsMap.Load(stub.addr); ok && requireFixtureType[*upstreamStats](t, val).failures.Load() != 0 {
		t.Errorf("a query svart refuses to forward was counted as an upstream failure")
	}

	victim := clientQuery("www.bank.example.", dns.TypeA, 0x0202, false, false)
	resp, _, err := resolveQuery(context.Background(), victim)
	if err != nil {
		t.Fatalf("IN query after CH query: %v", err)
	}
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
		t.Fatalf("IN query after CH query: rcode=%s answers=%v, want NOERROR with one A", dns.RcodeToString[resp.Rcode], resp.Answer)
	}
	for _, q := range stub.queries() {
		if q.Question[0].Qclass != dns.ClassINET {
			t.Errorf("upstream received a non-IN query %v", q.Question[0])
		}
	}
}

// TestQueryUpstreamRetriesTruncatedUDPOverTCP: a TC=1 UDP reply is not an
// answer (RFC 7766); svart must retry the same upstream over TCP instead of
// handing (and caching) the truncated message.
func TestQueryUpstreamRetriesTruncatedUDPOverTCP(t *testing.T) {
	defer setupTestDB(t)()

	txt := func(r *dns.Msg) *dns.Msg {
		m := new(dns.Msg)
		m.SetReply(r)
		for i := 0; i < 3; i++ {
			m.Answer = append(m.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
				Txt: []string{"v=spf1 include:_spf.example.com ~all " + strings.Repeat("x", 200)},
			})
		}
		return m
	}
	truncated := func(r *dns.Msg) *dns.Msg {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Truncated = true
		return m
	}
	udp, tcp := startUpstreamStubPair(t, truncated, txt)

	req := clientQuery("bigtxt.example.", dns.TypeTXT, 0x2222, false, false)
	resp, err := queryUpstream(context.Background(), req, udp.addr)
	if err != nil {
		t.Fatalf("queryUpstream: %v", err)
	}
	if resp.Truncated {
		t.Errorf("reply still has TC=1; want the full TCP answer")
	}
	if len(resp.Answer) != 3 {
		t.Errorf("reply has %d answers, want 3 from the TCP retry", len(resp.Answer))
	}
	if got := len(udp.queries()); got != 1 {
		t.Errorf("UDP stub saw %d queries, want 1", got)
	}
	if got := len(tcp.queries()); got != 1 {
		t.Errorf("TCP stub saw %d queries, want 1 (the truncation retry)", got)
	}
}
