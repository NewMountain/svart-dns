package svart

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchiveStatusAvailability(t *testing.T) {
	for _, tc := range []string{"absent", "empty", "not-directory", "broken-storage", "invalid-timestamp"} {
		t.Run(tc, func(t *testing.T) {
			defer setupTestDB(t)()
			previous := archivePath
			defer func() { archivePath = previous }()
			root := t.TempDir()
			archivePath = filepath.Join(root, "archive")
			switch tc {
			case "empty":
				if err := os.Mkdir(archivePath, 0700); err != nil {
					t.Fatal(err)
				}
			case "not-directory":
				if err := os.WriteFile(archivePath, []byte("fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			case "broken-storage":
				if _, err := db.Exec(`DROP TABLE query_logs`); err != nil {
					t.Fatal(err)
				}
			case "invalid-timestamp":
				if _, err := db.Exec(`INSERT INTO query_logs(client_ip,query_name,query_type,timestamp) VALUES('192.0.2.1','example.com','A','invalid')`); err != nil {
					t.Fatal(err)
				}
			}
			rr := httptest.NewRecorder()
			handleAPIArchiveStatus(rr, httptest.NewRequest(http.MethodGet, "/api/archive/status", nil))
			if tc == "absent" || tc == "empty" {
				if rr.Code != 200 {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
				var result APIResponse[ArchiveStatusView]
				if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Data.LiveRows != 0 || result.Data.ArchiveFiles == nil || len(result.Data.ArchiveFiles) != 0 || result.Data.ArchivePath != archivePath {
					t.Fatalf("unexpected empty status: %+v", result)
				}
			} else {
				want := "{\"data\":null,\"error\":\"Data unavailable; retry the request\",\"error_code\":\"unavailable\"}\n"
				if rr.Code != 503 || rr.Body.String() != want {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			}
		})
	}
}

func TestTypedBodyRejectsNullWrongTypeAndTrailingValues(t *testing.T) {
	for _, body := range []string{"null", `{"domain":false}`, `{"domain":"example.com"} {}`, `[]`} {
		rr := httptest.NewRecorder()
		var request ClientBlockDomainRequest
		if decodeJSONBody(rr, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(body)), 1024, &request) {
			t.Fatalf("accepted %s", body)
		}
		if rr.Code != 400 || !strings.Contains(rr.Body.String(), `"error_code":"invalid_request"`) {
			t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
		}
	}
}

func TestTypedOptionalFieldsPreserveExplicitFalse(t *testing.T) {
	row := QueryLogView{Blocked: false, ResultIsPublished: apiPtr(false)}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"result_is_published":false`) || strings.Contains(string(encoded), `"result_reason"`) {
		t.Fatalf("optional field contract: %s", encoded)
	}
	encoded, err = json.Marshal(APIResponse[[]PolicyView]{Data: []PolicyView{}})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"data":[],"error":null}` {
		t.Fatalf("empty array envelope: %s", encoded)
	}
}

func TestTypedBodyPreservesUnknownFieldCompatibility(t *testing.T) {
	rr := httptest.NewRecorder()
	var request ClientBlockDomainRequest
	body := `{"domain":"example.com","future":{"nested":[1,true,null]},"another_field":"kept-compatible"}`
	if !decodeJSONBody(rr, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(body)), 1024, &request) || request.Domain != "example.com" {
		t.Fatalf("unknown fields rejected or known field changed: status=%d request=%+v", rr.Code, request)
	}
}

func TestTypedBodyOptionalNullUsesZeroValue(t *testing.T) {
	rr := httptest.NewRecorder()
	var request UpdateClientAliasRequest
	if !decodeJSONBody(rr, httptest.NewRequest(http.MethodPut, "/fixture", strings.NewReader(`{"alias":null}`)), 1024, &request) || request.Alias != "" {
		t.Fatalf("optional null: status=%d request=%+v", rr.Code, request)
	}
}
