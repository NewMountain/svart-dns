package svart

import (
	"crypto/tls"

	"net/http"
	"net/http/httptest"

	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSessionLifecycle(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	clock := withSessionClock(t, time.Date(2026, 9, 25, 18, 30, 0, 0, time.UTC))

	user, err := createAdminUser("chris", "securepw-long-enough", RoleAdmin)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	_, hash, _, createdAt, _, fixtureErr530 := getAdminUserByUsername("chris")
	if fixtureErr530 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr530)
	}

	token, expires, err := createSession("chris", hash)
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	if len(token) != 43 || strings.Contains(token, "chris") {
		t.Fatalf("token %q: want 43-char base64url of 32 random bytes, not derived from the username", token)
	}
	if want := clock.Add(30 * 24 * time.Hour); !expires.Equal(want) {
		t.Fatalf("expires = %v, want %v", expires, want)
	}

	got := lookupSession(token)
	want := &AdminUser{ID: user.ID, Username: "chris", Role: RoleAdmin, CreatedAt: createdAt}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lookupSession = %+v, want %+v", got, want)
	}

	var stored struct{ idHash, username, created, lastSeen, expires string }
	if err := db.QueryRow("SELECT id_hash, username, created_at, last_seen_at, expires_at FROM sessions").
		Scan(&stored.idHash, &stored.username, &stored.created, &stored.lastSeen, &stored.expires); err != nil {
		t.Fatal(err)
	}
	wantRow := struct{ idHash, username, created, lastSeen, expires string }{
		idHash: hashSessionToken(token), username: "chris",
		created: "2026-09-25T18:30:00.000000Z", lastSeen: "2026-09-25T18:30:00.000000Z", expires: "2026-10-25T18:30:00.000000Z",
	}
	if stored != wantRow {
		t.Fatalf("stored row = %+v, want %+v (token itself must never be stored)", stored, wantRow)
	}

	for _, bogus := range []string{"", "bogus", token + "x", "chris:deadbeef", hashSessionToken(token)} {
		if lookupSession(bogus) != nil {
			t.Errorf("lookupSession(%q) accepted a token that was never issued", bogus)
		}
	}

	second, _, err := createSession("chris", hash)
	if err != nil || second == token {
		t.Fatalf("second session = %q, %v; want a distinct token", second, err)
	}

	if err := deleteSession(token); err != nil {
		t.Fatal(err)
	}
	if lookupSession(token) != nil {
		t.Fatal("session still valid after deleteSession (logout)")
	}
	if lookupSession(second) == nil {
		t.Fatal("logging out one session ended another")
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	start := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	clock := withSessionClock(t, start)
	if _, err := createAdminUser("sam", "a long passphrase here", RoleReadonly); err != nil {
		t.Fatal(err)
	}
	_, hash, _, _, _, fixtureErr592 := getAdminUserByUsername("sam")
	if fixtureErr592 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr592)
	}

	idle, _, fixtureErr594 := createSession("sam", hash)
	if fixtureErr594 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr594)
	}
	*clock = start.Add(7*24*time.Hour - time.Second)
	if lookupSession(idle) == nil {
		t.Fatal("session expired before the 7-day idle timeout")
	}
	*clock = clock.Add(7*24*time.Hour - time.Second) // just under a week after the last use
	if lookupSession(idle) == nil {
		t.Fatal("use did not extend the idle window")
	}
	*clock = clock.Add(7 * 24 * time.Hour)
	if lookupSession(idle) != nil {
		t.Fatal("session survived a full week idle")
	}
	var n int
	if fixtureErr608 := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id_hash = ?", hashSessionToken(idle)).Scan(&n); fixtureErr608 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr608)
	}
	if n != 0 {
		t.Fatal("expired session row was not deleted")
	}

	*clock = start
	daily, _, fixtureErr614 := createSession("sam", hash)
	if fixtureErr614 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr614)
	}
	for day := 1; day < 30; day++ {
		*clock = start.Add(time.Duration(day) * 24 * time.Hour)
		if lookupSession(daily) == nil {
			t.Fatalf("daily-used session ended on day %d, before the 30-day lifetime", day)
		}
	}
	*clock = start.Add(30 * 24 * time.Hour)
	if lookupSession(daily) != nil {
		t.Fatal("session outlived the 30-day absolute lifetime despite daily use")
	}
}

func TestConcurrentDemotionsKeepOneAdmin(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	a, fixtureErr630 := createAdminUser("admin", "a long passphrase here", RoleAdmin)
	if fixtureErr630 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr630)
	}
	b, fixtureErr631 := createAdminUser("sam", "a long passphrase here", RoleAdmin)
	if fixtureErr631 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr631)
	}

	errs := make(chan error, 2)
	for _, id := range []int{a.ID, b.ID} {
		go func(id int) { errs <- updateAdminUser(id, RoleReadonly, "") }(id)
	}
	var ok, refused int
	for i := 0; i < 2; i++ {
		switch err := <-errs; err {
		case nil:
			ok++
		case errDemoteLastAdmin:
			refused++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	var admins int
	if fixtureErr649 := db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE role = 'admin'").Scan(&admins); fixtureErr649 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr649)
	}
	if ok != 1 || refused != 1 || admins != 1 {
		t.Fatalf("ok=%d refused=%d admins=%d; want exactly one demotion and one remaining admin", ok, refused, admins)
	}
}

func TestSessionCookieAuth(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := createAdminUser("admin", "testpass-long-enough", RoleAdmin); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	_, hash, _, _, _, fixtureErr662 := getAdminUserByUsername("admin")
	if fixtureErr662 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr662)
	}
	token, _, err := createSession("admin", hash)
	if err != nil {
		t.Fatal(err)
	}

	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Name: "svart_session", Value: token})
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid session cookie, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("POST", "/api/test", nil)
	req.AddCookie(&http.Cookie{Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Name: "svart_session", Value: "admin:" + strings.Repeat("0", 64)})
	w = httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("forged old-format cookie: got %d, want 401", w.Code)
	}
}

func TestHandleAPIRevokeSessionsEndsEverySession(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	var tokens []string
	for _, name := range []string{"admin", "sam"} {
		if _, err := createAdminUser(name, "a long passphrase here", RoleAdmin); err != nil {
			t.Fatal(err)
		}
		_, hash, _, _, _, fixtureErr698 := getAdminUserByUsername(name)
		if fixtureErr698 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr698)
		}
		token, _, err := createSession(name, hash)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke", nil)
	w := httptest.NewRecorder()
	handleAPIRevokeSessions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	for _, token := range tokens {
		if strings.Contains(w.Body.String(), token) {
			t.Fatal("revocation response exposes a session token")
		}
		if lookupSession(token) != nil {
			t.Fatal("session remained valid after revoke-all")
		}
	}
	var n int
	if fixtureErr721 := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); fixtureErr721 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr721)
	}
	if n != 0 {
		t.Fatalf("%d session rows remain after revoke-all", n)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("revoke should clear the caller's cookie, got %+v", cookies)
	}
}

// With SYNC_REPLICATE_IDENTITY on, admin users replicate between nodes; sessions never do. A password change
// merged from a peer must still end local sessions issued under the old
// password, and a merged role change must take effect immediately.
func TestSyncMergedCredentialChangesApplyToLocalSessions(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t) // identity rows only merge when opted in (DD-029)

	for _, name := range []string{"admin", "morgan"} {
		if _, err := createAdminUser(name, "a long passphrase here", RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	_, adminHash, _, _, _, fixtureErr744 := getAdminUserByUsername("admin")
	if fixtureErr744 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr744)
	}
	_, morganHash, _, _, _, fixtureErr745 := getAdminUserByUsername("morgan")
	if fixtureErr745 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr745)
	}
	adminToken, _, fixtureErr746 := createSession("admin", adminHash)
	if fixtureErr746 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr746)
	}
	morganToken, _, fixtureErr747 := createSession("morgan", morganHash)
	if fixtureErr747 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr747)
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte("changed on the other node"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
	if err := mergeSyncResponse(&SyncResponse{Changes: SyncChanges{AdminUsers: []SyncAdminUser{
		{Username: "admin", PasswordHash: string(newHash), Role: RoleAdmin, UpdatedAt: later, NodeID: "svart-b"},
		{Username: "morgan", PasswordHash: morganHash, Role: RoleReadonly, UpdatedAt: later, NodeID: "svart-b"},
	}}}); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if lookupSession(adminToken) != nil {
		t.Fatal("session issued under the old password survived a synced password change")
	}
	if got := lookupSession(morganToken); got == nil || got.Role != RoleReadonly {
		t.Fatalf("after a synced demotion the session should carry role readonly, got %+v", got)
	}
}

func TestLoginFailsClosedWhenSessionsCannotBeStored(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass-long-enough", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE sessions"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"testpass-long-enough"}`))
	w := httptest.NewRecorder()
	handleAPIAuthLogin(w, req)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 {
		t.Fatalf("status %d cookies %v; want 503 and no cookie when the session cannot be stored", w.Code, w.Result().Cookies())
	}
}

func TestLogoutEndsSessionAndReloginReplacesCookie(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass-long-enough", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	login := func(cookie string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"testpass-long-enough"}`))
		if cookie != "" {
			req.AddCookie(&http.Cookie{Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Name: "svart_session", Value: cookie})
		}
		w := httptest.NewRecorder()
		handleAPIAuthLogin(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login status %d: %s", w.Code, w.Body.String())
		}
		return w.Result().Cookies()[0].Value
	}
	first := login("")
	second := login(first)
	if lookupSession(first) != nil {
		t.Fatal("logging in again kept the replaced session valid")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Name: "svart_session", Value: second})
	w := httptest.NewRecorder()
	handleAPIAuthLogout(w, req)
	if w.Code != http.StatusOK || lookupSession(second) != nil {
		t.Fatalf("logout status %d; session still valid: %v", w.Code, lookupSession(second) != nil)
	}
}

func TestSessionsPerUserAreCapped(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	clock := withSessionClock(t, time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC))
	if _, err := createAdminUser("admin", "testpass-long-enough", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	_, hash, _, _, _, fixtureErr825 := getAdminUserByUsername("admin")
	if fixtureErr825 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr825)
	}
	var tokens []string
	for i := 0; i < maxSessionsPerUser+5; i++ {
		*clock = clock.Add(time.Second)
		token, _, err := createSession("admin", hash)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, token)
	}
	var n int
	if fixtureErr836 := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE username = 'admin'").Scan(&n); fixtureErr836 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr836)
	}
	if n != maxSessionsPerUser {
		t.Fatalf("admin has %d sessions, want the cap %d", n, maxSessionsPerUser)
	}
	if lookupSession(tokens[0]) != nil || lookupSession(tokens[len(tokens)-1]) == nil {
		t.Fatal("the cap must revoke the oldest sessions and keep the newest")
	}
}

func TestRevokeSessionsRequiresAdminRole(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	readonlyKey, _, err := createAPIToken("Readonly", RoleReadonly)
	if err != nil {
		t.Fatalf("create readonly key: %v", err)
	}
	adminKey, _, err := createAPIToken("Admin", RoleAdmin)
	if err != nil {
		t.Fatalf("create admin key: %v", err)
	}
	handler := adminAuth(handleAPIRevokeSessions)

	readonlyReq := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke", nil)
	readonlyReq.Header.Set("X-Api-Key", readonlyKey)
	readonlyResp := httptest.NewRecorder()
	handler(readonlyResp, readonlyReq)
	if readonlyResp.Code != http.StatusForbidden {
		t.Fatalf("readonly status = %d, want 403", readonlyResp.Code)
	}

	adminReq := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke", nil)
	adminReq.Header.Set("X-Api-Key", adminKey)
	adminResp := httptest.NewRecorder()
	handler(adminResp, adminReq)
	if adminResp.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200; body = %s", adminResp.Code, adminResp.Body.String())
	}
}

func TestAuthCookiesAreSecureOnTLSRequests(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass", RoleAdmin); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{
			name:    "login",
			path:    "/api/auth/login",
			body:    `{"username":"admin","password":"testpass"}`,
			handler: handleAPIAuthLogin,
		},
		{name: "logout", path: "/api/auth/logout", handler: handleAPIAuthLogout},
		{name: "revoke", path: "/api/auth/sessions/revoke", handler: handleAPIRevokeSessions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			req.TLS = &tls.ConnectionState{}
			w := httptest.NewRecorder()
			tt.handler(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies = %d, want 1", len(cookies))
			}
			if !cookies[0].Secure {
				t.Fatal("TLS auth response set a cookie without Secure")
			}
		})
	}
}

// TestRequestIsHTTPSTrustsForwardedProtoOnlyWhenEnabled verifies that
// X-Forwarded-Proto is only trusted for the Secure cookie flag when the
// operator has explicitly enabled TRUST_PROXY_HEADERS. By default, a
// plain-HTTP request claiming X-Forwarded-Proto: https must NOT be treated
// as HTTPS — otherwise any client could spoof the header and defeat Secure.
func TestRequestIsHTTPSTrustsForwardedProtoOnlyWhenEnabled(t *testing.T) {
	origTrust := trustProxyHeaders
	defer func() { trustProxyHeaders = origTrust }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	trustProxyHeaders = false
	if requestIsHTTPS(req) {
		t.Fatal("expected X-Forwarded-Proto to be untrusted when TRUST_PROXY_HEADERS is disabled")
	}

	trustProxyHeaders = true
	if !requestIsHTTPS(req) {
		t.Fatal("expected X-Forwarded-Proto: https to be trusted when TRUST_PROXY_HEADERS is enabled")
	}

	// A plain HTTP request with no forwarded header is never HTTPS, flag or not.
	plain := httptest.NewRequest(http.MethodGet, "/", nil)
	if requestIsHTTPS(plain) {
		t.Fatal("expected plain HTTP request without X-Forwarded-Proto to not be treated as HTTPS")
	}

	// Direct TLS connections are always HTTPS regardless of the flag.
	trustProxyHeaders = false
	tlsReq := httptest.NewRequest(http.MethodGet, "/", nil)
	tlsReq.TLS = &tls.ConnectionState{}
	if !requestIsHTTPS(tlsReq) {
		t.Fatal("expected direct TLS connection to be treated as HTTPS")
	}
}

// TestAuthCookiesRespectTrustedForwardedProto verifies the end-to-end
// behavior through the real cookie-setting handlers: with
// TRUST_PROXY_HEADERS enabled, a plain-HTTP request carrying
// X-Forwarded-Proto: https still gets a Secure cookie (the scenario this
// closes: a TLS-terminating reverse proxy in front of svart-dns).
func TestAuthCookiesRespectTrustedForwardedProto(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass", RoleAdmin); err != nil {
		t.Fatalf("create admin user: %v", err)
	}

	origTrust := trustProxyHeaders
	trustProxyHeaders = true
	defer func() { trustProxyHeaders = origTrust }()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"testpass"}`))
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	handleAPIAuthLogin(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if !cookies[0].Secure {
		t.Fatal("expected Secure cookie when TRUST_PROXY_HEADERS is enabled and X-Forwarded-Proto is https")
	}
}

func TestAuthLoginRejectsOversizedRequestBody(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass", RoleAdmin); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	body := `{"username":"` + strings.Repeat("x", maxAuthRequestBodyBytes) + `","password":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	w := httptest.NewRecorder()

	handleAPIAuthLogin(w, req)

	// An oversized body trips http.MaxBytesReader and should be reported as
	// 413 Request Entity Too Large, distinct from a merely malformed body.
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", w.Code, w.Body.String())
	}
}
