package svart

import (
	"encoding/binary"
	"hash/maphash"
	"net/netip"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/yeti/svart-dns/internal/policycore"
)

const policyCacheShards = 16

// policyKey identifies one cached policy decision. A struct rather than
// ip + ":" + domain: the concatenation collided for IPv6 clients
// ("2001:db8::1" + "2:ads.example" == "2001:db8::1:2" + "ads.example"), so
// one client could plant a decision for another (D14).
type policyKey struct {
	client netip.Addr
	domain string
	rrtype uint16
}

// defaultPolicyCacheMaxBytes is the policy cache budget when
// POLICY_CACHE_SIZE_MB is unset: about 100,000 decisions. An entry costs
// ~560 B of heap (661 B accounted) on a multi-tier configuration, and a miss
// is one snapshot evaluation of a few microseconds, so a large cache buys
// little CPU for a lot of memory. The former 800 MiB held ~1.3M entries.
const defaultPolicyCacheMaxBytes = 64 << 20

// policyLRU is a sharded LRU cache with a byte budget.
// Used to cache evaluatePolicy results and bound memory growth.
//
// generation implements stale-write protection (see DD-002/A3 fix notes in
// policy.go): reloadPolicyState publishes a new policy snapshot and then
// invalidates the cache. Previously "invalidate" meant Clear() — a physical
// wipe of the map — which left a window where an in-flight evaluatePolicyFull
// that started before the reload (and is therefore computing against stale
// tier data) could Store() its result AFTER the Clear() completed, planting a
// stale decision that would then persist until the *next* reload.
//
// Instead of trying to reorder Store/Clear precisely (which just moves the
// race around), every entry is tagged with the generation that was current
// when the evaluation STARTED. Load() only honors an entry if its generation
// still matches the current one; a stale-generation entry is treated as a
// miss (and lazily evicted) regardless of when it was physically written.
// This is race-free by construction — correctness depends only on the
// comparison at Load time, not on any ordering between Store and a reload —
// and cheaper than reordering, since Clear() no longer needs to run
// synchronously with every mutation for correctness (it's still called for
// eager memory reclamation, but a bump-only invalidation would be equally
// correct).
type policyLRU struct {
	shards     [policyCacheShards]lruShard
	seed       maphash.Seed
	maxBytes   atomic.Int64
	hits       atomic.Int64
	misses     atomic.Int64
	generation atomic.Uint64
}

type lruShard struct {
	mu        sync.Mutex
	items     map[policyKey]*lruEntry
	head      *lruEntry // most recently used
	tail      *lruEntry // least recently used (evict from here)
	usedBytes int64
	maxBytes  int64
}

type lruEntry struct {
	key        policyKey
	value      *policycore.PolicyResult
	sizeBytes  int
	generation uint64 // generation of policyLRU in effect when this entry was computed
	prev       *lruEntry
	next       *lruEntry
}

// PolicyCacheStats exposes hit/miss counters and memory usage.
type PolicyCacheStats struct {
	Hits      int64   `json:"hits"`
	Misses    int64   `json:"misses"`
	HitRate   float64 `json:"hit_rate"`
	UsedBytes int64   `json:"used_bytes"`
	Entries   int64   `json:"entries"`
}

func newPolicyLRU(maxBytes int64) *policyLRU {
	c := &policyLRU{seed: maphash.MakeSeed()}
	c.maxBytes.Store(maxBytes)
	perShard := maxBytes / policyCacheShards
	for i := range c.shards {
		c.shards[i].items = make(map[policyKey]*lruEntry)
		c.shards[i].maxBytes = perShard
	}
	return c
}

// setMaxBytes changes the byte budget, evicting least recently used entries
// from shards that are over their new share.
func (c *policyLRU) setMaxBytes(maxBytes int64) {
	c.maxBytes.Store(maxBytes)
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		s.maxBytes = maxBytes / policyCacheShards
		s.evictOverBudget()
		s.mu.Unlock()
	}
}

func (c *policyLRU) shard(key policyKey) *lruShard {
	a := key.client.As16()
	h := maphash.String(c.seed, key.domain) ^ uint64(key.rrtype)*0xD6E8FEB86659FD93 ^
		(binary.LittleEndian.Uint64(a[:8])*0x9E3779B97F4A7C15 + binary.LittleEndian.Uint64(a[8:]))
	return &c.shards[h%policyCacheShards]
}

// Generation returns the cache's current generation counter. Callers that
// want stale-write protection should read this BEFORE computing the value to
// cache (i.e. before touching any of the underlying tier data), then pass it
// to StoreGen once the computation finishes.
func (c *policyLRU) Generation() uint64 {
	return c.generation.Load()
}

// Load retrieves a cached PolicyResult. Returns nil, false on miss.
//
// An entry whose generation no longer matches the cache's current generation
// is treated as a miss and lazily evicted — this is what makes a Store() that
// lands after a reload (with data captured before the reload) harmless: it
// simply becomes unreadable once the generation has moved on. See the
// policyLRU doc comment for the full race this protects against.
func (c *policyLRU) Load(key policyKey) (*policycore.PolicyResult, bool) {
	s := c.shard(key)
	currentGen := c.generation.Load()
	s.mu.Lock()
	e, ok := s.items[key]
	var value *policycore.PolicyResult
	if ok {
		if e.generation != currentGen {
			// Stale entry from before the last invalidation — treat as a
			// miss and reclaim it now rather than waiting for LRU pressure.
			s.remove(e)
			delete(s.items, key)
			s.usedBytes -= int64(e.sizeBytes)
			ok = false
		} else {
			s.moveToFront(e)
			// Capture the value while still holding the lock — reading
			// e.value after Unlock would race with a concurrent StoreGen on
			// the same key mutating the same *lruEntry (pre-existing
			// hazard this stress test exposed; entries are shared, mutable
			// objects, not copy-on-write).
			value = e.value
		}
	}
	s.mu.Unlock()

	if ok {
		c.hits.Add(1)
		return value, true
	}
	c.misses.Add(1)
	return nil, false
}

// Store adds or updates a cache entry, evicting LRU entries if over budget.
// The entry is tagged with the cache's CURRENT generation — suitable for
// callers that don't need stale-write protection (e.g. tests exercising
// basic LRU behavior). Production policy evaluation should use StoreGen with
// a generation captured before computation started; see evaluatePolicy.
func (c *policyLRU) Store(key policyKey, value *policycore.PolicyResult) {
	c.StoreGen(key, value, c.generation.Load())
}

// StoreGen adds or updates a cache entry tagged with the given generation.
// If the cache's current generation has already moved past the given one
// (a reload happened while the value was being computed), the store is
// skipped entirely — the caller's result is known-stale, so there's no point
// spending memory on it. This is a courtesy optimization only: correctness
// does not depend on it, since Load() independently rejects any entry whose
// generation doesn't match the current one, even if it did get stored.
func (c *policyLRU) StoreGen(key policyKey, value *policycore.PolicyResult, generation uint64) {
	if generation != c.generation.Load() {
		return
	}

	size := estimatePolicyResultSize(key, value)
	s := c.shard(key)
	s.mu.Lock()

	if e, ok := s.items[key]; ok {
		// Update existing
		s.usedBytes -= int64(e.sizeBytes)
		e.value = value
		e.sizeBytes = size
		e.generation = generation
		s.usedBytes += int64(size)
		s.moveToFront(e)
	} else {
		// New entry
		e := &lruEntry{key: key, value: value, sizeBytes: size, generation: generation}
		s.items[key] = e
		s.pushFront(e)
		s.usedBytes += int64(size)
	}

	s.evictOverBudget()
	s.mu.Unlock()
}

// Clear removes all entries from all shards and bumps the generation, so any
// entry stored concurrently with (or just after) this call — tagged with a
// generation captured before the bump — is immediately unreadable via Load.
func (c *policyLRU) Clear() {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		s.items = make(map[policyKey]*lruEntry)
		s.head = nil
		s.tail = nil
		s.usedBytes = 0
		s.mu.Unlock()
	}
	c.generation.Add(1)
	c.hits.Store(0)
	c.misses.Store(0)
}

// Stats returns aggregate cache statistics.
func (c *policyLRU) Stats() PolicyCacheStats {
	var usedBytes, entries int64
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		usedBytes += s.usedBytes
		entries += int64(len(s.items))
		s.mu.Unlock()
	}

	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	return PolicyCacheStats{
		Hits:      hits,
		Misses:    misses,
		HitRate:   hitRate,
		UsedBytes: usedBytes,
		Entries:   entries,
	}
}

// --- Doubly-linked list operations (caller must hold s.mu) ---

// evictOverBudget drops least recently used entries until the shard fits.
func (s *lruShard) evictOverBudget() {
	for s.usedBytes > s.maxBytes && s.tail != nil {
		victim := s.tail
		s.removeTail()
		delete(s.items, victim.key)
		s.usedBytes -= int64(victim.sizeBytes)
	}
}

func (s *lruShard) pushFront(e *lruEntry) {
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	}
	s.head = e
	if s.tail == nil {
		s.tail = e
	}
}

func (s *lruShard) moveToFront(e *lruEntry) {
	if s.head == e {
		return
	}
	// Unlink
	if e.prev != nil {
		e.prev.next = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	}
	if s.tail == e {
		s.tail = e.prev
	}
	// Push front
	e.prev = nil
	e.next = s.head
	if s.head != nil {
		s.head.prev = e
	}
	s.head = e
}

func (s *lruShard) removeTail() {
	if s.tail == nil {
		return
	}
	s.remove(s.tail)
}

// remove unlinks an arbitrary entry from the list (not just the tail) —
// used when Load() lazily evicts an entry whose generation has gone stale.
func (s *lruShard) remove(e *lruEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		s.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

// estimatePolicyResultSize returns a rough byte estimate for a cache entry.
// Covers the key, the PolicyResult struct, and nested string fields.
func estimatePolicyResultSize(key policyKey, r *policycore.PolicyResult) int {
	const ptrSize = int(unsafe.Sizeof(uintptr(0)))
	// lruEntry overhead: key string header + prev/next pointers + sizeBytes
	size := 64 + int(unsafe.Sizeof(key)) + len(key.domain) // lruEntry struct + key data

	// PolicyResult base fields
	size += 80 // struct overhead
	size += len(r.ClientIP) + len(r.Domain) + len(r.Result)

	// Tier evaluations
	for _, te := range []*policycore.TierEvaluation{r.RangeEvaluation, r.GroupEvaluation, r.IPEvaluation} {
		if te == nil {
			continue
		}
		size += 48 + len(te.Result) // TierEvaluation struct
		for _, e := range te.Entities {
			size += 64 + len(e.Tier) + len(e.Name) + len(e.Result)
			if e.PublishedList != nil {
				size += 48 + len(e.PublishedList.Action) + len(e.PublishedList.Rule) + len(e.PublishedList.ListName)
			}
			if e.CustomRule != nil {
				size += 32 + len(e.CustomRule.Action) + len(e.CustomRule.Rule)
			}
		}
	}

	// ResultSource (shares data with tier entities, count pointer only)
	if r.ResultSource != nil {
		size += ptrSize
	}

	return size
}
