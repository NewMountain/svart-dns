package svart

import (
	"encoding/binary"
	"errors"
	"hash/maphash"
	"math"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

// minCacheTTLSeconds floors the effective cache TTL to avoid thrashing when
// an upstream answer carries a near-zero TTL (some fast-failover/GSLB/CDN
// setups do this intentionally). It never overrides an operator's explicit
// configured cap that's itself below the floor (see effectiveCacheTTL) — an
// admin who deliberately configures a 1s cache TTL still gets 1s.
const minCacheTTLSeconds = 5

// maxNegativeCacheTTLSeconds caps how long NXDOMAIN/NODATA answers are cached
// and what the client is told (RFC 2308 section 5 recommends 1-3 hours as an
// upper bound; 5 minutes keeps a newly created name from staying invisible).
const maxNegativeCacheTTLSeconds = 300

const (
	// defaultCacheMaxBytes is the response cache budget when DNS_CACHE_SIZE_MB
	// is unset: about 140k typical answers at ~470 accounted bytes each.
	defaultCacheMaxBytes = 64 << 20
	cacheShards          = 32
	cacheSweepInterval   = time.Minute
	// cacheEntryOverhead is the heap cost of one entry beyond its
	// variable-length fields: the cachedAnswer struct (160 B size class),
	// the sync.Map trie entry and amortized indirect nodes, the key string,
	// the ring slot and allocator rounding. Measured on 300k entries: 442 B
	// of heap per NXDOMAIN answer (124 B variable) and 433 B per A answer
	// (107 B variable), i.e. ~320 B fixed; 352 keeps the accounted budget an
	// upper bound on real memory (TestCacheMemoryIsBounded).
	cacheEntryOverhead = 352
	// replyBufMaxPooled bounds the buffers kept in replyBufPool so one huge
	// TCP answer does not pin 64 KiB per pool slot.
	replyBufMaxPooled = 4096
)

// fastClock is a coalesced clock that avoids calling time.Now() on every hot-path
// operation. A background goroutine updates the shared timestamp every 500μs.
// Readers get ~500μs resolution — irrelevant for TTL checks (minutes/hours) but
// saves ~35ns per call vs time.Now()'s ~40ns vDSO cost. On the cache hot path
// (get + set per query), this reclaims ~70ns per DNS query.
type fastClock struct {
	now   atomic.Value // stores time.Time
	nanos atomic.Int64 // the same instant as unix nanoseconds
}

var clock = newFastClock()

func newFastClock() *fastClock {
	c := &fastClock{}
	t := time.Now()
	c.now.Store(t)
	c.nanos.Store(t.UnixNano())
	go func() {
		ticker := time.NewTicker(500 * time.Microsecond)
		defer ticker.Stop()
		for t := range ticker.C {
			c.now.Store(t)
			c.nanos.Store(t.UnixNano())
		}
	}()
	return c
}

func (c *fastClock) Now() time.Time {
	now, ok := c.now.Load().(time.Time)
	if !ok {
		panic("fast clock has an invalid internal type")
	}
	return now
}

// NowUnixNano is Now() as an integer: one atomic load, no interface
// assertion, used for cache expiry arithmetic on every query.
func (c *fastClock) NowUnixNano() int64 {
	return c.nanos.Load()
}

// dnsCacheKey identifies one cacheable answer. Only class IN is ever cached
// (other classes are refused before the cache), so the class is implied. DO
// and CD change what the upstream returns (RRSIGs, unvalidated data), so
// they are part of the key. A struct key avoids building a string per query.
type dnsCacheKey struct {
	name  string // lowercase FQDN, trailing dot
	qtype uint16
	do    bool
	cd    bool
}

func cacheKeyFor(req *dns.Msg) dnsCacheKey {
	q := req.Question[0]
	k := dnsCacheKey{name: lowerASCII(q.Name), qtype: q.Qtype, cd: req.CheckingDisabled}
	if opt := req.IsEdns0(); opt != nil {
		k.do = opt.Do()
	}
	return k
}

// lowerASCII lowercases A-Z only. Names from miekg are ASCII (other bytes
// are \DDD-escaped), and returning s itself when it has no upper case keeps
// the common all-lowercase query allocation-free.
func lowerASCII(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// cachedAnswer is one upstream answer in the form it is served: packed wire
// bytes (ID 0, no OPT, lowercase question) plus the offsets of every TTL
// field. A hit copies the bytes and patches ID, flags, remaining TTLs and the
// client's 0x20 spelling. The previous design stored a *dns.Msg and paid
// Msg.Copy + Pack per hit (5 allocs + ~2 µs under load, BenchmarkCacheGet_Hit).
//
// The fields after ttlOffs are the per-answer facts the handler needs on
// every serve, extracted once so hits never unpack: the CNAME/DNAME/SVCB
// targets for cloaking checks (D12), the A/AAAA addresses for IP-literal
// blocking, and whether any address is private (D13).
type cachedAnswer struct {
	key       dnsCacheKey
	wire      []byte
	ttlOffs   []uint16
	storedAt  int64 // unix ns
	expiresAt int64 // unix ns; storedAt when not cacheable
	targets   []responseTarget
	ips       []string
	privateIP bool
	size      int64

	ref  atomic.Bool // CLOCK reference bit, set on hits
	dead bool        // replaced or removed; guarded by the shard lock
}

func (e *cachedAnswer) rcode() int { return int(e.wire[3] & 0x0f) }

var errCacheWire = errors.New("dns cache: malformed packed answer")

// newCachedAnswer packs resp for serving under key. lifetime > 0 caps every
// TTL so no client is told to keep the data longer than we will; 0 means the
// answer is served once and not stored (TTLs pass through unchanged).
func newCachedAnswer(key dnsCacheKey, resp *dns.Msg, lifetime int, now int64) (*cachedAnswer, error) {
	m := dns.Msg{MsgHdr: resp.MsgHdr, Compress: true}
	m.Id = 0
	m.Response = true
	m.Opcode = dns.OpcodeQuery
	m.Authoritative = false
	m.Truncated = false
	m.RecursionDesired = false
	m.RecursionAvailable = true
	m.Zero = false
	m.CheckingDisabled = false
	m.Question = []dns.Question{{Name: key.name, Qtype: key.qtype, Qclass: dns.ClassINET}}
	m.Answer = resp.Answer
	m.Ns = resp.Ns
	for _, rr := range resp.Extra {
		if rr.Header().Rrtype != dns.TypeOPT {
			m.Extra = append(m.Extra, rr)
		}
	}
	packed, err := m.Pack()
	if err != nil {
		return nil, err
	}
	// Pack sizes its buffer for the uncompressed message; an exact copy
	// saves ~50 B per entry (measured, TestCacheMemoryIsBounded).
	wire := append(make([]byte, 0, len(packed)), packed...)
	offs, err := ttlOffsets(wire)
	if err != nil {
		return nil, err
	}
	if lifetime > 0 {
		if uint64(lifetime) > math.MaxUint32 {
			return nil, errors.New("DNS cache lifetime exceeds wire TTL range")
		}
		for _, off := range offs {
			if binary.BigEndian.Uint32(wire[off:]) > uint32(lifetime) {
				binary.BigEndian.PutUint32(wire[off:], uint32(lifetime))
			}
		}
	}

	e := &cachedAnswer{
		key:       key,
		wire:      wire,
		ttlOffs:   offs,
		storedAt:  now,
		expiresAt: now + int64(lifetime)*int64(time.Second),
	}
	self := strings.TrimSuffix(key.name, ".")
	for _, rr := range resp.Answer {
		switch v := rr.(type) {
		case *dns.CNAME:
			e.addTarget(v.Target, self, v.Hdr.Rrtype)
		case *dns.DNAME:
			e.addTarget(v.Target, self, v.Hdr.Rrtype)
		case *dns.SVCB:
			e.addTarget(v.Target, self, v.Hdr.Rrtype)
		case *dns.HTTPS:
			e.addTarget(v.Target, self, v.Hdr.Rrtype)
		case *dns.A:
			e.addIP(v.A)
		case *dns.AAAA:
			e.addIP(v.AAAA)
		}
	}
	e.size = int64(cacheEntryOverhead + len(wire) + len(key.name) + 2*len(offs))
	for _, t := range e.targets {
		e.size += int64(24 + len(t.name))
	}
	for _, ip := range e.ips {
		e.size += int64(16 + len(ip))
	}
	return e, nil
}

type responseTarget struct {
	name   string
	rrtype uint16
}

func (e *cachedAnswer) addTarget(target, self string, rrtype uint16) {
	t := strings.TrimSuffix(lowerASCII(target), ".")
	if t == "" || t == self { // SVCB "." means the owner name itself
		return
	}
	for _, have := range e.targets {
		if have.name == t && have.rrtype == rrtype {
			return
		}
	}
	e.targets = append(e.targets, responseTarget{name: t, rrtype: rrtype})
}

func (e *cachedAnswer) addIP(ip net.IP) {
	e.ips = append(e.ips, ip.String())
	if a, ok := netip.AddrFromSlice(ip); ok && isRebindAddress(a.Unmap()) {
		e.privateIP = true
	}
}

// ttlOffsets walks a packed message and returns the offset of every RR's TTL
// field, skipping OPT (whose "TTL" holds EDNS flags).
func ttlOffsets(wire []byte) ([]uint16, error) {
	if len(wire) < 12 || len(wire) > dns.MaxMsgSize {
		return nil, errCacheWire
	}
	qd := int(binary.BigEndian.Uint16(wire[4:]))
	rrs := int(binary.BigEndian.Uint16(wire[6:])) + int(binary.BigEndian.Uint16(wire[8:])) + int(binary.BigEndian.Uint16(wire[10:]))
	off := 12
	var err error
	for i := 0; i < qd; i++ {
		if off, err = skipWireName(wire, off); err != nil {
			return nil, err
		}
		off += 4
	}
	offs := make([]uint16, 0, rrs)
	for i := 0; i < rrs; i++ {
		if off, err = skipWireName(wire, off); err != nil {
			return nil, err
		}
		if off+10 > len(wire) {
			return nil, errCacheWire
		}
		if binary.BigEndian.Uint16(wire[off:]) != dns.TypeOPT {
			ttlOffset := off + 4
			if ttlOffset < 0 || ttlOffset > math.MaxUint16 {
				return nil, errCacheWire
			}
			offs = append(offs, uint16(ttlOffset))
		}
		off += 10 + int(binary.BigEndian.Uint16(wire[off+8:]))
	}
	if off > len(wire) {
		return nil, errCacheWire
	}
	return offs, nil
}

func skipWireName(wire []byte, off int) (int, error) {
	for {
		if off >= len(wire) {
			return 0, errCacheWire
		}
		l := int(wire[off])
		switch {
		case l == 0:
			return off + 1, nil
		case l&0xC0 == 0xC0:
			return off + 2, nil
		case l&0xC0 != 0:
			return 0, errCacheWire
		}
		off += l + 1
	}
}

// ednsOPTWire is our OPT RR: root owner, TYPE 41, CLASS = payload size, TTL =
// extended rcode 0, version 0, flags (DO patched in), RDLEN 0.
var ednsOPTWire = [11]byte{0, 0, 41, byte(ednsAdvertisedSize >> 8), byte(ednsAdvertisedSize & 0xff), 0, 0, 0, 0, 0, 0}

// appendReply appends the answer to req to buf. It never allocates when buf
// has room: that is the whole point of storing wire bytes.
func (e *cachedAnswer) appendReply(buf []byte, req *dns.Msg, now int64) []byte {
	start := len(buf)
	buf = append(buf, e.wire...)
	b := buf[start:]
	binary.BigEndian.PutUint16(b[0:], req.Id)
	flags := byte(0x80) // QR; opcode QUERY, AA and TC clear
	if req.RecursionDesired {
		flags |= 0x01
	}
	b[2] = flags
	flags = e.wire[3] & 0x8f // RA + rcode
	opt := req.IsEdns0()
	// AD only when the client can use it (RFC 6840 section 5.8).
	if e.wire[3]&0x20 != 0 && ((opt != nil && opt.Do()) || req.AuthenticatedData) {
		flags |= 0x20
	}
	if req.CheckingDisabled {
		flags |= 0x10
	}
	b[3] = flags

	elapsed := (now - e.storedAt) / int64(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	for _, off := range e.ttlOffs {
		ttl := int64(binary.BigEndian.Uint32(e.wire[off:])) - elapsed
		if ttl < 0 {
			ttl = 0
		}
		binary.BigEndian.PutUint32(b[off:], uint32(ttl))
	}

	if qn := req.Question[0].Name; qn != e.key.name {
		restoreQuestionCase(b, qn)
	}

	if opt != nil {
		buf = append(buf, ednsOPTWire[:]...)
		if opt.Do() {
			buf[len(buf)-4] = 0x80
		}
		b = buf[start:]
		binary.BigEndian.PutUint16(b[10:], binary.BigEndian.Uint16(b[10:])+1)
	}
	return buf
}

// restoreQuestionCase copies the client's spelling of the query name into the
// packed question, so resolvers using 0x20 randomization accept the answer.
// Owner names that were compressed against the question follow along.
func restoreQuestionCase(b []byte, name string) {
	if strings.IndexByte(name, '\\') >= 0 {
		return // escaped presentation form does not map byte-for-byte
	}
	pos, p := 12, 0
	for pos < len(b) {
		l := int(b[pos])
		if l == 0 || l&0xC0 != 0 || p+l > len(name) || pos+1+l > len(b) {
			return
		}
		copy(b[pos+1:pos+1+l], name[p:p+l])
		pos += l + 1
		p += l + 1
	}
}

// replyBufPool recycles hit-path reply buffers: miekg's UDP Write is
// synchronous and its TCP Write copies, so the buffer is free after Write.
var replyBufPool = sync.Pool{New: func() any {
	b := make([]byte, 0, 1500)
	return &b
}}

// dnsCache is the bounded response cache. Reads are one lock-free sync.Map
// Load (a concurrent hash trie since Go 1.24) plus an atomic reference bit;
// all bookkeeping for the byte budget lives in sharded CLOCK rings touched
// only on insert, eviction and the periodic sweep. An earlier bounded design
// took a mutex and moved an LRU node on every hit and was rejected for
// regressing BenchmarkCacheGet_Parallel.
type dnsCache struct {
	entries  sync.Map // dnsCacheKey → *cachedAnswer
	shards   [cacheShards]cacheShard
	seed     maphash.Seed
	maxBytes atomic.Int64

	hits        atomic.Int64
	misses      atomic.Int64
	liveEntries atomic.Int64
	liveBytes   atomic.Int64
	evicted     atomic.Int64
	expired     atomic.Int64
	peakBytes   atomic.Int64
	peakEntries atomic.Int64
}

type cacheShard struct {
	mu    sync.Mutex
	ring  []*cachedAnswer // entries in insertion order, possibly dead
	hand  int
	bytes int64
	live  int
}

func newDNSCache(maxBytes int64) *dnsCache {
	c := &dnsCache{seed: maphash.MakeSeed()}
	c.maxBytes.Store(maxBytes)
	return c
}

var cache = newDNSCache(defaultCacheMaxBytes)

// CacheStats summarizes DNS cache usage and memory estimates.
type CacheStats struct {
	Hits           int64
	Misses         int64
	HitRate        int
	Entries        int
	EstimatedBytes int64
	PeakEntries    int64
	PeakBytes      int64
	Expired        int64
	Evicted        int64
	MaxBytes       int64
}

// get returns the live answer for key. Expired entries read as misses and
// are reclaimed by the sweep or by the refresh that replaces them.
func (c *dnsCache) get(key dnsCacheKey, now int64) (*cachedAnswer, bool) {
	v, ok := c.entries.Load(key)
	if !ok {
		c.misses.Add(1)
		return nil, false
	}
	e, ok := v.(*cachedAnswer)
	if !ok {
		panic("DNS cache entry has an invalid internal type")
	}
	if now >= e.expiresAt {
		c.misses.Add(1)
		return nil, false
	}
	if !e.ref.Load() { // read first: keeps hot entries' cache line shared
		e.ref.Store(true)
	}
	c.hits.Add(1)
	return e, true
}

// set packs resp and stores it if it is cacheable (see effectiveCacheTTL).
// It returns the packed answer either way, or nil for an answer that is not
// servable from the cache form at all (not NOERROR/NXDOMAIN, TC, or wrong
// question).
func (c *dnsCache) set(key dnsCacheKey, resp *dns.Msg, configuredTTL int) *cachedAnswer {
	if !cacheableAnswer(key, resp) {
		return nil
	}
	lifetime := effectiveCacheTTL(resp, configuredTTL)
	e, err := newCachedAnswer(key, resp, lifetime, clock.NowUnixNano())
	if err != nil {
		return nil
	}
	if lifetime > 0 {
		c.insert(e)
	}
	return e
}

// cacheableAnswer: only a complete NOERROR or NXDOMAIN answer to exactly the
// question asked may be stored or served in cached form. SERVFAIL, REFUSED
// and TC=1 answers are transient (D3); an answer to another question is a
// poisoning attempt (D2).
func cacheableAnswer(key dnsCacheKey, resp *dns.Msg) bool {
	if resp.Truncated || (resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError) {
		return false
	}
	if len(resp.Question) != 1 {
		return false
	}
	q := resp.Question[0]
	return q.Qtype == key.qtype && q.Qclass == dns.ClassINET && strings.EqualFold(q.Name, key.name)
}

func (c *dnsCache) shardFor(key dnsCacheKey) *cacheShard {
	h := maphash.String(c.seed, key.name) ^ uint64(key.qtype)*0x9E3779B97F4A7C15
	return &c.shards[h%cacheShards]
}

func (c *dnsCache) insert(e *cachedAnswer) {
	budget := c.maxBytes.Load() / cacheShards
	if e.size > budget {
		return
	}
	s := c.shardFor(e.key)
	s.mu.Lock()
	if prev, loaded := c.entries.Swap(e.key, e); loaded {
		p, ok := prev.(*cachedAnswer)
		if !ok {
			panic("DNS cache replacement has an invalid internal type")
		}
		if !p.dead {
			p.dead = true
			c.account(s, -p.size, -1)
		}
	}
	c.account(s, e.size, 1)
	if !c.evictLocked(s, budget, e) {
		s.ring = append(s.ring, e)
	}
	s.mu.Unlock()
	c.updatePeaks()
}

// evictLocked runs the CLOCK hand until the shard is within budget. Dead
// slots are reclaimed, expired entries go first, and an entry read since the
// hand last passed gets a second chance. The first freed slot receives e
// (reporting true), just behind the hand, so a new entry gets a full
// revolution to be read before it is considered; it starts unreferenced, so
// a one-shot flood name loses to anything that was read again.
func (c *dnsCache) evictLocked(s *cacheShard, budget int64, e *cachedAnswer) (placed bool) {
	now := e.storedAt
	for s.bytes > budget && len(s.ring) > 0 {
		if s.hand >= len(s.ring) {
			s.hand = 0
		}
		v := s.ring[s.hand]
		if !v.dead {
			if now < v.expiresAt && v.ref.Load() {
				v.ref.Store(false)
				s.hand++
				continue
			}
			c.entries.CompareAndDelete(v.key, v)
			v.dead = true
			c.account(s, -v.size, -1)
			c.evicted.Add(1)
		}
		if placed {
			s.removeAt(s.hand)
		} else {
			s.ring[s.hand] = e
			s.hand++
			placed = true
		}
	}
	return placed
}

func (s *cacheShard) removeAt(i int) {
	last := len(s.ring) - 1
	s.ring[i] = s.ring[last]
	s.ring[last] = nil
	s.ring = s.ring[:last]
}

// sweep drops expired entries and compacts dead ring slots.
func (c *dnsCache) sweep(now int64) {
	removed := int64(0)
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		kept := s.ring[:0]
		for _, v := range s.ring {
			if v.dead {
				continue
			}
			if now >= v.expiresAt {
				c.entries.CompareAndDelete(v.key, v)
				v.dead = true
				c.account(s, -v.size, -1)
				removed++
				continue
			}
			kept = append(kept, v)
		}
		clear(s.ring[len(kept):])
		s.ring = kept
		s.hand = 0
		s.mu.Unlock()
	}
	if removed > 0 {
		c.expired.Add(removed)
		if logCache != nil {
			logCache.Debug("cleaned up expired cache entries", "deleted", removed)
		}
	}
}

// setMaxBytes changes the budget; shards shrink on their next insert or sweep.
func (c *dnsCache) setMaxBytes(n int64) {
	c.maxBytes.Store(n)
}

func (c *dnsCache) clear() {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		for _, v := range s.ring {
			v.dead = true
		}
		s.ring = nil
		s.hand = 0
		c.account(s, -s.bytes, -s.live)
		s.mu.Unlock()
	}
	c.entries.Clear()
	c.hits.Store(0)
	c.misses.Store(0)
	c.evicted.Store(0)
	c.expired.Store(0)
	c.peakBytes.Store(0)
	c.peakEntries.Store(0)
}

// account moves a shard's counters and the cache-wide atomics together.
// Caller holds s.mu.
func (c *dnsCache) account(s *cacheShard, bytes int64, entries int) {
	s.bytes += bytes
	s.live += entries
	c.liveBytes.Add(bytes)
	c.liveEntries.Add(int64(entries))
}

// totals reads the cache-wide atomics. Summing the shards under their locks
// on every insert (for peak tracking) cost 32 lock round trips per miss.
func (c *dnsCache) totals() (entries int, bytes int64) {
	return int(c.liveEntries.Load()), c.liveBytes.Load()
}

func (c *dnsCache) updatePeaks() {
	entries, bytes := c.totals()
	raisePeak(&c.peakEntries, int64(entries))
	raisePeak(&c.peakBytes, bytes)
}

func raisePeak(counter *atomic.Int64, current int64) {
	for {
		peak := counter.Load()
		if current <= peak || counter.CompareAndSwap(peak, current) {
			return
		}
	}
}

func (c *dnsCache) stats() CacheStats {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses

	hitRate := 0
	if total > 0 {
		hitRate = int((hits * 100) / total)
	}
	entries, bytes := c.totals()
	return CacheStats{
		Hits:           hits,
		Misses:         misses,
		HitRate:        hitRate,
		Entries:        entries,
		EstimatedBytes: bytes,
		PeakEntries:    c.peakEntries.Load(),
		PeakBytes:      c.peakBytes.Load(),
		Expired:        c.expired.Load(),
		Evicted:        c.evicted.Load(),
		MaxBytes:       c.maxBytes.Load(),
	}
}

// effectiveCacheTTL is how many seconds resp may be cached (0 = never):
//
//   - Only complete NOERROR/NXDOMAIN answers are cacheable. SERVFAIL,
//     REFUSED and TC=1 answers are transient and caching them turned one
//     upstream hiccup into an hour-long outage (D3).
//   - Positive answers: the smallest answer RR TTL.
//   - Negative answers (NXDOMAIN, NODATA): min(SOA TTL, SOA MINIMUM) from
//     the authority section (RFC 2308 section 5), capped at
//     maxNegativeCacheTTLSeconds. Without an SOA they are not cached
//     (RFC 2308: SHOULD NOT).
//
// The result is capped at configuredTTL (never cache longer than the admin
// configured) and floored at minCacheTTLSeconds — except the floor itself
// never exceeds configuredTTL, so an operator who deliberately configures a
// TTL below the floor (e.g. for testing) isn't overridden.
func effectiveCacheTTL(msg *dns.Msg, configuredTTL int) int {
	if configuredTTL <= 0 || msg == nil || msg.Truncated {
		return 0
	}
	if msg.Rcode != dns.RcodeSuccess && msg.Rcode != dns.RcodeNameError {
		return 0
	}

	ttl := -1
	if msg.Rcode == dns.RcodeSuccess {
		for _, rr := range msg.Answer {
			if t := int(rr.Header().Ttl); ttl == -1 || t < ttl {
				ttl = t
			}
		}
	}
	if ttl == -1 {
		for _, rr := range msg.Ns {
			if soa, ok := rr.(*dns.SOA); ok {
				t := int(soa.Hdr.Ttl)
				if int(soa.Minttl) < t {
					t = int(soa.Minttl)
				}
				if ttl == -1 || t < ttl {
					ttl = t
				}
			}
		}
		if ttl == -1 {
			return 0
		}
		if ttl > maxNegativeCacheTTLSeconds {
			ttl = maxNegativeCacheTTLSeconds
		}
	}

	if ttl > configuredTTL {
		ttl = configuredTTL
	}
	floor := minCacheTTLSeconds
	if floor > configuredTTL {
		floor = configuredTTL
	}
	if ttl < floor {
		ttl = floor
	}
	return ttl
}

func clearDNSCache() {
	cache.clear()
}

func getCacheStats() CacheStats {
	return cache.stats()
}

func startCacheCleanupWorker() {
	sweepTicker := time.NewTicker(cacheSweepInterval)
	defer sweepTicker.Stop()
	for t := range sweepTicker.C {
		cache.sweep(t.UnixNano())
	}
}
