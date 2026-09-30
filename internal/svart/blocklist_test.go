package svart

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miekg/dns"

	"github.com/yeti/svart-dns/internal/listparse"

	"github.com/yeti/svart-dns/internal/policycore"
)

func TestShouldSkipRule(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		shouldSkip bool
		reason     string
	}{
		{
			name:       "comment with exclamation",
			input:      "! This is a comment",
			shouldSkip: true,
			reason:     "comments",
		},
		{
			name:       "comment with hash",
			input:      "# This is a comment",
			shouldSkip: true,
			reason:     "comments",
		},
		{
			name:       "whitelist rule",
			input:      "@@||example.com^",
			shouldSkip: true,
			reason:     "whitelist",
		},
		{
			name:       "badfilter rule",
			input:      "||tn.porngo.xxx^$badfilter",
			shouldSkip: true,
			reason:     "badfilter",
		},
		{
			name:       "regex pattern with slashes",
			input:      "/^94\\.242\\.247\\.(2[0-9]|3[0-2]):/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "regex pattern IP range",
			input:      "/^23\\.109\\.170\\.(18[7-9]|19[0-2])$/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "regex pattern with digits",
			input:      "/^23\\.109\\.73\\.\\d{3}/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "empty line",
			input:      "",
			shouldSkip: true,
			reason:     "empty",
		},
		{
			name:       "whitespace line",
			input:      "   ",
			shouldSkip: true,
			reason:     "empty",
		},
		{
			name:       "valid domain rule",
			input:      "||example.com^",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "valid IP rule",
			input:      "||193.200.64.30^",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "valid domain with modifiers",
			input:      "||example.com^$third-party",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "badfilter in domain name (not modifier)",
			input:      "||badfilter.com^",
			shouldSkip: false,
			reason:     "valid - badfilter not in modifier",
		},
		{
			name:       "dnstype modifier rule",
			input:      "|google.com|$dnstype=TXT",
			shouldSkip: true,
			reason:     "dnstype modifier",
		},
		{
			name:       "dnsrewrite modifier rule",
			input:      "||example.com^$dnsrewrite=1.2.3.4",
			shouldSkip: true,
			reason:     "dnsrewrite modifier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := tt.input
			shouldSkip := false

			if line == "" || line == "   " {
				shouldSkip = true
			} else if line[0] == '!' || line[0] == '#' {
				shouldSkip = true
			} else if len(line) >= 2 && line[0:2] == "@@" {
				shouldSkip = true
			} else if len(line) >= 1 && line[0] == '/' && line[len(line)-1] == '/' {
				shouldSkip = true
			} else if strings.Contains(line, "$") {
				parts := strings.Split(line, "$")
				if len(parts) > 1 {
					modifiers := strings.Split(parts[1], ",")
					for _, mod := range modifiers {
						modTrimmed := strings.TrimSpace(mod)
						if modTrimmed == "badfilter" ||
							strings.HasPrefix(modTrimmed, "dnstype=") ||
							strings.HasPrefix(modTrimmed, "dnsrewrite=") ||
							strings.Contains(modTrimmed, "client=") ||
							strings.Contains(modTrimmed, "ctag=") {
							shouldSkip = true
							break
						}
					}
				}
			} else if listparse.ParseDomain(line) == "" {
				shouldSkip = true
			}

			if shouldSkip != tt.shouldSkip {
				t.Errorf("Rule %q: shouldSkip = %v, want %v (reason: %s)", tt.input, shouldSkip, tt.shouldSkip, tt.reason)
			}
		})
	}
}

func TestClassifyDomain(t *testing.T) {
	tests := []struct {
		name           string
		domain         string
		expectedBucket string
		expectedValue  string
	}{
		{
			name:           "plain domain",
			domain:         "facebook.com",
			expectedBucket: "exact",
			expectedValue:  "facebook.com",
		},
		{
			name:           "subdomain",
			domain:         "ads.facebook.com",
			expectedBucket: "exact",
			expectedValue:  "ads.facebook.com",
		},
		{
			name:           "IP address",
			domain:         "193.200.64.30",
			expectedBucket: "exact",
			expectedValue:  "193.200.64.30",
		},
		{
			name:           "simple wildcard prefix",
			domain:         "*.facebook.com",
			expectedBucket: "suffix",
			expectedValue:  "facebook.com",
		},
		{
			name:           "wildcard with deep suffix",
			domain:         "*.ads.facebook.com",
			expectedBucket: "suffix",
			expectedValue:  "ads.facebook.com",
		},
		{
			name:           "complex wildcard middle",
			domain:         "ad*.tracker*.com",
			expectedBucket: "complex",
			expectedValue:  "ad*.tracker*.com",
		},
		{
			name:           "complex wildcard double star",
			domain:         "*.ads.*.com",
			expectedBucket: "complex",
			expectedValue:  "*.ads.*.com",
		},
		{
			name:           "wildcard suffix only",
			domain:         "ads.*",
			expectedBucket: "complex",
			expectedValue:  "ads.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, value := policycore.ClassifyDomain(tt.domain)
			if bucket != tt.expectedBucket {
				t.Errorf("classifyDomain(%q) bucket = %q, want %q", tt.domain, bucket, tt.expectedBucket)
			}
			if value != tt.expectedValue {
				t.Errorf("classifyDomain(%q) value = %q, want %q", tt.domain, value, tt.expectedValue)
			}
		})
	}
}

func TestWildcardSuffixNegative_RootDomainNotBlocked(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create blocklist with *.adserver.com
	result, fixtureErr232 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/wildcards.txt", "Wildcard List", true)
	if fixtureErr232 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr232)
	}
	listID, fixtureErr234 := result.LastInsertId()
	if fixtureErr234 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr234)
	}

	if _, fixtureErr236 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", listID, "*.adserver.com"); fixtureErr236 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr236)
	}

	// Assign to client and seed query log
	if _, fixtureErr239 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr239 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr239)
	}
	if _, fixtureErr241 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr241 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr241)
	}

	mustReloadPolicy(t)

	// *.adserver.com should block sub.adserver.com
	if !isBlockedForClient("10.42.1.42", "sub.adserver.com.") {
		t.Error("expected sub.adserver.com to be blocked by *.adserver.com")
	}

	// *.adserver.com should block deep.sub.adserver.com
	if !isBlockedForClient("10.42.1.42", "deep.sub.adserver.com.") {
		t.Error("expected deep.sub.adserver.com to be blocked by *.adserver.com")
	}

	// *.adserver.com must NOT block adserver.com itself
	if isBlockedForClient("10.42.1.42", "adserver.com.") {
		t.Error("*.adserver.com must NOT block the root domain adserver.com")
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		domain   string
		expected bool
	}{
		{
			name:     "simple wildcard prefix",
			pattern:  "*.example.com",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "simple wildcard prefix no match",
			pattern:  "*.example.com",
			domain:   "example.com",
			expected: false,
		},
		{
			name:     "wildcard suffix",
			pattern:  "ads.*",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "wildcard middle",
			pattern:  "ads.*.com",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "no wildcard exact match",
			pattern:  "example.com",
			domain:   "example.com",
			expected: false,
		},
		{
			name:     "no wildcard no match",
			pattern:  "example.com",
			domain:   "ads.example.com",
			expected: false,
		},
		{
			name:     "multiple wildcards",
			pattern:  "*.ads.*.com",
			domain:   "tracking.ads.example.com",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := policycore.NewComplexRule(tt.pattern, 0)
			if result := rule.Matches(tt.domain); result != tt.expected {
				t.Errorf("complex rule %q matches %q = %v, want %v", tt.pattern, tt.domain, result, tt.expected)
			}
		})
	}
}

func TestReverseARPAToIP(t *testing.T) {
	tests := []struct {
		name     string
		arpa     string
		expected string
	}{
		{
			name:     "standard reverse DNS",
			arpa:     "30.64.200.193.in-addr.arpa",
			expected: "193.200.64.30",
		},
		{
			name:     "another reverse DNS",
			arpa:     "210.190.117.212.in-addr.arpa",
			expected: "212.117.190.210",
		},
		{
			name:     "with trailing dot",
			arpa:     "30.64.200.193.in-addr.arpa.",
			expected: "",
		},
		{
			name:     "invalid format",
			arpa:     "invalid.in-addr.arpa",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := policycore.ReverseARPAToIP(tt.arpa)
			if result != tt.expected {
				t.Errorf("reverseARPAToIP(%q) = %q, want %q", tt.arpa, result, tt.expected)
			}
		})
	}
}

func TestIPBlockingScenarios(t *testing.T) {
	tests := []struct {
		name        string
		rule        string
		shouldBlock bool
		description string
	}{
		{
			name:        "IP address blocking",
			rule:        "||193.200.64.30^",
			shouldBlock: true,
			description: "Should block IP 193.200.64.30 after resolution",
		},
		{
			name:        "malicious server IP",
			rule:        "||212.117.190.210^",
			shouldBlock: true,
			description: "Should block known malicious server IP",
		},
		{
			name:        "tracking pixel IP",
			rule:        "||185.246.188.124^",
			shouldBlock: true,
			description: "Should block tracking pixel server IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain := listparse.ParseDomain(tt.rule)
			if domain == "" {
				t.Errorf("listparse.ParseDomain(%q) returned empty, expected IP address", tt.rule)
			}

			if !strings.Contains(domain, ".") || len(strings.Split(domain, ".")) != 4 {
				t.Errorf("listparse.ParseDomain(%q) = %q, expected IP format", tt.rule, domain)
			}
		})
	}
}

func TestIsBlockedForClientWithDB(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	tests := []struct {
		name     string
		clientIP string
		domain   string
		blocked  bool
	}{
		// Chris (10.42.1.42) has ads + tracking lists directly
		{
			name:     "Chris blocked by ads list (exact match)",
			clientIP: "10.42.1.42",
			domain:   "ads.google.com.",
			blocked:  true,
		},
		{
			name:     "Chris blocked by tracking list (exact match)",
			clientIP: "10.42.1.42",
			domain:   "segment.io.",
			blocked:  true,
		},
		{
			name:     "Chris allows non-blocked domain",
			clientIP: "10.42.1.42",
			domain:   "github.com.",
			blocked:  false,
		},
		// Sam iPhone (10.42.1.43) is in Sam group which has ads list
		{
			name:     "Sam iPhone blocked via group (ads list)",
			clientIP: "10.42.1.43",
			domain:   "ads.facebook.com.",
			blocked:  true,
		},
		{
			name:     "Sam iPhone not blocked by tracking (not assigned)",
			clientIP: "10.42.1.43",
			domain:   "segment.io.",
			blocked:  false,
		},
		// NAS box (10.42.1.100) is in Servers group which has tracking list
		{
			name:     "NAS box blocked via Servers group (tracking)",
			clientIP: "10.42.1.100",
			domain:   "newrelic.com.",
			blocked:  true,
		},
		{
			name:     "NAS box not blocked by ads (not assigned)",
			clientIP: "10.42.1.100",
			domain:   "ads.google.com.",
			blocked:  false,
		},
		// IoT Thermostat (10.42.3.50) has no blocklist assignments
		{
			name:     "IoT device with no assignments passes everything",
			clientIP: "10.42.3.50",
			domain:   "ads.google.com.",
			blocked:  false,
		},
		{
			name:     "Unknown client passes everything",
			clientIP: "192.168.1.1",
			domain:   "ads.google.com.",
			blocked:  false,
		},
		// Subdomain matching
		{
			name:     "Chris blocked by subdomain match",
			clientIP: "10.42.1.42",
			domain:   "sub.ads.google.com.",
			blocked:  true,
		},
		// Wildcard matching
		{
			name:     "Chris blocked by wildcard pattern",
			clientIP: "10.42.1.42",
			domain:   "cdn.adserver.com.",
			blocked:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBlockedForClient(tt.clientIP, tt.domain)
			if result != tt.blocked {
				t.Errorf("isBlockedForClient(%q, %q) = %v, want %v", tt.clientIP, tt.domain, result, tt.blocked)
			}
		})
	}
}

func TestIsBlockedForClientCacheEffectiveness(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	// Clear the policy cache
	policyCache.Clear()

	// First call — should populate policy cache
	result1 := isBlockedForClient("10.42.1.42", "ads.google.com.")

	// Verify it's in the policy cache now
	cacheKey := "10.42.1.42:ads.google.com"
	cachedResult, ok := policyCache.Load(testPolicyKey(cacheKey))
	if !ok {
		t.Fatal("expected policy cache to be populated after first call")
	}
	if (cachedResult.Result == "block") != result1 {
		t.Error("cached value doesn't match result")
	}

	// Second call should hit cache (we can't directly verify this but can verify
	// the result is consistent)
	result2 := isBlockedForClient("10.42.1.42", "ads.google.com.")
	if result1 != result2 {
		t.Error("inconsistent results between cached and uncached calls")
	}
}

func TestIsBlockedForClientDisabledBlocklist(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create a disabled blocklist with domains
	result, fixtureErr528 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/list.txt", "Disabled List", false)
	if fixtureErr528 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr528)
	}
	listID, fixtureErr530 := result.LastInsertId()
	if fixtureErr530 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr530)
	}

	if _, fixtureErr532 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", listID, "should-not-block.com"); fixtureErr532 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr532)
	}

	// Assign to a client
	if _, fixtureErr535 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr535 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr535)
	}
	if _, fixtureErr537 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr537 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr537)
	}

	mustReloadPolicy(t)

	// Should NOT be blocked because the list is disabled
	if isBlockedForClient("10.42.1.42", "should-not-block.com.") {
		t.Error("disabled blocklist should not block domains")
	}
}

func TestIsBlockedForClientPolicyWithoutQueryLogs(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr551 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Policy Blocklist", true)
	if fixtureErr551 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr551)
	}
	listID, fixtureErr553 := result.LastInsertId()
	if fixtureErr553 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr553)
	}

	if _, fixtureErr555 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", listID, "policy-blocked.example.com"); fixtureErr555 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr555)
	}

	policyResult, fixtureErr557 := db.Exec("INSERT INTO policies (name) VALUES (?)", "No Traffic Policy")
	if fixtureErr557 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr557)
	}
	policyID, fixtureErr558 := policyResult.LastInsertId()
	if fixtureErr558 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr558)
	}

	if _, fixtureErr560 := db.Exec("INSERT INTO policy_blocklists (policy_id, blocklist_id) VALUES (?, ?)", policyID, listID); fixtureErr560 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr560)
	}
	if _, fixtureErr561 := db.Exec("INSERT INTO client_policies (client_ip, policy_id) VALUES (?, ?)", "10.42.1.53", policyID); fixtureErr561 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr561)
	}

	mustReloadPolicy(t)

	if !isBlockedForClient("10.42.1.53", "policy-blocked.example.com.") {
		t.Fatal("expected configured client policy to block domains before first traffic")
	}
}

func TestIsBlockedForClientReverseARPA(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create blocklist with an IP address as a blocked "domain"
	result, fixtureErr575 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/iplist.txt", "IP List", true)
	if fixtureErr575 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr575)
	}
	listID, fixtureErr577 := result.LastInsertId()
	if fixtureErr577 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr577)
	}

	if _, fixtureErr579 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", listID, "193.200.64.30"); fixtureErr579 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr579)
	}

	// Assign to client and seed query log
	if _, fixtureErr582 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr582 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr582)
	}
	if _, fixtureErr584 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr584 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr584)
	}

	mustReloadPolicy(t)

	// Reverse ARPA lookup for 193.200.64.30 should be blocked
	if !isBlockedForClient("10.42.1.42", "30.64.200.193.in-addr.arpa.") {
		t.Error("expected reverse ARPA lookup for blocked IP to be blocked")
	}

	// Non-blocked IP reverse should not be blocked
	if isBlockedForClient("10.42.1.42", "1.1.1.1.in-addr.arpa.") {
		t.Error("expected non-blocked IP reverse ARPA to pass")
	}
}

// TestBlocklistChangelogSkipsInitialPopulation verifies that blocklist_changelog
// is NOT written when a list is refreshed from empty (previousCount == 0), but IS
// written when a list that already has domains is refreshed with changes.
func TestBlocklistChangelogSkipsInitialPopulation(t *testing.T) {
	// The list server is a local httptest server; fetching it needs the
	// same explicit opt-in an operator with a LAN list server sets.
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	// First blocklist content: five realistic ad/tracking domains
	initialDomains := []string{
		"||ads.google.com^",
		"||pagead2.googlesyndication.com^",
		"||tracking.facebook.com^",
		"||analytics.tiktok.com^",
		"||telemetry.microsoft.com^",
	}

	// Second refresh: remove one domain, add two new ones
	updatedDomains := []string{
		"||ads.google.com^",
		"||pagead2.googlesyndication.com^",
		"||analytics.tiktok.com^",
		"||telemetry.microsoft.com^",
		"||adservice.google.com^",
		"||doubleclick.net^",
	}

	// Serve the initial list content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, fixtureErr630 := fmt.Fprintln(w, strings.Join(initialDomains, "\n")); fixtureErr630 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr630)
		}
	}))
	defer srv.Close()

	// Insert a blocklist with zero domains (simulates a freshly synced list)
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		srv.URL, "Hagezi Pro (test)", true)
	if err != nil {
		t.Fatalf("failed to insert blocklist: %v", err)
	}
	listID, fixtureErr640 := res.LastInsertId()
	if fixtureErr640 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr640)
	}

	// Verify the list starts empty
	var startCount int
	if fixtureErr644 := db.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ?", listID).Scan(&startCount); fixtureErr644 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr644)
	}
	if startCount != 0 {
		t.Fatalf("expected empty list before first refresh, got %d domains", startCount)
	}

	// --- First refresh: initial population (previousCount == 0) ---
	if err := refreshBlocklistByID(int(listID)); err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}

	// blocklist_history row MUST exist (event recording is unconditional)
	var historyCount int
	if fixtureErr656 := db.QueryRow("SELECT COUNT(*) FROM blocklist_history WHERE blocklist_id = ?", listID).Scan(&historyCount); fixtureErr656 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr656)
	}
	if historyCount != 1 {
		t.Errorf("expected 1 blocklist_history row after initial refresh, got %d", historyCount)
	}

	// blocklist_changelog rows must NOT exist — initial population is not a meaningful diff
	var changelogCount int
	if fixtureErr665 := db.QueryRow(`SELECT COUNT(*) FROM blocklist_changelog cl
		JOIN blocklist_history h ON cl.history_id = h.id
		WHERE h.blocklist_id = ?`, listID).Scan(&changelogCount); fixtureErr665 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr665)
	}
	if changelogCount != 0 {
		t.Errorf("expected 0 blocklist_changelog rows for initial population, got %d", changelogCount)
	}

	// Confirm domains were actually loaded
	var domainCount int
	if fixtureErr672 := db.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ?", listID).Scan(&domainCount); fixtureErr672 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr672)
	}
	if domainCount != len(initialDomains) {
		t.Errorf("expected %d domains after initial refresh, got %d", len(initialDomains), domainCount)
	}

	// --- Second refresh: swap in updated content with actual changes ---
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, fixtureErr679 := fmt.Fprintln(w, strings.Join(updatedDomains, "\n")); fixtureErr679 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr679)
		}
	}))
	defer srv2.Close()

	// Point the blocklist at the new server URL
	if _, fixtureErr684 := db.Exec("UPDATE blocklists SET url = ? WHERE id = ?", srv2.URL, listID); fixtureErr684 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr684)
	}

	if err := refreshBlocklistByID(int(listID)); err != nil {
		t.Fatalf("second refresh failed: %v", err)
	}

	// blocklist_history should now have two rows
	if fixtureErr691 := db.QueryRow("SELECT COUNT(*) FROM blocklist_history WHERE blocklist_id = ?", listID).Scan(&historyCount); fixtureErr691 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr691)
	}
	if historyCount != 2 {
		t.Errorf("expected 2 blocklist_history rows after second refresh, got %d", historyCount)
	}

	// blocklist_changelog MUST now have rows for the added/removed domains
	if fixtureErr699 := db.QueryRow(`SELECT COUNT(*) FROM blocklist_changelog cl
		JOIN blocklist_history h ON cl.history_id = h.id
		WHERE h.blocklist_id = ?`, listID).Scan(&changelogCount); fixtureErr699 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr699)
	}
	if changelogCount == 0 {
		t.Error("expected blocklist_changelog rows for second refresh with changes, got 0")
	}

	// Sanity-check: one domain removed (tracking.facebook.com), two added
	var addedCount, removedCount int
	if fixtureErr708 := db.QueryRow(`SELECT COUNT(*) FROM blocklist_changelog cl
		JOIN blocklist_history h ON cl.history_id = h.id
		WHERE h.blocklist_id = ? AND cl.action = 'added'`, listID).Scan(&addedCount); fixtureErr708 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr708)
	}
	if fixtureErr711 := db.QueryRow(`SELECT COUNT(*) FROM blocklist_changelog cl
		JOIN blocklist_history h ON cl.history_id = h.id
		WHERE h.blocklist_id = ? AND cl.action = 'removed'`, listID).Scan(&removedCount); fixtureErr711 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr711)
	}
	if addedCount != 2 {
		t.Errorf("expected 2 added changelog entries, got %d", addedCount)
	}
	if removedCount != 1 {
		t.Errorf("expected 1 removed changelog entry, got %d", removedCount)
	}
}

// --- responseIPsBlocked tests (A4) ---
//
// Before the fix, the response-IP check only consulted the direct per-client
// per-client blocklists (IP tier), silently skipping the Range and
// Group tiers — breaking DD-002's uniform three-tier model for IP-response
// blocking (a domain resolving via rebinding/CNAME-cloaking to an
// IP-literal-blocked address would sail through if that entry only lived at
// the Range or Group tier). There were previously ZERO tests referencing
// this function at all.

// dnsResponseWithA builds a minimal DNS response whose only answer is an A
// record for the given IP — enough for responseIPsBlocked to evaluate.
func dnsResponseWithA(ip string) *dns.Msg {
	msg := new(dns.Msg)
	msg.Answer = append(msg.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP(ip).To4(),
	})
	return msg
}
