package main

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDNSGuardConfigDefaults(t *testing.T) {
	cfg, err := loadDNSGuardConfig(envMap(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.allowedClients, defaultAllowedClients) {
		t.Errorf("default ACL = %v, want %v", cfg.allowedClients, defaultAllowedClients)
	}
	if cfg.rateLimit != nil {
		t.Errorf("rate limiting must be off by default")
	}
	if cfg.tcpMaxConns != 1000 || cfg.tcpMaxConnsPerIP != 32 {
		t.Errorf("TCP caps = %d/%d, want 1000/32", cfg.tcpMaxConns, cfg.tcpMaxConnsPerIP)
	}
}

func TestLoadDNSGuardConfigOverrides(t *testing.T) {
	cfg, err := loadDNSGuardConfig(envMap(map[string]string{
		"ALLOWED_CLIENTS":          " 10.42.0.0/16, 2001:db8:42::/48 ,198.51.100.7, ::ffff:192.0.2.0/120",
		"DNS_RATE_LIMIT_QPS":       "50",
		"DNS_TCP_MAX_CONNS":        "200",
		"DNS_TCP_MAX_CONNS_PER_IP": "4",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.42.0.0/16"),
		netip.MustParsePrefix("2001:db8:42::/48"),
		netip.MustParsePrefix("198.51.100.7/32"),
		netip.MustParsePrefix("192.0.2.0/24"),
	}
	if !reflect.DeepEqual(cfg.allowedClients, want) {
		t.Errorf("ACL = %v, want %v", cfg.allowedClients, want)
	}
	if cfg.rateLimit == nil || cfg.rateLimit.rate != 50 || cfg.rateLimit.burst != 100 {
		t.Errorf("rate limit = %+v, want 50 qps burst 100", cfg.rateLimit)
	}
	if cfg.tcpMaxConns != 200 || cfg.tcpMaxConnsPerIP != 4 {
		t.Errorf("TCP caps = %d/%d, want 200/4", cfg.tcpMaxConns, cfg.tcpMaxConnsPerIP)
	}

	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"10.42.200.1", true},
		{"10.11.0.1", false},
		{"192.168.1.5", false}, // the override replaces the defaults
		{"2001:db8:42:1::9", true},
		{"198.51.100.7", true},
		{"198.51.100.8", false},
		{"192.0.2.77", true},
	} {
		if got := cfg.clientAllowed(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("clientAllowed(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestLoadDNSGuardConfigRejectsInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		env     map[string]string
		wantErr string
	}{
		{map[string]string{"ALLOWED_CLIENTS": "10.0.0.0/33"}, `ALLOWED_CLIENTS: invalid CIDR "10.0.0.0/33"`},
		{map[string]string{"ALLOWED_CLIENTS": "lan"}, `ALLOWED_CLIENTS: invalid address "lan"`},
		{map[string]string{"ALLOWED_CLIENTS": " , "}, "ALLOWED_CLIENTS is set but contains no CIDRs"},
		{map[string]string{"ALLOWED_CLIENTS": "::ffff:0:0/64"}, "shorter than ::ffff:0:0/96"},
		{map[string]string{"DNS_RATE_LIMIT_QPS": "-5"}, `DNS_RATE_LIMIT_QPS: want a non-negative integer, got "-5"`},
		{map[string]string{"DNS_RATE_LIMIT_QPS": "fast"}, `DNS_RATE_LIMIT_QPS: want a non-negative integer, got "fast"`},
		{map[string]string{"DNS_TCP_MAX_CONNS": "0"}, "DNS_TCP_MAX_CONNS: want a positive integer, got 0"},
		{map[string]string{"DNS_TCP_MAX_CONNS_PER_IP": "x"}, `DNS_TCP_MAX_CONNS_PER_IP: want a non-negative integer, got "x"`},
	} {
		_, err := loadDNSGuardConfig(envMap(tc.env))
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("env %v: error = %v, want it to contain %q", tc.env, err, tc.wantErr)
		}
	}
}

// TestDNSClientACLOverrideAndRefusedCounter: ALLOWED_CLIENTS replaces the
// defaults, and every refusal is counted.
func TestDNSClientACLOverrideAndRefusedCounter(t *testing.T) {
	defer setupTestDB(t)()
	cfg, err := loadDNSGuardConfig(envMap(map[string]string{"ALLOWED_CLIENTS": "203.0.113.0/24"}))
	if err != nil {
		t.Fatal(err)
	}
	old := dnsGuard.Load()
	dnsGuard.Store(cfg)
	defer dnsGuard.Store(old)
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})

	before := dnsGuardStats.aclRefused.Load()
	req := new(dns.Msg)
	req.SetQuestion("printer.home.arpa.", dns.TypeA)

	w := newMockWriter("203.0.113.7")
	handleDNSRequest(w, req)
	if w.written == nil || w.written.Rcode != dns.RcodeSuccess {
		t.Fatalf("203.0.113.7 is in ALLOWED_CLIENTS: want NOERROR, got %+v", w.written)
	}
	w = newMockWriter("192.168.1.5")
	handleDNSRequest(w, req)
	if w.written == nil || w.written.Rcode != dns.RcodeRefused {
		t.Fatalf("192.168.1.5 is outside ALLOWED_CLIENTS: want REFUSED, got %+v", w.written)
	}
	if got := dnsGuardStats.aclRefused.Load() - before; got != 1 {
		t.Errorf("acl refused counter moved by %d, want 1", got)
	}
}

func TestClientRateLimiterTokenBucket(t *testing.T) {
	l := newClientRateLimiter(10, 20)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC).UnixNano()
	a := netip.MustParseAddr("192.168.1.10")
	neighbour := netip.MustParseAddr("192.168.1.200") // same /24: shares the bucket
	other := netip.MustParseAddr("192.168.2.10")

	allowed := 0
	for i := 0; i < 30; i++ {
		if l.allow(a, now) {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatalf("burst: %d of 30 allowed at one instant, want 20", allowed)
	}
	if l.allow(neighbour, now) {
		t.Errorf("192.168.1.200 shares 192.168.1.0/24 with the exhausted client and must be limited")
	}
	if !l.allow(other, now) {
		t.Errorf("192.168.2.10 is a different /24 and must have its own bucket")
	}
	// 0.5 s later, 10 qps has refilled 5 tokens.
	later := now + int64(500*time.Millisecond)
	allowed = 0
	for i := 0; i < 10; i++ {
		if l.allow(a, later) {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("after 500ms at 10 qps: %d allowed, want 5", allowed)
	}

	v6a := netip.MustParseAddr("2001:db8:1:ff00::1")
	v6b := netip.MustParseAddr("2001:db8:1:ffff::2") // same /56
	for i := 0; i < 20; i++ {
		l.allow(v6a, now)
	}
	if l.allow(v6b, now) {
		t.Errorf("IPv6 clients in one /56 must share a bucket")
	}
}

// TestClientRateLimiterShardCapFailsOpen: a full shard sweeps idle buckets,
// and if it is still full new clients are allowed rather than denied.
func TestClientRateLimiterShardCapFailsOpen(t *testing.T) {
	l := newClientRateLimiter(1, 1)
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC).UnixNano()
	for i := range l.shards {
		for j := 0; j < rateLimitShardCap; j++ {
			l.shards[i].buckets[netip.PrefixFrom(netip.AddrFrom4([4]byte{10, byte((j >> 8) & 255), byte(j & 255), 0}), 24)] = tokenBucket{tokens: 0, last: now}
		}
	}
	a := netip.MustParseAddr("172.16.5.5")
	if !l.allow(a, now) {
		t.Errorf("full shard with only fresh buckets must fail open")
	}
	// Once the old buckets are idle past rateLimitIdle they are swept and the
	// new client gets a real bucket (burst 1, so the second query is limited).
	idle := now + int64(rateLimitIdle) + int64(time.Second)
	if !l.allow(a, idle) || l.allow(a, idle) {
		t.Errorf("after the sweep the client must be rate limited normally")
	}
}

// TestRateLimitedUDPQueriesAreDropped: the per-client limit drops UDP (no
// reply to a possibly spoofed source) and counts it.
func TestRateLimitedUDPQueriesAreDropped(t *testing.T) {
	defer setupTestDB(t)()
	cfg, err := loadDNSGuardConfig(envMap(map[string]string{"DNS_RATE_LIMIT_QPS": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	old := dnsGuard.Load()
	dnsGuard.Store(cfg)
	defer dnsGuard.Store(old)
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})

	before := dnsGuardStats.rateLimited.Load()
	req := new(dns.Msg)
	req.SetQuestion("printer.home.arpa.", dns.TypeA)
	answered := 0
	for i := 0; i < 5; i++ {
		w := newMockWriter("10.42.1.42")
		handleDNSRequest(w, req)
		if w.written != nil {
			answered++
		}
	}
	if answered != 2 {
		t.Errorf("1 qps with burst 2: %d of 5 back-to-back queries answered, want 2", answered)
	}
	if got := dnsGuardStats.rateLimited.Load() - before; got != 3 {
		t.Errorf("rate-limited counter moved by %d, want 3", got)
	}
}

func TestUDPResponseLimit(t *testing.T) {
	plain := new(dns.Msg)
	plain.SetQuestion("example.com.", dns.TypeA)
	if got := udpResponseLimit(plain); got != 512 {
		t.Errorf("no EDNS: %d, want 512", got)
	}
	for _, tc := range []struct {
		advertised uint16
		want       int
	}{{4096, 1232}, {1232, 1232}, {1400, 1232}, {800, 800}, {100, 512}} {
		m := new(dns.Msg)
		m.SetQuestion("example.com.", dns.TypeA)
		m.SetEdns0(tc.advertised, false)
		if got := udpResponseLimit(m); got != tc.want {
			t.Errorf("EDNS %d: %d, want %d", tc.advertised, got, tc.want)
		}
	}
}

func TestSetReplyEDNSEchoesOnlyClientEDNS(t *testing.T) {
	upstream := new(dns.Msg)
	upstream.SetQuestion("example.com.", dns.TypeA)
	upstream.SetEdns0(4096, true)
	opt := upstream.IsEdns0()
	opt.Option = append(opt.Option, &dns.EDNS0_SUBNET{Code: dns.EDNS0SUBNET, Family: 1, SourceNetmask: 24, Address: []byte{203, 0, 113, 0}})

	noEDNS := new(dns.Msg)
	noEDNS.SetQuestion("example.com.", dns.TypeA)
	m := upstream.Copy()
	setReplyEDNS(m, noEDNS)
	if m.IsEdns0() != nil {
		t.Errorf("client without EDNS got an OPT record")
	}

	withDO := new(dns.Msg)
	withDO.SetQuestion("example.com.", dns.TypeA)
	withDO.SetEdns0(1400, true)
	m = upstream.Copy()
	setReplyEDNS(m, withDO)
	got := m.IsEdns0()
	if got == nil || got.UDPSize() != 1232 || !got.Do() || len(got.Option) != 0 {
		t.Errorf("EDNS client: OPT = %v, want size 1232, DO=1, no relayed options", got)
	}
	opts := 0
	for _, rr := range m.Extra {
		if rr.Header().Rrtype == dns.TypeOPT {
			opts++
		}
	}
	if opts != 1 {
		t.Errorf("%d OPT records in reply, want exactly 1", opts)
	}
}

func TestParseRebindConfig(t *testing.T) {
	if f, err := parseRebindConfig(envMap(nil)); f != nil || err != nil {
		t.Errorf("unset: want off, got %v %v", f, err)
	}
	if f, err := parseRebindConfig(envMap(map[string]string{"DNS_REBIND_PROTECTION": "false", "DNS_REBIND_ALLOW_DOMAINS": "corp.example"})); f != nil || err != nil {
		t.Errorf("false: want off, got %v %v", f, err)
	}
	if _, err := parseRebindConfig(envMap(map[string]string{"DNS_REBIND_PROTECTION": "sometimes"})); err == nil || !strings.Contains(err.Error(), `DNS_REBIND_PROTECTION: want true or false, got "sometimes"`) {
		t.Errorf("invalid value: error = %v", err)
	}
	f, err := parseRebindConfig(envMap(map[string]string{"DNS_REBIND_PROTECTION": "true", "DNS_REBIND_ALLOW_DOMAINS": " Corp.Example. , ,plex.direct"}))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"localhost", "local", "lan", "home.arpa", "internal", "corp.example", "plex.direct"}
	if !reflect.DeepEqual(f.allowSuffixes, want) {
		t.Errorf("allow suffixes = %v, want %v", f.allowSuffixes, want)
	}
	for name, allowed := range map[string]bool{
		"corp.example.":               true,
		"intranet.corp.example.":      true,
		"evilcorp.example.":           false,
		"nas.home.arpa.":              true,
		"printer.lan.":                true,
		"a.b.plex.direct.":            true,
		"rebind.attacker.example.":    false,
		"home.arpa.attacker.example.": false,
	} {
		if got := f.allows(name); got != allowed {
			t.Errorf("allows(%s) = %v, want %v", name, got, allowed)
		}
	}
}

func TestIsRebindAddress(t *testing.T) {
	for addr, want := range map[string]bool{
		"10.1.2.3": true, "172.16.0.1": true, "192.168.1.1": true, "127.0.0.1": true,
		"169.254.169.254": true, "0.0.0.0": true, "0.1.2.3": true, "100.64.0.1": true,
		"::1": true, "fd00::1": true, "fe80::1": true, "::": true,
		"93.184.216.34": false, "100.128.0.1": false, "2606:4700::1111": false, "8.8.8.8": false,
	} {
		if got := isRebindAddress(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isRebindAddress(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestLoadDNSGuardConfigPolicyCacheSize(t *testing.T) {
	cfg, err := loadDNSGuardConfig(envMap(nil))
	if err != nil || cfg.policyCacheMaxBytes != defaultPolicyCacheMaxBytes {
		t.Fatalf("default policy cache budget = %d (%v), want %d", cfg.policyCacheMaxBytes, err, defaultPolicyCacheMaxBytes)
	}
	cfg, err = loadDNSGuardConfig(envMap(map[string]string{"POLICY_CACHE_SIZE_MB": "512"}))
	if err != nil || cfg.policyCacheMaxBytes != 512<<20 {
		t.Fatalf("POLICY_CACHE_SIZE_MB=512 gave %d (%v)", cfg.policyCacheMaxBytes, err)
	}
	for _, bad := range []string{"0", "-1", "lots"} {
		if _, err := loadDNSGuardConfig(envMap(map[string]string{"POLICY_CACHE_SIZE_MB": bad})); err == nil {
			t.Errorf("POLICY_CACHE_SIZE_MB=%q accepted", bad)
		}
	}
}

func TestLoadDNSGuardConfigCacheSize(t *testing.T) {
	cfg, err := loadDNSGuardConfig(envMap(nil))
	if err != nil || cfg.cacheMaxBytes != 64<<20 {
		t.Fatalf("default cache budget = %d (%v), want 64 MiB", cfg.cacheMaxBytes, err)
	}
	cfg, err = loadDNSGuardConfig(envMap(map[string]string{"DNS_CACHE_SIZE_MB": "256"}))
	if err != nil || cfg.cacheMaxBytes != 256<<20 {
		t.Fatalf("DNS_CACHE_SIZE_MB=256 gave %d (%v)", cfg.cacheMaxBytes, err)
	}
	for _, bad := range []string{"0", "-1", "lots"} {
		if _, err := loadDNSGuardConfig(envMap(map[string]string{"DNS_CACHE_SIZE_MB": bad})); err == nil {
			t.Errorf("DNS_CACHE_SIZE_MB=%q accepted", bad)
		}
	}
}
