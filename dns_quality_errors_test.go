package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

type failingDNSQualityWriter struct {
	*mockDNSWriter
	attempts int
}

func (w *failingDNSQualityWriter) WriteMsg(*dns.Msg) error {
	w.attempts++
	return errors.New("write private-write.example. to 10.42.1.42 failed")
}
func (w *failingDNSQualityWriter) Write([]byte) (int, error) {
	w.attempts++
	return 0, errors.New("write private-write.example. to 10.42.1.42 failed")
}

func TestDNSWriteFailuresAreObservableWithoutQueryDisclosure(t *testing.T) {
	for _, path := range []string{"reply", "panic", "truncated", "cached"} {
		t.Run(path, func(t *testing.T) {
			defer setupTestDB(t)()
			previous, previousFlag := logDNS, logQueryLines.Load()
			defer func() { logDNS = previous; logQueryLines.Store(previousFlag) }()
			var output bytes.Buffer
			logDNS = slog.New(slog.NewJSONHandler(&output, nil))
			logQueryLines.Store(false)
			req := new(dns.Msg)
			req.SetQuestion("private-write.example.", dns.TypeA)
			writer := &failingDNSQualityWriter{mockDNSWriter: newMockWriter("10.42.1.42")}
			switch path {
			case "reply":
				writeRcode(writer, req, dns.RcodeRefused, true)
			case "panic":
				func() { defer recoverDNSHandler(writer, req); panic("fixture") }()
			case "truncated":
				reply := nxdomain(req)
				wire, err := reply.Pack()
				if err != nil {
					t.Fatal(err)
				}
				q := dnsQuery{w: writer, req: req}
				q.writeTruncated(wire, 512)
			case "cached":
				stub := startUpstreamStub(t, "udp", answerA("203.0.113.9"))
				useUpstreams(t, stub.addr)
				first := newMockWriter("10.42.1.42")
				handleDNSRequest(first, req)
				if first.written == nil {
					t.Fatal("cache prime did not answer")
				}
				handleDNSRequest(writer, req)
				if len(stub.queries()) != 1 {
					t.Fatal("expected cached second reply")
				}
			}
			if writer.attempts != 1 {
				t.Fatalf("write attempts = %d; must not retry partially written DNS replies", writer.attempts)
			}
			logs := output.String()
			if !strings.Contains(logs, "DNS response write failed") || !strings.Contains(logs, "error_class") {
				t.Errorf("missing safe write diagnostic: %s", logs)
			}
			for _, private := range []string{"private-write.example.", "10.42.1.42"} {
				if strings.Contains(logs, private) {
					t.Errorf("write failure disclosed %q", private)
				}
			}
		})
	}
}

func TestBootstrapStartupDiagnosticDistinguishesUnavailableStorage(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "unreadable"}[broken], func(t *testing.T) {
			defer setupTestDB(t)()
			useUpstreams(t, "tls://private-upstream.example")
			if _, err := db.Exec("DELETE FROM bootstrap_servers"); err != nil {
				t.Fatal(err)
			}
			if broken {
				if _, err := db.Exec("DROP TABLE bootstrap_servers"); err != nil {
					t.Fatal(err)
				}
			}
			previous := slog.Default()
			defer slog.SetDefault(previous)
			var output bytes.Buffer
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			warnIfDoHWithoutBootstrap()
			logs := output.String()
			if broken {
				if !strings.Contains(logs, "read bootstrap configuration failed") || strings.Contains(logs, "no bootstrap DNS servers") {
					t.Errorf("storage failure misdiagnosed: %s", logs)
				}
			} else if !strings.Contains(logs, "hostname resolution requires configured bootstrap servers") {
				t.Errorf("missing actual bootstrap requirement: %s", logs)
			}
			if strings.Contains(logs, "will use system resolver") {
				t.Errorf("advertised forbidden fallback: %s", logs)
			}
		})
	}
}
