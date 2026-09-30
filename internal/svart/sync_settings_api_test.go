package svart

import (
	"context"

	"encoding/json"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"
)

func TestSeedSyncSettings_EnvSeedsEmptyDB(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	t.Setenv("PEERS", "https://hypervisor-blue:3000,https://hypervisor-green:3000")
	t.Setenv("SYNC_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("SYNC_INTERVAL", "5s")

	if fixtureErr946 := seedSyncSettings(); fixtureErr946 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr946)
	}

	if v := getSyncSetting("sync_peers"); v != "https://hypervisor-blue:3000,https://hypervisor-green:3000" {
		t.Errorf("sync_peers = %q, want peers from env", v)
	}
	if v := getSyncSetting("sync_secret"); v != "0123456789abcdef0123456789abcdef" {
		t.Errorf("sync_secret = %q, want strong seeded secret", v)
	}
	if v := getSyncSetting("sync_interval"); v != "5s" {
		t.Errorf("sync_interval = %q, want '5s'", v)
	}
}

func TestSeedSyncSettings_NonEmptyDBNotOverwritten(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Pre-fill DB with values
	ts, nid := syncNow()
	if _, fixtureErr965 := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = 'sync_peers'",
		"https://existing-peer:3000", ts, nid); fixtureErr965 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr965)
	}
	if _, fixtureErr967 := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = 'sync_secret'",
		"existing-secret", ts, nid); fixtureErr967 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr967)
	}

	t.Setenv("PEERS", "https://env-peer:3000")
	t.Setenv("SYNC_SECRET", "env-secret")

	if fixtureErr973 := seedSyncSettings(); fixtureErr973 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr973)
	}

	if v := getSyncSetting("sync_peers"); v != "https://existing-peer:3000" {
		t.Errorf("sync_peers = %q, should not be overwritten by env", v)
	}
	if v := getSyncSetting("sync_secret"); v != "existing-secret" {
		t.Errorf("sync_secret = %q, should not be overwritten by env", v)
	}
}

func TestSeedSyncSettings_EmptyEnvNoChange(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	t.Setenv("PEERS", "")
	t.Setenv("SYNC_SECRET", "")
	t.Setenv("SYNC_INTERVAL", "")

	if fixtureErr991 := seedSyncSettings(); fixtureErr991 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr991)
	}

	if v := getSyncSetting("sync_peers"); v != "" {
		t.Errorf("sync_peers = %q, should remain empty", v)
	}
	if v := getSyncSetting("sync_interval"); v != "" {
		t.Errorf("sync_interval = %q, should remain empty", v)
	}
}

func TestGetSyncSetting(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Default values from createTables — all empty
	if v := getSyncSetting("sync_interval"); v != "" {
		t.Errorf("sync_interval default = %q, want empty", v)
	}
	if v := getSyncSetting("sync_peers"); v != "" {
		t.Errorf("sync_peers default = %q, want empty", v)
	}
	if v := getSyncSetting("nonexistent_key"); v != "" {
		t.Errorf("nonexistent key = %q, want empty", v)
	}
}

func TestHandleAPIPeers_GetExtendedFields(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	nodeID = "test-node"

	// Set a secret so has_secret is true
	ts, nid := syncNow()
	if _, fixtureErr1025 := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = 'sync_secret'",
		"0123456789abcdef0123456789abcdef", ts, nid); fixtureErr1025 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1025)
	}

	req := httptest.NewRequest("GET", "/api/peers", nil)
	w := httptest.NewRecorder()
	handleAPIPeers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Data struct {
			NodeID        string        `json:"node_id"`
			Peers         []interface{} `json:"peers"`
			SyncInterval  string        `json:"sync_interval"`
			HasSecret     bool          `json:"has_secret"`
			TLSConfigured bool          `json:"tls_configured"`
		} `json:"data"`
	}
	if fixtureErr1045 := json.NewDecoder(w.Body).Decode(&resp); fixtureErr1045 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1045)
	}

	if resp.Data.NodeID != "test-node" {
		t.Errorf("node_id = %q", resp.Data.NodeID)
	}
	if !resp.Data.HasSecret {
		t.Error("has_secret should be true")
	}
	if resp.Data.SyncInterval != "" {
		t.Errorf("sync_interval = %q, want empty (default)", resp.Data.SyncInterval)
	}
}

func TestHandleAPIPeersGetRedactsSyncSecretForReadonly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	ts, nid := syncNow()
	if _, fixtureErr1063 := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = 'sync_secret'",
		"0123456789abcdef0123456789abcdef", ts, nid); fixtureErr1063 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1063)
	}

	req := httptest.NewRequest("GET", "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestAuthContextKey, &APIToken{Role: RoleReadonly, Name: "viewer"}))
	w := httptest.NewRecorder()
	handleAPIPeers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	resp, fixtureErr1076 := data.(map[string]interface{})
	if !fixtureErr1076 {
		t.Fatalf("unexpected fixture value type: %T", data)
	}
	if _, ok := resp["sync_secret"]; ok {
		t.Fatalf("readonly response should redact sync_secret, got %v", resp["sync_secret"])
	}
	if resp["has_secret"] != true {
		t.Fatalf("expected has_secret to remain true, got %v", resp["has_secret"])
	}
}

func TestHandleAPIPeersGetRedactsSyncSecretForAdmin(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	ts, nid := syncNow()
	if _, fixtureErr1090 := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = 'sync_secret'",
		"0123456789abcdef0123456789abcdef", ts, nid); fixtureErr1090 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1090)
	}

	req := httptest.NewRequest("GET", "/api/peers", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestAuthContextKey, &APIToken{Role: RoleAdmin, Name: "admin"}))
	w := httptest.NewRecorder()
	handleAPIPeers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	resp, fixtureErr1103 := data.(map[string]interface{})
	if !fixtureErr1103 {
		t.Fatalf("unexpected fixture value type: %T", data)
	}
	if _, ok := resp["sync_secret"]; ok {
		t.Fatalf("admin response should redact sync_secret, got %v", resp["sync_secret"])
	}
	if resp["has_secret"] != true {
		t.Fatalf("expected has_secret to remain true, got %v", resp["has_secret"])
	}
}

func TestHandleAPIPeers_PostAndDelete(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	savedAllowedPeers := syncAllowedPeers
	syncAllowedPeers = map[string]struct{}{
		"https://hypervisor-blue:3000":  {},
		"https://hypervisor-green:3000": {},
	}
	nodeID = "test-node"
	tlsCert = "/tmp/fake-cert.pem"
	tlsKey = "/tmp/fake-key.pem"
	defer func() {
		tlsCert = ""
		tlsKey = ""
		syncAllowedPeers = savedAllowedPeers
	}()

	// Set node_name (required before adding peers)
	if _, fixtureErr1131 := db.Exec("UPDATE settings SET value = 'test-node' WHERE key = 'node_name'"); fixtureErr1131 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1131)
	}

	// POST: add a peer
	body := `{"url":"https://hypervisor-blue:3000"}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIPeers(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201, body: %s", w.Code, w.Body.String())
	}

	if v := getSyncSetting("sync_peers"); v != "https://hypervisor-blue:3000" {
		t.Errorf("sync_peers after POST = %q", v)
	}

	// POST: add another peer
	body2 := `{"url":"https://hypervisor-green:3000"}`
	req2 := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body2))
	w2 := httptest.NewRecorder()
	handleAPIPeers(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("second POST status = %d", w2.Code)
	}

	if v := getSyncSetting("sync_peers"); v != "https://hypervisor-blue:3000,https://hypervisor-green:3000" {
		t.Errorf("sync_peers after second POST = %q", v)
	}

	// POST: duplicate should fail with 409
	req3 := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w3 := httptest.NewRecorder()
	handleAPIPeers(w3, req3)

	if w3.Code != http.StatusConflict {
		t.Errorf("duplicate POST status = %d, want 409", w3.Code)
	}

	// DELETE: remove first peer
	req4 := httptest.NewRequest("DELETE", "/api/peers/https%3A%2F%2Fhypervisor-blue%3A3000", nil)
	w4 := httptest.NewRecorder()
	handleAPIPeers(w4, req4)

	if w4.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200", w4.Code)
	}

	if v := getSyncSetting("sync_peers"); v != "https://hypervisor-green:3000" {
		t.Errorf("sync_peers after DELETE = %q", v)
	}
}

func TestHandleAPIPeers_PostRejectsHTTP(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	body := `{"url":"http://insecure-peer:3000"}`
	req := httptest.NewRequest("POST", "/api/peers", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIPeers(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for http:// URL", w.Code)
	}
}

func TestDeletedPeerBlocksGossipRediscovery(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	savedTLSServerName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	nodeID = "test-node"
	externalIP = "10.42.1.101"
	tlsCert = "/tmp/fake-cert.pem"
	tlsKey = "/tmp/fake-key.pem"
	adminPort = "443"
	syncTLSServerName = testSyncTLSServerName
	syncAllowedPeers = map[string]struct{}{
		"https://10.42.1.6:443": {},
		"https://10.42.1.7:443": {},
		"https://10.42.1.8:443": {},
	}
	defer func() {
		tlsCert = ""
		tlsKey = ""
		externalIP = ""
		syncTLSServerName = savedTLSServerName
		syncAllowedPeers = savedAllowedPeers
	}()

	if _, fixtureErr1223 := db.Exec("UPDATE settings SET value = 'test-node' WHERE key = 'node_name'"); fixtureErr1223 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1223)
	}

	// Add two peers
	if _, fixtureErr1226 := addPeerToSettings("https://10.42.1.6:443"); fixtureErr1226 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1226)
	}
	if _, fixtureErr1227 := addPeerToSettings("https://10.42.1.7:443"); fixtureErr1227 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1227)
	}

	// Delete one peer
	if fixtureErr1230 := removePeerFromSettings("https://10.42.1.7:443"); fixtureErr1230 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1230)
	}

	// Verify it's in deleted_peers
	if !isDeletedPeer("https://10.42.1.7:443") {
		t.Fatal("expected 10.42.1.7 to be in deleted_peers after removal")
	}

	// Simulate gossip from the other peer that still knows about the deleted one
	resp := &SyncResponse{
		SelfURL:    "https://10.42.1.6:443",
		KnownPeers: []string{"https://10.42.1.7:443", "https://10.42.1.8:443"},
	}
	discoverPeers(resp)

	// 10.42.1.7 should NOT be re-added (was explicitly deleted)
	peers := getSyncSetting("sync_peers")
	if strings.Contains(peers, "10.42.1.7") {
		t.Errorf("deleted peer was re-added by gossip: sync_peers = %q", peers)
	}

	// 10.42.1.8 SHOULD be added (new peer, not deleted)
	if !strings.Contains(peers, "10.42.1.8") {
		t.Errorf("new peer was not added by gossip: sync_peers = %q", peers)
	}

	// Explicitly re-adding via POST should clear the deletion
	if fixtureErr1256 := removeDeletedPeer("https://10.42.1.7:443"); fixtureErr1256 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1256)
	}
	if isDeletedPeer("https://10.42.1.7:443") {
		t.Fatal("expected 10.42.1.7 to be removed from deleted_peers after explicit re-add")
	}
	if _, fixtureErr1260 := addPeerToSettings("https://10.42.1.7:443"); fixtureErr1260 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1260)
	}
	peers = getSyncSetting("sync_peers")
	if !strings.Contains(peers, "10.42.1.7") {
		t.Errorf("explicitly re-added peer should be in sync_peers: %q", peers)
	}
}

func TestSyncSettingsHook(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Verify the setting hook runs without error.
	// We can't easily test that restartSync() was called, but we can verify
	// the handler completes without panicking and the value is persisted.
	body := `{"value":"10s"}`
	req := httptest.NewRequest("PUT", "/api/settings/sync_interval", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}

	if v := getSyncSetting("sync_interval"); v != "10s" {
		t.Errorf("sync_interval = %q, want '10s'", v)
	}
}
