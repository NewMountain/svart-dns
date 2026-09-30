package main

import (
	"context"
	crand "crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type ResolverConfig struct {
	Timeout time.Duration
}

var resolverConfig = &ResolverConfig{
	Timeout: 5 * time.Second,
}

// upstreamRootCAs is the trust anchor set for DoT and DoH upstream
// certificates. nil means the system roots, which is what production uses.
// It exists so tests can verify real TLS behavior against local servers with
// a test CA, without the process-wide SSL_CERT_FILE trick.
var upstreamRootCAs *x509.CertPool

// ednsUDPSize is the EDNS0 UDP payload size svart advertises to upstreams
// and to clients. 1232 keeps every reply inside the IPv6 minimum MTU (DNS
// Flag Day 2020): fragmented UDP replies are both dropped by middleboxes and
// a known cache-poisoning vector, because only the first fragment carries
// the ID.
const ednsUDPSize = 1232

// miekg documents dns.Id as replaceable. Its default reads crypto/rand
// through binary.Read, which allocates; reading into a stack array is the
// same CSPRNG with no allocation and half the cost (61 ns vs 115 ns per ID
// on the dev box). Every SetQuestion, upstream and bootstrap, uses it.
func init() { dns.Id = cryptoMessageID }

func cryptoMessageID() uint16 {
	var b [2]byte
	_, _ = crand.Read(b[:]) // crypto/rand.Read never returns an error (Go 1.24+)
	return binary.BigEndian.Uint16(b[:])
}

var (
	// errQueryNotForwardable marks client requests svart refuses to send
	// upstream. It is the client's problem, not the upstream's, so selection
	// must not count it as an upstream failure.
	errNoEnabledUpstream   = errors.New("no enabled upstream serves query")
	errAllUpstreamsFailed  = errors.New("all upstreams failed")
	errQueryNotForwardable = errors.New("query cannot be forwarded upstream")

	// errUpstreamReplyMismatch marks a reply that does not answer the query
	// svart sent: a spoof attempt on UDP, a broken upstream otherwise.
	errUpstreamReplyMismatch = errors.New("upstream reply does not match the query")

	// errUpstreamSaturated marks a miss refused because too many upstream
	// exchanges are already in flight.
	errUpstreamSaturated = errors.New("too many upstream exchanges in flight")
)

// maxInflightUpstreamExchanges caps concurrent upstream exchanges. Each UDP
// exchange holds its own socket (a fresh random source port per query is
// ~16 bits of anti-spoofing entropy on top of the ID, which a shared socket
// pool would give up) for up to the request deadline, so a flood of misses
// against a slow or black-holed upstream would otherwise grow sockets and
// goroutines until fds or ephemeral ports run out. 4096 is far above normal
// load (a 50 ms upstream at 20k misses/s is ~1000 in flight) and far below
// the fd limit Go raises to at startup.
const maxInflightUpstreamExchanges = 4096

var upstreamExchangeSlots = make(chan struct{}, maxInflightUpstreamExchanges)

// upstreamStats fields are atomic.Int64 rather than plain int64 written via
// atomic.AddInt64. Previously they were plain int64 fields written with
// atomic.AddInt64 in recordSuccess/recordFailure but read as bare fields
// (no atomic load) in calculateWeight — a genuine data race under
// `go test -race` and, per the memory model, not guaranteed to observe the
// latest value even in practice on all platforms.
type upstreamStats struct {
	failures   atomic.Int64
	totalTime  atomic.Int64
	queryCount atomic.Int64
}

var upstreamStatsMap sync.Map

// warnIfDoHWithoutBootstrap logs a warning at startup if DoH/DoT upstreams
// are configured but no bootstrap DNS servers exist.
func warnIfDoHWithoutBootstrap() {
	upstreams := upstreamStorage.getAll()
	hasHostnameUpstream := false
	for _, u := range upstreams {
		if strings.Contains(u.Upstream, "://") {
			hasHostnameUpstream = true
			break
		}
	}
	if !hasHostnameUpstream {
		return
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM bootstrap_servers").Scan(&count); err != nil {
		slog.Error("read bootstrap configuration failed", "error", err)
		return
	}
	if count == 0 {
		slog.Warn("DoH/DoT upstreams configured but no bootstrap DNS servers — hostname resolution requires configured bootstrap servers. Add bootstrap servers via /api/bootstrap or the Config UI")
	}
}

// resolveQuery resolves a DNS query upstream, returning the response and the
// upstream address used. Caching and miss coalescing live in the DNS handler
// (resolveCoalesced in dns.go), which calls this only on a cache miss.
func resolveQuery(ctx context.Context, req *dns.Msg) (*dns.Msg, string, error) {
	if len(req.Question) == 0 {
		return nil, "", fmt.Errorf("no question in request")
	}

	q := req.Question[0]
	enabledUpstreams := upstreamStorage.forQuery(q.Name)
	if len(enabledUpstreams) == 0 {
		return nil, "", fmt.Errorf("%w: %s", errNoEnabledUpstream, q.Name)
	}

	settings := getRuntimeSettings()

	var resp *dns.Msg
	var usedUpstream string
	var err error

	switch settings.strategy {
	case "random":
		resp, usedUpstream, err = resolveRandom(ctx, req, enabledUpstreams)
	case "blended":
		// 70% weighted (favors faster/healthier upstreams), 30% pure random (privacy spread)
		if rand.Float64() < 0.70 { // #nosec G404 -- chooses a load-balancing strategy, not a security nonce; DNS IDs use crypto/rand.
			resp, usedUpstream, err = resolveLoadBalanced(ctx, req, enabledUpstreams)
		} else {
			resp, usedUpstream, err = resolveRandom(ctx, req, enabledUpstreams)
		}
	default: // "weighted" or legacy "load-balance"
		resp, usedUpstream, err = resolveLoadBalanced(ctx, req, enabledUpstreams)
	}

	if err != nil {
		return nil, "", err
	}

	return resp, usedUpstream, nil
}

func resolveLoadBalanced(ctx context.Context, req *dns.Msg, upstreams []Upstream) (*dns.Msg, string, error) {
	weights := make([]float64, len(upstreams))
	totalWeight := 0.0

	for i, u := range upstreams {
		weight := calculateWeight(u.Upstream)
		weights[i] = weight
		totalWeight += weight
	}

	for i := 0; i < len(upstreams); i++ {
		selected := weightedRandomSelect(upstreams, weights, totalWeight)

		start := clock.Now()
		resp, err := queryUpstream(ctx, req, selected.Upstream)
		latency := clock.Now().Sub(start).Milliseconds()

		if err != nil {
			if isLocalExchangeError(err) {
				return nil, "", err
			}
			recordFailure(selected.Upstream)
			dnsErrorLogger(logResolver, err).Warn("upstream query failed", "upstream", redactUpstream(selected.Upstream))
			continue
		}

		recordSuccess(selected.Upstream, latency)
		return resp, selected.Upstream, nil
	}

	return nil, "", errAllUpstreamsFailed
}

func resolveRandom(ctx context.Context, req *dns.Msg, upstreams []Upstream) (*dns.Msg, string, error) {
	// Shuffle to try in random order, falling back on failure
	perm := rand.Perm(len(upstreams)) // #nosec G404 -- upstream retry ordering is load distribution, not an authentication secret.

	for _, idx := range perm {
		u := upstreams[idx]

		start := clock.Now()
		resp, err := queryUpstream(ctx, req, u.Upstream)
		latency := clock.Now().Sub(start).Milliseconds()

		if err != nil {
			if isLocalExchangeError(err) {
				return nil, "", err
			}
			recordFailure(u.Upstream)
			dnsErrorLogger(logResolver, err).Warn("upstream query failed", "upstream", redactUpstream(u.Upstream))
			continue
		}

		recordSuccess(u.Upstream, latency)
		return resp, u.Upstream, nil
	}

	return nil, "", errAllUpstreamsFailed
}

func calculateWeight(upstream string) float64 {
	val, ok := upstreamStatsMap.Load(upstream)
	if !ok {
		return 1.0
	}

	stats := checkedUpstreamStats(val)
	queryCount := stats.queryCount.Load()
	if queryCount == 0 {
		return 1.0
	}

	failureRate := float64(stats.failures.Load()) / float64(queryCount)
	avgTime := float64(stats.totalTime.Load()) / float64(queryCount)

	weight := 1.0 / (1.0 + failureRate*10 + avgTime/1000)

	if weight < 0.01 {
		weight = 0.01
	}

	return weight
}

func weightedRandomSelect(upstreams []Upstream, weights []float64, totalWeight float64) Upstream {
	r := rand.Float64() * totalWeight // #nosec G404 -- samples health weights for load distribution; DNS IDs use crypto/rand.

	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if r <= cumulative {
			return upstreams[i]
		}
	}

	return upstreams[len(upstreams)-1]
}

func recordSuccess(upstream string, latencyMs int64) {
	val, _ := upstreamStatsMap.LoadOrStore(upstream, &upstreamStats{})
	stats := checkedUpstreamStats(val)

	stats.totalTime.Add(latencyMs)
	stats.queryCount.Add(1)

	dnsUpstreamQueries.WithLabelValues(upstream).Inc()
	dnsUpstreamDuration.WithLabelValues(upstream).Observe(float64(latencyMs) / 1000.0)
}

func recordFailure(upstream string) {
	val, _ := upstreamStatsMap.LoadOrStore(upstream, &upstreamStats{})
	stats := checkedUpstreamStats(val)

	stats.failures.Add(1)
	stats.queryCount.Add(1)

	dnsUpstreamErrors.WithLabelValues(upstream).Inc()
}

// isLocalExchangeError reports errors that say nothing about the upstream's
// health, so selection stops instead of failing over and penalizing every
// upstream in turn.
func isLocalExchangeError(err error) bool {
	return errors.Is(err, errQueryNotForwardable) || errors.Is(err, errUpstreamSaturated)
}

// queryUpstream performs one exchange with one upstream for a client
// request: build a fresh upstream message, exchange it, validate the reply
// against what was sent, and map the validated answer back onto the
// client's request.
func queryUpstream(ctx context.Context, req *dns.Msg, upstreamAddr string) (*dns.Msg, error) {
	query, err := newUpstreamQuery(req)
	if err != nil {
		return nil, err
	}
	// Fail fast rather than queue: a queued miss would still hold the
	// client's goroutine for the whole deadline.
	select {
	case upstreamExchangeSlots <- struct{}{}:
	default:
		dnsUpstreamSaturated.Inc()
		return nil, errUpstreamSaturated
	}
	defer func() { <-upstreamExchangeSlots }()

	reply, err := exchangeUpstream(ctx, query, upstreamAddr)
	if err != nil {
		return nil, err
	}
	if err := validateUpstreamReply(query, reply); err != nil {
		if errors.Is(err, errUpstreamReplyMismatch) {
			dnsUpstreamMismatchedReplies.WithLabelValues(upstreamAddr).Inc()
		}
		return nil, err
	}
	return clientReply(req, reply), nil
}

// newUpstreamQuery builds the message svart sends upstream. Only the
// question and the DO/CD/AD bits come from the client. The ID is fresh from
// crypto/rand (dns.Id), so a client-chosen ID tells an off-path spoofer
// nothing; the name is lowercased so every client casing is one upstream
// question; EDNS is svart's own OPT so client options (ECS reveals the
// client subnet, cookies identify the client) stay on the client hop.
// Only class IN is forwarded: the question is always built as IN, so
// forwarding another class would answer it with IN data.
func newUpstreamQuery(req *dns.Msg) (*dns.Msg, error) {
	if req.Opcode != dns.OpcodeQuery {
		return nil, fmt.Errorf("%w: opcode %s", errQueryNotForwardable, dns.OpcodeToString[req.Opcode])
	}
	if len(req.Question) != 1 {
		return nil, fmt.Errorf("%w: %d questions", errQueryNotForwardable, len(req.Question))
	}
	q := req.Question[0]
	if q.Qclass != dns.ClassINET {
		return nil, fmt.Errorf("%w: class %s", errQueryNotForwardable, dns.ClassToString[q.Qclass])
	}

	m := new(dns.Msg)
	m.SetQuestion(strings.ToLower(q.Name), q.Qtype) // random ID, RD=1, class IN
	m.CheckingDisabled = req.CheckingDisabled
	// AD in a query asks a validating upstream to report AD (RFC 6840 5.7),
	// which stubs using "trust-ad" rely on even without DO.
	m.AuthenticatedData = req.AuthenticatedData
	do := false
	if opt := req.IsEdns0(); opt != nil {
		do = opt.Do()
	}
	m.SetEdns0(ednsUDPSize, do)
	return m, nil
}

// validateUpstreamReply rejects any reply that is not the answer to sent:
// it must be a response, carry sent's ID and opcode, and echo exactly the
// one question (name case-insensitively, type and class exactly).
func validateUpstreamReply(sent, got *dns.Msg) error {
	switch {
	case !got.Response:
		return fmt.Errorf("%w: QR bit clear", errUpstreamReplyMismatch)
	case got.Id != sent.Id:
		return fmt.Errorf("%w: ID %d, sent %d", errUpstreamReplyMismatch, got.Id, sent.Id)
	case got.Opcode != sent.Opcode:
		return fmt.Errorf("%w: opcode %s", errUpstreamReplyMismatch, dns.OpcodeToString[got.Opcode])
	case len(got.Question) != 1:
		return fmt.Errorf("%w: %d questions", errUpstreamReplyMismatch, len(got.Question))
	}
	gq, sq := got.Question[0], sent.Question[0]
	if gq.Qtype != sq.Qtype || gq.Qclass != sq.Qclass || !strings.EqualFold(gq.Name, sq.Name) {
		return fmt.Errorf("%w: answered %q %s %s for %q %s %s", errUpstreamReplyMismatch,
			gq.Name, dns.ClassToString[gq.Qclass], dns.TypeToString[gq.Qtype],
			sq.Name, dns.ClassToString[sq.Qclass], dns.TypeToString[sq.Qtype])
	}
	// Extended rcodes (BADVERS, BADCOOKIE, ...) describe svart's EDNS
	// exchange with the upstream and cannot be expressed to a non-EDNS client.
	if got.Rcode > 0xF {
		return fmt.Errorf("upstream returned extended rcode %s", dns.RcodeToString[got.Rcode])
	}
	return nil
}

// clientReply turns a validated upstream reply into the reply for req. It is
// the single place a forwarded answer is shaped for the client: the client's
// ID and exact question (casing included) go back, and the upstream's OPT is
// replaced by svart's own because it describes the upstream hop (its UDP
// size, NSID, padding), not ours. reply is owned by the caller and reused.
func clientReply(req, reply *dns.Msg) *dns.Msg {
	reply.Id = req.Id
	reply.Question[0] = req.Question[0]
	reply.RecursionDesired = req.RecursionDesired
	reply.CheckingDisabled = req.CheckingDisabled

	extra := reply.Extra[:0]
	for _, rr := range reply.Extra {
		if rr.Header().Rrtype != dns.TypeOPT {
			extra = append(extra, rr)
		}
	}
	reply.Extra = extra
	if opt := req.IsEdns0(); opt != nil {
		reply.SetEdns0(ednsUDPSize, opt.Do())
	}
	return reply
}

// exchangeUpstream sends query to one upstream over its configured
// transport and returns the raw reply.
func exchangeUpstream(ctx context.Context, query *dns.Msg, upstreamAddr string) (*dns.Msg, error) {
	protocol, address := parseUpstreamAddress(upstreamAddr)
	if protocol == "https" {
		// The DoH client resolves its hostname only when it dials a new
		// connection, not per query.
		return queryDoH(ctx, query, upstreamAddr, address)
	}

	var serverName string
	switch protocol {
	case "tcp":
		address = withDefaultPort(address, "53")
	case "tls":
		address = withDefaultPort(address, "853")
		// The certificate must prove the configured name, not whichever IP
		// the (plaintext, spoofable) bootstrap lookup returned.
		var err error
		serverName, _, err = net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid TLS upstream address: %w", err)
		}
	}

	resolvedAddr, err := resolveUpstreamHostname(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve upstream hostname: %w", err)
	}

	switch protocol {
	case "udp", "":
		reply, err := queryUDP(ctx, query, resolvedAddr)
		if err != nil || !reply.Truncated {
			return reply, err
		}
		// TC=1 is not an answer, it is the upstream asking to be re-asked
		// over TCP (RFC 7766). Returning it would hand clients (and the
		// cache) an empty truncated reply.
		return queryStream(ctx, query, upstreamAddr, "tcp", "", resolvedAddr)
	case "tcp":
		return queryStream(ctx, query, upstreamAddr, "tcp", "", resolvedAddr)
	case "tls":
		return queryStream(ctx, query, upstreamAddr, "tcp-tls", serverName, resolvedAddr)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// withDefaultPort returns address unchanged if it has a port, otherwise
// host:port (bracketing IPv6 literals).
func withDefaultPort(address, port string) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return address
	}
	return net.JoinHostPort(strings.Trim(address, "[]"), port)
}

func parseUpstreamAddress(upstream string) (protocol, address string) {
	upstream = strings.TrimSpace(upstream)

	if strings.HasPrefix(upstream, "#") {
		return "", ""
	}

	if strings.HasPrefix(upstream, "[/") {
		idx := strings.Index(upstream, "]")
		if idx > 0 && idx+1 < len(upstream) {
			upstream = upstream[idx+1:]
		}
	}

	if protocol, address, found := strings.Cut(upstream, "://"); found {
		if protocol == "udp" {
			address = withDefaultPort(address, "53")
		}
		return protocol, address
	}
	return "udp", withDefaultPort(upstream, "53")
}

// The statistics store is populated only by recordSuccess and recordFailure.
func checkedUpstreamStats(value any) *upstreamStats {
	stats, ok := value.(*upstreamStats)
	if !ok {
		panic("upstream statistics store has an invalid internal type")
	}
	return stats
}
