package svart

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// maxDNSMessageSize is the largest DNS message the 16-bit length framing of
// DNS over TCP can express; a DoH body larger than this is not DNS.
const maxDNSMessageSize = 65535

// dohUpstream is the long-lived client for one configured DoH upstream. One
// Transport per upstream means one pooled HTTP/2 connection carries every
// query; building a Transport per query cost a TCP+TLS handshake each time
// and leaked the idle connection (D5).
type dohUpstream struct {
	url    string
	client *http.Client
}

// dohUpstreams maps the configured upstream string to its client. Entries
// are dropped by resetUpstreamTransports when the upstream set changes.
var dohUpstreams sync.Map // string -> *dohUpstream

func dohUpstreamFor(upstreamAddr, address string) *dohUpstream {
	if u, ok := dohUpstreams.Load(upstreamAddr); ok {
		return checkedDoHUpstream(u)
	}
	u, _ := dohUpstreams.LoadOrStore(upstreamAddr, &dohUpstream{url: dohURL(address), client: newDoHClient()})
	return checkedDoHUpstream(u)
}

// dohURL turns the address part of "https://host[:port][/path]" back into
// a URL, defaulting the RFC 8484 path.
func dohURL(address string) string {
	url := "https://" + address
	if !strings.Contains(address, "/") {
		url += "/dns-query"
	}
	return url
}

func newDoHClient() *http.Client {
	return &http.Client{
		Timeout: resolverConfig.Timeout,
		// A redirect would move svart's queries to a URL the operator never
		// configured; treat it as a failed upstream instead.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			// Proxy stays nil on purpose: HTTP(S)_PROXY from the environment
			// must not reroute DNS traffic.
			DialContext:       dialViaBootstrap,
			ForceAttemptHTTP2: true,
			// The Transport derives ServerName from the URL host, so the
			// certificate is verified for the configured hostname even though
			// dialViaBootstrap connects to a bootstrap-resolved IP.
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: upstreamRootCAs},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: resolverConfig.Timeout,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          4,
			MaxIdleConnsPerHost:   4,
		},
	}
}

// dialViaBootstrap dials upstream hostnames through the bootstrap servers,
// never the system resolver, which may point back at svart itself.
func dialViaBootstrap(ctx context.Context, network, addr string) (net.Conn, error) {
	resolved, err := resolveUpstreamHostname(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("bootstrap resolve for DoH: %w", err)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, resolved)
}

func queryDoH(ctx context.Context, query *dns.Msg, upstreamAddr, address string) (*dns.Msg, error) {
	up := dohUpstreamFor(upstreamAddr, address)

	packed, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, up.url, bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	httpResp, err := up.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer closeUpstreamTransport(httpResp.Body)

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", httpResp.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(httpResp.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/dns-message" {
		return nil, fmt.Errorf("DoH response Content-Type %q, want application/dns-message", httpResp.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, maxDNSMessageSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(body) > maxDNSMessageSize {
		return nil, fmt.Errorf("DoH response body exceeds %d bytes", maxDNSMessageSize)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(body); err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %w", err)
	}
	return resp, nil
}

// udpUpstreamClient is shared: dns.Client only reads its fields. Each
// exchange still dials its own socket, so every query leaves from a fresh
// random ephemeral port (see maxInflightUpstreamExchanges).
var udpUpstreamClient = &dns.Client{Net: "udp", Timeout: resolverConfig.Timeout}

func queryUDP(ctx context.Context, query *dns.Msg, address string) (*dns.Msg, error) {
	resp, _, err := udpUpstreamClient.ExchangeContext(ctx, query, address)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

const (
	// streamPoolMaxIdle bounds idle connections kept per TCP/DoT upstream.
	// Bursts beyond it dial extra connections and close them afterwards.
	streamPoolMaxIdle = 4

	// streamIdleTimeout retires idle connections before typical server idle
	// timeouts, so most reuses hit a live connection. A connection the
	// server closed anyway is caught by the retry in queryStream.
	streamIdleTimeout = 10 * time.Second
)

// streamPool keeps idle connections to one TCP or DoT upstream. Each
// connection serves one exchange at a time (no pipelining), so replies
// cannot interleave and no ID demultiplexing is needed. Reuse saves a TCP
// handshake per query, and for DoT a TLS handshake too.
type streamPool struct {
	client *dns.Client

	mu     sync.Mutex
	idle   []pooledConn // most recently used last
	closed bool
}

type pooledConn struct {
	conn      *dns.Conn
	addr      string // resolved address it was dialed to
	idleSince time.Time
}

// streamPools maps the configured upstream string to its pool. The key
// fixes network and TLS server name, so one entry never mixes transports.
var streamPools sync.Map // string -> *streamPool

func streamPoolFor(upstreamAddr, network, serverName string) *streamPool {
	if p, ok := streamPools.Load(upstreamAddr); ok {
		return checkedStreamPool(p)
	}
	client := &dns.Client{Net: network, Timeout: resolverConfig.Timeout}
	if network == "tcp-tls" {
		client.TLSConfig = &tls.Config{
			ServerName: serverName,
			MinVersion: tls.VersionTLS12,
			RootCAs:    upstreamRootCAs,
		}
	}
	p, _ := streamPools.LoadOrStore(upstreamAddr, &streamPool{client: client})
	return checkedStreamPool(p)
}

// queryStream exchanges query over a pooled TCP or DoT connection to addr
// (the bootstrap-resolved address; for DoT the certificate is verified for
// serverName).
func queryStream(ctx context.Context, query *dns.Msg, upstreamAddr, network, serverName, addr string) (*dns.Msg, error) {
	p := streamPoolFor(upstreamAddr, network, serverName)
	for {
		conn, reused := p.take(addr, clock.Now())
		if !reused {
			var err error
			if conn, err = p.client.DialContext(ctx, addr); err != nil {
				return nil, err
			}
		}
		resp, _, err := p.client.ExchangeWithConnContext(ctx, query, conn)
		if err == nil {
			p.give(pooledConn{conn: conn, addr: addr, idleSince: clock.Now()})
			return resp, nil
		}
		// Any error leaves the connection's framing unknown; never reuse it.
		closeUpstreamTransport(conn)
		// A pooled connection may have been closed by the server while
		// idle, which says nothing about the upstream: retry. Each failed
		// idle connection is discarded, so this ends at a fresh dial.
		if !reused || ctx.Err() != nil {
			return nil, err
		}
	}
}

// take returns an idle connection to addr that is young enough to reuse,
// closing any stale ones it passes (dialed to an old bootstrap address or
// idle too long).
func (p *streamPool) take(addr string, now time.Time) (*dns.Conn, bool) {
	var stale []*dns.Conn
	defer func() {
		for _, c := range stale {
			closeUpstreamTransport(c)
		}
	}()

	p.mu.Lock()
	defer p.mu.Unlock()
	for n := len(p.idle); n > 0; n = len(p.idle) {
		pc := p.idle[n-1]
		p.idle[n-1] = pooledConn{}
		p.idle = p.idle[:n-1]
		if pc.addr == addr && now.Sub(pc.idleSince) < streamIdleTimeout {
			return pc.conn, true
		}
		stale = append(stale, pc.conn)
	}
	return nil, false
}

func (p *streamPool) give(pc pooledConn) {
	p.mu.Lock()
	if p.closed || len(p.idle) >= streamPoolMaxIdle {
		p.mu.Unlock()
		closeUpstreamTransport(pc.conn)
		return
	}
	p.idle = append(p.idle, pc)
	p.mu.Unlock()
}

// close retires the pool; connections still in use are closed when their
// exchange hands them back.
func (p *streamPool) close() {
	p.mu.Lock()
	idle := p.idle
	p.idle, p.closed = nil, true
	p.mu.Unlock()
	for _, pc := range idle {
		closeUpstreamTransport(pc.conn)
	}
}

// resetUpstreamTransports drops every pooled upstream connection. It runs
// when the configured upstream set changes, so a removed or edited
// upstream stops holding connections. A DoH request still in flight on a
// dropped client returns its connection to that client's pool, where
// IdleConnTimeout closes it; a stream connection in use is closed when its
// exchange ends.
func resetUpstreamTransports() {
	dohUpstreams.Range(func(key, value any) bool {
		dohUpstreams.Delete(key)
		checkedDoHUpstream(value).client.CloseIdleConnections()
		return true
	})
	streamPools.Range(func(key, value any) bool {
		streamPools.Delete(key)
		checkedStreamPool(value).close()
		return true
	})
}

// Internal stores only contain these concrete transport owners.
func checkedDoHUpstream(value any) *dohUpstream {
	upstream, ok := value.(*dohUpstream)
	if !ok {
		panic("DoH transport store has an invalid internal type")
	}
	return upstream
}

func checkedStreamPool(value any) *streamPool {
	pool, ok := value.(*streamPool)
	if !ok {
		panic("stream transport store has an invalid internal type")
	}
	return pool
}

func closeUpstreamTransport(transport io.Closer) {
	if err := transport.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		dnsErrorLogger(logResolver, err).Warn("upstream transport cleanup failed")
	}
}
