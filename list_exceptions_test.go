package main

import (
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

// TestListExceptionsThroughPolicyEngine drives real list content (Hagezi
// spam-tlds and Dandelion Sprout lines) through parsing, storage and policy
// evaluation: a list's own @@ rules and $denyallow values exempt names from
// that list, other lists still block them, and wildcard patterns ending in
// "." match the names they were written for.
func TestListExceptionsThroughPolicyEngine(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	spam, err := listparse.Parse(strings.NewReader(strings.Join([]string{
		"||*.africa^$denyallow=nation.africa",
		"||*.top^$dnstype=~CNAME,denyallow=caitlin.top|reminder.top",
		"@@||masto*.top^",
		"*austrailasale.",
	}, "\n")), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	const client = "192.168.1.50"
	spamID := insertTestList(t, "Spam TLDs", spam)
	strict := insertTestList(t, "Strict", listparse.Result{Domains: []string{"reminder.top"}})
	for _, id := range []int64{spamID, strict} {
		if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", client, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES (?, 'Test laptop')", client); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := reloadPolicyState("test", reloadListContent); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		domain, want, rule string
	}{
		{"scam.africa", "block", "||*.africa^$denyallow=nation.africa"},
		{"nation.africa", "allow", ""},     // $denyallow
		{"www.nation.africa", "allow", ""}, // exceptions cover subdomains
		{"caitlin.top", "allow", ""},       // $denyallow with several values
		{"mastodon.top", "allow", ""},      // @@ wildcard exception
		{"cheap-pills.top", "block", "||*.top^$dnstype=~CNAME,denyallow=caitlin.top|reminder.top"},
		{"reminder.top", "block", "reminder.top"}, // exempt from Spam TLDs, blocked by Strict
		{"austrailasale.com", "block", "*austrailasale."},
		{"shop.austrailasale.co.uk", "block", "*austrailasale."},
		{"example.com", "allow", ""},
	}
	for _, c := range cases {
		policyCache.Clear()
		r := evaluatePolicyFull(client, c.domain, 1)
		rule := ""
		if r.ResultSource != nil && r.ResultSource.PublishedList != nil {
			rule = r.ResultSource.PublishedList.Rule
		} else if r.ResultSource != nil && r.ResultSource.CustomRule != nil {
			rule = r.ResultSource.CustomRule.Rule
		}
		if r.Result != c.want || rule != c.rule {
			t.Errorf("%s: result %q rule %q, want %q rule %q", c.domain, r.Result, rule, c.want, c.rule)
		}
	}

	var count int
	if err := db.QueryRow("SELECT domain_count FROM blocklists WHERE id = ?", spamID).Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 3 {
		t.Errorf("domain_count = %d, want 3 (exceptions are not blocked domains)", count)
	}
}

// insertTestList stores a parsed list the way a refresh does.
func insertTestList(t *testing.T, alias string, list listparse.Result) int64 {
	t.Helper()
	rows, count := listparse.StoredRows(list, true)
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled, domain_count) VALUES (?, ?, 1, ?)",
		"https://lists.example/"+strings.ToLower(strings.ReplaceAll(alias, " ", "-"))+".txt", alias, count)
	if err != nil {
		t.Fatal(err)
	}
	id, fixtureErr3251 := res.LastInsertId()
	if fixtureErr3251 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3251)
	}
	for _, row := range rows {
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, row); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// TestAdGuardRulesThroughPolicyEngine drives Dandelion Sprout's AdGuard
// syntax through the policy engine. A version of the parser that dropped
// $dnstype applied "|google.com|$dnstype=TXT" to every query, blocking
// google.com and apple.com outright; the parity run against production lists
// caught it.
func TestAdGuardRulesThroughPolicyEngine(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	list, err := listparse.Parse(strings.NewReader(strings.Join([]string{
		"\xef\xbb\xbf[Adblock Plus 3.13]",
		"! Brazilian mobsters trying to DDoS Cisco with TXT-type DNS requests",
		"|google.com|$dnstype=TXT",
		"|apple.com|$dnstype=TXT",
		"||glthub.",
		"://ww4.$denyallow=ww4.ikea.com|ww4.autotask.net",
		".booking.com-id*",
		"||steamcommin*$denyallow=likely-a-fake-malware-version-of-steamcommunity.com",
		"@@://blog.*.top^",
		"||*.top^$dnstype=~CNAME",
	}, "\n")), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if list.Invalid != 0 || list.Unsupported != 0 {
		t.Fatalf("invalid=%d unsupported=%d, want 0 and 0 (record-type restrictions are supported)", list.Invalid, list.Unsupported)
	}
	const client = "192.168.1.50"
	id := insertTestList(t, "Dandelion Sprout", list)
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", client, id); err != nil {
		t.Fatal(err)
	}
	if err := reloadPolicyState("test", reloadListContent); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct{ domain, want, rule string }{
		{"google.com", "allow", ""},
		{"accounts.google.com", "allow", ""},
		{"apple.com", "allow", ""},
		{"glthub.com", "block", "||glthub."},
		{"cdn1.glthub.net", "block", "||glthub."},
		{"github.com", "allow", ""},
		{"ww4.parked-domain.com", "block", "://ww4.$denyallow=ww4.ikea.com|ww4.autotask.net"},
		{"ww4.ikea.com", "allow", ""},
		{"www.ikea.com", "allow", ""},
		{"secure.booking.com-id48213.info", "block", ".booking.com-id*"},
		{"booking.com", "allow", ""},
		{"steamcommininty.com", "block", "||steamcommin*$denyallow=likely-a-fake-malware-version-of-steamcommunity.com"},
		{"login.steamcommunlty.ru", "allow", ""},
		{"login.steamcommininty.ru", "block", "||steamcommin*$denyallow=likely-a-fake-malware-version-of-steamcommunity.com"},
		{"steamcommunity.com", "allow", ""},
		{"blog.example.top", "allow", ""},
		{"cheap-pills.top", "block", "||*.top^$dnstype=~CNAME"},
	} {
		policyCache.Clear()
		r := evaluatePolicyFull(client, c.domain, 1)
		rule := ""
		if r.ResultSource != nil && r.ResultSource.PublishedList != nil {
			rule = r.ResultSource.PublishedList.Rule
		}
		if r.Result != c.want || rule != c.rule {
			t.Errorf("%s: result %q rule %q, want %q rule %q", c.domain, r.Result, rule, c.want, c.rule)
		}
	}
}
