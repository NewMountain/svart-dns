package svart

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type compatibilityInterleavedRead struct {
	queryer
	queries int
	between func()
}

func (q *compatibilityInterleavedRead) Query(query string, args ...any) (*sql.Rows, error) {
	q.queries++
	if q.queries == 2 {
		q.between()
	}
	return q.queryer.Query(query, args...)
}

func TestCompatibilityListViewGenerationCoherence(t *testing.T) {
	for _, kind := range []string{"blocklist", "allowlist"} {
		for _, change := range []string{"source", "generation"} {
			t.Run(kind+"/"+change, func(t *testing.T) {
				defer setupTestDB(t)()
				store, err := compatibilityStore(kind)
				if err != nil {
					t.Fatal(err)
				}
				before := ListCompatibilityReport{Version: 1, AssessedAt: "2026-09-29T19:00:00Z", Lines: 1, Applied: 1}
				raw, err := json.Marshal(before)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("INSERT INTO "+store.lists+"(id,url,alias,enabled,domain_count) VALUES(971,'https://lists.example/old','coherence',1,1); INSERT INTO local_"+kind+"_generations(list_id,url,compatibility_report) VALUES(971,'https://lists.example/old',?)", string(raw)); err != nil {
					t.Fatal(err)
				}
				q := &compatibilityInterleavedRead{queryer: readDB, between: func() {
					if change == "source" {
						if _, err := db.Exec("UPDATE " + store.lists + " SET url='https://lists.example/new' WHERE id=971"); err != nil {
							t.Fatal(err)
						}
					} else {
						after := before
						after.Applied = 2
						after.Lines = 2
						raw, err := json.Marshal(after)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := db.Exec("UPDATE "+store.lists+" SET domain_count=2 WHERE id=971; UPDATE local_"+kind+"_generations SET compatibility_report=? WHERE list_id=971", string(raw)); err != nil {
							t.Fatal(err)
						}
					}
				}}
				rows, err := readListViewRows(q, kind)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != 1 {
					t.Fatalf("rows=%+v", rows)
				}
				row := rows[0]
				if row.URL == "https://lists.example/new" && row.Compatibility.Assessed {
					t.Fatalf("new URL got old assessment: %+v", row)
				}
				if row.Compatibility.Assessed && row.Compatibility.Applied != row.DomainCount {
					t.Fatalf("mixed generation: %+v", row)
				}
			})
		}
	}
}

func TestCompatibilitySemanticallyCorruptReportsAreUnavailable(t *testing.T) {
	for _, kind := range []string{"blocklist", "allowlist"} {
		t.Run(kind, func(t *testing.T) {
			defer setupTestDB(t)()
			store, err := compatibilityStore(kind)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("INSERT INTO " + store.lists + "(id,url,alias,enabled) VALUES(972,'https://lists.example/corrupt','corrupt',1); INSERT INTO local_" + kind + "_generations(list_id,url) VALUES(972,'https://lists.example/corrupt')"); err != nil {
				t.Fatal(err)
			}
			for name, raw := range map[string]string{
				"missing":            `{"version":1}`,
				"timestamp":          `{"version":1,"assessed_at":"yesterday","lines":0,"applied":0,"unsupported":0,"invalid":0,"diagnostics":[]}`,
				"negative":           `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":0,"applied":-1,"unsupported":0,"invalid":0,"diagnostics":[]}`,
				"missing_counter":    `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":0,"unsupported":0,"invalid":0,"diagnostics":[]}`,
				"missing_diagnostic": `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":1,"applied":0,"unsupported":1,"invalid":0,"diagnostics":[]}`,
				"bad_diagnostic":     `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":1,"applied":0,"unsupported":1,"invalid":0,"diagnostics":[{"line":2,"rule":"x","reason":"unsupported"}]}`,
				"empty_reason":       `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":1,"applied":0,"unsupported":1,"invalid":0,"diagnostics":[{"line":1,"rule":"x","reason":""}]}`,
			} {
				t.Run(name, func(t *testing.T) {
					if _, err := db.Exec("UPDATE local_"+kind+"_generations SET compatibility_report=? WHERE list_id=972", raw); err != nil {
						t.Fatal(err)
					}
					for _, view := range []string{"detail", "list"} {
						rr := httptest.NewRecorder()
						req := httptest.NewRequest(http.MethodGet, "/api/"+store.lists, nil)
						if view == "detail" {
							handleAPIListCompatibility(rr, req, kind, 972)
						} else if kind == "blocklist" {
							handleAPIGetBlocklists(rr, req)
						} else {
							handleAPIGetAllowlists(rr, req)
						}
						if rr.Code != http.StatusServiceUnavailable {
							t.Fatalf("%s corrupt report status=%d body=%s", view, rr.Code, rr.Body.String())
						}
					}
				})
			}
		})
	}
}

func TestCompatibilityValidEmptyAssessment(t *testing.T) {
	raw := `{"version":1,"assessed_at":"2026-09-29T19:00:00Z","lines":0,"applied":0,"unsupported":0,"invalid":0,"diagnostics":[]}`
	report, err := decodeListCompatibilityReport(raw)
	if err != nil || report == nil || !compatibilitySummary(report).Assessed {
		t.Fatalf("valid empty report=%+v err=%v", report, err)
	}
}
