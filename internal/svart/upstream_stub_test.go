package svart

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// upstreamStub is a local DNS server standing in for an upstream resolver.
// It records a copy of every query it receives so tests can assert exactly
// what svart put on the wire.
type upstreamStub struct {
	addr     string
	accepted atomic.Int64 // TCP connections accepted
	discard  atomic.Bool  // benchmarks: do not record queries

	mu   sync.Mutex
	seen []*dns.Msg
}

func (s *upstreamStub) record(r *dns.Msg) {
	if s.discard.Load() {
		return
	}
	s.mu.Lock()
	s.seen = append(s.seen, r.Copy())
	s.mu.Unlock()
}

func (s *upstreamStub) queries() []*dns.Msg {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*dns.Msg, len(s.seen))
	copy(out, s.seen)
	return out
}

// stubHandler wraps an answer function; a nil reply means "stay silent".
func (s *upstreamStub) handler(t testing.TB, answer func(*dns.Msg) *dns.Msg) dns.HandlerFunc {
	return func(w dns.ResponseWriter, r *dns.Msg) {
		s.record(r)
		if reply := answer(r); reply != nil {
			if err := w.WriteMsg(reply); err != nil {
				t.Errorf("DNS fixture response: %v", err)
			}
		}
	}
}

func serveStub(t testing.TB, srv *dns.Server) {
	t.Helper()
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ActivateAndServe() }()
	select {
	case <-started:
	case err := <-errCh:
		t.Fatalf("stub DNS server failed to start: %v", err)
	}
	t.Cleanup(func() {
		fixtureErr1502 := srv.Shutdown()
		if fixtureErr1502 !=

			nil {
			t.Errorf("fixture operation failed: %v", fixtureErr1502)
		}
	})
}

// startUpstreamStub starts a stub on 127.0.0.1 for network "udp" or "tcp".
func startUpstreamStub(t testing.TB, network string, answer func(*dns.Msg) *dns.Msg) *upstreamStub {
	t.Helper()
	return startUpstreamStubWith(t, network, answer, nil)
}

// startUpstreamStubWith lets the test adjust the dns.Server before it starts.
func startUpstreamStubWith(t testing.TB, network string, answer func(*dns.Msg) *dns.Msg, configure func(*dns.Server)) *upstreamStub {
	t.Helper()
	s := &upstreamStub{}
	srv := &dns.Server{Net: network, Handler: s.handler(t, answer)}
	if configure != nil {
		configure(srv)
	}
	switch network {
	case "udp":
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen udp: %v", err)
		}
		srv.PacketConn = pc
		s.addr = pc.LocalAddr().String()
	case "tcp":
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen tcp: %v", err)
		}
		srv.Listener = countingListener{Listener: l, n: &s.accepted}
		s.addr = l.Addr().String()
	default:
		t.Fatalf("unsupported stub network %q", network)
	}
	serveStub(t, srv)
	return s
}

// startUpstreamStubPair starts a UDP stub and a TCP stub on the same port,
// which is how a real resolver is reachable for the RFC 7766 TCP fallback.
func startUpstreamStubPair(t testing.TB, udpAnswer, tcpAnswer func(*dns.Msg) *dns.Msg) (udp, tcp *upstreamStub) {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen udp: %v", err)
		}
		addr := pc.LocalAddr().String()
		l, err := net.Listen("tcp", addr)
		if err != nil {
			checkTestClose(t, pc) // TCP port already taken; pick another pair
			continue
		}
		udp, tcp = &upstreamStub{addr: addr}, &upstreamStub{addr: addr}
		serveStub(t, &dns.Server{Net: "udp", PacketConn: pc, Handler: udp.handler(t, udpAnswer)})
		serveStub(t, &dns.Server{Net: "tcp", Listener: l, Handler: tcp.handler(t, tcpAnswer)})
		return udp, tcp
	}
	t.Fatal("could not bind a UDP+TCP port pair")
	return nil, nil
}

// answerA replies with one A record owned by the question name.
func answerA(ip string) func(*dns.Msg) *dns.Msg {
	parsed := net.ParseIP(ip)
	return func(r *dns.Msg) *dns.Msg {
		m := new(dns.Msg)
		m.SetReply(r)
		if len(r.Question) == 1 && r.Question[0].Qtype == dns.TypeA {
			m.Answer = append(m.Answer, &dns.A{
				Hdr: dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
				A:   parsed,
			})
		}
		return m
	}
}

// counterValue reads a Prometheus counter's current value.
func counterValue(t testing.TB, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("read counter: %v", err)
	}
	return m.GetCounter().GetValue()
}

// useUpstreams replaces the configured upstream set with the given entries,
// all enabled, through the same DB load path production uses.
func useUpstreams(t testing.TB, upstreams ...string) {
	t.Helper()
	if _, err := db.Exec("DELETE FROM upstreams"); err != nil {
		t.Fatalf("clear upstreams: %v", err)
	}
	for _, u := range upstreams {
		if _, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, 1)", u); err != nil {
			t.Fatalf("insert upstream %q: %v", u, err)
		}
	}
	if err := loadUpstreamsFromDB(); err != nil {
		t.Fatalf("load upstreams: %v", err)
	}
	upstreamStatsMap = sync.Map{}
}
