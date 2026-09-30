package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONHandlerRejectsTrailingDocumentsBeforeWork(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		handler          http.HandlerFunc
	}{
		{"investigation", "/api/investigate", `{"sql":"SELECT 1"} {}`, handleAPIInvestigate},
		{"peer add", "/api/peers", `{"url":"https://example.com"} {}`, handleAPIPeersPost},
		{"peer confirm", "/api/peers/confirm", `{"peer_url":"https://example.com","pairing_code":"fixture"} {}`, handleAPIPeersConfirm},
		{"pair complete", "/api/sync/pair/complete", `{"peer_url":"https://example.com","pairing_code":"fixture","proof":"fixture"} {}`, handleAPISyncPairComplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer setupTestDB(t)()
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			req.TLS = &tls.ConnectionState{}
			tc.handler(rr, req)
			if rr.Code != 400 || !strings.Contains(rr.Body.String(), "expected exactly one JSON value") {
				t.Fatalf("trailing JSON reached work: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}
