package main

import (
	"github.com/miekg/dns"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInputReadFailureCannotReportSuccessfulParity(t *testing.T) {
	var output, diagnostics strings.Builder
	code := command([]string{"-a", "127.0.0.1:1", "-b", "127.0.0.1:1"}, strings.NewReader(strings.Repeat("x", 1024*1024)), &output, &diagnostics)
	if code == 0 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "token too long") {
		t.Fatalf("incomplete input reported code=%d output=%q diagnostics=%q", code, output.String(), diagnostics.String())
	}
}

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestZeroWorkersCannotClaimSuccessfulComparison(t *testing.T) {
	var diagnostics strings.Builder
	code := command([]string{"-workers", "0"}, strings.NewReader(""), io.Discard, &diagnostics)
	if code != 2 || !strings.Contains(diagnostics.String(), "workers must be positive") {
		t.Fatalf("code=%d diagnostics=%q", code, diagnostics.String())
	}
}

func comparisonUpstream(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	server := &dns.Server{PacketConn: conn, Handler: handler, NotifyStartedFunc: func() { close(ready) }}
	done := make(chan error, 1)
	go func() { done <- server.ActivateAndServe() }()
	<-ready
	t.Cleanup(func() {
		if err := server.Shutdown(); err != nil {
			t.Error(err)
		}
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return conn.LocalAddr().String()
}

func TestComparisonReportsEveryDifferenceInInputOrder(t *testing.T) {
	a := comparisonUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		if err := w.WriteMsg(resp); err != nil {
			t.Error(err)
		}
	})
	b := comparisonUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		resp := new(dns.Msg)
		resp.SetReply(req)
		switch req.Question[0].Name {
		case "blocked.example.":
			resp.Rcode = dns.RcodeNameError
		case "sinkhole.example.":
			resp.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET}, A: net.IPv4zero}}
		}
		if err := w.WriteMsg(resp); err != nil {
			t.Error(err)
		}
	})
	args := []string{"-a", a, "-b", b, "-workers", "3"}
	input := " same.example \n\nblocked.example\nsinkhole.example\n"
	var output, diagnostics strings.Builder
	if code := command(args, strings.NewReader(input), &output, &diagnostics); code != 0 {
		t.Fatalf("code=%d diagnostics=%s", code, diagnostics.String())
	}
	want := "DIFF\tblocked.example\tNOERROR\tblocked\nDIFF\tsinkhole.example\tNOERROR\tblocked\n"
	if output.String() != want {
		t.Fatalf("output=%q want=%q", output.String(), want)
	}
	if diagnostics.String() != "names 3, differing 2, outcomes map[a:NOERROR:3 b:NOERROR:1 b:blocked:2]\n" {
		t.Fatalf("summary=%q", diagnostics.String())
	}
	if code := command(args, strings.NewReader(input), failedOutput{}, io.Discard); code != 1 {
		t.Fatalf("closed output code=%d", code)
	}
	if code := command(args, strings.NewReader("same.example"), io.Discard, failedOutput{}); code != 1 {
		t.Fatalf("closed diagnostics code=%d", code)
	}
}

func TestOutcomeRetriesLocalTimeoutAndPreservesDNSFailures(t *testing.T) {
	var calls atomic.Int32
	target := comparisonUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		if calls.Add(1) <= 2 {
			return
		}
		resp := new(dns.Msg)
		resp.SetReply(req)
		resp.Rcode = dns.RcodeRefused
		if err := w.WriteMsg(resp); err != nil {
			t.Error(err)
		}
	})
	if got := outcome(&dns.Client{Timeout: 10 * time.Millisecond}, target, "example.org"); got != "REFUSED" || calls.Load() != 3 {
		t.Fatalf("outcome=%s calls=%d", got, calls.Load())
	}
	if got := outcome(&dns.Client{Timeout: time.Millisecond}, "invalid-address", "example.org"); got != "timeout" {
		t.Fatalf("unreachable=%s", got)
	}
}

func TestCommandHelpParseAndDiagnosticFailures(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
	}{{[]string{"-help"}, 0}, {[]string{"-unknown"}, 2}, {[]string{"-workers", "-1"}, 2}} {
		var diagnostic strings.Builder
		if code := command(tc.args, strings.NewReader(""), io.Discard, &diagnostic); code != tc.code || diagnostic.Len() == 0 {
			t.Fatalf("%v code=%d diagnostic=%q", tc.args, code, diagnostic.String())
		}
	}
	if code := command([]string{"-workers", "0"}, strings.NewReader(""), io.Discard, failedOutput{}); code != 1 {
		t.Fatalf("invalid worker diagnostic code=%d", code)
	}
	if code := command(nil, strings.NewReader(strings.Repeat("x", 1024*1024)), io.Discard, failedOutput{}); code != 1 {
		t.Fatalf("input diagnostic code=%d", code)
	}
}
