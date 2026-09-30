package main

import (
	"database/sql"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestStaleListMetricUnavailableOnDatabaseFailure(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("DROP TABLE allowlists"); err != nil {
		t.Fatal(err)
	}
	if got := countStaleLists(); !math.IsNaN(got) {
		t.Fatalf("failed discovery reported %v stale lists, want unavailable (NaN)", got)
	}
}

func TestRefreshDiscoveryUsesReadPool(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("INSERT INTO blocklists (url,alias,enabled) VALUES ('https://lists.example/pro.txt','Pro',1)"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestRollback(t, tx) }()
	type outcome struct {
		lists []staleList
		err   error
	}
	done := make(chan outcome, 1)
	go func() { lists, err := findStaleLists(); done <- outcome{lists, err} }()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if len(got.lists) != 1 || got.lists[0].Alias != "Pro" {
			t.Fatalf("discovered lists = %+v", got.lists)
		}
	case <-time.After(2 * time.Second):
		// Release the lock before failing so no goroutine outlives the fixture.
		checkTestRollback(t, tx)
		<-done
		t.Fatal("list discovery waited for the writer pool")
	}
}

func TestRefreshDiscoveryReturnsNoPartialLists(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec("INSERT INTO blocklists (url,alias,enabled) VALUES ('https://lists.example/pro.txt','Pro',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE allowlists"); err != nil {
		t.Fatal(err)
	}
	for _, find := range []func() ([]staleList, error){findStaleLists, findEmptyLists} {
		lists, err := find()
		if err == nil || lists != nil {
			t.Fatalf("partial discovery: lists=%v error=%v", lists, err)
		}
	}
}

func TestRefreshRetryBackoffAndSourceChanges(t *testing.T) {
	retries := make(refreshRetries)
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	list := staleList{ID: 1, Type: "blocklist", URL: "https://lists.example/pro.txt"}
	failure := errors.New("fetch unavailable")
	if !retries.ready(list, now) {
		t.Fatal("new/empty list was delayed")
	}
	for _, want := range []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour} {
		retries.record(list, now, failure)
		if got := retries[refreshKey{list.Type, list.ID}].delay; got != want {
			t.Fatalf("delay %s, want %s", got, want)
		}
		if retries.ready(list, now.Add(want-time.Nanosecond)) {
			t.Fatal("retry before backoff expired")
		}
		now = now.Add(want)
		if !retries.ready(list, now) {
			t.Fatal("retry not ready at deadline")
		}
	}
	changed := list
	changed.URL = "https://lists.example/new.txt"
	if !retries.ready(changed, now.Add(-time.Second)) {
		t.Fatal("new source inherited old backoff")
	}
	changed = list
	changed.LastUpdated = "2026-09-28 00:00:00"
	if !retries.ready(changed, now.Add(-time.Second)) {
		t.Fatal("manual refresh did not reset old backoff")
	}
	retries.record(list, now, nil)
	if len(retries) != 0 {
		t.Fatal("successful refresh retained retry state")
	}
}

func TestAutomaticRetryDoesNotPreventManualRefresh(t *testing.T) {
	defer setupTestDB(t)()
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	server := newGatedListServer(t, "ads.example")
	result, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES(?,'Recovery',1)", server.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	lists, err := findEmptyLists()
	if err != nil {
		t.Fatal(err)
	}
	retries := make(refreshRetries)
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	retries.record(lists[0], now, errors.New("earlier failure"))
	if retries.ready(lists[0], now) {
		t.Fatal("expected backoff")
	}
	if err := refreshBlocklistByID(int(id)); err != nil {
		t.Fatal(err)
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); !reflect.DeepEqual(got, []string{"ads.example"}) {
		t.Fatalf("manual recovery rules=%v", got)
	}
}

func TestSuccessfulEmptyRefreshDoesNotRepeatDiscovery(t *testing.T) {
	for _, store := range []listStore{blockListStore, allowListStore} {
		t.Run(store.kind, func(t *testing.T) {
			defer setupTestDB(t)()
			t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
			server := newGatedListServer(t, "! valid empty list")
			result, err := db.Exec("INSERT INTO "+store.lists+"(url,alias,enabled,refresh_interval,last_updated) VALUES(?,'Empty recovery',1,0,CURRENT_TIMESTAMP)", server.srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			lists, err := findEmptyLists()
			if err != nil {
				t.Fatal(err)
			}
			if len(lists) != 1 {
				t.Fatalf("new-node recovery candidates=%v", lists)
			}
			if err := refreshListByID(store, int(id)); err != nil {
				t.Fatal(err)
			}
			lists, err = findEmptyLists()
			if err != nil {
				t.Fatal(err)
			}
			if len(lists) != 0 {
				t.Fatalf("successfully downloaded empty list keeps retrying: %v", lists)
			}
		})
	}
}

func TestEmptyGenerationPersistsAndNewSourceRecovers(t *testing.T) {
	defer setupTestDB(t)()
	for _, store := range []listStore{blockListStore, allowListStore} {
		result, err := db.Exec("INSERT INTO "+store.lists+"(url,alias,enabled,refresh_interval,last_updated) VALUES('https://lists.example/empty.txt',?,1,0,CURRENT_TIMESTAMP)", store.kind)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if err := storeListGeneration(store, int(id), nil, 0, "https://lists.example/empty.txt", nil); err != nil {
			t.Fatal(err)
		}
	}
	// A new connection has no memory of previous downloads or worker retries.
	var seq int
	var name, path string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite3", path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, reopened) }()
	original := readDB
	readDB = reopened
	defer func() { readDB = original }()
	lists, err := findEmptyLists()
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 0 {
		t.Fatalf("persisted empty generations requeued: %v", lists)
	}
	if _, err := db.Exec("UPDATE allowlists SET url='https://lists.example/new.txt'"); err != nil {
		t.Fatal(err)
	}
	lists, err = findEmptyLists()
	if err != nil {
		t.Fatal(err)
	}
	if len(lists) != 1 || lists[0].Type != "allowlist" {
		t.Fatalf("changed source recovery candidates=%v", lists)
	}
}

func TestDownloadWithChangedSourceCannotPublish(t *testing.T) {
	defer setupTestDB(t)()
	result, err := db.Exec("INSERT INTO blocklists(url,alias,enabled) VALUES('https://lists.example/new.txt','Changed source',1)")
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := storeListGeneration(blockListStore, int(id), []string{"ads.example"}, 1, "https://lists.example/old.txt", nil); err == nil {
		t.Fatal("superseded download published")
	}
	if got := storedRules(t, "blocked_domains", "blocklist_id", int(id)); len(got) != 0 {
		t.Fatalf("superseded rules stored: %v", got)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM local_blocklist_generations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("superseded download recorded %d local generations", count)
	}
}
