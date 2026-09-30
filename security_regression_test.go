package main

// Black-box regression tests for the September 2026 auth/HTTP API security
// review (design/SECURITY-REVIEW-2026-09.md, items A1–A4, A6–A12, S4). Every
// test drives the real admin mux built by startAdminServer over a loopback
// socket, so middleware ordering, routing, and auth wrappers are all in the
// path exactly as in production.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"

	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const secTestAdminPassword = "correct horse battery staple"

type secEnv struct {
	base     string
	roToken  string
	admToken string
	cookie   string
}

// secStartAdminServer runs the production admin server on a free loopback
// port and returns once /health answers. Stopping waits for the server
// goroutine to return instead of sleeping.
func secStartAdminServer(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	checkTestClose(t, l)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := startAdminServer(ctx, addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(base + "/health")
		if err == nil {
			checkTestClose(t, resp.Body)
			return base
		}
		if time.Now().After(deadline) {
			t.Fatalf("admin server never became ready: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func secSetup(t *testing.T) *secEnv {
	t.Helper()
	cleanup := setupFullTestEnv(t)
	t.Cleanup(cleanup)
	if _, err := createAdminUser("admin", secTestAdminPassword, RoleAdmin); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if err := bootstrapAuth(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	ro, _, err := createAPIToken("homepage-widget", RoleReadonly)
	if err != nil {
		t.Fatalf("create readonly token: %v", err)
	}
	adm, _, err := createAPIToken("automation", RoleAdmin)
	if err != nil {
		t.Fatalf("create admin token: %v", err)
	}
	e := &secEnv{base: secStartAdminServer(t), roToken: ro, admToken: adm}
	e.cookie = secLogin(t, e.base, "admin", secTestAdminPassword)
	return e
}

func secLogin(t *testing.T, base, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	status, hdr, respBody := secDo(t, http.MethodPost, base+"/api/auth/login", secBrowserHeaders(base, ""), body)
	if status != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", username, status, respBody)
	}
	resp := http.Response{Header: hdr}
	for _, c := range resp.Cookies() {
		if c.Name == "svart_session" && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("login %s returned no svart_session cookie", username)
	return ""
}

// secBrowserHeaders is what the SPA's fetch() sends for a same-origin JSON request.
func secBrowserHeaders(base, cookie string) map[string]string {
	h := map[string]string{
		"Content-Type":   "application/json",
		"Origin":         base,
		"Sec-Fetch-Site": "same-origin",
		"Sec-Fetch-Mode": "cors",
	}
	if cookie != "" {
		h["Cookie"] = "svart_session=" + cookie
	}
	return h
}

func secDo(t *testing.T, method, url string, hdr map[string]string, body string) (int, http.Header, string) {
	t.Helper()
	var rb io.Reader
	if body != "" {
		rb = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rb)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range hdr {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { checkTestClose(t, resp.Body) }()
	b, fixtureErr3987 := io.ReadAll(resp.Body)
	if fixtureErr3987 != nil {
		t.Fatalf("fixture operation failed: %v",

			fixtureErr3987)
	}
	return resp.StatusCode, resp.Header, string(b)
}

// secErrorOf returns the envelope's error message, or a size note for a
// success body, so failure output stays readable for multi-megabyte responses.
func secErrorOf(body string) string {
	var env struct {
		Error *string `json:"error"`
	}
	if json.Unmarshal([]byte(body), &env) == nil && env.Error != nil {
		return "error=" + *env.Error
	}
	return fmt.Sprintf("<non-error body, %d bytes>", len(body))
}

func secUserCount(t *testing.T, username string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_users WHERE username = ?", username).Scan(&n); err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

func secUserID(t *testing.T, username string) int {
	t.Helper()
	var id int
	if err := db.QueryRow("SELECT id FROM admin_users WHERE username = ?", username).Scan(&id); err != nil {
		t.Fatalf("lookup user %s: %v", username, err)
	}
	return id
}

// ---- A2: CSRF ----

func TestSecurityCSRFTextPlainFormCannotCreateAdmin(t *testing.T) {
	e := secSetup(t)
	// Exactly what <form enctype="text/plain" method=POST> on another LAN
	// origin produces; the browser attaches the SameSite=Lax cookie for a
	// same-site top-level POST navigation.
	body := `{"username":"csrf","password":"pwned-password","role":"admin","x":"="}` + "\r\n"
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", map[string]string{
		"Cookie":         "svart_session=" + e.cookie,
		"Content-Type":   "text/plain",
		"Origin":         "http://other-app.lan:8080",
		"Sec-Fetch-Site": "same-site",
		"Sec-Fetch-Mode": "navigate",
	}, body)
	if status != http.StatusForbidden {
		t.Fatalf("cross-origin text/plain POST: status %d, want 403; body %s", status, resp)
	}
	if !strings.Contains(resp, "cross-origin") {
		t.Fatalf("error should explain the cross-origin rejection, got %s", resp)
	}
	if n := secUserCount(t, "csrf"); n != 0 {
		t.Fatalf("CSRF created %d admin users", n)
	}
}

func TestSecurityCSRFCrossSiteJSONWithCookieRejected(t *testing.T) {
	e := secSetup(t)
	hdr := secBrowserHeaders(e.base, e.cookie)
	hdr["Origin"] = "https://evil.example"
	hdr["Sec-Fetch-Site"] = "cross-site"
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", hdr,
		`{"username":"csrf2","password":"pwned-password","role":"admin"}`)
	if status != http.StatusForbidden {
		t.Fatalf("cross-site POST: status %d, want 403; body %s", status, resp)
	}
	if n := secUserCount(t, "csrf2"); n != 0 {
		t.Fatalf("CSRF created %d admin users", n)
	}
}

func TestSecurityCSRFForeignOriginWithoutFetchMetadataRejected(t *testing.T) {
	e := secSetup(t)
	// Older browsers send Origin but not Sec-Fetch-Site.
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", map[string]string{
		"Cookie":       "svart_session=" + e.cookie,
		"Content-Type": "application/json",
		"Origin":       "http://printer.lan",
	}, `{"username":"csrf3","password":"pwned-password","role":"admin"}`)
	if status != http.StatusForbidden {
		t.Fatalf("foreign Origin POST: status %d, want 403; body %s", status, resp)
	}
	if n := secUserCount(t, "csrf3"); n != 0 {
		t.Fatalf("CSRF created %d admin users", n)
	}
}

func TestSecurityCookieMutationRequiresJSONContentType(t *testing.T) {
	e := secSetup(t)
	// No Origin/Sec-Fetch headers at all, but a cookie and a non-JSON type:
	// a JSON API must refuse to interpret it.
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", map[string]string{
		"Cookie":       "svart_session=" + e.cookie,
		"Content-Type": "text/plain",
	}, `{"username":"plain","password":"pwned-password","role":"admin"}`)
	if status != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain cookie POST: status %d, want 415; body %s", status, resp)
	}
	if !strings.Contains(resp, "application/json") {
		t.Fatalf("error should name the required content type, got %s", resp)
	}
	if n := secUserCount(t, "plain"); n != 0 {
		t.Fatalf("non-JSON request created %d users", n)
	}
}

func TestSecuritySameOriginSPAMutationStillWorks(t *testing.T) {
	e := secSetup(t)
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", secBrowserHeaders(e.base, e.cookie),
		`{"username":"sam","password":"a long passphrase here","role":"readonly"}`)
	if status != http.StatusCreated {
		t.Fatalf("same-origin SPA POST: status %d, want 201; body %s", status, resp)
	}
	if n := secUserCount(t, "sam"); n != 1 {
		t.Fatalf("expected user sam to exist, count %d", n)
	}
}

func TestSecurityAPIKeyScriptMutationStillWorks(t *testing.T) {
	e := secSetup(t)
	// curl -X POST -H 'X-Api-Key: …' -d '{…}' sends form content type and no
	// browser metadata; an API key cannot be attached by a cross-origin page.
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/users", map[string]string{
		"X-Api-Key":    e.admToken,
		"Content-Type": "application/x-www-form-urlencoded",
	}, `{"username":"ci-bot","password":"a long passphrase here","role":"readonly"}`)
	if status != http.StatusCreated {
		t.Fatalf("API-key POST: status %d, want 201; body %s", status, resp)
	}
}

// ---- A4: sessions ----

func TestSecuritySessionCookieIsRandomPerLogin(t *testing.T) {
	e := secSetup(t)
	second := secLogin(t, e.base, "admin", secTestAdminPassword)
	if second == e.cookie {
		t.Fatalf("two logins produced the same session cookie %q", second)
	}
	if strings.Contains(second, "admin") {
		t.Fatalf("session cookie embeds the username: %q", second)
	}
}

func TestSecuritySessionInvalidAfterLogout(t *testing.T) {
	e := secSetup(t)
	status, _, _ := secDo(t, http.MethodPost, e.base+"/api/auth/logout", secBrowserHeaders(e.base, e.cookie), "")
	if status != http.StatusOK {
		t.Fatalf("logout status %d", status)
	}
	status, _, resp := secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + e.cookie}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("replayed cookie after logout: status %d, want 401; body %s", status, resp)
	}
}

func TestSecuritySessionRevokedOnRoleChange(t *testing.T) {
	e := secSetup(t)
	if _, err := createAdminUser("sam", "a long passphrase here", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	dariaCookie := secLogin(t, e.base, "sam", "a long passphrase here")
	status, _, resp := secDo(t, http.MethodPut, fmt.Sprintf("%s/api/users/%d", e.base, secUserID(t, "sam")),
		map[string]string{"X-Api-Key": e.admToken, "Content-Type": "application/json"}, `{"role":"readonly"}`)
	if status != http.StatusOK {
		t.Fatalf("demote sam: status %d body %s", status, resp)
	}
	status, _, resp = secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + dariaCookie}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("session after role change: status %d, want 401; body %s", status, resp)
	}
}

func TestSecuritySessionRevokedOnPasswordChangeAndDelete(t *testing.T) {
	e := secSetup(t)
	for _, name := range []string{"sam", "erik"} {
		if _, err := createAdminUser(name, "a long passphrase here", RoleAdmin); err != nil {
			t.Fatal(err)
		}
	}
	dariaCookie := secLogin(t, e.base, "sam", "a long passphrase here")
	erikCookie := secLogin(t, e.base, "erik", "a long passphrase here")

	status, _, resp := secDo(t, http.MethodPut, fmt.Sprintf("%s/api/users/%d", e.base, secUserID(t, "sam")),
		map[string]string{"X-Api-Key": e.admToken, "Content-Type": "application/json"}, `{"password":"a brand new passphrase"}`)
	if status != http.StatusOK {
		t.Fatalf("change sam password: status %d body %s", status, resp)
	}
	status, _, resp = secDo(t, http.MethodDelete, fmt.Sprintf("%s/api/users/%d", e.base, secUserID(t, "erik")),
		map[string]string{"X-Api-Key": e.admToken}, "")
	if status != http.StatusOK {
		t.Fatalf("delete erik: status %d body %s", status, resp)
	}
	for name, cookie := range map[string]string{"sam": dariaCookie, "erik": erikCookie} {
		status, _, resp := secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + cookie}, "")
		if status != http.StatusUnauthorized {
			t.Fatalf("%s session after credential change: status %d, want 401; body %s", name, status, resp)
		}
	}
	// A user re-created with the same name must not inherit old sessions.
	if _, err := createAdminUser("erik", "a long passphrase here", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	status, _, _ = secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + erikCookie}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("old erik session after re-create: status %d, want 401", status)
	}
}

func TestSecuritySessionRevokeAllEndpoint(t *testing.T) {
	e := secSetup(t)
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/auth/sessions/revoke", map[string]string{"X-Api-Key": e.admToken}, "")
	if status != http.StatusOK {
		t.Fatalf("revoke all: status %d body %s", status, resp)
	}
	status, _, _ = secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + e.cookie}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("session after revoke-all: status %d, want 401", status)
	}
	// Fresh logins keep working after a revoke.
	fresh := secLogin(t, e.base, "admin", secTestAdminPassword)
	status, _, _ = secDo(t, http.MethodGet, e.base+"/api/users", map[string]string{"Cookie": "svart_session=" + fresh}, "")
	if status != http.StatusOK {
		t.Fatalf("fresh session after revoke-all: status %d, want 200", status)
	}
}

func TestSecuritySessionCookieAttributes(t *testing.T) {
	e := secSetup(t)
	_, hdr, _ := secDo(t, http.MethodPost, e.base+"/api/auth/login", secBrowserHeaders(e.base, ""),
		fmt.Sprintf(`{"username":"admin","password":%q}`, secTestAdminPassword))
	resp := http.Response{Header: hdr}
	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "svart_session" {
			got = c
		}
	}
	if got == nil {
		t.Fatal("no session cookie")
	}
	if !got.HttpOnly || got.SameSite != http.SameSiteStrictMode || got.Path != "/" || got.Secure {
		t.Fatalf("cookie attributes = HttpOnly:%v SameSite:%v Path:%q Secure:%v; want HttpOnly, Strict, /, not Secure over plain HTTP",
			got.HttpOnly, got.SameSite, got.Path, got.Secure)
	}
	if got.MaxAge <= 0 || got.MaxAge > 30*24*60*60 {
		t.Fatalf("cookie MaxAge = %d, want (0, 30d]", got.MaxAge)
	}
}

func TestSecuritySessionsAreNotReplicated(t *testing.T) {
	e := secSetup(t)
	resp, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte(e.cookie)) {
		t.Fatal("sync payload contains a live session cookie")
	}
	if bytes.Contains(bytes.ToLower(payload), []byte("session")) {
		t.Fatalf("sync payload mentions sessions: %s", payload)
	}
}

// ---- A3: login throttling ----

// secCaptureAuthLogs swaps the auth component logger for one that records
// every record, restoring it at test end.
func secCaptureAuthLogs(t *testing.T) *secLogSink {
	t.Helper()
	sink := &secLogSink{}
	prev := logAuth
	logAuth = slog.New(sink).With("component", "auth")
	t.Cleanup(func() { logAuth = prev })
	return sink
}

type secLogSink struct {
	mu      sync.Mutex
	records []slog.Record
}

func (s *secLogSink) Enabled(context.Context, slog.Level) bool { return true }
func (s *secLogSink) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r.Clone())
	return nil
}
func (s *secLogSink) WithAttrs(_ []slog.Attr) slog.Handler { return s }
func (s *secLogSink) WithGroup(string) slog.Handler        { return s }

func (s *secLogSink) failedLogins() []map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []map[string]string
	for _, r := range s.records {
		if r.Level != slog.LevelWarn || !strings.Contains(r.Message, "login failed") {
			continue
		}
		m := map[string]string{"msg": r.Message}
		r.Attrs(func(a slog.Attr) bool {
			m[a.Key] = a.Value.String()
			return true
		})
		out = append(out, m)
	}
	return out
}

func secLoginAttempt(t *testing.T, base, username, password string) (int, http.Header, string) {
	t.Helper()
	return secDo(t, http.MethodPost, base+"/api/auth/login", secBrowserHeaders(base, ""),
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, password))
}

func TestSecurityLoginPerUsernameLockout(t *testing.T) {
	e := secSetup(t)
	var statuses []int
	for i := 0; i < 5; i++ {
		st, _, _ := secLoginAttempt(t, e.base, "admin", fmt.Sprintf("guess-%d", i))
		statuses = append(statuses, st)
	}
	for i, st := range statuses {
		if st != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d, want 401 (all: %v)", i+1, st, statuses)
		}
	}
	// Once locked, even the correct password is refused until the backoff expires.
	st, hdr, body := secLoginAttempt(t, e.base, "admin", secTestAdminPassword)
	if st != http.StatusTooManyRequests {
		t.Fatalf("attempt after 5 failures: status %d, want 429; body %s", st, body)
	}
	retry, err := strconv.Atoi(hdr.Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", hdr.Get("Retry-After"))
	}
}

func TestSecurityLoginPerClientIPLockout(t *testing.T) {
	e := secSetup(t)
	var last int
	var lastBody string
	for i := 0; i < 21; i++ {
		last, _, lastBody = secLoginAttempt(t, e.base, fmt.Sprintf("sprayed-user-%d", i), "Winter2026!")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("21st failed login from one IP across distinct usernames: status %d, want 429; body %s", last, lastBody)
	}
}

func TestSecurityFailedLoginsAreLoggedWithoutPassword(t *testing.T) {
	e := secSetup(t)
	sink := secCaptureAuthLogs(t)
	secLoginAttempt(t, e.base, "admin", "hunter2-wrong-password")
	secLoginAttempt(t, e.base, "ghost", "hunter2-wrong-password")
	got := sink.failedLogins()
	if len(got) != 2 {
		t.Fatalf("failed-login warn records = %d, want 2: %v", len(got), got)
	}
	for _, rec := range got {
		if rec["client_ip"] != "127.0.0.1" {
			t.Fatalf("failed-login log lacks client_ip: %v", rec)
		}
		if rec["username"] != "admin" && rec["username"] != "ghost" {
			t.Fatalf("failed-login log lacks username: %v", rec)
		}
		for k, v := range rec {
			if strings.Contains(v, "hunter2") {
				t.Fatalf("failed-login log leaks the password in %q: %v", k, rec)
			}
		}
	}
}

func TestSecurityLoginUnknownUserCostsABcryptCompare(t *testing.T) {
	e := secSetup(t)
	measure := func(username string) time.Duration {
		// Distinct usernames per attempt stay under the per-username lockout.
		start := time.Now()
		for i := 0; i < 3; i++ {
			secLoginAttempt(t, e.base, fmt.Sprintf("%s%d", username, i), "wrong-password")
		}
		return time.Since(start) / 3
	}
	if _, err := createAdminUser("known0", "a long passphrase here", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := createAdminUser("known1", "a long passphrase here", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := createAdminUser("known2", "a long passphrase here", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	known := measure("known")
	unknown := measure("nosuchuser")
	// bcrypt at DefaultCost is tens of milliseconds; skipping it answers in
	// well under a millisecond. Require the unknown-user path to cost at least
	// half of the known-user path so the difference is not a username oracle.
	if unknown*2 < known {
		t.Fatalf("unknown-user login took %v vs known-user %v: username enumeration by timing", unknown, known)
	}
}

// ---- A1: request bodies and analysis budgets ----

func TestSecurityJSONBodiesAreCapped(t *testing.T) {
	e := secSetup(t)
	huge := strings.Repeat("a", 5<<20)
	cases := []struct{ path, body string }{
		{"/api/policies", `{"name":"` + huge + `"}`},
		{"/api/blocklists", `{"url":"https://example.com/` + huge + `","alias":"x"}`},
		{"/api/analysis/matrix", `{"domains":["` + huge + `"]}`},
	}
	for _, c := range cases {
		status, _, resp := secDo(t, http.MethodPost, e.base+c.path,
			map[string]string{"X-Api-Key": e.admToken, "Content-Type": "application/json"}, c.body)
		if status != http.StatusRequestEntityTooLarge {
			t.Fatalf("POST %s with a 5 MiB body: status %d, want 413; %s", c.path, status, secErrorOf(resp))
		}
	}
}

func secInsertBlocklist(t *testing.T, alias string, domains ...string) int {
	t.Helper()
	res, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, 1)", "https://lists.example.com/"+alias+".txt", alias)
	if err != nil {
		t.Fatal(err)
	}
	id, fixtureErr20416 := res.LastInsertId()
	if fixtureErr20416 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr20416)
	}
	for _, d := range domains {
		if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, d); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if _, err := db.Exec("UPDATE blocklists SET domain_count = ? WHERE id = ?", len(domains), id); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	return int(id)
}

func TestSecurityAnalysisCompareDedupesRepeatedIDs(t *testing.T) {
	e := secSetup(t)
	a := secInsertBlocklist(t, "Dedupe A", "ads.example.com", "track.example.com")
	b := secInsertBlocklist(t, "Dedupe B", "track.example.com", "pixel.example.net")
	mustReloadPolicy(t)
	ids := make([]int, 0, 4000)
	for i := 0; i < 2000; i++ {
		ids = append(ids, a, b)
	}
	body, fixtureErr21176 := json.Marshal(map[string]interface{}{"blocklist_ids": ids})
	if fixtureErr21176 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr21176)
	}
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/analysis/compare",
		map[string]string{"X-Api-Key": e.roToken, "Content-Type": "application/json"}, string(body))
	if status != http.StatusOK {
		t.Fatalf("compare repeated ids: status %d body %s", status, resp)
	}
	var env struct {
		Data compareResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(resp), &env); err != nil {
		t.Fatal(err)
	}
	want := compareResponse{
		Lists: []compareListInfo{
			{ID: a, Alias: "Dedupe A", DomainCount: 2},
			{ID: b, Alias: "Dedupe B", DomainCount: 2},
		},
		Overlap:     [][]int{{2, 1}, {1, 2}},
		UniqueCount: []int{1, 1},
		UnionSize:   3,
	}
	gotJSON, fixtureErr21899 := json.Marshal(env.Data)
	if fixtureErr21899 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr21899)
	}
	wantJSON, fixtureErr21937 := json.Marshal(want)
	if fixtureErr21937 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr21937)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		if len(env.Data.Lists) != 2 {
			t.Fatalf("compare returned %d lists for 2 distinct ids, want %s", len(env.Data.Lists), wantJSON)
		}
		t.Fatalf("compare result = %s, want %s", gotJSON, wantJSON)
	}
}

func TestSecurityAnalysisRejectsUnknownAndTooManyLists(t *testing.T) {
	e := secSetup(t)
	hdr := map[string]string{"X-Api-Key": e.roToken, "Content-Type": "application/json"}

	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/analysis/matrix", hdr,
		`{"domains":["ads.google.com"],"blocklist_ids":[900001]}`)
	if status != http.StatusBadRequest || !strings.Contains(resp, "900001") {
		t.Fatalf("matrix unknown id: status %d body %s; want 400 naming the id", status, resp)
	}

	var ids []int
	for i := 0; i < 65; i++ {
		ids = append(ids, secInsertBlocklist(t, fmt.Sprintf("Bulk %02d", i), fmt.Sprintf("ads%d.example.com", i)))
	}
	mustReloadPolicy(t)
	for _, path := range []string{"/api/analysis/matrix", "/api/analysis/compare"} {
		body, fixtureErr22959 := json.Marshal(map[string]interface{}{"domains": []string{"ads.google.com"}, "blocklist_ids": ids})
		if fixtureErr22959 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr22959)
		}
		status, _, resp := secDo(t, http.MethodPost, e.base+path, hdr, string(body))
		if status != http.StatusBadRequest || !strings.Contains(resp, "64") {
			t.Fatalf("%s with 65 lists: status %d body %s; want 400 naming the 64-list cap", path, status, resp)
		}
	}
}

func TestSecurityAnalysisMatrixRejectsOverBudgetWork(t *testing.T) {
	e := secSetup(t)
	var ids []int
	for i := 0; i < 40; i++ {
		ids = append(ids, secInsertBlocklist(t, fmt.Sprintf("Budget %02d", i), fmt.Sprintf("ads%d.example.com", i)))
	}
	mustReloadPolicy(t)
	domains := make([]string, 10000)
	for i := range domains {
		domains[i] = fmt.Sprintf("host%d.tracker.example.com", i)
	}
	body, fixtureErr23721 := json.Marshal(map[string]interface{}{"domains": domains, "blocklist_ids": ids})
	if fixtureErr23721 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr23721)
	}
	status, _, resp := secDo(t, http.MethodPost, e.base+"/api/analysis/matrix",
		map[string]string{"X-Api-Key": e.roToken, "Content-Type": "application/json"}, string(body))
	if status != http.StatusBadRequest || !strings.Contains(resp, "budget") {
		t.Fatalf("10000 domains x 40 lists: status %d %s; want 400 explaining the work budget", status, secErrorOf(resp))
	}
}

func TestSecurityUniqueDomainsRejectsBadIDs(t *testing.T) {
	e := secSetup(t)
	status, _, resp := secDo(t, http.MethodGet, e.base+"/api/blocklists/unique-domains?ids=1,abc,900001",
		map[string]string{"X-Api-Key": e.roToken}, "")
	if status != http.StatusBadRequest {
		t.Fatalf("unique-domains with junk ids: status %d body %s, want 400", status, resp)
	}
}

// ---- A9: pagination ----

func TestSecurityNegativePaginationRejected(t *testing.T) {
	e := secSetup(t)
	if _, err := db.Exec("INSERT INTO blocklist_history (blocklist_id, previous_count, new_count, added_count, removed_count) VALUES (1, 0, 10, 10, 0)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	var historyID int
	if err := db.QueryRow("SELECT MAX(id) FROM blocklist_history").Scan(&historyID); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		fmt.Sprintf("/api/blocklists/history/%d/domains?offset=-5", historyID),
		fmt.Sprintf("/api/blocklists/history/%d/domains?limit=-1", historyID),
		"/api/query-logs?offset=-1",
		"/api/query-logs?limit=-10",
		"/api/blocklists/1/domains?offset=-1",
		"/api/blocklists/1/domains?limit=-1",
		"/api/allowlists/1/domains?offset=-3",
		"/api/analysis/domains?source=top-sites&limit=-1",
		"/api/stats/top-clients?limit=-1",
		"/api/stats/top-domains?limit=-1",
		"/api/stats/timeseries?buckets=-1",
		"/api/stats/dashboard?client_limit=-1",
	}
	for _, p := range paths {
		status, _, resp := secDo(t, http.MethodGet, e.base+p, map[string]string{"X-Api-Key": e.roToken}, "")
		if status != http.StatusBadRequest {
			t.Errorf("GET %s: status %d, want 400; body %s", p, status, resp)
		}
	}
}

// ---- A10: last admin ----

func TestSecurityLastAdminCannotBeDemoted(t *testing.T) {
	e := secSetup(t)
	status, _, resp := secDo(t, http.MethodPut, fmt.Sprintf("%s/api/users/%d", e.base, secUserID(t, "admin")),
		map[string]string{"X-Api-Key": e.admToken, "Content-Type": "application/json"}, `{"role":"readonly"}`)
	if status != http.StatusConflict {
		t.Fatalf("demote last admin: status %d, want 409; body %s", status, resp)
	}
	var role string
	if err := db.QueryRow("SELECT role FROM admin_users WHERE username = 'admin'").Scan(&role); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if role != RoleAdmin {
		t.Fatalf("last admin role = %q after rejected demotion", role)
	}
}

// ---- A11: URL redaction ----

func TestSecurityListAndUpstreamURLsRedactedForReadonly(t *testing.T) {
	e := secSetup(t)
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	const listURL = "https://svc:s3cr3t@lists.example.com/pro.txt?token=abc123"
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	const upstreamURL = "https://acct:pw@dns.example.net/dns-query?key=k3y"
	if _, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, 'Tokenized', 1)", listURL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, 'Tokenized Allow', 1)", listURL); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, 1)", upstreamURL); err != nil {
		t.Fatal(err)
	}
	if err := loadUpstreamsFromDB(); err != nil {
		t.Fatal(err)
	}
	res, fixtureErr27253 := db.Exec("INSERT INTO policies (name) VALUES ('Kids')")
	if fixtureErr27253 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr27253)
	}
	policyID, fixtureErr27319 := res.LastInsertId()
	if fixtureErr27319 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr27319)
	}

	paths := []string{"/api/blocklists", "/api/allowlists", "/api/upstreams", fmt.Sprintf("/api/policies/%d", policyID)}
	for _, p := range paths {
		_, _, roBody := secDo(t, http.MethodGet, e.base+p, map[string]string{"X-Api-Key": e.roToken}, "")
		for _, secret := range []string{"s3cr3t", "abc123", "pw@", "k3y", "svc:", "acct:"} {
			if strings.Contains(roBody, secret) {
				t.Errorf("readonly GET %s leaks %q", p, secret)
			}
		}
		if !strings.Contains(roBody, "lists.example.com") && !strings.Contains(roBody, "dns.example.net") {
			t.Errorf("readonly GET %s lost the URL host entirely: %s", p, roBody)
		}
		_, _, admBody := secDo(t, http.MethodGet, e.base+p, map[string]string{"X-Api-Key": e.admToken}, "")
		if !strings.Contains(admBody, "abc123") && !strings.Contains(admBody, "k3y") {
			t.Errorf("admin GET %s should see the full URL: %s", p, admBody)
		}
	}
}

// ---- A7: security headers ----

func TestSecurityHeadersOnAdminResponses(t *testing.T) {
	e := secSetup(t)
	for _, p := range []string{"/", "/login", "/api/stats", "/docs", "/health"} {
		_, h, _ := secDo(t, http.MethodGet, e.base+p, map[string]string{"X-Api-Key": e.roToken}, "")
		csp := h.Get("Content-Security-Policy")
		for _, directive := range []string{"frame-ancestors 'none'", "form-action 'self'", "object-src 'none'", "base-uri 'none'"} {
			if !strings.Contains(csp, directive) {
				t.Errorf("%s CSP %q lacks %q", p, csp, directive)
			}
		}
		if got := h.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("%s X-Frame-Options = %q, want DENY", p, got)
		}
		if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q, want nosniff", p, got)
		}
		if got := h.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s Referrer-Policy = %q, want no-referrer", p, got)
		}
		if got := h.Get("Strict-Transport-Security"); got != "" {
			t.Errorf("%s sent HSTS %q over plain HTTP", p, got)
		}
	}
}

// ---- A12: Host allowlist ----

func TestSecurityAdminAllowedHosts(t *testing.T) {
	t.Setenv("ADMIN_ALLOWED_HOSTS", "svart.home.arpa,10.42.1.6,127.0.0.1")
	e := secSetup(t)
	status, _, resp := secDo(t, http.MethodGet, e.base+"/api/stats",
		map[string]string{"X-Api-Key": e.roToken, "Host": "attacker-rebind.example"}, "")
	if status != http.StatusMisdirectedRequest {
		t.Fatalf("rebinding Host: status %d, want 421; %s", status, secErrorOf(resp))
	}
	status, _, resp = secDo(t, http.MethodGet, e.base+"/api/stats",
		map[string]string{"X-Api-Key": e.roToken, "Host": "svart.home.arpa:3000"}, "")
	if status != http.StatusOK {
		t.Fatalf("allowed Host: status %d, want 200; body %s", status, resp)
	}
	// Liveness probes address the node by IP/localhost and must keep working.
	status, _, _ = secDo(t, http.MethodGet, e.base+"/health", map[string]string{"Host": "localhost:3000"}, "")
	if status != http.StatusOK {
		t.Fatalf("/health with unlisted Host: status %d, want 200", status)
	}
}

// ---- A8 / S4: list fetch SSRF ----
