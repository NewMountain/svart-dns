package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIReadFailuresNeverSucceed(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		fault   string
	}{
		{"analysis domain aggregate", handleAPIAnalysisDomains, "/api/analysis/domains?source=top-queried", `DROP TABLE query_logs`},
		{"query log detail lookup", handleAPIQueryLogDetail, "/api/query-logs/900", `DROP TABLE query_logs`},
		{"policy assigned entities", func(w http.ResponseWriter, r *http.Request) { handleAPIGetPolicyDetail(w, r, 900) }, "/api/policies/900", `INSERT INTO policies(id,name,description) VALUES(900,'Fixture',''); DROP TABLE ip_ranges`},
		{"client detail aggregate", handleAPIClient, "/api/clients/192.0.2.10", `DROP TABLE query_logs`},
		{"group detail aggregate", handleAPIGroupAction, "/api/groups/900", `INSERT INTO client_groups(id,name) VALUES(900,'Fixture'); DROP TABLE query_logs`},
		{"range detail aggregate", handleAPIRangeAction, "/api/ranges/900", `INSERT INTO ip_ranges(id,name,cidr) VALUES(900,'Fixture','192.0.2.0/24'); DROP TABLE query_logs`},
		{"missing group storage", handleAPIGroupAction, "/api/groups/900", `DROP TABLE client_groups`},
		{"missing policy storage", func(w http.ResponseWriter, r *http.Request) { handleAPIGetPolicyDetail(w, r, 900) }, "/api/policies/900", `DROP TABLE policies`},
		{"timezone failure", handleAPIQueryLogs, "/api/query-logs", `DROP TABLE settings`},
		{"range filter failure", handleAPIQueryLogs, "/api/query-logs?range_id=900", `DROP TABLE ip_ranges`},
		{"dashboard timeseries scan", handleAPIStatsTimeseries, "/api/stats/timeseries", `INSERT INTO query_logs(client_ip,query_name,query_type,timestamp) VALUES('192.0.2.10','example.com','A','z-invalid')`},
		{"summary aggregate", handleAPIStats, "/api/stats", `DROP TABLE query_logs`},
		{"blocklists iteration", handleAPIGetBlocklists, "/api/blocklists", `ALTER TABLE blocklists RENAME TO saved_blocklists; CREATE VIEW blocklists AS SELECT id,url,CASE WHEN id=900 THEN json_extract('invalid','$') ELSE alias END AS alias,enabled,domain_count,last_updated,refresh_interval FROM saved_blocklists`},
		{"rewrite scan", handleAPIGetRewrites, "/api/rewrites", `INSERT INTO rewrites(domain,target,ip_addresses,enabled) VALUES('broken.example','','192.0.2.1','not-a-boolean')`},
		{"history scan", handleAPIBlocklistHistory, "/api/blocklists/history", `UPDATE blocklist_history SET new_count='not-a-number' WHERE id=901`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer setupTestDB(t)()
			seedCheckpointContract(t)
			if _, err := db.Exec(tc.fault); err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			tc.handler(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != http.StatusServiceUnavailable {
				t.Fatalf("failed read returned %d %s", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), `"error_code":"unavailable"`) || !strings.Contains(rr.Body.String(), `"data":null`) {
				t.Fatalf("error contract missing: %s", rr.Body.String())
			}
			if strings.Contains(rr.Body.String(), "no such") || strings.Contains(rr.Body.String(), "converting") {
				t.Fatalf("internal database details leaked: %s", rr.Body.String())
			}
		})
	}
}

func TestConflictReadFailureCannotAdmitRule(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec(`DROP TABLE allowed_domains`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/clients/192.0.2.10/block-domain", strings.NewReader(`{"domain":"ads.example.com"}`))
	handleAPIClientBlockDomain(rr, req, "192.0.2.10", []string{"192.0.2.10", "block-domain"})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM blocked_domains WHERE domain='ads.example.com'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("admitted %d rules after conflict lookup failed", count)
	}
}

func TestLoginDatabaseUnavailableDoesNotBecomeBadCredentials(t *testing.T) {
	defer setupTestDB(t)()
	hasUsers.Store(true)
	if _, err := db.Exec(`DROP TABLE admin_users`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handleAPIAuthLogin(rr, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"operator","password":"fixture-only-password"}`)))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDashboardReadDoesNotReserveTwoConnections(t *testing.T) {
	for _, handler := range []http.HandlerFunc{handleAPIStatsTimeseries, handleAPIStatsLatency} {
		t.Run("bounded pool", func(t *testing.T) {
			defer setupTestDB(t)()
			readDB.SetMaxOpenConns(1)
			rr := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				handler(rr, httptest.NewRequest(http.MethodGet, "/api/stats/timeseries", nil))
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			select {
			case <-done:
				if rr.Code != 200 {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			case <-ctx.Done():
				readDB.SetMaxOpenConns(2)
				if err := readDB.Ping(); err != nil {
					t.Fatal(err)
				}
				<-done
				t.Fatal("one dashboard read deadlocked waiting for a second connection")
			}
		})
	}
}

func TestRevokeTokenDistinguishesMissingAndUnavailable(t *testing.T) {
	defer setupTestDB(t)()
	request := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handleAPITokenAction(rr, httptest.NewRequest(http.MethodDelete, "/api/tokens/999", nil))
		return rr
	}
	if rr := request(); rr.Code != 404 || !strings.Contains(rr.Body.String(), `"error":"token not found"`) {
		t.Fatalf("missing token: %d %s", rr.Code, rr.Body.String())
	}
	if _, err := db.Exec(`DROP TABLE api_tokens`); err != nil {
		t.Fatal(err)
	}
	if rr := request(); rr.Code != 503 || !strings.Contains(rr.Body.String(), `"error_code":"unavailable"`) {
		t.Fatalf("unavailable token: %d %s", rr.Code, rr.Body.String())
	}
}
