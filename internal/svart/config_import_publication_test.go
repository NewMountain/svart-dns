package svart

import (
	"github.com/miekg/dns"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestImportSnapshotFailuresPreserveDatabaseAndRuntime(t *testing.T) {
	for _, fault := range []string{"upstreams", "rewrites", "policy"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			localSQL(t, `INSERT INTO upstreams(upstream,enabled) VALUES('192.0.2.53:53',1); INSERT INTO rewrites(domain,target,ip_addresses,enabled) VALUES('printer.example.com','','192.0.2.20',1)`)
			if err := loadUpstreamsFromDB(); err != nil {
				t.Fatal(err)
			}
			if err := loadRewritesFromDB(); err != nil {
				t.Fatal(err)
			}
			mustReloadPolicy(t)
			switch fault {
			case "upstreams":
				localSQL(t, `UPDATE upstreams SET enabled='not-a-boolean'`)
			case "rewrites":
				localSQL(t, `DROP TABLE rewrites`)
			case "policy":
				localSQL(t, `DROP TABLE client_policies`)
			}
			before := localStoredState(t)
			policy, rewrites, upstreams := policyState.Load(), rewritesCache.Load(), upstreamStorage.snap.Load()
			settings, logging := getRuntimeSettings(), loggingEnabled.Load()
			response := httptest.NewRecorder()
			handleAPIConfigImport(response, httptest.NewRequest("POST", "/api/config/import", strings.NewReader(`{"settings":{"cache_ttl":"7200"},"clients":[{"ip":"192.0.2.25","alias":"Office laptop"}]}`)))
			if response.Code != 500 {
				t.Errorf("failed snapshot returned status=%d body=%s; want 500", response.Code, response.Body.String())
			}
			if after := localStoredState(t); !reflect.DeepEqual(after, before) {
				t.Error("failed import changed the complete persistent preimage")
			}
			if policyState.Load() != policy || rewritesCache.Load() != rewrites || upstreamStorage.snap.Load() != upstreams || getRuntimeSettings() != settings || loggingEnabled.Load() != logging {
				t.Error("failed import published partial runtime state")
			}
		})
	}
}

func TestImportPublishesCompleteRuntimeAndRetryKeepsLogicalState(t *testing.T) {
	defer setupTestDB(t)()
	const body = `{"upstreams":[{"upstream":"192.0.2.53:53","enabled":true}],"rewrites":[{"domain":"printer.example.com","ip_addresses":"192.0.2.20","enabled":true}],"settings":{"cache_ttl":"7200","logging_enabled":"false"},"blocklists":[{"alias":"Office rules","url":"","enabled":true,"domains":["ads.example.com"]}],"clients":[{"ip":"192.0.2.25","alias":"Office laptop","blocklists":["Office rules"]}]}`
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		handleAPIConfigImport(response, httptest.NewRequest("POST", "/api/config/import", strings.NewReader(body)))
		if response.Code != 200 || response.Body.String() != "{\"data\":{\"success\":true},\"error\":null}\n" {
			t.Fatalf("import %d: status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if got := upstreamStorage.getAll(); !reflect.DeepEqual(got, []Upstream{{ID: 1, Upstream: "192.0.2.53:53", Enabled: true}}) {
			t.Fatalf("live upstreams=%#v", got)
		}
		if got := getClientAliasCached("192.0.2.25"); got != "Office laptop" {
			t.Fatalf("live alias=%q", got)
		}
		if getRuntimeSettings().cacheTTL != 7200 || loggingEnabled.Load() {
			t.Fatalf("runtime TTL=%d logging=%v", getRuntimeSettings().cacheTTL, loggingEnabled.Load())
		}
		query := new(dns.Msg)
		query.SetQuestion("printer.example.com.", dns.TypeA)
		reply := checkRewrite(query)
		if reply == nil || len(reply.Answer) != 1 || reply.Answer[0].String() != "printer.example.com.\t300\tIN\tA\t192.0.2.20" {
			t.Fatalf("rewrite reply=%v", reply)
		}
		decision := evaluatePolicy("192.0.2.25", "ads.example.com.", 1)
		if decision.Result != "block" {
			t.Fatalf("imported client rule decision=%#v", decision)
		}
		var rules, upstreams, rewrites int
		if err := db.QueryRow("SELECT (SELECT COUNT(*) FROM blocked_domains),(SELECT COUNT(*) FROM upstreams),(SELECT COUNT(*) FROM rewrites)").Scan(&rules, &upstreams, &rewrites); err != nil {
			t.Fatal(err)
		}
		if rules != 1 || upstreams != 1 || rewrites != 1 {
			t.Fatalf("retry row counts=%d,%d,%d want1,1,1", rules, upstreams, rewrites)
		}
	}
}
