package svart

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

func TestDuckDBImport(t *testing.T) {
	conn, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("failed to open DuckDB: %v", err)
	}
	defer func() { checkTestClose(t, conn) }()
	var result int
	if err := conn.QueryRow("SELECT 42").Scan(&result); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

// testDBPath extracts the file path of the main database from the global db connection.
func testDBPath(t *testing.T) string {
	t.Helper()
	var seq int
	var name, file string
	row := db.QueryRow("PRAGMA database_list")
	if err := row.Scan(&seq, &name, &file); err != nil {
		t.Fatalf("failed to get DB path: %v", err)
	}
	return file
}

func TestInvestigateQueryBasic(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	// Insert test data into SQLite
	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('10.0.0.1', 'example.com', 'A', 'NOERROR', 0, 1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	cols, rows, duration, err := investigateQuery("SELECT client_ip, query_name FROM query_logs;", 5)
	if err != nil {
		t.Fatalf("investigate query failed: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 columns, got %d", len(cols))
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
	if duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestInvestigateQueryMultipleRows(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	// Insert multiple rows
	entries := []struct {
		ip     string
		domain string
	}{
		{"10.42.1.42", "google.com"},
		{"10.42.1.42", "ads.google.com"},
		{"10.42.1.43", "facebook.com"},
		{"10.42.1.100", "apt.ubuntu.com"},
	}
	for _, e := range entries {
		if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES (?, ?, 'A', 'NOERROR', 0, 1)", e.ip, e.domain); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}

	cols, rows, _, err := investigateQuery("SELECT client_ip, COUNT(*) AS cnt FROM query_logs GROUP BY client_ip ORDER BY cnt DESC", 5)
	if err != nil {
		t.Fatalf("investigate query failed: %v", err)
	}
	if len(cols) != 2 {
		t.Errorf("expected 2 columns, got %d", len(cols))
	}
	// 10.42.1.42 has 2 queries, 10.42.1.43 and 10.42.1.100 have 1 each
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
}

func TestInvestigateSchema(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	tables, err := investigateSchema(t.Context())
	if err != nil {
		t.Fatalf("investigateSchema failed: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("expected 1 table entry, got %d", len(tables))
	}
	if tables[0].Name != "query_logs" {
		t.Errorf("expected table name 'query_logs', got %v", tables[0].Name)
	}

	columns := tables[0].Columns
	// query_logs has many columns — just verify a few key ones exist
	colNames := make(map[string]bool)
	for _, c := range columns {
		colNames[c.Name] = true
	}
	for _, expected := range []string{"id", "client_ip", "query_name", "query_type", "blocked", "timestamp"} {
		if !colNames[expected] {
			t.Errorf("expected column %q not found in schema", expected)
		}
	}
}

func TestInvestigateQueryTimeout(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('10.0.0.9', 'timeout.test', 'A', 'NOERROR', 0, 1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// A simple query with a very short timeout should still succeed (it's fast)
	_, _, _, err := investigateQuery("SELECT COUNT(*) FROM query_logs", 1)
	if err != nil {
		t.Errorf("simple query with 1s timeout should succeed: %v", err)
	}
}

func TestInvestigateQueryAllowsCTEsOverQueryLogs(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('10.0.0.5', 'allowed.test', 'A', 'NOERROR', 0, 1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	sql := `
WITH client_counts AS (
	SELECT client_ip, COUNT(*) AS cnt
	FROM query_logs
	GROUP BY client_ip
)
SELECT client_ip, cnt
FROM client_counts
WHERE cnt > 0
`
	cols, rows, _, err := investigateQuery(sql, 5)
	if err != nil {
		t.Fatalf("investigate query failed: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
}

func TestInvestigateQueryRejectsUnsafeSQL(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	tests := []struct {
		name string
		sql  string
		want string
	}{
		{name: "non-select", sql: "PRAGMA show_tables", want: "only supports SELECT"},
		{name: "disallowed table", sql: "SELECT * FROM admin_users", want: "only query_logs"},
		{name: "schema qualified source", sql: "SELECT client_ip FROM svart.query_logs", want: "schema-qualified"},
		{name: "table function", sql: "SELECT * FROM read_parquet('/tmp/test.parquet')", want: "filesystem access"},
		{name: "file scalar function", sql: "SELECT read_text('/etc/passwd') FROM query_logs LIMIT 1", want: "filesystem access"},
		{name: "no query_logs source", sql: "SELECT 1", want: "must read from query_logs"},
		{name: "multiple statements", sql: "SELECT client_ip FROM query_logs; SELECT 1", want: "single SELECT statement"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := investigateQuery(tc.sql, 5)
			if err == nil {
				t.Fatal("expected query to be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error to contain %q, got %v", tc.want, err)
			}
		})
	}
}

func TestInvestigateQueryNotInitialized(t *testing.T) {
	// Don't init DuckDB — should return error
	oldDuck := duckDB
	duckDB = nil
	defer func() { duckDB = oldDuck }()

	_, _, _, err := investigateQuery("SELECT COUNT(*) FROM query_logs", 5)
	if err == nil {
		t.Error("expected error when DuckDB not initialized")
	}
}

func TestHandleAPIInvestigate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('10.0.0.1', 'example.com', 'A', 'NOERROR', 0, 1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	body := `{"sql": "SELECT client_ip FROM query_logs", "timeout": 5}`
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("response data is not a map")
	}
	if requireFixtureType[float64](t, data["row_count"]) != 1 {
		t.Errorf("expected row_count 1, got %v", data["row_count"])
	}
}

func TestHandleAPIInvestigateEmptySQL(t *testing.T) {
	body := `{"sql": "", "timeout": 5}`
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 400 {
		t.Errorf("expected 400 for empty SQL, got %d", w.Code)
	}
}

func TestHandleAPIInvestigateRejectsUnsafeQuery(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	body := `{"sql": "SELECT * FROM admin_users", "timeout": 5}`
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIInvestigateMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/investigate", nil)
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 405 {
		t.Errorf("expected 405 for GET, got %d", w.Code)
	}
}

func TestHandleAPIInvestigateSchema(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	req := httptest.NewRequest("GET", "/api/investigate/schema", nil)
	w := httptest.NewRecorder()
	handleAPIInvestigateSchema(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp apiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("response data is not a map")
	}
	tables, ok := data["tables"].([]interface{})
	if !ok || len(tables) == 0 {
		t.Fatal("expected non-empty tables array")
	}
}

func TestHandleAPIInvestigateSchemaMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/investigate/schema", nil)
	w := httptest.NewRecorder()
	handleAPIInvestigateSchema(w, req)

	if w.Code != 405 {
		t.Errorf("expected 405 for POST, got %d", w.Code)
	}
}

func TestHandleAPIInvestigateConcurrency(t *testing.T) {
	// When investigateBusy is already set, should return 429
	investigateBusy.Store(true)
	defer investigateBusy.Store(false)

	body := `{"sql": "SELECT COUNT(*) FROM query_logs", "timeout": 5}`
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 429 {
		t.Errorf("expected 429 when busy, got %d: %s", w.Code, w.Body.String())
	}
}

// newInvestigateParser returns a bare DuckDB engine for validator tests:
// validation only needs DuckDB's parser, not the app database.
func newInvestigateParser(t *testing.T) *sql.DB {
	t.Helper()
	parser, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open DuckDB: %v", err)
	}
	t.Cleanup(func() { checkTestClose(t, parser) })
	return parser
}

// TestValidateInvestigateSQLRejectsFourWaySelfJoin is the DoS scenario this
// hardening closes: `SELECT count(*) FROM query_logs a, query_logs b,
// query_logs c` (3 references) is left to the timeout backstop, but a
// 4-way (or greater) self-join is rejected outright by the validator since
// it's an even larger cartesian-product blast radius for no legitimate
// analytical benefit.
func TestValidateInvestigateSQLRejectsFourWaySelfJoin(t *testing.T) {
	err := validateInvestigateSQL(context.Background(), newInvestigateParser(t), "SELECT count(*) FROM query_logs a, query_logs b, query_logs c, query_logs d")
	if err == nil {
		t.Fatal("expected 4-way self-join over query_logs to be rejected")
	}
	if !strings.Contains(err.Error(), "referenced more than") {
		t.Fatalf("expected error about excessive query_logs references, got %v", err)
	}
}

// TestValidateInvestigateSQLAllowsThreeWaySelfJoin confirms the cap sits at
// >3 (not >=3): a 3-way self-join is not rejected by the validator — it's
// still bounded by the DuckDB engine timeout / hard-deadline backstop in
// investigateQuery, not by the reference-count cap.
func TestValidateInvestigateSQLAllowsThreeWaySelfJoin(t *testing.T) {
	err := validateInvestigateSQL(context.Background(), newInvestigateParser(t), "SELECT count(*) FROM query_logs a, query_logs b, query_logs c")
	if err != nil {
		t.Fatalf("expected 3-way self-join to pass validation, got %v", err)
	}
}

// TestValidateInvestigateSQLCountsJoinSyntaxToo ensures the reference cap
// isn't bypassed by using explicit JOIN syntax instead of comma joins.
func TestValidateInvestigateSQLCountsJoinSyntaxToo(t *testing.T) {
	err := validateInvestigateSQL(context.Background(), newInvestigateParser(t), `SELECT count(*) FROM query_logs a
		JOIN query_logs b ON true
		JOIN query_logs c ON true
		JOIN query_logs d ON true`)
	if err == nil {
		t.Fatal("expected 4-way JOIN self-join over query_logs to be rejected")
	}
	if !strings.Contains(err.Error(), "referenced more than") {
		t.Fatalf("expected error about excessive query_logs references, got %v", err)
	}
}

// TestInvestigateQueryBoundedByContextEvenWithZeroTimeout verifies the
// end-to-end investigateQuery path returns quickly (rather than hanging)
// when its context is already expired, and that investigateMu is released
// afterward — a follow-up query must still succeed immediately.
func TestInvestigateQueryBoundedByContextEvenWithZeroTimeout(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	if _, err := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('10.0.0.9', 'bounded.test', 'A', 'NOERROR', 0, 1)"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	start := time.Now()
	_, _, _, err := investigateQuery("SELECT COUNT(*) FROM query_logs", 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected an already-expired context to produce an error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("expected investigateQuery to return well before the hard deadline, took %v", elapsed)
	}

	// investigateMu must not be stuck — a subsequent normal query should
	// succeed immediately.
	_, _, _, err = investigateQuery("SELECT COUNT(*) FROM query_logs", 5)
	if err != nil {
		t.Fatalf("expected follow-up query to succeed after mutex release, got %v", err)
	}
}

func TestHandleAPIInvestigateTimeoutClamp(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	dbPath := testDBPath(t)
	tmpArchive := t.TempDir()
	if err := initDuckDB(dbPath, tmpArchive); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	defer closeDuckDB()

	// Timeout > 60 should be clamped to 60 (but query should still succeed)
	body := `{"sql": "SELECT COUNT(*) FROM query_logs", "timeout": 120}`
	req := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)

	if w.Code != 200 {
		t.Errorf("expected 200 with clamped timeout, got %d: %s", w.Code, w.Body.String())
	}
}
