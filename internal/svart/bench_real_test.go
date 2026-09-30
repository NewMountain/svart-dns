package svart

import (
	"bufio"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

// loadHageziUltimate downloads and parses the Hagezi Ultimate blocklist.
// Caches to a temp file so subsequent runs don't re-download.
const hageziCachePath = "/tmp/hagezi-ultimate-cached.txt"
const hageziURL = "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/adblock/ultimate.txt"

func loadHageziDomains(b *testing.B) map[string]bool {
	b.Helper()

	// Try cached file first
	if f, err := os.Open(hageziCachePath); err == nil {
		defer func() { checkTestClose(b, f) }()
		return parseHageziFile(b, f)
	}

	// Download
	b.Logf("Downloading Hagezi Ultimate blocklist (first run only)...")
	resp, err := http.Get(hageziURL)
	if err != nil {
		b.Skipf("Cannot fetch Hagezi list (network unavailable): %v", err)
		return nil
	}
	defer func() { checkTestClose(b, resp.Body) }()

	// Save to cache file
	cacheFile, err := os.Create(hageziCachePath)
	if err != nil {
		b.Fatalf("Cannot create cache file: %v", err)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(cacheFile, scanner.Text()); err != nil {
			b.Errorf("fixture operation failed: %v", err)
		}
	}
	checkTestClose(b, cacheFile)

	// Re-open and parse
	f, err := os.Open(hageziCachePath)
	if err != nil {
		b.Fatalf("Cannot re-open cache file: %v", err)
	}
	defer func() { checkTestClose(b, f) }()
	return parseHageziFile(b, f)
}

func parseHageziFile(b *testing.B, f *os.File) map[string]bool {
	b.Helper()
	domains := make(map[string]bool, 300000)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "[") {
			continue
		}
		d := listparse.ParseDomain(line)
		if d != "" {
			domains[d] = true
		}
	}
	b.Logf("Loaded %d domains from Hagezi Ultimate", len(domains))
	return domains
}

// --- Benchmarks against real Hagezi Ultimate list ---

// BenchmarkHagezi_Miss: lookup a legitimate domain against 267k+ real blocked domains.
func BenchmarkHagezi_Miss(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	// Domains that should NOT be blocked (legitimate sites)
	legit := []string{
		"google.com",
		"github.com",
		"en.wikipedia.org",
		"example.lan",
		"kernel.org",
		"go.dev",
		"reddit.com",
		"stackoverflow.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isBlockedForClient("10.42.1.42", legit[i%len(legit)])
	}
}

// BenchmarkHagezi_Hit: lookup a blocked domain against the real list.
func BenchmarkHagezi_Hit(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	// Pick 8 real blocked domains from the list
	blocked := make([]string, 0, 8)
	for d := range domains {
		blocked = append(blocked, d)
		if len(blocked) == 8 {
			break
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isBlockedForClient("10.42.1.42", blocked[i%len(blocked)])
	}
}

// BenchmarkHagezi_ColdLookup: clear cache between each lookup to force full check every time.
func BenchmarkHagezi_ColdLookup(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policyCache.Clear()
		isBlockedForClient("10.42.1.42", "google.com")
	}
}

// BenchmarkHagezi_SubdomainWalk: test subdomain matching against the real list.
// E.g. "sub.tracker.example.com" walks up to "tracker.example.com" then "example.com".
func BenchmarkHagezi_SubdomainWalk(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	// Deep subdomains of legitimate sites (should miss but walk the full chain)
	deep := []string{
		"api.v2.cdn.assets.static.example.com",
		"tracking.pixel.ads.deeply.nested.subdomain.test.org",
		"a.b.c.d.e.f.g.legitimate-domain.net",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		policyCache.Clear()
		isBlockedForClient("10.42.1.42", deep[i%len(deep)])
	}
}

// BenchmarkHagezi_MixedTraffic: simulate realistic traffic where ~10% of queries are blocked.
func BenchmarkHagezi_MixedTraffic(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	// Build a mix: 90% legitimate, 10% blocked
	blocked := make([]string, 0, 100)
	for d := range domains {
		blocked = append(blocked, d)
		if len(blocked) == 100 {
			break
		}
	}
	legit := []string{
		"google.com", "github.com", "wikipedia.org", "youtube.com",
		"reddit.com", "stackoverflow.com", "amazon.com", "netflix.com",
		"twitter.com", "linkedin.com", "apple.com", "microsoft.com",
		"cloudflare.com", "fastly.com", "akamai.com", "example.lan",
		"kernel.org", "go.dev", "rust-lang.org", "python.org",
	}

	// Pre-build query list: 900 legit + 100 blocked
	queries := make([]string, 0, 1000)
	for i := 0; i < 900; i++ {
		queries = append(queries, legit[i%len(legit)])
	}
	for i := 0; i < 100; i++ {
		queries = append(queries, blocked[i%len(blocked)])
	}
	// Shuffle
	// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
	rand.Shuffle(len(queries), func(i, j int) {
		queries[i], queries[j] = queries[j], queries[i]
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		isBlockedForClient("10.42.1.42", queries[i%len(queries)])
	}
}

// BenchmarkHagezi_Parallel: concurrent lookups against the real list.
func BenchmarkHagezi_Parallel(b *testing.B) {
	domains := loadHageziDomains(b)
	setupBlocklistBench("10.42.1.42", domains)

	lookups := []string{
		"google.com",
		"github.com",
		"facebook.com",
		"tracker.example.com",
		"example.lan",
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

// BenchmarkHagezi_ParseDomain: benchmark our adblock parser against real list lines.
func BenchmarkHagezi_ParseDomain(b *testing.B) {
	// Representative lines from Hagezi Ultimate
	lines := []string{
		"||doubleclick.net^",
		"||googleadservices.com^",
		"||facebook-tracking.example.com^",
		"||0.beer^",
		"||zz-analytics.mxpnl.net^",
		"||tracker.unity3d.com^",
		"||metric.gstatic.com^",
		"||telemetry.mozilla.org^",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		listparse.ParseDomain(lines[i%len(lines)])
	}
}
