package svart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/policycore"
)

// resultTier extracts the winning tier from a PolicyResult, or "" if no decision.
func resultTier(r *policycore.PolicyResult) string {
	if r.ResultSource != nil {
		return r.ResultSource.Tier
	}
	return ""
}

// setupPolicyTestEnv creates a complete three-tier policy environment:
// - Range: 10.42.0.0/16 (broad), 10.42.1.0/24 (specific)
// - Groups: "Sam" (with 10.42.1.43, 10.42.1.44), "Servers" (with 10.42.1.100)
// - Direct client: 10.42.1.42 (Chris)
func setupPolicyTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupTestDB(t)

	// --- Blocklists ---
	// BL1: Range-level blocklist (ads)
	result, fixtureErr33 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Range Ads', 1)")
	if fixtureErr33 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr33)
	}
	rangeBLID, fixtureErr34 := result.LastInsertId()
	if fixtureErr34 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr34)
	}
	for _, d := range []string{"ads.google.com", "pagead2.googlesyndication.com", "ad.doubleclick.net"} {
		if _, fixtureErr36 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", rangeBLID, d); fixtureErr36 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr36)
		}
	}

	// BL2: Group-level blocklist (tracking)
	var fixtureErr40 error
	result, fixtureErr40 = db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Group Tracking', 1)")
	if fixtureErr40 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr40)
	}
	groupBLID, fixtureErr41 := result.LastInsertId()
	if fixtureErr41 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr41)
	}
	for _, d := range []string{"segment.io", "mixpanel.com", "amplitude.com", "hotjar.com"} {
		if _, fixtureErr43 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", groupBLID, d); fixtureErr43 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr43)
		}
	}

	// BL3: Client-direct blocklist (custom)
	var fixtureErr47 error
	result, fixtureErr47 = db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Chris Custom Block', 1)")
	if fixtureErr47 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr47)
	}
	clientBLID, fixtureErr48 := result.LastInsertId()
	if fixtureErr48 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr48)
	}
	for _, d := range []string{"reddit.com", "news.ycombinator.com"} {
		if _, fixtureErr50 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", clientBLID, d); fixtureErr50 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr50)
		}
	}

	// --- Allowlists ---
	// AL1: Range-level allowlist
	var fixtureErr55 error
	result, fixtureErr55 = db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'Range Allow', 1)")
	if fixtureErr55 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr55)
	}
	rangeALID, fixtureErr56 := result.LastInsertId()
	if fixtureErr56 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr56)
	}
	for _, d := range []string{"accounts.google.com"} {
		if _, fixtureErr58 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", rangeALID, d); fixtureErr58 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr58)
		}
	}

	// AL2: Group-level allowlist
	var fixtureErr62 error
	result, fixtureErr62 = db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'Group Allow', 1)")
	if fixtureErr62 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr62)
	}
	groupALID, fixtureErr63 := result.LastInsertId()
	if fixtureErr63 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr63)
	}
	for _, d := range []string{"segment.io"} { // overrides group block
		if _, fixtureErr65 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", groupALID, d); fixtureErr65 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr65)
		}
	}

	// AL3: Client-direct allowlist
	var fixtureErr69 error
	result, fixtureErr69 = db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'Chris Allow', 1)")
	if fixtureErr69 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr69)
	}
	clientALID, fixtureErr70 := result.LastInsertId()
	if fixtureErr70 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr70)
	}
	for _, d := range []string{"mixpanel.com", "reddit.com"} { // overrides client-direct block for reddit, overrides group block for mixpanel
		if _, fixtureErr72 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", clientALID, d); fixtureErr72 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr72)
		}
	}

	// --- Ranges ---
	if _, fixtureErr76 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Broad Network', '10.42.0.0/16')"); fixtureErr76 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr76)
	}
	if _, fixtureErr77 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Home Subnet', '10.42.1.0/24')"); fixtureErr77 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr77)
	}

	// Assign blocklist/allowlist to ranges
	if _, fixtureErr80 := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?)", rangeBLID); fixtureErr80 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr80)
	} // broad
	if _, fixtureErr81 := db.Exec("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (2, ?)", rangeALID); fixtureErr81 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr81)
	} // specific /24

	// --- Groups ---
	if _, fixtureErr84 := db.Exec("INSERT INTO client_groups (name) VALUES ('Sam')"); fixtureErr84 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr84)
	} // group_id = 1
	if _, fixtureErr85 := db.Exec("INSERT INTO client_groups (name) VALUES ('Servers')"); fixtureErr85 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr85)
	} // group_id = 2

	if _, fixtureErr87 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.43', 1)"); fixtureErr87 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr87)
	}
	if _, fixtureErr88 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.44', 1)"); fixtureErr88 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr88)
	}
	if _, fixtureErr89 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.100', 2)"); fixtureErr89 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr89)
	}
	if _, fixtureErr90 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.42', 1)"); fixtureErr90 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr90)
	} // Chris also in Sam group

	if _, fixtureErr92 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (1, ?)", groupBLID); fixtureErr92 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr92)
	}
	if _, fixtureErr93 := db.Exec("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (1, ?)", groupALID); fixtureErr93 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr93)
	}

	// --- Client-direct assignments ---
	// Seed query logs so clients show up
	for _, ip := range []string{"10.42.1.42", "10.42.1.43", "10.42.1.44", "10.42.1.100", "10.42.3.50"} {
		if _, fixtureErr98 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", ip); fixtureErr98 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr98)
		}
	}

	if _, fixtureErr101 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.42.1.42', ?)", clientBLID); fixtureErr101 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr101)
	}
	if _, fixtureErr102 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES ('10.42.1.42', ?)", clientALID); fixtureErr102 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr102)
	}

	// Load everything
	mustReloadPolicy(t)

	return cleanup
}

func TestEvaluatePolicy_RangeBlocks_GroupAllows(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.43", "segment.io", 1)
	if result.Result == "block" {
		t.Error("expected segment.io to be ALLOWED (group allow overrides range block)")
	}
	if resultTier(result) != "group" {
		t.Errorf("expected tier 'group', got %q", resultTier(result))
	}
	if result.Result != "allow" {
		t.Error("expected result='allow'")
	}
}

func TestEvaluatePolicy_GroupBlocks_IPAllows(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.42", "mixpanel.com", 1)
	if result.Result == "block" {
		t.Error("expected mixpanel.com to be ALLOWED (IP allow overrides group block)")
	}
	if resultTier(result) != "ip" {
		t.Errorf("expected tier 'ip', got %q", resultTier(result))
	}
	if result.Result != "allow" {
		t.Error("expected result='allow'")
	}
}

func TestEvaluatePolicy_GroupAllows_IPBlocks(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.42", "news.ycombinator.com", 1)
	if result.Result != "block" {
		t.Error("expected news.ycombinator.com to be BLOCKED (IP block)")
	}
	if resultTier(result) != "ip" {
		t.Errorf("expected tier 'ip', got %q", resultTier(result))
	}
}

func TestEvaluatePolicy_NoPolicyAnywhere(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.42", "example.org", 1)
	if result.Result != "allow" {
		t.Errorf("expected default allow, got %q", result.Result)
	}
	if result.ResultSource == nil || result.ResultSource.Tier != "default" {
		t.Error("expected default tier result source")
	}
}

func TestEvaluatePolicy_OverlappingCIDRs(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.42", "accounts.google.com", 1)
	if result.Result == "block" {
		t.Error("expected accounts.google.com ALLOWED by specific /24 range allowlist")
	}
}

func TestEvaluatePolicy_OverlappingCIDRs_BlockWins(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result1, fixtureErr182 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Broad Block', 1)")
	if fixtureErr182 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr182)
	}
	blID, fixtureErr183 := result1.LastInsertId()
	if fixtureErr183 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr183)
	}
	if _, fixtureErr184 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'facebook.com')", blID); fixtureErr184 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr184)
	}

	result2, fixtureErr186 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'Narrow Allow', 1)")
	if fixtureErr186 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr186)
	}
	alID, fixtureErr187 := result2.LastInsertId()
	if fixtureErr187 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr187)
	}
	if _, fixtureErr188 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, 'facebook.com')", alID); fixtureErr188 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr188)
	}

	if _, fixtureErr190 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Broad', '10.42.0.0/16')"); fixtureErr190 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr190)
	}
	if _, fixtureErr191 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Narrow', '10.42.1.0/24')"); fixtureErr191 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr191)
	}

	if _, fixtureErr193 := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?)", blID); fixtureErr193 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr193)
	}
	if _, fixtureErr194 := db.Exec("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (2, ?)", alID); fixtureErr194 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr194)
	}

	if _, fixtureErr196 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.99', 'test.com.', 'A', 'NOERROR')"); fixtureErr196 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr196)
	}

	mustReloadPolicy(t)

	result := evaluatePolicy("10.42.1.99", "facebook.com", 1)
	if result.Result != "block" {
		t.Error("expected facebook.com BLOCKED (block wins across range entities)")
	}
	if resultTier(result) != "range" {
		t.Errorf("expected tier 'range', got %q", resultTier(result))
	}
}

func TestEvaluatePolicy_AllowBlockSameTierSameClient(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.1.42", "reddit.com", 1)
	if result.Result == "block" {
		t.Error("expected reddit.com ALLOWED (allow wins over block within same tier)")
	}
	if result.Result != "allow" {
		t.Error("expected result='allow'")
	}
	if resultTier(result) != "ip" {
		t.Errorf("expected tier 'ip', got %q", resultTier(result))
	}
}

func TestEvaluatePolicy_MultipleGroupsConflict(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result1, fixtureErr229 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'GA Block', 1)")
	if fixtureErr229 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr229)
	}
	blID, fixtureErr230 := result1.LastInsertId()
	if fixtureErr230 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr230)
	}
	if _, fixtureErr231 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'conflict.example.com')", blID); fixtureErr231 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr231)
	}

	result2, fixtureErr233 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'GB Allow', 1)")
	if fixtureErr233 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr233)
	}
	alID, fixtureErr234 := result2.LastInsertId()
	if fixtureErr234 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr234)
	}
	if _, fixtureErr235 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, 'conflict.example.com')", alID); fixtureErr235 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr235)
	}

	if _, fixtureErr237 := db.Exec("INSERT INTO client_groups (name) VALUES ('GroupA')"); fixtureErr237 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr237)
	}
	if _, fixtureErr238 := db.Exec("INSERT INTO client_groups (name) VALUES ('GroupB')"); fixtureErr238 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr238)
	}

	if _, fixtureErr240 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.50', 1)"); fixtureErr240 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr240)
	}
	if _, fixtureErr241 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.50', 2)"); fixtureErr241 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr241)
	}

	if _, fixtureErr243 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (1, ?)", blID); fixtureErr243 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr243)
	}
	if _, fixtureErr244 := db.Exec("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (2, ?)", alID); fixtureErr244 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr244)
	}

	if _, fixtureErr246 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.50', 'test.com.', 'A', 'NOERROR')"); fixtureErr246 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr246)
	}

	mustReloadPolicy(t)

	result := evaluatePolicy("10.42.1.50", "conflict.example.com", 1)
	if result.Result != "block" {
		t.Error("expected conflict.example.com BLOCKED (block wins across groups within same tier)")
	}
	if resultTier(result) != "group" {
		t.Errorf("expected tier 'group', got %q", resultTier(result))
	}
}

func TestEvaluatePolicy_CacheHit(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	policyCache.Clear()

	r1 := evaluatePolicy("10.42.1.42", "example.org", 1)

	cachedResult, ok := policyCache.Load(testPolicyKey("10.42.1.42:example.org"))
	if !ok {
		t.Fatal("expected policy cache to be populated")
	}
	if cachedResult.Result != r1.Result {
		t.Error("cached result doesn't match")
	}

	r2 := evaluatePolicy("10.42.1.42", "example.org", 1)
	if r1.Result != r2.Result || resultTier(r1) != resultTier(r2) {
		t.Error("inconsistent results between cached and uncached calls")
	}
}

func TestEvaluatePolicy_TierEvaluationsPresent(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	policyCache.Clear()
	// 10.42.1.42 is in ranges, groups, and has IP-level data
	result := evaluatePolicyFull("10.42.1.42", "ads.google.com", 1)

	// Verify client_ip and domain are in the result
	if result.ClientIP != "10.42.1.42" {
		t.Errorf("expected client_ip '10.42.1.42', got %q", result.ClientIP)
	}
	if result.Domain != "ads.google.com" {
		t.Errorf("expected domain 'ads.google.com', got %q", result.Domain)
	}

	// Range should be present (client is in ranges)
	if result.RangeEvaluation == nil {
		t.Fatal("expected range_evaluation to be present")
	}
	if len(result.RangeEvaluation.Entities) == 0 {
		t.Error("expected non-empty entities in range evaluation")
	}

	// Group should be present (client is in Sam group)
	if result.GroupEvaluation == nil {
		t.Fatal("expected group_evaluation to be present")
	}
}

func TestEvaluatePolicy_RangeBlockOnly(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("10.42.3.50", "ads.google.com", 1)
	if result.Result != "block" {
		t.Error("expected ads.google.com BLOCKED by range tier for IoT device")
	}
	if resultTier(result) != "range" {
		t.Errorf("expected tier 'range', got %q", resultTier(result))
	}
}

func TestEvaluatePolicy_UnknownClientOutsideRange(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	result := evaluatePolicy("192.168.1.1", "ads.google.com", 1)
	if result.Result == "block" {
		t.Error("expected NOT blocked (no matching range, group, or IP)")
	}
}

func TestListAttribution_SmallestListWins(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create two blocklists: "Small List" (2 domains) and "Big List" (100 domains)
	// Both contain overlap.example.com
	result1, fixtureErr719 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('http://small', 'Small List', 1)")
	if fixtureErr719 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr719)
	}
	smallID, fixtureErr720 := result1.LastInsertId()
	if fixtureErr720 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr720)
	}
	if _, fixtureErr721 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'overlap.example.com')", smallID); fixtureErr721 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr721)
	}
	if _, fixtureErr722 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'unique-small.com')", smallID); fixtureErr722 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr722)
	}
	if _, fixtureErr723 := db.Exec("UPDATE blocklists SET domain_count = 2 WHERE id = ?", smallID); fixtureErr723 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr723)
	}

	result2, fixtureErr725 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('http://big', 'Big List', 1)")
	if fixtureErr725 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr725)
	}
	bigID, fixtureErr726 := result2.LastInsertId()
	if fixtureErr726 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr726)
	}
	if _, fixtureErr727 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'overlap.example.com')", bigID); fixtureErr727 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr727)
	}
	for i := 0; i < 99; i++ {
		if _, fixtureErr729 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", bigID, strings.Replace("big-domain-NUM.com", "NUM", strings.Repeat("x", i+1), 1)); fixtureErr729 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr729)
		}
	}
	if _, fixtureErr731 := db.Exec("UPDATE blocklists SET domain_count = 100 WHERE id = ?", bigID); fixtureErr731 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr731)
	}

	// Create a group and assign both lists
	if _, fixtureErr734 := db.Exec("INSERT INTO client_groups (name) VALUES ('TestGroup')"); fixtureErr734 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr734)
	}
	if _, fixtureErr735 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (1, ?)", smallID); fixtureErr735 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr735)
	}
	if _, fixtureErr736 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (1, ?)", bigID); fixtureErr736 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr736)
	}
	if _, fixtureErr737 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.42.1.60', 1)"); fixtureErr737 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr737)
	}
	if _, fixtureErr738 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.60', 'test.com.', 'A', 'NOERROR')"); fixtureErr738 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr738)
	}

	mustReloadPolicy(t)

	result := evaluatePolicyFull("10.42.1.60", "overlap.example.com", 1)
	if result.Result != "block" {
		t.Fatal("expected overlap.example.com to be blocked")
	}
	if result.ResultSource == nil || result.ResultSource.PublishedList == nil {
		t.Fatal("expected published_list in result_source")
	}
	if result.ResultSource.PublishedList.ListName != "Small List" {
		t.Errorf("expected attribution to 'Small List', got %q", result.ResultSource.PublishedList.ListName)
	}
	if result.ResultSource.PublishedList.ListID != int(smallID) {
		t.Errorf("expected list_id=%d, got %d", smallID, result.ResultSource.PublishedList.ListID)
	}
}

func TestListAttribution_ManualBlockNoAttribution(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	clientIP := "10.42.1.70"
	if _, fixtureErr762 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", clientIP); fixtureErr762 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr762)
	}

	// Add manual block (url='')
	body := strings.NewReader(`{"domain":"manual-block.example.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIClientBlockDomain(w, req, clientIP, []string{clientIP, "block-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — %s", w.Code, w.Body.String())
	}

	policyCache.Clear()
	result := evaluatePolicyFull(clientIP, "manual-block.example.com", 1)
	if result.Result != "block" {
		t.Fatal("expected block")
	}
	// Manual blocks have custom_rule, not published_list
	if result.ResultSource == nil {
		t.Fatal("expected result_source")
	}
	if result.ResultSource.PublishedList != nil {
		t.Error("expected published_list to be nil for manual block")
	}
	if result.ResultSource.CustomRule == nil {
		t.Error("expected custom_rule to be set for manual block")
	}
}

func TestDecisionAuditTrail(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	// Block a domain via policy, log it, verify audit trail columns
	policyResult := evaluatePolicyFull("10.42.3.50", "ads.google.com", 1)
	if policyResult.Result != "block" {
		t.Fatal("expected block")
	}

	// Manually insert a log entry using the same logic as logQuery
	entry := queryLogEntry{
		clientIP:            "10.42.3.50",
		queryName:           "ads.google.com",
		queryType:           "A",
		responseCode:        "NXDOMAIN",
		blocked:             true,
		latencyMicroseconds: 1,
	}
	flattenPolicyResult(&entry, policyResult, "")
	legacyBlock := deriveLegacyBlockFields(entry)

	// Insert directly to check columns
	_, err := db.Exec(`INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds, block_tier, block_rule, block_source, block_list_id, block_list_name) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.clientIP, entry.queryName, entry.queryType, entry.responseCode, entry.blocked, entry.latencyMicroseconds,
		legacyBlock.tier, legacyBlock.rule, legacyBlock.source, legacyBlock.listID, legacyBlock.listName)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	// Read back and verify
	var blockTier, blockRule, blockSource, blockListName string
	var blockListID int
	err = db.QueryRow(`SELECT COALESCE(block_tier,''), COALESCE(block_rule,''), COALESCE(block_source,''), COALESCE(block_list_id,0), COALESCE(block_list_name,'') FROM query_logs WHERE client_ip='10.42.3.50' AND query_name='ads.google.com' ORDER BY id DESC LIMIT 1`).
		Scan(&blockTier, &blockRule, &blockSource, &blockListID, &blockListName)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if blockTier != "range" {
		t.Errorf("expected block_tier='range', got %q", blockTier)
	}
	if blockRule != "ads.google.com" {
		t.Errorf("expected block_rule='ads.google.com', got %q", blockRule)
	}
	if blockSource == "" {
		t.Error("expected block_source to be non-empty")
	}
}

// TestEvaluatePolicyConcurrentReloadStress is the A3 regression test: it
// interleaves real reloads (reloadPolicyState, which publishes a new policy
// snapshot and then invalidates policyCache) with
// concurrent evaluatePolicy calls on the SAME (client, domain) key — the
// exact pattern that used to let a slow in-flight evaluatePolicyFull plant a
// stale decision into the cache right after a reload's Clear() ran. Run under
// -race; after settling to a known final state, evaluatePolicy must reflect
// that state, not a decision computed against data from mid-race.
func TestEvaluatePolicyConcurrentReloadStress(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	const clientIP = "10.42.1.42"
	const domain = "toggle-test.example.com"

	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Toggle List', 1)")
	if err != nil {
		t.Fatalf("failed to insert toggle blocklist: %v", err)
	}
	toggleListID, fixtureErr859 := result.LastInsertId()
	if fixtureErr859 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr859)
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", clientIP, toggleListID); err != nil {
		t.Fatalf("failed to assign toggle blocklist: %v", err)
	}

	addDomain := func() {
		if _, fixtureErr865 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", toggleListID, domain); fixtureErr865 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr865)
		}
	}
	removeDomain := func() {
		if _, fixtureErr868 := db.Exec("DELETE FROM blocked_domains WHERE blocklist_id = ? AND domain = ?", toggleListID, domain); fixtureErr868 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr868)
		}
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var reloadErr atomic.Value // string

	// Reloader: repeatedly toggles the domain in/out of the blocklist and
	// reloads — publishing a new snapshot and invalidating policyCache on
	// every iteration, same as production mutation handlers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		blocked := false
		for {
			select {
			case <-stop:
				return
			default:
			}
			if blocked {
				removeDomain()
			} else {
				addDomain()
			}
			blocked = !blocked
			if err := reloadPolicyState("test", reloadListContent); err != nil {
				reloadErr.Store(err.Error())
				return
			}
		}
	}()

	// Evaluators: hammer evaluatePolicy for the same key concurrently with
	// the reloads above.
	var evalCount atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				evaluatePolicy(clientIP, domain, 1)
				evalCount.Add(1)
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()

	if v := reloadErr.Load(); v != nil {
		t.Fatalf("reload failed during stress: %v", v)
	}
	if evalCount.Load() == 0 {
		t.Fatal("expected evaluations to run")
	}

	// Settle to a known final state (domain removed → allow) and verify the
	// cached/evaluated answer matches — proving no stale decision from
	// mid-race is stuck in the cache.
	removeDomain()
	mustReloadPolicy(t)
	if got := evaluatePolicy(clientIP, domain, 1); got.Result != "allow" {
		t.Errorf("expected 'allow' after settling with domain removed, got %q", got.Result)
	}

	// Flip once more (domain added → block) and verify again.
	addDomain()
	mustReloadPolicy(t)
	if got := evaluatePolicy(clientIP, domain, 1); got.Result != "block" {
		t.Errorf("expected 'block' after settling with domain added, got %q", got.Result)
	}
}
