package main

import (
	"errors"
	"github.com/miekg/dns"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type failedOutput struct{}

func (failedOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCommandFlagsAndFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		code       int
		diagnostic string
	}{
		{[]string{"-unknown"}, 2, "flag provided but not defined: -unknown"},
		{[]string{"-help"}, 0, "Usage of dnsbench:"},
		{[]string{"-source-net", "::1"}, 2, "dnsbench: -source-net \"::1\" is not an IPv4 address"},
		{[]string{"-clients", "0"}, 1, "dnsbench: -clients must be positive"},
		{[]string{"-blocked-pct", "80", "-miss-pct", "30"}, 1, "-blocked-pct + -miss-pct must be between 0 and 100"},
		{[]string{}, 1, "-blocklist is required when -blocked-pct > 0"},
	} {
		var output, diagnostics strings.Builder
		code := command(tc.args, &output, &diagnostics)
		if code != tc.code || !strings.Contains(diagnostics.String(), tc.diagnostic) || output.Len() != 0 {
			t.Fatalf("%v: code=%d output=%q diagnostics=%q", tc.args, code, output.String(), diagnostics.String())
		}
	}
}

func TestCommandTextAndJSONUseActualTargetAndFailuresPropagate(t *testing.T) {
	target := benchmarkUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(req)
		if err := w.WriteMsg(reply); err != nil {
			t.Error(err)
		}
	})
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-server", host, "-port", port, "-clients", "1", "-queries", "2", "-warmup", "0", "-blocked-pct", "0", "-rate", "500", "-sources", "1", "-source-net", "127.0.0.0"}
	var output, diagnostics strings.Builder
	if code := command(args, &output, &diagnostics); code != 0 || diagnostics.Len() != 0 {
		t.Fatalf("code=%d diagnostics=%s", code, diagnostics.String())
	}
	if !strings.HasPrefix(output.String(), "=== "+target+" ("+target+") ===\nqueries 2  clients 1  mix blocked=0% miss=0%\nanswered 2  blocked 0  timeouts 0  errors 0\nthroughput ") || !strings.Contains(output.String(), "latency p50 ") {
		t.Fatalf("text output=%q", output.String())
	}
	for _, jsonFlag := range []bool{false, true} {
		cmdArgs := append([]string{}, args...)
		if jsonFlag {
			cmdArgs = append(cmdArgs, "-json")
		}
		diagnostics.Reset()
		if code := command(cmdArgs, failedOutput{}, &diagnostics); code != 1 || !strings.Contains(diagnostics.String(), io.ErrClosedPipe.Error()) {
			t.Fatalf("output failure code=%d diagnostics=%s", code, diagnostics.String())
		}
	}
	if code := command([]string{"-source-net", "invalid"}, io.Discard, failedOutput{}); code != 1 {
		t.Fatalf("diagnostic failure code=%d", code)
	}
	if code := command([]string{"-clients", "0"}, io.Discard, failedOutput{}); code != 1 {
		t.Fatalf("run diagnostic failure code=%d", code)
	}
}

func TestTextSummaryLiteralContract(t *testing.T) {
	var out strings.Builder
	err := printText(&out, Summary{Label: "run", Target: "127.0.0.1:53", Queries: 4, Clients: 2, Answered: 1, Blocked: 1, Timeouts: 1, Errors: 1, QPS: 2, WallSec: 1, P50Micros: 10, P95Micros: 20, P99Micros: 30, P999Micros: 40, MaxMicros: 50})
	if err != nil {
		t.Fatal(err)
	}
	want := "=== run (127.0.0.1:53) ===\nqueries 4  clients 2  mix blocked=0% miss=0%\nanswered 1  blocked 1  timeouts 1  errors 1\nthroughput 2 qps over 1.00s\nlatency p50 10µs  p95 20µs  p99 30µs  p99.9 40µs  max 50µs\n"
	if out.String() != want {
		t.Fatalf("output=%q want=%q", out.String(), want)
	}
	if err := printText(failedOutput{}, Summary{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error=%v", err)
	}
}

func TestDriveTimesOutWhenRealUDPUpstreamDoesNotReply(t *testing.T) {
	target := benchmarkUpstream(t, func(dns.ResponseWriter, *dns.Msg) {})
	got, _ := drive(config{target: target, clients: 1, timeout: 10 * time.Millisecond}, []string{"example.org."})
	if len(got) != 1 || got[0].outcome != outcomeTimeout {
		t.Fatalf("outcome=%+v", got)
	}
}
