package main

import (
	"strings"
	"testing"
	"time"
)

const farFuture = "9999-12-31T23:59:59Z"

func mustExec(t *testing.T, query string, args ...interface{}) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func queryString(t *testing.T, query string, args ...interface{}) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func rowCount(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return count
}

func TestMergeSyncResponseRejectsFutureAndMalformedTimestamps(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const localUpdatedAt = "2026-01-15T08:30:00Z"
	mustExec(t, "INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES ('tls://dns.quad9.net', 1, ?, 'svart-a')", localUpdatedAt)

	withinSkew := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	beyondSkew := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	resp := &SyncResponse{
		NodeID:     "svart-b",
		ServerTime: withinSkew,
		Changes: SyncChanges{
			Upstreams: []SyncUpstream{
				{Upstream: "tls://dns.quad9.net", Enabled: false, UpdatedAt: farFuture, NodeID: "svart-b"},
				{Upstream: "https://dns.mullvad.net/dns-query", Enabled: true, UpdatedAt: "~", NodeID: "svart-b"},
				{Upstream: "tls://1.1.1.1", Enabled: true, UpdatedAt: "", NodeID: "svart-b"},
				{Upstream: "tls://dns.adguard-dns.com", Enabled: true, UpdatedAt: "2026-02-30T00:00:00Z", NodeID: "svart-b"},
				{Upstream: "tls://dns10.quad9.net", Enabled: true, UpdatedAt: "99999999", NodeID: "svart-b"},
				{Upstream: "tls://9.9.9.10", Enabled: true, UpdatedAt: withinSkew, NodeID: "svart-b"},
			},
			Rewrites: []SyncRewrite{
				{Domain: "nas.home.arpa", IPAddresses: "10.42.1.118", Enabled: true, UpdatedAt: beyondSkew, NodeID: "svart-b"},
			},
		},
	}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if got := queryString(t, "SELECT enabled || '|' || updated_at FROM upstreams WHERE upstream = 'tls://dns.quad9.net'"); got != "1|"+localUpdatedAt {
		t.Fatalf("quad9 enabled|updated_at = %q, want unchanged 1|%s", got, localUpdatedAt)
	}
	for _, rejected := range []string{"https://dns.mullvad.net/dns-query", "tls://1.1.1.1", "tls://dns.adguard-dns.com", "tls://dns10.quad9.net"} {
		if n := rowCount(t, "SELECT COUNT(*) FROM upstreams WHERE upstream = ?", rejected); n != 0 {
			t.Errorf("upstream %s with an invalid timestamp was inserted", rejected)
		}
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM upstreams WHERE upstream = 'tls://9.9.9.10'"); n != 1 {
		t.Errorf("upstream stamped inside the skew allowance was not merged")
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM rewrites WHERE domain = 'nas.home.arpa'"); n != 0 {
		t.Errorf("rewrite stamped an hour in the future was merged")
	}
}

func TestTombstoneStillDeletesRowAfterRejectedFutureUpdate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	mustExec(t, "INSERT INTO blocklists (url, alias, enabled, updated_at, node_id) VALUES ('https://raw.githubusercontent.com/hagezi/dns-blocklists/main/domains/pro.txt', 'Hagezi Pro', 1, '2026-01-15T08:30:00Z', 'svart-a')")

	poison := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{Blocklists: []SyncBlocklist{{
		Alias: "Hagezi Pro", URL: "https://attacker.example/pro.txt", Enabled: true, UpdatedAt: farFuture, NodeID: "svart-b",
	}}}}
	if err := mergeSyncResponse(poison); err != nil {
		t.Fatalf("merge poison: %v", err)
	}
	deletion := &SyncResponse{NodeID: "svart-green", Tombstones: []Tombstone{{
		TableName: "blocklists", NaturalKey: "Hagezi Pro", DeletedAt: time.Now().UTC().Format(time.RFC3339Nano), NodeID: "svart-green",
	}}}
	if err := mergeSyncResponse(deletion); err != nil {
		t.Fatalf("merge tombstone: %v", err)
	}

	if n := rowCount(t, "SELECT COUNT(*) FROM blocklists WHERE alias = 'Hagezi Pro'"); n != 0 {
		t.Fatalf("a future-stamped update made the blocklist undeletable (rows = %d)", n)
	}
}

func TestMergeSyncResponseRejectsFutureTombstones(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	localUpdatedAt := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	mustExec(t, "INSERT INTO rewrites (domain, target, ip_addresses, enabled, updated_at, node_id) VALUES ('printer.home.arpa', '', '10.42.1.60', 1, ?, 'svart-a')", localUpdatedAt)

	resp := &SyncResponse{NodeID: "svart-b", Tombstones: []Tombstone{
		{TableName: "rewrites", NaturalKey: "printer.home.arpa", DeletedAt: farFuture, NodeID: "svart-b"},
		{TableName: "rewrites", NaturalKey: "scanner.home.arpa", DeletedAt: "~", NodeID: "svart-b"},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if n := rowCount(t, "SELECT COUNT(*) FROM rewrites WHERE domain = 'printer.home.arpa'"); n != 1 {
		t.Fatal("a far-future tombstone deleted a live rewrite")
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM sync_tombstones WHERE table_name = 'rewrites'"); n != 0 {
		t.Fatalf("invalid tombstones were recorded for propagation (rows = %d)", n)
	}
}

// Real databases hold both syncNow()'s RFC3339Nano and SQLite's
// CURRENT_TIMESTAMP / datetime('now') form ("YYYY-MM-DD HH:MM:SS", UTC).
// Byte comparison misorders them within the same day because ' ' < 'T'.
func TestMergeSyncResponseOrdersLegacySQLiteTimestampsChronologically(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	mustExec(t, "UPDATE settings SET value = 'America/Chicago', updated_at = '2026-01-15 10:00:00', node_id = 'svart-a' WHERE key = 'timezone'")
	mustExec(t, "INSERT INTO client_aliases (ip_address, alias, updated_at, node_id) VALUES ('10.42.1.42', 'chris-desktop', '2026-01-15T11:00:00Z', 'svart-a')")

	resp := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{
		Settings: []SyncSetting{
			{Key: "timezone", Value: "Europe/Oslo", UpdatedAt: "2026-01-15T09:00:00Z", NodeID: "svart-b"},
		},
		ClientAliases: []SyncClientAlias{
			{IPAddress: "10.42.1.42", Alias: "chris-workstation", UpdatedAt: "2026-01-15 12:00:00", NodeID: "svart-b"},
		},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if got := getSyncSetting("timezone"); got != "America/Chicago" {
		t.Errorf("timezone = %q; an update from 09:00Z overwrote a 10:00 UTC legacy-format value", got)
	}
	if got := queryString(t, "SELECT alias FROM client_aliases WHERE ip_address = '10.42.1.42'"); got != "chris-workstation" {
		t.Errorf("alias = %q; a 12:00 UTC legacy-format update lost to an 11:00Z local value", got)
	}
}

// RFC3339Nano drops trailing zeros, so "10:00:00Z" sorts after
// "10:00:00.5Z" as bytes even though it is half a second earlier.
func TestMergeSyncResponseOrdersFractionalSecondsChronologically(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	mustExec(t, "INSERT INTO rewrites (domain, target, ip_addresses, enabled, updated_at, node_id) VALUES ('nas.home.arpa', '', '10.42.1.118', 1, '2026-01-15T10:00:00.5Z', 'svart-a')")
	resp := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{Rewrites: []SyncRewrite{
		{Domain: "nas.home.arpa", IPAddresses: "10.42.1.200", Enabled: true, UpdatedAt: "2026-01-15T10:00:00Z", NodeID: "svart-b"},
	}}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}
	if got := queryString(t, "SELECT ip_addresses FROM rewrites WHERE domain = 'nas.home.arpa'"); got != "10.42.1.118" {
		t.Fatalf("ip_addresses = %q; an older write won because of byte ordering", got)
	}
}

func TestTombstoneComparesLegacyTimestampsChronologically(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	mustExec(t, "INSERT INTO client_aliases (ip_address, alias, updated_at, node_id) VALUES ('10.42.1.50', 'living-room-tv', '2026-01-15 12:00:00', 'svart-a')")
	mustExec(t, "INSERT INTO client_aliases (ip_address, alias, updated_at, node_id) VALUES ('10.42.1.51', 'old-chromecast', '2026-01-15 12:00:00', 'svart-a')")

	resp := &SyncResponse{NodeID: "svart-b", Tombstones: []Tombstone{
		{TableName: "client_aliases", NaturalKey: "10.42.1.50", DeletedAt: "2026-01-15T10:00:00Z", NodeID: "svart-b"},
		{TableName: "client_aliases", NaturalKey: "10.42.1.51", DeletedAt: "2026-01-15T13:00:00Z", NodeID: "svart-b"},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM client_aliases WHERE ip_address = '10.42.1.50'"); n != 1 {
		t.Error("a 10:00Z tombstone deleted an alias written at 12:00 UTC")
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM client_aliases WHERE ip_address = '10.42.1.51'"); n != 0 {
		t.Error("a 13:00Z tombstone did not delete an alias written at 12:00 UTC")
	}
}

func TestInitSyncRepairsPoisonedTimestampsWithoutDeletingRows(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	logs := captureSyncLogs(t)

	forged := forgedBcryptHash(t)
	withinSkew := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	mustExec(t, "INSERT INTO admin_users (username, password_hash, role, updated_at, node_id) VALUES ('backdoor', ?, 'admin', ?, 'svart-b')", forged, farFuture)
	mustExec(t, "INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES ('tls://dns.quad9.net', 1, '~', 'svart-b')")
	mustExec(t, "INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES ('tls://9.9.9.10', 1, ?, 'svart-a')", withinSkew)
	mustExec(t, "INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES ('tls://dns.mullvad.net', 1, '2026-01-15 10:00:00', 'svart-a')")
	mustExec(t, "INSERT INTO sync_tombstones (table_name, natural_key, deleted_at, node_id) VALUES ('rewrites', 'nas.home.arpa', ?, 'svart-b')", farFuture)

	before := time.Now().UTC()
	initSync()
	after := time.Now().UTC()

	repairedAt := func(value string) {
		t.Helper()
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatalf("repaired timestamp %q is not RFC3339Nano: %v", value, err)
		}
		if parsed.Before(before) || parsed.After(after) {
			t.Fatalf("repaired timestamp %s is outside the repair window [%s, %s]", value, before, after)
		}
	}
	if got := queryString(t, "SELECT password_hash || '|' || role || '|' || node_id FROM admin_users WHERE username = 'backdoor'"); got != forged+"|admin|svart-b" {
		t.Fatalf("poisoned admin row content changed: %q", got)
	}
	repairedAt(queryString(t, "SELECT CAST(updated_at AS TEXT) FROM admin_users WHERE username = 'backdoor'"))
	repairedAt(queryString(t, "SELECT CAST(updated_at AS TEXT) FROM upstreams WHERE upstream = 'tls://dns.quad9.net'"))
	repairedAt(queryString(t, "SELECT CAST(deleted_at AS TEXT) FROM sync_tombstones WHERE table_name = 'rewrites' AND natural_key = 'nas.home.arpa'"))
	if got := queryString(t, "SELECT CAST(updated_at AS TEXT) FROM upstreams WHERE upstream = 'tls://9.9.9.10'"); got != withinSkew {
		t.Errorf("timestamp inside the skew allowance was rewritten: %q", got)
	}
	if got := queryString(t, "SELECT CAST(updated_at AS TEXT) FROM upstreams WHERE upstream = 'tls://dns.mullvad.net'"); got != "2026-01-15 10:00:00" {
		t.Errorf("legacy SQLite timestamp was rewritten: %q", got)
	}

	output := logs.String()
	if strings.Count(output, "level=ERROR") != 3 {
		t.Errorf("want one ERROR line per repaired row (3):\n%s", output)
	}
	for _, want := range []string{"original=" + farFuture, "original=~", "table=admin_users", "table=upstreams", "table=sync_tombstones"} {
		if !strings.Contains(output, want) {
			t.Errorf("repair log missing %q:\n%s", want, output)
		}
	}
}

func TestMergeSyncResponseRejectsRowsTheHTTPAPIWouldReject(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	recent := time.Now().UTC().Format(time.RFC3339Nano)
	resp := &SyncResponse{NodeID: "svart-b", Changes: SyncChanges{
		Ranges: []SyncRange{
			{CIDR: "10.42.40.0/33", Name: "Guest VLAN", UpdatedAt: recent, NodeID: "svart-b"},
			{CIDR: "iot-vlan", Name: "IoT", UpdatedAt: recent, NodeID: "svart-b"},
			{CIDR: "10.42.60.0/24", Name: "", UpdatedAt: recent, NodeID: "svart-b"},
			{CIDR: "10.42.50.0/24", Name: "IoT jail", UpdatedAt: recent, NodeID: "svart-b"},
		},
		Policies: []SyncPolicy{
			{Name: "   ", Description: "blank", UpdatedAt: recent, NodeID: "svart-b"},
			{Name: "Kids", Description: "Hagezi Pro + custom", UpdatedAt: recent, NodeID: "svart-b"},
		},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}

	if got := queryString(t, "SELECT group_concat(cidr, ',') FROM ip_ranges"); got != "10.42.50.0/24" {
		t.Errorf("ip_ranges = %q, want only 10.42.50.0/24", got)
	}
	if got := queryString(t, "SELECT group_concat(name, ',') FROM policies"); got != "Kids" {
		t.Errorf("policies = %q, want only Kids", got)
	}
}

func TestMergeSyncResponseRejectsTombstonesForUnknownTables(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	resp := &SyncResponse{NodeID: "svart-b", Tombstones: []Tombstone{
		{TableName: "query_logs", NaturalKey: "1", DeletedAt: time.Now().UTC().Format(time.RFC3339Nano), NodeID: "svart-b"},
	}}
	if err := mergeSyncResponse(resp); err != nil {
		t.Fatalf("mergeSyncResponse: %v", err)
	}
	if n := rowCount(t, "SELECT COUNT(*) FROM sync_tombstones"); n != 0 {
		t.Fatalf("tombstone for a non-replicated table was stored for propagation (rows = %d)", n)
	}
}
