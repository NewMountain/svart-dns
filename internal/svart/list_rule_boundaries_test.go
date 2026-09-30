package svart

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/listparse"
)

func boundaryTypedRule(t *testing.T) (string, string) {
	t.Helper()
	const text = `/^ads\D+\.example$/$dnstype=TXT,important`
	encoded, err := listparse.EncodeStored(listparse.Rule{Version: 1, Text: text, Pattern: `^ads\D+\.example$`, Kind: listparse.KindRegex, DNSTypes: []uint16{16}, Important: true})
	if err != nil {
		t.Fatal(err)
	}
	return encoded, text
}

func TestTypedRuleListPagesShowSourceAndSearchSourceOnly(t *testing.T) {
	defer setupTestDB(t)()
	encoded, text := boundaryTypedRule(t)
	localSQL(t, `INSERT INTO blocklists(id,url,alias) VALUES(777,'https://lists.example/block','Block'); INSERT INTO allowlists(id,url,alias) VALUES(778,'https://lists.example/allow','Allow')`)
	localSQL(t, `INSERT INTO blocked_domains(blocklist_id,domain) VALUES(777,?)`, encoded)
	localSQL(t, `INSERT INTO allowed_domains(allowlist_id,domain) VALUES(778,?)`, encoded)
	for _, tc := range []struct {
		path    string
		handler http.HandlerFunc
		want    []string
		total   int
	}{
		{"/api/blocklists/777/domains", handleAPIBlocklistAction, []string{text}, 1},
		{"/api/allowlists/778/domains", handleAPIAllowlistAction, []string{text}, 1},
		{"/api/blocklists/777/domains?search=version", handleAPIBlocklistAction, []string{}, 0},
		{"/api/blocklists/777/domains?search=" + url.QueryEscape(text), handleAPIBlocklistAction, []string{text}, 1},
	} {
		w := httptest.NewRecorder()
		tc.handler(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != 200 {
			t.Fatalf("%s status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		got := decodeAPIData[struct {
			Domains []string `json:"domains"`
			Total   int      `json:"total"`
		}](t, w)
		if !reflect.DeepEqual(got.Domains, tc.want) || got.Total != tc.total {
			t.Errorf("%s got=%+v want=%v total=%d", tc.path, got, tc.want, tc.total)
		}
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", 777); !reflect.DeepEqual(got, []string{encoded}) {
		t.Fatalf("presentation mutated stored rules: %v", got)
	}
}

func TestTypedCheckpointPreservesIdentityAndDisplaysOriginalText(t *testing.T) {
	defer setupTestDB(t)()
	encoded, text := boundaryTypedRule(t)
	localSQL(t, `INSERT INTO blocklists(id,url,alias) VALUES(777,'https://lists.example/block','Block'); INSERT INTO blocklist_history(id,blocklist_id) VALUES(901,777),(902,777); INSERT INTO blocked_domains(blocklist_id,domain) VALUES(777,'current.example'); INSERT INTO blocklist_changelog(history_id,domain,action) VALUES(902,'current.example','added')`)
	localSQL(t, `INSERT INTO blocklist_changelog(history_id,domain,action) VALUES(902,?,'removed')`, encoded)
	for _, tc := range []struct {
		search string
		want   []string
		total  int
	}{{"", []string{text}, 1}, {"version", []string{}, 0}, {text, []string{text}, 1}} {
		got, err := queryCheckpointPage(context.Background(), 901, tc.search, 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Domains, tc.want) || got.Total != tc.total {
			t.Errorf("search%q got=%+v want=%v total=%d", tc.search, got, tc.want, tc.total)
		}
	}
	var raw string
	if err := db.QueryRow("SELECT domain FROM blocklist_changelog WHERE action='removed'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw != encoded {
		t.Fatalf("checkpoint identity changed: %q", raw)
	}
}

func TestReservedTypedRulesCannotEnterDisabledManualImports(t *testing.T) {
	defer setupTestDB(t)()
	encoded, _ := boundaryTypedRule(t)
	for _, rule := range []string{encoded, "!svart-rule-v2:{}", " !SVART-RULE-v1:{} "} {
		body, err := json.Marshal(ConfigExport{Blocklists: []ListExport{{Alias: "Imported", URL: "", Enabled: false, Domains: []string{rule}}}})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handleAPIConfigImport(w, httptest.NewRequest(http.MethodPost, "/api/config/import", bytes.NewReader(body)))
		if w.Code != 400 || !strings.Contains(w.Body.String(), "reserved encoded rule") {
			t.Errorf("status=%d body=%s", w.Code, w.Body.String())
		}
		assertSyncCount(t, "SELECT count(*) FROM blocklists WHERE alias='Imported'", 0)
	}
}

func TestSyncRejectsEncodedManualRulesBeforeMutation(t *testing.T) {
	encoded, _ := boundaryTypedRule(t)
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	resp := &SyncResponse{Changes: SyncChanges{BlockedDomains: []SyncManualDomain{{ListAlias: "Manual", Domain: encoded, UpdatedAt: "2026-09-28T12:00:00Z"}}, AllowedDomains: []SyncManualDomain{{ListAlias: "Manual", Domain: "!svart-rule-v2:{}", UpdatedAt: "2026-09-28T12:00:00Z"}}}}
	got, rejected := admitSyncResponse(resp, now, false)
	if len(got.Changes.BlockedDomains) != 0 || len(got.Changes.AllowedDomains) != 0 || !reflect.DeepEqual(rejected, []syncRejection{{Table: "blocked_domains", Reason: rejectInvalidRow}, {Table: "allowed_domains", Reason: rejectInvalidRow}}) {
		t.Fatalf("admitted=%+v rejected=%+v", got.Changes, rejected)
	}
	if resp.Changes.BlockedDomains[0].Domain != encoded {
		t.Fatal("admission mutated original recoverable payload")
	}
}

func TestSyncManualRowsCannotModifyDownloadedLists(t *testing.T) {
	for _, allow := range []bool{false, true} {
		t.Run(map[bool]string{false: "block", true: "allow"}[allow], func(t *testing.T) {
			defer setupTestDB(t)()
			localSQL(t, `INSERT INTO blocklists(alias,url,updated_at) VALUES('Downloaded','https://lists.example/block','2020-01-01T00:00:00Z'); INSERT INTO allowlists(alias,url,updated_at) VALUES('Downloaded','https://lists.example/allow','2020-01-01T00:00:00Z')`)
			row := SyncManualDomain{ListAlias: "Downloaded", Domain: "injected.example", UpdatedAt: "2021-01-01T00:00:00Z"}
			changes := SyncChanges{BlockedDomains: []SyncManualDomain{row}}
			if allow {
				changes = SyncChanges{AllowedDomains: []SyncManualDomain{row}}
			}
			before := localStoredState(t)
			if err := mergeSyncResponse(&SyncResponse{Changes: changes}); err == nil {
				t.Fatal("manual sync row changed downloaded list")
			}
			if after := localStoredState(t); !reflect.DeepEqual(after, before) {
				t.Fatal("rejected merge changed stored state")
			}
		})
	}
}
