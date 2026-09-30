package svart

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/bcrypt"
)

// syncRejectedRows reads one series of the rejected-row counter from the
// default registry, so the assertion observes exactly what /metrics serves.
func syncRejectedRows(t *testing.T, peer, table, reason string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "svart_dns_sync_rows_rejected_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["peer"] == peer && labels["table"] == table && labels["reason"] == reason {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func captureSyncLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	saved := logSync
	logSync = slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { logSync = saved })
	return &output
}

func forgedBcryptHash(t *testing.T) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("attacker-chosen-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(hash)
}

func TestBuildSyncResponseOmitsIdentityTablesByDefault(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	before := time.Now().UTC().Add(-time.Second)
	if _, err := createAdminUser("chris", "correct horse battery staple", RoleAdmin); err != nil {
		t.Fatalf("createAdminUser: %v", err)
	}
	if _, _, err := createAPIToken("grafana-scraper", RoleReadonly); err != nil {
		t.Fatalf("createAPIToken: %v", err)
	}
	for _, tombstone := range [][2]string{
		{"admin_users", "former-admin"},
		{"api_tokens", "sv_0badc0de"},
		{"upstreams", "tls://dns.quad9.net"},
	} {
		if err := recordTombstone(tombstone[0], tombstone[1]); err != nil {
			t.Fatalf("recordTombstone %v: %v", tombstone, err)
		}
	}

	resp, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}
	if resp.Changes.AdminUsers != nil {
		t.Fatalf("admin_users replicated without opt-in: %+v", resp.Changes.AdminUsers)
	}
	if resp.Changes.APITokens != nil {
		t.Fatalf("api_tokens replicated without opt-in: %+v", resp.Changes.APITokens)
	}
	var tombstoneTables []string
	for _, tombstone := range resp.Tombstones {
		tombstoneTables = append(tombstoneTables, tombstone.TableName+"/"+tombstone.NaturalKey)
	}
	if want := []string{"upstreams/tls://dns.quad9.net"}; !equalStrings(tombstoneTables, want) {
		t.Fatalf("tombstones = %v, want %v", tombstoneTables, want)
	}
}

func TestMergeSyncResponseIgnoresIdentityRowsAndTombstonesByDefault(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := createAdminUser("chris", "correct horse battery staple", RoleAdmin); err != nil {
		t.Fatalf("createAdminUser: %v", err)
	}
	_, localHash, _, _, found, fixtureErr3125 := getAdminUserByUsername("chris")
	if fixtureErr3125 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3125)
	}
	if !found {
		t.Fatal("local admin missing after create")
	}
	_, localToken, err := createAPIToken("grafana-scraper", RoleReadonly)
	if err != nil {
		t.Fatalf("createAPIToken: %v", err)
	}

	// Newer than the local rows and inside the clock-skew allowance, so only
	// the identity gate can keep these out.
	remoteTime := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	forged := forgedBcryptHash(t)
	resp := &SyncResponse{
		NodeID:     "svart-b",
		ServerTime: remoteTime,
		Changes: SyncChanges{
			AdminUsers: []SyncAdminUser{
				{Username: "backdoor", PasswordHash: forged, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"},
				{Username: "chris", PasswordHash: forged, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"},
			},
			APITokens: []SyncAPIToken{
				{TokenPrefix: "sv_deadbeef", Name: "attacker", TokenHash: forged, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"},
			},
			Rewrites: []SyncRewrite{
				{Domain: "nas.home.arpa", IPAddresses: "10.42.1.118", Enabled: true, UpdatedAt: remoteTime, NodeID: "svart-b"},
			},
		},
		Tombstones: []Tombstone{
			{TableName: "admin_users", NaturalKey: "chris", DeletedAt: remoteTime, NodeID: "svart-b"},
			{TableName: "api_tokens", NaturalKey: localToken.TokenPrefix, DeletedAt: remoteTime, NodeID: "svart-b"},
		},
	}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	users, err := listAdminUsers()
	if err != nil {
		t.Fatalf("listAdminUsers: %v", err)
	}
	var usernames []string
	for _, user := range users {
		usernames = append(usernames, user.Username)
	}
	if want := []string{"chris"}; !equalStrings(usernames, want) {
		t.Fatalf("admin users = %v, want %v", usernames, want)
	}
	if _, hash, _, _, found, err := getAdminUserByUsername("chris"); err != nil || !found || hash != localHash {
		t.Fatal("remote peer overwrote the local admin password hash")
	}
	tokens, err := listAPITokens()
	if err != nil {
		t.Fatalf("listAPITokens: %v", err)
	}
	var prefixes []string
	for _, token := range tokens {
		prefixes = append(prefixes, token.TokenPrefix)
	}
	if want := []string{localToken.TokenPrefix}; !equalStrings(prefixes, want) {
		t.Fatalf("api token prefixes = %v, want %v", prefixes, want)
	}
	var identityTombstones int
	if err := db.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name IN ('admin_users','api_tokens')").Scan(&identityTombstones); err != nil {
		t.Fatalf("count identity tombstones: %v", err)
	}
	if identityTombstones != 0 {
		t.Fatalf("identity tombstones recorded for propagation = %d, want 0", identityTombstones)
	}
	var rewriteIPs string
	if err := db.QueryRow("SELECT ip_addresses FROM rewrites WHERE domain = 'nas.home.arpa'").Scan(&rewriteIPs); err != nil {
		t.Fatalf("non-identity row in the same payload was not merged: %v", err)
	}
	if rewriteIPs != "10.42.1.118" {
		t.Fatalf("rewrite ip_addresses = %q, want 10.42.1.118", rewriteIPs)
	}
}

func TestSyncOnceCountsRejectedRowsPerPeerAndWarnsOncePerType(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	logs := captureSyncLogs(t)

	remoteTime := time.Now().UTC().Format(time.RFC3339Nano)
	forged := forgedBcryptHash(t)
	payload := SyncResponse{
		NodeID:     "svart-b",
		ServerTime: remoteTime,
		Changes: SyncChanges{
			AdminUsers: []SyncAdminUser{{Username: "backdoor", PasswordHash: forged, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"}},
			APITokens:  []SyncAPIToken{{TokenPrefix: "sv_deadbeef", Name: "attacker", TokenHash: forged, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"}},
			Upstreams:  []SyncUpstream{{Upstream: "tls://dns.quad9.net", Enabled: false, UpdatedAt: "9999-12-31T23:59:59Z", NodeID: "svart-b"}},
			Rewrites: []SyncRewrite{
				{Domain: "printer.home.arpa", IPAddresses: "10.42.1.60", Enabled: true, UpdatedAt: "~", NodeID: "svart-b"},
				{Domain: "nas.home.arpa", IPAddresses: "10.42.1.118", Enabled: true, UpdatedAt: remoteTime, NodeID: "svart-b"},
			},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, payload)
	}))
	defer server.Close()

	const label = "peer-rejection-regression"
	series := []struct{ table, reason string }{
		{"admin_users", "identity_replication_disabled"},
		{"api_tokens", "identity_replication_disabled"},
		{"upstreams", "future_timestamp"},
		{"rewrites", "invalid_timestamp"},
	}
	// The counter lives in the process-wide registry, so compare deltas.
	baseline := make([]float64, len(series))
	for i, s := range series {
		baseline[i] = syncRejectedRows(t, label, s.table, s.reason)
	}

	peer := &peerState{URL: server.URL, MetricLabel: label}
	syncOnce(server.Client(), peer, testSyncSecret)
	syncOnce(server.Client(), peer, testSyncSecret)

	if !peer.Healthy || peer.ConsecutiveErrors != 0 {
		t.Fatalf("rejected rows marked the peer unhealthy: healthy=%v errors=%d last_error=%q", peer.Healthy, peer.ConsecutiveErrors, peer.LastError)
	}
	for i, s := range series {
		if got := syncRejectedRows(t, label, s.table, s.reason) - baseline[i]; got != 2 {
			t.Errorf("rejected %s/%s increased by %v, want 2 (one per poll)", s.table, s.reason, got)
		}
	}

	output := logs.String()
	for _, table := range []string{"table=admin_users", "table=api_tokens", "table=upstreams", "table=rewrites"} {
		if got := strings.Count(output, table); got != 1 {
			t.Errorf("warn lines with %s = %d, want exactly 1 across two polls\n%s", table, got, output)
		}
	}
	if !strings.Contains(output, "level=WARN") {
		t.Errorf("rejections were not logged at warn level:\n%s", output)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&count); err != nil || count != 0 {
		t.Fatalf("admin_users rows = %d (err %v), want 0", count, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM rewrites WHERE domain = 'nas.home.arpa'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("valid rewrite rows = %d (err %v), want 1", count, err)
	}
}

func TestHandleAPIPeersGetReportsIdentityReplicationDisabledByDefault(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handleAPIPeersGet(w, httptest.NewRequest(http.MethodGet, "/api/peers", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q", *apiErr)
	}
	got, present := requireFixtureType[map[string]interface{}](t, data)["replicate_identity"]
	if !present || got != false {
		t.Fatalf("replicate_identity = %v (present=%v), want false", got, present)
	}
}

func equalStrings(got, want []string) bool {
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// enableIdentityReplicationForTest opts this test into SYNC_REPLICATE_IDENTITY
// and restores the default afterwards.
func enableIdentityReplicationForTest(t *testing.T) {
	t.Helper()
	saved := syncReplicateIdentity.Load()
	syncReplicateIdentity.Store(true)
	t.Cleanup(func() { syncReplicateIdentity.Store(saved) })
}
