package main

// Regression tests for metric label exposure (review items D9, A5).
// /metrics is unauthenticated, so labels must not carry client IPs, entity
// names or upstream URL secrets unless the operator opts in.

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// secmetricsLabels returns every series of c as "name=value,..." strings.
func secmetricsLabels(c prometheus.Collector) []string {
	ch := make(chan prometheus.Metric, 64)
	go func() { c.Collect(ch); close(ch) }()
	var out []string
	for m := range ch {
		var d dto.Metric
		if err := m.Write(&d); err != nil {
			continue
		}
		var parts []string
		for _, lp := range d.GetLabel() {
			parts = append(parts, lp.GetName()+"="+lp.GetValue())
		}
		out = append(out, strings.Join(parts, ","))
	}
	sort.Strings(out)
	return out
}

func secmetricsCounter(c *prometheus.CounterVec, labels ...string) float64 {
	var d dto.Metric
	if err := c.WithLabelValues(labels...).Write(&d); err != nil {
		return 0
	}
	return d.GetCounter().GetValue()
}

// secmetricsRangeClientTraffic sends a blocked and a rewritten query from a
// client inside a named range and waits for the log writer to record them.
func secmetricsRangeClientTraffic(t *testing.T) {
	t.Helper()
	res, fixtureErr1345 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('https://lists.example/ads.txt', 'Ads List', 1)")
	if fixtureErr1345 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1345)
	}
	blID, fixtureErr1468 := res.LastInsertId()
	if fixtureErr1468 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1468)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'ads.example')", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	var fixtureErr1672 error
	res, fixtureErr1672 = db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Sam''s devices', '10.42.5.0/24')")
	if fixtureErr1672 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1672)
	}
	rID, fixtureErr1770 := res.LastInsertId()
	if fixtureErr1770 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1770)
	}
	if _, err := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", rID, blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	mustReloadPolicy(t)
	rewritesCache.Load().Store("nas.home.arpa", Rewrite{Domain: "nas.home.arpa", IPAddresses: []string{"192.168.1.20"}})

	oldLogging := loggingEnabled.Load()
	loggingEnabled.Store(true)
	t.Cleanup(func() { loggingEnabled.Store(oldLogging) })

	before := secmetricsCounter(dnsQueriesTotal, "block") + secmetricsCounter(dnsQueriesTotal, "rewrite")
	for _, name := range []string{"ads.example.", "nas.home.arpa."} {
		req := new(dns.Msg)
		req.SetQuestion(name, dns.TypeA)
		handleDNSRequest(newMockWriter("10.42.5.9"), req)
	}
	deadline := time.Now().Add(5 * time.Second)
	for secmetricsCounter(dnsQueriesTotal, "block")+secmetricsCounter(dnsQueriesTotal, "rewrite") < before+2 {
		if time.Now().After(deadline) {
			t.Fatal("log writer never recorded the two queries")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func secmetricsResetPerClient() {
	dnsClientQueries.Reset()
	dnsClientListHits.Reset()
	dnsClientCustomHits.Reset()
	dnsEntityDecisions.Reset()
}

// TestPerClientMetricLabelsOffByDefault ports PoC 9 and A5: client_ip
// labels grew one series per source address (100k IPs -> 50 MiB, never
// deleted) and published who is on the network, and entity labels
// published names like "Sam's devices", all on an unauthenticated
// endpoint.
func TestPerClientMetricLabelsOffByDefault(t *testing.T) {
	defer setupTestDB(t)()
	secmetricsResetPerClient()
	defer secmetricsResetPerClient()
	secmetricsRangeClientTraffic(t)

	for name, c := range map[string]prometheus.Collector{
		"svart_dns_client_queries_total":     dnsClientQueries,
		"svart_dns_client_list_hits_total":   dnsClientListHits,
		"svart_dns_client_custom_hits_total": dnsClientCustomHits,
		"svart_dns_entity_decisions_total":   dnsEntityDecisions,
	} {
		if series := secmetricsLabels(c); len(series) != 0 {
			t.Errorf("%s exposes %v by default; per-client/entity labels must be opt-in (METRICS_PER_CLIENT)", name, series)
		}
	}
}

// TestUpstreamMetricLabelsRedactURLSecrets (A5): DoH URLs often carry an
// account or profile ID in the path (NextDNS, ControlD), and URLs can carry
// userinfo or query tokens. Labels keep only scheme://host[:port].
func TestUpstreamMetricLabelsRedactURLSecrets(t *testing.T) {
	dnsUpstreamQueries.Reset()
	dnsUpstreamErrors.Reset()
	dnsUpstreamDuration.Reset()
	defer func() {
		dnsUpstreamQueries.Reset()
		dnsUpstreamErrors.Reset()
		dnsUpstreamDuration.Reset()
	}()

	recordSuccess("https://user:hunter2@dns.nextdns.io/abc123def?token=s3cr3t", 12)
	recordSuccess("https://dns.nextdns.io/abc123def", 15)
	recordSuccess("9.9.9.9:53", 8)
	recordFailure("tls://admin:pw@dns.quad9.net:853/x")
	recordFailure("[/corp.example/]10.0.0.53")

	queries := secmetricsLabels(dnsUpstreamQueries)
	errors := secmetricsLabels(dnsUpstreamErrors)
	durations := secmetricsLabels(dnsUpstreamDuration)
	all := strings.Join(append(append(append([]string{}, queries...), errors...), durations...), " ")
	for _, secret := range []string{"hunter2", "user", "abc123def", "s3cr3t", "token", "admin", "pw@", "/x", "corp.example"} {
		if strings.Contains(all, secret) {
			t.Errorf("upstream labels leak %q: %s", secret, all)
		}
	}
	wantQueries := []string{"upstream=9.9.9.9:53", "upstream=https://dns.nextdns.io"}
	if strings.Join(queries, " ") != strings.Join(wantQueries, " ") {
		t.Errorf("upstream query series = %v, want %v", queries, wantQueries)
	}
	wantErrors := []string{"upstream=10.0.0.53", "upstream=tls://dns.quad9.net:853"}
	if strings.Join(errors, " ") != strings.Join(wantErrors, " ") {
		t.Errorf("upstream error series = %v, want %v", errors, wantErrors)
	}
}

// TestPerClientMetricLabelsOptIn: with METRICS_PER_CLIENT=true the
// per-client and entity series come back (existing Grafana dashboards use
// them).
func TestPerClientMetricLabelsOptIn(t *testing.T) {
	defer setupTestDB(t)()
	secmetricsResetPerClient()
	defer secmetricsResetPerClient()
	labeler, err := loadPerClientMetricsConfig(envMap(map[string]string{"METRICS_PER_CLIENT": "true"}))
	if err != nil || labeler == nil || labeler.limit != defaultMetricsClientLimit {
		t.Fatalf("opt-in config = %+v, %v", labeler, err)
	}
	clientLabels.Store(labeler)
	defer clientLabels.Store(nil)
	secmetricsRangeClientTraffic(t)

	want := map[string][]string{
		"client_queries": {"action=block,client_ip=10.42.5.9", "action=rewrite,client_ip=10.42.5.9"},
		"client_lists":   {"client_ip=10.42.5.9,list_name=Ads List,result=block"},
		"entities":       {"entity=Sam's devices,result=block,tier=range"},
	}
	got := map[string][]string{
		"client_queries": secmetricsLabels(dnsClientQueries),
		"client_lists":   secmetricsLabels(dnsClientListHits),
		"entities":       secmetricsLabels(dnsEntityDecisions),
	}
	for k := range want {
		if strings.Join(got[k], " | ") != strings.Join(want[k], " | ") {
			t.Errorf("%s series = %v, want %v", k, got[k], want[k])
		}
	}
}

// TestClientLabelerCapsCardinality: past the limit unknown clients share
// "other"; aliased clients (an admin-curated set) always keep their label.
func TestClientLabelerCapsCardinality(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES ('10.42.1.43', 'Sam iPhone')"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := loadClientAliasCache(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	l := newClientLabeler(2)
	for _, tc := range []struct{ ip, want string }{
		{"10.42.1.42", "10.42.1.42"},
		{"10.42.1.50", "10.42.1.50"},
		{"10.42.1.51", "other"},
		{"2001:db8::77", "other"},
		{"10.42.1.43", "10.42.1.43"}, // aliased: admitted past the limit
		{"10.42.1.42", "10.42.1.42"}, // admitted clients keep their label
	} {
		if got := l.label(tc.ip); got != tc.want {
			t.Errorf("label(%s) = %q, want %q", tc.ip, got, tc.want)
		}
	}
}

func TestLoadPerClientMetricsConfig(t *testing.T) {
	for _, env := range []map[string]string{nil, {"METRICS_PER_CLIENT": "false"}, {"METRICS_PER_CLIENT": "0", "METRICS_CLIENT_LIMIT": "10"}} {
		if l, err := loadPerClientMetricsConfig(envMap(env)); l != nil || err != nil {
			t.Errorf("%v: want off, got %+v %v", env, l, err)
		}
	}
	l, err := loadPerClientMetricsConfig(envMap(map[string]string{"METRICS_PER_CLIENT": "true", "METRICS_CLIENT_LIMIT": "40"}))
	if err != nil || l == nil || l.limit != 40 {
		t.Errorf("limit 40: got %+v %v", l, err)
	}
	for env, wantErr := range map[string]map[string]string{
		`METRICS_PER_CLIENT: want true or false, got "yes please"`: {"METRICS_PER_CLIENT": "yes please"},
		`METRICS_CLIENT_LIMIT: want a positive integer, got "0"`:   {"METRICS_PER_CLIENT": "true", "METRICS_CLIENT_LIMIT": "0"},
	} {
		if _, err := loadPerClientMetricsConfig(envMap(wantErr)); err == nil || err.Error() != env {
			t.Errorf("%v: error = %v, want %q", wantErr, err, env)
		}
	}
}

func TestQtypeMetricLabelBucketsUnknownTypes(t *testing.T) {
	for in, want := range map[string]string{"A": "A", "HTTPS": "HTTPS", "TYPE65400": "other", "": "other"} {
		if got := qtypeMetricLabel(in); got != want {
			t.Errorf("qtypeMetricLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
