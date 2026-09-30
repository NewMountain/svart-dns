package svart

import (
	"testing"
	"time"
)

func TestMergeSyncResponse_NewRow(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"

	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "tls://dns.quad9.net", Enabled: true, UpdatedAt: "2026-03-04T12:00:00Z", NodeID: "remote-node"},
			},
			Blocklists: []SyncBlocklist{
				{Alias: "Hagezi Pro", URL: "https://example.com/list.txt", Enabled: true, UpdatedAt: "2026-03-04T12:00:00Z", NodeID: "remote-node"},
			},
			Settings: []SyncSetting{
				{Key: "log_retention_days", Value: "365", UpdatedAt: time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), NodeID: "remote-node"},
			},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Check all rows were inserted
	var count int
	if fixtureErr664 := db.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream = 'tls://dns.quad9.net'").Scan(&count); fixtureErr664 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr664)
	}
	if count != 1 {
		t.Errorf("upstream not inserted, count=%d", count)
	}

	if fixtureErr669 := db.QueryRow("SELECT COUNT(*) FROM blocklists WHERE alias = 'Hagezi Pro'").Scan(&count); fixtureErr669 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr669)
	}
	if count != 1 {
		t.Errorf("blocklist not inserted, count=%d", count)
	}

	var val string
	if fixtureErr675 := db.QueryRow("SELECT value FROM settings WHERE key = 'log_retention_days'").Scan(&val); fixtureErr675 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr675)
	}
	if val != "365" {
		t.Errorf("setting value = %q, want '365'", val)
	}
}

func TestMergeSyncResponse_Tombstone(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"

	// Create a local upstream
	if _, fixtureErr688 := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"9.9.9.9:53", true, "2026-01-01T00:00:00Z", "local-node"); fixtureErr688 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr688)
	}

	// Merge a tombstone for it
	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Tombstones: []Tombstone{
			{TableName: "upstreams", NaturalKey: "9.9.9.9:53", DeletedAt: "2026-03-04T12:00:00Z", NodeID: "remote-node"},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Verify deleted
	var count int
	if fixtureErr706 := db.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream = '9.9.9.9:53'").Scan(&count); fixtureErr706 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr706)
	}
	if count != 0 {
		t.Errorf("upstream should be deleted, count=%d", count)
	}

	// Verify tombstone recorded locally
	if fixtureErr712 := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name = 'upstreams' AND natural_key = '9.9.9.9:53'").Scan(&count); fixtureErr712 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr712)
	}
	if count != 1 {
		t.Errorf("tombstone not recorded, count=%d", count)
	}
}

func TestMergeSyncResponse_JunctionMerge(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"
	ts := "2026-03-04T12:00:00Z"

	// Merge creates parent entities and junction rows in one shot
	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Changes: SyncChanges{
			Groups: []SyncGroup{
				{Name: "Sam", UpdatedAt: ts, NodeID: "remote-node"},
			},
			Blocklists: []SyncBlocklist{
				{Alias: "Hagezi Pro", URL: "https://example.com/list.txt", Enabled: true, UpdatedAt: ts, NodeID: "remote-node"},
			},
			GroupMembers: []SyncGroupMember{
				{GroupName: "Sam", ClientIP: "10.42.1.43", UpdatedAt: ts, NodeID: "remote-node"},
			},
			GroupBlocklists: []SyncGroupList{
				{GroupName: "Sam", ListAlias: "Hagezi Pro", UpdatedAt: ts, NodeID: "remote-node"},
			},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	var count int
	if fixtureErr750 := db.QueryRow("SELECT COUNT(*) FROM client_group_members").Scan(&count); fixtureErr750 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr750)
	}
	if count != 1 {
		t.Errorf("group_members count=%d, want 1", count)
	}

	if fixtureErr755 := db.QueryRow("SELECT COUNT(*) FROM group_blocklists").Scan(&count); fixtureErr755 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr755)
	}
	if count != 1 {
		t.Errorf("group_blocklists count=%d, want 1", count)
	}
}

func TestMergeSyncResponse_Idempotent(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"

	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "9.9.9.9:53", Enabled: true, UpdatedAt: "2026-03-04T12:00:00Z", NodeID: "remote-node"},
			},
		},
	}

	// Merge twice — should be idempotent
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("second merge: %v", err)
	}

	var count int
	if fixtureErr786 := db.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream = '9.9.9.9:53'").Scan(&count); fixtureErr786 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr786)
	}
	if count != 1 {
		t.Errorf("upstream count=%d, want 1 (idempotent)", count)
	}
}

func TestMergeSyncResponse_RefreshesRuntimeSettings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const (
		localUpdatedAt  = "2000-01-01T00:00:00Z"
		remoteUpdatedAt = "2000-01-02T00:00:00Z"
	)

	result, err := db.Exec(`
		UPDATE settings
		SET updated_at = ?, node_id = 'local-node'
		WHERE key IN ('cache_ttl', 'bootstrap_ttl', 'strategy', 'denied_ttl')
	`, localUpdatedAt)
	if err != nil {
		t.Fatalf("set deterministic local timestamps: %v", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		t.Fatalf("count settings with deterministic local timestamps: %v", err)
	} else if rows != 4 {
		t.Fatalf("settings with deterministic local timestamps = %d, want 4", rows)
	}

	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: remoteUpdatedAt,
		Changes: SyncChanges{
			Settings: []SyncSetting{
				{Key: "cache_ttl", Value: "1800", UpdatedAt: remoteUpdatedAt, NodeID: "remote-node"},
				{Key: "bootstrap_ttl", Value: "90", UpdatedAt: remoteUpdatedAt, NodeID: "remote-node"},
				{Key: "strategy", Value: "random", UpdatedAt: remoteUpdatedAt, NodeID: "remote-node"},
				{Key: "denied_ttl", Value: "600", UpdatedAt: remoteUpdatedAt, NodeID: "remote-node"},
			},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if ttl := getCacheTTL(); ttl != 1800 {
		t.Errorf("expected runtime cache_ttl 1800, got %d", ttl)
	}
	if ttl := getBootstrapTTL(); ttl != 90 {
		t.Errorf("expected runtime bootstrap_ttl 90, got %d", ttl)
	}
	if ttl := getDeniedTTL(); ttl != 600 {
		t.Errorf("expected runtime denied_ttl 600, got %d", ttl)
	}
	if strategy := getResolutionStrategy(); strategy != "random" {
		t.Errorf("expected runtime strategy random, got %q", strategy)
	}
}

func TestGCTombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"

	// Insert an old tombstone (>24h ago)
	oldTime := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, fixtureErr854 := db.Exec("INSERT INTO sync_tombstones (table_name, natural_key, deleted_at, node_id) VALUES (?, ?, ?, ?)",
		"upstreams", "old-upstream", oldTime, "test-node"); fixtureErr854 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr854)
	}

	// Insert a recent tombstone (<24h)
	recentTime := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, fixtureErr859 := db.Exec("INSERT INTO sync_tombstones (table_name, natural_key, deleted_at, node_id) VALUES (?, ?, ?, ?)",
		"upstreams", "recent-upstream", recentTime, "test-node"); fixtureErr859 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr859)
	}

	// Clear peer states so GC uses 24h unconditionally
	savedPeerStates := peerStates
	peerStates = nil
	defer func() { peerStates = savedPeerStates }()

	gcTombstones()

	var count int
	if fixtureErr870 := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones").Scan(&count); fixtureErr870 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr870)
	}
	if count != 1 {
		t.Errorf("tombstones after GC = %d, want 1 (only recent)", count)
	}

	var key string
	if fixtureErr876 := db.QueryRow("SELECT natural_key FROM sync_tombstones").Scan(&key); fixtureErr876 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr876)
	}
	if key != "recent-upstream" {
		t.Errorf("remaining tombstone = %q, want 'recent-upstream'", key)
	}
}

func TestRecordTombstone(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"

	if err := recordTombstone("upstreams", "9.9.9.9:53"); err != nil {
		t.Fatalf("recordTombstone: %v", err)
	}

	// Recording again should update (upsert)
	if err := recordTombstone("upstreams", "9.9.9.9:53"); err != nil {
		t.Fatalf("recordTombstone second: %v", err)
	}

	var count int
	if fixtureErr898 := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name = 'upstreams' AND natural_key = '9.9.9.9:53'").Scan(&count); fixtureErr898 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr898)
	}
	if count != 1 {
		t.Errorf("tombstone count = %d, want 1", count)
	}
}

func TestMergeSyncResponse_TombstoneJunction(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "local-node"
	ts := "2026-03-04T12:00:00Z"

	// Create parent entities and a junction row
	if _, fixtureErr912 := db.Exec("INSERT INTO client_groups (name, updated_at, node_id) VALUES (?, ?, ?)", "Sam", ts, "local-node"); fixtureErr912 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr912)
	}
	var gid int
	if fixtureErr914 := db.QueryRow("SELECT id FROM client_groups WHERE name = 'Sam'").Scan(&gid); fixtureErr914 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr914)
	}
	if _, fixtureErr915 := db.Exec("INSERT INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?)",
		"10.42.1.43", gid, ts, "local-node"); fixtureErr915 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr915)
	}

	// Tombstone the membership
	resp := &SyncResponse{
		NodeID:     "remote-node",
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
		Tombstones: []Tombstone{
			{TableName: "client_group_members", NaturalKey: "10.42.1.43|Sam", DeletedAt: "2026-03-05T00:00:00Z", NodeID: "remote-node"},
		},
	}

	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("merge: %v", err)
	}

	var count int
	if fixtureErr932 := db.QueryRow("SELECT COUNT(*) FROM client_group_members WHERE group_id = ?", gid).Scan(&count); fixtureErr932 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr932)
	}
	if count != 0 {
		t.Errorf("group member should be deleted, count=%d", count)
	}
}
