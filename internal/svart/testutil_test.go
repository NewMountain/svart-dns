package svart

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// setupTestDBTB creates a temporary SQLite database with the full schema and
// realistic seed data. It swaps the global db variable and returns a cleanup
// function that restores the original. The test owns the complete temporary directory.
func setupTestDBTB(tb testing.TB) func() {
	tb.Helper()
	// The helper swaps process-global databases, including in nested peer tests.
	// Drain the previous owner before replacing its logger or writer pointer.
	closeLogWriter()

	// Suppress migration INFO spam during tests — only show errors
	tb.Setenv("LOG_LEVEL", "error")
	initLogging()

	dbPath := filepath.Join(tb.TempDir(), "svart-dns-test.db")

	oldDB := db
	oldReadDB := readDB

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			closeLogWriter()
			if readDB != nil && readDB != oldReadDB {
				if err := readDB.Close(); err != nil {
					tb.Errorf("close test read database: %v", err)
				}
			}
			readDB = oldReadDB
			if db != nil && db != oldDB {
				if err := db.Close(); err != nil {
					tb.Errorf("close test database: %v", err)
				}
			}
			db = oldDB
		})
	}
	tb.Cleanup(cleanup)

	if err := initDatabase(dbPath); err != nil {
		tb.Fatalf("failed to init test database: %v", err)
	}

	// Reset all in-memory stores to clean state
	upstreamStorage.publish(nil)
	policyState.Store(emptyPolicySnapshot())
	policyCache.Clear()
	rewritesCache.Store(&sync.Map{})
	cache.clear()

	// Reset api_tokens, admin_users, and auth state
	if _, err := db.Exec("DELETE FROM api_tokens"); err != nil {
		tb.Fatalf("reset API tokens: %v", err)
	}
	if _, err := db.Exec("DELETE FROM admin_users"); err != nil {
		tb.Fatalf("reset admin users: %v", err)
	}
	hasTokens.Store(false)
	hasUsers.Store(false)
	tokenCache = sync.Map{}
	lastUsedUpdate = sync.Map{}
	// Every HTTP test logs in from 127.0.0.1; start each with clean counters.
	logins = newLoginLimiter()
	tokenVerifications = tokenAdmission{}

	return cleanup
}

// setupTestDB is the testing.T wrapper around setupTestDBTB.
func setupTestDB(t *testing.T) func() {
	return setupTestDBTB(t)
}

// seedUpstreams inserts a realistic set of upstream DNS servers.
func seedUpstreams(t *testing.T) {
	t.Helper()

	upstreams := []struct {
		addr    string
		enabled bool
	}{
		{"9.9.9.9:53", true},                            // Quad9 UDP
		{"tls://dns.quad9.net", true},                   // Quad9 DoT
		{"https://dns.mullvad.net/dns-query", true},     // Mullvad DoH
		{"1.1.1.1:53", true},                            // Cloudflare UDP
		{"https://cloudflare-dns.com/dns-query", false}, // Cloudflare DoH (disabled)
	}

	for _, u := range upstreams {
		_, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, ?)", u.addr, u.enabled)
		if err != nil {
			t.Fatalf("failed to seed upstream %s: %v", u.addr, err)
		}
	}

	if err := loadUpstreamsFromDB(); err != nil {
		t.Fatalf("failed to load upstreams: %v", err)
	}
}

// seedBlocklists inserts blocklists with realistic domain data and wires them
// to clients and groups. Returns the blocklist IDs for further use.
func seedBlocklists(t *testing.T) (adsListID, trackingListID, malwareListID int64) {
	t.Helper()

	// Blocklist 1: Ad domains
	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt",
		"Hagezi Pro", true)
	if err != nil {
		t.Fatalf("failed to seed blocklist: %v", err)
	}
	var fixtureErr118 error
	adsListID, fixtureErr118 = result.LastInsertId()
	if fixtureErr118 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr118)
	}

	adDomains := []string{
		"ads.google.com",
		"pagead2.googlesyndication.com",
		"ad.doubleclick.net",
		"ads.facebook.com",
		"pixel.facebook.com",
		"analytics.tiktok.com",
		"ads.yahoo.com",
		"advertising.amazon.com",
		"*.adserver.com",
		"tracking.ads.example.com",
	}
	for _, d := range adDomains {
		if _, fixtureErr133 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", adsListID, d); fixtureErr133 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr133)
		}
	}
	if _, fixtureErr135 := db.Exec("UPDATE blocklists SET domain_count = ? WHERE id = ?", len(adDomains), adsListID); fixtureErr135 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr135)
	}

	// Blocklist 2: Tracking domains
	result, err = db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://big.oisd.nl/",
		"OISD Big", true)
	if err != nil {
		t.Fatalf("failed to seed blocklist: %v", err)
	}
	var fixtureErr144 error
	trackingListID, fixtureErr144 = result.LastInsertId()
	if fixtureErr144 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr144)
	}

	trackingDomains := []string{
		"segment.io",
		"segment.com",
		"mixpanel.com",
		"amplitude.com",
		"hotjar.com",
		"fullstory.com",
		"mouseflow.com",
		"sentry.io",
		"bugsnag.com",
		"newrelic.com",
	}
	for _, d := range trackingDomains {
		if _, fixtureErr159 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", trackingListID, d); fixtureErr159 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr159)
		}
	}
	if _, fixtureErr161 := db.Exec("UPDATE blocklists SET domain_count = ? WHERE id = ?", len(trackingDomains), trackingListID); fixtureErr161 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr161)
	}

	// Blocklist 3: Malware (disabled)
	result, err = db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		"Steven Black Unified", false)
	if err != nil {
		t.Fatalf("failed to seed blocklist: %v", err)
	}
	var fixtureErr170 error
	malwareListID, fixtureErr170 = result.LastInsertId()
	if fixtureErr170 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr170)
	}

	malwareDomains := []string{
		"malware.example.com",
		"phishing.evil.net",
		"193.200.64.30",
	}
	for _, d := range malwareDomains {
		if _, fixtureErr178 := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", malwareListID, d); fixtureErr178 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr178)
		}
	}
	if _, fixtureErr180 := db.Exec("UPDATE blocklists SET domain_count = ? WHERE id = ?", len(malwareDomains), malwareListID); fixtureErr180 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr180)
	}

	return
}

// seedClientsAndGroups creates realistic client, group, and assignment data.
func seedClientsAndGroups(t *testing.T, adsListID, trackingListID int64) {
	t.Helper()

	// Client aliases
	clients := []struct {
		ip    string
		alias string
	}{
		{"10.42.1.42", "Chris Macbook"},
		{"10.42.1.43", "Sam iPhone"},
		{"10.42.1.44", "Sam Macbook"},
		{"10.42.1.100", "NAS box"},
		{"10.42.1.118", "Sophie Phone"},
		{"10.42.3.50", "IoT Thermostat"},
	}
	for _, c := range clients {
		if _, fixtureErr202 := db.Exec("INSERT INTO client_aliases (ip_address, alias) VALUES (?, ?)", c.ip, c.alias); fixtureErr202 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr202)
		}
	}

	// Seed some query logs so clients show up in getAllClients
	for _, c := range clients {
		if _, fixtureErr207 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?)",
			c.ip, "example.com.", "A", "NOERROR", false, 12); fixtureErr207 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr207)
		}
	}

	// Groups
	result, fixtureErr212 := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "Sam")
	if fixtureErr212 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr212)
	}
	dariaGroupID, fixtureErr213 := result.LastInsertId()
	if fixtureErr213 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr213)
	}

	var fixtureErr215 error
	result, fixtureErr215 = db.Exec("INSERT INTO client_groups (name) VALUES (?)", "Servers")
	if fixtureErr215 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr215)
	}
	serversGroupID, fixtureErr216 := result.LastInsertId()
	if fixtureErr216 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr216)
	}

	// Group members
	if _, fixtureErr219 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.42.1.43", dariaGroupID); fixtureErr219 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr219)
	}
	if _, fixtureErr220 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.42.1.44", dariaGroupID); fixtureErr220 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr220)
	}
	if _, fixtureErr221 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.42.1.100", serversGroupID); fixtureErr221 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr221)
	}

	// Blocklist assignments
	// Sam group gets ads blocklist
	if _, fixtureErr225 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", dariaGroupID, adsListID); fixtureErr225 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr225)
	}

	// Chris gets both ads and tracking directly
	if _, fixtureErr228 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", adsListID); fixtureErr228 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr228)
	}
	if _, fixtureErr229 := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", trackingListID); fixtureErr229 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr229)
	}

	// Servers group gets tracking blocklist
	if _, fixtureErr232 := db.Exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", serversGroupID, trackingListID); fixtureErr232 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr232)
	}
}

// seedRewrites inserts realistic DNS rewrite entries.
func seedRewrites(t *testing.T) {
	t.Helper()

	rewrites := []struct {
		domain  string
		ips     string
		enabled bool
	}{
		{"grafana.example.lan", "10.42.1.5", true},
		{"git.example.lan", "10.42.1.5", true},
		{"nas-box.example.lan", "10.42.1.100", true},
		{"plex.example.lan", "10.42.1.100 10.42.1.101", true},
		{"old.example.lan", "10.42.1.200", false},
	}
	for _, rw := range rewrites {
		if _, fixtureErr251 := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
			rw.domain, "", rw.ips, rw.enabled); fixtureErr251 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr251)
		}
	}

	if err := loadRewritesFromDB(); err != nil {
		t.Fatalf("failed to load rewrites: %v", err)
	}
}

// seedQueryLogs inserts a batch of realistic query log entries across multiple
// clients and domains for testing analytics and filtering.
func seedQueryLogs(t *testing.T) {
	t.Helper()

	logs := []struct {
		clientIP            string
		queryName           string
		queryType           string
		responseCode        string
		blocked             bool
		upstream            string
		latencyMicroseconds int
	}{
		{"10.42.1.42", "google.com.", "A", "NOERROR", false, "9.9.9.9:53", 15},
		{"10.42.1.42", "ads.google.com.", "A", "NXDOMAIN", true, "", 1},
		{"10.42.1.42", "github.com.", "A", "NOERROR", false, "tls://dns.quad9.net", 42},
		{"10.42.1.42", "segment.io.", "A", "NXDOMAIN", true, "", 1},
		{"10.42.1.42", "reddit.com.", "A", "NOERROR", false, "https://dns.mullvad.net/dns-query", 120},
		{"10.42.1.43", "facebook.com.", "A", "NOERROR", false, "9.9.9.9:53", 18},
		{"10.42.1.43", "ads.facebook.com.", "A", "NXDOMAIN", true, "", 1},
		{"10.42.1.43", "instagram.com.", "A", "NOERROR", false, "1.1.1.1:53", 22},
		{"10.42.1.43", "tiktok.com.", "A", "NOERROR", false, "https://dns.mullvad.net/dns-query", 95},
		{"10.42.1.100", "apt.ubuntu.com.", "A", "NOERROR", false, "9.9.9.9:53", 8},
		{"10.42.1.100", "registry.docker.io.", "A", "NOERROR", false, "tls://dns.quad9.net", 35},
		{"10.42.1.100", "newrelic.com.", "A", "NXDOMAIN", true, "", 1},
		{"10.42.1.100", "sentry.io.", "A", "NXDOMAIN", true, "", 1},
		{"10.42.3.50", "api.smartthings.com.", "A", "NOERROR", false, "1.1.1.1:53", 55},
		{"10.42.3.50", "telemetry.nest.com.", "A", "NOERROR", false, "9.9.9.9:53", 30},
		{"10.42.1.42", "google.com.", "AAAA", "NOERROR", false, "9.9.9.9:53", 16},
		{"10.42.1.118", "proctoring-app.edu.", "A", "NOERROR", false, "1.1.1.1:53", 25},
		{"10.42.1.118", "tiktok.com.", "A", "NOERROR", false, "https://dns.mullvad.net/dns-query", 88},
	}

	for _, l := range logs {
		if _, fixtureErr295 := db.Exec(`INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, upstream, latency_microseconds)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			l.clientIP, l.queryName, l.queryType, l.responseCode, l.blocked, l.upstream, l.latencyMicroseconds); fixtureErr295 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr295)
		}
	}
}

// setupFullTestEnv is a convenience that sets up the DB with all seed data
// and loads all in-memory stores. Returns a cleanup function.
func setupFullTestEnv(t *testing.T) func() {
	t.Helper()
	cleanup := setupTestDB(t)
	seedUpstreams(t)
	adsID, trackingID, _ := seedBlocklists(t)
	seedClientsAndGroups(t, adsID, trackingID)
	seedRewrites(t)
	seedQueryLogs(t)

	mustReloadPolicy(t)

	mustReloadPolicy(t)

	return cleanup
}

func decodeAPIData[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()

	type envelope[T any] struct {
		Data  T       `json:"data"`
		Error *string `json:"error"`
	}

	var resp envelope[T]
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode api response: %v\nbody: %s", err, rr.Body.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected api error: %s", *resp.Error)
	}
	return resp.Data
}

// mustReloadPolicy rebuilds the policy snapshot from the test database.
func mustReloadPolicy(tb testing.TB) {
	tb.Helper()
	if err := reloadPolicyState("test", reloadListContent); err != nil {
		tb.Fatalf("reload policy: %v", err)
	}
}

// Tests that require settings fail immediately rather than interpreting a read
// failure as an absent value. Production callers propagate availability errors.
func getSyncSetting(key string) string {
	settings, err := readSyncSettings()
	if err != nil {
		panic(err)
	}
	return settings[key]
}

// setTestRepositoryRoot supports both go test's package directory and crash
// subprocesses, which inherit the repository-root directory from their parent.
func setTestRepositoryRoot() error {
	if _, err := os.Stat("go.mod"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Chdir("../.."); err != nil {
		return err
	}
	_, err := os.Stat("go.mod")
	return err
}
