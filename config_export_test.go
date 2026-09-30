package main

import (
	"slices"
	"testing"
)

// TestConfigExportKeepsDisabledListAssignments: a disabled list stays
// assigned to its groups, ranges and policies in the database, so an export
// (a backup) must keep those assignments or a restore silently drops them.
func TestConfigExportKeepsDisabledListAssignments(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	var disabledID, groupID int
	if err := db.QueryRow("SELECT id FROM blocklists WHERE alias = 'Steven Black Unified' AND enabled = 0").Scan(&disabledID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := db.QueryRow("SELECT id FROM client_groups WHERE name = 'Sam'").Scan(&groupID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	insert := func(q string, args ...any) int64 {
		t.Helper()
		res, err := db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, fixtureErr906 := res.LastInsertId()
		if fixtureErr906 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr906)
		}
		return id
	}
	allowID := insert("INSERT INTO allowlists (url, alias, enabled) VALUES ('https://lists.example.net/allow.txt', 'Streaming Fixes', 0)")
	policyID := insert("INSERT INTO policies (name, description) VALUES ('Strict', 'kids devices')")
	rangeID := insert("INSERT INTO ip_ranges (name, cidr) VALUES ('IoT VLAN', '10.42.3.0/24')")
	insert("INSERT INTO policy_blocklists (policy_id, blocklist_id) VALUES (?, ?)", policyID, disabledID)
	insert("INSERT INTO policy_allowlists (policy_id, allowlist_id) VALUES (?, ?)", policyID, allowID)
	insert("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", groupID, disabledID)
	insert("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (?, ?)", groupID, allowID)
	insert("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", rangeID, disabledID)
	insert("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (?, ?)", rangeID, allowID)
	mustReloadPolicy(t)

	export, err := buildConfigExport()
	if err != nil {
		t.Fatal(err)
	}
	find := func(name string, names []string) int {
		t.Helper()
		i := slices.Index(names, name)
		if i < 0 {
			t.Fatalf("%q missing from export (%v)", name, names)
		}
		return i
	}
	var policyNames, groupNames, rangeNames []string
	for _, p := range export.Policies {
		policyNames = append(policyNames, p.Name)
	}
	for _, g := range export.Groups {
		groupNames = append(groupNames, g.Name)
	}
	for _, r := range export.Ranges {
		rangeNames = append(rangeNames, r.Name)
	}
	type lists struct{ block, allow []string }
	for what, c := range map[string]struct{ got, want lists }{
		"policy Strict": {lists{export.Policies[find("Strict", policyNames)].Blocklists, export.Policies[find("Strict", policyNames)].Allowlists},
			lists{[]string{"Steven Black Unified"}, []string{"Streaming Fixes"}}},
		"group Sam": {lists{export.Groups[find("Sam", groupNames)].Blocklists, export.Groups[find("Sam", groupNames)].Allowlists},
			lists{[]string{"Hagezi Pro", "Steven Black Unified"}, []string{"Streaming Fixes"}}},
		"range IoT VLAN": {lists{export.Ranges[find("IoT VLAN", rangeNames)].Blocklists, export.Ranges[find("IoT VLAN", rangeNames)].Allowlists},
			lists{[]string{"Steven Black Unified"}, []string{"Streaming Fixes"}}},
	} {
		slices.Sort(c.got.block)
		slices.Sort(c.got.allow)
		if !slices.Equal(c.got.block, c.want.block) || !slices.Equal(c.got.allow, c.want.allow) {
			t.Errorf("%s exported blocklists %v allowlists %v, want %v and %v", what, c.got.block, c.got.allow, c.want.block, c.want.allow)
		}
	}
}
