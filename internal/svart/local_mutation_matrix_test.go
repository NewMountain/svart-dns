package svart

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Capture every persistent table, including complete rows and the autoincrement
// sequence. Failed transactions must preserve the complete state, not just counts.
func localStoredState(t *testing.T) map[string][][]any {
	t.Helper()
	tables := []string{}
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, rows)
	state := map[string][][]any{}
	for _, name := range tables {
		rows, err := db.Query(`SELECT * FROM "` + name + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		records := [][]any{}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			records = append(records, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		checkTestClose(t, rows)
		state[name] = records
	}
	return state
}

func localSQL(t *testing.T, query string, args ...any) sql.Result {
	t.Helper()
	result, err := db.Exec(query, args...)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func localHTTP(handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rr
}

func TestLocalMutationFaultMatrix(t *testing.T) {
	for _, family := range []string{"group_delete", "group_rename", "list_delete", "range_delete", "policy_delete", "manual_insert", "manual_delete", "rewrite_update", "alias_delete", "user_delete", "token_delete"} {
		for _, fault := range []string{"ABORT", "IGNORE", "COMMIT"} {
			t.Run(family+"/"+fault, func(t *testing.T) {
				defer setupTestDB(t)()
				localSQL(t, `INSERT INTO policies(id,name) VALUES(1,'Family bundle'); INSERT INTO client_groups(id,name,policy_id) VALUES(1,'Family',1); INSERT INTO ip_ranges(id,name,cidr,policy_id) VALUES(1,'LAN','192.0.2.0/24',1); INSERT INTO blocklists(id,alias,url,enabled,domain_count) VALUES(1,'Family rules','',1,1); INSERT INTO blocked_domains(blocklist_id,domain) VALUES(1,'ads.example.com'); INSERT INTO client_group_members(client_ip,group_id) VALUES('192.0.2.42',1); INSERT INTO group_blocklists(group_id,blocklist_id) VALUES(1,1); INSERT INTO policy_blocklists(policy_id,blocklist_id) VALUES(1,1); INSERT INTO range_blocklists(range_id,blocklist_id) VALUES(1,1); INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES('192.0.2.42',1); INSERT INTO client_aliases(ip_address,alias) VALUES('192.0.2.42','Family laptop'); INSERT INTO rewrites(id,domain,target,ip_addresses,enabled) VALUES(1,'home.example.com','','192.0.2.42',1); INSERT INTO admin_users(id,username,password_hash,role) VALUES(1,'operator','unused','admin'),(2,'observer','unused','readonly'); INSERT INTO api_tokens(id,name,token_prefix,token_hash,token,role) VALUES(1,'automation','sv_12345678','unused','','readonly')`)
				mustReloadPolicy(t)
				if err := loadClientAliasCache(); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
				if err := loadRewritesFromDB(); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
				if _, _, err := refreshAuthCaches(); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
				table, event := "sync_tombstones", "INSERT"
				var mutate func() error
				switch family {
				case "group_delete":
					mutate = func() error { return deleteGroup(1) }
				case "group_rename":
					table, event = "group_blocklists", "UPDATE"
					mutate = func() error { return updateGroupName(1, "Household") }
				case "list_delete":
					table, event = "blocklists", "DELETE"
					mutate = func() error {
						rr := localHTTP(handleAPIBlocklistAction, "DELETE", "/api/blocklists/1", "")
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "range_delete":
					mutate = func() error {
						rr := localHTTP(handleAPIRangeAction, "DELETE", "/api/ranges/1", "")
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "policy_delete":
					mutate = func() error {
						rr := localHTTP(handleAPIPolicyAction, "DELETE", "/api/policies/1", "")
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "manual_insert":
					table, event = "blocked_domains", "INSERT"
					mutate = func() error {
						rr := localHTTP(handleAPIClient, "POST", "/api/clients/192.0.2.43/block-domain", `{"domain":"tracking.example.com"}`)
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "manual_delete":
					mutate = func() error {
						rr := localHTTP(handleAPIClient, "DELETE", "/api/clients/192.0.2.42/block-domain/ads.example.com", "")
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "rewrite_update":
					table, event = "rewrites", "UPDATE"
					mutate = func() error {
						rr := localHTTP(handleAPIRewriteAction, "PUT", "/api/rewrites/1", `{"domain":"printer.example.com","ip_addresses":"192.0.2.43","enabled":false}`)
						if rr.Code < 400 {
							return nil
						}
						return fmt.Errorf("%s", rr.Body.String())
					}
				case "alias_delete":
					mutate = func() error { return setClientAlias("192.0.2.42", "") }
				case "user_delete":
					mutate = func() error { return deleteAdminUser(2) }
				case "token_delete":
					mutate = func() error { return revokeAPIToken(1) }
				}
				body := "SELECT RAISE(IGNORE);"
				if fault == "ABORT" {
					body = "SELECT RAISE(ABORT,'injected local failure');"
				}
				if fault == "COMMIT" {
					localSQL(t, "CREATE TABLE local_commit_failure(parent INTEGER REFERENCES policies(id) DEFERRABLE INITIALLY DEFERRED)")
					body = "INSERT INTO local_commit_failure(parent) VALUES(-1);"
				}
				localSQL(t, fmt.Sprintf("CREATE TRIGGER reject_local BEFORE %s ON %s BEGIN %s END", event, table, body))
				before := localStoredState(t)
				policy, rewrites, aliases := policyState.Load(), rewritesCache.Load(), clientAliasCache.Load()
				users, tokens := hasUsers.Load(), hasTokens.Load()
				if err := mutate(); err == nil {
					t.Fatal("mutation reported success despite injected failure")
				}
				if after := localStoredState(t); !reflect.DeepEqual(before, after) {
					t.Fatal("failed mutation changed complete persistent state")
				}
				if policyState.Load() != policy || rewritesCache.Load() != rewrites || !reflect.DeepEqual(clientAliasCache.Load(), aliases) || hasUsers.Load() != users || hasTokens.Load() != tokens {
					t.Fatal("failed mutation changed runtime state")
				}
				localSQL(t, "DROP TRIGGER reject_local")
				if err := mutate(); err != nil {
					t.Fatalf("good retry failed: %v", err)
				}
			})
		}
	}
}

func TestLocalParentIterationFailure(t *testing.T) {
	defer setupTestDB(t)()
	localSQL(t, `INSERT INTO client_groups(id,name) VALUES(1,'Family'); INSERT INTO client_group_members(client_ip,group_id) VALUES('192.0.2.42',1); ALTER TABLE client_group_members RENAME TO original_members; CREATE VIEW client_group_members AS SELECT * FROM original_members UNION ALL SELECT json_extract('invalid','$'),1,'2026-01-01T00:00:00Z',''`)
	before := localStoredState(t)
	if err := deleteGroup(1); err == nil {
		t.Fatal("iteration failure accepted")
	}
	if after := localStoredState(t); !reflect.DeepEqual(before, after) {
		t.Fatal("iteration failure changed persistent state")
	}
}

func TestLocalBootstrapAuthReadFailure(t *testing.T) {
	for _, table := range []string{"admin_users", "api_tokens"} {
		t.Run(table, func(t *testing.T) {
			defer setupTestDB(t)()
			firstRun.mu.Lock()
			oldActive, oldHash := firstRun.active, firstRun.tokenHash
			firstRun.active = false
			firstRun.mu.Unlock()
			defer func() {
				firstRun.mu.Lock()
				firstRun.active, firstRun.tokenHash = oldActive, oldHash
				firstRun.mu.Unlock()
			}()
			localSQL(t, "DROP TABLE "+table)
			if err := bootstrapAuth(); err == nil {
				t.Fatal("bootstrap accepted unreadable administrator state")
			}
			firstRun.mu.Lock()
			defer firstRun.mu.Unlock()
			if firstRun.active {
				t.Fatal("read failure enabled first-run setup")
			}
		})
	}
}

func TestLocalSyncSettingsReadFailure(t *testing.T) {
	defer setupTestDB(t)()
	localSQL(t, `ALTER TABLE settings RENAME TO original_settings; CREATE VIEW settings AS SELECT key,value FROM original_settings UNION ALL SELECT 'sync_peers',json_extract('invalid','$')`)
	rr := localHTTP(handleAPIPeersGet, "GET", "/api/peers", "")
	if rr.Code != 503 || !strings.Contains(rr.Body.String(), `"error_code":"unavailable"`) {
		t.Fatalf("read failure=%d %s", rr.Code, rr.Body.String())
	}
}

func TestLocalRenamePeerReplay(t *testing.T) {
	for _, fixture := range []struct {
		name, path, body string
		handler          http.HandlerFunc
	}{
		{"group", "/api/groups/1", `{"name":"Household"}`, handleAPIGroupAction},
		{"blocklist", "/api/blocklists/1", `{"alias":"Household blocks"}`, handleAPIBlocklistAction},
		{"allowlist", "/api/allowlists/1", `{"alias":"Household exceptions"}`, handleAPIAllowlistAction},
		{"policy", "/api/policies/1", `{"name":"Household bundle"}`, handleAPIPolicyAction},
		{"range", "/api/ranges/1", `{"cidr":"192.0.2.0/25"}`, handleAPIRangeAction},
		{"rewrite", "/api/rewrites/1", `{"domain":"printer.example.com"}`, handleAPIRewriteAction},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			cleanup := setupTestDB(t)
			localSQL(t, `INSERT INTO policies(id,name,updated_at) VALUES(1,'Family bundle','2021-01-01T00:00:00Z'); INSERT INTO client_groups(id,name,policy_id,updated_at) VALUES(1,'Family',1,'2021-01-01T00:00:00Z'); INSERT INTO ip_ranges(id,name,cidr,policy_id,updated_at) VALUES(1,'LAN','192.0.2.0/24',1,'2021-01-01T00:00:00Z'); INSERT INTO blocklists(id,alias,url,enabled,domain_count,updated_at) VALUES(1,'Family blocks','',1,1,'2021-01-01T00:00:00Z'); INSERT INTO allowlists(id,alias,url,enabled,domain_count,updated_at) VALUES(1,'Family exceptions','',1,1,'2021-01-01T00:00:00Z'); INSERT INTO blocked_domains(blocklist_id,domain,updated_at) VALUES(1,'ads.example.com','2021-01-01T00:00:00Z'); INSERT INTO allowed_domains(allowlist_id,domain,updated_at) VALUES(1,'docs.example.com','2021-01-01T00:00:00Z'); INSERT INTO client_group_members(client_ip,group_id,updated_at) VALUES('192.0.2.42',1,'2021-01-01T00:00:00Z'); INSERT INTO group_blocklists(group_id,blocklist_id,updated_at) VALUES(1,1,'2021-01-01T00:00:00Z'); INSERT INTO policy_blocklists(policy_id,blocklist_id,updated_at) VALUES(1,1,'2021-01-01T00:00:00Z'); INSERT INTO range_blocklists(range_id,blocklist_id,updated_at) VALUES(1,1,'2021-01-01T00:00:00Z'); INSERT INTO client_blocklists(client_ip,blocklist_id,updated_at) VALUES('192.0.2.42',1,'2021-01-01T00:00:00Z'); INSERT INTO client_policies(client_ip,policy_id,updated_at) VALUES('192.0.2.42',1,'2021-01-01T00:00:00Z'); INSERT INTO rewrites(id,domain,target,ip_addresses,enabled,updated_at) VALUES(1,'home.example.com','','192.0.2.42',1,'2021-01-01T00:00:00Z')`)
			for _, prefix := range []string{"client", "group", "range", "policy"} {
				column, value := "client_ip", "'192.0.2.42'"
				if prefix != "client" {
					column, value = prefix+"_id", "1"
				}
				localSQL(t, "INSERT INTO "+prefix+"_allowlists("+column+",allowlist_id,updated_at) VALUES("+value+",1,'2021-01-01T00:00:00Z')")
			}
			mustReloadPolicy(t)
			stale, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			rr := localHTTP(fixture.handler, "PUT", fixture.path, fixture.body)
			if rr.Code != 200 {
				t.Fatalf("rename: %d %s", rr.Code, rr.Body.String())
			}
			renamed, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			cleanup()
			defer setupTestDB(t)()
			for _, response := range []*SyncResponse{stale, renamed, stale} {
				if err := mergeSyncResponse(response); err != nil {
					t.Fatal(err)
				}
			}
			actual, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			// These snapshots use natural identities; local row IDs are intentionally absent.
			renamed.Changes.Settings = nil
			actual.Changes.Settings = nil
			if !reflect.DeepEqual(actual.Changes, renamed.Changes) {
				expectedJSON, fixtureErr12731 := json.MarshalIndent(renamed.Changes, "", "  ")
				if fixtureErr12731 != nil {
					t.Errorf("fixture operation failed: %v", fixtureErr12731)
				}
				actualJSON, fixtureErr12800 := json.MarshalIndent(actual.Changes, "", "  ")
				if fixtureErr12800 != nil {
					t.Errorf("fixture operation failed: %v", fixtureErr12800)
				}
				t.Fatalf("peer differs after rename/stale replay\nwant %s\ngot %s", expectedJSON, actualJSON)
			}
		})
	}
}

func TestLocalManualDeleteMultipleLists(t *testing.T) {
	defer setupTestDB(t)()
	localSQL(t, `INSERT INTO blocklists(id,alias,url,enabled,domain_count) VALUES(1,'Shared custom rules','',1,1),(2,'Laptop custom rules','',1,1); INSERT INTO client_blocklists(client_ip,blocklist_id) VALUES('192.0.2.42',1),('192.0.2.42',2); INSERT INTO blocked_domains(blocklist_id,domain) VALUES(1,'ads.example.com'),(2,'tracking.example.com')`)
	mustReloadPolicy(t)
	rr := localHTTP(handleAPIClient, "DELETE", "/api/clients/192.0.2.42/block-domain/tracking.example.com", "")
	if rr.Code != 200 {
		t.Fatalf("delete %d %s", rr.Code, rr.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE domain='tracking.example.com'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("successful deletion left displayed domain in another assigned manual list")
	}
	if isBlockedForClient("192.0.2.42", "tracking.example.com") {
		t.Fatal("deleted custom domain still blocks")
	}
	if !isBlockedForClient("192.0.2.42", "ads.example.com") {
		t.Fatal("unrelated manual list changed")
	}
}

func TestLocalPolicySnapshotReadFailureRollsBackMutation(t *testing.T) {
	for _, fault := range []string{"query", "scan", "iteration"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			mustReloadPolicy(t)
			beforePolicy := policyState.Load()
			localSQL(t, "ALTER TABLE client_blocklists RENAME TO original_client_blocklists")
			switch fault {
			case "scan":
				localSQL(t, `CREATE VIEW client_blocklists AS SELECT '192.0.2.42' client_ip,'invalid-id' blocklist_id`)
			case "iteration":
				localSQL(t, `CREATE VIEW client_blocklists AS SELECT '192.0.2.42' client_ip,1 blocklist_id UNION ALL SELECT '192.0.2.43',json_extract('invalid','$')`)
			}
			before := localStoredState(t)
			rr := localHTTP(handleAPIGroups, "POST", "/api/groups", `{"name":"Family"}`)
			if rr.Code != 503 {
				t.Fatalf("fault %s status=%d body=%s", fault, rr.Code, rr.Body.String())
			}
			if !reflect.DeepEqual(before, localStoredState(t)) || beforePolicy != policyState.Load() {
				t.Fatal("failed runtime snapshot left mutation or replaced active policy")
			}
		})
	}
}

func TestLocalManualDeletionReplay(t *testing.T) {
	for _, action := range []string{"block-domain", "allow-domain"} {
		t.Run(action, func(t *testing.T) {
			cleanup := setupTestDB(t)
			path := "/api/clients/192.0.2.42/" + action
			rr := localHTTP(handleAPIClient, "POST", path, `{"domain":"ads.example.com"}`)
			if rr.Code != 200 {
				t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
			}
			stale, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			rr = localHTTP(handleAPIClient, "DELETE", path+"/ads.example.com", "")
			if rr.Code != 200 {
				t.Fatalf("delete: %d %s", rr.Code, rr.Body.String())
			}
			deleted, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			table := "blocked_domains"
			domains := stale.Changes.BlockedDomains
			if action == "allow-domain" {
				table = "allowed_domains"
				domains = stale.Changes.AllowedDomains
			}
			if len(domains) != 1 {
				t.Fatalf("source domains=%v", domains)
			}
			var stamp string
			for _, tomb := range deleted.Tombstones {
				if tomb.TableName == table {
					stamp = tomb.DeletedAt
				}
			}
			if stamp == "" {
				t.Fatal("missing exact domain tombstone")
			}
			rr = localHTTP(handleAPIClient, "POST", path, `{"domain":"ads.example.com"}`)
			if rr.Code != 200 {
				t.Fatalf("readd: %d %s", rr.Code, rr.Body.String())
			}
			readded, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			cleanup()
			defer setupTestDB(t)()
			for _, snapshot := range []*SyncResponse{stale, deleted, stale} {
				if err := mergeSyncResponse(snapshot); err != nil {
					t.Fatal(err)
				}
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+table, 0)
			domains[0].UpdatedAt = stamp
			equal := &SyncResponse{}
			if action == "block-domain" {
				equal.Changes.BlockedDomains = domains
			} else {
				equal.Changes.AllowedDomains = domains
			}
			if err := mergeSyncResponse(equal); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+table, 0)
			if err := mergeSyncResponse(readded); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+table, 1)
			if err := mergeSyncResponse(deleted); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM "+table, 1)
		})
	}
}

func TestLocalGossipCannotUndoExplicitPeerDeletion(t *testing.T) {
	defer setupTestDB(t)()
	const peer = "https://peer.example.com:443"
	if added, err := addPeer(peer, true); err != nil || !added {
		t.Fatalf("initial add=%t %v", added, err)
	}
	// Gossip selected this candidate before the concurrent DELETE committed.
	if err := removePeerFromSettings(peer); err != nil {
		t.Fatal(err)
	}
	before := localStoredState(t)
	if added, err := addPeer(peer, false); err != nil || added {
		t.Fatalf("automatic add after explicit DELETE=%t %v", added, err)
	}
	if !reflect.DeepEqual(before, localStoredState(t)) {
		t.Fatal("gossip changed explicit deletion state")
	}
	if added, err := addPeer(peer, true); err != nil || !added {
		t.Fatalf("intentional re-add=%t %v", added, err)
	}
	if isDeletedPeer(peer) {
		t.Fatal("explicit re-add retained deletion marker")
	}
}

func TestLocalAuthSessionRevocationAtomic(t *testing.T) {
	for _, fault := range []string{"ABORT", "IGNORE", "COMMIT"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			localSQL(t, `INSERT INTO admin_users(id,username,password_hash,role) VALUES(1,'observer','unused','readonly'); INSERT INTO sessions(id_hash,username,credential_fp,created_at,last_seen_at,expires_at) VALUES('fixture-session','observer','fixture-fingerprint','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2027-01-01T00:00:00Z')`)
			if _, _, err := refreshAuthCaches(); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			body := "SELECT RAISE(IGNORE);"
			if fault == "ABORT" {
				body = "SELECT RAISE(ABORT,'injected session failure');"
			}
			if fault == "COMMIT" {
				localSQL(t, "CREATE TABLE local_commit_failure(parent INTEGER REFERENCES policies(id) DEFERRABLE INITIALLY DEFERRED)")
				body = "INSERT INTO local_commit_failure(parent) VALUES(-1);"
			}
			localSQL(t, "CREATE TRIGGER reject_session BEFORE DELETE ON sessions BEGIN "+body+" END")
			before := localStoredState(t)
			generation := tokenAuthGeneration
			if err := updateAdminUser(1, RoleAdmin, ""); err == nil {
				t.Fatal("credential update accepted failed session revocation")
			}
			if !reflect.DeepEqual(before, localStoredState(t)) || generation != tokenAuthGeneration {
				t.Fatal("failed revocation changed credentials, sessions, or authority generation")
			}
			localSQL(t, "DROP TRIGGER reject_session")
			if err := updateAdminUser(1, RoleAdmin, ""); err != nil {
				t.Fatal(err)
			}
			assertSyncCount(t, "SELECT COUNT(*) FROM sessions", 0)
			assertSyncCount(t, "SELECT COUNT(*) FROM admin_users WHERE role='admin'", 1)
		})
	}
}

func TestLocalAuthMutationCountFailure(t *testing.T) {
	defer setupTestDB(t)()
	localSQL(t, "DROP TABLE api_tokens")
	before := localStoredState(t)
	users, tokens := hasUsers.Load(), hasTokens.Load()
	if _, err := createAdminUser("operator", "fixture-password", RoleAdmin); err == nil {
		t.Fatal("create accepted failed authority snapshot")
	}
	if !reflect.DeepEqual(before, localStoredState(t)) || users != hasUsers.Load() || tokens != hasTokens.Load() {
		t.Fatal("failed authority snapshot changed database or caches")
	}
}

func TestLocalSyncSettingLateReadFailure(t *testing.T) {
	defer setupTestDB(t)()
	localSQL(t, `INSERT INTO settings(key,value) VALUES(NULL,'malformed row')`)
	before := localStoredState(t)
	oldSettings := runtimeSettings.Load()
	syncMu.Lock()
	oldInterval, oldSecret := syncInterval, syncSecret
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		syncInterval, syncSecret = oldInterval, oldSecret
		syncMu.Unlock()
	}()
	rr := localHTTP(handleAPISetting, "PUT", "/api/settings/sync_interval", `{"value":"17s"}`)
	if rr.Code != 503 {
		t.Fatalf("late settings read failure=%d %s", rr.Code, rr.Body.String())
	}
	if !reflect.DeepEqual(before, localStoredState(t)) || oldSettings != runtimeSettings.Load() {
		t.Fatal("late settings read failure changed persisted or runtime state")
	}
	syncMu.Lock()
	unchanged := syncInterval == oldInterval && syncSecret == oldSecret
	syncMu.Unlock()
	if !unchanged {
		t.Fatal("late settings read failure changed active sync state")
	}
	localSQL(t, "DELETE FROM settings WHERE key IS NULL")
	rr = localHTTP(handleAPISetting, "PUT", "/api/settings/sync_interval", `{"value":"17s"}`)
	if rr.Code != 200 {
		t.Fatalf("retry=%d %s", rr.Code, rr.Body.String())
	}
	if getSyncSetting("sync_interval") != "17s" {
		t.Fatal("successful retry was not persisted")
	}
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncInterval != 17*time.Second {
		t.Fatal("successful retry did not activate sync interval")
	}
}

func TestLocalBootstrapAddAtomic(t *testing.T) {
	for _, table := range []string{"bootstrap_servers", "sync_tombstones"} {
		for _, fault := range []string{"ABORT", "IGNORE", "COMMIT"} {
			t.Run(table+"/"+fault, func(t *testing.T) {
				defer setupTestDB(t)()
				const server = "192.0.2.53:53"
				localSQL(t, "INSERT INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) VALUES('bootstrap_servers',?,'2026-01-01T00:00:00Z','old-peer')", server)
				event := "INSERT"
				if table == "sync_tombstones" {
					event = "DELETE"
				}
				body := "SELECT RAISE(IGNORE);"
				if fault == "ABORT" {
					body = "SELECT RAISE(ABORT,'injected bootstrap failure');"
				}
				if fault == "COMMIT" {
					localSQL(t, "CREATE TABLE local_commit_failure(parent INTEGER REFERENCES policies(id) DEFERRABLE INITIALLY DEFERRED)")
					body = "INSERT INTO local_commit_failure(parent) VALUES(-1);"
				}
				localSQL(t, "CREATE TRIGGER reject_bootstrap BEFORE "+event+" ON "+table+" BEGIN "+body+" END")
				invalidateBootstrapCache()
				defer invalidateBootstrapCache()
				entry := bootstrapEntry{ip: "198.51.100.53", expiresAt: clock.Now().Add(time.Hour)}
				bootstrapHosts.Store("dns.example.com", entry)
				bootstrapFlights.Lock()
				generation := bootstrapFlights.generation
				bootstrapFlights.Unlock()
				before := localStoredState(t)
				rr := localHTTP(handleAPIBootstrap, "POST", "/api/bootstrap", `{"server":"192.0.2.53:53"}`)
				if rr.Code != 503 || !reflect.DeepEqual(before, localStoredState(t)) {
					t.Fatalf("failed add=%d %s, unchanged=%t", rr.Code, rr.Body.String(), reflect.DeepEqual(before, localStoredState(t)))
				}
				bootstrapFlights.Lock()
				unchanged := generation == bootstrapFlights.generation
				bootstrapFlights.Unlock()
				cached, ok := bootstrapHosts.Load("dns.example.com")
				if !unchanged || !ok || cached != entry {
					t.Fatal("failed bootstrap add changed active cache")
				}
				localSQL(t, "DROP TRIGGER reject_bootstrap")
				rr = localHTTP(handleAPIBootstrap, "POST", "/api/bootstrap", `{"server":"192.0.2.53:53"}`)
				if rr.Code != 200 {
					t.Fatalf("retry=%d %s", rr.Code, rr.Body.String())
				}
				assertSyncCount(t, "SELECT COUNT(*) FROM bootstrap_servers WHERE server='192.0.2.53:53'", 1)
				assertSyncCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name='bootstrap_servers' AND natural_key='192.0.2.53:53'", 0)
				if _, ok := bootstrapHosts.Load("dns.example.com"); ok {
					t.Fatal("successful bootstrap add retained stale cached answer")
				}
			})
		}
	}
}
