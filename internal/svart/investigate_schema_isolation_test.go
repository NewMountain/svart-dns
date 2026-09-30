package svart

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

// The DNS process owns a live SQLite WAL connection. Schema discovery must
// still work when its local DuckDB connection is unusable: the independently
// embedded SQLite reader belongs in the disposable worker, never this process.
func TestInvestigateSchemaIsolatedFromParentNativeEngine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.db")
	source, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, source) }()
	if _, err := source.Exec(`CREATE TABLE query_logs(timestamp DATETIME, query_name TEXT, coalesced_count INTEGER); INSERT INTO query_logs VALUES('2026-09-20 08:15:00','www.google.com',7)`); err != nil {
		t.Fatal(err)
	}
	oldDB, oldDuck, oldPath, oldArchive := db, duckDB, duckDBPath, duckDBArchive
	defer func() { db, duckDB, duckDBPath, duckDBArchive = oldDB, oldDuck, oldPath, oldArchive }()
	db = source
	archive := t.TempDir()
	if err := initDuckDB(path, archive); err != nil {
		t.Fatal(err)
	}
	closeDuckDB()
	tables, err := investigateSchema(t.Context())
	if err != nil {
		t.Fatalf("schema must execute outside the parent native engine: %v", err)
	}
	want := []InvestigationSchemaTable{{Name: "query_logs", Type: "view", Columns: []InvestigationColumn{{Name: "timestamp", Type: "TIMESTAMP"}, {Name: "query_name", Type: "VARCHAR"}, {Name: "coalesced_count", Type: "BIGINT"}}}}
	if !reflect.DeepEqual(tables, want) {
		t.Fatalf("schema=%#v want=%#v", tables, want)
	}
	// Add a current archive with an archive-only field, then a legacy archive
	// lacking the count/field. Exact metadata and every row must survive union.
	type current struct {
		Timestamp      int64  `parquet:"timestamp"`
		QueryName      string `parquet:"query_name"`
		Count          int64  `parquet:"coalesced_count"`
		Classification string `parquet:"classification"`
	}
	if err := parquet.WriteFile(filepath.Join(archive, "current.parquet"), []current{{time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), "archive.ubuntu.com", 3, "allowed"}}); err != nil {
		t.Fatal(err)
	}
	want[0].Columns = append(want[0].Columns, InvestigationColumn{Name: "classification", Type: "VARCHAR"})
	for _, stage := range []string{"current", "legacy"} {
		if stage == "legacy" {
			type legacy struct {
				Timestamp int64  `parquet:"timestamp"`
				QueryName string `parquet:"query_name"`
			}
			if err := parquet.WriteFile(filepath.Join(archive, "legacy.parquet"), []legacy{{time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC).UnixMilli(), "firmware.smarttv.example"}}); err != nil {
				t.Fatal(err)
			}
		}
		tables, err = investigateSchema(t.Context())
		if err != nil || !reflect.DeepEqual(tables, want) {
			t.Fatalf("%s schema=%#v want=%#v error=%v", stage, tables, want, err)
		}
	}
	_, rows, _, err := investigateQuery("SELECT query_name,coalesce(coalesced_count,1),classification FROM query_logs ORDER BY timestamp", 5)
	if err != nil || fmt.Sprint(rows) != "[[firmware.smarttv.example 1 <nil>] [archive.ubuntu.com 3 allowed] [www.google.com 7 <nil>]]" {
		t.Fatalf("unified rows=%v error=%v", rows, err)
	}
	var count, total int
	if err := source.QueryRow("SELECT count(*),sum(coalesced_count) FROM query_logs").Scan(&count, &total); err != nil {
		t.Fatal(err)
	}
	if count != 1 || total != 7 {
		t.Fatalf("live SQLite count=%d total=%d want1,7", count, total)
	}
}
