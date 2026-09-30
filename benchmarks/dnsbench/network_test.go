package main

import (
	"encoding/json"
	"github.com/miekg/dns"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func benchmarkUpstream(t *testing.T, handler dns.HandlerFunc) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := &dns.Server{PacketConn: conn, Handler: handler, NotifyStartedFunc: func() { close(started) }}
	done := make(chan error, 1)
	go func() { done <- server.ActivateAndServe() }()
	<-started
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

func captureRun(t *testing.T, c config) (string, error) {
	t.Helper()
	var output strings.Builder
	runErr := run(c, &output)
	return output.String(), runErr
}

func TestRunMeasuresRealDNSAndReportsClassification(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "answered", true: "blocked"}[blocked], func(t *testing.T) {
			var calls atomic.Int32
			target := benchmarkUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
				calls.Add(1)
				reply := new(dns.Msg)
				reply.SetReply(req)
				address := net.IPv4(198, 18, 0, 42)
				if blocked {
					address = net.IPv4zero
				}
				reply.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: req.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: address}}
				if err := w.WriteMsg(reply); err != nil {
					t.Error(err)
				}
			})
			cfg := config{target: target, label: "local-world", clients: 1, queries: 4, warmup: 2, timeout: time.Second, jsonOut: true}
			if blocked {
				cfg.blockedPct = 100
				cfg.blocklist = filepath.Join(t.TempDir(), "rules.txt")
				if err := os.WriteFile(cfg.blocklist, []byte("ads.example.com\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			output, err := captureRun(t, cfg)
			if err != nil {
				t.Fatal(err)
			}
			var result Summary
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			want := []int{4, 0, 0, 0, 0, 0, 0}
			if blocked {
				want = []int{0, 4, 0, 0, 4, 0, 0}
			}
			got := []int{result.Answered, result.Blocked, result.Timeouts, result.Errors, result.ExpectedBlocked, result.UnexpectedAllowed, result.UnexpectedBlocked}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("classification=%v want%v", got, want)
			}
			if result.Label != "local-world" || result.Target != target || result.Clients != 1 || result.Queries != 4 || calls.Load() != 6 {
				t.Fatalf("report=%+v upstream calls=%d", result, calls.Load())
			}
			if result.WallSec <= 0 || result.QPS <= 0 || result.P50Micros <= 0 || result.P99Micros < result.P50Micros {
				t.Fatalf("invalid measured latencies=%+v", result)
			}
		})
	}
}

func TestUDPTruncationIsNotASuccessfulOrBlockedAnswer(t *testing.T) {
	for _, rcode := range []int{dns.RcodeSuccess, dns.RcodeNameError} {
		t.Run(dns.RcodeToString[rcode], func(t *testing.T) {
			target := benchmarkUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
				reply := new(dns.Msg)
				reply.SetReply(req)
				reply.Rcode = rcode
				reply.Truncated = true
				if err := w.WriteMsg(reply); err != nil {
					t.Error(err)
				}
			})
			output, err := captureRun(t, config{target: target, clients: 2, queries: 4, timeout: time.Second, jsonOut: true})
			if err != nil {
				t.Fatal(err)
			}
			var result Summary
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatal(err)
			}
			if result.Errors != 4 || result.Answered != 0 || result.Blocked != 0 || result.QPS != 0 {
				t.Fatalf("incomplete UDP replies counted as successful DNS: %+v", result)
			}
		})
	}
}

func TestDomainFilePreservesSupportedRulesAndReportsScannerFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "rules.txt")
	if err := os.WriteFile(file, []byte("# source\n\n! comment\n0.0.0.0 Ads.Example.COM\n127.0.0.1 localhost\n||tracker.example.org^\n*.wild.example.\n/path\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadDomains(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ads.example.com.", "tracker.example.org.", "wild.example."}; !reflect.DeepEqual(got, want) {
		t.Fatalf("domains=%v want%v", got, want)
	}
	if err := os.WriteFile(file, make([]byte, 1024*1024+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDomains(file); err == nil || err.Error() != "bufio.Scanner: token too long" {
		t.Fatalf("oversized file error=%v", err)
	}
}

func TestDriveClassifiesUnreachableTargetsAndInvalidDNSNames(t *testing.T) {
	for _, tc := range []struct{ target, name string }{{"bad-address", "example.org."}, {"127.0.0.1:0", "example.net."}, {"127.0.0.1:53", strings.Repeat("a", 64) + ".example."}} {
		got, _ := drive(config{target: tc.target, clients: 2, timeout: time.Millisecond}, []string{tc.name, tc.name})
		if len(got) != 2 || got[0].outcome != outcomeError || got[1].outcome != outcomeError {
			t.Fatalf("%s outcomes=%+v", tc.target, got)
		}
	}
}

func TestSummaryIncludesFailureCountsAndLiteralPercentiles(t *testing.T) {
	got := summarize(config{label: "fixture", target: "127.0.0.1:53", clients: 2, blockedPct: 25, missPct: 25}, []sample{{time.Microsecond, outcomeAnswered}, {4 * time.Microsecond, outcomeBlocked}, {0, outcomeTimeout}, {0, outcomeError}}, 2*time.Second)
	want := Summary{Label: "fixture", Target: "127.0.0.1:53", Clients: 2, Queries: 4, Answered: 1, Blocked: 1, Timeouts: 1, Errors: 1, WallSec: 2, QPS: 1, P50Micros: 4, P95Micros: 4, P99Micros: 4, P999Micros: 4, MaxMicros: 4, BlockedPct: 25, MissPct: 25}
	if got != want {
		t.Fatalf("summary=%+v want%+v", got, want)
	}
	empty := summarize(config{}, []sample{{outcome: outcomeTimeout}}, time.Second)
	if empty != (Summary{Queries: 1, Timeouts: 1, WallSec: 1}) {
		t.Fatalf("all timeout report=%+v", empty)
	}
}
