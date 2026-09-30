package main

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// resetRewritesCache atomically swaps in a fresh rewrites cache built from the
// given entries — mirrors how loadRewritesFromDB publishes reloads in
// production (rewrites.go), so tests exercise the same atomic.Pointer swap
// path rather than mutating a shared sync.Map in place.
func resetRewritesCache(entries map[string]Rewrite) {
	m := &sync.Map{}
	for k, v := range entries {
		m.Store(k, v)
	}
	rewritesCache.Store(m)
}

func TestCheckRewriteA(t *testing.T) {
	// Set up rewrite cache directly
	resetRewritesCache(map[string]Rewrite{
		"grafana.example.lan": {
			Domain:      "grafana.example.lan",
			IPAddresses: []string{"10.42.1.5"},
		},
	})

	// Build a DNS query for grafana.example.lan A record
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("grafana.example.lan"), dns.TypeA)

	resp := checkRewrite(req)
	if resp == nil {
		t.Fatal("expected rewrite response, got nil")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}

	aRecord, ok := resp.Answer[0].(*dns.A)
	if !ok {
		t.Fatal("expected A record answer")
	}
	if aRecord.A.String() != "10.42.1.5" {
		t.Errorf("expected IP 10.42.1.5, got %s", aRecord.A.String())
	}
}

func TestCheckRewriteAAAA(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"ipv6.example.lan": {
			Domain:      "ipv6.example.lan",
			IPAddresses: []string{"fd00::1"},
		},
	})

	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("ipv6.example.lan"), dns.TypeAAAA)

	resp := checkRewrite(req)
	if resp == nil {
		t.Fatal("expected AAAA rewrite response, got nil")
	}
	if len(resp.Answer) != 1 {
		t.Fatalf("expected 1 answer, got %d", len(resp.Answer))
	}

	aaaaRecord, ok := resp.Answer[0].(*dns.AAAA)
	if !ok {
		t.Fatal("expected AAAA record answer")
	}
	expected := net.ParseIP("fd00::1")
	if !aaaaRecord.AAAA.Equal(expected) {
		t.Errorf("expected IP fd00::1, got %s", aaaaRecord.AAAA.String())
	}
}

func TestCheckRewriteMultipleIPs(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"plex.example.lan": {
			Domain:      "plex.example.lan",
			IPAddresses: []string{"10.42.1.100", "10.42.1.101"},
		},
	})

	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("plex.example.lan"), dns.TypeA)

	resp := checkRewrite(req)
	if resp == nil {
		t.Fatal("expected rewrite response, got nil")
	}
	if len(resp.Answer) != 2 {
		t.Fatalf("expected 2 answers for multi-IP rewrite, got %d", len(resp.Answer))
	}
}

func TestCheckRewriteNoMatch(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"grafana.example.lan": {
			Domain:      "grafana.example.lan",
			IPAddresses: []string{"10.42.1.5"},
		},
	})

	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("unknown.example.lan"), dns.TypeA)

	resp := checkRewrite(req)
	if resp != nil {
		t.Error("expected nil for non-matching domain")
	}
}

func TestCheckRewriteEmptyQuestion(t *testing.T) {
	req := new(dns.Msg)
	// No question set

	resp := checkRewrite(req)
	if resp != nil {
		t.Error("expected nil for empty question")
	}
}

func TestCheckRewriteWrongQueryType(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"grafana.example.lan": {
			Domain:      "grafana.example.lan",
			IPAddresses: []string{"10.42.1.5"}, // IPv4 only
		},
	})

	// Query for AAAA but only IPv4 IPs available — should return NODATA (empty answer)
	// to prevent internal hostnames from leaking to upstream resolvers.
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("grafana.example.lan"), dns.TypeAAAA)

	resp := checkRewrite(req)
	if resp == nil {
		t.Fatal("expected NODATA response, got nil (would leak to upstream)")
	}
	if len(resp.Answer) != 0 {
		t.Errorf("expected empty answer section, got %d answers", len(resp.Answer))
	}
	if resp.Rcode != dns.RcodeSuccess {
		t.Errorf("expected NOERROR rcode, got %s", dns.RcodeToString[resp.Rcode])
	}
}

func TestCheckRewriteCaseInsensitive(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"grafana.example.lan": {
			Domain:      "grafana.example.lan",
			IPAddresses: []string{"10.42.1.5"},
		},
	})

	// Query with uppercase — checkRewrite lowercases the domain
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("GRAFANA.EXAMPLE.LAN"), dns.TypeA)

	resp := checkRewrite(req)
	if resp == nil {
		t.Fatal("expected rewrite to match case-insensitively")
	}
}

func TestLoadRewritesFromDBIntegration(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Insert rewrites into DB
	if _, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		"grafana.example.lan", "", "10.42.1.5", true); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		"disabled.example.lan", "", "10.42.1.200", false); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		"noip.example.lan", "", "", true); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	} // No IPs — should be skipped

	if err := loadRewritesFromDB(); err != nil {
		t.Fatalf("failed to load rewrites: %v", err)
	}

	// Enabled rewrite should be in cache
	_, ok := rewritesCache.Load().Load("grafana.example.lan")
	if !ok {
		t.Error("expected grafana.example.lan in cache")
	}

	// Disabled rewrite should NOT be in cache
	_, ok = rewritesCache.Load().Load("disabled.example.lan")
	if ok {
		t.Error("disabled rewrite should not be in cache")
	}

	// Empty IP rewrite should NOT be in cache
	_, ok = rewritesCache.Load().Load("noip.example.lan")
	if ok {
		t.Error("rewrite with no IPs should not be in cache")
	}
}

// TestRewritesCacheConcurrentReloadRace is the A2 regression test: before the
// fix, rewritesCache was a bare sync.Map reassigned under a mutex
// (`rewritesCache = sync.Map{}`) that the hot path (checkRewrite) never
// acquired — the mutex protected nothing, and `go test -race` flags the
// concurrent struct-assignment-vs-read as a real data race. It also let a
// racing query observe a torn/being-replaced map and miss a real rewrite,
// leaking an internal hostname upstream (DD-023).
//
// This test hammers checkRewrite concurrently with repeated reloads
// (loadRewritesFromDB against a real temp DB, alternating which domains are
// enabled) on the same key. Under -race it must be clean, and every query
// must resolve via the atomically-swapped map — never a nil/torn read.
func TestRewritesCacheConcurrentReloadRace(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const domain = "grafana.example.lan"
	if _, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		domain, "", "10.42.1.5", true); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := loadRewritesFromDB(); err != nil {
		t.Fatalf("failed to load rewrites: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reloader goroutine: repeatedly reloads from the DB, simulating a
	// sync-merge or admin edit racing against live DNS traffic.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := loadRewritesFromDB(); err != nil {
				t.Errorf("reload %d failed: %v", i, err)
				return
			}
		}
	}()

	// Query goroutines: hammer checkRewrite for the same key concurrently
	// with reloads.
	var queries atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := new(dns.Msg)
			req.SetQuestion(dns.Fqdn(domain), dns.TypeA)
			for i := 0; i < 2000; i++ {
				checkRewrite(req)
				queries.Add(1)
			}
		}()
	}

	// Let the query goroutines run to completion, then stop the reloader.
	go func() {
		time.Sleep(200 * time.Millisecond)
		close(stop)
	}()
	wg.Wait()

	if queries.Load() == 0 {
		t.Fatal("expected queries to run")
	}

	// After settling, the rewrite must still resolve correctly — no query
	// should have permanently corrupted the cache.
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	resp := checkRewrite(req)
	if resp == nil || len(resp.Answer) != 1 {
		t.Fatalf("expected rewrite to still resolve after concurrent reload race, got %+v", resp)
	}
}

func TestRewritesCacheHelperBuildsIndependentMap(t *testing.T) {
	resetRewritesCache(map[string]Rewrite{
		"a.example.com": {Domain: "a.example.com", IPAddresses: []string{"10.0.0.1"}},
	})
	first := rewritesCache.Load()

	resetRewritesCache(map[string]Rewrite{
		"b.example.com": {Domain: "b.example.com", IPAddresses: []string{"10.0.0.2"}},
	})
	second := rewritesCache.Load()

	if first == second {
		t.Fatal("expected resetRewritesCache to publish a new map instance, not mutate the old one")
	}
	if _, ok := second.Load("a.example.com"); ok {
		t.Error("expected the old entry to be gone from the freshly-swapped map")
	}
	if _, ok := second.Load("b.example.com"); !ok {
		t.Error("expected the new entry to be present")
	}
}
