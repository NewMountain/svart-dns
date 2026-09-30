package main

import (
	"reflect"
	"strings"
	"testing"
)

// The local generation tables are initialized after all backfills and the
// approved token scrub. A failure there must still undo the entire startup.
func TestMigrationIndependentLateInitializationFailure(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t,
		`DELETE FROM settings WHERE key='cache_ttl'`,
		`DROP TABLE sessions`,
		`ALTER TABLE query_logs DROP COLUMN block_rule`,
		`DROP TABLE local_allowlist_generations`,
		`CREATE TABLE local_allowlist_generations(list_id INTEGER PRIMARY KEY, legacy_payload BLOB)`,
		`INSERT INTO local_allowlist_generations VALUES(83001,x'0001ff007f')`,
		`INSERT INTO api_tokens(id,name,token_prefix,token_hash,token,role) VALUES(83001,'Legacy fixture','sv_fixture','fixture-hash','synthetic-legacy-fixture','readonly')`)
	before := migrationSnapshot(t)
	for attempt := 0; attempt < 2; attempt++ {
		err := initializeSchema()
		if err == nil || !strings.Contains(err.Error(), "local_allowlist_generations.url is missing") {
			t.Fatalf("expected late local-generation refusal, got %v", err)
		}
		if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
			t.Fatal("late initialization failure changed the complete schema/data snapshot")
		}
	}
}

func TestMigrationIndependentInitializationDeferredCommit(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t,
		`DELETE FROM settings WHERE key='cache_ttl'`,
		`DROP TABLE sessions`,
		`DROP TABLE local_allowlist_generations`,
		`INSERT INTO api_tokens(id,name,token_prefix,token_hash,token,role) VALUES(83001,'Legacy fixture','sv_fixture','fixture-hash','synthetic-legacy-fixture','readonly')`,
		`CREATE TABLE independent_parent(id INTEGER PRIMARY KEY)`,
		`CREATE TABLE independent_child(parent_id INTEGER REFERENCES independent_parent(id) DEFERRABLE INITIALLY DEFERRED)`,
		`CREATE TRIGGER independent_commit_fault AFTER UPDATE OF token ON api_tokens BEGIN INSERT INTO independent_child VALUES(83099); END`)
	before := migrationSnapshot(t)
	err := initializeSchema()
	if err == nil || !strings.Contains(err.Error(), "commit schema migration") {
		t.Fatalf("expected SQLite deferred commit refusal, got %v", err)
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatal("deferred initialization commit failure changed the complete schema/data snapshot")
	}
}

func TestMigrationIndependentLegacyRenameRollback(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t,
		`PRAGMA foreign_keys=OFF`,
		`DROP TABLE blocklists`,
		`CREATE TABLE blocklists(id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, legacy_payload BLOB)`,
		`INSERT INTO blocklists(id,pattern,enabled,legacy_payload) VALUES(83001,'https://lists.example.org/privacy.txt',1,x'0001ff007f'),(83002,'https://lists.example.org/privacy.txt',0,x'ff00017f00')`,
		`INSERT INTO blocked_domains(id,blocklist_id,domain) VALUES(83001,83001,'ads.example.org'),(83002,83002,'tracking.example.org')`,
		`CREATE VIEW independent_patterns AS SELECT id,pattern,legacy_payload FROM blocklists`,
		`PRAGMA foreign_keys=ON`)
	before := migrationSnapshot(t)
	err := initializeSchema()
	if err == nil || !strings.Contains(err.Error(), "blocklists.alias") {
		t.Fatalf("expected derived-alias conflict refusal after legacy rename, got %v", err)
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatal("legacy rename refusal changed the complete schema/data/view snapshot")
	}
}
