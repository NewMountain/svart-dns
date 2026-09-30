package svart

import (
	"fmt"
	"math/rand"
	"net/netip"
	"sync"
	"testing"

	"github.com/miekg/dns"

	"github.com/yeti/svart-dns/internal/listparse"

	"github.com/yeti/svart-dns/internal/policycore"
)

// buildBlocklistDomains generates n realistic-looking blocked domains.
func buildBlocklistDomains(n int) map[string]bool {
	tlds := []string{".com", ".net", ".org", ".io", ".co", ".info", ".xyz"}
	prefixes := []string{"tracker", "ads", "pixel", "beacon", "telemetry", "analytics", "metric", "click", "pop", "banner"}
	mids := []string{"serve", "hub", "net", "cdn", "sys", "link", "data", "tag", "sync", "log"}

	domains := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		d := fmt.Sprintf("%s%d.%s%s", prefixes[i%len(prefixes)], i, mids[i%len(mids)], tlds[i%len(tlds)])
		domains[d] = true
	}
	return domains
}

// setupBlocklistBench publishes a policy snapshot in which clientIP has one
// directly assigned blocklist holding domains (built the way production
// builds it: one index, normalized rules).
func setupBlocklistBench(clientIP string, domains map[string]bool) {
	lists := []policycore.List{{ID: 1, Name: "bench"}}
	ix, err := policycore.BuildIndex(lists, func(add func(int, string)) error {
		for d := range domains {
			add(0, listparse.NormalizeEntry(d))
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	policyState.Store(policycore.BuildSnapshot(policycore.Config{
		Lists:   lists,
		Clients: map[string]*policycore.ListIDs{clientIP: {Block: []int{1}}},
	}, ix))
	policyCache.Clear()
}

// BenchmarkIsBlockedForClient_Miss measures lookup latency when the domain is NOT blocked.
// This is the common case for legitimate traffic — the full check path runs.
func BenchmarkIsBlockedForClient_Miss(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000, 500000} {
		b.Run(fmt.Sprintf("domains=%d", size), func(b *testing.B) {
			domains := buildBlocklistDomains(size)
			setupBlocklistBench("10.42.1.42", domains)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Use a domain guaranteed to not be in the blocklist
				isBlockedForClient("10.42.1.42", "legitimate-site.example.com")
			}
		})
	}
}

// BenchmarkIsBlockedForClient_Hit measures lookup latency when the domain IS blocked.
func BenchmarkIsBlockedForClient_Hit(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000, 500000} {
		b.Run(fmt.Sprintf("domains=%d", size), func(b *testing.B) {
			domains := buildBlocklistDomains(size)
			setupBlocklistBench("10.42.1.42", domains)

			// Pick a domain that exists in the blocklist
			var target string
			for d := range domains {
				target = d
				break
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				isBlockedForClient("10.42.1.42", target)
			}
		})
	}
}

// BenchmarkIsBlockedForClient_CachedHit measures the block cache fast path.
func BenchmarkIsBlockedForClient_CachedHit(b *testing.B) {
	domains := buildBlocklistDomains(100000)
	setupBlocklistBench("10.42.1.42", domains)

	// Prime the cache
	isBlockedForClient("10.42.1.42", "legitimate-site.example.com")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isBlockedForClient("10.42.1.42", "legitimate-site.example.com")
	}
}

// BenchmarkIsBlockedForClient_SubdomainMatch measures subdomain walking performance.
func BenchmarkIsBlockedForClient_SubdomainMatch(b *testing.B) {
	domains := buildBlocklistDomains(100000)
	// Add a parent domain that will match via subdomain walk
	domains["doubleclick.net"] = true
	setupBlocklistBench("10.42.1.42", domains)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Clear cache to force the full subdomain walk each time
		policyCache.Clear()
		isBlockedForClient("10.42.1.42", "pagead2.g.doubleclick.net")
	}
}

// BenchmarkIsBlockedForClient_WildcardScan measures wildcard matching (worst-case linear scan).
func BenchmarkIsBlockedForClient_WildcardScan(b *testing.B) {
	for _, wildcardCount := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("wildcards=%d", wildcardCount), func(b *testing.B) {
			domains := buildBlocklistDomains(10000)
			for i := 0; i < wildcardCount; i++ {
				domains[fmt.Sprintf("*.tracker%d.com", i)] = true
			}
			setupBlocklistBench("10.42.1.42", domains)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				policyCache.Clear()
				isBlockedForClient("10.42.1.42", "sub.nonexistent-domain.example.com")
			}
		})
	}
}

// BenchmarkIsBlockedForClient_UnknownClient measures the fast exit for unknown clients.
func BenchmarkIsBlockedForClient_UnknownClient(b *testing.B) {
	domains := buildBlocklistDomains(500000)
	setupBlocklistBench("10.42.1.42", domains)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isBlockedForClient("10.42.99.99", "anything.com")
	}
}

// BenchmarkIsBlockedForClient_Parallel measures concurrent lookup throughput.
func BenchmarkIsBlockedForClient_Parallel(b *testing.B) {
	domains := buildBlocklistDomains(100000)
	setupBlocklistBench("10.42.1.42", domains)

	lookups := []string{
		"legitimate-site.example.com",
		"tracker0.serve.com",
		"sub.ads123.hub.net",
		"google.com",
		"facebook.com",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			isBlockedForClient("10.42.1.42", lookups[i%len(lookups)])
			i++
		}
	})
}

// --- Policy engine benchmarks ---

func BenchmarkEvaluatePolicy_CachedHit(b *testing.B) {
	domains := buildBlocklistDomains(100000)
	setupBlocklistBench("10.42.1.42", domains)
	policyCache.Clear()

	// Prime the cache
	evaluatePolicy("10.42.1.42", "legitimate-site.example.com", 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluatePolicy("10.42.1.42", "legitimate-site.example.com", 1)
	}
}

func BenchmarkEvaluatePolicy_ColdMiss(b *testing.B) {
	domains := buildBlocklistDomains(100000)
	setupBlocklistBench("10.42.1.42", domains)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policyCache.Clear()
		evaluatePolicy("10.42.1.42", "legitimate-site.example.com", 1)
	}
}

// BenchmarkMatchRange measures finding the ranges that contain a client:
// ten /16s and a more specific /24, sorted the way snapshots sort them.
func BenchmarkMatchRange(b *testing.B) {
	cfg := policycore.Config{}
	for i := 0; i < 10; i++ {
		cidr := fmt.Sprintf("10.%d.0.0/16", i)
		cfg.Ranges = append(cfg.Ranges, policycore.RangeConfig{ID: i + 1, Name: fmt.Sprintf("Range%d", i), CIDR: cidr})
	}
	cfg.Ranges = append(cfg.Ranges, policycore.RangeConfig{ID: 11, Name: "Specific", CIDR: "10.0.1.0/24"})
	s := policycore.BuildSnapshot(cfg, emptyPolicySnapshot().Index)
	client := netip.MustParseAddr("10.0.1.42")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for r := range s.Ranges {
			if s.Ranges[r].Prefix.Contains(client) {
				break
			}
		}
	}
}

// --- DNS Cache benchmarks ---
//
// A "hit" here is the full work to produce a servable reply: key, lookup and
// the patched wire bytes. The pre-2026-09 version measured lookup + Msg.Copy
// and left the Pack to WriteMsg, so the new numbers include strictly more.

func BenchmarkCacheGet_Hit(b *testing.B) {
	cache.clear()
	cache.set(testCacheKey("cached.example.com", dns.TypeA), newTestDNSMsg("cached.example.com", dns.TypeA), 3600)
	req := new(dns.Msg)
	req.SetQuestion("cached.example.com.", dns.TypeA)
	buf := make([]byte, 0, 512)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now := clock.NowUnixNano()
		e, ok := cache.get(cacheKeyFor(req), now)
		if !ok {
			b.Fatal("miss")
		}
		buf = e.appendReply(buf[:0], req, now)
	}
}

func BenchmarkCacheGet_Miss(b *testing.B) {
	cache.clear()
	key := testCacheKey("nonexistent.example.com", dns.TypeA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.get(key, clock.NowUnixNano())
	}
}

func BenchmarkCacheSet(b *testing.B) {
	cache.clear()
	msg := newTestDNSMsg("bench.example.com", dns.TypeA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("bench%d.example.com.", i)
		msg.Question[0].Name = name
		cache.set(dnsCacheKey{name: name, qtype: dns.TypeA}, msg, 3600)
	}
}

func BenchmarkCacheGet_Parallel(b *testing.B) {
	cache.clear()
	// Pre-populate 1000 entries
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("domain%d.example.com", i)
		cache.set(testCacheKey(name, dns.TypeA), newTestDNSMsg(name, dns.TypeA), 3600)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := new(dns.Msg)
		req.SetQuestion("domain0.example.com.", dns.TypeA)
		buf := make([]byte, 0, 512)
		i := 0
		for pb.Next() {
			req.Question[0].Name = fmt.Sprintf("domain%d.example.com.", i%1000)
			now := clock.NowUnixNano()
			if e, ok := cache.get(cacheKeyFor(req), now); ok {
				buf = e.appendReply(buf[:0], req, now)
			}
			i++
		}
	})
}

// --- Rewrite benchmarks ---

func BenchmarkCheckRewrite_Hit(b *testing.B) {
	rewritesCache.Store(&sync.Map{})
	rewritesCache.Load().Store("nas.example.lan.", "10.42.1.10")
	rewritesCache.Load().Store("grafana.example.lan.", "10.42.1.5")
	rewritesCache.Load().Store("plex.example.lan.", "10.42.1.20")

	msg := new(dns.Msg)
	msg.SetQuestion("nas.example.lan.", dns.TypeA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkRewrite(msg)
	}
}

func BenchmarkCheckRewrite_Miss(b *testing.B) {
	rewritesCache.Store(&sync.Map{})
	rewritesCache.Load().Store("nas.example.lan.", "10.42.1.10")

	msg := new(dns.Msg)
	msg.SetQuestion("google.com.", dns.TypeA)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		checkRewrite(msg)
	}
}

// --- Wildcard matching benchmarks ---

func BenchmarkMatchWildcard_SimplePrefix(b *testing.B) {
	rule := policycore.NewComplexRule("*.doubleclick.net", 0)
	for i := 0; i < b.N; i++ {
		rule.Matches("pagead2.g.doubleclick.net")
	}
}

func BenchmarkMatchWildcard_ComplexPattern(b *testing.B) {
	rule := policycore.NewComplexRule("ad*.tracker*.com", 0)
	for i := 0; i < b.N; i++ {
		rule.Matches("ads.tracker-cdn.com")
	}
}

func BenchmarkMatchWildcard_NoWildcard(b *testing.B) {
	rule := policycore.NewComplexRule("exact.domain.com", 0)
	for i := 0; i < b.N; i++ {
		rule.Matches("exact.domain.com")
	}
}

// --- Upstream parsing benchmarks ---

func BenchmarkParseUpstreamAddress(b *testing.B) {
	inputs := []string{
		"9.9.9.9:53",
		"tls://dns.quad9.net:853",
		"https://doh.mullvad.net/dns-query",
		"udp://1.1.1.1:53",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseUpstreamAddress(inputs[i%len(inputs)])
	}
}

// --- Weighted selection benchmarks ---

func BenchmarkCalculateWeight(b *testing.B) {
	upstreamStatsMap = sync.Map{}
	for i := 0; i < 20; i++ {
		addr := fmt.Sprintf("server%d.dns.com:53", i)
		for j := 0; j < 100; j++ {
			// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
			recordSuccess(addr, int64(10+rand.Intn(200)))
		}
		if i%3 == 0 {
			for j := 0; j < 10; j++ {
				recordFailure(addr)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateWeight(fmt.Sprintf("server%d.dns.com:53", i%20))
	}
}

func BenchmarkWeightedRandomSelect(b *testing.B) {
	upstreams := make([]Upstream, 5)
	weights := make([]float64, 5)
	totalWeight := 0.0
	for i := range upstreams {
		upstreams[i] = Upstream{ID: i + 1, Upstream: fmt.Sprintf("server%d:53", i), Enabled: true}
		weights[i] = float64(5-i) * 0.2
		totalWeight += weights[i]
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		weightedRandomSelect(upstreams, weights, totalWeight)
	}
}

// --- cache key benchmark ---

func BenchmarkCacheKeyFor(b *testing.B) {
	req := new(dns.Msg)
	req.SetQuestion("www.example.com.", dns.TypeA)
	for i := 0; i < b.N; i++ {
		cacheKeyFor(req)
	}
}

// --- Block cache lookup (sync.Map Load) ---

func BenchmarkPolicyCacheLookup(b *testing.B) {
	policyCache.Clear()
	// Pre-populate 100k cache entries
	notBlocked := &policycore.PolicyResult{Result: ""}
	blocked := &policycore.PolicyResult{Result: "block"}
	client := netip.MustParseAddr("10.42.1.42")
	for i := 0; i < 100000; i++ {
		key := policyKey{client: client, domain: fmt.Sprintf("domain%d.example.com", i)}
		if i%3 == 0 {
			policyCache.Store(key, blocked)
		} else {
			policyCache.Store(key, notBlocked)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policyCache.Load(policyKey{client: client, domain: fmt.Sprintf("domain%d.example.com", i%100000)})
	}
}

// --- Full request simulation benchmark ---
// Simulates the hot path: cache check → blocklist check → (no upstream, just measure decision logic)

func BenchmarkFullLookupDecision(b *testing.B) {
	// Setup cache with some entries
	cache.clear()
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("cached%d.example.com", i)
		cache.set(testCacheKey(name, dns.TypeA), newTestDNSMsg(name, dns.TypeA), 3600)
	}
	hitReq := new(dns.Msg)
	hitReq.SetQuestion("cached50.example.com.", dns.TypeA)
	buf := make([]byte, 0, 512)

	// Setup blocklist
	domains := buildBlocklistDomains(100000)
	setupBlocklistBench("10.42.1.42", domains)

	// Setup rewrites
	rewritesCache.Store(&sync.Map{})
	rewritesCache.Load().Store("nas.example.lan.", "10.42.1.10")

	queries := []struct {
		name string
		typ  uint16
	}{
		{"cached50.example.com", dns.TypeA},      // cache hit
		{"tracker0.serve.com", dns.TypeA},        // blocked
		{"google.com", dns.TypeA},                // cache miss + not blocked
		{"nas.example.lan", dns.TypeA},           // rewrite
		{"pagead2.g.doubleclick.net", dns.TypeA}, // not in blocklist
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]

		// 1. Check cache (a hit renders the reply, as the old Copy did)
		now := clock.NowUnixNano()
		if e, ok := cache.get(dnsCacheKey{name: dns.Fqdn(q.name), qtype: q.typ}, now); ok {
			buf = e.appendReply(buf[:0], hitReq, now)
			continue
		}

		// 2. Check rewrite
		msg := new(dns.Msg)
		msg.SetQuestion(dns.Fqdn(q.name), q.typ)
		if resp := checkRewrite(msg); resp != nil {
			continue
		}

		// 3. Check blocklist
		isBlockedForClient("10.42.1.42", q.name)
	}
}

// newTestDNSMsg creates a minimal DNS message for benchmarks (reuses cache_test helper if available).
// Defined here in case cache_test.go hasn't been compiled in the same test binary context.
