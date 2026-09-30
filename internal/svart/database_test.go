package svart

import (
	"fmt"
	"sync"
	"testing"
)

func TestSetupTestDB(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Verify tables exist by querying them
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM settings").Scan(&count); err != nil {
		t.Fatalf("settings table not accessible: %v", err)
	}
	if count == 0 {
		t.Error("expected default settings to be seeded, got 0 rows")
	}

	// Verify default settings were seeded
	var strategy string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = 'strategy'").Scan(&strategy); err != nil {
		t.Fatalf("failed to read strategy setting: %v", err)
	}
	if strategy != "weighted" {
		t.Errorf("expected default strategy 'weighted', got %q", strategy)
	}

	// Verify default bootstrap server
	var bootstrapServer string
	if err := db.QueryRow("SELECT server FROM bootstrap_servers WHERE id = 1").Scan(&bootstrapServer); err != nil {
		t.Fatalf("failed to read bootstrap server: %v", err)
	}
	if bootstrapServer != "9.9.9.9:53" {
		t.Errorf("expected default bootstrap '9.9.9.9:53', got %q", bootstrapServer)
	}
}

func TestSetupFullTestEnv(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	// Verify upstreams loaded
	upstreams := upstreamStorage.getAll()
	if len(upstreams) != 5 {
		t.Errorf("expected 5 upstreams, got %d", len(upstreams))
	}

	enabledCount := 0
	for _, u := range upstreams {
		if u.Enabled {
			enabledCount++
		}
	}
	if enabledCount != 4 {
		t.Errorf("expected 4 enabled upstreams, got %d", enabledCount)
	}

	// Verify blocklists loaded
	var blocklistCount int
	if fixtureErr63 := db.QueryRow("SELECT COUNT(*) FROM blocklists").Scan(&blocklistCount); fixtureErr63 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr63)
	}
	if blocklistCount != 3 {
		t.Errorf("expected 3 blocklists, got %d", blocklistCount)
	}

	// Verify blocked domains loaded
	var domainCount int
	if fixtureErr70 := db.QueryRow("SELECT COUNT(*) FROM blocked_domains").Scan(&domainCount); fixtureErr70 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr70)
	}
	if domainCount != 23 {
		t.Errorf("expected 23 blocked domains, got %d", domainCount)
	}

	// Verify clients exist
	var clientCount int
	if fixtureErr77 := db.QueryRow("SELECT COUNT(DISTINCT client_ip) FROM query_logs").Scan(&clientCount); fixtureErr77 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr77)
	}
	if clientCount != 6 {
		t.Errorf("expected 6 clients in query logs, got %d", clientCount)
	}

	// Verify groups
	var groupCount int
	if fixtureErr84 := db.QueryRow("SELECT COUNT(*) FROM client_groups").Scan(&groupCount); fixtureErr84 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr84)
	}
	if groupCount != 2 {
		t.Errorf("expected 2 groups, got %d", groupCount)
	}

	// Verify rewrites loaded into cache
	_, hasGrafana := rewritesCache.Load().Load("grafana.example.lan")
	if !hasGrafana {
		t.Error("expected grafana.example.lan in rewrites cache")
	}
	_, hasOld := rewritesCache.Load().Load("old.example.lan")
	if hasOld {
		t.Error("disabled rewrite old.example.lan should not be in cache")
	}

	// Verify query logs
	var logCount int
	if fixtureErr101 := db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&logCount); fixtureErr101 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr101)
	}
	// 6 from seedClientsAndGroups + 18 from seedQueryLogs
	if logCount != 24 {
		t.Errorf("expected 24 query log entries, got %d", logCount)
	}
}

func TestDatabaseCRUDUpstreams(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create
	u, err := upstreamStorage.add("9.9.9.9:53", true)
	if err != nil {
		t.Fatalf("failed to add upstream: %v", err)
	}
	if u.Upstream != "9.9.9.9:53" || !u.Enabled || u.ID == 0 {
		t.Errorf("unexpected upstream after add: %+v", u)
	}

	// Read
	all := upstreamStorage.getAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(all))
	}
	if all[0].Upstream != "9.9.9.9:53" {
		t.Errorf("expected upstream '9.9.9.9:53', got %q", all[0].Upstream)
	}

	// Update
	if err := upstreamStorage.update(u.ID, "tls://dns.quad9.net"); err != nil {
		t.Fatalf("failed to update upstream: %v", err)
	}
	all = upstreamStorage.getAll()
	if all[0].Upstream != "tls://dns.quad9.net" {
		t.Errorf("expected updated upstream, got %q", all[0].Upstream)
	}

	// Toggle
	if err := upstreamStorage.toggle(u.ID); err != nil {
		t.Fatalf("failed to toggle upstream: %v", err)
	}
	all = upstreamStorage.getAll()
	if all[0].Enabled {
		t.Error("expected upstream to be disabled after toggle")
	}

	// Delete
	if err := upstreamStorage.delete(u.ID); err != nil {
		t.Fatalf("failed to delete upstream: %v", err)
	}
	all = upstreamStorage.getAll()
	if len(all) != 0 {
		t.Errorf("expected 0 upstreams after delete, got %d", len(all))
	}
}

func TestGetAllClientsIncludesConfiguredUnseenClients(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, fixtureErr162 := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES (?, ?)", "10.0.0.2", "Alias Only"); fixtureErr162 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr162)
	}

	policyResult, fixtureErr164 := db.Exec("INSERT INTO policies (name) VALUES (?)", "Policy Only")
	if fixtureErr164 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr164)
	}
	policyID, fixtureErr165 := policyResult.LastInsertId()
	if fixtureErr165 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr165)
	}
	if _, fixtureErr166 := db.Exec("INSERT INTO client_policies (client_ip, policy_id) VALUES (?, ?)", "10.0.0.3", policyID); fixtureErr166 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr166)
	}

	blocklistResult, fixtureErr168 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", "", "Blocklist Only", true)
	if fixtureErr168 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr168)
	}
	blocklistID, fixtureErr169 := blocklistResult.LastInsertId()
	if fixtureErr169 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr169)
	}
	if _, fixtureErr170 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.0.0.4", blocklistID); fixtureErr170 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr170)
	}

	allowlistResult, fixtureErr172 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Allowlist Only", true)
	if fixtureErr172 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr172)
	}
	allowlistID, fixtureErr173 := allowlistResult.LastInsertId()
	if fixtureErr173 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr173)
	}
	if _, fixtureErr174 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.0.0.5", allowlistID); fixtureErr174 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr174)
	}

	groupResult, fixtureErr176 := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "Configured Group")
	if fixtureErr176 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr176)
	}
	groupID, fixtureErr177 := groupResult.LastInsertId()
	if fixtureErr177 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr177)
	}
	if _, fixtureErr178 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.0.0.6", groupID); fixtureErr178 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr178)
	}

	if _, fixtureErr180 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.0.0.7", "example.com.", "A", "NOERROR"); fixtureErr180 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr180)
	}

	clients, err := getAllClients()
	if err != nil {
		t.Fatalf("failed to get clients: %v", err)
	}

	byIP := make(map[string]Client, len(clients))
	for _, client := range clients {
		byIP[client.IPAddress] = client
	}

	tests := []struct {
		ip         string
		alias      string
		queryCount int64
	}{
		{ip: "10.0.0.2", alias: "Alias Only", queryCount: 0},
		{ip: "10.0.0.3", alias: "", queryCount: 0},
		{ip: "10.0.0.4", alias: "", queryCount: 0},
		{ip: "10.0.0.5", alias: "", queryCount: 0},
		{ip: "10.0.0.6", alias: "", queryCount: 0},
		{ip: "10.0.0.7", alias: "", queryCount: 1},
	}

	for _, tt := range tests {
		client, ok := byIP[tt.ip]
		if !ok {
			t.Fatalf("expected client %s in list, got %+v", tt.ip, clients)
		}
		if client.Alias != tt.alias {
			t.Errorf("client %s alias = %q, want %q", tt.ip, client.Alias, tt.alias)
		}
		if client.QueryCount != tt.queryCount {
			t.Errorf("client %s query_count = %d, want %d", tt.ip, client.QueryCount, tt.queryCount)
		}
	}
}

func TestDatabaseCRUDRewrites(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create
	id, err := createRewrite("grafana.example.lan", "10.42.1.5", true)
	if err != nil {
		t.Fatalf("failed to create rewrite: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero rewrite ID")
	}

	// Read
	rewrites, err := getRewrites()
	if err != nil {
		t.Fatalf("failed to get rewrites: %v", err)
	}
	if len(rewrites) != 1 {
		t.Fatalf("expected 1 rewrite, got %d", len(rewrites))
	}
	if rewrites[0].Domain != "grafana.example.lan" {
		t.Errorf("expected domain 'grafana.example.lan', got %q", rewrites[0].Domain)
	}

	// Update domain
	if err := updateRewriteDomain(int(id), "prometheus.example.lan"); err != nil {
		t.Fatalf("failed to update rewrite domain: %v", err)
	}
	var fixtureErr249 error
	rewrites, fixtureErr249 = getRewrites()
	if fixtureErr249 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr249)
	}
	if rewrites[0].Domain != "prometheus.example.lan" {
		t.Errorf("expected updated domain, got %q", rewrites[0].Domain)
	}

	// Update IPs
	if err := updateRewriteIPs(int(id), "10.42.1.5 10.42.1.6"); err != nil {
		t.Fatalf("failed to update rewrite IPs: %v", err)
	}
	var fixtureErr258 error
	rewrites, fixtureErr258 = getRewrites()
	if fixtureErr258 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr258)
	}
	if rewrites[0].IPAddresses != "10.42.1.5 10.42.1.6" {
		t.Errorf("expected updated IPs, got %q", rewrites[0].IPAddresses)
	}

	// Toggle disable
	if err := updateRewriteEnabled(int(id), false); err != nil {
		t.Fatalf("failed to disable rewrite: %v", err)
	}
	var fixtureErr267 error
	rewrites, fixtureErr267 = getRewrites()
	if fixtureErr267 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr267)
	}
	if rewrites[0].Enabled {
		t.Error("expected rewrite to be disabled")
	}

	// Delete
	if err := deleteRewrite(int(id)); err != nil {
		t.Fatalf("failed to delete rewrite: %v", err)
	}
	var fixtureErr276 error
	rewrites, fixtureErr276 = getRewrites()
	if fixtureErr276 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr276)
	}
	if len(rewrites) != 0 {
		t.Errorf("expected 0 rewrites after delete, got %d", len(rewrites))
	}
}

func TestDatabaseCRUDGroups(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create group
	groupID, err := createGroup("Sam")
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// Read group name
	name, err := getGroupName(int(groupID))
	if err != nil {
		t.Fatalf("failed to get group name: %v", err)
	}
	if name != "Sam" {
		t.Errorf("expected group name 'Sam', got %q", name)
	}

	// Update group name
	if err := updateGroupName(int(groupID), "Sam Devices"); err != nil {
		t.Fatalf("failed to update group name: %v", err)
	}
	var fixtureErr305 error
	name, fixtureErr305 = getGroupName(int(groupID))
	if fixtureErr305 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr305)
	}
	if name != "Sam Devices" {
		t.Errorf("expected 'Sam Devices', got %q", name)
	}

	// Add member
	if err := addClientToGroup("10.42.1.43", fmt.Sprintf("%d", groupID)); err != nil {
		t.Fatalf("failed to add client to group: %v", err)
	}
	members, err := getGroupMembers(int(groupID))
	if err != nil {
		t.Fatalf("failed to get group members: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("expected 1 member, got %d", len(members))
	}

	// Remove member
	if err := removeClientFromGroup("10.42.1.43", fmt.Sprintf("%d", groupID)); err != nil {
		t.Fatalf("failed to remove client from group: %v", err)
	}
	var fixtureErr326 error
	members, fixtureErr326 = getGroupMembers(int(groupID))
	if fixtureErr326 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr326)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members after remove, got %d", len(members))
	}

	// Delete group
	if err := deleteGroup(int(groupID)); err != nil {
		t.Fatalf("failed to delete group: %v", err)
	}
	_, err = getGroupName(int(groupID))
	if err == nil {
		t.Error("expected error getting deleted group name")
	}
}

func TestDatabaseCRUDClientAlias(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Set alias
	if err := setClientAlias("10.42.1.42", "Chris Macbook"); err != nil {
		t.Fatalf("failed to set alias: %v", err)
	}

	// Read alias
	var alias string
	if fixtureErr352 := db.QueryRow("SELECT alias FROM client_aliases WHERE ip_address = '10.42.1.42'").Scan(&alias); fixtureErr352 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr352)
	}
	if alias != "Chris Macbook" {
		t.Errorf("expected alias 'Chris Macbook', got %q", alias)
	}

	// Update alias
	if err := setClientAlias("10.42.1.42", "Chris Work Laptop"); err != nil {
		t.Fatalf("failed to update alias: %v", err)
	}
	if fixtureErr361 := db.QueryRow("SELECT alias FROM client_aliases WHERE ip_address = '10.42.1.42'").Scan(&alias); fixtureErr361 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr361)
	}
	if alias != "Chris Work Laptop" {
		t.Errorf("expected updated alias, got %q", alias)
	}

	// Delete alias (empty string)
	if err := setClientAlias("10.42.1.42", ""); err != nil {
		t.Fatalf("failed to delete alias: %v", err)
	}
	var count int
	if fixtureErr371 := db.QueryRow("SELECT COUNT(*) FROM client_aliases WHERE ip_address = '10.42.1.42'").Scan(&count); fixtureErr371 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr371)
	}
	if count != 0 {
		t.Error("expected alias to be deleted")
	}
}

func TestDatabaseSettings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	settings, err := readSyncSettings()
	if err != nil {
		t.Fatal(err)
	}
	cacheTTL, bootstrapTTL, logRetention := settings["cache_ttl"], settings["bootstrap_ttl"], settings["log_retention_days"]

	if cacheTTL != "3600" {
		t.Errorf("expected cache_ttl '3600', got %q", cacheTTL)
	}
	if bootstrapTTL != "3600" {
		t.Errorf("expected bootstrap_ttl '3600', got %q", bootstrapTTL)
	}
	if logRetention != "1095" {
		t.Errorf("expected log_retention_days '1095', got %q", logRetention)
	}

	// Update a setting
	_, err = db.Exec("UPDATE settings SET value = '14400' WHERE key = 'cache_ttl'")
	if err != nil {
		t.Fatalf("failed to update setting: %v", err)
	}

	if _, err := db.Exec("UPDATE settings SET value = '120' WHERE key = 'bootstrap_ttl'"); err != nil {
		t.Fatalf("failed to update bootstrap_ttl: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'random' WHERE key = 'strategy'"); err != nil {
		t.Fatalf("failed to update strategy: %v", err)
	}

	settings, err = readSyncSettings()
	if err != nil {
		t.Fatal(err)
	}
	cacheTTL = settings["cache_ttl"]
	if cacheTTL != "14400" {
		t.Errorf("expected updated cache_ttl '14400', got %q", cacheTTL)
	}

	if err := loadRuntimeSettingsFromDB(); err != nil {
		t.Fatalf("failed to refresh runtime settings: %v", err)
	}
	if ttl := getCacheTTL(); ttl != 14400 {
		t.Errorf("expected runtime cache_ttl 14400, got %d", ttl)
	}
	if ttl := getBootstrapTTL(); ttl != 120 {
		t.Errorf("expected runtime bootstrap_ttl 120, got %d", ttl)
	}
	if strategy := getResolutionStrategy(); strategy != "random" {
		t.Errorf("expected runtime strategy random, got %q", strategy)
	}
}

func TestGetDeniedTTL(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Default value
	ttl := getDeniedTTL()
	if ttl != 3600 {
		t.Errorf("expected default denied_ttl 3600, got %d", ttl)
	}

	// Update to custom value
	_, err := db.Exec("UPDATE settings SET value = '300' WHERE key = 'denied_ttl'")
	if err != nil {
		t.Fatalf("failed to update denied_ttl: %v", err)
	}
	if err := loadRuntimeSettingsFromDB(); err != nil {
		t.Fatalf("failed to refresh runtime settings: %v", err)
	}

	ttl = getDeniedTTL()
	if ttl != 300 {
		t.Errorf("expected denied_ttl 300, got %d", ttl)
	}

	// Invalid value falls back to default
	if _, fixtureErr458 := db.Exec("UPDATE settings SET value = 'garbage' WHERE key = 'denied_ttl'"); fixtureErr458 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr458)
	}
	if err := loadRuntimeSettingsFromDB(); err != nil {
		t.Fatalf("failed to refresh runtime settings: %v", err)
	}
	ttl = getDeniedTTL()
	if ttl != 3600 {
		t.Errorf("expected fallback denied_ttl 3600 for invalid value, got %d", ttl)
	}

	// Zero falls back to default
	if _, fixtureErr468 := db.Exec("UPDATE settings SET value = '0' WHERE key = 'denied_ttl'"); fixtureErr468 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr468)
	}
	if err := loadRuntimeSettingsFromDB(); err != nil {
		t.Fatalf("failed to refresh runtime settings: %v", err)
	}
	ttl = getDeniedTTL()
	if ttl != 3600 {
		t.Errorf("expected fallback denied_ttl 3600 for zero, got %d", ttl)
	}
}

func TestDatabaseForeignKeyCascade(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Enable foreign keys (SQLite requires this per-connection)
	if _, fixtureErr483 := db.Exec("PRAGMA foreign_keys = ON"); fixtureErr483 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr483)
	}

	// Create a group with a blocklist assignment
	groupID, fixtureErr486 := createGroup("TestGroup")
	if fixtureErr486 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr486)
	}
	if _, fixtureErr487 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/list.txt", "Test List", true); fixtureErr487 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr487)
	}

	var blocklistID int
	if fixtureErr491 := db.QueryRow("SELECT id FROM blocklists WHERE alias = 'Test List'").Scan(&blocklistID); fixtureErr491 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr491)
	}

	if fixtureErr493 := addGroupBlocklist(int(groupID), blocklistID); fixtureErr493 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr493)
	}

	// Verify assignment exists
	var assignCount int
	if fixtureErr497 := db.QueryRow("SELECT COUNT(*) FROM group_blocklists WHERE group_id = ?", groupID).Scan(&assignCount); fixtureErr497 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr497)
	}
	if assignCount != 1 {
		t.Fatalf("expected 1 group_blocklist assignment, got %d", assignCount)
	}

	// Delete the group — should cascade delete group_blocklists
	if fixtureErr503 := deleteGroup(int(groupID)); fixtureErr503 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr503)
	}

	if fixtureErr505 := db.QueryRow("SELECT COUNT(*) FROM group_blocklists WHERE group_id = ?", groupID).Scan(&assignCount); fixtureErr505 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr505)
	}
	if assignCount != 0 {
		t.Errorf("expected group_blocklists to cascade delete, got %d remaining", assignCount)
	}
}

func TestDatabaseQueryLogs(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	// Test getRecentQueries
	queries, err := getRecentQueries(5)
	if err != nil {
		t.Fatalf("failed to get recent queries: %v", err)
	}
	if len(queries) != 5 {
		t.Errorf("expected 5 recent queries, got %d", len(queries))
	}

	// Test getTopDomains
	topDomains, err := getTopDomains(3)
	if err != nil {
		t.Fatalf("failed to get top domains: %v", err)
	}
	if len(topDomains) == 0 {
		t.Error("expected at least 1 top domain")
	}
	// google.com should be near the top (queried multiple times by 10.42.1.42)
	if topDomains[0].Count < 2 {
		t.Errorf("expected top domain to have count >= 2, got %d", topDomains[0].Count)
	}

	// Test getTopClients
	topClients, err := getTopClients(3)
	if err != nil {
		t.Fatalf("failed to get top clients: %v", err)
	}
	if len(topClients) == 0 {
		t.Error("expected at least 1 top client")
	}

	// Test getDashboardStats
	total, blocked, avgLatency, err := getDashboardStats()
	if err != nil {
		t.Fatal(err)
	}
	if total != 24 {
		t.Errorf("expected 24 total queries, got %d", total)
	}
	if blocked < 4 {
		t.Errorf("expected at least 4 blocked queries, got %d", blocked)
	}
	if avgLatency == 0 {
		t.Error("expected non-zero average latency")
	}
}

func TestReadDBConcurrentReads(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Insert a realistic query log entry
	if _, fixtureErr567 := db.Exec(`INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked)
		VALUES ('10.0.0.1', 'example.com', 'A', 'NOERROR', 0)`); fixtureErr567 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr567)
	}

	// Verify concurrent reads all see the data
	var wg sync.WaitGroup
	errors := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var count int
			if err := readDB.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&count); err != nil {
				errors <- err
			} else if count < 1 {
				errors <- fmt.Errorf("expected count >= 1, got %d", count)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Fatalf("concurrent read failed: %v", err)
	}
}

func TestReadDBIsReadOnly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Writes through readDB should fail
	_, err := readDB.Exec(`INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked)
		VALUES ('10.0.0.1', 'should-fail.com', 'A', 'NOERROR', 0)`)
	if err == nil {
		t.Error("expected readDB write to fail (mode=ro), but it succeeded")
	}
}

func TestCoalescedCountColumnExists(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	var exists bool
	if fixtureErr609 := db.QueryRow("SELECT COUNT(*) > 0 FROM pragma_table_info('query_logs') WHERE name='coalesced_count'").Scan(&exists); fixtureErr609 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr609)
	}
	if !exists {
		t.Error("coalesced_count column missing from query_logs")
	}

	// Verify default is 1
	if _, fixtureErr615 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked) VALUES ('10.0.0.1', 'test.example.com', 'A', 'NOERROR', 0)"); fixtureErr615 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr615)
	}
	var val int
	if fixtureErr617 := db.QueryRow("SELECT coalesced_count FROM query_logs ORDER BY rowid DESC LIMIT 1").Scan(&val); fixtureErr617 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr617)
	}
	if val != 1 {
		t.Errorf("expected default coalesced_count=1, got %d", val)
	}
}

func TestSyncedTableNaturalKeysUnique(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Every synced table with a natural key must reject duplicates.
	cases := []struct {
		table   string
		insertA string
		insertB string // duplicate of A
	}{
		{"bootstrap_servers", "INSERT INTO bootstrap_servers (server) VALUES ('1.2.3.4:53')", "INSERT INTO bootstrap_servers (server) VALUES ('1.2.3.4:53')"},
		{"upstreams", "INSERT INTO upstreams (upstream) VALUES ('https://test.example/dns-query')", "INSERT INTO upstreams (upstream) VALUES ('https://test.example/dns-query')"},
		{"blocklists", "INSERT INTO blocklists (alias, url) VALUES ('test-list', 'https://example.com/list.txt')", "INSERT INTO blocklists (alias, url) VALUES ('test-list', 'https://example.com/other.txt')"},
		{"allowlists", "INSERT INTO allowlists (alias, url) VALUES ('test-allow', 'https://example.com/allow.txt')", "INSERT INTO allowlists (alias, url) VALUES ('test-allow', 'https://example.com/other.txt')"},
		{"rewrites", "INSERT INTO rewrites (domain, target) VALUES ('test.local', '10.0.0.1')", "INSERT INTO rewrites (domain, target) VALUES ('test.local', '10.0.0.2')"},
		{"api_tokens", "INSERT INTO api_tokens (name, token_prefix, token_hash, role) VALUES ('test-token', 'sv_abc', 'hash1', 'readonly')", "INSERT INTO api_tokens (name, token_prefix, token_hash, role) VALUES ('test-token', 'sv_def', 'hash2', 'readonly')"},
	}

	for _, tc := range cases {
		_, err := db.Exec(tc.insertA)
		if err != nil {
			t.Fatalf("%s: first insert failed: %v", tc.table, err)
		}
		_, err = db.Exec(tc.insertB)
		if err == nil {
			t.Errorf("%s: expected UNIQUE constraint violation on duplicate natural key, got nil", tc.table)
		}
	}
}
