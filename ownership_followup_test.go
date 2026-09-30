package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarySchemaFailureKeepsNativeDiagnosticPrivate(t *testing.T) {
	independentArchiveFixture(t)
	name := "private-customer-storage-unique-marker.parquet"
	if err := os.WriteFile(filepath.Join(archivePath, name), []byte("not parquet"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := initDuckDB(testDBPath(t), archivePath); err != nil {
		t.Fatal(err)
	}
	defer closeDuckDB()
	req := httptest.NewRequest(http.MethodGet, "/api/investigate/schema", nil)
	rec := httptest.NewRecorder()
	handleAPIInvestigateSchema(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), name) || strings.Contains(rec.Body.String(), archivePath) {
		t.Fatalf("native storage diagnostic leaked: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "investigation schema unavailable") {
		t.Fatalf("unsafe/unclear error: %s", rec.Body.String())
	}
}

func TestLegacyRollbackTailAndReusedOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.spool")
	first := []byte("{\"ts\":123,\"query_name\":\"original.example.\",\"unknown\":\"雪\\nvalue\"}\n")
	tail := []byte("{\"ts\":456,\"query_name\":\"rollback-tail.example.\",\"unknown\":\"exact\"}\n")
	if err := os.WriteFile(path, first, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(tail); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.journal.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	if len(rows) != 2 || !bytes.Equal(rows[0].payload, first) || !bytes.Equal(rows[1].payload, tail) {
		t.Fatalf("legacy rollback tail not imported exactly: %+v", rows)
	}
	replacement := []byte("{\"ts\":789,\"query_name\":\"reused-offset.example.\"}\n")
	if err = os.WriteFile(path, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	s, err = openLogSpool(path)
	if err == nil {
		s.close()
		t.Fatal("reused legacy offsets falsely accepted after rollback")
	}
	if !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("unexpected error=%v", err)
	}
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, replacement) {
		t.Fatal("rejected rollback original was modified")
	}
}

func TestLegacyPartialCompletionAndMissingProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.spool")
	partial := []byte(`{"ts":123,"query_name":"completed.example.`)
	if err := os.WriteFile(path, partial, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	complete := append(append([]byte(nil), partial...), []byte("\"}\n")...)
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write([]byte("\"}\n")); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openLogSpool(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.journal.batch(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].payload, complete) {
		t.Fatalf("completed partial=%+v", rows)
	}
	var quarantined []byte
	if err = s.journal.db.QueryRow("SELECT payload FROM journal_quarantine").Scan(&quarantined); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(quarantined, partial) {
		t.Fatal("partial original was overwritten")
	}
	if _, err = s.journal.db.Exec("DELETE FROM journal_meta WHERE key='legacy_provenance_v1'"); err != nil {
		t.Fatal(err)
	}
	s.close()
	s, err = openLogSpool(path)
	if err == nil {
		s.close()
		t.Fatal("legacy marker without provenance accepted")
	}
	if !strings.Contains(err.Error(), "no exact prefix provenance") {
		t.Fatalf("error=%v", err)
	}
}

func TestLegacyImportRejectsConflictingTokenAndIgnoredOwnership(t *testing.T) {
	for _, fault := range []string{"conflicting-token", "journal_records", "journal_meta", "journal_quarantine"} {
		t.Run(fault, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "query.spool")
			payload := []byte("{\"ts\":123,\"query_name\":\"legacy-original.example.\"}\n")
			if fault == "journal_quarantine" {
				payload = []byte(`{"ts":123`)
			}
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			j, err := openDurableJournal(path + ".sqlite")
			if err != nil {
				t.Fatal(err)
			}
			if fault == "conflicting-token" {
				if err := j.writeBatch([]*journalRequest{{token: "legacy:0", payload: []byte("{\"ts\":456}\n")}}); err != nil {
					t.Fatal(err)
				}
				// #nosec G202 -- Fixture SQL identifiers come only from the local schema or fixed test-case table list; values are bound.
			} else if _, err := j.db.Exec("CREATE TRIGGER migration_fault BEFORE INSERT ON " + fault + " BEGIN SELECT RAISE(IGNORE); END"); err != nil {
				t.Fatal(err)
			}
			if err := j.close(); err != nil {
				t.Fatal(err)
			}
			s, err := openLogSpool(path)
			if err == nil {
				s.close()
				t.Fatal("failed legacy ownership was accepted")
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(original, payload) {
				t.Fatal("legacy source was changed")
			}
			j, err = openDurableJournal(path + ".sqlite")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := j.close(); err != nil {
					t.Errorf("fixture operation failed: %v", err)
				}
			}()
			var marked int
			if err := j.db.QueryRow("SELECT count(*) FROM journal_meta WHERE key IN ('legacy_imported','legacy_provenance_v1')").Scan(&marked); err != nil {
				t.Fatal(err)
			}
			if marked != 0 {
				t.Fatalf("failed migration acknowledged=%d metadata rows", marked)
			}
		})
	}
}
