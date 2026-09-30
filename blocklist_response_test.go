package main

import (
	"testing"

	"github.com/miekg/dns"
)

func TestIsResponseBlockedByIP_NoAnswers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if responseIPsBlocked("10.42.1.42", extractResponseIPs(new(dns.Msg))) {
		t.Error("expected no answers to never be blocked")
	}
}

func TestIsResponseBlockedByIP_NoMatchAnywhere(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if responseIPsBlocked("10.42.1.42", extractResponseIPs(dnsResponseWithA("8.8.8.8"))) {
		t.Error("expected unlisted IP to not be blocked")
	}
}

func TestIsResponseBlockedByIP_DirectClientTier(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr763 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'IP Literal Malware', 1)")
	if fixtureErr763 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr763)
	}
	blID, fixtureErr764 := result.LastInsertId()
	if fixtureErr764 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr764)
	}
	if _, fixtureErr765 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "193.200.64.30"); fixtureErr765 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr765)
	}
	if _, fixtureErr766 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", blID); fixtureErr766 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr766)
	}
	if _, fixtureErr767 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.42', 'test.com.', 'A', 'NOERROR')"); fixtureErr767 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr767)
	}

	mustReloadPolicy(t)

	if !responseIPsBlocked("10.42.1.42", extractResponseIPs(dnsResponseWithA("193.200.64.30"))) {
		t.Error("expected direct client-tier IP-literal block to block the response")
	}
	if responseIPsBlocked("10.42.1.99", extractResponseIPs(dnsResponseWithA("193.200.64.30"))) {
		t.Error("expected a client with no assignment to this list to NOT be blocked")
	}
}

// TestIsResponseBlockedByIP_RangeTier is the core A4 regression case: an
// IP-literal block that lives ONLY at the Range tier must still block the
// response — this previously fell through silently.
func TestIsResponseBlockedByIP_RangeTier(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr786 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Range IP Literal', 1)")
	if fixtureErr786 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr786)
	}
	blID, fixtureErr787 := result.LastInsertId()
	if fixtureErr787 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr787)
	}
	if _, fixtureErr788 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "185.246.188.124"); fixtureErr788 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr788)
	}

	result2, fixtureErr790 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('IOT VLAN', '10.42.3.0/24')")
	if fixtureErr790 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr790)
	}
	rangeID, fixtureErr791 := result2.LastInsertId()
	if fixtureErr791 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr791)
	}
	if _, fixtureErr792 := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", rangeID, blID); fixtureErr792 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr792)
	}

	mustReloadPolicy(t)

	if !responseIPsBlocked("10.42.3.50", extractResponseIPs(dnsResponseWithA("185.246.188.124"))) {
		t.Error("expected range-tier IP-literal block to block the response (regression: range tier was never consulted)")
	}
	if responseIPsBlocked("10.42.1.42", extractResponseIPs(dnsResponseWithA("185.246.188.124"))) {
		t.Error("expected a client outside the range to NOT be blocked")
	}
}

// TestIsResponseBlockedByIP_GroupTier is the Group-tier counterpart.
func TestIsResponseBlockedByIP_GroupTier(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr809 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Group IP Literal', 1)")
	if fixtureErr809 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr809)
	}
	blID, fixtureErr810 := result.LastInsertId()
	if fixtureErr810 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr810)
	}
	if _, fixtureErr811 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "212.117.190.210"); fixtureErr811 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr811)
	}

	result2, fixtureErr813 := db.Exec("INSERT INTO client_groups (name) VALUES ('Kids')")
	if fixtureErr813 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr813)
	}
	groupID, fixtureErr814 := result2.LastInsertId()
	if fixtureErr814 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr814)
	}
	if _, fixtureErr815 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", groupID, blID); fixtureErr815 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr815)
	}
	if _, fixtureErr816 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.42.1.43", groupID); fixtureErr816 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr816)
	}

	mustReloadPolicy(t)

	if !responseIPsBlocked("10.42.1.43", extractResponseIPs(dnsResponseWithA("212.117.190.210"))) {
		t.Error("expected group-tier IP-literal block to block the response (regression: group tier was never consulted)")
	}
	if responseIPsBlocked("10.42.1.44", extractResponseIPs(dnsResponseWithA("212.117.190.210"))) {
		t.Error("expected a client not in the group to NOT be blocked")
	}
}

// TestIsResponseBlockedByIP_AllowOverridesBlockSameTier verifies that a
// custom/published allow for the response IP within the SAME tier entity
// overrides a published block — matching buildEntityResult's within-entity
// precedence in policy.go.
func TestIsResponseBlockedByIP_AllowOverridesBlockSameTier(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	blResult, fixtureErr836 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'CDN IPs', 1)")
	if fixtureErr836 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr836)
	}
	blID, fixtureErr837 := blResult.LastInsertId()
	if fixtureErr837 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr837)
	}
	if _, fixtureErr838 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "203.0.113.10"); fixtureErr838 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr838)
	}
	if _, fixtureErr839 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", blID); fixtureErr839 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr839)
	}

	alResult, fixtureErr841 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'CDN Allow', 1)")
	if fixtureErr841 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr841)
	}
	alID, fixtureErr842 := alResult.LastInsertId()
	if fixtureErr842 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr842)
	}
	if _, fixtureErr843 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", alID, "203.0.113.10"); fixtureErr843 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr843)
	}
	if _, fixtureErr844 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.42", alID); fixtureErr844 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr844)
	}
	if _, fixtureErr845 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.42', 'test.com.', 'A', 'NOERROR')"); fixtureErr845 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr845)
	}

	mustReloadPolicy(t)

	if responseIPsBlocked("10.42.1.42", extractResponseIPs(dnsResponseWithA("203.0.113.10"))) {
		t.Error("expected direct-client allow to override the direct-client block for the same IP")
	}
}

// TestIsResponseBlockedByIP_NarrowestTierWins verifies the DD-002 cascade:
// an IP-tier allow overrides a broader Range-tier block for the same client.
func TestIsResponseBlockedByIP_NarrowestTierWins(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	blResult, fixtureErr860 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Broad Range Block', 1)")
	if fixtureErr860 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr860)
	}
	blID, fixtureErr861 := blResult.LastInsertId()
	if fixtureErr861 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr861)
	}
	if _, fixtureErr862 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "198.51.100.7"); fixtureErr862 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr862)
	}

	rangeResult, fixtureErr864 := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('Home', '10.42.1.0/24')")
	if fixtureErr864 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr864)
	}
	rangeID, fixtureErr865 := rangeResult.LastInsertId()
	if fixtureErr865 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr865)
	}
	if _, fixtureErr866 := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", rangeID, blID); fixtureErr866 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr866)
	}

	alResult, fixtureErr868 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('', 'Direct Allow', 1)")
	if fixtureErr868 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr868)
	}
	alID, fixtureErr869 := alResult.LastInsertId()
	if fixtureErr869 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr869)
	}
	if _, fixtureErr870 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", alID, "198.51.100.7"); fixtureErr870 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr870)
	}
	if _, fixtureErr871 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.42", alID); fixtureErr871 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr871)
	}
	if _, fixtureErr872 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES ('10.42.1.42', 'test.com.', 'A', 'NOERROR')"); fixtureErr872 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr872)
	}

	mustReloadPolicy(t)

	// A client in the range but with no direct allow should still be blocked.
	if !responseIPsBlocked("10.42.1.99", extractResponseIPs(dnsResponseWithA("198.51.100.7"))) {
		t.Error("expected range-tier block to apply to a client without a narrower override")
	}
	// The client WITH the direct-tier allow should win (narrowest wins).
	if responseIPsBlocked("10.42.1.42", extractResponseIPs(dnsResponseWithA("198.51.100.7"))) {
		t.Error("expected IP-tier allow to override the Range-tier block (DD-002 narrowest-wins cascade)")
	}
}
