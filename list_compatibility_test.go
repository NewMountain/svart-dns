package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

func TestCompatibilityPaginationKeepsCompleteSourceRules(t *testing.T) {
	defer setupTestDB(t)()
	report := ListCompatibilityReport{Version: 1, AssessedAt: "2026-09-29T19:00:00Z", Lines: 57, Applied: 3, Unsupported: 53, Invalid: 1}
	for i := range 54 {
		report.Diagnostics = append(report.Diagnostics, listparse.Diagnostic{Line: i + 4, Rule: fmt.Sprintf("||track%d.example^$client=~192.0.2.7", i), Reason: "unsupported modifier: client"})
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO blocklists(id,url,alias,enabled) VALUES(901,'https://example.com/list.txt','compatibility',1); INSERT INTO local_blocklist_generations(list_id,url,compatibility_report) VALUES(901,'https://example.com/list.txt',?)`, string(raw)); err != nil {
		t.Fatal(err)
	}
	var all []listparse.Diagnostic
	for _, offset := range []int{0, 50} {
		rr := httptest.NewRecorder()
		handleAPIBlocklistAction(rr, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/blocklists/901/compatibility?offset=%d&limit=50", offset), nil))
		if rr.Code != 200 {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
		var response struct {
			Data ListCompatibilityPage `json:"data"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Data.Total != 54 || !response.Data.Summary.Assessed || response.Data.Summary.Applied != 3 || response.Data.Offset != offset {
			t.Fatalf("page=%+v", response.Data)
		}
		all = append(all, response.Data.Diagnostics...)
	}
	if !reflect.DeepEqual(all, report.Diagnostics) {
		t.Fatalf("diagnostics differ: got=%+v want=%+v", all, report.Diagnostics)
	}
	for _, suffix := range []string{"?offset=-1", "?limit=0", "?limit=1001"} {
		rr := httptest.NewRecorder()
		handleAPIBlocklistAction(rr, httptest.NewRequest(http.MethodGet, "/api/blocklists/901/compatibility"+suffix, nil))
		if rr.Code != 400 {
			t.Fatalf("%s: %d", suffix, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	handleAPIBlocklistAction(rr, httptest.NewRequest(http.MethodGet, "/api/blocklists/900009/compatibility", nil))
	if rr.Code != 404 {
		t.Fatalf("unknown list: %d", rr.Code)
	}
}

func TestCompatibilityLegacyAndUnreadableReportStayDistinct(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec(`INSERT INTO allowlists(id,url,alias,enabled) VALUES(902,'https://example.com/allow.txt','legacy',1)`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handleAPIAllowlistAction(rr, httptest.NewRequest(http.MethodGet, "/api/allowlists/902/compatibility", nil))
	if rr.Code != 200 {
		t.Fatalf("legacy=%d", rr.Code)
	}
	var response struct {
		Data ListCompatibilityPage `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Summary.Assessed || len(response.Data.Diagnostics) != 0 {
		t.Fatalf("legacy=%+v", response)
	}
	if _, err := db.Exec(`INSERT INTO local_allowlist_generations(list_id,url,compatibility_report) VALUES(902,'https://example.com/allow.txt','invalid-json')`); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	handleAPIAllowlistAction(rr, httptest.NewRequest(http.MethodGet, "/api/allowlists/902/compatibility", nil))
	if rr.Code != 503 {
		t.Fatalf("corrupt=%d %s", rr.Code, rr.Body.String())
	}
}
