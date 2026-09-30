package svart

import "testing"

func TestConfigExportRejectsBrokenReferencesAndReleasesSnapshot(t *testing.T) {
	for _, fixture := range []string{
		`INSERT INTO policy_blocklists(policy_id,blocklist_id) VALUES(999,999)`,
		`INSERT INTO policy_allowlists(policy_id,allowlist_id) VALUES(999,999)`,
		`INSERT INTO group_blocklists(group_id,blocklist_id) VALUES(999,999)`,
		`INSERT INTO group_allowlists(group_id,allowlist_id) VALUES(999,999)`,
		`INSERT INTO range_blocklists(range_id,blocklist_id) VALUES(999,999)`,
		`INSERT INTO range_allowlists(range_id,allowlist_id) VALUES(999,999)`,
		`INSERT INTO client_group_members(group_id,client_ip) VALUES(999,'192.0.2.10')`,
		`INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES('192.0.2.10',999)`,
		`INSERT INTO client_allowlists(client_ip,allowlist_id) VALUES('192.0.2.10',999)`,
		`INSERT INTO client_policies(client_ip,policy_id) VALUES('192.0.2.10',999)`,
		`INSERT INTO client_groups(name,policy_id) VALUES('Broken group',999)`,
		`INSERT INTO ip_ranges(name,cidr,policy_id) VALUES('Broken range','192.0.2.0/24',999)`,
		`INSERT INTO blocked_domains(blocklist_id,domain) VALUES(999,'ads.example')`,
		`INSERT INTO allowed_domains(allowlist_id,domain) VALUES(999,'login.example')`,
	} {
		t.Run(fixture, func(t *testing.T) {
			defer setupTestDB(t)()
			readDB.SetMaxOpenConns(1)
			if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(fixture); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				export, err := buildConfigExport()
				if export != nil || err == nil {
					t.Fatalf("broken reference exported successfully: export=%#v err=%v", export, err)
				}
				if inUse := readDB.Stats().InUse; inUse != 0 {
					t.Fatalf("read connections held after failure=%d, want 0", inUse)
				}
			}
			if _, err := db.Exec("INSERT INTO settings(key,value) VALUES('snapshot-writer-check','ok')"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
