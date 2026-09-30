package svart

import (
	"fmt"
	"github.com/miekg/dns"
	"reflect"
	"testing"
)

func TestLocalRangeRulesPersistAndServeDNS(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	guard, err := loadDNSGuardConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	oldGuard := dnsGuard.Load()
	dnsGuard.Store(guard)
	t.Cleanup(func() { dnsGuard.Store(oldGuard) })
	oldLogging := loggingEnabled.Load()
	loggingEnabled.Store(false)
	defer loggingEnabled.Store(oldLogging)
	localSQL(t, `INSERT INTO ip_ranges(id,name,cidr) VALUES(1,'Loopback test clients','127.0.0.0/8'); INSERT INTO blocklists(id,alias,url,enabled,domain_count) VALUES(1,'Published fixture','https://lists.example.com/fixture.txt',1,1); INSERT INTO blocked_domains(blocklist_id,domain) VALUES(1,'docs.example.com'); INSERT INTO range_blocklists(range_id,blocklist_id) VALUES(1,1)`)
	stub := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(r)
		rr, fixtureErr973 := dns.NewRR(r.Question[0].Name + " 60 IN A 198.51.100.42")
		if fixtureErr973 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr973)
		}
		reply.Answer = []dns.RR{rr}
		if err := w.WriteMsg(reply); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	})
	secdnsSetUpstream(t, stub)
	udp, tcp := secdnsServe(t)
	mustReloadPolicy(t)
	initial := new(dns.Msg)
	initial.SetQuestion("docs.example.com.", dns.TypeA)
	if reply := secdnsExchange(t, "udp", udp, initial); reply.Rcode != dns.RcodeNameError {
		t.Fatalf("fixture must block before custom allow: %v", reply)
	}
	for _, rule := range []struct{ action, domain string }{{"block-domain", "ads.example.com"}, {"allow-domain", "docs.example.com"}} {
		rr := localHTTP(handleAPIRangeAction, "POST", "/api/ranges/1/"+rule.action, fmt.Sprintf(`{"domain":%q}`, rule.domain))
		if rr.Code != 200 {
			t.Fatalf("create %s: %d %s", rule.action, rr.Code, rr.Body.String())
		}
	}
	checkDetail := func(wantBlocked, wantAllowed []string) {
		t.Helper()
		rr := localHTTP(handleAPIRangeAction, "GET", "/api/ranges/1", "")
		if rr.Code != 200 {
			t.Fatalf("detail %d %s", rr.Code, rr.Body.String())
		}
		detail := decodeAPIData[RangeDetailView](t, rr)
		if !reflect.DeepEqual(detail.CustomBlocked, wantBlocked) || !reflect.DeepEqual(detail.CustomAllowed, wantAllowed) {
			t.Fatalf("custom rules blocked=%#v allowed=%#v", detail.CustomBlocked, detail.CustomAllowed)
		}
	}
	checkDetail([]string{"ads.example.com"}, []string{"docs.example.com"})
	// Reload from persisted rows as at startup, then exchange real UDP and TCP packets.
	mustReloadPolicy(t)
	for network, addr := range map[string]string{"udp": udp, "tcp": tcp} {
		query := new(dns.Msg)
		query.SetQuestion("ads.example.com.", dns.TypeA)
		reply := secdnsExchange(t, network, addr, query)
		if reply.Rcode != dns.RcodeNameError {
			t.Fatalf("%s custom block rcode=%s", network, dns.RcodeToString[reply.Rcode])
		}
		query.SetQuestion("docs.example.com.", dns.TypeA)
		reply = secdnsExchange(t, network, addr, query)
		if reply.Rcode != dns.RcodeSuccess || len(reply.Answer) != 1 {
			t.Fatalf("%s custom allow reply=%v", network, reply)
		}
		if got := requireFixtureType[*dns.A](t, reply.Answer[0]).A.String(); got != "198.51.100.42" {
			t.Fatalf("answer=%s", got)
		}
	}
	for _, rule := range []struct{ action, domain string }{{"block-domain", "ads.example.com"}, {"allow-domain", "docs.example.com"}} {
		rr := localHTTP(handleAPIRangeAction, "DELETE", "/api/ranges/1/"+rule.action+"/"+rule.domain, "")
		if rr.Code != 200 {
			t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
		}
	}
	checkDetail([]string{}, []string{})
	mustReloadPolicy(t)
	query := new(dns.Msg)
	query.SetQuestion("ads.example.com.", dns.TypeA)
	reply := secdnsExchange(t, "udp", udp, query)
	if reply.Rcode != dns.RcodeSuccess || len(reply.Answer) != 1 {
		t.Fatalf("deleted block still active: %v", reply)
	}
	query.SetQuestion("docs.example.com.", dns.TypeA)
	if reply := secdnsExchange(t, "tcp", tcp, query); reply.Rcode != dns.RcodeNameError {
		t.Fatalf("deleted allow still overrides published block: %v", reply)
	}
}
