package svart

import (
	"math/rand/v2"
	"testing"
)

// BenchmarkEvaluatePolicy_Uncached is a policy-cache miss on a realistic
// configuration: six overlapping lists (Tranco-derived, with wildcard,
// complex and IP rules), an allowlist, a 0.0.0.0/0 range and a /16, two
// groups, and a client with direct and custom rules, evaluated for a mix of
// listed, parent-matched and never-listed names. BenchmarkEvaluatePolicy_ColdMiss
// clears the whole cache every iteration, so it mostly measures Clear.
func BenchmarkEvaluatePolicy_Uncached(b *testing.B) {
	defer setupTestDBTB(b)()
	// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
	rng := rand.New(rand.NewPCG(42, 7))
	lists := fixtureLists(b, rng)
	tx, fixtureErr653 := db.Begin()
	if fixtureErr653 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr653)
	}
	var ids []int64
	for i, l := range lists {
		res, fixtureErr719 := tx.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, 1)",
			"https://lists.example.net/"+worldListNames[false][i]+".txt", worldListNames[false][i])
		if fixtureErr719 != nil {
			b.Errorf("fixture operation failed: %v", fixtureErr719)
		}
		id, fixtureErr895 := res.LastInsertId()
		if fixtureErr895 != nil {
			b.Errorf("fixture operation failed: %v", fixtureErr895)
		}
		ids = append(ids, id)
		for _, r := range l.rules {
			if _, err := tx.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, r); err != nil {
				b.Errorf("fixture operation failed: %v", err)
			}
		}
	}
	res, fixtureErr1153 := tx.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES ('https://lists.example.net/allow.txt', 'Hagezi Allow', 1)")
	if fixtureErr1153 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr1153)
	}
	allowID, fixtureErr1286 := res.LastInsertId()
	if fixtureErr1286 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr1286)
	}
	for _, r := range lists[5].rules[:40] {
		if _, err := tx.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", allowID, r); err != nil {
			b.Errorf("fixture operation failed: %v", err)
		}
	}
	var fixtureErr1534 error
	res, fixtureErr1534 = tx.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Manual Block (10.20.30.40)', 1)")
	if fixtureErr1534 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr1534)
	}
	manualID, fixtureErr1645 := res.LastInsertId()
	if fixtureErr1645 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr1645)
	}
	if _, err := tx.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'tiktok.com'), (?, '*.doubleclick.net')", manualID, manualID); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO ip_ranges (id, name, cidr) VALUES (1, 'everyone', '0.0.0.0/0'), (2, 'office', '10.20.0.0/16')"); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?), (2, ?), (2, ?)", ids[0], ids[1], ids[2]); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (2, ?)", allowID); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO client_groups (id, name) VALUES (1, 'Kids'), (2, 'IoT jail')"); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (1, ?), (2, ?)", ids[3], ids[4]); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES ('10.20.30.40', 1), ('10.20.30.40', 2)"); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.20.30.40', ?), ('10.20.30.40', ?)", ids[5], manualID); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	mustReloadPolicy(b)
	queries := queryCorpus(rng, lists, 4096)

	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		evaluatePolicyFull("10.20.30.40", queries[i&4095], 1)
	}
}
