package main

import (
	"strings"
	"testing"
	"time"
)

func TestSyncLWWRetainedTombstonesPreventResurrection(t *testing.T) {
	for _, fixture := range []struct {
		table, key, countQuery string
		response               func(string) *SyncResponse
	}{
		{"upstreams", "192.0.2.53:53", "SELECT COUNT(*) FROM upstreams WHERE upstream='192.0.2.53:53'", func(ts string) *SyncResponse {
			return &SyncResponse{Changes: SyncChanges{Upstreams: []SyncUpstream{{Upstream: "192.0.2.53:53", Enabled: true, UpdatedAt: ts, NodeID: "peer-two"}}}}
		}},
		{"rewrites", "printer.example", "SELECT COUNT(*) FROM rewrites WHERE domain='printer.example'", func(ts string) *SyncResponse {
			return &SyncResponse{Changes: SyncChanges{Rewrites: []SyncRewrite{{Domain: "printer.example", IPAddresses: "192.0.2.10", Enabled: true, UpdatedAt: ts, NodeID: "peer-two"}}}}
		}},
	} {
		t.Run(fixture.table, func(t *testing.T) {
			defer setupTestDB(t)()
			deleted := time.Now().UTC().Add(-time.Minute)
			if _, err := db.Exec("INSERT INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) VALUES(?,?,?,'peer-one')", fixture.table, fixture.key, deleted.Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			for _, step := range []struct {
				at   time.Time
				want int
			}{{deleted.Add(-time.Second), 0}, {deleted, 0}, {deleted.Add(time.Second), 1}} {
				if err := mergeSyncResponse(fixture.response(step.at.Format(time.RFC3339Nano))); err != nil {
					t.Fatal(err)
				}
				var count int
				if err := db.QueryRow(fixture.countQuery).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != step.want {
					t.Fatalf("incoming=%s live rows=%d want%d", step.at, count, step.want)
				}
			}
		})
	}
}

func TestSyncLWWReadFailureRollsBackEarlierSettings(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("DROP TABLE upstreams"); err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	response := &SyncResponse{Changes: SyncChanges{Settings: []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: timestamp}}, Upstreams: []SyncUpstream{{Upstream: "192.0.2.53:53", Enabled: true, UpdatedAt: timestamp}}}}
	err := mergeSyncResponse(response)
	if err == nil || !strings.Contains(err.Error(), "read upstreams row: no such table: upstreams") {
		t.Fatalf("read failure=%v, want explicit upstream row read error", err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='cache_ttl'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "3600" {
		t.Fatalf("partial settings mutation=%q want3600", value)
	}
}
