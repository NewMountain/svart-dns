package main

import (
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/policycore"
)

func TestPolicyLRU_BasicStoreThenLoad(t *testing.T) {
	c := newPolicyLRU(1024 * 1024) // 1MB
	r := &policycore.PolicyResult{ClientIP: "10.42.1.42", Domain: "example.com", Result: "block"}

	c.Store(testPolicyKey("10.42.1.42:example.com"), r)
	got, ok := c.Load(testPolicyKey("10.42.1.42:example.com"))
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.Result != "block" {
		t.Errorf("expected result 'block', got %q", got.Result)
	}
}

func TestPolicyLRU_MissReturnsNil(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	got, ok := c.Load(testPolicyKey("nonexistent"))
	if ok || got != nil {
		t.Error("expected cache miss")
	}
}

func TestPolicyLRU_EvictsLRU(t *testing.T) {
	// Tiny budget: enough for ~2-3 entries
	c := newPolicyLRU(16 * 300) // ~300 bytes per entry, 16 shards

	// Insert many entries to force eviction
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("10.42.1.42:domain%d.com", i)
		c.Store(testPolicyKey(key), &policycore.PolicyResult{Result: "block", Domain: fmt.Sprintf("domain%d.com", i)})
	}

	// Some early entries should have been evicted
	stats := c.Stats()
	if stats.Entries >= 1000 {
		t.Errorf("expected eviction to reduce entries below 1000, got %d", stats.Entries)
	}
	if stats.UsedBytes <= 0 {
		t.Error("expected positive used bytes")
	}
}

func TestPolicyLRU_Clear(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	for i := 0; i < 100; i++ {
		c.Store(testPolicyKey(fmt.Sprintf("key%d", i)), &policycore.PolicyResult{Result: "block"})
	}

	c.Clear()
	stats := c.Stats()
	if stats.Entries != 0 {
		t.Errorf("expected 0 entries after clear, got %d", stats.Entries)
	}
	if stats.UsedBytes != 0 {
		t.Errorf("expected 0 used bytes after clear, got %d", stats.UsedBytes)
	}
}

func TestPolicyLRU_UpdateExisting(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	c.Store(testPolicyKey("key1"), &policycore.PolicyResult{Result: "block"})
	c.Store(testPolicyKey("key1"), &policycore.PolicyResult{Result: "allow"})

	got, ok := c.Load(testPolicyKey("key1"))
	if !ok || got.Result != "allow" {
		t.Error("expected updated value")
	}

	stats := c.Stats()
	if stats.Entries != 1 {
		t.Errorf("expected 1 entry (not 2), got %d", stats.Entries)
	}
}

func TestPolicyLRU_HitMissStats(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	c.Store(testPolicyKey("exists"), &policycore.PolicyResult{Result: "block"})

	c.Load(testPolicyKey("exists"))
	c.Load(testPolicyKey("exists"))
	c.Load(testPolicyKey("missing"))

	stats := c.Stats()
	if stats.Hits != 2 {
		t.Errorf("expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestPolicyLRU_Concurrent(t *testing.T) {
	c := newPolicyLRU(100 * 1024 * 1024) // 100MB

	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				key := fmt.Sprintf("client%d:domain%d.com", id, i)
				c.Store(testPolicyKey(key), &policycore.PolicyResult{Result: "block", Domain: fmt.Sprintf("domain%d.com", i)})
				c.Load(testPolicyKey(key))
			}
		}(g)
	}
	wg.Wait()

	stats := c.Stats()
	if stats.Entries == 0 {
		t.Error("expected entries after concurrent writes")
	}
}

func TestPolicyLRU_ByteBudgetRespected(t *testing.T) {
	maxBytes := int64(50 * 1024) // 50KB
	c := newPolicyLRU(maxBytes)

	// Insert many entries
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("10.42.1.42:domain%d.example.com", i)
		c.Store(testPolicyKey(key), &policycore.PolicyResult{
			Result:   "block",
			ClientIP: "10.42.1.42",
			Domain:   fmt.Sprintf("domain%d.example.com", i),
		})
	}

	stats := c.Stats()
	if stats.UsedBytes > maxBytes+1024 { // small tolerance for rounding
		t.Errorf("used bytes %d exceeds max %d", stats.UsedBytes, maxBytes)
	}
}

func TestPolicyLRU_SetMaxBytesShrinksToTheNewBudget(t *testing.T) {
	c := newPolicyLRU(1 << 20)
	for i := 0; i < 2000; i++ {
		d := fmt.Sprintf("host%d.tracker.example.com", i)
		c.Store(testPolicyKey("10.42.1.42:"+d), &policycore.PolicyResult{Result: "block", ClientIP: "10.42.1.42", Domain: d})
	}
	before := c.Stats()
	c.setMaxBytes(64 << 10)
	after := c.Stats()
	if after.UsedBytes > 64<<10 || after.Entries >= before.Entries || c.maxBytes.Load() != 64<<10 {
		t.Fatalf("after shrinking to 64 KiB: %d bytes in %d entries (was %d in %d), budget %d",
			after.UsedBytes, after.Entries, before.UsedBytes, before.Entries, c.maxBytes.Load())
	}
	last := "host1999.tracker.example.com"
	if _, ok := c.Load(testPolicyKey("10.42.1.42:" + last)); !ok {
		t.Fatalf("the most recently stored entry %s was evicted", last)
	}
}

// TestPolicyLRU_StaleGenerationEntryIsTreatedAsMiss is the deterministic A3
// regression test. It reproduces the exact stale-write race without relying
// on timing: a "slow" evaluation captures the generation, a reload happens
// (bumping the generation), and then the slow evaluation's Store lands with
// its now-stale generation. Before the fix, this Store would have physically
// overwritten (or repopulated) the cache with a decision computed against
// pre-reload data, and it would persist there until the next reload. After
// the fix, Load() must treat it as a miss because its generation no longer
// matches current.
func TestPolicyLRU_StaleGenerationEntryIsTreatedAsMiss(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)

	// "Slow" evaluation starts: captures the generation before computing.
	genAtStart := c.Generation()

	// A reload happens concurrently — bumps the generation (Clear does this).
	c.Clear()

	// The slow evaluation finishes and stores its (now stale) result, tagged
	// with the generation it captured at the start.
	c.StoreGen(testPolicyKey("10.42.1.42:stale.example.com"), &policycore.PolicyResult{Result: "block"}, genAtStart)

	// A fresh Load must treat this as a miss — the entry is invisible even
	// though it's physically present in the map.
	if _, ok := c.Load(testPolicyKey("10.42.1.42:stale.example.com")); ok {
		t.Fatal("expected stale-generation entry to be treated as a cache miss")
	}
}

// TestPolicyLRU_StoreGenNoOpWhenGenerationAlreadyStale verifies the courtesy
// optimization: StoreGen skips writing an entry at all if the generation has
// already moved on by the time Store is called (not required for correctness
// — Load() alone guarantees that — but avoids wasting cache memory on known
// stale data).
func TestPolicyLRU_StoreGenNoOpWhenGenerationAlreadyStale(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	staleGen := c.Generation()
	c.Clear() // bumps generation

	c.StoreGen(testPolicyKey("key"), &policycore.PolicyResult{Result: "block"}, staleGen)

	stats := c.Stats()
	if stats.Entries != 0 {
		t.Errorf("expected StoreGen to skip writing a stale-generation entry, got %d entries", stats.Entries)
	}
}

// TestPolicyLRU_FreshGenerationSurvivesReload verifies that an entry stored
// with the CURRENT generation (the normal case — no race) is unaffected by
// this change and remains a cache hit.
func TestPolicyLRU_FreshGenerationSurvivesReload(t *testing.T) {
	c := newPolicyLRU(1024 * 1024)

	gen := c.Generation()
	c.StoreGen(testPolicyKey("key"), &policycore.PolicyResult{Result: "allow"}, gen)

	got, ok := c.Load(testPolicyKey("key"))
	if !ok || got.Result != "allow" {
		t.Fatal("expected fresh-generation entry to be a cache hit")
	}
}

// TestPolicyLRU_ConcurrentReloadDuringLoadStoreIsRaceFree stresses Load/Store
// against concurrent Clear() (bumping the generation) on the SAME key —
// the interleaving pattern the A3 bug was about. Run under -race.
func TestPolicyLRU_ConcurrentReloadDuringLoadStoreIsRaceFree(_ *testing.T) {
	c := newPolicyLRU(1024 * 1024)
	const key = "10.42.1.42:stress.example.com"

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reloader: repeatedly clears (bumps generation), simulating concurrent
	// blocklist/allowlist/range reloads racing against live evaluation.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			c.Clear()
		}
	}()

	// Evaluators: mimic evaluatePolicy's pattern — capture generation, "compute"
	// (no-op here), then StoreGen with the captured generation — interleaved
	// with Load on the same key.
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				gen := c.Generation()
				c.StoreGen(testPolicyKey(key), &policycore.PolicyResult{Result: "block", ClientIP: "10.42.1.42", Domain: "stress.example.com"}, gen)
				c.Load(testPolicyKey(key))
			}
		}(g)
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func BenchmarkPolicyLRU_Load(b *testing.B) {
	c := newPolicyLRU(800 * 1024 * 1024)
	// Pre-populate
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("10.42.1.42:domain%d.example.com", i)
		c.Store(testPolicyKey(key), &policycore.PolicyResult{Result: "block"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Load(testPolicyKey(fmt.Sprintf("10.42.1.42:domain%d.example.com", i%100000)))
	}
}

func BenchmarkPolicyLRU_Store(b *testing.B) {
	c := newPolicyLRU(800 * 1024 * 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("10.42.1.42:domain%d.example.com", i)
		c.Store(testPolicyKey(key), &policycore.PolicyResult{Result: "block"})
	}
}

// testPolicyKey turns the old "ip:domain" string keys into policyKey. A
// string without a parseable IP prefix becomes a domain-only key.
func testPolicyKey(s string) policyKey {
	if i := strings.Index(s, ":"); i > 0 {
		if a, err := netip.ParseAddr(s[:i]); err == nil {
			return policyKey{client: a, domain: s[i+1:], rrtype: 1}
		}
	}
	return policyKey{domain: s, rrtype: 1}
}
