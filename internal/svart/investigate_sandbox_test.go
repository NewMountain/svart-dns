package svart

import (
	"context"
	"encoding/json"
	"fmt"
	assets "github.com/yeti/svart-dns"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

// Markers planted in the tables the investigation sandbox must never expose.
// Any response body containing one of them is a confidentiality breach.
const (
	sandboxAdminHashMarker = "$2a$10$SANDBOXLEAKadminhashXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	sandboxSessionSecretMarker = "SANDBOXLEAK-session-secret-9f1c2b7e4d"
	sandboxSyncSecretMarker    = "SANDBOXLEAK-sync-secret-0a8e6d3c5b"
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	sandboxTokenHashMarker = "$2a$10$SANDBOXLEAKtokenhashXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
)

var sandboxSecretMarkers = []string{
	sandboxAdminHashMarker,
	sandboxSessionSecretMarker,
	sandboxSyncSecretMarker,
	sandboxTokenHashMarker,
	"SANDBOXLEAK",
}

// setupInvestigateSandbox builds a fresh app DB holding both realistic DNS
// traffic and planted secrets, and an investigation engine over it.
func setupInvestigateSandbox(t *testing.T) string {
	t.Helper()
	cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	archiveDir := t.TempDir()
	if err := initDuckDB(testDBPath(t), archiveDir); err != nil {
		t.Fatalf("initDuckDB failed: %v", err)
	}
	t.Cleanup(closeDuckDB)

	mustExec := func(query string, args ...interface{}) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
	}
	mustExec("INSERT INTO admin_users (username, password_hash, role, created_at, updated_at) VALUES ('chris', ?, 'admin', '2026-09-01 12:00:00', '2026-09-01 12:00:00')", sandboxAdminHashMarker)
	mustExec("INSERT OR REPLACE INTO settings (key, value) VALUES ('session_secret', ?)", sandboxSessionSecretMarker)
	mustExec("INSERT OR REPLACE INTO settings (key, value) VALUES ('sync_secret', ?)", sandboxSyncSecretMarker)
	mustExec("INSERT INTO api_tokens (name, token_prefix, token_hash, role) VALUES ('grafana', 'svt_7f3a', ?, 'readonly')", sandboxTokenHashMarker)

	traffic := []struct {
		ts, ip, name, qtype, rcode string
		blocked, coalesced         int
	}{
		{"2026-09-20 08:15:00", "10.42.1.42", "www.google.com", "A", "NOERROR", 0, 1},
		{"2026-09-20 08:16:30", "10.42.1.42", "ads.doubleclick.net", "A", "NXDOMAIN", 1, 3},
		{"2026-09-21 09:00:00", "10.42.1.42", "www.google.com", "AAAA", "NOERROR", 0, 1},
		{"2026-09-21 10:30:00", "10.42.1.43", "graph.facebook.com", "A", "NXDOMAIN", 1, 5},
		{"2026-09-22 11:00:00", "10.42.1.100", "archive.ubuntu.com", "A", "NOERROR", 0, 2},
	}
	for _, q := range traffic {
		mustExec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES (?, ?, ?, ?, ?, ?, ?)",
			q.ts, q.ip, q.name, q.qtype, q.rcode, q.blocked, q.coalesced)
	}
	return archiveDir
}

func postInvestigate(t *testing.T, sqlText string) (int, apiResponse, string) {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{"sql": sqlText, "timeout": 5})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/investigate", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handleAPIInvestigate(w, req)
	raw := w.Body.String()
	var resp apiResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode response %q: %v", raw, err)
	}
	return w.Code, resp, raw
}

func assertNoSandboxSecrets(t *testing.T, raw string) {
	t.Helper()
	for _, marker := range sandboxSecretMarkers {
		if strings.Contains(raw, marker) {
			t.Fatalf("response leaked planted secret %q: %s", marker, raw)
		}
	}
}

// TestInvestigateSandboxRejectsEscapes is the S1 regression suite. Each case
// is a way to reach something other than query_logs: the attached app DB
// (admin hashes, API token hashes, session/sync secrets), the filesystem,
// engine metadata or settings, or dynamic SQL. Every one must be a 400 with
// a specific reason, and no response may carry a planted secret.
func TestInvestigateSandboxRejectsEscapes(t *testing.T) {
	setupInvestigateSandbox(t)

	tests := []struct {
		name string
		sql  string
		want string
	}{
		// The proven PoCs: FROM-first scalar subqueries skipped the old tokenizer.
		{"from-first subquery reads admin hashes", `SELECT (FROM svart.admin_users SELECT string_agg(username||'='||password_hash)) AS h FROM query_logs LIMIT 1`, "schema-qualified"},
		{"from-first subquery reads settings secrets", `SELECT (FROM svart.settings SELECT string_agg(key||'='||value)) AS s FROM query_logs LIMIT 1`, "schema-qualified"},
		{"from-first subquery reads api tokens", `SELECT (FROM svart.api_tokens SELECT string_agg(token_hash)) AS n FROM query_logs LIMIT 1`, "schema-qualified"},
		{"from-first top level", `FROM svart.admin_users SELECT username, password_hash`, "schema-qualified"},
		{"from-first top level bare", `FROM svart.settings`, "schema-qualified"},
		{"upper-case catalog", `SELECT (FROM SVART.ADMIN_USERS SELECT min(password_hash)) FROM query_logs`, "schema-qualified"},
		{"quoted catalog", `SELECT * FROM "svart"."settings"`, "schema-qualified"},
		{"three-part name", `SELECT * FROM svart.main.admin_users`, "schema-qualified"},
		{"where-clause subquery", `SELECT count(*) FROM query_logs WHERE query_name = (FROM svart.settings SELECT value WHERE key = 'sync_secret')`, "schema-qualified"},
		{"limit subquery", `SELECT client_ip FROM query_logs LIMIT (FROM svart.api_tokens SELECT count(*))`, "schema-qualified"},
		{"aggregate filter subquery", `SELECT count(*) FILTER (WHERE (FROM svart.settings SELECT count(*)) > 0) FROM query_logs`, "schema-qualified"},
		{"set operation branch", `SELECT client_ip FROM query_logs UNION ALL FROM svart.admin_users SELECT password_hash`, "schema-qualified"},
		{"information_schema", `SELECT * FROM information_schema.tables`, "schema-qualified"},
		{"unqualified app table in subquery", `SELECT (FROM admin_users SELECT count(*)) FROM query_logs`, `source "admin_users" is not allowed`},
		{"engine metadata view", `SELECT * FROM duckdb_tables`, `source "duckdb_tables" is not allowed`},
		{"sqlite_master view", `SELECT * FROM sqlite_master`, `source "sqlite_master" is not allowed`},
		{"string literal file ref", `SELECT * FROM '/etc/passwd'`, `source "/etc/passwd" is not allowed`},
		{"string literal parquet ref", `SELECT count(*) FROM 'archives/svart-dns-2026-08-01.parquet'`, `is not allowed`},
		{"cte cannot shadow its own source", `WITH duckdb_tables AS (SELECT * FROM duckdb_tables) SELECT * FROM duckdb_tables JOIN query_logs ON true`, `source "duckdb_tables" is not allowed`},
		{"cte out of scope", `SELECT (WITH t AS (SELECT 1 AS x FROM query_logs) SELECT count(*) FROM t) AS a, (SELECT count(*) FROM t) AS b FROM query_logs`, `source "t" is not allowed`},
		{"engine metadata table function", `SELECT * FROM duckdb_tables()`, "engine metadata"},
		{"engine settings table function", `SELECT name, value FROM duckdb_settings()`, "engine metadata"},
		{"table function beside query_logs", `SELECT * FROM query_logs, read_text('/etc/passwd')`, "filesystem access"},
		{"glob", `SELECT * FROM glob('/var/lib/svart-dns/*')`, "filesystem access"},
		{"sqlite_scan app db", `SELECT * FROM sqlite_scan('svart-dns.db', 'admin_users')`, "external database access"},
		{"dynamic sql", `SELECT * FROM query('SELECT * FROM svart.admin_users')`, "dynamic SQL"},
		{"current_setting", `SELECT current_setting('extension_directory') AS v FROM query_logs LIMIT 1`, "current_setting() is not allowed"},
		{"current_setting inside lambda", `SELECT list_transform([1], x -> current_setting('threads')) FROM query_logs`, "current_setting() is not allowed"},
		{"getvariable", `SELECT getvariable('sync') FROM query_logs`, "getvariable() is not allowed"},
		{"plan serialization binds arbitrary sql", `SELECT json_serialize_plan('SELECT * FROM svart.admin_users') FROM query_logs`, "json_serialize_plan() is not allowed"},
		{"catalog macro via pg_catalog", `SELECT pg_catalog.pg_get_viewdef(1) FROM query_logs`, "pg_catalog.pg_get_viewdef"},
		{"catalog macro unqualified", `SELECT pg_get_viewdef(1) FROM query_logs`, "pg_get_viewdef() is not allowed"},
		{"macro reading pragma", `SELECT get_block_size('memory') FROM query_logs`, "get_block_size() is not allowed"},
		{"pivot statement", `PIVOT query_logs ON query_type USING count(*)`, "only supports SELECT"},
		{"pivot subquery", `SELECT * FROM (PIVOT query_logs ON query_type USING count(*))`, "only supports SELECT"},
		{"unpivot subquery", `SELECT * FROM (UNPIVOT query_logs ON query_type INTO NAME k VALUE v)`, "PIVOT/UNPIVOT"},
		{"lateral comma join", `SELECT * FROM query_logs a, LATERAL (SELECT a.id) b`, "joined subqueries"},
		{"lateral join", `SELECT * FROM query_logs a JOIN LATERAL (SELECT a.client_ip) b ON true`, "joined subqueries"},
		{"implicit lateral", `SELECT * FROM query_logs a, (SELECT a.client_ip AS ip) b`, "joined subqueries"},
		{"recursive cte", `WITH RECURSIVE r(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM r) SELECT * FROM r, query_logs`, "recursive CTEs are not allowed"},
		{"multiple statements", `SELECT client_ip FROM query_logs; SELECT 1`, "single SELECT statement"},
		{"statement smuggled after select", `SELECT 1 FROM query_logs; FROM svart.admin_users`, "single SELECT statement"},
		{"attach", `ATTACH '/tmp/other.db' AS other`, "only supports SELECT"},
		{"copy out", `COPY (SELECT * FROM query_logs) TO '/tmp/exfil.csv'`, "only supports SELECT"},
		{"set", `SET enable_external_access = true`, "only supports SELECT"},
		{"install", `INSTALL httpfs`, "only supports SELECT"},
		{"load", `LOAD httpfs`, "only supports SELECT"},
		{"pragma", `PRAGMA database_list`, "only supports SELECT"},
		{"no query_logs", `SELECT 1`, "must read from query_logs"},
		{"values only", `SELECT * FROM (VALUES ('10.42.1.42')) v(ip)`, "must read from query_logs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, resp, raw := postInvestigate(t, tc.sql)
			assertNoSandboxSecrets(t, raw)
			if code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", code, raw)
			}
			if resp.Error == nil || !strings.Contains(*resp.Error, tc.want) {
				t.Fatalf("expected error containing %q, got %s", tc.want, raw)
			}
		})
	}
}

func investigateRowsAsStrings(rows [][]interface{}) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, v := range row {
			cells[i] = fmt.Sprint(v)
		}
		out = append(out, cells)
	}
	return out
}

// TestInvestigateSandboxAllowsAnalystQueries pins the queries the sandbox
// exists to serve, with exact results over the seeded traffic, so tightening
// the validator cannot silently take them away.
func TestInvestigateSandboxAllowsAnalystQueries(t *testing.T) {
	setupInvestigateSandbox(t)

	tests := []struct {
		name string
		sql  string
		want [][]string
	}{
		{
			name: "joins between CTEs over query_logs",
			sql: `WITH blocked AS (
	SELECT client_ip, SUM(coalesced_count)::BIGINT AS blocked_queries FROM query_logs WHERE blocked = 1 GROUP BY client_ip
), totals AS (
	SELECT client_ip, SUM(coalesced_count)::BIGINT AS total_queries FROM query_logs GROUP BY client_ip
)
SELECT totals.client_ip, total_queries, COALESCE(blocked_queries, 0) AS blocked_queries
FROM totals LEFT JOIN blocked ON blocked.client_ip = totals.client_ip
ORDER BY totals.client_ip`,
			want: [][]string{{"10.42.1.100", "2", "0"}, {"10.42.1.42", "5", "3"}, {"10.42.1.43", "5", "5"}},
		},
		{
			name: "window function",
			sql:  `SELECT client_ip, query_name, row_number() OVER (PARTITION BY client_ip ORDER BY timestamp) AS nth FROM query_logs WHERE client_ip = '10.42.1.42' ORDER BY nth`,
			want: [][]string{{"10.42.1.42", "www.google.com", "1"}, {"10.42.1.42", "ads.doubleclick.net", "2"}, {"10.42.1.42", "www.google.com", "3"}},
		},
		{
			name: "group by with having and aggregates",
			sql:  `SELECT query_type, count(*) AS n, SUM(coalesced_count)::BIGINT AS total FROM query_logs GROUP BY query_type HAVING count(*) > 1 ORDER BY query_type`,
			want: [][]string{{"A", "4", "11"}},
		},
		{
			name: "scalar subquery in select list",
			sql:  `SELECT client_ip, count(*) AS n, (SELECT count(*) FROM query_logs) AS all_rows FROM query_logs GROUP BY client_ip ORDER BY client_ip`,
			want: [][]string{{"10.42.1.100", "1", "5"}, {"10.42.1.42", "3", "5"}, {"10.42.1.43", "1", "5"}},
		},
		{
			name: "date functions",
			sql:  `SELECT strftime(timestamp, '%Y-%m-%d') AS day, date_trunc('hour', timestamp)::VARCHAR AS hour_bucket, count(*) AS n FROM query_logs WHERE client_ip = '10.42.1.42' GROUP BY ALL ORDER BY day`,
			want: [][]string{{"2026-09-20", "2026-09-20 08:00:00", "2"}, {"2026-09-21", "2026-09-21 09:00:00", "1"}},
		},
		{
			name: "regexp and string functions",
			sql:  `SELECT DISTINCT regexp_extract(query_name, '([a-z0-9-]+\.[a-z]+)$', 1) AS registrable, upper(split_part(query_name, '.', 1)) AS first_label, length(query_name) AS len FROM query_logs WHERE regexp_matches(query_name, 'google|facebook') ORDER BY registrable`,
			want: [][]string{{"facebook.com", "GRAPH", "18"}, {"google.com", "WWW", "14"}},
		},
		{
			name: "derived table and IN subquery",
			sql:  `SELECT client_ip, n FROM (SELECT client_ip, count(*) AS n FROM query_logs GROUP BY client_ip) per_client WHERE client_ip IN (SELECT client_ip FROM query_logs WHERE blocked = 1) ORDER BY client_ip`,
			want: [][]string{{"10.42.1.42", "3"}, {"10.42.1.43", "1"}},
		},
		{
			name: "union all",
			sql:  `SELECT 'blocked' AS kind, count(*) AS n FROM query_logs WHERE blocked = 1 UNION ALL SELECT 'allowed', count(*) FROM query_logs WHERE blocked = 0 ORDER BY kind`,
			want: [][]string{{"allowed", "3"}, {"blocked", "2"}},
		},
		{
			name: "from-first over query_logs",
			sql:  `FROM query_logs SELECT count(*) AS n WHERE query_type = 'A'`,
			want: [][]string{{"4"}},
		},
		{
			name: "list aggregate with lambda",
			sql:  `SELECT list_sort(list(DISTINCT query_type)) AS types, list_transform([1, 2], x -> x * 10) AS scaled FROM query_logs`,
			want: [][]string{{"[A AAAA]", "[10 20]"}},
		},
		{
			name: "values cte joined to query_logs",
			sql:  `WITH watch(ip) AS (VALUES ('10.42.1.43')) SELECT q.query_name FROM query_logs q JOIN watch ON q.client_ip = watch.ip`,
			want: [][]string{{"graph.facebook.com"}},
		},
		{
			name: "cte referencing an earlier cte",
			sql:  `WITH blocked AS (SELECT * FROM query_logs WHERE blocked = 1), heavy AS (SELECT client_ip FROM blocked WHERE coalesced_count >= 5) SELECT client_ip FROM heavy`,
			want: [][]string{{"10.42.1.43"}},
		},
		{
			name: "casts with type parameters",
			sql:  `SELECT CAST(SUM(coalesced_count) AS DECIMAL(10,1))::VARCHAR AS total, CAST(list(DISTINCT response_code ORDER BY response_code) AS VARCHAR[]) AS rcodes FROM query_logs`,
			want: [][]string{{"12.0", "[NOERROR NXDOMAIN]"}},
		},
		{
			name: "trailing semicolon",
			sql:  `SELECT count(*) FROM query_logs;`,
			want: [][]string{{"5"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, rows, _, err := investigateQuery(tc.sql, 5)
			if err != nil {
				t.Fatalf("expected query to run, got %v", err)
			}
			got := investigateRowsAsStrings(rows)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("rows mismatch\n got: %v\nwant: %v", got, tc.want)
			}
		})
	}
}

var frontendInvestigationTemplates = assets.InvestigationTemplates

func TestInvestigateSandboxRunsFrontendTemplates(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)
	oldArchivePath := archivePath
	archivePath = archiveDir
	t.Cleanup(func() { archivePath = oldArchivePath })
	if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, coalesced_count) VALUES ('2026-08-01 07:45:00', '10.42.1.42', 'archive.example.com', 'A', 4)"); err != nil {
		t.Fatal(err)
	}
	if n, err := archiveDay(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)); err != nil || n != 1 {
		t.Fatalf("archive = %d, %v; want 1", n, err)
	}
	legacyPath := filepath.Join(archiveDir, "2025-03-14.parquet")
	if err := parquet.WriteFile(legacyPath, []legacyArchiveRow{{Timestamp: time.Date(2025, 3, 14, 22, 5, 0, 0, time.UTC).UnixMilli(), ClientIP: "10.42.1.42", QueryName: "legacy.example.com", QueryType: "A"}}); err != nil {
		t.Fatal(err)
	}
	var templates []struct {
		Label string `json:"label"`
		SQL   string `json:"sql"`
	}
	if err := json.Unmarshal(frontendInvestigationTemplates, &templates); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"Client activity over time":   `[["10.42.1.100","2026-09-22T00:00:00Z",2],["10.42.1.42","2025-03-14T00:00:00Z",1],["10.42.1.42","2026-08-01T00:00:00Z",4],["10.42.1.42","2026-09-20T00:00:00Z",4],["10.42.1.42","2026-09-21T00:00:00Z",1],["10.42.1.43","2026-09-21T00:00:00Z",5]]`,
		"Domain investigation":        `[["10.42.1.42","archive.example.com",4],["10.42.1.42","legacy.example.com",1]]`,
		"Behavioral change detection": `[["10.42.1.100","2026-09",1],["10.42.1.42","2025-03",1],["10.42.1.42","2026-08",1],["10.42.1.42","2026-09",2],["10.42.1.43","2026-09",1]]`,
		"Rogue device check":          `[["10.42.1.42",4,10],["10.42.1.100",1,2],["10.42.1.43",1,5]]`,
	}
	if len(templates) != len(expected) {
		t.Fatalf("template count = %d; expected %d exact result contracts", len(templates), len(expected))
	}
	for _, tmpl := range templates {
		t.Run(tmpl.Label, func(t *testing.T) {
			want, ok := expected[tmpl.Label]
			if !ok {
				t.Fatal("new UI template needs an exact API result contract")
			}
			code, resp, raw := postInvestigate(t, tmpl.SQL)
			if code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", code, raw)
			}
			data := requireFixtureType[map[string]interface{}](t, resp.Data)
			rows := requireFixtureType[[]interface{}](t, data["rows"])
			// ORDER BY may leave ties unspecified; compare full rows as a multiset.
			var expectedRows []interface{}
			if err := json.Unmarshal([]byte(want), &expectedRows); err != nil {
				t.Fatal(err)
			}
			canonical := func(rows []interface{}) []string {
				out := make([]string, len(rows))
				for i, row := range rows {
					b, fixtureErr18063 := json.Marshal(row)
					if fixtureErr18063 != nil {
						t.Errorf("fixture operation failed: %v", fixtureErr18063)
					}
					out[i] = string(b)
				}
				sort.Strings(out)
				return out
			}
			if !reflect.DeepEqual(canonical(rows), canonical(expectedRows)) {
				t.Fatalf("rows = %s; want %s", raw, want)
			}
			assertNoSandboxSecrets(t, raw)
		})
	}
}

// TestInitDuckDBLocksEngineConfiguration covers S5 and the settings half of
// S6: the engine must never download extensions on its own, and once set up
// its configuration must be frozen so no later statement can re-enable that.
func TestInitDuckDBLocksEngineConfiguration(t *testing.T) {
	setupInvestigateSandbox(t)

	rows, err := duckDB.Query(`SELECT name, value FROM duckdb_settings()
		WHERE name IN ('autoinstall_known_extensions', 'autoload_known_extensions', 'allow_community_extensions', 'lock_configuration')
		ORDER BY name`)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	defer func() { checkTestClose(t, rows) }()
	got := map[string]string{}
	for rows.Next() {
		var name, value string
		if err := rows.Scan(&name, &value); err != nil {
			t.Fatalf("scan setting: %v", err)
		}
		got[name] = value
	}
	want := map[string]string{
		"allow_community_extensions":   "false",
		"autoinstall_known_extensions": "false",
		"autoload_known_extensions":    "false",
		"lock_configuration":           "true",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("settings mismatch\n got: %v\nwant: %v", got, want)
	}

	if _, err := duckDB.Exec("SET autoload_known_extensions = true"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected locked configuration to refuse SET, got %v", err)
	}
}

// TestInvestigateViewDoesNotExposeAppDatabase is the engine-side half of S1:
// even SQL that never passed the validator must find no name for the app
// database's other tables, because the view reads query_logs through
// sqlite_scan instead of ATTACHing the whole file.
func TestInvestigateViewDoesNotExposeAppDatabase(t *testing.T) {
	setupInvestigateSandbox(t)

	ctx := context.Background()
	conn, err := duckDB.Conn(ctx)
	if err != nil {
		t.Fatalf("acquire DuckDB connection: %v", err)
	}
	defer func() { checkTestClose(t, conn) }()
	if err := prepareInvestigateView(ctx, t, conn); err != nil {
		t.Fatalf("prepare view: %v", err)
	}

	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM query_logs").Scan(&n); err != nil || n != 5 {
		t.Fatalf("view count = %d, %v; want 5 rows", n, err)
	}
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM duckdb_databases() WHERE database_name NOT IN ('memory', 'system', 'temp')").Scan(&n); err != nil || n != 0 {
		t.Fatalf("attached databases = %d, %v; want none besides DuckDB's own", n, err)
	}
	var hash string
	err = conn.QueryRowContext(ctx, "SELECT password_hash FROM svart.admin_users").Scan(&hash)
	if err == nil || !strings.Contains(err.Error(), "Catalog Error") {
		t.Fatalf("expected no svart catalog, got hash=%q err=%v", hash, err)
	}
}

// TestInvestigateQueryIncludesParquetArchive proves the hot+cold view: once a
// day has been archived to Parquet, its rows must still be queryable next to
// the live SQLite rows, with one consistent timestamp type.
func TestInvestigateQueryIncludesParquetArchive(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)

	origArchivePath := archivePath
	archivePath = archiveDir
	t.Cleanup(func() { archivePath = origArchivePath })

	if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('2026-08-01 07:45:00', '10.42.1.42', 'telemetry.old-vendor.example', 'A', 'NXDOMAIN', 1, 4)"); err != nil {
		t.Fatalf("seed archived row: %v", err)
	}
	archived, err := archiveDay(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || archived != 1 {
		t.Fatalf("archiveDay = %d, %v; want 1 row archived", archived, err)
	}

	_, rows, _, err := investigateQuery(`SELECT query_name, timestamp::VARCHAR AS ts, typeof(timestamp) AS ts_type, coalesced_count
		FROM query_logs WHERE client_ip = '10.42.1.42' ORDER BY timestamp`, 5)
	if err != nil {
		t.Fatalf("expected hot+cold query to run, got %v", err)
	}
	want := [][]string{
		{"telemetry.old-vendor.example", "2026-08-01 07:45:00", "TIMESTAMP", "4"},
		{"www.google.com", "2026-09-20 08:15:00", "TIMESTAMP", "1"},
		{"ads.doubleclick.net", "2026-09-20 08:16:30", "TIMESTAMP", "3"},
		{"www.google.com", "2026-09-21 09:00:00", "TIMESTAMP", "1"},
	}
	if got := investigateRowsAsStrings(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows mismatch\n got: %v\nwant: %v", got, want)
	}
}

// legacyArchiveRow is the Parquet schema archives had before
// coalesced_count was added (DD-027); such files still sit in ARCHIVE_PATH.
type legacyArchiveRow struct {
	Timestamp int64  `parquet:"timestamp,snappy"`
	ClientIP  string `parquet:"client_ip,snappy,dict"`
	QueryName string `parquet:"query_name,snappy"`
	QueryType string `parquet:"query_type,snappy,dict"`
	Blocked   bool   `parquet:"blocked"`
}

// TestInvestigateQueryReadsMixedArchiveSchemas covers an archive directory
// holding both a current-schema file and a pre-coalesced_count file.
func TestInvestigateQueryReadsMixedArchiveSchemas(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)

	origArchivePath := archivePath
	archivePath = archiveDir
	t.Cleanup(func() { archivePath = origArchivePath })

	if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, coalesced_count) VALUES ('2026-08-02 06:00:00', '10.42.1.43', 'metrics.smarttv.example', 'A', 'NXDOMAIN', 1, 7)"); err != nil {
		t.Fatalf("seed archived row: %v", err)
	}
	if archived, err := archiveDay(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)); err != nil || archived != 1 {
		t.Fatalf("archiveDay = %d, %v; want 1 row archived", archived, err)
	}

	legacyPath := filepath.Join(archiveDir, archiveDayFileName(time.Date(2025, 3, 14, 0, 0, 0, 0, time.UTC)))
	legacy := []legacyArchiveRow{{
		Timestamp: time.Date(2025, 3, 14, 22, 5, 0, 0, time.UTC).UnixMilli(),
		ClientIP:  "10.42.1.43",
		QueryName: "firmware.smarttv.example",
		QueryType: "A",
		Blocked:   false,
	}}
	if err := parquet.WriteFile(legacyPath, legacy); err != nil {
		t.Fatalf("write legacy archive: %v", err)
	}

	_, rows, _, err := investigateQuery(`SELECT query_name, timestamp::VARCHAR AS ts, coalesce(coalesced_count, 1) AS n
		FROM query_logs WHERE client_ip = '10.42.1.43' ORDER BY timestamp`, 5)
	if err != nil {
		t.Fatalf("expected mixed-schema archive query to run, got %v", err)
	}
	want := [][]string{
		{"firmware.smarttv.example", "2025-03-14 22:05:00", "1"},
		{"metrics.smarttv.example", "2026-08-02 06:00:00", "7"},
		{"graph.facebook.com", "2026-09-21 10:30:00", "5"},
	}
	if got := investigateRowsAsStrings(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("rows mismatch\n got: %v\nwant: %v", got, want)
	}
}

// TestInvestigateAllowedFunctionsExistInEngine keeps the allowlist honest
// against the vendored DuckDB: a typo or a function renamed by an upgrade
// shows up here instead of as a mysteriously refused query.
func TestInvestigateAllowedFunctionsExistInEngine(t *testing.T) {
	setupInvestigateSandbox(t)

	rows, err := duckDB.Query("SELECT DISTINCT lower(function_name) FROM duckdb_functions()")
	if err != nil {
		t.Fatalf("list functions: %v", err)
	}
	defer func() { checkTestClose(t, rows) }()
	engine := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan function: %v", err)
		}
		engine[name] = struct{}{}
	}
	var missing []string
	for name := range investigateAllowedFunctions {
		if _, ok := engine[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("allowlisted functions absent from this DuckDB build: %v", missing)
	}
}

// TestValidateInvestigateASTFailsClosed feeds parse trees with node kinds
// DuckDB 1.1.3 never emits, standing in for syntax a future upgrade adds:
// the walker must refuse what it does not recognize.
func TestValidateInvestigateASTFailsClosed(t *testing.T) {
	const queryLogs = `{"type":"BASE_TABLE","alias":"","sample":null,"schema_name":"","table_name":"query_logs","column_name_alias":[],"catalog_name":""}`
	tests := []struct {
		name string
		ast  string
		want string
	}{
		{
			name: "unknown query node",
			ast:  `{"error":false,"statements":[{"node":{"type":"GRAPH_MATCH_NODE","modifiers":[]}}]}`,
			want: "invalid investigation query: unsupported SQL construct GRAPH_MATCH_NODE",
		},
		{
			name: "unknown expression class",
			ast:  `{"error":false,"statements":[{"node":{"type":"SELECT_NODE","modifiers":[],"cte_map":{"map":[]},"select_list":[{"class":"REMOTE_CALL","type":"REMOTE_CALL"}],"from_table":` + queryLogs + `}}]}`,
			want: "invalid investigation query: unsupported SQL construct REMOTE_CALL",
		},
		{
			name: "unknown table source",
			ast:  `{"error":false,"statements":[{"node":{"type":"SELECT_NODE","modifiers":[],"cte_map":{"map":[]},"select_list":[],"from_table":{"type":"COLUMN_DATA"}}}]}`,
			want: "invalid investigation query: unsupported table source COLUMN_DATA",
		},
		{
			name: "parser error is reported",
			ast:  `{"error":true,"error_type":"parser","error_message":"syntax error at or near \"FORM\""}`,
			want: `invalid investigation query: could not parse SQL: syntax error at or near "FORM"`,
		},
		{
			name: "no statements",
			ast:  `{"error":false,"statements":[]}`,
			want: "invalid investigation query: sql is required",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInvestigateAST([]byte(tc.ast))
			if err == nil || err.Error() != tc.want {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}

	if err := validateInvestigateAST([]byte(`{"error":false,"statements":[`)); err == nil || !strings.HasPrefix(err.Error(), "decode DuckDB parse tree:") {
		t.Fatalf("malformed parse tree: got %v, want a decode error", err)
	}
}

// TestInitDuckDBLeavesNoExtensionFiles is the regression test for the 45 MB
// extension copy that every engine init used to leave in TMPDIR.
func TestInitDuckDBLeavesNoExtensionFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	for i := 0; i < 3; i++ {
		if err := initDuckDB(filepath.Join(tmp, "absent.db"), tmp); err != nil {
			t.Fatalf("initDuckDB: %v", err)
		}
		var n int
		if err := duckDB.QueryRow("SELECT count(*) FROM duckdb_extensions() WHERE extension_name = 'sqlite_scanner' AND loaded").Scan(&n); err != nil || n != 1 {
			t.Fatalf("sqlite_scanner loaded = %d, err %v; want 1", n, err)
		}
		closeDuckDB()
	}
	left, err := filepath.Glob(filepath.Join(tmp, "*duckdb-ext*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("extension temp dirs left behind: %v", left)
	}
}
