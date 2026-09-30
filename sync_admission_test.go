package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSyncTimestampAcceptsEveryFormatRealDatabasesHold(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Time
	}{
		{"2026-09-25T19:57:36Z", time.Date(2026, 9, 25, 19, 57, 36, 0, time.UTC)},
		{"2026-09-25T19:57:36.123456789Z", time.Date(2026, 9, 25, 19, 57, 36, 123456789, time.UTC)},
		{"2026-09-25T21:57:36+02:00", time.Date(2026, 9, 25, 19, 57, 36, 0, time.UTC)},
		{"2026-09-25 19:57:36", time.Date(2026, 9, 25, 19, 57, 36, 0, time.UTC)},
		{"2026-09-25 19:57:36.250", time.Date(2026, 9, 25, 19, 57, 36, 250000000, time.UTC)},
	}
	for _, tc := range cases {
		got, err := parseSyncTimestamp(tc.raw)
		if err != nil {
			t.Errorf("parseSyncTimestamp(%q) error = %v", tc.raw, err)
			continue
		}
		if !got.Equal(tc.want) || got.Location() != time.UTC {
			t.Errorf("parseSyncTimestamp(%q) = %v, want %v in UTC", tc.raw, got, tc.want)
		}
	}
}

func TestParseSyncTimestampRejectsValuesThatAreNotInstants(t *testing.T) {
	for _, raw := range []string{"", "~", "99999999", "2026-02-30T00:00:00Z", "2026-09-25T19:57:36", "yesterday", "2026-09-25T25:00:00Z"} {
		_, err := parseSyncTimestamp(raw)
		if err == nil || err.Error() != "sync timestamp is neither RFC3339 nor SQLite UTC" {
			t.Errorf("parseSyncTimestamp(%q) error = %v, want the not-an-instant error", raw, err)
		}
	}
}

func TestAdmitSyncTimestampBoundsTheFuture(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		raw, wantCanonical, wantReason string
	}{
		{"2026-09-25 19:00:00", "2026-09-25T19:00:00Z", ""},
		{"2026-09-25T20:05:00Z", "2026-09-25T20:05:00Z", ""},
		{"2026-09-25T20:05:00.000000001Z", "", rejectFutureTimestamp},
		{"9999-12-31T23:59:59Z", "", rejectFutureTimestamp},
		{"~", "", rejectInvalidTimestamp},
	}
	for _, tc := range cases {
		canonical, reason := admitSyncTimestamp(tc.raw, now)
		if canonical != tc.wantCanonical || reason != tc.wantReason {
			t.Errorf("admitSyncTimestamp(%q) = (%q, %q), want (%q, %q)", tc.raw, canonical, reason, tc.wantCanonical, tc.wantReason)
		}
	}
}

func TestSyncWriteWinsComparesInstants(t *testing.T) {
	cases := []struct {
		incoming, stored string
		want             bool
	}{
		{"2026-01-15T10:00:01Z", "2026-01-15T10:00:00Z", true},
		{"2026-01-15T10:00:00Z", "2026-01-15T10:00:00Z", false},
		{"2026-01-15T10:00:00Z", "2026-01-15T10:00:00.5Z", false},
		{"2026-01-15T11:00:00Z", "2026-01-15 10:00:00", true},
		{"2026-01-15T09:00:00Z", "2026-01-15 10:00:00", false},
		{"2026-01-15T09:00:00Z", "", true},
		{"2026-01-15T09:00:00Z", "~", true},
	}
	for _, tc := range cases {
		if got := syncWriteWins(tc.incoming, tc.stored); got != tc.want {
			t.Errorf("syncWriteWins(%q, %q) = %v, want %v", tc.incoming, tc.stored, got, tc.want)
		}
	}
}

func TestAdmitSyncResponseFiltersWithoutMutatingInput(t *testing.T) {
	now := time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)
	input := &SyncResponse{
		NodeID:     "svart-b",
		ServerTime: "2026-09-25T20:00:00Z",
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "tls://dns.quad9.net", Enabled: true, UpdatedAt: "2026-09-25 19:30:00", NodeID: "svart-b"},
				{Upstream: "tls://1.1.1.1", Enabled: true, UpdatedAt: "9999-12-31T23:59:59Z", NodeID: "svart-b"},
			},
			Ranges: []SyncRange{
				{CIDR: "10.42.50.0/24", Name: "IoT jail", UpdatedAt: "2026-09-25T19:59:00Z", NodeID: "svart-b"},
				{CIDR: "10.42.50.0/33", Name: "IoT jail", UpdatedAt: "2026-09-25T19:59:00Z", NodeID: "svart-b"},
			},
			AdminUsers: []SyncAdminUser{
				{Username: "chris", PasswordHash: "$2a$10$hash", Role: RoleAdmin, UpdatedAt: "2026-09-25T19:59:00Z", NodeID: "svart-b"},
			},
		},
		Tombstones: []Tombstone{
			{TableName: "rewrites", NaturalKey: "nas.home.arpa", DeletedAt: "2026-09-25T19:58:00+01:00", NodeID: "svart-b"},
			{TableName: "admin_users", NaturalKey: "chris", DeletedAt: "2026-09-25T19:58:00Z", NodeID: "svart-b"},
			{TableName: "query_logs", NaturalKey: "1", DeletedAt: "2026-09-25T19:58:00Z", NodeID: "svart-b"},
		},
	}
	snapshot := *input
	snapshot.Changes.Upstreams = append([]SyncUpstream(nil), input.Changes.Upstreams...)
	snapshot.Tombstones = append([]Tombstone(nil), input.Tombstones...)

	admitted, rejected := admitSyncResponse(input, now, false)

	want := &SyncResponse{
		NodeID:     "svart-b",
		ServerTime: "2026-09-25T20:00:00Z",
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{{Upstream: "tls://dns.quad9.net", Enabled: true, UpdatedAt: "2026-09-25T19:30:00Z", NodeID: "svart-b"}},
			Ranges:    []SyncRange{{CIDR: "10.42.50.0/24", Name: "IoT jail", UpdatedAt: "2026-09-25T19:59:00Z", NodeID: "svart-b"}},
		},
		Tombstones: []Tombstone{{TableName: "rewrites", NaturalKey: "nas.home.arpa", DeletedAt: "2026-09-25T18:58:00Z", NodeID: "svart-b"}},
	}
	if !reflect.DeepEqual(admitted, want) {
		t.Fatalf("admitted =\n%+v\nwant\n%+v", admitted, want)
	}
	wantRejected := []syncRejection{
		{Table: "upstreams", Reason: rejectFutureTimestamp},
		{Table: "ip_ranges", Reason: rejectInvalidRow},
		{Table: "admin_users", Reason: rejectIdentityDisabled},
		{Table: "sync_tombstones", Reason: rejectIdentityDisabled},
		{Table: "sync_tombstones", Reason: rejectInvalidRow},
	}
	if !reflect.DeepEqual(rejected, wantRejected) {
		t.Fatalf("rejected = %+v, want %+v", rejected, wantRejected)
	}
	if !reflect.DeepEqual(input.Changes.Upstreams, snapshot.Changes.Upstreams) || !reflect.DeepEqual(input.Tombstones, snapshot.Tombstones) {
		t.Fatal("admitSyncResponse modified its input")
	}
}

func TestParseSyncReplicateIdentity(t *testing.T) {
	for raw, want := range map[string]bool{"": false, "false": false, "0": false, "true": true, "TRUE": true, "1": true, " true ": true} {
		got, err := parseSyncReplicateIdentity(raw)
		if err != nil || got != want {
			t.Errorf("parseSyncReplicateIdentity(%q) = (%v, %v), want (%v, nil)", raw, got, err, want)
		}
	}
	for _, raw := range []string{"yes", "enabled", "on"} {
		got, err := parseSyncReplicateIdentity(raw)
		wantErr := `SYNC_REPLICATE_IDENTITY must be true or false, got "` + raw + `"`
		if got || err == nil || err.Error() != wantErr {
			t.Errorf("parseSyncReplicateIdentity(%q) = (%v, %v), want (false, %s)", raw, got, err, wantErr)
		}
	}
}

func TestConfigureSyncIdentityReplicationFailsClosed(t *testing.T) {
	logs := captureSyncLogs(t)
	saved := syncReplicateIdentity.Load()
	t.Cleanup(func() { syncReplicateIdentity.Store(saved) })

	syncReplicateIdentity.Store(true)
	configureSyncIdentityReplication("yes")
	if syncReplicateIdentity.Load() {
		t.Fatal("an unrecognised SYNC_REPLICATE_IDENTITY value enabled identity replication")
	}
	if !strings.Contains(logs.String(), "level=ERROR") || !strings.Contains(logs.String(), "invalid SYNC_REPLICATE_IDENTITY") {
		t.Fatalf("invalid value was not logged at error level:\n%s", logs.String())
	}

	configureSyncIdentityReplication("true")
	if !syncReplicateIdentity.Load() {
		t.Fatal("SYNC_REPLICATE_IDENTITY=true did not enable identity replication")
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "DD-029") {
		t.Fatalf("opt-in was not logged as a warning:\n%s", logs.String())
	}
}

func TestIdentityReplicationOptInKeepsReplicatingUsersTokensAndTombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t)

	before := time.Now().UTC().Add(-time.Second)
	if _, err := createAdminUser("chris", "correct horse battery staple", RoleAdmin); err != nil {
		t.Fatalf("createAdminUser: %v", err)
	}
	if err := recordTombstone("api_tokens", "sv_0badc0de"); err != nil {
		t.Fatalf("recordTombstone: %v", err)
	}
	built, err := buildSyncResponse(before)
	if err != nil {
		t.Fatalf("buildSyncResponse: %v", err)
	}
	if len(built.Changes.AdminUsers) != 1 || built.Changes.AdminUsers[0].Username != "chris" {
		t.Fatalf("admin_users = %+v, want chris", built.Changes.AdminUsers)
	}
	if len(built.Tombstones) != 1 || built.Tombstones[0].TableName != "api_tokens" {
		t.Fatalf("tombstones = %+v, want the api_tokens tombstone", built.Tombstones)
	}

	remoteTime := time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano)
	remoteHash := forgedBcryptHash(t)
	merge := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{AdminUsers: []SyncAdminUser{
		{Username: "sam", PasswordHash: remoteHash, Role: RoleReadonly, UpdatedAt: remoteTime, NodeID: "svart-b"},
	}}, Tombstones: []Tombstone{
		{TableName: "admin_users", NaturalKey: "chris", DeletedAt: remoteTime, NodeID: "svart-b"},
	}}
	if err := mergeSyncResponse(merge); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}
	users, err := listAdminUsers()
	if err != nil {
		t.Fatalf("listAdminUsers: %v", err)
	}
	if len(users) != 1 || users[0].Username != "sam" || users[0].Role != RoleReadonly {
		t.Fatalf("admin users = %+v, want only sam (readonly)", users)
	}
}

func TestIdentityReplicationOptInRejectsRowsTheHTTPAPIWouldReject(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t)

	remoteTime := time.Now().UTC().Format(time.RFC3339Nano)
	hash := forgedBcryptHash(t)
	resp := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{
		AdminUsers: []SyncAdminUser{
			{Username: "root", PasswordHash: hash, Role: "superadmin", UpdatedAt: remoteTime, NodeID: "svart-b"},
			{Username: "nopass", PasswordHash: "", Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"},
			{Username: "", PasswordHash: hash, Role: RoleAdmin, UpdatedAt: remoteTime, NodeID: "svart-b"},
			{Username: "future", PasswordHash: hash, Role: RoleAdmin, UpdatedAt: farFuture, NodeID: "svart-b"},
			{Username: "sam", PasswordHash: hash, Role: RoleReadonly, UpdatedAt: remoteTime, NodeID: "svart-b"},
		},
		APITokens: []SyncAPIToken{
			{TokenPrefix: "sv_deadbeef", Name: "root-token", TokenHash: hash, Role: "owner", UpdatedAt: remoteTime, NodeID: "svart-b"},
			{TokenPrefix: "sv_c0ffee00", Name: "grafana", TokenHash: hash, Role: RoleReadonly, UpdatedAt: remoteTime, NodeID: "svart-b"},
		},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}
	if got := queryString(t, "SELECT group_concat(username, ',') FROM admin_users"); got != "sam" {
		t.Errorf("admin_users = %q, want only sam", got)
	}
	if got := queryString(t, "SELECT group_concat(token_prefix, ',') FROM api_tokens"); got != "sv_c0ffee00" {
		t.Errorf("api_tokens = %q, want only sv_c0ffee00", got)
	}
}

func TestHandleAPIPeersGetReportsIdentityReplicationOptIn(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	enableIdentityReplicationForTest(t)

	w := httptest.NewRecorder()
	handleAPIPeersGet(w, httptest.NewRequest(http.MethodGet, "/api/peers", nil))
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q", *apiErr)
	}
	if got := requireFixtureType[map[string]interface{}](t, data)["replicate_identity"]; got != true {
		t.Fatalf("replicate_identity = %v, want true", got)
	}
}
