package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// The httptest certificate is valid for 127.0.0.1, ::1, example.com and
// *.example.com. Tests use dot.example.com / doh.example.com as the
// configured upstream hostname (covered) and dns.trusted-resolver.test
// (not covered) as the hostname an attacker's certificate does not match.

// trustTestUpstreamCA makes svart's upstream TLS trust the httptest CA for
// the duration of the test.
func trustTestUpstreamCA(t testing.TB, cert *x509.Certificate) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	old := upstreamRootCAs
	upstreamRootCAs = pool
	t.Cleanup(func() { upstreamRootCAs = old })
}

// useBootstrap points bootstrap resolution at local stub servers.
func useBootstrap(t testing.TB, servers ...string) {
	t.Helper()
	if _, err := db.Exec("DELETE FROM bootstrap_servers"); err != nil {
		t.Fatalf("clear bootstrap servers: %v", err)
	}
	for _, s := range servers {
		if _, err := db.Exec("INSERT INTO bootstrap_servers (server) VALUES (?)", s); err != nil {
			t.Fatalf("insert bootstrap server %q: %v", s, err)
		}
	}
}

// dohStub is a DoH upstream on 127.0.0.1 that counts TCP connections.
type dohStub struct {
	srv    *httptest.Server
	opened atomic.Int64
	closed atomic.Int64
	h2     atomic.Int64
	sni    sync.Map // ServerName -> true
}

func startDoHStub(t testing.TB, respond func(w http.ResponseWriter, q *dns.Msg)) *dohStub {
	t.Helper()
	s := &dohStub{}
	s.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 {
			s.h2.Add(1)
		}
		if r.TLS != nil {
			s.sni.Store(r.TLS.ServerName, true)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := new(dns.Msg)
		if err := q.Unpack(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		respond(w, q)
	}))
	s.srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		switch state {
		case http.StateNew:
			s.opened.Add(1)
		case http.StateClosed, http.StateHijacked:
			s.closed.Add(1)
		}
	}
	s.srv.EnableHTTP2 = true
	s.srv.StartTLS()
	t.Cleanup(s.srv.Close)
	trustTestUpstreamCA(t, s.srv.Certificate())
	return s
}

func (s *dohStub) port() string {
	_, port, err := net.SplitHostPort(strings.TrimPrefix(s.srv.URL, "https://"))
	if err != nil {
		panic(err)
	}
	return port
}

func writeDNSMessage(t testing.TB, w http.ResponseWriter, contentType string, m *dns.Msg, trailing int) {
	out, err := m.Pack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(out); err != nil {
		t.Errorf("DoH fixture message: %v", err)
	}
	if trailing > 0 {
		_, err := w.Write(make([]byte, trailing))
		checkRejectedResponseWrite(t, err)
	}
}

func dohAnswerA(t testing.TB, ip string) func(http.ResponseWriter, *dns.Msg) {
	return func(w http.ResponseWriter, q *dns.Msg) {
		writeDNSMessage(t, w, "application/dns-message", answerA(ip)(q), 0)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queryA(t *testing.T, upstream, name string) (*dns.Msg, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return queryUpstream(ctx, clientQuery(name, dns.TypeA, 0x4242, false, false), upstream)
}

// TestDoHReusesOneConnection is the D5 regression test: svart built a new
// http.Transport per query, so 200 queries cost 200 TLS handshakes and
// leaked 200 idle connections.
func TestDoHReusesOneConnection(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoHStub(t, dohAnswerA(t, "192.0.2.1"))
	upstream := stub.srv.URL + "/dns-query"

	for i := 0; i < 20; i++ {
		resp, err := queryA(t, upstream, fmt.Sprintf("seq-%d.example.com.", i))
		if err != nil {
			t.Fatalf("sequential query %d: %v", i, err)
		}
		if len(resp.Answer) != 1 {
			t.Fatalf("sequential query %d: answers %v", i, resp.Answer)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("par-%d.example.com.", i)
			resp, err := queryA(t, upstream, name)
			if err != nil {
				errs <- err
				return
			}
			if resp.Question[0].Name != name {
				errs <- fmt.Errorf("query %s got reply for %s", name, resp.Question[0].Name)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent query: %v", err)
	}

	if got := stub.opened.Load(); got != 1 {
		t.Errorf("52 DoH queries opened %d TLS connections, want 1", got)
	}
	if got := stub.h2.Load(); got != 52 {
		t.Errorf("%d of 52 DoH requests used HTTP/2, want all", got)
	}
}

// TestDoHReloadKeepsOrDropsConnections: reloading an unchanged upstream list
// (sync merges reload on every poll) must keep the pooled connection, while
// a real change must close the old client's idle connections.
func TestDoHReloadKeepsOrDropsConnections(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoHStub(t, dohAnswerA(t, "192.0.2.1"))
	upstream := stub.srv.URL + "/dns-query"
	useUpstreams(t, upstream)

	if _, err := queryA(t, upstream, "before.example.com."); err != nil {
		t.Fatalf("first query: %v", err)
	}
	if err := loadUpstreamsFromDB(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, err := queryA(t, upstream, "after-reload.example.com."); err != nil {
		t.Fatalf("query after unchanged reload: %v", err)
	}
	if got := stub.opened.Load(); got != 1 {
		t.Errorf("unchanged reload: %d connections opened, want 1", got)
	}

	useUpstreams(t, upstream, "192.0.2.53:53")
	waitFor(t, "the old DoH connection to close after the upstream list changed", func() bool {
		return stub.closed.Load() == 1
	})
}

// TestDoHVerifiesURLHostnameViaBootstrapIP: svart dials the IP the
// bootstrap servers returned but authenticates the URL's hostname.
func TestDoHVerifiesURLHostnameViaBootstrapIP(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoHStub(t, dohAnswerA(t, "192.0.2.1"))
	bootstrap := startUpstreamStub(t, "udp", answerA("127.0.0.1"))
	useBootstrap(t, bootstrap.addr)

	good := "https://doh.example.com:" + stub.port() + "/dns-query"
	if resp, err := queryA(t, good, "www.example.org."); err != nil || len(resp.Answer) != 1 {
		t.Fatalf("DoH via bootstrap to a hostname the certificate covers: resp=%v err=%v", resp, err)
	}
	if _, ok := stub.sni.Load("doh.example.com"); !ok {
		t.Errorf("DoH TLS handshake did not send SNI doh.example.com")
	}

	bad := "https://dns.trusted-resolver.test:" + stub.port() + "/dns-query"
	if resp, err := queryA(t, bad, "www.example.org."); err == nil {
		t.Fatalf("DoH accepted a certificate not valid for dns.trusted-resolver.test: %v", resp)
	}
}

// TestDoHRejectsMalformedHTTPReplies: DNS messages are at most 65535 bytes,
// must be application/dns-message, and a DoH server must not be able to
// bounce svart's queries to another URL.
func TestDoHRejectsMalformedHTTPReplies(t *testing.T) {
	defer setupTestDB(t)()

	tests := []struct {
		name    string
		respond func(w http.ResponseWriter, q *dns.Msg)
	}{
		{"body over 65535 bytes", func(w http.ResponseWriter, q *dns.Msg) {
			writeDNSMessage(t, w, "application/dns-message", answerA("192.0.2.1")(q), 70000)
		}},
		{"wrong content type", func(w http.ResponseWriter, q *dns.Msg) {
			writeDNSMessage(t, w, "text/html; charset=utf-8", answerA("192.0.2.1")(q), 0)
		}},
		{"missing content type", func(w http.ResponseWriter, q *dns.Msg) {
			out, fixtureErr7655 := answerA("192.0.2.1")(q).Pack()
			if fixtureErr7655 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr7655)
			}
			w.Header()["Content-Type"] = nil
			var fixtureErr7735 error
			_, fixtureErr7735 = w.Write(out)
			if fixtureErr7735 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr7735)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := startDoHStub(t, tt.respond)
			if resp, err := queryA(t, stub.srv.URL+"/dns-query", "www.example.org."); err == nil {
				t.Fatalf("DoH accepted the reply: %v", resp)
			}
		})
	}

	t.Run("redirect not followed", func(t *testing.T) {
		var redirected atomic.Int64
		stub := &dohStub{}
		stub.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/dns-query" {
				http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
				return
			}
			redirected.Add(1)
			body, fixtureErr8374 := io.ReadAll(r.Body)
			if fixtureErr8374 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr8374)
			}
			q := new(dns.Msg)
			if err := q.Unpack(body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			dohAnswerA(t, "192.0.2.1")(w, q)
		}))
		stub.srv.StartTLS()
		t.Cleanup(stub.srv.Close)
		trustTestUpstreamCA(t, stub.srv.Certificate())

		if resp, err := queryA(t, stub.srv.URL+"/dns-query", "www.example.org."); err == nil {
			t.Errorf("DoH followed a redirect and accepted %v", resp)
		}
		if got := redirected.Load(); got != 0 {
			t.Errorf("redirect target received %d queries, want 0", got)
		}
	})
}

// dotStub is a DoT upstream on 127.0.0.1 that records SNI and connections.
type dotStub struct {
	addr     string
	port     string
	accepted atomic.Int64
	sni      sync.Map
	rec      *upstreamStub // queries received
}

func startDoTStub(t testing.TB, cfg func(*tls.Config), answer func(*dns.Msg) *dns.Msg) *dotStub {
	t.Helper()
	hs := httptest.NewUnstartedServer(http.NotFoundHandler())
	hs.StartTLS()
	t.Cleanup(hs.Close)
	trustTestUpstreamCA(t, hs.Certificate())

	s := &dotStub{}
	tlsCfg := &tls.Config{
		Certificates: hs.TLS.Certificates,
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			s.sni.Store(hello.ServerName, true)
			return nil, nil
		},
	}
	if cfg != nil {
		cfg(tlsCfg)
	}
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	l := tls.NewListener(countingListener{Listener: inner, n: &s.accepted}, tlsCfg)
	s.addr = inner.Addr().String()
	var fixtureErr9884 error
	_, s.port, fixtureErr9884 = net.SplitHostPort(s.addr)
	if fixtureErr9884 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr9884)
	}
	s.rec = &upstreamStub{addr: s.addr}
	serveStub(t, &dns.Server{Net: "tcp-tls", Listener: l, Handler: s.rec.handler(t, answer)})
	return s
}

type countingListener struct {
	net.Listener
	n *atomic.Int64
}

func (l countingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err == nil {
		l.n.Add(1)
	}
	return c, err
}

// TestDoTVerifiesConfiguredHostname is the D7 regression test: svart used to
// verify the DoT certificate against the bootstrap-resolved IP, so a spoofed
// plaintext bootstrap answer plus any IP-SAN certificate was a MITM of the
// "encrypted" upstream.
func TestDoTVerifiesConfiguredHostname(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoTStub(t, nil, answerA("192.0.2.1"))
	bootstrap := startUpstreamStub(t, "udp", answerA("127.0.0.1"))
	useBootstrap(t, bootstrap.addr)

	resp, err := queryA(t, "tls://dot.example.com:"+stub.port, "www.example.org.")
	if err != nil || len(resp.Answer) != 1 {
		t.Fatalf("DoT to a hostname the certificate covers: resp=%v err=%v", resp, err)
	}
	if _, ok := stub.sni.Load("dot.example.com"); !ok {
		t.Errorf("DoT handshake did not send SNI dot.example.com")
	}

	if resp, err := queryA(t, "tls://dns.trusted-resolver.test:"+stub.port, "bank.example."); err == nil {
		t.Fatalf("DoT accepted a certificate not valid for dns.trusted-resolver.test (only for its IP): %v", resp.Answer)
	}
}

// TestDoTRefusesLegacyTLS pins the TLS 1.2 floor for DoT upstreams.
func TestDoTRefusesLegacyTLS(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoTStub(t, func(c *tls.Config) { c.MaxVersion = tls.VersionTLS11 }, answerA("192.0.2.1"))
	if resp, err := queryA(t, "tls://"+stub.addr, "www.example.org."); err == nil {
		t.Fatalf("DoT negotiated TLS < 1.2: %v", resp)
	}
}
