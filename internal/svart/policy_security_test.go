package svart

// Regression tests for policy matching (review items D6, D14, D18).

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

func secpolicyAssignList(t testing.TB, clientIP string, domains ...string) {
	t.Helper()
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, 1)",
		"https://lists.example/"+clientIP+".txt", "list-"+clientIP)
	if err != nil {
		t.Fatal(err)
	}
	id, fixtureErr447 := res.LastInsertId()
	if fixtureErr447 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr447)
	}
	for _, d := range domains {
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", clientIP, id); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?,'x.','A','NOERROR',0,1)", clientIP); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
}

// TestDeepNamePolicyEvaluationIsNotQuadratic ports PoC 10: the suffix walk
// built every parent with strings.Join per list, so a legal 127-label name
// cost ~1,500 allocations and 200 KB per evaluation (and random names skip
// the policy cache). Evaluation must not allocate per label.
func TestDeepNamePolicyEvaluationIsNotQuadratic(t *testing.T) {
	defer setupTestDB(t)()
	for i := 0; i < 6; i++ {
		res, fixtureErr1494 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, 1)", fmt.Sprintf("https://l%d.example/", i), fmt.Sprintf("L%d", i))
		if fixtureErr1494 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr1494)
		}
		id, fixtureErr1642 := res.LastInsertId()
		if fixtureErr1642 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr1642)
		}
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, fmt.Sprintf("ads%d.example", i)); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, fmt.Sprintf("*.track%d.example", i)); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.0.0.5', ?)", id); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES ('10.0.0.5','x.','A','NOERROR',0,1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	mustReloadPolicy(t)

	labels := make([]string, 126)
	for i := range labels {
		labels[i] = "a"
	}
	deep := strings.Join(labels, ".") + ".com" // 253 chars, 127 labels (legal)
	normal := testing.AllocsPerRun(50, func() { evaluatePolicyFull("10.0.0.5", "www.example.com", 1) })
	deepAllocs := testing.AllocsPerRun(50, func() { evaluatePolicyFull("10.0.0.5", deep, 1) })
	t.Logf("allocs: www.example.com=%.0f, 127-label name=%.0f", normal, deepAllocs)
	if deepAllocs > normal+2 {
		t.Errorf("127-label name costs %.0f allocations vs %.0f for www.example.com; label count must not multiply work", deepAllocs, normal)
	}
	if r := evaluatePolicyFull("10.0.0.5", "x.y.pixel.track3.example", 1); r.Result != "block" {
		t.Errorf("wildcard *.track3.example must still match deep subdomains, got %q", r.Result)
	}
	if r := evaluatePolicyFull("10.0.0.5", "cdn.ads4.example", 1); r.Result != "block" {
		t.Errorf("ads4.example must still block its subdomains, got %q", r.Result)
	}
}

// TestPolicyCacheKeyCannotCollideForIPv6 ports PoC 11: "2001:db8::1" +
// "2:ads.example" and "2001:db8::1:2" + "ads.example" shared the string key
// "2001:db8::1:2:ads.example", letting the attacker plant an allow for the
// victim.
func TestPolicyCacheKeyCannotCollideForIPv6(t *testing.T) {
	defer setupTestDB(t)()
	victim, attacker := "2001:db8::1:2", "2001:db8::1"
	secpolicyAssignList(t, victim, "ads.example")
	mustReloadPolicy(t)

	if a := evaluatePolicy(attacker, "2:ads.example.", 1); a.Result == "block" {
		t.Fatalf("attacker's own query should not be blocked")
	}
	if v := evaluatePolicy(victim, "ads.example.", 1); v.Result != "block" || v.ClientIP != victim {
		t.Errorf("victim %s got %q for ads.example (cached for client %s); the cache key collided", victim, v.Result, v.ClientIP)
	}
}

// TestListEntriesAreNormalized (D18): entries with a trailing dot, upper
// case or Unicode (IDN) never matched, and ip6.arpa reverse lookups were not
// mapped to IP-literal entries.
func TestListEntriesAreNormalized(t *testing.T) {
	defer setupTestDB(t)()
	secpolicyAssignList(t, "10.42.1.42",
		"tracker.example.",         // trailing dot
		"Telemetry.Vendor.Example", // upper case
		"bücher.example",           // Unicode, must match the punycode query
		"*.münchen-ads.example",    // Unicode wildcard suffix
		"2001:DB8:0:0:0:0:0:BAD",   // non-canonical IPv6 literal
		"4321:0:1:2:3:4:567:89ab",  // IPv6 literal for ip6.arpa reverse
		"198.51.100.23",            // IPv4 literal for in-addr.arpa reverse
	)
	mustReloadPolicy(t)

	for _, q := range []string{
		"tracker.example.",
		"cdn.tracker.example.",
		"telemetry.vendor.example.",
		"xn--bcher-kva.example.",
		"img.xn--mnchen-ads-9db.example.",
		"b.a.9.8.7.6.5.0.4.0.0.0.3.0.0.0.2.0.0.0.1.0.0.0.0.0.0.0.1.2.3.4.ip6.arpa.",
		"23.100.51.198.in-addr.arpa.",
	} {
		if r := evaluatePolicy("10.42.1.42", q, 1); r.Result != "block" {
			t.Errorf("%s: want block, got %q", q, r.Result)
		}
	}
	if !responseIPsBlocked("10.42.1.42", []string{"2001:db8::bad"}) {
		t.Errorf("non-canonical IPv6 list entry must match the canonical response address 2001:db8::bad")
	}
}

// TestParseDomainNormalizesEntries (D18) at list-fetch time.
func TestParseDomainNormalizesEntries(t *testing.T) {
	for in, want := range map[string]string{
		"tracker.example.":         "tracker.example",
		"||Tracker.Example.^":      "tracker.example",
		"0.0.0.0 bücher.example":   "xn--bcher-kva.example",
		"||*.münchen-ads.example^": "*.xn--mnchen-ads-9db.example",
		"2001:DB8::0:BAD":          "2001:db8::bad",
		"127.0.0.1 ads.example.":   "ads.example",
	} {
		if got := listparse.ParseDomain(in); got != want {
			t.Errorf("listparse.ParseDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
