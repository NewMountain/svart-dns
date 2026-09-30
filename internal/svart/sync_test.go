package svart

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"

	"testing"
	"time"
)

func TestBuildSyncResponse_Empty(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	resp, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	if resp.NodeID != nodeID {
		t.Errorf("node_id = %q, want %q", resp.NodeID, nodeID)
	}
	if resp.ServerTime == "" {
		t.Error("server_time is empty")
	}
}

func TestBuildSyncResponse_ReturnsChangedRows(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"
	before := time.Now().UTC().Add(-1 * time.Second)

	// Insert an upstream with updated_at
	ts, nid := syncNow()
	if _, fixtureErr42 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"9.9.9.9:53", true, ts, nid); fixtureErr42 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr42)
	}

	// Insert a blocklist
	if _, fixtureErr46 := db.Exec("INSERT INTO blocklists (url, alias, enabled, updated_at, node_id) VALUES (?, ?, ?, ?, ?)",
		"https://example.com/list.txt", "test-list", true, ts, nid); fixtureErr46 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr46)
	}

	// Insert a setting (use unique key — log_retention_days already exists from seed)
	if _, fixtureErr50 := db.Exec("INSERT INTO settings (key, value, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"test_setting", "test_value", ts, nid); fixtureErr50 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr50)
	}

	resp, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	if len(resp.Changes.Upstreams) != 1 {
		t.Errorf("upstreams = %d, want 1", len(resp.Changes.Upstreams))
	} else {
		u := resp.Changes.Upstreams[0]
		if u.Upstream != "9.9.9.9:53" || !u.Enabled {
			t.Errorf("upstream = %+v", u)
		}
	}

	if len(resp.Changes.Blocklists) != 1 {
		t.Errorf("blocklists = %d, want 1", len(resp.Changes.Blocklists))
	} else if resp.Changes.Blocklists[0].Alias != "test-list" {
		t.Errorf("blocklist alias = %q", resp.Changes.Blocklists[0].Alias)
	}

	if len(resp.Changes.Settings) != 1 {
		t.Errorf("settings = %d, want 1", len(resp.Changes.Settings))
	}
}

func TestBuildSyncResponseOmitsLocalSecrets(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const updatedAt = "2026-07-10T20:00:00Z"
	for key, value := range map[string]string{
		"sync_secret":    t.Name() + "-sync",
		"session_secret": t.Name() + "-session",
	} {
		if _, err := db.Exec(
			"UPDATE settings SET value = ?, updated_at = ?, node_id = 'local-node' WHERE key = ?",
			value, updatedAt, key,
		); err != nil {
			t.Fatalf("update %s: %v", key, err)
		}
	}

	resp, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}
	for _, setting := range resp.Changes.Settings {
		if isNeverExposeSetting(setting.Key) {
			t.Fatalf("sync response exposes %s", setting.Key)
		}
	}
}

func TestMergeSyncResponseIgnoresLocalSecrets(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const localUpdatedAt = "2000-01-01T00:00:00Z"
	for key, value := range map[string]string{
		"sync_secret":    t.Name() + "-sync",
		"session_secret": t.Name() + "-session",
		"node_name":      "local-node-name",
	} {
		if _, err := db.Exec(
			"UPDATE settings SET value = ?, updated_at = ?, node_id = 'local-node' WHERE key = ?",
			value, localUpdatedAt, key,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: "2000-01-02T00:00:00Z",
		Changes: SyncChanges{Settings: []SyncSetting{
			{Key: "sync_secret", Value: "remote-sync-secret", UpdatedAt: "2000-01-02T00:00:00Z", NodeID: "remote-node"},
			{Key: "session_secret", Value: "remote-session-secret", UpdatedAt: "2000-01-02T00:00:00Z", NodeID: "remote-node"},
			{Key: "node_name", Value: "remote-node-name", UpdatedAt: "2000-01-02T00:00:00Z", NodeID: "remote-node"},
		}},
	}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	for key, expected := range map[string]string{
		"sync_secret":    t.Name() + "-sync",
		"session_secret": t.Name() + "-session",
		"node_name":      "local-node-name",
	} {
		if got := getSyncSetting(key); got != expected {
			t.Fatalf("%s = %q, want unchanged %q", key, got, expected)
		}
	}
}

func TestMergeSyncResponseIgnoresLocalSettingTombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const localUpdatedAt = "2000-01-01T00:00:00Z"
	protected := map[string]string{
		"sync_secret":    t.Name() + "-sync",
		"session_secret": t.Name() + "-session",
		"node_name":      "local-node-name",
		"sync_peers":     "https://10.42.1.7:443",
		"deleted_peers":  "https://10.42.1.8:443",
	}
	for key, value := range protected {
		if _, err := db.Exec(
			"UPDATE settings SET value = ?, updated_at = ?, node_id = 'local-node' WHERE key = ?",
			value, localUpdatedAt, key,
		); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}

	tombstones := make([]Tombstone, 0, len(protected))
	for key := range protected {
		tombstones = append(tombstones, Tombstone{
			TableName:  "settings",
			NaturalKey: key,
			DeletedAt:  "2000-01-02T00:00:00Z",
			NodeID:     "remote-node",
		})
	}
	if err := mergeSyncResponse(&SyncResponse{Tombstones: tombstones}); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	for key, expected := range protected {
		if got := getSyncSetting(key); got != expected {
			t.Fatalf("%s = %q, want unchanged %q", key, got, expected)
		}
		var count int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM sync_tombstones WHERE table_name = 'settings' AND natural_key = ?",
			key,
		).Scan(&count); err != nil {
			t.Fatalf("count tombstone for %s: %v", key, err)
		}
		if count != 0 {
			t.Fatalf("protected setting %s was recorded as a propagating tombstone", key)
		}
	}
}

func TestSyncAuthRequiresPeerKeyAndRejectsAdminAPIKey(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	adminKey, _, err := createAPIToken("Admin", RoleAdmin)
	if err != nil {
		t.Fatalf("create admin API key: %v", err)
	}

	syncMu.Lock()
	savedSecret := syncSecret
	syncSecret = "peer-only-secret"
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		syncSecret = savedSecret
		syncMu.Unlock()
	}()

	handler := syncAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"success": true})
	})

	adminReq := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	adminReq.TLS = &tls.ConnectionState{}
	adminReq.Header.Set("X-Api-Key", adminKey)
	adminResp := httptest.NewRecorder()
	handler(adminResp, adminReq)
	if adminResp.Code != http.StatusUnauthorized {
		t.Fatalf("admin API key status = %d, want 401", adminResp.Code)
	}

	peerReq := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	peerReq.TLS = &tls.ConnectionState{}
	peerReq.Header.Set("X-Sync-Key", "peer-only-secret")
	peerResp := httptest.NewRecorder()
	handler(peerResp, peerReq)
	if peerResp.Code != http.StatusOK {
		t.Fatalf("peer key status = %d, want 200", peerResp.Code)
	}
}

func TestSyncAuthRejectsPeerKeyOverPlainHTTP(t *testing.T) {
	syncMu.Lock()
	savedSecret := syncSecret
	syncSecret = "0123456789abcdef0123456789abcdef"
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		syncSecret = savedSecret
		syncMu.Unlock()
	}()

	called := false
	handler := syncAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/sync", nil)
	req.Header.Set("X-Sync-Key", "0123456789abcdef0123456789abcdef")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426", w.Code)
	}
	if called {
		t.Fatal("plaintext sync request reached protected handler")
	}
}

func TestBuildSyncResponse_SinceFilters(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"

	// Insert an upstream with a known timestamp
	oldTime := "2020-01-01T00:00:00Z"
	if _, fixtureErr278 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"1.1.1.1:53", true, oldTime, "old-node"); fixtureErr278 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr278)
	}

	newTime := time.Now().UTC().Format(time.RFC3339Nano)
	if _, fixtureErr282 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"9.9.9.9:53", true, newTime, "new-node"); fixtureErr282 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr282)
	}

	// Query since after oldTime but before newTime
	since, fixtureErr286 := time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	if fixtureErr286 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr286)
	}
	resp, err := buildSyncResponse(since)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	if len(resp.Changes.Upstreams) != 1 {
		t.Errorf("expected 1 upstream, got %d", len(resp.Changes.Upstreams))
	} else if resp.Changes.Upstreams[0].Upstream != "9.9.9.9:53" {
		t.Errorf("wrong upstream: %s", resp.Changes.Upstreams[0].Upstream)
	}
}

func TestBuildSyncResponse_Tombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"
	before := time.Now().UTC().Add(-1 * time.Second)

	if fixtureErr306 := recordTombstone("upstreams", "9.9.9.9:53"); fixtureErr306 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr306)
	}
	if fixtureErr307 := recordTombstone("blocklists", "test-list"); fixtureErr307 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr307)
	}

	resp, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	if len(resp.Tombstones) != 2 {
		t.Fatalf("tombstones = %d, want 2", len(resp.Tombstones))
	}

	found := make(map[string]bool)
	for _, ts := range resp.Tombstones {
		found[ts.TableName+":"+ts.NaturalKey] = true
	}
	if !found["upstreams:9.9.9.9:53"] {
		t.Error("missing upstreams tombstone")
	}
	if !found["blocklists:test-list"] {
		t.Error("missing blocklists tombstone")
	}
}

func TestBuildSyncResponseOmitsNodeLocalSettingTombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const (
		deletedAt  = "2026-07-10T20:00:00Z"
		sourceNode = "source-node"
	)
	for _, tombstone := range []Tombstone{
		{TableName: "settings", NaturalKey: "sync_secret", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "settings", NaturalKey: "session_secret", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "settings", NaturalKey: "node_name", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "settings", NaturalKey: "sync_peers", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "settings", NaturalKey: "deleted_peers", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "settings", NaturalKey: "sync_interval", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "upstreams", NaturalKey: "9.9.9.9:53", DeletedAt: deletedAt, NodeID: sourceNode},
	} {
		if _, err := db.Exec(
			"INSERT INTO sync_tombstones (table_name, natural_key, deleted_at, node_id) VALUES (?, ?, ?, ?)",
			tombstone.TableName, tombstone.NaturalKey, tombstone.DeletedAt, tombstone.NodeID,
		); err != nil {
			t.Fatalf("insert tombstone %s:%s: %v", tombstone.TableName, tombstone.NaturalKey, err)
		}
	}

	resp, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}
	sort.Slice(resp.Tombstones, func(i, j int) bool {
		left := resp.Tombstones[i].TableName + ":" + resp.Tombstones[i].NaturalKey
		right := resp.Tombstones[j].TableName + ":" + resp.Tombstones[j].NaturalKey
		return left < right
	})
	want := []Tombstone{
		{TableName: "settings", NaturalKey: "sync_interval", DeletedAt: deletedAt, NodeID: sourceNode},
		{TableName: "upstreams", NaturalKey: "9.9.9.9:53", DeletedAt: deletedAt, NodeID: sourceNode},
	}
	if !reflect.DeepEqual(resp.Tombstones, want) {
		t.Fatalf("outbound tombstones = %#v, want %#v", resp.Tombstones, want)
	}
}

func TestBuildSyncResponse_JunctionNaturalKeys(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"
	before := time.Now().UTC().Add(-1 * time.Second)
	ts, nid := syncNow()

	// Set up parent entities
	if _, fixtureErr382 := db.Exec("INSERT INTO client_groups (name, updated_at, node_id) VALUES (?, ?, ?)", "Sam", ts, nid); fixtureErr382 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr382)
	}
	if _, fixtureErr383 := db.Exec("INSERT INTO blocklists (url, alias, enabled, updated_at, node_id) VALUES (?, ?, ?, ?, ?)",
		"https://example.com/list.txt", "Hagezi Pro", true, ts, nid); fixtureErr383 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr383)
	}

	// Get IDs
	var groupID, blID int
	if fixtureErr388 := db.QueryRow("SELECT id FROM client_groups WHERE name = 'Sam'").Scan(&groupID); fixtureErr388 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr388)
	}
	if fixtureErr389 := db.QueryRow("SELECT id FROM blocklists WHERE alias = 'Hagezi Pro'").Scan(&blID); fixtureErr389 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr389)
	}

	// Insert junction rows
	if _, fixtureErr392 := db.Exec("INSERT INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"10.42.1.43", groupID, ts, nid); fixtureErr392 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr392)
	}
	if _, fixtureErr394 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)",
		groupID, blID, ts, nid); fixtureErr394 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr394)
	}

	resp, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	// Check group member uses group name (not ID)
	if len(resp.Changes.GroupMembers) != 1 {
		t.Fatalf("group_members = %d, want 1", len(resp.Changes.GroupMembers))
	}
	gm := resp.Changes.GroupMembers[0]
	if gm.GroupName != "Sam" || gm.ClientIP != "10.42.1.43" {
		t.Errorf("group member = %+v", gm)
	}

	// Check group blocklist uses group name + list alias
	if len(resp.Changes.GroupBlocklists) != 1 {
		t.Fatalf("group_blocklists = %d, want 1", len(resp.Changes.GroupBlocklists))
	}
	gb := resp.Changes.GroupBlocklists[0]
	if gb.GroupName != "Sam" || gb.ListAlias != "Hagezi Pro" {
		t.Errorf("group blocklist = %+v", gb)
	}
}

func TestSyncResponseJSON_Serialization(t *testing.T) {
	resp := &SyncResponse{
		NodeID:     "pve-blue",
		ServerTime: "2026-03-04T12:00:00Z",
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "9.9.9.9:53", Enabled: true, UpdatedAt: "2026-03-04T12:00:00Z", NodeID: "pve-blue"},
			},
		},
		Tombstones: []Tombstone{
			{TableName: "blocklists", NaturalKey: "old-list", DeletedAt: "2026-03-04T12:00:00Z", NodeID: "pve-blue"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SyncResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.NodeID != "pve-blue" {
		t.Errorf("node_id = %q", decoded.NodeID)
	}
	if len(decoded.Changes.Upstreams) != 1 {
		t.Errorf("upstreams = %d", len(decoded.Changes.Upstreams))
	}
	if len(decoded.Tombstones) != 1 {
		t.Errorf("tombstones = %d", len(decoded.Tombstones))
	}
}

func TestMergeSyncResponse_LWW_NewerWins(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"

	// Insert a local upstream with an old timestamp
	if _, fixtureErr463 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"9.9.9.9:53", true, "2026-01-01T00:00:00Z", "local-node"); fixtureErr463 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr463)
	}

	// Merge with a newer change that disables it
	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "9.9.9.9:53", Enabled: false, UpdatedAt: "2026-06-01T00:00:00Z", NodeID: "remote-node"},
			},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Verify the upstream is now disabled
	var enabled bool
	if fixtureErr483 := db.QueryRow("SELECT enabled FROM upstreams WHERE upstream = '9.9.9.9:53'").Scan(&enabled); fixtureErr483 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr483)
	}
	if enabled {
		t.Error("upstream should be disabled after newer merge")
	}
}

func TestMergeSyncResponse_LWW_OlderIgnored(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"

	// Insert a local upstream with a newer timestamp
	if _, fixtureErr496 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"9.9.9.9:53", true, "2026-06-01T00:00:00Z", "local-node"); fixtureErr496 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr496)
	}

	// Merge with an older change
	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "9.9.9.9:53", Enabled: false, UpdatedAt: "2026-01-01T00:00:00Z", NodeID: "remote-node"},
			},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Verify the upstream is still enabled (local is newer)
	var enabled bool
	if fixtureErr516 := db.QueryRow("SELECT enabled FROM upstreams WHERE upstream = '9.9.9.9:53'").Scan(&enabled); fixtureErr516 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr516)
	}
	if !enabled {
		t.Error("upstream should still be enabled — local version is newer")
	}
}

func TestBuildSyncResponse_APITokensAreHashOnly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t)

	before := time.Now().UTC().Add(-1 * time.Second)
	plaintext, token, err := createAPIToken("Sync Token", RoleReadonly)
	if err != nil {
		t.Fatalf("createAPIToken failed: %v", err)
	}

	var tokenHash string
	if err := db.QueryRow("SELECT token_hash FROM api_tokens WHERE id = ?", token.ID).Scan(&tokenHash); err != nil {
		t.Fatalf("query token hash: %v", err)
	}

	resp, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}

	if len(resp.Changes.APITokens) != 1 {
		t.Fatalf("api_tokens = %d, want 1", len(resp.Changes.APITokens))
	}
	if resp.Changes.APITokens[0].TokenPrefix != plaintext[:11] {
		t.Fatalf("token_prefix = %q, want %q", resp.Changes.APITokens[0].TokenPrefix, plaintext[:11])
	}
	if resp.Changes.APITokens[0].TokenHash != tokenHash {
		t.Fatal("expected sync response to include token hash")
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal sync response: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal sync response: %v", err)
	}

	changes, fixtureErr563 := decoded["changes"].(map[string]interface{})
	if !fixtureErr563 {
		t.Fatalf("unexpected fixture value type: %T", decoded["changes"])
	}
	apiTokens, fixtureErr564 := changes["api_tokens"].([]interface{})
	if !fixtureErr564 {
		t.Fatalf("unexpected fixture value type: %T", changes["api_tokens"])
	}
	first, fixtureErr565 := apiTokens[0].(map[string]interface{})
	if !fixtureErr565 {
		t.Fatalf("unexpected fixture value type: %T", apiTokens[0])
	}
	if _, ok := first["token"]; ok {
		t.Fatalf("expected sync payload to omit plaintext token, got %v", first["token"])
	}
}

func TestMergeSyncResponse_APITokensIgnoreLegacyPlaintextField(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t)

	plaintext, token, err := createAPIToken("Remote Token", RoleReadonly)
	if err != nil {
		t.Fatalf("createAPIToken failed: %v", err)
	}

	var tokenHash string
	if err := db.QueryRow("SELECT token_hash FROM api_tokens WHERE id = ?", token.ID).Scan(&tokenHash); err != nil {
		t.Fatalf("query token hash: %v", err)
	}

	if _, err := db.Exec("DELETE FROM api_tokens"); err != nil {
		t.Fatalf("delete local token: %v", err)
	}
	hasTokens.Store(false)

	payload, err := json.Marshal(map[string]interface{}{
		"node_id":     "remote-node",
		"server_time": time.Now().UTC().Format(time.RFC3339Nano),
		"changes": map[string]interface{}{
			"api_tokens": []map[string]interface{}{
				{
					"token_prefix": token.TokenPrefix,
					"name":         token.Name,
					"token_hash":   tokenHash,
					"token":        plaintext,
					"role":         token.Role,
					"created_at":   token.CreatedAt,
					"last_used_at": "",
					"updated_at":   time.Now().UTC().Format(time.RFC3339Nano),
					"node_id":      "remote-node",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var resp SyncResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if err := mergeSyncResponse(&resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	var storedToken string
	if err := db.QueryRow("SELECT COALESCE(token, '') FROM api_tokens WHERE token_prefix = ?", token.TokenPrefix).Scan(&storedToken); err != nil {
		t.Fatalf("query merged token: %v", err)
	}
	if storedToken != "" {
		t.Fatalf("expected merge to ignore plaintext token field, got %q", storedToken)
	}

	validated, validationErr := validateAPIKey(context.Background(), plaintext)
	if validationErr != nil {
		t.Fatalf("validate fixture API key: %v", validationErr)
	}
	if validated == nil {
		t.Fatal("expected merged token hash to validate plaintext token")
	}
}
