package svart

// Black-box regression tests for the September 2026 auth/HTTP API security
// review (design/SECURITY-REVIEW-2026-09.md, items A1–A4, A6–A12, S4). Every
// test drives the real admin mux built by startAdminServer over a loopback
// socket, so middleware ordering, routing, and auth wrappers are all in the
// path exactly as in production.

import (
	"fmt"

	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"testing"
)

func secListServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func secDomainCount(t *testing.T, listID int) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ?", listID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSecurityListFetchRefusesLoopbackByDefault(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	internal := secListServer(t, "internal-only-api-token-7f3a9c\n{\"db_password\":\"hunter2\"}\n")
	res, fixtureErr31070 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Looks Legit', 1)", internal.URL+"/secret")
	if fixtureErr31070 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr31070)
	}
	id, fixtureErr31190 := res.LastInsertId()
	if fixtureErr31190 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr31190)
	}
	err := refreshBlocklistByID(int(id))
	if err == nil || !strings.Contains(err.Error(), "ALLOW_PRIVATE_LIST_URLS") {
		t.Fatalf("refresh of loopback URL: err = %v; want refusal naming ALLOW_PRIVATE_LIST_URLS", err)
	}
	if n := secDomainCount(t, int(id)); n != 0 {
		t.Fatalf("loopback fetch stored %d rows", n)
	}
}

func TestSecurityListFetchRefusesRedirectToLoopback(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	internal := secListServer(t, "internal-only-api-token-7f3a9c\n")
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/secret", http.StatusFound)
	}))
	defer redirector.Close()
	// The redirect chain must never end in fetched loopback content. The
	// per-hop re-check with private destinations otherwise allowed is covered
	// by the list_refresh unit tests.
	res, fixtureErr32089 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Redirector', 1)", redirector.URL+"/list.txt")
	if fixtureErr32089 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr32089)
	}
	id, fixtureErr32212 := res.LastInsertId()
	if fixtureErr32212 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr32212)
	}
	if err := refreshBlocklistByID(int(id)); err == nil {
		t.Fatal("refresh through a redirect to loopback succeeded")
	}
	if n := secDomainCount(t, int(id)); n != 0 {
		t.Fatalf("redirected fetch stored %d rows", n)
	}
}

func TestSecurityListURLValidatedAtAPI(t *testing.T) {
	e := secSetup(t)
	hdr := map[string]string{"X-Api-Key": e.admToken, "Content-Type": "application/json"}
	bad := []string{
		"file:///etc/passwd",
		"gopher://127.0.0.1:6379/_FLUSHALL",
		"http://127.0.0.1:9090/metrics",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/list.txt",
		"https:///no-host",
	}
	for i, u := range bad {
		for _, path := range []string{"/api/blocklists", "/api/allowlists"} {
			status, _, resp := secDo(t, http.MethodPost, e.base+path, hdr,
				fmt.Sprintf(`{"url":%q,"alias":"Bad list %d","enabled":true}`, u, i))
			if status != http.StatusBadRequest {
				t.Errorf("POST %s url=%s: status %d, want 400; body %s", path, u, status, resp)
			}
		}
		status, _, resp := secDo(t, http.MethodPut, e.base+"/api/blocklists/1", hdr, fmt.Sprintf(`{"url":%q}`, u))
		if status != http.StatusBadRequest {
			t.Errorf("PUT /api/blocklists/1 url=%s: status %d, want 400; body %s", u, status, resp)
		}
	}
	var stored string
	if err := db.QueryRow("SELECT url FROM blocklists WHERE id = 1").Scan(&stored); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if !strings.HasPrefix(stored, "https://cdn.jsdelivr.net/") {
		t.Fatalf("rejected PUT still changed the stored URL to %q", stored)
	}
}

func TestSecurityListRefreshSizeAndLineLimitsKeepPreviousVersion(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	t.Setenv("LIST_MAX_LINES", "4")
	cleanup := setupTestDB(t)
	defer cleanup()

	body := "0.0.0.0 ads.example.com\n0.0.0.0 track.example.com\n"
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}))
	defer srv.Close()

	res, fixtureErr34291 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Local Mirror', 1)", srv.URL+"/hosts")
	if fixtureErr34291 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr34291)
	}
	id, fixtureErr34406 := res.LastInsertId()
	if fixtureErr34406 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr34406)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatalf("initial refresh: %v", err)
	}
	if n := secDomainCount(t, int(id)); n != 2 {
		t.Fatalf("initial refresh stored %d domains, want 2", n)
	}

	mu.Lock()
	body = "a.example.com\nb.example.com\nc.example.com\nd.example.com\ne.example.com\n"
	mu.Unlock()
	err := refreshBlocklistByID(int(id))
	if err == nil || !strings.Contains(err.Error(), "LIST_MAX_LINES") {
		t.Fatalf("over-long list: err = %v; want failure naming LIST_MAX_LINES", err)
	}
	var domains []string
	rows, fixtureErr34963 := db.Query("SELECT domain FROM blocked_domains WHERE blocklist_id = ? ORDER BY domain", id)
	if fixtureErr34963 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr34963)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		domains = append(domains, d)
	}
	checkTestClose(t, rows)
	if strings.Join(domains, ",") != "ads.example.com,track.example.com" {
		t.Fatalf("failed refresh replaced the previous version: %v", domains)
	}

	t.Setenv("LIST_MAX_LINES", "")
	t.Setenv("LIST_MAX_DOWNLOAD_BYTES", "16")
	err = refreshBlocklistByID(int(id))
	if err == nil || !strings.Contains(err.Error(), "LIST_MAX_DOWNLOAD_BYTES") {
		t.Fatalf("oversized list: err = %v; want failure naming LIST_MAX_DOWNLOAD_BYTES", err)
	}
	if n := secDomainCount(t, int(id)); n != 2 {
		t.Fatalf("oversized refresh changed the stored list: %d domains", n)
	}
}

func TestSecurityListRefreshRejectsNonHostnameLines(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()
	srv := secListServer(t, strings.Join([]string{
		"# Title: Local mirror",
		"0.0.0.0 ads.example.com",
		"||tracker.example.net^",
		"*.adserver.example.org",
		"internal-only-api-token-7f3a9c:hunter2",
		`{"db_password":"hunter2","role":"root"}`,
		"<script>alert(1)</script>.example.com",
		"193.200.64.30",
	}, "\n")+"\n")
	res, fixtureErr36288 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Mixed', 1)", srv.URL+"/mixed.txt")
	if fixtureErr36288 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr36288)
	}
	id, fixtureErr36400 := res.LastInsertId()
	if fixtureErr36400 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr36400)
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	var domains []string
	rows, fixtureErr36540 := db.Query("SELECT domain FROM blocked_domains WHERE blocklist_id = ? ORDER BY domain", id)
	if fixtureErr36540 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr36540)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		display, err := storedRuleDisplayText(d)
		if err != nil {
			t.Fatal(err)
		}
		domains = append(domains, display)
	}
	checkTestClose(t, rows)
	want := "*.adserver.example.org,193.200.64.30,ads.example.com,tracker.example.net"
	if strings.Join(domains, ",") != want {
		t.Fatalf("stored domains = %v, want %s", domains, want)
	}
}
