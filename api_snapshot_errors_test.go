package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSnapshotReadFailuresUseSafeUnavailableEnvelope(t *testing.T) {
	for _, test := range []struct {
		name, path string
		handler    http.HandlerFunc
	}{
		{"configuration export", "/api/config/export", handleAPIConfigExport},
		{"peer sync snapshot", "/api/sync", handleAPISync},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer setupTestDB(t)()
			// A late component fails after earlier snapshot queries succeeded. The
			// response must contain neither partial configuration nor SQL diagnostics.
			if _, err := db.Exec(`DROP TABLE client_aliases`); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			test.handler(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			expected := "{\"data\":null,\"error\":\"Data unavailable; retry the request\",\"error_code\":\"unavailable\"}\n"
			if response.Code != 503 || response.Body.String() != expected {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
