package svart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateSyncSecretStrengthRejectsUnsafeHeaderValues(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef"
	for _, secret := range []string{
		strings.Repeat("x", 31),
		" " + valid,
		valid + " ",
		valid + "\r",
		valid + "\n",
		valid + "\x00",
		valid + "\x7f",
	} {
		if err := validateSyncSecretStrength(secret); err == nil {
			t.Fatalf("validateSyncSecretStrength accepted an unsafe value of length %d", len(secret))
		}
	}
	if err := validateSyncSecretStrength(valid); err != nil {
		t.Fatalf("validateSyncSecretStrength rejected a valid secret: %v", err)
	}
}

func TestInitSyncRejectsWeakStoredSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = 'weak-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Fatalf("seed weak sync secret: %v", err)
	}

	initSync()

	if syncSecret != "" {
		t.Fatal("weak stored sync secret was activated")
	}
}

func TestSeedSyncSettingsRejectsWeakSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	t.Setenv("SYNC_SECRET", "weak-secret")

	if err := seedSyncSettings(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	if got := getSyncSetting("sync_secret"); got != "" {
		t.Fatalf("stored sync_secret is non-empty after weak env seed")
	}
}

func TestPeerStatusDoesNotReportWeakSecretAsReady(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = 'weak-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Fatalf("seed weak sync secret: %v", err)
	}

	w := httptest.NewRecorder()
	handleAPIPeersGet(w, httptest.NewRequest(http.MethodGet, "/api/peers", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q, want nil", *apiErr)
	}
	response := requireFixtureType[map[string]interface{}](t, data)
	if response["has_secret"] != false {
		t.Fatalf("has_secret = %v, want false for a weak secret", response["has_secret"])
	}
	if response["sync_tls_ready"] != false {
		t.Fatalf("sync_tls_ready = %v, want false for a weak secret", response["sync_tls_ready"])
	}
}
