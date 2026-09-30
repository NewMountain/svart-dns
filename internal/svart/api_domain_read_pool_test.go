package svart

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDomainPageDoesNotWaitForWriterConnection(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		handler    http.HandlerFunc
		seed       string
	}{
		{"blocklist", "/api/blocklists/900/domains", handleAPIBlocklistAction, `INSERT INTO blocklists(id,url,alias,enabled) VALUES(900,'https://example.com/list.txt','fixture',1); INSERT INTO blocked_domains(domain,blocklist_id) VALUES('ads.example.com',900)`},
		{"allowlist", "/api/allowlists/900/domains", handleAPIAllowlistAction, `INSERT INTO allowlists(id,url,alias,enabled) VALUES(900,'https://example.com/list.txt','fixture',1); INSERT INTO allowed_domains(domain,allowlist_id) VALUES('allowed.example.com',900)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer setupTestDB(t)()
			if _, err := db.Exec(tc.seed); err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			writer, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { checkTestClose(t, writer) }()
			rr := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { defer close(done); tc.handler(rr, httptest.NewRequest(http.MethodGet, tc.path, nil)) }()
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			select {
			case <-done:
				if rr.Code != 200 {
					t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
				}
			case <-timer.C:
				checkTestClose(t, writer)
				<-done
				t.Fatal("domain read waited for the reserved writer connection")
			}
		})
	}
}
