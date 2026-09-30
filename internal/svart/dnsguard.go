package svart

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// dnsGuardConfig is the DNS listener's abuse-control configuration, parsed
// once from the environment at startup (loadDNSGuardConfig) and read on the
// hot path through an atomic pointer.
type dnsGuardConfig struct {
	// allowedClients is the source-address ACL. Queries from anywhere else
	// are answered REFUSED before any rewrite, policy, cache or upstream work
	// (D1: an open resolver is an amplification weapon).
	allowedClients []netip.Prefix
	// rateLimit is nil when per-client rate limiting is off (the default).
	rateLimit *clientRateLimiter
	// tcpMaxConns / tcpMaxConnsPerIP bound concurrently open TCP connections
	// (each one is a goroutine held for the read and idle timeouts).
	tcpMaxConns      int
	tcpMaxConnsPerIP int
	// rebind is the optional DNS-rebinding filter (D13); nil = off.
	rebind *rebindFilter
	// cacheMaxBytes is the response cache budget (DNS_CACHE_SIZE_MB).
	cacheMaxBytes int64
	// policyCacheMaxBytes is the policy decision cache budget
	// (POLICY_CACHE_SIZE_MB).
	policyCacheMaxBytes int64
}

// defaultAllowedClients are the networks a home or office resolver serves:
// loopback, RFC 1918, CGNAT (RFC 6598, also Tailscale), link-local and ULA.
// IPv4-mapped IPv6 sources are unmapped before matching, so they are covered.
var defaultAllowedClients = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

const (
	defaultTCPMaxConns      = 1000
	defaultTCPMaxConnsPerIP = 32
	// dnsTCPIdleTimeout is how long an established TCP connection may sit
	// between queries (miekg's default is 8 s). Stub resolvers use one
	// connection per query, so 3 s only costs forwarders a reconnect.
	dnsTCPIdleTimeout = 3 * time.Second
	// dnsTCPMaxQueries caps pipelined queries per TCP connection.
	dnsTCPMaxQueries = 128
	// dnsReadTimeout / dnsWriteTimeout bound a single read or write.
	dnsReadTimeout  = 2 * time.Second
	dnsWriteTimeout = 2 * time.Second
)

var dnsGuard atomic.Pointer[dnsGuardConfig]

func init() {
	dnsGuard.Store(&dnsGuardConfig{
		allowedClients:      defaultAllowedClients,
		tcpMaxConns:         defaultTCPMaxConns,
		tcpMaxConnsPerIP:    defaultTCPMaxConnsPerIP,
		cacheMaxBytes:       defaultCacheMaxBytes,
		policyCacheMaxBytes: defaultPolicyCacheMaxBytes,
	})
}

// loadDNSGuardConfig parses the DNS abuse controls from the environment.
// Invalid values are a startup error rather than a silent fallback: an
// operator who wrote ALLOWED_CLIENTS meant something specific, and guessing
// could either expose the resolver or lock the network out.
func loadDNSGuardConfig(getenv func(string) string) (*dnsGuardConfig, error) {
	cfg := &dnsGuardConfig{
		allowedClients:      defaultAllowedClients,
		tcpMaxConns:         defaultTCPMaxConns,
		tcpMaxConnsPerIP:    defaultTCPMaxConnsPerIP,
		cacheMaxBytes:       defaultCacheMaxBytes,
		policyCacheMaxBytes: defaultPolicyCacheMaxBytes,
	}

	if raw := strings.TrimSpace(getenv("ALLOWED_CLIENTS")); raw != "" {
		prefixes, err := parseAllowedClients(raw)
		if err != nil {
			return nil, err
		}
		cfg.allowedClients = prefixes
	}

	qps, err := parseNonNegativeInt(getenv, "DNS_RATE_LIMIT_QPS", 0)
	if err != nil {
		return nil, err
	}
	if qps > 0 {
		cfg.rateLimit = newClientRateLimiter(float64(qps), 2*float64(qps))
	}

	if cfg.tcpMaxConns, err = parsePositiveInt(getenv, "DNS_TCP_MAX_CONNS", defaultTCPMaxConns); err != nil {
		return nil, err
	}
	if cfg.tcpMaxConnsPerIP, err = parsePositiveInt(getenv, "DNS_TCP_MAX_CONNS_PER_IP", defaultTCPMaxConnsPerIP); err != nil {
		return nil, err
	}
	if cfg.rebind, err = parseRebindConfig(getenv); err != nil {
		return nil, err
	}
	cacheMB, err := parsePositiveInt(getenv, "DNS_CACHE_SIZE_MB", defaultCacheMaxBytes>>20)
	if err != nil {
		return nil, err
	}
	if int64(cacheMB) > math.MaxInt64>>20 {
		return nil, fmt.Errorf("DNS_CACHE_SIZE_MB: byte budget exceeds int64 capacity")
	}
	cfg.cacheMaxBytes = int64(cacheMB) << 20
	policyMB, err := parsePositiveInt(getenv, "POLICY_CACHE_SIZE_MB", defaultPolicyCacheMaxBytes>>20)
	if err != nil {
		return nil, err
	}
	if int64(policyMB) > math.MaxInt64>>20 {
		return nil, fmt.Errorf("POLICY_CACHE_SIZE_MB: byte budget exceeds int64 capacity")
	}
	cfg.policyCacheMaxBytes = int64(policyMB) << 20
	return cfg, nil
}

// parseAllowedClients parses a comma-separated list of CIDRs or bare
// addresses. IPv4-mapped IPv6 prefixes are unmapped so they match the
// unmapped source addresses the handler compares against.
func parseAllowedClients(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		var p netip.Prefix
		if strings.Contains(field, "/") {
			parsed, err := netip.ParsePrefix(field)
			if err != nil {
				return nil, fmt.Errorf("ALLOWED_CLIENTS: invalid CIDR %q: %w", field, err)
			}
			p = parsed
		} else {
			a, err := netip.ParseAddr(field)
			if err != nil {
				return nil, fmt.Errorf("ALLOWED_CLIENTS: invalid address %q: %w", field, err)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if p.Addr().Is4In6() {
			bits := p.Bits() - 96
			if bits < 0 {
				return nil, fmt.Errorf("ALLOWED_CLIENTS: IPv4-mapped prefix %q is shorter than ::ffff:0:0/96", field)
			}
			p = netip.PrefixFrom(p.Addr().Unmap(), bits)
		}
		prefixes = append(prefixes, p.Masked())
	}
	if len(prefixes) == 0 {
		return nil, fmt.Errorf("ALLOWED_CLIENTS is set but contains no CIDRs")
	}
	return prefixes, nil
}

func parseNonNegativeInt(getenv func(string) string, key string, def int) (int, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: want a non-negative integer, got %q", key, raw)
	}
	return n, nil
}

func parsePositiveInt(getenv func(string) string, key string, def int) (int, error) {
	n, err := parseNonNegativeInt(getenv, key, def)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, fmt.Errorf("%s: want a positive integer, got 0", key)
	}
	return n, nil
}

// rateLimitQPS is the configured per-client rate (0 = off), for startup logs.
func (c *dnsGuardConfig) rateLimitQPS() float64 {
	if c.rateLimit == nil {
		return 0
	}
	return c.rateLimit.rate
}

// clientAllowed reports whether addr (already unmapped) may use the resolver.
func (c *dnsGuardConfig) clientAllowed(addr netip.Addr) bool {
	for _, p := range c.allowedClients {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// addrFromNetAddr extracts the unmapped source address from a socket
// address without allocating for the UDP/TCP cases.
func addrFromNetAddr(a net.Addr) netip.Addr {
	switch v := a.(type) {
	case *net.UDPAddr:
		return v.AddrPort().Addr().Unmap()
	case *net.TCPAddr:
		return v.AddrPort().Addr().Unmap()
	case nil:
		return netip.Addr{}
	default:
		ap, err := netip.ParseAddrPort(a.String())
		if err != nil {
			return netip.Addr{}
		}
		return ap.Addr().Unmap()
	}
}

// --- counters (exported at scrape time by metrics.go) -----------------------

// dnsGuardStats counts abuse-control outcomes. Plain atomics rather than
// Prometheus vectors: these fire on the hot path, and a CounterFunc reads them
// at scrape time (DD-015).
var dnsGuardStats struct {
	aclRefused    atomic.Int64
	rateLimited   atomic.Int64
	tcpRejected   atomic.Int64
	handlerPanics atomic.Int64
	overloadShed  atomic.Int64
}

// --- per-client rate limiting (opt-in) --------------------------------------

const (
	rateLimitShards = 64
	// rateLimitShardCap bounds each shard's bucket map. The ACL runs first,
	// so keys come only from allowed networks, but a /8 or ULA allowance is
	// still large; past the cap idle buckets are swept, and if the shard is
	// still full the query is allowed (an optional abuse control must not
	// turn into a way to deny service to new clients).
	rateLimitShardCap = 4096
	rateLimitIdle     = 60 * time.Second
)

// clientRateLimiter is a sharded token bucket keyed by the client's /24
// (IPv4) or /56 (IPv6), the usual allocation to one household or site.
type clientRateLimiter struct {
	rate   float64 // tokens per second
	burst  float64
	shards [rateLimitShards]rateLimitShard
}

type rateLimitShard struct {
	mu      sync.Mutex
	buckets map[netip.Prefix]tokenBucket
}

type tokenBucket struct {
	tokens float64
	last   int64 // unix nanoseconds
}

func newClientRateLimiter(rate, burst float64) *clientRateLimiter {
	l := &clientRateLimiter{rate: rate, burst: burst}
	for i := range l.shards {
		l.shards[i].buckets = make(map[netip.Prefix]tokenBucket)
	}
	return l
}

func rateLimitKey(addr netip.Addr) netip.Prefix {
	bits := 56
	if addr.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(addr, bits).Masked()
}

func (l *clientRateLimiter) allow(addr netip.Addr, now int64) bool {
	if !addr.IsValid() {
		return false
	}
	key := rateLimitKey(addr)
	b := key.Addr().As16()
	h := (uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[12])) * 0x9E3779B97F4A7C15
	s := &l.shards[h>>(64-6)]

	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, ok := s.buckets[key]
	if !ok {
		if len(s.buckets) >= rateLimitShardCap {
			s.sweep(now)
			if len(s.buckets) >= rateLimitShardCap {
				return true
			}
		}
		bucket = tokenBucket{tokens: l.burst, last: now}
	}
	elapsed := float64(now-bucket.last) / 1e9
	if elapsed > 0 {
		bucket.tokens += elapsed * l.rate
		if bucket.tokens > l.burst {
			bucket.tokens = l.burst
		}
		bucket.last = now
	}
	allowed := bucket.tokens >= 1
	if allowed {
		bucket.tokens--
	}
	s.buckets[key] = bucket
	return allowed
}

func (s *rateLimitShard) sweep(now int64) {
	for k, b := range s.buckets {
		if now-b.last > int64(rateLimitIdle) {
			delete(s.buckets, k)
		}
	}
}

// --- TCP connection limits --------------------------------------------------

// limitedListener enforces the ACL at accept time and caps concurrently open
// connections globally and per source address. Rejected connections are
// closed immediately, before miekg spends a goroutine and a read timeout on
// them.
type limitedListener struct {
	net.Listener
	guard *dnsGuardConfig
	mu    sync.Mutex
	total int
	perIP map[netip.Addr]int
}

func newLimitedListener(l net.Listener, guard *dnsGuardConfig) *limitedListener {
	return &limitedListener{Listener: l, guard: guard, perIP: make(map[netip.Addr]int)}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		addr := addrFromNetAddr(c.RemoteAddr())
		if !l.guard.clientAllowed(addr) {
			dnsGuardStats.aclRefused.Add(1)
			if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				dnsErrorLogger(logDNS, err).Warn("failed to close rejected DNS connection")
			}
			continue
		}
		l.mu.Lock()
		if l.total >= l.guard.tcpMaxConns || l.perIP[addr] >= l.guard.tcpMaxConnsPerIP {
			l.mu.Unlock()
			dnsGuardStats.tcpRejected.Add(1)
			if err := c.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				dnsErrorLogger(logDNS, err).Warn("failed to close rejected DNS connection")
			}
			continue
		}
		l.total++
		l.perIP[addr]++
		l.mu.Unlock()
		return &limitedConn{Conn: c, listener: l, addr: addr}, nil
	}
}

func (l *limitedListener) release(addr netip.Addr) {
	l.mu.Lock()
	l.total--
	if n := l.perIP[addr] - 1; n > 0 {
		l.perIP[addr] = n
	} else {
		delete(l.perIP, addr)
	}
	l.mu.Unlock()
}

type limitedConn struct {
	net.Conn
	listener *limitedListener
	addr     netip.Addr
	once     sync.Once
}

func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.listener.release(c.addr) })
	return err
}

// --- DNS rebinding protection (opt-in) --------------------------------------

// rebindFilter blocks answers that point a public name at a private,
// loopback or link-local address (like dnsmasq's stop-dns-rebind), which is
// how a web page gets a browser to talk to the admin UI of a router or NAS.
// Rewrites never reach it (they are answered before the cache).
type rebindFilter struct {
	// allowSuffixes are names (and their subdomains) that legitimately
	// resolve to internal addresses: built-in local-only zones plus
	// DNS_REBIND_ALLOW_DOMAINS. Lowercase, no leading or trailing dots.
	allowSuffixes []string
}

// builtinRebindAllow are zones that only ever mean "this network" (RFC 6761,
// RFC 6762, RFC 8375, ICANN .internal) and are commonly answered by the
// router with private addresses.
var builtinRebindAllow = []string{"localhost", "local", "lan", "home.arpa", "internal"}

func parseRebindConfig(getenv func(string) string) (*rebindFilter, error) {
	raw := strings.TrimSpace(getenv("DNS_REBIND_PROTECTION"))
	if raw == "" {
		return nil, nil
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("DNS_REBIND_PROTECTION: want true or false, got %q", raw)
	}
	if !on {
		return nil, nil
	}
	f := &rebindFilter{allowSuffixes: append([]string(nil), builtinRebindAllow...)}
	for _, d := range strings.Split(getenv("DNS_REBIND_ALLOW_DOMAINS"), ",") {
		d = strings.Trim(strings.ToLower(strings.TrimSpace(d)), ".")
		if d != "" {
			f.allowSuffixes = append(f.allowSuffixes, d)
		}
	}
	return f, nil
}

// allows reports whether name (lowercase FQDN) is exempt from the filter.
func (f *rebindFilter) allows(name string) bool {
	n := strings.TrimSuffix(name, ".")
	for _, s := range f.allowSuffixes {
		if n == s || (len(n) > len(s) && n[len(n)-len(s)-1] == '.' && n[len(n)-len(s):] == s) {
			return true
		}
	}
	return false
}

// isRebindAddress: addresses a public name has no business resolving to.
func isRebindAddress(a netip.Addr) bool {
	return a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() ||
		(a.Is4() && (a.As4()[0] == 0 || cgnatPrefix.Contains(a)))
}
