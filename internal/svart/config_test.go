package svart

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigExportImport(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = 'export-sync-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'export-session-secret' WHERE key = 'session_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Export
	req := httptest.NewRequest("GET", "/api/config/export", nil)
	w := httptest.NewRecorder()
	handleAPIConfigExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("export failed with status %d: %s", w.Code, w.Body.String())
	}

	var exportResp struct {
		Data ConfigExport `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &exportResp); err != nil {
		t.Fatalf("failed to unmarshal export: %v", err)
	}

	export := exportResp.Data

	if export.Version != "1.0" {
		t.Errorf("expected version '1.0', got %q", export.Version)
	}

	if len(export.Upstreams) != 5 {
		t.Errorf("expected 5 upstreams, got %d", len(export.Upstreams))
	}

	if len(export.Blocklists) != 3 {
		t.Errorf("expected 3 blocklists, got %d", len(export.Blocklists))
	}

	if len(export.Rewrites) != 5 {
		t.Errorf("expected 5 rewrites, got %d", len(export.Rewrites))
	}

	if len(export.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(export.Groups))
	}

	if len(export.Settings) == 0 {
		t.Error("expected non-empty settings")
	}
	if _, ok := export.Settings["sync_secret"]; ok {
		t.Fatalf("config export should omit sync_secret, got %q", export.Settings["sync_secret"])
	}
	if _, ok := export.Settings["session_secret"]; ok {
		t.Fatalf("config export should omit session_secret, got %q", export.Settings["session_secret"])
	}

	if len(export.Bootstrap) == 0 {
		t.Error("expected non-empty bootstrap servers")
	}

	// Now import into a fresh DB
	cleanup2 := setupTestDB(t)
	defer cleanup2()

	exportJSON, fixtureErr2021 := json.Marshal(export)
	if fixtureErr2021 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr2021)
	}
	req = httptest.NewRequest("POST", "/api/config/import", strings.NewReader(string(exportJSON)))
	w = httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("import failed with status %d: %s", w.Code, w.Body.String())
	}

	// Verify imported data
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM upstreams").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 imported upstreams, got %d", count)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM blocklists").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 imported blocklists, got %d", count)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM rewrites").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 imported rewrites, got %d", count)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM client_groups").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 imported groups, got %d", count)
	}
}

func TestConfigImportCannotOverwriteSecrets(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'existing-sync-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'existing-session-secret' WHERE key = 'session_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	config := ConfigExport{
		Version: "1.0",
		// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
		Settings: map[string]string{
			"cache_ttl":      "7200",
			"sync_secret":    "imported-sync-secret",
			"session_secret": "imported-session-secret",
		},
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/config/import", strings.NewReader(string(configJSON)))
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	for key, expected := range map[string]string{
		"cache_ttl":      "7200",
		"sync_secret":    "existing-sync-secret",
		"session_secret": "existing-session-secret",
	} {
		var actual string
		if err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&actual); err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if actual != expected {
			t.Fatalf("%s = %q, want %q", key, actual, expected)
		}
	}
}

func TestConfigExportEmpty(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/config/export", nil)
	w := httptest.NewRecorder()
	handleAPIConfigExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("export failed with status %d", w.Code)
	}

	var resp struct {
		Data ConfigExport `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// All arrays should be non-nil (empty arrays in JSON)
	if resp.Data.Upstreams == nil {
		t.Error("expected non-nil upstreams array")
	}
	if resp.Data.Blocklists == nil {
		t.Error("expected non-nil blocklists array")
	}
	if resp.Data.Rewrites == nil {
		t.Error("expected non-nil rewrites array")
	}
	if resp.Data.Groups == nil {
		t.Error("expected non-nil groups array")
	}
	if resp.Data.Ranges == nil {
		t.Error("expected non-nil ranges array")
	}
	if resp.Data.Clients == nil {
		t.Error("expected non-nil clients array")
	}
}

func TestConfigImportValidation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Malformed JSON
	req := httptest.NewRequest("POST", "/api/config/import", strings.NewReader("{invalid json"))
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

// TestConfigImportRejectsOversizedBody verifies the MaxBytesReader cap added
// to /api/config/import: a body over maxConfigImportBodyBytes must be
// rejected with 413, not silently read into memory in full.
func TestConfigImportRejectsOversizedBody(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	oversized := `{"version":"1.0","settings":{"padding":"` + strings.Repeat("a", maxConfigImportBodyBytes+1024) + `"}}`
	req := httptest.NewRequest("POST", "/api/config/import", strings.NewReader(oversized))
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for oversized config import body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestConfigImportIdempotent(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	config := ConfigExport{
		Version: "1.0",
		Upstreams: []UpstreamExport{
			{Upstream: "9.9.9.9:53", Enabled: true},
		},
		Blocklists: []ListExport{
			{URL: "https://example.com/list.txt", Alias: "Test List", Enabled: true},
		},
		Allowlists: []ListExport{},
		Rewrites: []RewriteExport{
			{Domain: "test.local", IPAddresses: "10.0.0.1", Enabled: true},
		},
		Groups:    []GroupExport{},
		Ranges:    []RangeExport{},
		Settings:  map[string]string{"cache_ttl": "7200"},
		Bootstrap: []string{"9.9.9.9:53"},
		Clients:   []ClientExport{},
	}

	configJSON, fixtureErr7404 := json.Marshal(config)
	if fixtureErr7404 !=

		nil {
		t.Errorf("fixture operation failed: %v", fixtureErr7404)
	}

	// Import twice — should be idempotent
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/config/import", strings.NewReader(string(configJSON)))
		w := httptest.NewRecorder()
		handleAPIConfigImport(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("import %d failed with status %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// Should still have only 1 upstream (not duplicated)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM upstreams").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 upstream after idempotent import, got %d", count)
	}
}

func TestConfigImportRefreshesRuntimeSettings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	config := ConfigExport{
		Version:   "1.0",
		Settings:  map[string]string{"cache_ttl": "7200", "bootstrap_ttl": "120", "strategy": "random", "denied_ttl": "300"},
		Bootstrap: []string{"9.9.9.9:53"},
	}

	configJSON, fixtureErr8407 := json.Marshal(config)
	if fixtureErr8407 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr8407)
	}

	req := httptest.NewRequest("POST", "/api/config/import", strings.NewReader(string(configJSON)))
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("import failed with status %d: %s", w.Code, w.Body.String())
	}

	if ttl := getCacheTTL(); ttl != 7200 {
		t.Errorf("expected runtime cache_ttl 7200, got %d", ttl)
	}
	if ttl := getBootstrapTTL(); ttl != 120 {
		t.Errorf("expected runtime bootstrap_ttl 120, got %d", ttl)
	}
	if ttl := getDeniedTTL(); ttl != 300 {
		t.Errorf("expected runtime denied_ttl 300, got %d", ttl)
	}
	if strategy := getResolutionStrategy(); strategy != "random" {
		t.Errorf("expected runtime strategy random, got %q", strategy)
	}
}
