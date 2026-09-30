package main

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/yeti/svart-dns/internal/policycore"
)

// mockDNSWriter captures the DNS response written by handleDNSRequest.
type mockDNSWriter struct {
	written *dns.Msg
	addr    net.Addr
}

func (m *mockDNSWriter) LocalAddr() net.Addr         { return m.addr }
func (m *mockDNSWriter) RemoteAddr() net.Addr        { return m.addr }
func (m *mockDNSWriter) WriteMsg(msg *dns.Msg) error { m.written = msg; return nil }

// Write decodes raw replies (cache hits are served as patched wire bytes)
// so tests can inspect them like WriteMsg output.
func (m *mockDNSWriter) Write(b []byte) (int, error) {
	msg := new(dns.Msg)
	if err := msg.Unpack(b); err != nil {
		return 0, err
	}
	m.written = msg
	return len(b), nil
}
func (m *mockDNSWriter) Close() error        { return nil }
func (m *mockDNSWriter) TsigStatus() error   { return nil }
func (m *mockDNSWriter) TsigTimersOnly(bool) {}
func (m *mockDNSWriter) Hijack()             {}

func newMockWriter(ip string) *mockDNSWriter {
	return &mockDNSWriter{
		addr: &net.UDPAddr{IP: net.ParseIP(ip), Port: 53},
	}
}

func TestFlattenPolicyResult_NilPolicy(t *testing.T) {
	e := &queryLogEntry{}
	flattenPolicyResult(e, nil, "9.9.9.9:53")
	if e.result != "allow" {
		t.Errorf("expected result 'allow', got %q", e.result)
	}
	if e.resultReason != "default_allow" {
		t.Errorf("expected reason 'default_allow', got %q", e.resultReason)
	}
}

func TestFlattenPolicyResult_Rewrite(t *testing.T) {
	e := &queryLogEntry{}
	flattenPolicyResult(e, nil, "rewrite")
	if e.result != "rewrite" {
		t.Errorf("expected result 'rewrite', got %q", e.result)
	}
	if e.resultReason != "rewrite" {
		t.Errorf("expected reason 'rewrite', got %q", e.resultReason)
	}
}

func TestFlattenPolicyResult_PublishedBlock(t *testing.T) {
	p := &policycore.PolicyResult{
		Result: "block",
		ResultSource: &policycore.EntityResult{
			Tier:   "group",
			Name:   "Kids",
			Result: "block",
			PublishedList: &policycore.PublishedHit{
				Action:   "block",
				Rule:     "ads.google.com",
				ListID:   1,
				ListName: "Hagezi Pro",
			},
		},
		GroupEvaluation: &policycore.TierEvaluation{
			Result: "block",
			ResultSource: &policycore.EntityResult{
				Tier:   "group",
				Name:   "Kids",
				Result: "block",
				PublishedList: &policycore.PublishedHit{
					Action:   "block",
					Rule:     "ads.google.com",
					ListID:   1,
					ListName: "Hagezi Pro",
				},
			},
		},
	}
	e := &queryLogEntry{}
	flattenPolicyResult(e, p, "")

	if e.result != "block" {
		t.Errorf("expected result 'block', got %q", e.result)
	}
	if e.resultReason != "published_block" {
		t.Errorf("expected reason 'published_block', got %q", e.resultReason)
	}
	if e.resultTier != "group" {
		t.Errorf("expected tier 'group', got %q", e.resultTier)
	}
	if e.resultEntity != "Kids" {
		t.Errorf("expected entity 'Kids', got %q", e.resultEntity)
	}
	if !e.resultIsPublished {
		t.Error("expected resultIsPublished=true")
	}
	if e.resultRule != "ads.google.com" {
		t.Errorf("expected rule 'ads.google.com', got %q", e.resultRule)
	}
	if e.resultListName != "Hagezi Pro" {
		t.Errorf("expected list name 'Hagezi Pro', got %q", e.resultListName)
	}
	if e.groupResult != "block" {
		t.Errorf("expected groupResult 'block', got %q", e.groupResult)
	}
	if e.groupEntity != "Kids" {
		t.Errorf("expected groupEntity 'Kids', got %q", e.groupEntity)
	}
	if buildQueryLogPolicyJSON(*e) == "" {
		t.Error("expected persisted policy JSON for non-default policy hit")
	}
}

func TestFlattenPolicyResult_CustomBlock(t *testing.T) {
	p := &policycore.PolicyResult{
		Result: "block",
		ResultSource: &policycore.EntityResult{
			Tier:   "ip",
			Name:   "10.42.1.42",
			Result: "block",
			CustomRule: &policycore.CustomHit{
				Action: "block",
				Rule:   "sketchy-tracker.io",
			},
		},
		IPEvaluation: &policycore.TierEvaluation{
			Result: "block",
			ResultSource: &policycore.EntityResult{
				Tier:   "ip",
				Name:   "10.42.1.42",
				Result: "block",
				CustomRule: &policycore.CustomHit{
					Action: "block",
					Rule:   "sketchy-tracker.io",
				},
			},
		},
	}
	e := &queryLogEntry{}
	flattenPolicyResult(e, p, "")

	if e.resultReason != "custom_block" {
		t.Errorf("expected reason 'custom_block', got %q", e.resultReason)
	}
	if e.resultIsPublished {
		t.Error("expected resultIsPublished=false for custom rule")
	}
	if e.resultRule != "sketchy-tracker.io" {
		t.Errorf("expected rule 'sketchy-tracker.io', got %q", e.resultRule)
	}
	if e.ipResult != "block" {
		t.Errorf("expected ipResult 'block', got %q", e.ipResult)
	}
}

func TestFlattenPolicyResult_CustomAllow(t *testing.T) {
	p := &policycore.PolicyResult{
		Result: "allow",
		ResultSource: &policycore.EntityResult{
			Tier:   "ip",
			Name:   "10.42.1.42",
			Result: "allow",
			CustomRule: &policycore.CustomHit{
				Action: "allow",
				Rule:   "youtube.com",
			},
		},
	}
	e := &queryLogEntry{}
	flattenPolicyResult(e, p, "")

	if e.resultReason != "custom_allow" {
		t.Errorf("expected reason 'custom_allow', got %q", e.resultReason)
	}
}

func TestFlattenPolicyResult_DefaultAllow(t *testing.T) {
	p := &policycore.PolicyResult{
		Result: "allow",
		ResultSource: &policycore.EntityResult{
			Tier:   "default",
			Name:   "no matching rule",
			Result: "allow",
		},
	}
	e := &queryLogEntry{}
	flattenPolicyResult(e, p, "")

	if e.resultReason != "default_allow" {
		t.Errorf("expected reason 'default_allow', got %q", e.resultReason)
	}
	if e.resultTier != "default" {
		t.Errorf("expected tier 'default', got %q", e.resultTier)
	}
	if buildQueryLogPolicyJSON(*e) != "" {
		t.Error("default allow should not persist policy JSON")
	}
}

func TestFlattenPolicyResult_RangeTier(t *testing.T) {
	rangeEntity := &policycore.EntityResult{
		Tier:   "range",
		Name:   "IOT VLAN",
		Result: "block",
		PublishedList: &policycore.PublishedHit{
			Action:   "block",
			Rule:     "telemetry.nest.com",
			ListID:   2,
			ListName: "OISD Big",
		},
	}
	p := &policycore.PolicyResult{
		Result:       "block",
		ResultSource: rangeEntity,
		RangeEvaluation: &policycore.TierEvaluation{
			Result:       "block",
			ResultSource: rangeEntity,
		},
	}
	e := &queryLogEntry{}
	flattenPolicyResult(e, p, "")

	if e.rangeResult != "block" {
		t.Errorf("expected rangeResult 'block', got %q", e.rangeResult)
	}
	if e.rangeEntity != "IOT VLAN" {
		t.Errorf("expected rangeEntity 'IOT VLAN', got %q", e.rangeEntity)
	}
	if e.resultTier != "range" {
		t.Errorf("expected resultTier 'range', got %q", e.resultTier)
	}
}

func TestClientAliasCache(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES ('10.42.1.42', 'Chris Macbook')"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES ('10.42.1.43', 'Sam iPhone')"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if err := loadClientAliasCache(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if got := getClientAliasCached("10.42.1.42"); got != "Chris Macbook" {
		t.Errorf("expected 'Chris Macbook', got %q", got)
	}
	if got := getClientAliasCached("10.42.1.43"); got != "Sam iPhone" {
		t.Errorf("expected 'Sam iPhone', got %q", got)
	}
	if got := getClientAliasCached("192.168.1.1"); got != "" {
		t.Errorf("expected empty for unknown IP, got %q", got)
	}
}

// TestDoomLoopThrottling verifies that a doom-looping client
// still gets its blocked NXDOMAIN (with the SOA negative TTL) immediately.
// This test used to assert a ~1 s sleep in the handler; that "throttle"
// parked one goroutine per query and was bypassed by changing letter case
// (review item D8), so the explicit behavior is now an immediate answer.
func TestDoomLoopThrottling(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Set up a blocklist with a known domain and load it into the policy engine.
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Throttle Test', 1)")
	if err != nil {
		t.Fatalf("failed to insert blocklist: %v", err)
	}
	blID, fixtureErr8172 := result.LastInsertId()
	if fixtureErr8172 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr8172)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "tracker.doom-loop.example.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.99", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Seed a query_log entry so the client IP is included in per-client data.
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?)",
		"10.42.1.99", "example.com.", "A", "NOERROR", false, 1); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	// Clear rewrite cache so the query goes through the policy path.
	rewritesCache.Store(&sync.Map{})

	// First, verify the domain is blocked without throttling (baseline — fast).
	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("tracker.doom-loop.example.com"), dns.TypeA)

	w := newMockWriter("10.42.1.99")
	start := time.Now()
	handleDNSRequest(w, req)
	elapsed := time.Since(start)

	if w.written == nil || w.written.Rcode != dns.RcodeNameError {
		t.Fatal("expected NXDOMAIN for blocked domain")
	}
	if elapsed >= 900*time.Millisecond {
		t.Errorf("baseline (no doom loop) took %v — expected < 900ms", elapsed)
	}

	// The doom-loop defense no longer delays answers (D8): a looping client
	// gets the same immediate NXDOMAIN, with the SOA negative-TTL hint that
	// tells well-behaved stubs to stop asking.
	if len(w.written.Ns) != 1 {
		t.Errorf("expected the SOA negative-TTL hint in the blocked answer, got %d authority records", len(w.written.Ns))
	}
}

func TestBlockedNXDOMAINHasSOA(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Set up a blocklist with a known domain and load it into the policy engine.
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Test Ads', 1)")
	if err != nil {
		t.Fatalf("failed to insert blocklist: %v", err)
	}
	blID, fixtureErr10265 := result.LastInsertId()
	if fixtureErr10265 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr10265)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "ads.malicious.example.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Assign the blocklist to the test client IP directly.
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	// Clear rewrite cache so the query goes through the policy path.
	rewritesCache.Store(&sync.Map{})

	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn("ads.malicious.example.com"), dns.TypeA)

	w := newMockWriter("10.42.1.42")
	handleDNSRequest(w, req)

	if w.written == nil {
		t.Fatal("expected a response, got nil")
	}
	if w.written.Rcode != dns.RcodeNameError {
		t.Errorf("expected NXDOMAIN rcode, got %s", dns.RcodeToString[w.written.Rcode])
	}

	// Authority section must contain exactly one SOA record.
	if len(w.written.Ns) != 1 {
		t.Fatalf("expected 1 authority record, got %d", len(w.written.Ns))
	}
	soa, ok := w.written.Ns[0].(*dns.SOA)
	if !ok {
		t.Fatalf("expected SOA record in authority section, got %T", w.written.Ns[0])
	}
	if soa.Minttl != 3600 {
		t.Errorf("expected SOA Minttl=3600, got %d", soa.Minttl)
	}
	if soa.Hdr.Ttl != 3600 {
		t.Errorf("expected SOA Hdr.Ttl=3600, got %d", soa.Hdr.Ttl)
	}
	if soa.Ns != "svart-dns.local." {
		t.Errorf("expected SOA Ns=svart-dns.local., got %q", soa.Ns)
	}
}
