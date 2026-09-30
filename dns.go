package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"

	"github.com/yeti/svart-dns/internal/policycore"
)

func startDNSServer(ctx context.Context, network, addr string) error {
	server := &dns.Server{
		Net:           network,
		Handler:       dns.HandlerFunc(handleDNSRequest),
		ReadTimeout:   dnsReadTimeout,
		WriteTimeout:  dnsWriteTimeout,
		IdleTimeout:   func() time.Duration { return dnsTCPIdleTimeout },
		MaxTCPQueries: dnsTCPMaxQueries,
	}
	switch network {
	case "tcp":
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		server.Listener = newLimitedListener(l, dnsGuard.Load())
	case "udp":
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			return err
		}
		server.PacketConn = pc
	default:
		return fmt.Errorf("unsupported DNS network %q", network)
	}

	// Shutdown only works once the server has started; waiting for the
	// start signal keeps a cancel that lands first from being ignored.
	started := make(chan struct{})
	done := make(chan struct{})
	server.NotifyStartedFunc = func() { close(started) }
	go func() {
		select {
		case <-started:
		case <-done:
			return
		}
		select {
		case <-ctx.Done():
			if err := server.Shutdown(); err != nil {
				dnsErrorLogger(logDNS, err).Warn("DNS server shutdown failed")
			}
		case <-done:
		}
	}()

	logDNS.Info("starting DNS server", "addr", addr, "network", network)
	err := server.ActivateAndServe()
	close(done)
	if ctx.Err() != nil {
		// Context cancelled = clean shutdown, not an error
		return nil
	}
	return err
}

// ednsAdvertisedSize is the UDP payload size we advertise and the ceiling we
// honour for clients (DNS flag day 2020: 1232 avoids IP fragmentation).
const ednsAdvertisedSize = 1232

// udpResponseLimit is the largest UDP answer the client can take: 512 bytes
// without EDNS, otherwise its advertised size clamped to [512, 1232].
func udpResponseLimit(req *dns.Msg) int {
	opt := req.IsEdns0()
	if opt == nil {
		return dns.MinMsgSize
	}
	size := int(opt.UDPSize())
	if size < dns.MinMsgSize {
		return dns.MinMsgSize
	}
	if size > ednsAdvertisedSize {
		return ednsAdvertisedSize
	}
	return size
}

// setReplyEDNS makes msg's OPT record reflect this server and this client:
// none when the client sent none (RFC 6891 section 7), otherwise exactly one
// with our payload size and the client's DO bit. Upstream OPT options (ECS,
// padding, cookies) are never relayed.
func setReplyEDNS(msg, req *dns.Msg) {
	kept := msg.Extra[:0]
	for _, rr := range msg.Extra {
		if rr.Header().Rrtype != dns.TypeOPT {
			kept = append(kept, rr)
		}
	}
	msg.Extra = kept
	if opt := req.IsEdns0(); opt != nil {
		msg.SetEdns0(ednsAdvertisedSize, opt.Do())
	}
}

// writeReply finalizes msg as the answer to req and writes it. UDP answers are
// truncated to the client's limit with TC set, so the server never sends more
// than the client can take (D1: no amplification); TCP answers go out whole.
func writeReply(w dns.ResponseWriter, req, msg *dns.Msg, udp bool) {
	msg.Id = req.Id
	setReplyEDNS(msg, req)
	if udp {
		msg.Truncate(udpResponseLimit(req))
	}
	if err := w.WriteMsg(msg); err != nil {
		dnsErrorLogger(logDNS, err).Warn("DNS response write failed")
	}
}

// writeRcode answers req with an empty response carrying rcode.
func writeRcode(w dns.ResponseWriter, req *dns.Msg, rcode int, udp bool) {
	msg := new(dns.Msg)
	msg.SetRcode(req, rcode)
	writeReply(w, req, msg, udp)
}

// recoverDNSHandler turns a handler panic into a logged SERVFAIL: a bug on
// one query path must not take DNS down for the whole network.
func recoverDNSHandler(w dns.ResponseWriter, r *dns.Msg) {
	p := recover()
	if p == nil {
		return
	}
	dnsGuardStats.handlerPanics.Add(1)
	name := ""
	if len(r.Question) > 0 {
		name = r.Question[0].Name
	}
	logger := dnsDiagnosticLogger(logDNS, "", name)
	if logQueryLines.Load() {
		logger = logger.With("panic", fmt.Sprint(p))
	}
	logger.Error("DNS handler panic, answered SERVFAIL", "panic_type", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
	msg := new(dns.Msg)
	msg.SetRcode(r, dns.RcodeServerFailure)
	if err := w.WriteMsg(msg); err != nil {
		dnsErrorLogger(logDNS, err).Warn("DNS response write failed")
	}
}

// rfc8482Reply answers an ANY query with a single synthesized HINFO record
// (RFC 8482 section 4.2) instead of forwarding it: ANY is the classic
// amplification query and no modern client needs a real answer.
func rfc8482Reply(r *dns.Msg) *dns.Msg {
	msg := new(dns.Msg)
	msg.SetReply(r)
	msg.RecursionAvailable = true
	msg.Answer = []dns.RR{&dns.HINFO{
		Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeHINFO, Class: dns.ClassINET, Ttl: 3600},
		Cpu: "RFC8482",
		Os:  "",
	}}
	return msg
}

// blockedReply is the sinkhole answer: NXDOMAIN plus an SOA whose negative
// TTL tells the client how long to cache it (RFC 2308).
func blockedReply(r *dns.Msg) *dns.Msg {
	deniedTTL := getDeniedTTL()
	msg := new(dns.Msg)
	msg.SetRcode(r, dns.RcodeNameError)
	msg.RecursionAvailable = true
	msg.Ns = append(msg.Ns, &dns.SOA{
		Hdr:    dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: deniedTTL},
		Ns:     "svart-dns.local.",
		Mbox:   "admin.svart-dns.local.",
		Minttl: deniedTTL,
	})
	return msg
}

// queryClientIP is the client identity used for policy and logs. Loopback is
// remapped to the host's external IP so queries from the host itself get
// correct policy evaluation and show as the real IP in logs/Grafana.
func queryClientIP(src netip.Addr) string {
	if externalIP != "" && src.IsLoopback() {
		return externalIP
	}
	return src.String()
}

// policyClientAddr is queryClientIP as an address. A remapped loopback
// source takes externalIP's address; if that does not parse, the zero Addr
// still keys the host's own queries consistently.
func policyClientAddr(src netip.Addr) netip.Addr {
	if externalIP != "" && src.IsLoopback() {
		a, err := netip.ParseAddr(externalIP)
		if err != nil {
			return netip.Addr{}
		}
		return a
	}
	return src
}

// upstreamQueryTimeout bounds one coalesced upstream resolution.
const upstreamQueryTimeout = 3 * time.Second

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	startTime := clock.Now()
	defer observeDNSResponse(startTime)
	defer recoverDNSHandler(w, r)

	remote := w.RemoteAddr()
	src := addrFromNetAddr(remote)
	_, udp := remote.(*net.UDPAddr)

	guard := dnsGuard.Load()
	if !guard.clientAllowed(src) {
		dnsGuardStats.aclRefused.Add(1)
		writeRcode(w, r, dns.RcodeRefused, udp)
		return
	}
	if guard.rateLimit != nil && !guard.rateLimit.allow(src, startTime.UnixNano()) {
		dnsGuardStats.rateLimited.Add(1)
		// UDP sources are unauthenticated: dropping sends nothing to a
		// possibly spoofed victim. A TCP client gets an explicit REFUSED.
		if !udp {
			writeRcode(w, r, dns.RcodeRefused, udp)
		}
		return
	}
	if r.Opcode != dns.OpcodeQuery {
		writeRcode(w, r, dns.RcodeNotImplemented, udp)
		return
	}
	if len(r.Question) != 1 {
		writeRcode(w, r, dns.RcodeFormatError, udp)
		return
	}

	if loggingEnabled.Load() && queryLogWriter != nil && queryLogWriter.spool != nil || activeLoki != nil {
		admissionStarted := clock.Now()
		accepted := admitDNSLog()
		observeDNSAdmission(admissionStarted, accepted)
		if !accepted {
			dnsGuardStats.overloadShed.Add(1)
			writeRcode(w, r, dns.RcodeServerFailure, udp)
			return
		}
		defer func() { <-dnsLogAdmission }()
	}

	key := cacheKeyFor(r)
	q := dnsQuery{
		w:        w,
		req:      r,
		udp:      udp,
		clientIP: queryClientIP(src),
		policyIP: policyClientAddr(src),
		name:     key.name,
		qtype:    dns.Type(r.Question[0].Qtype).String(),
		start:    startTime,
	}

	if dnsDebugEnabled && logQueryLines.Load() {
		logDNS.LogAttrs(context.Background(), slog.LevelDebug, "query received",
			slog.String("client_ip", q.clientIP),
			slog.String("domain", q.name),
			slog.String("type", q.qtype),
		)
	}

	// Only the Internet class is resolved. CHAOS/HESIOD queries used to be
	// forwarded and cached under the IN key, blanking names for everyone.
	if r.Question[0].Qclass != dns.ClassINET {
		q.log("REFUSED", false, "", nil)
		writeRcode(w, r, dns.RcodeRefused, udp)
		return
	}

	if rewriteResp := checkRewrite(r); rewriteResp != nil {
		if dnsDebugEnabled && logQueryLines.Load() {
			logDNS.LogAttrs(context.Background(), slog.LevelDebug, "rewrite",
				slog.String("domain", q.name),
			)
		}
		// Evaluate policy for client context (range/group) even though rewrite wins
		q.log(dns.RcodeToString[rewriteResp.Rcode], false, "rewrite", evaluatePolicyAddr(q.policyIP, q.clientIP, q.name, r.Question[0].Qtype))
		writeReply(w, r, rewriteResp, udp)
		return
	}

	policyResult := evaluatePolicyAddr(q.policyIP, q.clientIP, q.name, r.Question[0].Qtype)
	if policyResult.Result == "block" {
		q.debugBlocked("blocked", policyResult)
		// Doom-looping clients (DD-027) get this same immediate answer: the
		// old 1 s sleep parked a goroutine per query and was trivially
		// bypassed by changing letter case (D8). The log writer coalesces
		// their rows, the SOA tells them to cache, and DNS_RATE_LIMIT_QPS is
		// the real rate control.
		q.block(policyResult)
		return
	}

	if r.Question[0].Qtype == dns.TypeANY {
		q.log("NOERROR", false, "rfc8482", policyResult)
		writeReply(w, r, rfc8482Reply(r), udp)
		return
	}

	now := clock.NowUnixNano()
	if e, ok := cache.get(key, now); ok {
		q.serve(e, policyResult, "cache", now)
		return
	}

	res := resolveCoalesced(key, r)
	switch {
	case res.err != nil:
		dnsErrorLogger(dnsDiagnosticLogger(logDNS, q.clientIP, q.name), res.err).Warn("resolution failed")
		q.log("SERVFAIL", false, "", nil)
		writeRcode(w, r, dns.RcodeServerFailure, udp)
	case res.answer != nil:
		q.serve(res.answer, policyResult, res.upstream, clock.NowUnixNano())
	default:
		// SERVFAIL, REFUSED or TC=1 from upstream: passed through, never
		// cached. Records are stripped because they were never checked
		// against policy (a TC=1 answer may carry a partial answer).
		msg := new(dns.Msg)
		msg.SetRcode(r, res.msg.Rcode)
		msg.Truncated = res.msg.Truncated
		msg.RecursionAvailable = true
		q.log(dns.RcodeToString[res.msg.Rcode], false, res.upstream, policyResult)
		writeReply(w, r, msg, udp)
	}
}

// dnsQuery is one client query that passed the front-door checks.
type dnsQuery struct {
	w        dns.ResponseWriter
	req      *dns.Msg
	udp      bool
	clientIP string
	policyIP netip.Addr // clientIP parsed, for the policy cache key
	name     string     // lowercase FQDN: the cache key name and the logged name
	qtype    string
	start    time.Time
}

func (q *dnsQuery) log(rcode string, blocked bool, upstream string, policy *policycore.PolicyResult) {
	logQuery(q.clientIP, q.name, q.qtype, rcode, blocked, upstream, clock.Now().Sub(q.start).Microseconds(), policy)
}

// block answers with the sinkhole NXDOMAIN and logs the deciding policy.
func (q *dnsQuery) block(policy *policycore.PolicyResult) {
	q.log("NXDOMAIN", true, "", policy)
	writeReply(q.w, q.req, blockedReply(q.req), q.udp)
}

func (q *dnsQuery) debugBlocked(msg string, p *policycore.PolicyResult) {
	if !dnsDebugEnabled || !logQueryLines.Load() {
		return
	}
	tier, rule := "", ""
	if p != nil && p.ResultSource != nil {
		tier = p.ResultSource.Tier
		if p.ResultSource.PublishedList != nil {
			rule = p.ResultSource.PublishedList.Rule
		} else if p.ResultSource.CustomRule != nil {
			rule = p.ResultSource.CustomRule.Rule
		}
	}
	logDNS.LogAttrs(context.Background(), slog.LevelDebug, msg,
		slog.String("domain", q.name),
		slog.String("client_ip", q.clientIP),
		slog.String("tier", tier),
		slog.String("rule", rule),
	)
}

// serve answers from a packed answer (cache hit or fresh upstream answer),
// after the checks that depend on what the answer contains.
func (q *dnsQuery) serve(e *cachedAnswer, policy *policycore.PolicyResult, upstream string, now int64) {
	// D12: CNAME cloaking. A first-party name that aliases to a blocked
	// tracker is blocked. An explicit allow for the queried name also covers
	// what it points at, so an allowlisted site behind a blocked CDN keeps
	// working (AdGuard Home makes the same call).
	if len(e.targets) > 0 && !explicitlyAllowed(policy) {
		for _, target := range e.targets {
			if tp := evaluatePolicyAddr(q.policyIP, q.clientIP, target.name, target.rrtype); tp.Result == "block" {
				q.debugBlocked("blocked CNAME target", tp)
				q.block(tp)
				return
			}
		}
	}
	if len(e.ips) > 0 && responseIPsBlocked(q.clientIP, e.ips) {
		q.debugBlocked("blocked by IP response", nil)
		q.block(nil)
		return
	}
	if e.privateIP {
		if rb := dnsGuard.Load().rebind; rb != nil && !rb.allows(q.name) {
			rp := rebindPolicyResult(q.clientIP, q.name, e)
			q.debugBlocked("blocked DNS rebinding answer", rp)
			q.block(rp)
			return
		}
	}

	q.log(dns.RcodeToString[e.rcode()], false, upstream, policy)

	bp, ok := replyBufPool.Get().(*[]byte)
	if !ok {
		panic("DNS reply buffer pool has an invalid internal type")
	}
	buf := e.appendReply((*bp)[:0], q.req, now)
	if limit := udpResponseLimit(q.req); q.udp && len(buf) > limit {
		q.writeTruncated(buf, limit)
	} else {
		if _, err := q.w.Write(buf); err != nil {
			dnsErrorLogger(logDNS, err).Warn("DNS response write failed")
		}
	}
	if cap(buf) <= replyBufMaxPooled {
		*bp = buf[:0]
		replyBufPool.Put(bp)
	}

	rcode := dns.RcodeToString[e.rcode()]
	if dnsDebugEnabled && logQueryLines.Load() {
		logDNS.LogAttrs(context.Background(), slog.LevelDebug, "resolved",
			slog.String("domain", q.name),
			slog.String("rcode", rcode),
			slog.String("upstream", upstream),
		)
	}
}

// writeTruncated is the rare path: an answer larger than the client's UDP
// limit is unpacked and cut down by miekg, which sets TC.
func (q *dnsQuery) writeTruncated(wire []byte, limit int) {
	msg := new(dns.Msg)
	if err := msg.Unpack(wire); err != nil {
		writeRcode(q.w, q.req, dns.RcodeServerFailure, q.udp)
		return
	}
	msg.Truncate(limit)
	if err := q.w.WriteMsg(msg); err != nil {
		dnsErrorLogger(logDNS, err).Warn("DNS response write failed")
	}
}

func explicitlyAllowed(p *policycore.PolicyResult) bool {
	return p != nil && p.Result == "allow" && p.ResultSource != nil && p.ResultSource.Tier != "default"
}

// rebindPolicyResult records a rebinding block in the query log like any
// other policy decision, naming the offending address as the rule.
func rebindPolicyResult(clientIP, name string, e *cachedAnswer) *policycore.PolicyResult {
	rule := ""
	for _, ip := range e.ips {
		if a, err := netip.ParseAddr(ip); err == nil && isRebindAddress(a.Unmap()) {
			rule = ip
			break
		}
	}
	src := &policycore.EntityResult{Tier: "rebind", Name: "DNS rebinding protection", Result: "block",
		CustomRule: &policycore.CustomHit{Action: "block", Rule: rule}}
	return &policycore.PolicyResult{ClientIP: clientIP, Domain: strings.TrimSuffix(name, "."), Result: "block", ResultSource: src}
}

// --- miss path: coalescing and overload shedding ------------------------------

// resolution is the outcome of one upstream round trip, shared by every
// query coalesced onto it. Exactly one of answer, msg or err is meaningful:
// answer for a servable NOERROR/NXDOMAIN, msg for a transient upstream rcode
// (read-only, shared), err when there is nothing to relay.
type resolution struct {
	answer   *cachedAnswer
	msg      *dns.Msg
	upstream string
	err      error
}

var (
	errResolverOverloaded = errors.New("too many pending upstream resolutions")
	errUnusableAnswer     = errors.New("upstream answer does not answer the question")
	errFlightAborted      = errors.New("coalesced resolution aborted")
)

// maxPendingResolutions caps goroutines parked on the miss path (leaders
// plus coalesced waiters). miekg starts a goroutine per UDP packet; without a
// cap, a flood of unique names against a slow upstream parks one goroutine
// per query for the whole upstream timeout (D11). Past the cap a miss is
// answered SERVFAIL at once; 10k parked goroutines is ~80 MB of stacks.
var maxPendingResolutions int64 = 10000

var pendingResolutions atomic.Int64

var resolverFlights = flightGroup{calls: make(map[dnsCacheKey]*flightCall)}

// resolveCoalesced resolves key upstream, sharing one round trip among all
// concurrent identical misses (which also denies an off-path spoofer the
// many simultaneous outstanding queries a birthday attack needs, D2).
func resolveCoalesced(key dnsCacheKey, r *dns.Msg) resolution {
	if pendingResolutions.Add(1) > maxPendingResolutions {
		pendingResolutions.Add(-1)
		dnsGuardStats.overloadShed.Add(1)
		return resolution{err: errResolverOverloaded}
	}
	defer pendingResolutions.Add(-1)

	ctx, cancel := context.WithTimeout(context.Background(), upstreamQueryTimeout)
	defer cancel()
	return resolverFlights.do(ctx, key, func() resolution {
		resp, upstream, err := resolveQuery(ctx, r)
		if err != nil {
			return resolution{err: err}
		}
		if resp.Truncated || (resp.Rcode != dns.RcodeSuccess && resp.Rcode != dns.RcodeNameError) {
			return resolution{msg: resp, upstream: upstream}
		}
		e := cache.set(key, resp, getRuntimeSettings().cacheTTL)
		if e == nil {
			return resolution{err: errUnusableAnswer}
		}
		return resolution{answer: e, upstream: upstream}
	})
}

// flightGroup runs one call per key at a time; concurrent callers for the
// same key wait for the leader's result (or their own deadline).
type flightGroup struct {
	mu    sync.Mutex
	calls map[dnsCacheKey]*flightCall
}

type flightCall struct {
	done chan struct{}
	res  resolution
}

func (g *flightGroup) do(ctx context.Context, key dnsCacheKey, fn func() resolution) resolution {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		select {
		case <-c.done:
			return c.res
		case <-ctx.Done():
			return resolution{err: ctx.Err()}
		}
	}
	// Preset so waiters see an error, not a zero value, if fn panics (the
	// handler's recover turns the leader's panic into SERVFAIL).
	c := &flightCall{done: make(chan struct{}), res: resolution{err: errFlightAborted}}
	g.calls[key] = c
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		close(c.done)
	}()
	c.res = fn()
	return c.res
}

func logQuery(clientIP, queryName, queryType, responseCode string, blocked bool, upstream string, latencyMicroseconds int64, policy *policycore.PolicyResult) {
	if !loggingEnabled.Load() {
		return
	}
	entry := queryLogEntry{
		clientIP:            clientIP,
		queryName:           queryName,
		queryType:           queryType,
		responseCode:        responseCode,
		blocked:             blocked,
		upstream:            upstream,
		latencyMicroseconds: latencyMicroseconds,
		coalescedCount:      1,
	}

	// Rich policy result columns
	flattenPolicyResult(&entry, policy, upstream)
	queryLogWriter.log(entry)
}

// flattenPolicyResult populates the rich policy columns on queryLogEntry.
func flattenPolicyResult(e *queryLogEntry, p *policycore.PolicyResult, upstream string) {
	// Determine result and reason
	if p == nil {
		if upstream == "rewrite" {
			e.result = "rewrite"
			e.resultReason = "rewrite"
			e.resultTier = "rewrite"
		} else {
			e.result = "allow"
			e.resultReason = "default_allow"
			e.resultTier = "default"
		}
		return
	}

	e.result = p.Result
	if e.result == "" {
		e.result = "allow"
	}

	// Populate winning source
	if p.ResultSource != nil {
		src := p.ResultSource
		e.resultTier = src.Tier
		e.resultEntity = src.Name
		flattenEntityToWinning(src, &e.resultIsPublished, &e.resultRule, &e.resultListID, &e.resultListName)
		e.resultReason = deriveResultReason(src)
	} else {
		e.resultReason = "default_allow"
		e.resultTier = "default"
	}

	// Per-tier evaluations
	if p.RangeEvaluation != nil {
		if p.RangeEvaluation.ResultSource != nil {
			flattenTierEval(p.RangeEvaluation, &e.rangeResult, &e.rangeEntity, &e.rangeIsPublished, &e.rangeRule, &e.rangeListID, &e.rangeListName)
		} else if len(p.RangeEvaluation.Entities) > 0 {
			e.rangeEntity = p.RangeEvaluation.Entities[0].Name
		}
	}
	if p.GroupEvaluation != nil {
		if p.GroupEvaluation.ResultSource != nil {
			flattenTierEval(p.GroupEvaluation, &e.groupResult, &e.groupEntity, &e.groupIsPublished, &e.groupRule, &e.groupListID, &e.groupListName)
		} else if len(p.GroupEvaluation.Entities) > 0 {
			e.groupEntity = p.GroupEvaluation.Entities[0].Name
		}
	}
	if p.IPEvaluation != nil {
		if p.IPEvaluation.ResultSource != nil {
			flattenTierEval(p.IPEvaluation, &e.ipResult, &e.ipEntity, &e.ipIsPublished, &e.ipRule, &e.ipListID, &e.ipListName)
		} else if len(p.IPEvaluation.Entities) > 0 {
			e.ipEntity = p.IPEvaluation.Entities[0].Name
		}
	}
}

// flattenTierEval extracts the winning entity's data from a TierEvaluation.
func flattenTierEval(te *policycore.TierEvaluation, result, entity *string, isPublished *bool, rule *string, listID *int, listName *string) {
	*result = te.Result
	if te.ResultSource != nil {
		*entity = te.ResultSource.Name
		flattenEntityToWinning(te.ResultSource, isPublished, rule, listID, listName)
	}
}

// flattenEntityToWinning extracts published/custom hit data from an EntityResult.
func flattenEntityToWinning(er *policycore.EntityResult, isPublished *bool, rule *string, listID *int, listName *string) {
	if er.PublishedList != nil {
		*isPublished = true
		*rule = er.PublishedList.Rule
		*listID = er.PublishedList.ListID
		*listName = er.PublishedList.ListName
	} else if er.CustomRule != nil {
		*isPublished = false
		*rule = er.CustomRule.Rule
	}
}

// deriveResultReason determines the result_reason string from an EntityResult.
func deriveResultReason(er *policycore.EntityResult) string {
	if er.Tier == "rebind" {
		return "rebind_block"
	}
	if er.PublishedList != nil {
		switch er.PublishedList.Action {
		case "block":
			return "published_block"
		case "allow":
			return "published_allow"
		}
	}
	if er.CustomRule != nil {
		switch er.CustomRule.Action {
		case "block":
			return "custom_block"
		case "allow":
			return "custom_allow"
		}
	}
	if er.Result == "block" {
		return "published_block"
	}
	if er.Result == "allow" {
		return "default_allow"
	}
	return "default_allow"
}

// isDockerBridgeInterface returns true if the interface looks like a Docker/container bridge.
func isDockerBridgeInterface(name string) bool {
	for _, prefix := range []string{"docker", "br-", "veth", "cni", "flannel", "podman"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// detectExternalIP finds the host's real non-loopback, non-Docker-bridge IPv4 address.
// Skips interfaces named like container bridges (docker0, br-*, veth*).
// Falls back to any non-loopback IPv4 if no "clean" address is found.
func detectExternalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil || ip.IsLoopback() {
				continue
			}

			// Skip Docker/container bridge interfaces
			if isDockerBridgeInterface(iface.Name) {
				if fallback == "" {
					fallback = ip.String()
				}
				continue
			}

			return ip.String()
		}
	}
	return fallback
}

func checkRewrite(r *dns.Msg) *dns.Msg {
	if len(r.Question) == 0 {
		return nil
	}

	q := r.Question[0]
	domain := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	val, ok := rewritesCache.Load().Load(domain)
	if !ok {
		return nil
	}

	rewrite, valid := val.(Rewrite)
	if !valid {
		panic("DNS rewrite store has an invalid internal type")
	}
	ips := rewrite.IPAddresses

	if len(ips) == 0 {
		return nil
	}

	msg := new(dns.Msg)
	msg.SetReply(r)

	switch q.Qtype {
	case dns.TypeA:
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() != nil {
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: ip.To4(),
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	case dns.TypeAAAA:
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() == nil {
				rr := &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					AAAA: ip,
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	}

	// Always return a response for rewritten domains — even with no matching
	// record type. This prevents AAAA queries for IPv4-only rewrites from
	// leaking internal hostnames to upstream resolvers.
	return msg
}
