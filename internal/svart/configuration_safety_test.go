package svart

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

var invalidRetentionValues = []string{"0", "-1", "garbage", "", "1.5", "9223372036854775808", "9223372036854775807", "3650001"}

func TestRetentionIngressRejectsAtomically(t *testing.T) {
	for _, entry := range []string{"api", "import", "sync"} {
		for _, value := range invalidRetentionValues {
			t.Run(entry+"/"+value, func(t *testing.T) {
				defer setupTestDB(t)()
				oldPath := archivePath
				archivePath = t.TempDir()
				defer func() { archivePath = oldPath }()
				hotTime := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02 15:04:05")
				if _, err := db.Exec("INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,response_code,latency_microseconds) VALUES(?,'192.0.2.10','ingress.example.','A','NOERROR',17)", hotTime); err != nil {
					t.Fatal(err)
				}
				archiveFile := filepath.Join(archivePath, "svart-dns-"+time.Now().UTC().AddDate(0, 0, -10).Format("2006-01-02")+".parquet")
				archived := []QueryLogRow{{Timestamp: 946684800, ClientIP: "192.0.2.11", QueryName: "ingress-archive.example.", QueryType: "A"}}
				if err := parquet.WriteFile(archiveFile, archived); err != nil {
					t.Fatal(err)
				}
				// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
				archiveBefore, err := os.ReadFile(archiveFile)
				if err != nil {
					t.Fatal(err)
				}
				switch entry {
				case "api":
					body := mustFixture(json.Marshal(map[string]string{"value": value}))
					w := httptest.NewRecorder()
					handleAPISetting(w, httptest.NewRequest("PUT", "/api/settings/log_retention_days", bytes.NewReader(body)))
					if w.Code != 400 {
						t.Errorf("status = %d, want 400", w.Code)
					}
				case "import":
					body := mustFixture(json.Marshal(ConfigExport{Settings: map[string]string{"log_retention_days": value}, Upstreams: []UpstreamExport{{Upstream: "192.0.2.53:53", Enabled: true}}}))
					w := httptest.NewRecorder()
					handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewReader(body)))
					if w.Code != 400 {
						t.Errorf("status = %d, want 400", w.Code)
					}
				case "sync":
					err = mergeSyncResponse(&SyncResponse{Changes: SyncChanges{Settings: []SyncSetting{{Key: "cache_ttl", Value: "7200", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}, {Key: "log_retention_days", Value: value, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}}})
					if err == nil {
						t.Error("invalid retention sync must reject entire transaction")
					}
				}
				var got string
				if err = db.QueryRow("SELECT value FROM settings WHERE key='log_retention_days'").Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != "1095" {
					t.Errorf("retention = %q, want 1095", got)
				}
				if err := db.QueryRow("SELECT value FROM settings WHERE key='cache_ttl'").Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != "3600" {
					t.Errorf("partially committed cache_ttl=%q, want 3600", got)
				}
				var count int
				if err = db.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream='192.0.2.53:53'").Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 0 {
					t.Errorf("partially imported upstreams = %d, want 0", count)
				}
				cleanupOldLogs()
				runArchiveCycle()
				var hot string
				if err := db.QueryRow("SELECT timestamp||'|'||client_ip||'|'||query_name||'|'||query_type||'|'||response_code||'|'||latency_microseconds FROM query_logs").Scan(&hot); err != nil {
					t.Fatal(err)
				}
				if want := hotTime + "|192.0.2.10|ingress.example.|A|NOERROR|17"; hot != want {
					t.Errorf("hot row=%q, want %q", hot, want)
				}
				// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
				archiveAfter, err := os.ReadFile(archiveFile)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(archiveBefore, archiveAfter) {
					t.Error("rejected ingress changed archive bytes")
				}

			})
		}
	}
}

func TestInvalidStoredRetentionPreservesHistory(t *testing.T) {
	for _, value := range append(append([]string{}, invalidRetentionValues...), "missing") {
		t.Run(value, func(t *testing.T) {
			defer setupTestDB(t)()
			oldPath := archivePath
			archivePath = t.TempDir()
			defer func() { archivePath = oldPath }()
			if _, err := db.Exec("INSERT INTO query_logs (timestamp,client_ip,query_name,query_type,response_code,blocked,latency_microseconds) VALUES ('2001-01-01 12:00:00','192.0.2.10','retained.example.','A','NOERROR',0,17)"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(archivePath, "svart-dns-2000-01-01.parquet")
			rows := []QueryLogRow{{Timestamp: 946684800, ClientIP: "192.0.2.11", QueryName: "archive.example.", QueryType: "AAAA", ResponseCode: "NOERROR"}}
			if err := parquet.WriteFile(path, rows); err != nil {
				t.Fatal(err)
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if value == "missing" {
				_, err = db.Exec("DELETE FROM settings WHERE key='log_retention_days'")
			} else {
				_, err = db.Exec("UPDATE settings SET value=? WHERE key='log_retention_days'", value)
			}
			if err != nil {
				t.Fatal(err)
			}
			cleanupOldLogs()
			runArchiveCycle()
			var name string
			if err = db.QueryRow("SELECT query_name FROM query_logs").Scan(&name); err != nil {
				t.Errorf("hot row lost: %v", err)
			}
			if name != "retained.example." {
				t.Errorf("hot row = %q", name)
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("archive bytes changed")
			}
			actual, err := parquet.ReadFile[QueryLogRow](path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, rows) {
				t.Errorf("archive rows = %#v, want %#v", actual, rows)
			}
		})
	}
}

func TestPositiveRetentionDeletesOnlyExpiredHistory(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("UPDATE settings SET value='30' WHERE key='log_retention_days'"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		days int
		name string
	}{{40, "expired.example."}, {2, "recent.example."}} {
		if _, err := db.Exec("INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,response_code) VALUES(?, '192.0.2.10', ?, 'A','NOERROR')", time.Now().UTC().AddDate(0, 0, -row.days).Format("2006-01-02 15:04:05"), row.name); err != nil {
			t.Fatal(err)
		}
	}
	cleanupOldLogs()
	var name string
	if err := db.QueryRow("SELECT query_name FROM query_logs").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "recent.example." {
		t.Fatalf("retained row = %q", name)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retained count = %d, want 1", count)
	}
}

func TestMigrationPreservesConfiguredSevenDayRetention(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("UPDATE settings SET value='7' WHERE key='log_retention_days'"); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(); err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='log_retention_days'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "7" {
		t.Fatalf("configured retention after migration=%q, want 7", value)
	}
}
