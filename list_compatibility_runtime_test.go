package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"testing"

	"github.com/miekg/dns"
)

func compatibilityList(t *testing.T, lines ...string) int {
	t.Helper()
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, lines...)
	res, err := db.Exec("INSERT INTO blocklists (url,alias,enabled) VALUES (?,'Compatibility fixture',1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	client := queryClientIP(netip.MustParseAddr("127.0.0.1"))
	if _, err := db.Exec("INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES (?,?)", client, id); err != nil {
		t.Fatal(err)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func TestListCompatibilityRecordTypesOverDNS(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	compatibilityList(t, "|google.com|$dnstype=TXT", ` /^unused$/ `,
		`/^ad-\D+\.example$/`, "||alias.example^$dnstype=~CNAME")
	up := secdnsUpstream(t, "udp", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		name := r.Question[0].Name
		if name == "news.example." {
			m.Answer = append(m.Answer, &dns.CNAME{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300}, Target: "alias.example."})
		}
		if r.Question[0].Qtype == dns.TypeTXT {
			m.Answer = append(m.Answer, &dns.TXT{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300}, Txt: []string{"fixture"}})
		} else {
			m.Answer = append(m.Answer, &dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("203.0.113.42")})
		}
		if err := w.WriteMsg(m); err != nil {
			t.Error(err)
		}
	})
	secdnsSetUpstream(t, up)
	udpAddr, tcpAddr := secdnsServe(t)
	for _, transport := range []struct{ network, addr string }{{"udp", udpAddr}, {"tcp", tcpAddr}} {
		for round := 0; round < 2; round++ {
			for _, tc := range []struct {
				name     string
				typeCode uint16
				rcode    int
			}{
				{"google.com.", dns.TypeA, dns.RcodeSuccess},
				{"google.com.", dns.TypeTXT, dns.RcodeNameError},
				{"google.com.", dns.TypeA, dns.RcodeSuccess},
				{"www.google.com.", dns.TypeTXT, dns.RcodeSuccess},
				{"ad-text.example.", dns.TypeA, dns.RcodeNameError},
				{"ad-123.example.", dns.TypeA, dns.RcodeSuccess},
				{"news.example.", dns.TypeA, dns.RcodeSuccess},
				{"alias.example.", dns.TypeA, dns.RcodeNameError},
			} {
				q := new(dns.Msg)
				q.SetQuestion(tc.name, tc.typeCode)
				got := secdnsExchange(t, transport.network, transport.addr, q)
				if got.Rcode != tc.rcode {
					t.Errorf("%s round %d %s type %d: rcode=%d want %d", transport.network, round, tc.name, tc.typeCode, got.Rcode, tc.rcode)
				}
			}
		}
	}
}

func TestListCompatibilityPolicyAPIRecordType(t *testing.T) {
	defer setupTestDB(t)()
	compatibilityList(t, "|google.com|$dnstype=TXT")
	client := queryClientIP(netip.MustParseAddr("127.0.0.1"))
	for _, tc := range []struct {
		value  string
		status int
		result string
	}{
		{"", http.StatusOK, "allow"},
		{"A", http.StatusOK, "allow"},
		{"txt", http.StatusOK, "block"},
		{"NOT_A_TYPE", http.StatusBadRequest, ""},
		{"TYPE65536", http.StatusBadRequest, ""},
	} {
		w := httptest.NewRecorder()
		handleAPIPolicyEvaluate(w, httptest.NewRequest(http.MethodGet, "/api/policy/evaluate?client_ip="+client+"&domain=google.com&type="+tc.value, nil))
		if w.Code != tc.status {
			t.Errorf("type %q: status=%d want %d body=%s", tc.value, w.Code, tc.status, w.Body.String())
		}
		if tc.status == http.StatusOK {
			var got struct {
				Data struct {
					Result string `json:"result"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Data.Result != tc.result {
				t.Errorf("type %q result=%q want %q", tc.value, got.Data.Result, tc.result)
			}
		}
	}
}

func TestListCompatibilityInvalidRegexPreservesGeneration(t *testing.T) {
	defer setupTestDB(t)()
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, "ads.example")
	res, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES (?,'Regex refresh',1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	before := policyState.Load()
	server.serve("tracker.example", "/[invalid/")
	if err := refreshBlocklistByID(int(id)); err == nil {
		t.Error("invalid regex replaced the active generation")
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, []string{"ads.example"}) {
		t.Errorf("stored=%v want [ads.example]", got)
	}
	if policyState.Load() != before {
		t.Error("failed compilation changed the active policy")
	}
}
