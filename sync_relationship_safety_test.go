package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type syncRelationshipFixture struct {
	table, key string
	changes    func(string) SyncChanges
}

func syncRelationshipFixtures() []syncRelationshipFixture {
	return []syncRelationshipFixture{
		{"client_group_members", "192.0.2.10|Office", func(ts string) SyncChanges {
			return SyncChanges{GroupMembers: []SyncGroupMember{{ClientIP: "192.0.2.10", GroupName: "Office", UpdatedAt: ts}}}
		}},
		{"client_blocklists", "192.0.2.10|Manual block", func(ts string) SyncChanges {
			return SyncChanges{ClientBlocklists: []SyncClientList{{ClientIP: "192.0.2.10", ListAlias: "Manual block", UpdatedAt: ts}}}
		}},
		{"client_allowlists", "192.0.2.10|Manual allow", func(ts string) SyncChanges {
			return SyncChanges{ClientAllowlists: []SyncClientList{{ClientIP: "192.0.2.10", ListAlias: "Manual allow", UpdatedAt: ts}}}
		}},
		{"group_blocklists", "Office|Manual block", func(ts string) SyncChanges {
			return SyncChanges{GroupBlocklists: []SyncGroupList{{GroupName: "Office", ListAlias: "Manual block", UpdatedAt: ts}}}
		}},
		{"group_allowlists", "Office|Manual allow", func(ts string) SyncChanges {
			return SyncChanges{GroupAllowlists: []SyncGroupList{{GroupName: "Office", ListAlias: "Manual allow", UpdatedAt: ts}}}
		}},
		{"range_blocklists", "192.0.2.0/24|Manual block", func(ts string) SyncChanges {
			return SyncChanges{RangeBlocklists: []SyncRangeList{{RangeCIDR: "192.0.2.0/24", ListAlias: "Manual block", UpdatedAt: ts}}}
		}},
		{"range_allowlists", "192.0.2.0/24|Manual allow", func(ts string) SyncChanges {
			return SyncChanges{RangeAllowlists: []SyncRangeList{{RangeCIDR: "192.0.2.0/24", ListAlias: "Manual allow", UpdatedAt: ts}}}
		}},
		{"blocked_domains", "Manual block|ads.example.com", func(ts string) SyncChanges {
			return SyncChanges{BlockedDomains: []SyncManualDomain{{ListAlias: "Manual block", Domain: "ads.example.com", UpdatedAt: ts}}}
		}},
		{"allowed_domains", "Manual allow|mail.example.com", func(ts string) SyncChanges {
			return SyncChanges{AllowedDomains: []SyncManualDomain{{ListAlias: "Manual allow", Domain: "mail.example.com", UpdatedAt: ts}}}
		}},
		{"policy_blocklists", "Family|Manual block", func(ts string) SyncChanges {
			return SyncChanges{PolicyBlocklists: []SyncPolicyList{{PolicyName: "Family", ListAlias: "Manual block", UpdatedAt: ts}}}
		}},
		{"policy_allowlists", "Family|Manual allow", func(ts string) SyncChanges {
			return SyncChanges{PolicyAllowlists: []SyncPolicyList{{PolicyName: "Family", ListAlias: "Manual allow", UpdatedAt: ts}}}
		}},
		{"client_policies", "192.0.2.10", func(ts string) SyncChanges {
			return SyncChanges{ClientPolicies: []SyncClientPolicy{{ClientIP: "192.0.2.10", PolicyName: "Family", UpdatedAt: ts}}}
		}},
	}
}

func seedSyncRelationshipParents(t *testing.T) {
	t.Helper()
	for _, query := range []string{
		"INSERT INTO blocklists(alias,url,updated_at) VALUES('Manual block','','2020-01-01T00:00:00Z')",
		"INSERT INTO allowlists(alias,url,updated_at) VALUES('Manual allow','','2020-01-01T00:00:00Z')",
		"INSERT INTO client_groups(name,updated_at) VALUES('Office','2020-01-01T00:00:00Z')",
		"INSERT INTO ip_ranges(name,cidr,updated_at) VALUES('Office LAN','192.0.2.0/24','2020-01-01T00:00:00Z')",
		"INSERT INTO policies(name,updated_at) VALUES('Family','2020-01-01T00:00:00Z')",
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

func assertSyncCount(t *testing.T, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s = %d, want %d", query, got, want)
	}
}

func TestSyncRelationshipTwoPeerReplay(t *testing.T) {
	for _, f := range syncRelationshipFixtures() {
		t.Run(f.table, func(t *testing.T) {
			// An offline peer exports the still-live relationship, then rejoins a peer
			// that has already deleted it. Both exports come from real SQLite databases.
			cleanup := setupTestDB(t)
			seedSyncRelationshipParents(t)
			if err := mergeSyncResponse(&SyncResponse{Changes: f.changes("2021-01-01T00:00:00Z")}); err != nil {
				t.Fatal(err)
			}
			stale, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			cleanup()
			defer setupTestDB(t)()
			seedSyncRelationshipParents(t)
			deletion := Tombstone{TableName: f.table, NaturalKey: f.key, DeletedAt: "2022-01-01T00:00:00Z", NodeID: "peer-a"}
			if err := mergeSyncResponse(&SyncResponse{Tombstones: []Tombstone{deletion}}); err != nil {
				t.Fatal(err)
			}
			for _, response := range []*SyncResponse{stale, {Changes: f.changes(deletion.DeletedAt)}} {
				if err := mergeSyncResponse(response); err != nil {
					t.Fatal(err)
				}
				assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 0)
			}
			if err := mergeSyncResponse(&SyncResponse{Changes: f.changes("2023-01-01T00:00:00Z")}); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 1)
			if err := mergeSyncResponse(&SyncResponse{Tombstones: []Tombstone{deletion}}); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 1)
			assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='"+f.table+"'", 1)
		})
	}
}

func TestSyncRelationshipSQLFailureRollsBackAndKeepsCursor(t *testing.T) {
	for _, f := range syncRelationshipFixtures() {
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			t.Run(f.table+"/"+operation, func(t *testing.T) {
				defer setupTestDB(t)()
				seedSyncRelationshipParents(t)
				if operation != "INSERT" {
					if err := mergeSyncResponse(&SyncResponse{Changes: f.changes("2021-01-01T00:00:00Z")}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := db.Exec(fmt.Sprintf("CREATE TRIGGER refuse_sync BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'injected relationship failure'); END", operation, f.table)); err != nil {
					t.Fatal(err)
				}
				changes := f.changes("2023-01-01T00:00:00Z")
				response := &SyncResponse{Changes: changes, ServerTime: "2024-01-01T00:00:00Z"}
				if operation == "DELETE" {
					response.Changes = SyncChanges{}
					response.Tombstones = []Tombstone{{TableName: f.table, NaturalKey: f.key, DeletedAt: "2023-01-01T00:00:00Z"}}
				}
				response.Changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
				beforeRuntime := getRuntimeSettings()
				beforePolicy := policyState.Load()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, response) }))
				defer server.Close()
				cursor := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
				peer := &peerState{URL: server.URL, LastSyncAt: cursor}
				syncOnce(server.Client(), peer, "")
				if peer.Healthy || !peer.LastSyncAt.Equal(cursor) || !strings.Contains(peer.LastError, "injected relationship failure") {
					t.Errorf("failed apply: healthy=%v cursor=%s error=%q", peer.Healthy, peer.LastSyncAt, peer.LastError)
				}
				assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='3600'", 1)
				assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='"+f.table+"'", 0)
				want := 1
				if operation == "INSERT" {
					want = 0
				}
				assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, want)
				if operation != "INSERT" {
					assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table+" WHERE updated_at='2021-01-01T00:00:00Z'", 1)
				}
				if getRuntimeSettings() != beforeRuntime || policyState.Load() != beforePolicy {
					t.Error("failed transaction changed runtime settings or policy snapshot")
				}
			})
		}
	}
}

func TestSyncMissingReferencesAreRecoverableFailures(t *testing.T) {
	for _, f := range syncRelationshipFixtures() {
		t.Run(f.table, func(t *testing.T) {
			defer setupTestDB(t)()
			response := &SyncResponse{Changes: f.changes("2021-01-01T00:00:00Z")}
			err := mergeSyncResponse(response)
			if err == nil || !strings.Contains(err.Error(), "missing reference") {
				t.Fatalf("error=%v, want missing reference diagnostic", err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 0)
			seedSyncRelationshipParents(t)
			if err := mergeSyncResponse(response); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 1)
		})
	}
}

func TestSyncPolicyAssignmentsRequireParents(t *testing.T) {
	for _, fixture := range []struct {
		table   string
		changes SyncChanges
	}{
		{"client_groups", SyncChanges{Groups: []SyncGroup{{Name: "Office", PolicyName: "Family", UpdatedAt: "2021-01-01T00:00:00Z"}}}},
		{"ip_ranges", SyncChanges{Ranges: []SyncRange{{Name: "Office LAN", CIDR: "192.0.2.0/24", PolicyName: "Family", UpdatedAt: "2021-01-01T00:00:00Z"}}}},
	} {
		t.Run(fixture.table, func(t *testing.T) {
			defer setupTestDB(t)()
			err := mergeSyncResponse(&SyncResponse{Changes: fixture.changes})
			if err == nil || !strings.Contains(err.Error(), "missing reference") {
				t.Fatalf("error=%v, want missing reference diagnostic", err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+fixture.table, 0)
		})
	}
}

func TestSyncReadFailuresDoNotBecomeMissingRows(t *testing.T) {
	for _, fixture := range []struct {
		name, prepare, want string
		deletion            bool
	}{
		{"relationship table", "DROP TABLE client_blocklists", "read client_blocklists row: no such table", false},
		{"parent table", "DROP TABLE blocklists", "read reference: no such table", false},
		{"parent scan", "ALTER TABLE blocklists RENAME TO saved_blocklists; CREATE VIEW blocklists AS SELECT 'invalid-id' AS id, alias FROM saved_blocklists", "converting driver.Value", false},
		{"deletion query", "DROP TABLE client_blocklists", "read client_blocklists deletion candidates: no such table", true},
		{"deletion scan", "ALTER TABLE client_blocklists RENAME TO saved_relationship; CREATE VIEW client_blocklists AS SELECT 'invalid-id' AS rowid, * FROM saved_relationship", "scan client_blocklists deletion candidate", true},
		{"deletion iteration", "ALTER TABLE client_blocklists RENAME TO saved_relationship; CREATE VIEW client_blocklists AS SELECT rowid, * FROM saved_relationship UNION ALL SELECT abs(-9223372036854775808), client_ip, blocklist_id, updated_at, node_id FROM saved_relationship", "iterate client_blocklists deletion candidates: integer overflow", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			defer setupTestDB(t)()
			seedSyncRelationshipParents(t)
			changes := syncRelationshipFixtures()[1].changes("2021-01-01T00:00:00Z")
			if err := mergeSyncResponse(&SyncResponse{Changes: changes}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(fixture.prepare); err != nil {
				t.Fatal(err)
			}
			response := &SyncResponse{Changes: changes}
			if fixture.deletion {
				response.Changes = SyncChanges{}
				response.Tombstones = []Tombstone{{TableName: "client_blocklists", NaturalKey: "192.0.2.10|Manual block", DeletedAt: "2022-01-01T00:00:00Z"}}
			}
			response.Changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
			err := mergeSyncResponse(response)
			if err == nil || !strings.Contains(err.Error(), fixture.want) {
				t.Fatalf("error=%v, want %s", err, fixture.want)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='3600'", 1)
			assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='client_blocklists'", 0)
		})
	}
}

func TestSyncSimpleDeletionFailureRollsBack(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("INSERT INTO rewrites(domain,target,updated_at) VALUES('printer.example.com','192.0.2.20','2021-01-01T00:00:00Z'); CREATE TRIGGER refuse_delete BEFORE DELETE ON rewrites BEGIN SELECT RAISE(ABORT,'injected deletion failure'); END"); err != nil {
		t.Fatal(err)
	}
	err := mergeSyncResponse(&SyncResponse{Tombstones: []Tombstone{{TableName: "rewrites", NaturalKey: "printer.example.com", DeletedAt: "2022-01-01T00:00:00Z"}}})
	if err == nil || !strings.Contains(err.Error(), "delete rewrites row: injected deletion failure") {
		t.Fatalf("error=%v, want failed deletion", err)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM rewrites WHERE domain='printer.example.com'", 1)
	assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='rewrites'", 0)
}

func TestSyncMalformedCompoundTombstoneIsRejectedIndividually(t *testing.T) {
	defer setupTestDB(t)()
	response := &SyncResponse{Changes: SyncChanges{ClientAliases: []SyncClientAlias{{IPAddress: "192.0.2.10", Alias: "Office desktop", UpdatedAt: "2021-01-01T00:00:00Z"}}}, Tombstones: []Tombstone{{TableName: "client_blocklists", NaturalKey: "missing-separator", DeletedAt: "2022-01-01T00:00:00Z"}}}
	admitted, rejected := admitSyncResponse(response, time.Now(), false)
	if len(admitted.Tombstones) != 0 || len(rejected) != 1 || rejected[0] != (syncRejection{Table: "sync_tombstones", Reason: rejectInvalidRow}) {
		t.Fatalf("admitted=%v rejected=%v, want one invalid-row rejection", admitted.Tombstones, rejected)
	}
	if err := mergeSyncResponse(response); err != nil {
		t.Fatal(err)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_aliases WHERE ip_address='192.0.2.10' AND alias='Office desktop'", 1)
	assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='client_blocklists'", 0)
}

func TestSyncRetainedDeletionDoesNotRequireDeletedParents(t *testing.T) {
	fixtures := syncRelationshipFixtures()
	fixtures = append(fixtures,
		syncRelationshipFixture{"client_groups", "Office", func(ts string) SyncChanges {
			return SyncChanges{Groups: []SyncGroup{{Name: "Office", PolicyName: "Family", UpdatedAt: ts}}}
		}},
		syncRelationshipFixture{"ip_ranges", "192.0.2.0/24", func(ts string) SyncChanges {
			return SyncChanges{Ranges: []SyncRange{{Name: "Office LAN", CIDR: "192.0.2.0/24", PolicyName: "Family", UpdatedAt: ts}}}
		}},
	)
	for _, f := range fixtures {
		t.Run(f.table, func(t *testing.T) {
			defer setupTestDB(t)()
			deletion := Tombstone{TableName: f.table, NaturalKey: f.key, DeletedAt: "2022-01-01T00:00:00Z"}
			if err := mergeSyncResponse(&SyncResponse{Tombstones: []Tombstone{deletion}}); err != nil {
				t.Fatal(err)
			}
			if err := mergeSyncResponse(&SyncResponse{Changes: f.changes("2021-01-01T00:00:00Z")}); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+f.table, 0)
			assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='"+f.table+"'", 1)
		})
	}
}

func TestSyncCommitFailureKeepsRuntimeAndCanReplay(t *testing.T) {
	defer setupTestDB(t)()
	seedSyncRelationshipParents(t)
	if _, err := db.Exec(`CREATE TABLE sync_commit_failure (parent_id INTEGER REFERENCES policies(id) DEFERRABLE INITIALLY DEFERRED);
 CREATE TRIGGER refuse_commit AFTER INSERT ON client_blocklists BEGIN INSERT INTO sync_commit_failure VALUES(-1); END`); err != nil {
		t.Fatal(err)
	}
	changes := syncRelationshipFixtures()[1].changes("2021-01-01T00:00:00Z")
	changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	response := &SyncResponse{Changes: changes, ServerTime: "2024-01-01T00:00:00Z"}
	beforeRuntime := getRuntimeSettings()
	beforePolicy := policyState.Load()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, response) }))
	defer server.Close()
	cursor := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	peer := &peerState{URL: server.URL, LastSyncAt: cursor}
	syncOnce(server.Client(), peer, "")
	if peer.Healthy || !peer.LastSyncAt.Equal(cursor) || !strings.Contains(peer.LastError, "FOREIGN KEY constraint failed") {
		t.Fatalf("commit failure: healthy=%v cursor=%s error=%q", peer.Healthy, peer.LastSyncAt, peer.LastError)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists", 0)
	assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='3600'", 1)
	if getRuntimeSettings() != beforeRuntime || policyState.Load() != beforePolicy {
		t.Fatal("failed commit changed runtime settings or policy snapshot")
	}
	if _, err := db.Exec("DROP TRIGGER refuse_commit"); err != nil {
		t.Fatal(err)
	}
	syncOnce(server.Client(), peer, "")
	if !peer.Healthy || peer.LastSyncAt.Format(time.RFC3339) != "2024-01-01T00:00:00Z" || peer.LastError != "" {
		t.Fatalf("retry: healthy=%v cursor=%s error=%q", peer.Healthy, peer.LastSyncAt, peer.LastError)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists", 1)
	assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='120'", 1)
	if getRuntimeSettings().cacheTTL != 120 {
		t.Fatalf("committed cacheTTL=%d, want120", getRuntimeSettings().cacheTTL)
	}
	if policyState.Load() == beforePolicy {
		t.Fatal("successful commit did not publish a new policy snapshot")
	}
}

func TestSyncIgnoredMutationIsNotSuccessfulApply(t *testing.T) {
	for _, operation := range []string{"INSERT", "UPDATE", "DELETE", "TOMBSTONE"} {
		t.Run(operation, func(t *testing.T) {
			defer setupTestDB(t)()
			seedSyncRelationshipParents(t)
			changes := syncRelationshipFixtures()[1].changes("2021-01-01T00:00:00Z")
			if operation != "INSERT" {
				if err := mergeSyncResponse(&SyncResponse{Changes: changes}); err != nil {
					t.Fatal(err)
				}
			}
			table, statement := "client_blocklists", operation
			if operation == "TOMBSTONE" {
				table, statement = "sync_tombstones", "INSERT"
			}
			if _, err := db.Exec(fmt.Sprintf("CREATE TRIGGER ignore_mutation BEFORE %s ON %s BEGIN SELECT RAISE(IGNORE); END", statement, table)); err != nil {
				t.Fatal(err)
			}
			response := &SyncResponse{Changes: syncRelationshipFixtures()[1].changes("2023-01-01T00:00:00Z")}
			if operation == "DELETE" || operation == "TOMBSTONE" {
				response.Changes = SyncChanges{}
				response.Tombstones = []Tombstone{{TableName: "client_blocklists", NaturalKey: "192.0.2.10|Manual block", DeletedAt: "2023-01-01T00:00:00Z"}}
			}
			response.Changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
			err := mergeSyncResponse(response)
			if err == nil || !strings.Contains(err.Error(), "mutation affected no rows") {
				t.Fatalf("error=%v, want ignored mutation failure", err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='3600'", 1)
			assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='client_blocklists'", 0)
			want := 1
			if operation == "INSERT" {
				want = 0
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists", want)
		})
	}
}
