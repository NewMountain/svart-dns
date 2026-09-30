package svart

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSyncMissingParentAutomaticallyRetriesFullSnapshot(t *testing.T) {
	cleanup := setupTestDB(t)
	seedSyncRelationshipParents(t)
	if err := mergeSyncResponse(&SyncResponse{Changes: syncRelationshipFixtures()[1].changes("2023-01-01T00:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	cursor := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	delta, err := buildSyncResponse(cursor)
	if err != nil {
		t.Fatal(err)
	}
	full, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Changes.Blocklists) != 0 || len(delta.Changes.ClientBlocklists) != 1 {
		t.Fatalf("delta blocklists=%d assignments=%d, want0 and1", len(delta.Changes.Blocklists), len(delta.Changes.ClientBlocklists))
	}
	cleanup()
	defer setupTestDB(t)()
	requested := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		since := r.URL.Query().Get("since")
		requested <- since
		if since == "0001-01-01T00:00:00Z" {
			writeJSON(w, 200, full)
		} else {
			writeJSON(w, 200, delta)
		}
	}))
	defer server.Close()
	peer := &peerState{URL: server.URL, LastSyncAt: cursor}
	syncOnce(server.Client(), peer, "")
	if len(requested) != 2 {
		t.Fatalf("requests=%d, want 2", len(requested))
	}
	first, second := <-requested, <-requested
	if first != "2022-01-01T00:00:00Z" || second != "0001-01-01T00:00:00Z" {
		t.Fatalf("cursors=%s,%s, want delta then full", first, second)
	}
	if !peer.Healthy || peer.LastSyncAt.Format(time.RFC3339Nano) != full.ServerTime || peer.LastError != "" {
		t.Fatalf("full retry healthy=%v cursor=%s error=%q", peer.Healthy, peer.LastSyncAt, peer.LastError)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists cb JOIN blocklists b ON cb.blocklist_id=b.id WHERE cb.client_ip='192.0.2.10' AND b.alias='Manual block'", 1)
}

func TestSyncMissingParentFullRetryIsBoundedAndKeepsCursor(t *testing.T) {
	defer setupTestDB(t)()
	response := &SyncResponse{Changes: syncRelationshipFixtures()[1].changes("2023-01-01T00:00:00Z"), ServerTime: "2024-01-01T00:00:00Z"}
	response.Changes.Settings = []SyncSetting{{Key: "cache_ttl", Value: "120", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}}
	var calls atomic.Int32
	var responseMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		responseMu.Lock()
		defer responseMu.Unlock()
		payload := *response
		if r.URL.Query().Get("since") != "0001-01-01T00:00:00Z" {
			payload.Changes.Blocklists = nil
		}
		writeJSON(w, 200, &payload)
	}))
	defer server.Close()
	cursor := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	peer := &peerState{URL: server.URL, LastSyncAt: cursor}
	syncOnce(server.Client(), peer, "")
	if calls.Load() != 2 || peer.Healthy || !peer.LastSyncAt.Equal(cursor) || !strings.Contains(peer.LastError, "missing reference") {
		t.Fatalf("failed retry calls=%d healthy=%v cursor=%s error=%q", calls.Load(), peer.Healthy, peer.LastSyncAt, peer.LastError)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists", 0)
	assertSyncCount(t, "SELECT COUNT(*) FROM settings WHERE key='cache_ttl' AND value='3600'", 1)
	// A later poll must make progress automatically once the source parent is repaired.
	responseMu.Lock()
	response.Changes.Blocklists = []SyncBlocklist{{Alias: "Manual block", URL: "", Enabled: true, UpdatedAt: "2020-01-01T00:00:00Z"}}
	responseMu.Unlock()
	syncOnce(server.Client(), peer, "")
	if calls.Load() != 4 || !peer.Healthy || peer.LastSyncAt.Format(time.RFC3339) != "2024-01-01T00:00:00Z" {
		t.Fatalf("repaired source calls=%d healthy=%v cursor=%s", calls.Load(), peer.Healthy, peer.LastSyncAt)
	}
	assertSyncCount(t, "SELECT COUNT(*) FROM client_blocklists", 1)
}

func TestSyncReferenceDatabaseFailureDoesNotRetryFull(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("DROP TABLE blocklists"); err != nil {
		t.Fatal(err)
	}
	response := &SyncResponse{Changes: syncRelationshipFixtures()[1].changes("2023-01-01T00:00:00Z"), ServerTime: "2024-01-01T00:00:00Z"}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); writeJSON(w, 200, response) }))
	defer server.Close()
	cursor := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	peer := &peerState{URL: server.URL, LastSyncAt: cursor}
	syncOnce(server.Client(), peer, "")
	if calls.Load() != 1 || peer.Healthy || !peer.LastSyncAt.Equal(cursor) || !strings.Contains(peer.LastError, "read reference: no such table") {
		t.Fatalf("database failure calls=%d healthy=%v cursor=%s error=%q", calls.Load(), peer.Healthy, peer.LastSyncAt, peer.LastError)
	}
}
