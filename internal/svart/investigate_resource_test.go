package svart

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestInvestigateCTEAmplificationCancelsWhileDNSResponds(t *testing.T) {
	setupInvestigateSandbox(t)
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<500)
 INSERT INTO query_logs (timestamp,client_ip,query_name,query_type,coalesced_count)
 SELECT '2026-09-26 12:00:00','192.0.2.42','www.example.com','A',i FROM n`); err != nil {
		t.Fatal(err)
	}
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})
	udp, _ := secdnsServe(t)
	server := httptest.NewServer(http.HandlerFunc(handleAPIInvestigate))
	defer server.Close()
	// One physical source reference, amplified through CTE aliases. This must
	// remain safe even though it is below the direct-source reference limit.
	payload, fixtureErr937 := json.Marshal(map[string]interface{}{"timeout": 1, "sql": `WITH traffic AS (SELECT coalesced_count FROM query_logs)
 SELECT SUM(sin(a.coalesced_count*b.coalesced_count+c.coalesced_count*d.coalesced_count))
 FROM traffic a, traffic b, traffic c, traffic d`})
	if fixtureErr937 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr937)
	}
	type result struct {
		code int
		body string
		err  error
	}
	finished := make(chan result, 1)
	begin := time.Now()
	go func() {
		response, err := server.Client().Post(server.URL, "application/json", strings.NewReader(string(payload)))
		if err != nil {
			finished <- result{err: err}
			return
		}
		defer func() { checkTestClose(t, response.Body) }()
		body, err := io.ReadAll(response.Body)
		finished <- result{response.StatusCode, string(body), err}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for !investigateBusy.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !investigateBusy.Load() {
		t.Fatal("investigation never became active")
	}
	for i := 0; i < 20; i++ {
		q := new(dns.Msg)
		q.SetQuestion("printer.home.arpa.", dns.TypeA)
		started := time.Now()
		r := secdnsExchange(t, "udp", udp, q)
		if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 || requireFixtureType[*dns.A](t, r.Answer[0]).A.String() != "192.168.1.40" {
			t.Fatalf("DNS response = %v", r)
		}
		if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
			t.Errorf("DNS took %v; limit 200ms", elapsed)
		}
	}
	select {
	case result := <-finished:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.code != 504 || !strings.Contains(result.body, "timeout") {
			t.Fatalf("expensive CTE response = %d %s; want 504 timeout", result.code, result.body)
		}
		assertNoSandboxSecrets(t, result.body)
	case <-time.After(7 * time.Second):
		t.Fatal("CTE query failed to honor hard timeout")
	}
	if elapsed := time.Since(begin); elapsed > 3*time.Second {
		t.Fatalf("CTE cancellation took %v; want under 3s", elapsed)
	}
	code, _, raw := postInvestigate(t, "SELECT COUNT(*) AS count FROM query_logs WHERE client_ip = '192.0.2.42'")
	if code != 200 || !strings.Contains(raw, `"rows":[[500]]`) {
		t.Fatalf("post-cancellation recovery = %d %s", code, raw)
	}
	t.Log("CTE-amplified query returned 504 within 3s; twenty concurrent UDP probes under 200ms; next query read all 500 seeded client rows")
}
