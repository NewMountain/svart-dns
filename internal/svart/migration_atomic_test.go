package svart

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mattn/go-sqlite3"
)

// Snapshot every stored column, table, index and trigger, including SQLite's
// sequence state. No projection hides a damaged relationship or extra column.
func migrationSnapshot(t *testing.T) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name,sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot, tables []string
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatal(err)
		}
		snapshot = append(snapshot, ddl)
		if strings.HasPrefix(ddl, "CREATE TABLE") {
			tables = append(tables, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range tables {
		rows, err := db.Query(`SELECT * FROM "` + strings.ReplaceAll(table, `"`, `""`) + `" ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values := make([]sql.RawBytes, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			line := table
			for _, v := range values {
				if v == nil {
					line += "|NULL"
				} else {
					line += fmt.Sprintf("|%q", string(v))
				}
			}
			snapshot = append(snapshot, line)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return snapshot
}
func migrationExec(t *testing.T, statements ...string) {
	t.Helper()
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationFaultsAreAtomic(t *testing.T) {
	for _, fault := range []string{"ABORT", "IGNORE", "COMMIT"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			migrationExec(t, `INSERT INTO blocklists(id,url,alias) VALUES(91010,'','Privacy')`, `INSERT INTO client_blocklists(client_ip,blocklist_id,updated_at) VALUES('192.0.2.20',91010,'')`, `ALTER TABLE query_logs DROP COLUMN block_rule`)
			if fault == "COMMIT" {
				migrationExec(t, `CREATE TABLE migration_parent(id INTEGER PRIMARY KEY)`, `CREATE TABLE migration_child(parent_id INTEGER REFERENCES migration_parent(id) DEFERRABLE INITIALLY DEFERRED)`, `CREATE TRIGGER migration_fault AFTER UPDATE ON client_blocklists BEGIN INSERT INTO migration_child VALUES(999); END`)
			} else {
				migrationExec(t, `CREATE TRIGGER migration_fault BEFORE UPDATE ON client_blocklists BEGIN SELECT RAISE(`+fault+func() string {
					if fault == "ABORT" {
						return ", 'migration fixture failure'"
					}
					return ""
				}()+`); END`)
			}
			before := migrationSnapshot(t)
			err := migrateSchema()
			if err == nil {
				t.Error("migration reported success despite SQLite " + fault)
			}
			if fault == "COMMIT" && (err == nil || !strings.Contains(err.Error(), "commit schema migration")) {
				t.Fatalf("expected actual deferred commit failure, got %v", err)
			}
			if fault == "IGNORE" && (err == nil || !strings.Contains(err.Error(), "updated 0 of 1")) {
				t.Fatalf("expected verified silent-ignore detection, got %v", err)
			}
			if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
				t.Errorf("SQLite %s migration changed complete snapshot\nbefore=%q\nafter=%q", fault, before, after)
			}
		})
	}
}

func TestMigrationOrphanIsPreserved(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t, `PRAGMA foreign_keys=OFF`, `INSERT INTO client_group_members(client_ip,group_id) VALUES('192.0.2.50',91099)`, `PRAGMA foreign_keys=ON`)
	before := migrationSnapshot(t)
	if err := migrateSchema(); err == nil {
		t.Error("migration accepted an orphan without explicit reconciliation")
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Errorf("orphan migration changed complete snapshot\nbefore=%q\nafter=%q", before, after)
	}
}

func TestMigrationAllNaturalKeysPreserveCompleteState(t *testing.T) {
	cases := []struct{ table, index, insert string }{
		{"bootstrap_servers", "idx_bootstrap_servers_unique", `INSERT INTO bootstrap_servers(id,server,node_id) VALUES(91001,'192.0.2.53:53','east'),(91002,'192.0.2.53:53','west')`},
		{"upstreams", "idx_upstreams_unique", `INSERT INTO upstreams(id,upstream,enabled) VALUES(91001,'192.0.2.53:53',1),(91002,'192.0.2.53:53',0)`},
		{"blocklists", "idx_blocklists_alias_unique", `INSERT INTO blocklists(id,url,alias,enabled) VALUES(91001,'https://lists.example.org/a','Privacy',1),(91002,'https://lists.example.org/b','Privacy',0)`},
		{"allowlists", "idx_allowlists_alias_unique", `INSERT INTO allowlists(id,url,alias,enabled) VALUES(91001,'https://lists.example.org/a','Trusted',1),(91002,'https://lists.example.org/b','Trusted',0)`},
		{"rewrites", "idx_rewrites_domain_unique", `INSERT INTO rewrites(id,domain,target,ip_addresses) VALUES(91001,'git.example.org','', '192.0.2.1'),(91002,'git.example.org','','192.0.2.2')`},
		{"api_tokens", "idx_api_tokens_name_unique", `INSERT INTO api_tokens(id,name,token_prefix,token_hash,role) VALUES(91001,'Monitoring','sv_a','fixture-hash-a','readonly'),(91002,'Monitoring','sv_b','fixture-hash-b','admin')`},
	}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			defer setupTestDB(t)()
			migrationExec(t, "DROP INDEX "+tc.index, tc.insert)
			if tc.table == "blocklists" || tc.table == "allowlists" {
				kind, domainTable := "blocklist", "blocked_domains"
				if tc.table == "allowlists" {
					kind, domainTable = "allowlist", "allowed_domains"
				}
				migrationExec(t, `INSERT INTO client_groups(id,name) VALUES(91001,'Office')`, `INSERT INTO ip_ranges(id,name,cidr) VALUES(91001,'Documentation network','192.0.2.0/24')`, `INSERT INTO policies(id,name) VALUES(91001,'Workstations')`,
					fmt.Sprintf("INSERT INTO %s(%s_id,domain) VALUES(91001,'ads.example.org'),(91002,'tracking.example.org')", domainTable, kind),
					fmt.Sprintf("INSERT INTO client_%ss(client_ip,%s_id) VALUES('192.0.2.10',91001),('192.0.2.20',91002)", kind, kind),
					fmt.Sprintf("INSERT INTO group_%ss(group_id,%s_id) VALUES(91001,91001),(91001,91002)", kind, kind),
					fmt.Sprintf("INSERT INTO range_%ss(range_id,%s_id) VALUES(91001,91001),(91001,91002)", kind, kind),
					fmt.Sprintf("INSERT INTO policy_%ss(policy_id,%s_id) VALUES(91001,91001),(91001,91002)", kind, kind))
				if tc.table == "blocklists" {
					migrationExec(t, `INSERT INTO blocklist_history(id,blocklist_id,sample_added) VALUES(91001,91001,'ads.example.org'),(91002,91002,'tracking.example.org')`, `INSERT INTO blocklist_changelog(history_id,domain,action) VALUES(91001,'ads.example.org','added'),(91002,'tracking.example.org','added')`)
				}
			}
			before := migrationSnapshot(t)
			for attempt := 0; attempt < 2; attempt++ {
				err := migrateSchema()
				if err == nil || !strings.Contains(err.Error(), tc.table+".") || !strings.Contains(err.Error(), "reconcile") {
					t.Fatalf("want actionable %s conflict, got %v", tc.table, err)
				}
				if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
					t.Fatalf("%s complete state changed\nbefore=%q\nafter=%q", tc.table, before, after)
				}
			}
		})
	}
}

func TestMigrationLegacyPatternPreservesRelationships(t *testing.T) {
	defer setupTestDB(t)()
	// Recreate the original pattern schema, retaining dependent table definitions.
	migrationExec(t, `PRAGMA foreign_keys=OFF`, `DROP TABLE blocklists`, `CREATE TABLE blocklists(id INTEGER PRIMARY KEY AUTOINCREMENT, pattern TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT 1, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, legacy_note TEXT)`, `INSERT INTO blocklists(id,pattern,enabled,created_at,legacy_note) VALUES(123,'https://lists.example.org/privacy.txt',0,'2021-01-02 03:04:05','retain this old extension')`, `INSERT INTO blocked_domains(id,blocklist_id,domain) VALUES(456,123,'ads.example.org')`, `INSERT INTO client_blocklists(client_ip,blocklist_id,updated_at,node_id) VALUES('192.0.2.5',123,'2021-01-02T03:04:05Z','legacy')`, `PRAGMA foreign_keys=ON`)
	if err := migrateSchema(); err != nil {
		t.Fatal(err)
	}
	var actual string
	if err := db.QueryRow(`SELECT json_array(b.id,b.url,b.alias,b.enabled,b.created_at,b.legacy_note,d.id,d.domain,c.client_ip,c.updated_at,c.node_id) FROM blocklists b JOIN blocked_domains d ON d.blocklist_id=b.id JOIN client_blocklists c ON c.blocklist_id=b.id`).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	expected := `[123,"https://lists.example.org/privacy.txt","https://lists.example.org/privacy.txt",0,"2021-01-02 03:04:05","retain this old extension",456,"ads.example.org","192.0.2.5","2021-01-02T03:04:05Z","legacy"]`
	if actual != expected {
		t.Fatalf("legacy conversion=%s want %s", actual, expected)
	}
	migrationExec(t, `INSERT INTO blocklists(url,alias) VALUES('https://lists.example.org/new.txt','New after upgrade')`)
	before := migrationSnapshot(t)
	for attempt := 0; attempt < 2; attempt++ {
		if err := migrateSchema(); err != nil {
			t.Fatal(err)
		}
		if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
			t.Fatalf("repeat migration changed complete state\nbefore=%q\nafter=%q", before, after)
		}
	}
}

func TestMigrationInitializationFailureIsAtomic(t *testing.T) {
	for _, fault := range []string{"ABORT", "IGNORE", "DDL"} {
		t.Run(fault, func(t *testing.T) {
			defer setupTestDB(t)()
			migrationExec(t, `DELETE FROM settings WHERE key='cache_ttl'`, `DROP TABLE sessions`)
			if fault == "DDL" {
				migrationExec(t, `DROP TABLE sync_tombstones`, `CREATE VIEW sync_tombstones AS SELECT 1 AS unexpected`)
			} else {
				suffix := ""
				if fault == "ABORT" {
					suffix = ", 'fixture-private-message'"
				}
				migrationExec(t, `CREATE TRIGGER migration_fault BEFORE INSERT ON settings WHEN NEW.key='cache_ttl' BEGIN SELECT RAISE(`+fault+suffix+`); END`)
			}
			before := migrationSnapshot(t)
			err := initializeSchema()
			if err == nil {
				t.Fatal("initialization falsely succeeded")
			}
			if strings.Contains(err.Error(), "fixture-private-message") {
				t.Fatal("SQLite trigger text escaped diagnostics")
			}
			if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
				t.Fatalf("initialization changed complete state\nbefore=%q\nafter=%q", before, after)
			}
		})
	}
}

func TestMigrationPartialColumnsAndSettingsRemainIdempotent(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t, `ALTER TABLE query_logs DROP COLUMN block_rule`, `ALTER TABLE query_logs DROP COLUMN result_rule`, `ALTER TABLE client_groups DROP COLUMN policy_id`, `ALTER TABLE allowlists DROP COLUMN refresh_interval`, `UPDATE settings SET value='7' WHERE key='log_retention_days'`, `UPDATE settings SET value='false' WHERE key='logging_enabled'`, `INSERT INTO blocklists(id,url,alias,refresh_interval) VALUES(92001,'https://lists.example.org/privacy.txt','Privacy disabled refresh',0)`)
	if err := initializeSchema(); err != nil {
		t.Fatal(err)
	}
	before := migrationSnapshot(t)
	for attempt := 0; attempt < 3; attempt++ {
		if err := initializeSchema(); err != nil {
			t.Fatal(err)
		}
		if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
			t.Fatalf("restart changed complete state\nbefore=%q\nafter=%q", before, after)
		}
	}
	var retention, logging string
	var refresh int
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='log_retention_days'`).Scan(&retention); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT value FROM settings WHERE key='logging_enabled'`).Scan(&logging); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT refresh_interval FROM blocklists WHERE id=92001`).Scan(&refresh); err != nil {
		t.Fatal(err)
	}
	if retention != "7" || logging != "false" || refresh != 0 {
		t.Fatalf("retention=%s logging=%s refresh=%d", retention, logging, refresh)
	}
}

func TestMigrationUnreadableIntrospectionIsNotAbsentColumn(t *testing.T) {
	defer setupTestDB(t)()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	exists, err := migrationColumnExists(tx, "blocklists", "url")
	if err == nil || exists {
		t.Fatalf("closed transaction introspection: exists=%v error=%v", exists, err)
	}
}

func TestMigrationSQLiteIntrospectionFailurePreservesState(t *testing.T) {
	defer setupTestDB(t)()
	before := migrationSnapshot(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Raw(func(raw any) error {
		requireFixtureType[*sqlite3.SQLiteConn](t, raw).RegisterAuthorizer(func(op int, table, _, _ string) int {
			if op == sqlite3.SQLITE_READ && table == "sqlite_master" {
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	migrationErr := migrateSchema()
	conn, err = db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Raw(func(raw any) error {
		requireFixtureType[*sqlite3.SQLiteConn](t, raw).RegisterAuthorizer(nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if migrationErr == nil || !strings.Contains(migrationErr.Error(), "inspect table blocklists") {
		t.Fatalf("want checked SQLite introspection error, got %v", migrationErr)
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatalf("unreadable schema mutated state\nbefore=%q\nafter=%q", before, after)
	}
}

func TestMigrationRejectsMisnamedNaturalKeyIndex(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t, `DROP INDEX idx_blocklists_alias_unique`, `CREATE INDEX idx_blocklists_alias_unique ON blocklists(url)`)
	before := migrationSnapshot(t)
	err := migrateSchema()
	if err == nil || !strings.Contains(err.Error(), "does not uniquely enforce blocklists.alias") {
		t.Fatalf("invalid natural-key index accepted: %v", err)
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatalf("invalid index changed complete state\nbefore=%q\nafter=%q", before, after)
	}
}

func TestMigrationBackfillPreservesExistingNodeIdentity(t *testing.T) {
	defer setupTestDB(t)()
	migrationExec(t, `INSERT INTO blocklists(id,url,alias) VALUES(92301,'','Privacy')`, `INSERT INTO client_blocklists(client_ip,blocklist_id,updated_at,node_id) VALUES('192.0.2.25',92301,'','original-node')`, `INSERT INTO client_groups(id,name,node_id) VALUES(92301,'Office','parent-node')`, `INSERT INTO group_blocklists(group_id,blocklist_id,updated_at,node_id) VALUES(92301,92301,'','relationship-node')`)
	if err := migrateSchema(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ table, node string }{{"client_blocklists", "original-node"}, {"group_blocklists", "relationship-node"}} {
		var updated, node string
		if err := db.QueryRow("SELECT updated_at,node_id FROM "+tc.table).Scan(&updated, &node); err != nil {
			t.Fatal(err)
		}
		if updated == "" || node != tc.node {
			t.Fatalf("%s metadata timestamp=%q node=%q", tc.table, updated, node)
		}
	}
	before := migrationSnapshot(t)
	if err := migrateSchema(); err != nil {
		t.Fatal(err)
	}
	if after := migrationSnapshot(t); !reflect.DeepEqual(before, after) {
		t.Fatalf("backfill repeated changed state\nbefore=%q\nafter=%q", before, after)
	}
}
