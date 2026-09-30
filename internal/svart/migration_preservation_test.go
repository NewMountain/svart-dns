package svart

import (
	"reflect"
	"testing"
)

func TestMigrationPreservesConflictingNaturalKeyRows(t *testing.T) {
	defer setupTestDB(t)()
	for _, statement := range []string{
		`DROP INDEX idx_blocklists_alias_unique`,
		`INSERT INTO blocklists(id,alias,url,enabled,domain_count,refresh_interval) VALUES(91001,'Legacy privacy rules','https://lists.example.org/first.txt',1,1,604800),(91002,'Legacy privacy rules','https://lists.example.org/second.txt',0,1,86400)`,
		`INSERT INTO blocked_domains(blocklist_id,domain) VALUES(91001,'ads.example.org'),(91002,'tracking.example.net')`,
		`INSERT INTO client_blocklists(client_ip,blocklist_id,updated_at) VALUES('192.0.2.10',91001,'2026-09-01T00:00:00Z'),('192.0.2.11',91002,'2026-09-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	before := migrationConflictRows(t)
	err := migrateSchema()
	if after := migrationConflictRows(t); !reflect.DeepEqual(after, before) {
		t.Fatalf("migration discarded distinct legacy rules or assignments (migration error=%v): before=%v after=%v", err, before, after)
	}
	if err == nil {
		t.Fatal("conflicting aliases require explicit reconciliation, not silently successful migration")
	}
}

func migrationConflictRows(t *testing.T) []string {
	t.Helper()
	rows, err := db.Query(`SELECT json_array(b.id,b.alias,b.url,b.enabled,b.domain_count,b.refresh_interval,d.domain,c.client_ip) FROM blocklists b LEFT JOIN blocked_domains d ON d.blocklist_id=b.id LEFT JOIN client_blocklists c ON c.blocklist_id=b.id WHERE b.id IN(91001,91002) ORDER BY b.id,d.domain,c.client_ip`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, rows) }()
	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func TestMigrationPreservesExplicitDisabledRefreshOnRestart(t *testing.T) {
	defer setupTestDB(t)()
	if _, err := db.Exec(`INSERT INTO blocklists(id,alias,url,enabled,refresh_interval) VALUES(91003,'Operator scheduled privacy rules','https://lists.example.org/privacy.txt',1,0)`); err != nil {
		t.Fatal(err)
	}
	if err := migrateSchema(); err != nil {
		t.Fatal(err)
	}
	var interval int
	if err := db.QueryRow(`SELECT refresh_interval FROM blocklists WHERE id=91003`).Scan(&interval); err != nil {
		t.Fatal(err)
	}
	if interval != 0 {
		t.Fatalf("restart changed explicitly disabled automatic refresh: got %d, want 0", interval)
	}
}
