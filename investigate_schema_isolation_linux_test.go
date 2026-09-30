//go:build linux

package main

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestInvestigateSchemaWorkerPreservesConcurrentDNS(t *testing.T) {
	finalDigestArchive(t)
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})
	udp, tcp := secdnsServe(t)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		handleAPIInvestigateSchema(rec, httptest.NewRequest("GET", "/api/investigate/schema", nil))
		done <- rec
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	probes := 0
	var worst time.Duration
	for {
		select {
		case rec := <-done:
			if rec.Code != 200 {
				t.Fatalf("schema HTTP%d %s", rec.Code, rec.Body.String())
			}
			if probes < 2 {
				t.Fatalf("observed worker/DNS probes=%d want>=2", probes)
			}
			if pids := investigationChildPIDs(t); len(pids) != 0 {
				t.Fatalf("unreaped schema workers=%v", pids)
			}
			t.Logf("schema HTTP200 DNS probes=%d worst=%v live_children=0", probes, worst)
			return
		case <-timeout.C:
			t.Fatal("schema worker exceeded10s")
		case <-ticker.C:
			if len(investigationChildPIDs(t)) == 0 {
				continue
			}
			for _, endpoint := range []struct{ network, address string }{{"udp", udp}, {"tcp", tcp}} {
				question := new(dns.Msg)
				question.SetQuestion("printer.home.arpa.", dns.TypeA)
				begin := time.Now()
				answer := secdnsExchange(t, endpoint.network, endpoint.address, question)
				elapsed := time.Since(begin)
				if answer.Rcode != dns.RcodeSuccess || len(answer.Answer) != 1 || requireFixtureType[*dns.A](t, answer.Answer[0]).A.String() != "192.168.1.40" {
					t.Fatalf("DNS answer=%v", answer)
				}
				if elapsed > 200*time.Millisecond {
					t.Fatalf("%s DNS=%v exceeds200ms", endpoint.network, elapsed)
				}
				if elapsed > worst {
					worst = elapsed
				}
				probes++
			}
		}
	}
}
