package main

import (
	"os"
	"os/exec"
	"testing"

	"github.com/miekg/dns"
)

// make test-api-clients bundles the tracked TypeScript fixture and runs this
// actual HTTP/DNS integration. Ordinary Go-only test runs need no Node.
func TestIndependentGeneratedClientsHTTPAndDNS(t *testing.T) {
	script := os.Getenv("SVART_REVIEW_CLIENT_SCRIPT")
	if script == "" {
		t.Skip("run make test-api-clients for the generated-client HTTP/DNS integration")
	}
	env := secSetup(t)
	seedCheckpointContract(t)
	localSQL(t, `INSERT INTO ip_ranges(id,name,cidr) VALUES(1,'Review loopback clients','127.0.0.0/8')`)
	oldLogging := loggingEnabled.Load()
	loggingEnabled.Store(false)
	defer loggingEnabled.Store(oldLogging)
	stub := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		if err := w.WriteMsg(answerA("198.51.100.42")(r)); err != nil {
			t.Errorf("DNS fixture response: %v", err)
		}
	})
	secdnsSetUpstream(t, stub)
	udp, tcp := secdnsServe(t)
	client := func(phase string) {
		// #nosec G204 G702 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
		cmd := exec.Command("node", script)
		cmd.Env = append(os.Environ(), "SVART_REVIEW_BASE="+env.base, "SVART_REVIEW_TOKEN="+env.admToken, "SVART_REVIEW_PHASE="+phase)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("generated-client phase %s: %v\n%s", phase, err, output)
		}
		t.Log(string(output))
	}
	client("create")
	for _, phase := range []string{"created", "deleted"} {
		if phase == "deleted" {
			client("delete")
		}
		mustReloadPolicy(t)
		for network, addr := range map[string]string{"udp": udp, "tcp": tcp} {
			for _, domain := range []string{"ads.example.com.", "docs.example.com."} {
				question := new(dns.Msg)
				question.SetQuestion(domain, dns.TypeA)
				reply := secdnsExchange(t, network, addr, question)
				if phase == "created" && domain == "ads.example.com." {
					if reply.Rcode != dns.RcodeNameError || len(reply.Answer) != 0 {
						t.Fatalf("%s block: %v", network, reply)
					}
				} else if reply.Rcode != dns.RcodeSuccess || len(reply.Answer) != 1 || requireFixtureType[*dns.A](t, reply.Answer[0]).A.String() != "198.51.100.42" {
					t.Fatalf("%s %s %s: %v", phase, network, domain, reply)
				}
			}
		}
		t.Logf("%s: exact persisted range policy applied over UDP and TCP", phase)
	}
}
