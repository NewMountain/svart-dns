package svart

import (
	"bytes"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

func TestDNSDiagnosticPathsRespectQueryPrivacy(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, path := range []string{"blocked", "cname", "no-upstream", "mismatch-weighted", "mismatch-random", "panic"} {
			t.Run(fmt.Sprintf("%s/optin=%t", path, enabled), func(t *testing.T) {
				t.Cleanup(setupTestDB(t))
				previousDNS, previousResolver, previousFlag, previousDebug := logDNS, logResolver, logQueryLines.Load(), dnsDebugEnabled
				defer func() {
					logDNS, logResolver = previousDNS, previousResolver
					logQueryLines.Store(previousFlag)
					dnsDebugEnabled = previousDebug
				}()
				var output bytes.Buffer
				logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
				logDNS, logResolver = logger, logger
				logQueryLines.Store(enabled)
				dnsDebugEnabled = true
				name, target, client := fmt.Sprintf("private-%s-%t.example.", path, enabled), "private-cname-target.example", "10.42.1.42"
				req := new(dns.Msg)
				req.SetQuestion(name, dns.TypeA)
				writer := newMockWriter(client)
				wantCode := dns.RcodeServerFailure
				switch path {
				case "blocked", "cname":
					blocked := strings.TrimSuffix(name, ".")
					if path == "cname" {
						blocked = target
					}
					secdnsBlockFor(t, client, blocked)
					if path == "cname" {
						stub := startUpstreamStub(t, "udp", func(r *dns.Msg) *dns.Msg {
							reply := new(dns.Msg)
							reply.SetReply(r)
							reply.Answer = []dns.RR{&dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60}, Target: target + "."}, &dns.A{Hdr: dns.RR_Header{Name: target + ".", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: net.ParseIP("203.0.113.9")}}
							return reply
						})
						useUpstreams(t, stub.addr)
					}
					wantCode = dns.RcodeNameError
				case "no-upstream":
					useUpstreams(t, "[/unused.test/]127.0.0.1:1")
				case "mismatch-weighted", "mismatch-random":
					stub := startUpstreamStub(t, "udp", forgedA(func(m *dns.Msg) { m.Question[0].Name = target + "."; m.Answer[0].Header().Name = target + "." }))
					useUpstreams(t, stub.addr)
					settings := getRuntimeSettings()
					settings.strategy = strings.TrimPrefix(path, "mismatch-")
					runtimeSettings.Store(settings)
				}
				if path == "panic" {
					func() { defer recoverDNSHandler(writer, req); panic("panic containing " + name) }()
				} else {
					handleDNSRequest(writer, req)
				}
				if writer.written == nil || writer.written.Rcode != wantCode {
					t.Fatalf("expected DNS rcode %d, got %v", wantCode, writer.written)
				}
				logs := output.String()
				for _, private := range []string{name, target, client} {
					if !enabled && strings.Contains(logs, private) {
						t.Errorf("opted-out %s disclosed query identity %q", path, private)
					}
				}
				if enabled && !strings.Contains(logs, name) {
					t.Errorf("opted-in %s lost query detail: %s", path, logs)
				}
				if path == "no-upstream" && !enabled && !strings.Contains(logs, `"error_class":"no_enabled_upstream"`) {
					t.Errorf("missing safe diagnostic: %s", logs)
				}
				if strings.HasPrefix(path, "mismatch-") && !enabled && !strings.Contains(logs, `"error_class":"reply_mismatch"`) {
					t.Errorf("missing mismatch diagnostic: %s", logs)
				}
				if path == "panic" && !strings.Contains(logs, `"stack"`) {
					t.Error("panic lost stack diagnostic")
				}
			})
		}
	}
}
