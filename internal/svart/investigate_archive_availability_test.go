package svart

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func assertInvestigationArchiveCount(t *testing.T, want int) {
	t.Helper()
	status, response, raw := postInvestigate(t, `SELECT count(*) FROM query_logs`)
	if status != http.StatusOK || response.Error != nil {
		t.Fatalf("healthy archive HTTP%d %s", status, raw)
	}
	var body struct {
		Data struct {
			Rows [][]int `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(body.Data.Rows); got != fmt.Sprintf("[[%d]]", want) {
		t.Fatalf("unified rows=%s, want [[%d]]", got, want)
	}
	w := httptest.NewRecorder()
	handleAPIInvestigateSchema(w, httptest.NewRequest(http.MethodGet, "/api/investigate/schema", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("healthy schema HTTP%d %s", w.Code, w.Body.String())
	}
	t.Logf("healthy unified count=%d; query and schema HTTP200", want)
}

func assertInvestigationArchiveUnavailable(t *testing.T) {
	t.Helper()
	status, _, raw := postInvestigate(t, `SELECT count(*) FROM query_logs`)
	w := httptest.NewRecorder()
	handleAPIInvestigateSchema(w, httptest.NewRequest(http.MethodGet, "/api/investigate/schema", nil))
	for _, got := range []struct {
		name, body string
		status     int
	}{{"query", raw, status}, {"schema", w.Body.String(), w.Code}} {
		want := "{\"data\":null,\"error\":\"investigation archive unavailable; check server storage and retry\",\"error_code\":\"unavailable\"}\n"
		if got.status != http.StatusServiceUnavailable || got.body != want {
			t.Errorf("%s HTTP%d %s; want HTTP503 %s", got.name, got.status, got.body, want)
		}
		t.Logf("%s HTTP%d %s", got.name, got.status, got.body)
	}
}

func TestInvestigateArchiveUnavailableAndRecovery(t *testing.T) {
	for _, failure := range []string{"not-directory", "permission-denied"} {
		t.Run(failure, func(t *testing.T) {
			if failure == "permission-denied" && os.Geteuid() == 0 {
				t.Skip("permission denial requires an unprivileged process")
			}
			archiveDir := setupInvestigateSandbox(t)
			coldPath := filepath.Join(archiveDir, "cold.parquet")
			cold := []legacyArchiveRow{{Timestamp: time.Date(2025, 3, 14, 22, 5, 0, 0, time.UTC).UnixMilli(), ClientIP: "192.0.2.42", QueryName: "cold.example.com", QueryType: "A"}}
			if err := parquet.WriteFile(coldPath, cold); err != nil {
				t.Fatal(err)
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			original, err := os.ReadFile(coldPath)
			if err != nil {
				t.Fatal(err)
			}
			assertInvestigationArchiveCount(t, 6)
			var restore func() error
			if failure == "not-directory" {
				saved := archiveDir + ".preserved"
				if err := os.Rename(archiveDir, saved); err != nil {
					t.Fatal(err)
				}
				restore = func() error {
					if err := os.Remove(archiveDir); err != nil && !os.IsNotExist(err) {
						return err
					}
					return os.Rename(saved, archiveDir)
				}
			} else {
				// #nosec G302 -- Directory execute permission is required to restore traversal in this permission-failure fixture.
				restore = func() error { return os.Chmod(archiveDir, 0700) }
			}
			restored := false
			t.Cleanup(func() {
				if !restored {
					if err := restore(); err != nil {
						t.Error(err)
					}
				}
			})
			if failure == "not-directory" {
				err = os.WriteFile(archiveDir, []byte("archive mount unavailable fixture"), 0600)
			} else {
				err = os.Chmod(archiveDir, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertInvestigationArchiveUnavailable(t)
			if err := restore(); err != nil {
				t.Fatal(err)
			}
			restored = true
			assertInvestigationArchiveCount(t, 6)
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			after, err := os.ReadFile(coldPath)
			if err != nil || string(after) != string(original) {
				t.Fatalf("cold archive changed: read error=%v", err)
			}
		})
	}
}

func TestInvestigateArchiveFreshAndEmpty(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)
	assertInvestigationArchiveCount(t, 5)
	if err := os.WriteFile(filepath.Join(archiveDir, "operator-note.txt"), []byte("No archives have been written yet."), 0600); err != nil {
		t.Fatal(err)
	}
	assertInvestigationArchiveCount(t, 5)
	duckDBArchive = filepath.Join(archiveDir, "fresh")
	assertInvestigationArchiveCount(t, 5)
	if _, err := os.Stat(duckDBArchive); !os.IsNotExist(err) {
		t.Fatalf("inspection created fresh archive directory: %v", err)
	}
}
