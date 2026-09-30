package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSyncDiffOrdersMixedTimestampFormatsExactly(t *testing.T) {
	defer setupTestDB(t)()
	for _, r := range []struct{ key, stamp string }{{"legacy", "2026-01-02 03:04:05.000000002"}, {"fraction", "2026-01-02T03:04:05.000000003Z"}, {"whole", "2026-01-02T03:04:05Z"}} {
		if _, err := db.Exec("INSERT INTO settings(key,value,updated_at) VALUES(?,'yes',?)", r.key, r.stamp); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, 1, 2, 3, 4, 5, 1, time.UTC)
	resp, err := buildSyncResponse(since)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, s := range resp.Changes.Settings {
		if s.Key == "legacy" || s.Key == "fraction" || s.Key == "whole" {
			got[s.Key] = true
		}
	}
	if len(got) != 2 || !got["legacy"] || !got["fraction"] {
		t.Fatalf("changes = %#v, want legacy and fraction only", got)
	}
	resp, err = buildSyncResponse(since.Add(2 * time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range resp.Changes.Settings {
		if s.Key == "legacy" || s.Key == "fraction" || s.Key == "whole" {
			t.Fatalf("replayed row: %#v", s)
		}
	}
}

func TestSyncFutureCursorCannotStallNextValidPoll(t *testing.T) {
	defer setupTestDB(t)()
	valid := time.Now().UTC().Add(-time.Second)
	stamp := valid.Add(100 * 365 * 24 * time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := &SyncResponse{ServerTime: stamp.Format(time.RFC3339Nano)}
		requested, err := time.Parse(time.RFC3339Nano, r.URL.Query().Get("since"))
		if err != nil {
			t.Errorf("invalid requested cursor: %v", err)
		}
		if stamp.Equal(valid.Add(time.Millisecond)) && requested.Before(stamp) {
			response.Changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "7200", UpdatedAt: stamp.Format(time.RFC3339Nano)}}
		}
		writeJSON(w, 200, response)
	}))
	defer server.Close()
	peer := &peerState{URL: server.URL, LastSyncAt: valid}
	syncOnce(server.Client(), peer, "")
	if !peer.LastSyncAt.Equal(valid) {
		t.Errorf("future cursor advanced to %s, want %s", peer.LastSyncAt, valid)
	}
	if peer.Healthy {
		t.Error("future cursor marked peer healthy")
	}
	stamp = valid.Add(time.Millisecond)
	syncOnce(server.Client(), peer, "")
	if !peer.LastSyncAt.Equal(stamp) || !peer.Healthy {
		t.Errorf("valid next poll: cursor=%s healthy=%v", peer.LastSyncAt, peer.Healthy)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='cache_ttl'").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "7200" {
		t.Fatalf("valid change after rejected cursor=%q, want 7200", value)
	}
}

func TestFreshNodeDefaultsCannotOverwriteMeshSettings(t *testing.T) {
	// Capture an actual fresh node's outbound seed configuration before creating
	// the customized peer. Both databases are disposable and distinct.
	cleanup := setupTestDB(t)
	fresh, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	defer setupTestDB(t)()
	if _, err := db.Exec("UPDATE settings SET value='7200', updated_at='2020-01-01T00:00:00Z', node_id='custom-node' WHERE key='cache_ttl'"); err != nil {
		t.Fatal(err)
	}
	if err := mergeSyncResponse(fresh); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := db.QueryRow("SELECT value FROM settings WHERE key='cache_ttl'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "7200" {
		t.Fatalf("custom mesh cache_ttl = %q, want 7200", got)
	}
}
