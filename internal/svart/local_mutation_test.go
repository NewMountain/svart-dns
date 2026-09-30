package svart

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestLocalDeletionTombstoneFailure(t *testing.T) {
	for _, fault := range []string{"ABORT", "IGNORE"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			id, err := createGroup("Family")
			if err != nil {
				t.Fatal(err)
			}
			if err := addClientToGroup("192.0.2.42", fmt.Sprint(id)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER reject_tomb BEFORE INSERT ON sync_tombstones WHEN NEW.table_name='client_group_members' BEGIN SELECT RAISE(%s%s); END`, fault, map[string]string{"ABORT": ", 'injected tombstone failure'"}[fault])); err != nil {
				t.Fatal(err)
			}
			if err := deleteGroup(int(id)); err == nil {
				t.Error("delete succeeded despite rejected relationship tombstone")
			}
			var groups, members, tombs int
			if err := db.QueryRow("SELECT COUNT(*) FROM client_groups WHERE id=?", id).Scan(&groups); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			if err := db.QueryRow("SELECT COUNT(*) FROM client_group_members WHERE group_id=?", id).Scan(&members); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			if err := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones").Scan(&tombs); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
			if groups != 1 || members != 1 || tombs != 0 {
				t.Fatalf("failed deletion changed state: groups=%d members=%d tombs=%d", groups, members, tombs)
			}
		})
	}
}

func TestLocalAliasCacheReadFailure(t *testing.T) {
	defer setupTestDB(t)()
	old := map[string]string{"192.0.2.42": "Family laptop", "192.0.2.43": "Family phone"}
	clientAliasCache.Store(old)
	for _, statement := range []string{"ALTER TABLE client_aliases RENAME TO original_aliases", `CREATE VIEW client_aliases AS SELECT '192.0.2.44' ip_address,'New client' alias UNION ALL SELECT '192.0.2.45',json_extract('invalid','$')`} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := loadClientAliasCache(); err == nil || !strings.Contains(err.Error(), "malformed JSON") {
		t.Fatalf("alias read failure=%v", err)
	}
	if got := clientAliasCache.Load(); !reflect.DeepEqual(got, old) {
		t.Fatalf("read failure replaced complete cache: %#v", got)
	}
}

func TestLocalAuthCacheReadFailure(t *testing.T) {
	defer setupTestDB(t)()
	hasUsers.Store(true)
	hasTokens.Store(true)
	if _, err := db.Exec("DROP TABLE api_tokens"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := refreshAuthCaches(); err == nil || !strings.Contains(err.Error(), "no such table: api_tokens") {
		t.Fatalf("authentication cache read failure=%v", err)
	}
	if !hasUsers.Load() || !hasTokens.Load() {
		t.Fatal("read failure published empty authentication state")
	}
}

func TestLocalManualRuleInsertFailure(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec(`CREATE TRIGGER reject_rule BEFORE INSERT ON blocked_domains BEGIN SELECT RAISE(ABORT,'injected rule failure'); END`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/clients/192.0.2.42/block-domain", strings.NewReader(`{"domain":"ads.example.com"}`))
	handleAPIClientBlockDomain(rr, req, "192.0.2.42", []string{"192.0.2.42", "block-domain"})
	if rr.Code < 400 {
		t.Fatalf("expected failure, got %d %s", rr.Code, rr.Body.String())
	}
	var lists, assignments int
	if err := db.QueryRow("SELECT COUNT(*) FROM blocklists").Scan(&lists); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM client_blocklists").Scan(&assignments); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if lists != 0 || assignments != 0 {
		t.Fatalf("failed rule left list=%d assignment=%d", lists, assignments)
	}
}

func TestLocalSemanticValidation(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		handler          http.HandlerFunc
	}{
		{"group name", "/api/groups", `{"name":"  "}`, handleAPIGroups},
		{"rewrite batch", "/api/rewrites/batch", `{"rewrites":[{"domain":"home.example.com","ip_addresses":"192.0.2.42"},{"domain":"","ip_addresses":"invalid"}]}`, handleAPIRewritesBatch},
		{"range id", "/api/ranges/1trailing", `{}`, handleAPIRangeAction},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer setupTestDB(t)()
			rr := httptest.NewRecorder()
			method := "POST"
			if tc.name == "range id" {
				method = "PUT"
			}
			tc.handler(rr, httptest.NewRequest(method, tc.path, strings.NewReader(tc.body)))
			if rr.Code != 400 {
				t.Fatalf("got %d %s, want 400", rr.Code, rr.Body.String())
			}
		})
	}
}
