package svart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseSyncIntervalEnforcesSafeBounds(t *testing.T) {
	tests := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "1s", want: time.Second},
		{value: "2s", want: 2 * time.Second},
		{value: "1h", want: time.Hour},
		{value: "", want: 2 * time.Second},
		{value: "not-a-duration", wantErr: true},
		{value: "-1s", wantErr: true},
		{value: "0s", wantErr: true},
		{value: "999ms", wantErr: true},
		{value: "1h1s", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, err := parseSyncInterval(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSyncInterval(%q) = %s, want error", tt.value, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("parseSyncInterval(%q) = %s, %v; want %s, nil", tt.value, got, err, tt.want)
			}
		})
	}
}

func TestHandleAPISettingRejectsUnsafeSyncIntervalWithoutMutation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = '2s' WHERE key = 'sync_interval'"); err != nil {
		t.Fatalf("seed sync interval: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/settings/sync_interval", strings.NewReader(`{"value":"0s"}`))
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if got := getSyncSetting("sync_interval"); got != "2s" {
		t.Fatalf("stored sync_interval = %q, want unchanged 2s", got)
	}
}

func TestMergeRejectsUnsafeSyncIntervalWithoutMutation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = '2s', updated_at = '2026-01-01T00:00:00Z' WHERE key = 'sync_interval'"); err != nil {
		t.Fatalf("seed sync interval: %v", err)
	}

	err := mergeSyncResponse(&SyncResponse{Changes: SyncChanges{Settings: []SyncSetting{{
		Key:       "sync_interval",
		Value:     "0s",
		UpdatedAt: "2026-07-12T00:00:00Z",
		NodeID:    "remote-node",
	}}}})
	if err == nil {
		t.Fatal("merge accepted unsafe sync_interval")
	}
	if got := getSyncSetting("sync_interval"); got != "2s" {
		t.Fatalf("stored sync_interval = %q, want unchanged 2s", got)
	}
}

func TestInitSyncFallsBackFromUnsafeStoredInterval(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = '0s' WHERE key = 'sync_interval'"); err != nil {
		t.Fatalf("seed sync interval: %v", err)
	}

	initSync()

	if syncInterval != 2*time.Second {
		t.Fatalf("runtime syncInterval = %s, want safe fallback 2s", syncInterval)
	}
}

func TestSeedSyncSettingsRejectsUnsafeInterval(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	t.Setenv("SYNC_INTERVAL", "0s")

	if err := seedSyncSettings(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if got := getSyncSetting("sync_interval"); got != "" {
		t.Fatalf("stored sync_interval = %q, want empty after unsafe env seed", got)
	}
}

func TestConfigImportRejectsUnsafeSyncIntervalWithoutMutation(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = '2s' WHERE key = 'sync_interval'"); err != nil {
		t.Fatalf("seed sync interval: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/config/import", strings.NewReader(`{"settings":{"sync_interval":"0s"}}`))
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if got := getSyncSetting("sync_interval"); got != "2s" {
		t.Fatalf("stored sync_interval = %q, want unchanged 2s", got)
	}
}
