package svart

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestImportRollsBackSQLFailure(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec(`CREATE TRIGGER fail_import BEFORE INSERT ON client_aliases BEGIN SELECT RAISE(ABORT, 'injected write failure'); END`); err != nil {
		t.Fatal(err)
	}
	body, fixtureErr344 := json.Marshal(ConfigExport{Upstreams: []UpstreamExport{{Upstream: "192.0.2.53:53", Enabled: true}}, Clients: []ClientExport{{IP: "192.0.2.1", Alias: "printer"}}})
	if fixtureErr344 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr344)
	}
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewReader(body)))
	if w.Code != 500 {
		t.Errorf("status = %d, want 500; body=%s", w.Code, w.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream='192.0.2.53:53'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("partially imported upstream count = %d, want 0", count)
	}
}

func TestImportValidatesEntirePayload(t *testing.T) {
	for _, config := range []ConfigExport{
		{Ranges: []RangeExport{{Name: "invalid", CIDR: "not-a-network"}}},
		{Clients: []ClientExport{{IP: "not-an-address", Alias: "printer"}}},
		{Groups: []GroupExport{{Name: "workstations", Members: []string{"invalid-ip"}}}},
		{Policies: []PolicyExport{{Name: "unknown-list", Blocklists: []string{"missing-list"}}}},
		{Bootstrap: []string{"not a server"}},
		{Upstreams: []UpstreamExport{{Upstream: "bogus://invalid", Enabled: true}}},
	} {
		t.Run("reject", func(t *testing.T) {
			defer setupTestDB(t)()
			config.Settings = map[string]string{"log_retention_days": "40"}
			body, fixtureErr1648 := json.Marshal(config)
			if fixtureErr1648 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr1648)
			}
			w := httptest.NewRecorder()
			handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewReader(body)))
			if w.Code != 400 {
				t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			var value string
			if err := db.QueryRow("SELECT value FROM settings WHERE key='log_retention_days'").Scan(&value); err != nil {
				t.Fatal(err)
			}
			if value != "1095" {
				t.Errorf("partial setting mutation = %q, want 1095", value)
			}
		})
	}
}

func TestConfigRoundTripKeepsManualRules(t *testing.T) {
	cleanup := setupTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO blocklists(alias,url,enabled) VALUES('Custom rules','',0)`,
		`INSERT INTO blocked_domains(blocklist_id,domain) SELECT id,'ads.example' FROM blocklists WHERE alias='Custom rules'`,
		`INSERT INTO allowlists(alias,url,enabled) VALUES('Custom exceptions','',1)`,
		`INSERT INTO allowed_domains(allowlist_id,domain) SELECT id,'login.example' FROM allowlists WHERE alias='Custom exceptions'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	exported, err := buildConfigExport()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	defer setupTestDB(t)()
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("import=%d %s", w.Code, w.Body.String())
	}
	for _, want := range []struct{ query, domain string }{{"SELECT domain FROM blocked_domains", "ads.example"}, {"SELECT domain FROM allowed_domains", "login.example"}} {
		var got string
		if err := db.QueryRow(want.query).Scan(&got); err != nil {
			t.Errorf("manual rule missing: %v", err)
		}
		if got != want.domain {
			t.Errorf("rule=%q, want %q", got, want.domain)
		}
	}
	var enabled bool
	if err := db.QueryRow("SELECT enabled FROM blocklists WHERE alias='Custom rules'").Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Error("disabled manual list became enabled")
	}
}

func TestConfigExportReportsAssignmentReadFailure(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("DROP TABLE policy_blocklists"); err != nil {
		t.Fatal(err)
	}
	export, err := buildConfigExport()
	if err == nil || export != nil {
		t.Fatalf("incomplete export=%v error=%v, want error and no export", export, err)
	}
}

func TestManualRuleImportMergesOmittedAndEmptyDomains(t *testing.T) {
	defer setupTestDB(t)()
	for _, body := range []string{
		`{"blocklists":[{"alias":"Custom rules","url":"","enabled":false,"domains":["ads.example","*tracking.example"]}]}`,
		`{"blocklists":[{"alias":"Custom rules","url":"","enabled":false}]}`,
		`{"blocklists":[{"alias":"Custom rules","url":"","enabled":false,"domains":[]}]}`,
		`{"blocklists":[{"alias":"Custom rules","url":"","enabled":false,"domains":["ads.example"]}]}`,
	} {
		w := httptest.NewRecorder()
		handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewBufferString(body)))
		if w.Code != 200 {
			t.Fatalf("import=%d %s", w.Code, w.Body.String())
		}
		var got string
		if err := db.QueryRow("SELECT group_concat(domain, ',') FROM (SELECT domain FROM blocked_domains ORDER BY domain)").Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != "*tracking.example,ads.example" {
			t.Errorf("merged rules=%q, want both original rules", got)
		}
		var enabled bool
		if err := db.QueryRow("SELECT enabled FROM blocklists WHERE alias='Custom rules'").Scan(&enabled); err != nil {
			t.Fatal(err)
		}
		if enabled {
			t.Error("disabled list was enabled during merge")
		}
	}
}

func TestConfigRoundTripKeepsNamedDisabledAssignments(t *testing.T) {
	cleanup := setupTestDB(t)
	body := `{"blocklists":[{"alias":"Paused blockers","url":"","enabled":false,"domains":["ads.example"]}],"allowlists":[{"alias":"Paused exceptions","url":"","enabled":false,"domains":["login.example"]}],"policies":[{"name":"Devices","blocklists":["Paused blockers"],"allowlists":["Paused exceptions"]}],"groups":[{"name":"Laptops","policy":"Devices","members":["192.0.2.10"],"blocklists":["Paused blockers"],"allowlists":["Paused exceptions"]}],"ranges":[{"name":"Office","cidr":"192.0.2.0/24","policy":"Devices","blocklists":["Paused blockers"],"allowlists":["Paused exceptions"]}],"clients":[{"ip":"192.0.2.10","alias":"Laptop","policy":"Devices","groups":["Laptops"],"blocklists":["Paused blockers"],"allowlists":["Paused exceptions"]}]}`
	w := httptest.NewRecorder()
	handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewBufferString(body)))
	if w.Code != 200 {
		t.Fatalf("seed=%d %s", w.Code, w.Body.String())
	}
	export, err := buildConfigExport()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	defer setupTestDB(t)()
	w = httptest.NewRecorder()
	handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewReader(encoded)))
	if w.Code != 200 {
		t.Fatalf("restore=%d %s", w.Code, w.Body.String())
	}
	restored, err := buildConfigExport()
	if err != nil {
		t.Fatal(err)
	}
	// Export metadata changes with time; the complete user-owned configuration
	// must round-trip exactly, including disabled list assignments and names.
	restored.ExportedAt = export.ExportedAt
	if !reflect.DeepEqual(restored, export) {
		t.Fatalf("restored config=%#v, want %#v", restored, export)
	}
}
