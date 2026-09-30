package svart

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// setupTestServer is a fresh install: no administrator, no ADMIN_PASSWORD,
// first-run setup armed with a token the test knows.
func setupTestServer(t *testing.T) (base, token string) {
	t.Helper()
	t.Setenv("ADMIN_PASSWORD", "")
	cleanup := setupFullTestEnv(t)
	t.Cleanup(cleanup)
	if err := bootstrapAuth(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if !setupRequired() {
		t.Fatal("a fresh install with no ADMIN_PASSWORD should require first-run setup")
	}
	savedPort := dnsPort
	dnsPort = "53"
	t.Cleanup(func() { dnsPort = savedPort })
	tok, err := startFirstRunSetup() // re-arm with a token this test can read
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		firstRun.mu.Lock()
		firstRun.active = false
		firstRun.mu.Unlock()
	})
	return secStartAdminServer(t), tok
}

func postSetup(t *testing.T, base, token, username, password string) (int, http.Header, string) {
	t.Helper()
	// #nosec G117 -- The setup request contract requires a password field; this payload is sent only to the local test server.
	body, fixtureErr1006 := json.Marshal(setupRequest{Token: token, Username: username, Password: password})
	if fixtureErr1006 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1006)
	}
	return secDo(t, http.MethodPost, base+"/api/setup", secBrowserHeaders(base, ""), string(body))
}

func TestFirstRunSetupCreatesAdminAndSignsIn(t *testing.T) {
	base, token := setupTestServer(t)

	status, _, body := secDo(t, http.MethodGet, base+"/api/setup", nil, "")
	var st struct {
		Data setupStatus `json:"data"`
	}
	if status != http.StatusOK || json.Unmarshal([]byte(body), &st) != nil {
		t.Fatalf("GET /api/setup: %d %s", status, body)
	}
	if !st.Data.Required || st.Data.MinLength != minAdminPasswordLen || st.Data.DNSPort == "" {
		t.Fatalf("setup status = %+v, want required with password rule and DNS port", st.Data)
	}
	status, _, body = secDo(t, http.MethodGet, base+"/api/auth/check", nil, "")
	if status != http.StatusOK || !strings.Contains(body, `"setup_required":true`) {
		t.Fatalf("auth check should tell the SPA to go to setup: %d %s", status, body)
	}

	// Pasted lowercase and without dashes, the way terminals mangle it.
	loose := strings.ToLower(strings.ReplaceAll(token, "-", ""))
	status, hdr, body := postSetup(t, base, loose, "chris", "correct horse battery staple")
	if status != http.StatusCreated {
		t.Fatalf("setup: %d %s", status, body)
	}
	var cookie string
	for _, c := range (&http.Response{Header: hdr}).Cookies() {
		if c.Name == sessionCookieName {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatal("setup must sign the new administrator in")
	}
	status, _, body = secDo(t, http.MethodGet, base+"/api/users", secBrowserHeaders(base, cookie), "")
	if status != http.StatusOK || !strings.Contains(body, `"username":"chris"`) || !strings.Contains(body, `"role":"admin"`) {
		t.Fatalf("new admin session should list users: %d %s", status, body)
	}

	// Setup is over: the token no longer works and the status says nothing more.
	status, _, body = postSetup(t, base, token, "mallory", "another long password")
	if status != http.StatusConflict {
		t.Fatalf("second setup: %d %s, want 409", status, body)
	}
	status, _, body = secDo(t, http.MethodGet, base+"/api/setup", nil, "")
	if status != http.StatusOK || strings.TrimSpace(body) != `{"data":{"required":false},"error":null}` {
		t.Fatalf("status after setup leaks detail: %s", body)
	}
	if n := secUserCount(t, "mallory"); n != 0 {
		t.Fatalf("second setup created %d users", n)
	}
}

func TestFirstRunSetupRejectsBadInput(t *testing.T) {
	base, token := setupTestServer(t)
	cases := []struct {
		name, token, user, pass string
		status                  int
		errPart                 string
	}{
		{"wrong token", "AAAA-BBBB-CCCC-DDDD-EEEE-FFFF-GGGG-HHHH", "chris", "correct horse battery staple", http.StatusUnauthorized, "wrong setup token"},
		{"empty token", "", "chris", "correct horse battery staple", http.StatusUnauthorized, "wrong setup token"},
		{"short password", token, "chris", "hunter2", http.StatusBadRequest, "at least 12 characters"},
		{"bcrypt-truncated password", token, "chris", strings.Repeat("x", 73), http.StatusBadRequest, "at most 72 bytes"},
		{"blank username", token, "  ", "correct horse battery staple", http.StatusBadRequest, "username is required"},
		{"username with colon", token, "chris:admin", "correct horse battery staple", http.StatusBadRequest, "without spaces or colons"},
	}
	for _, c := range cases {
		status, _, body := postSetup(t, base, c.token, c.user, c.pass)
		if status != c.status || !strings.Contains(secErrorOf(body), c.errPart) {
			t.Errorf("%s: got %d %s, want %d containing %q", c.name, status, secErrorOf(body), c.status, c.errPart)
		}
	}
	if !setupRequired() {
		t.Fatal("rejected attempts must leave setup pending")
	}
}

func TestFirstRunSetupThrottlesTokenGuessing(t *testing.T) {
	base, _ := setupTestServer(t)
	var last int
	for i := 0; i < 30 && last != http.StatusTooManyRequests; i++ {
		last, _, _ = postSetup(t, base, "AAAA-BBBB-CCCC-DDDD-EEEE-FFFF-GGGG-HHHH", "chris", "correct horse battery staple")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("30 wrong setup tokens never throttled (last status %d)", last)
	}
}

func TestFirstRunSetupIsSingleUseUnderConcurrency(t *testing.T) {
	base, token := setupTestServer(t)
	const n = 8
	statuses := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses[i], _, _ = postSetup(t, base, token, "admin"+string("abcdefgh"[i]), "correct horse battery staple")
		}(i)
	}
	wg.Wait()
	created := 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", s)
		}
	}
	var users int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&users); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if created != 1 || users != 1 {
		t.Fatalf("%d setups succeeded and %d admins exist, want exactly 1 of each", created, users)
	}
}

func TestFirstRunSetupNotOfferedWhenAdminPasswordSet(t *testing.T) {
	t.Setenv("ADMIN_PASSWORD", "a configured admin password")
	cleanup := setupFullTestEnv(t)
	t.Cleanup(cleanup)
	firstRun.mu.Lock()
	firstRun.active = false
	firstRun.mu.Unlock()
	if err := bootstrapAuth(); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if setupRequired() {
		t.Fatal("ADMIN_PASSWORD seeds the administrator; setup must not be offered")
	}
}

func TestVirtualInterfacesAreNotSuggested(t *testing.T) {
	for name, want := range map[string]bool{
		"docker0": true, "br-228c04336ad7": true, "veth1a2b": true, "virbr0": true, "cni0": true,
		"eth0": false, "enp6s0": false, "wlan0": false, "bond0": false, "tailscale0": false,
	} {
		if got := isVirtualInterface(name); got != want {
			t.Errorf("isVirtualInterface(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestTemporaryIPv6AddressesAreDetected(t *testing.T) {
	proc := strings.Join([]string{
		"20010db8000000010000000000000011 02 80 00 80 enp6s0", // DHCPv6, permanent
		"20010db800000001d4f3d1efeb3891e8 02 40 00 01 enp6s0", // RFC 4941 temporary
		"fe80000000000000f8289ffffe44b6b8 c7b 40 20 80 veth81b754e",
		"garbage line",
	}, "\n")
	got := parseTemporaryIPv6(proc)
	if len(got) != 1 || !got["2001:db8:0:1:d4f3:d1ef:eb38:91e8"] {
		t.Fatalf("temporary addresses = %v, want only the RFC 4941 one", got)
	}
}
