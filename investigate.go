package main

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/marcboeker/go-duckdb"
)

//go:embed web/duckdb-extensions/sqlite_scanner.duckdb_extension
var sqliteScannerExtension []byte

var (
	duckDB          *sql.DB
	investigateMu   sync.Mutex
	investigateBusy atomic.Bool
	duckDBPath      string // SQLite DB path read by the query_logs view
	duckDBExtDir    string // DuckDB recreates its (empty) extension dir; removed on close
	duckDBArchive   string // Parquet archive directory
)

// maxInvestigateRequestBodyBytes caps the /api/investigate request body
// (a JSON object with a "sql" string and a "timeout" int). No legitimate
// investigation query needs anywhere near this much SQL text.
const maxInvestigateRequestBodyBytes = 64 * 1024

var errInvalidInvestigateQuery = errors.New("invalid investigation query")

var errInvestigateArchiveUnavailable = errors.New("investigation archive unavailable; check server storage and retry")

// investigateSourceTable is the only catalog object user SQL may read.
const investigateSourceTable = "query_logs"

// maxInvestigateQueryLogsRefs bounds how many times query_logs (directly, not
// through a CTE alias) may be referenced in a single investigation query.
// query_logs spans up to 3 years of DNS traffic; an unbounded self-join (e.g.
// `SELECT count(*) FROM query_logs a, query_logs b, query_logs c, query_logs d`)
// cartesian-products that multi-year table and can pin the shared DuckDB
// engine well past any reasonable timeout. This is a coarse blast-radius cap,
// not a substitute for the timeout backstop below — a 3-way self-join is
// still allowed and bounded by the killable query worker deadline.
const maxInvestigateQueryLogsRefs = 3

// The investigation sandbox is enforced on DuckDB's own parse tree
// (json_serialize_sql), not on a tokenizer of our own: the parser that
// validates a query is the parser that runs it, so no syntax variant (FROM-
// first, string-literal tables, implicit lateral joins, ...) can mean one
// thing to the validator and another to the engine. The rules, in terms of
// that tree:
//
//   - exactly one statement, and it is a SELECT;
//   - every table reference is an unqualified name that is either query_logs
//     or a CTE in scope; everything else (other catalogs, schemas, table
//     functions, PIVOT/UNPIVOT, replacement scans on file names) is refused;
//   - every function called is on investigateAllowedFunctions;
//   - every node kind is one this walker knows; an unknown kind is refused,
//     so a DuckDB upgrade that adds syntax fails closed until reviewed.
//
// The engine side narrows what a hole could reach: the view reads only the
// query_logs table through sqlite_scan (the app database is never ATTACHed,
// so admin_users, api_tokens and settings have no name to reach them by),
// extension autoload/autoinstall are off, and the configuration is locked.

// investigateAllowedFunctions is every scalar, aggregate and macro function
// user SQL may call. It was built from duckdb_functions() of the DuckDB
// v1.1.3 bundled with go-duckdb v1.8.5, keeping the pure data functions and
// dropping everything that reads engine state (current_setting, getvariable,
// current_database/schema/query, version, in_search_path, get_block_size,
// the pg_catalog macros), parses or plans SQL (json_serialize_sql,
// json_deserialize_sql, json_serialize_plan), mutates session state
// (nextval, currval, setseed), or exposes internals (__internal_*, stats,
// vector_type, txid_current). Table functions are never callable: FROM
// accepts only query_logs and CTEs. It is an allowlist so that a DuckDB
// upgrade cannot quietly add a callable function; add names deliberately.
var investigateAllowedFunctions = newInvestigateNameSet(`
!__postfix !~~ !~~* % & && * ** + - ->> / // <-> << <=> <@ >> @ @> ^ ^@ | || ~ ~~ ~~* ~~~
abs acos acosh add age aggregate alias any_value apply approx_count_distinct
approx_quantile approx_top_k arbitrary arg_max arg_max_null arg_min arg_min_null argmax
argmin array_agg array_aggr array_aggregate array_append array_apply array_cat
array_concat array_contains array_cosine_distance array_cosine_similarity
array_cross_product array_distance array_distinct array_dot_product array_extract
array_filter array_grade_up array_has array_has_all array_has_any array_indexof
array_inner_product array_intersect array_length array_negative_dot_product
array_negative_inner_product array_pop_back array_pop_front array_position array_prepend
array_push_back array_push_front array_reduce array_resize array_reverse
array_reverse_sort array_select array_slice array_sort array_to_json array_to_string
array_to_string_comma_default array_transform array_unique array_value array_where
array_zip ascii asin asinh atan atan2 atanh avg bar base64 bin bit_and bit_count
bit_length bit_or bit_position bit_xor bitstring bitstring_agg bool_and bool_or
can_cast_implicitly cardinality cbrt ceil ceiling century chr combine concat concat_ws
constant_or_null contains corr cos cosh cot count count_if count_star covar_pop
covar_samp create_sort_key current_date damerau_levenshtein date_add date_diff date_part
date_sub date_trunc datediff datepart datesub datetrunc day dayname dayofmonth dayofweek
dayofyear decade decode degrees divide editdist3 element_at encode ends_with entropy
enum_code enum_first enum_last enum_range enum_range_boundary epoch epoch_ms epoch_ns
epoch_us equi_width_bins era error even exp factorial favg fdiv filter finalize first
flatten floor fmod format format_bytes formatReadableDecimalSize formatReadableSize
from_base64 from_binary from_hex from_json from_json_strict fsum gamma gcd
gen_random_uuid generate_series generate_subscripts geomean geometric_mean get_bit
get_current_time get_current_timestamp grade_up greatest greatest_common_divisor
group_concat hamming hash hex histogram histogram_exact hour ilike_escape instr
is_histogram_other_bin isfinite isinf isnan isodow isoyear jaccard jaro_similarity
jaro_winkler_similarity json json_array json_array_length json_contains json_exists
json_extract json_extract_path json_extract_path_text json_extract_string
json_group_array json_group_object json_group_structure json_keys json_merge_patch
json_object json_pretty json_quote json_structure json_transform json_transform_strict
json_type json_valid json_value julian kahan_sum kurtosis kurtosis_pop last last_day
lcase lcm least least_common_multiple left left_grapheme len length length_grapheme
levenshtein lgamma like_escape list list_aggr list_aggregate list_any_value list_append
list_apply list_approx_count_distinct list_avg list_bit_and list_bit_or list_bit_xor
list_bool_and list_bool_or list_cat list_concat list_contains list_cosine_distance
list_cosine_similarity list_count list_distance list_distinct list_dot_product
list_element list_entropy list_extract list_filter list_first list_grade_up list_has
list_has_all list_has_any list_histogram list_indexof list_inner_product list_intersect
list_kurtosis list_kurtosis_pop list_last list_mad list_max list_median list_min
list_mode list_negative_dot_product list_negative_inner_product list_pack list_position
list_prepend list_product list_reduce list_resize list_reverse list_reverse_sort
list_select list_sem list_skewness list_slice list_sort list_stddev_pop list_stddev_samp
list_string_agg list_sum list_transform list_unique list_value list_var_pop
list_var_samp list_where list_zip listagg ln log log10 log2 lower lpad ltrim mad
make_date make_time make_timestamp map map_concat map_contains map_contains_entry
map_contains_value map_entries map_extract map_from_entries map_keys map_values max
max_by md5 md5_number md5_number_lower md5_number_upper mean median microsecond
millennium millisecond min min_by minute mismatches mod mode month monthname multiply
nanosecond nextafter nfc_normalize not_ilike_escape not_like_escape now nullif
octet_length ord parse_dirname parse_dirpath parse_filename parse_path pi position pow
power prefix printf product quantile quantile_cont quantile_disc quarter radians random
range reduce regexp_escape regexp_extract regexp_extract_all regexp_full_match
regexp_matches regexp_replace regexp_split_to_array regexp_split_to_table regr_avgx
regr_avgy regr_count regr_intercept regr_r2 regr_slope regr_sxx regr_sxy regr_syy repeat
replace reservoir_quantile reverse right right_grapheme round round_even roundbankers
row row_to_json rpad rtrim second sem set_bit sha1 sha256 sign signbit sin sinh skewness
split split_part sqrt starts_with stddev stddev_pop stddev_samp str_split
str_split_regex strftime string_agg string_split string_split_regex string_to_array
strip_accents strlen strpos strptime struct_extract struct_insert struct_pack substr
substring substring_grapheme subtract suffix sum sum_no_overflow sumkahan tan tanh
time_bucket timetz_byte_comparable timezone timezone_hour timezone_minute to_base
to_base64 to_binary to_centuries to_days to_decades to_hex to_hours to_json
to_microseconds to_millennia to_milliseconds to_minutes to_months to_quarters to_seconds
to_timestamp to_weeks to_years today transaction_timestamp translate trim trunc
try_strptime typeof ucase unbin unhex unicode union_extract union_tag union_value
unnest unpivot_list upper url_decode url_encode uuid var_pop var_samp variance week
weekday weekofyear xor year yearweek
`)

func newInvestigateNameSet(names string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, name := range strings.Fields(names) {
		set[strings.ToLower(name)] = struct{}{}
	}
	return set
}

// investigateFunctionRefusal explains why a function outside the allowlist
// is refused, so the analyst learns what the sandbox protects rather than
// just that a name is missing.
func investigateFunctionRefusal(name string) string {
	switch {
	case strings.HasPrefix(name, "read_"), strings.HasPrefix(name, "parquet_"),
		name == "glob", name == "sniff_csv":
		return "filesystem access"
	case strings.HasPrefix(name, "sqlite_"):
		return "external database access"
	case name == "query", name == "query_table", name == "json_execute_serialized_sql",
		name == "json_serialize_plan", name == "json_serialize_sql", name == "json_deserialize_sql":
		return "dynamic SQL"
	case name == "current_setting", name == "getvariable":
		return "engine settings and variables are not readable"
	case strings.HasPrefix(name, "duckdb_"), strings.HasPrefix(name, "pragma_"), strings.HasPrefix(name, "pg_"),
		strings.HasPrefix(name, "current_"), name == "which_secret", name == "get_block_size", name == "version":
		return "engine metadata"
	default:
		return "not on the investigation function allowlist"
	}
}

// Parse-tree node kinds the walker accepts. Kinds not listed here, and
// expression classes not in investigateAllowedExpressionClasses, are refused.
var (
	investigateQueryNodeTypes = newInvestigateNameSet("SELECT_NODE SET_OPERATION_NODE RECURSIVE_CTE_NODE CTE_NODE BOUND_SUBQUERY_NODE")
	investigateTableRefTypes  = newInvestigateNameSet("BASE_TABLE SUBQUERY JOIN EMPTY EXPRESSION_LIST TABLE_FUNCTION PIVOT CTE SHOW_REF COLUMN_DATA DELIM_GET")
	// Result modifiers and ORDER BY entries carry a "type" but are plain
	// clauses over expressions the walker checks anyway.
	investigateClauseTypes = newInvestigateNameSet(`LIMIT_MODIFIER LIMIT_PERCENT_MODIFIER ORDER_MODIFIER DISTINCT_MODIFIER
		ORDER_DEFAULT ASCENDING DESCENDING`)
	investigateAllowedExpressionClasses = newInvestigateNameSet(`BETWEEN CASE CAST COLLATE COLUMN_REF COMPARISON CONJUNCTION
		CONSTANT FUNCTION LAMBDA LAMBDA_REF OPERATOR POSITIONAL_REFERENCE STAR SUBQUERY WINDOW`)
)

// investigateCTEScope is the set of CTE names (lower-cased) a table
// reference may resolve to at one point in the tree.
type investigateCTEScope map[string]struct{}

func (s investigateCTEScope) with(name string) investigateCTEScope {
	next := make(investigateCTEScope, len(s)+1)
	for k := range s {
		next[k] = struct{}{}
	}
	next[strings.ToLower(name)] = struct{}{}
	return next
}

// investigateASTWalk accumulates what the walk learned about the query.
type investigateASTWalk struct {
	queryLogsRefs int
}

func invalidInvestigate(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errInvalidInvestigateQuery, fmt.Sprintf(format, args...))
}

type investigateParseResult struct {
	Error        bool              `json:"error"`
	ErrorType    string            `json:"error_type"`
	ErrorMessage string            `json:"error_message"`
	Statements   []json.RawMessage `json:"statements"`
}

// validateInvestigateAST decides, from DuckDB's serialized parse tree of a
// query, whether the query may run. It is pure: astJSON is the output of
// json_serialize_sql for the exact text that will be executed.
func validateInvestigateAST(astJSON []byte) error {
	var parsed investigateParseResult
	if err := json.Unmarshal(astJSON, &parsed); err != nil {
		return fmt.Errorf("decode DuckDB parse tree: %w", err)
	}
	if parsed.Error {
		if parsed.ErrorType == "not implemented" {
			return invalidInvestigate("investigation only supports SELECT queries (PIVOT, PRAGMA, SET, ATTACH, COPY and other statements are not allowed)")
		}
		return invalidInvestigate("could not parse SQL: %s", parsed.ErrorMessage)
	}
	switch len(parsed.Statements) {
	case 0:
		return invalidInvestigate("sql is required")
	case 1:
	default:
		return invalidInvestigate("only a single SELECT statement is allowed")
	}

	var statement interface{}
	if err := json.Unmarshal(parsed.Statements[0], &statement); err != nil {
		return fmt.Errorf("decode DuckDB statement: %w", err)
	}
	walk := &investigateASTWalk{}
	if err := walk.value(statement, investigateCTEScope{}); err != nil {
		return err
	}
	if walk.queryLogsRefs == 0 {
		return invalidInvestigate("queries must read from query_logs")
	}
	return nil
}

func (w *investigateASTWalk) value(v interface{}, scope investigateCTEScope) error {
	switch node := v.(type) {
	case map[string]interface{}:
		return w.object(node, scope)
	case []interface{}:
		for _, child := range node {
			if err := w.value(child, scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *investigateASTWalk) children(node map[string]interface{}, scope investigateCTEScope, skip string) error {
	for key, child := range node {
		if key == skip {
			continue
		}
		if err := w.value(child, scope); err != nil {
			return err
		}
	}
	return nil
}

// object dispatches on the three shapes DuckDB serializes: expressions carry
// "class", query nodes and table references carry a string "type", and
// everything else (statement wrappers, CTE entries, sample options, typed
// constants) is a plain container walked for what it holds.
func (w *investigateASTWalk) object(node map[string]interface{}, scope investigateCTEScope) error {
	if class, ok := node["class"].(string); ok {
		return w.expression(class, node, scope)
	}
	kind, ok := node["type"].(string)
	if !ok {
		return w.children(node, scope, "")
	}
	switch {
	case hasInvestigateName(investigateQueryNodeTypes, kind):
		return w.queryNode(kind, node, scope)
	case hasInvestigateName(investigateTableRefTypes, kind):
		return w.tableRef(kind, node, scope)
	case hasInvestigateName(investigateClauseTypes, kind),
		// Type descriptors of CAST targets and typed constants (DECIMAL
		// width, LIST child, STRUCT fields, ...): data shapes, not sources.
		strings.HasSuffix(kind, "_TYPE_INFO"):
		return w.children(node, scope, "")
	default:
		return invalidInvestigate("unsupported SQL construct %s", kind)
	}
}

func hasInvestigateName(set map[string]struct{}, name string) bool {
	_, ok := set[strings.ToLower(name)]
	return ok
}

// Optional parser fields may be absent or null, but a present value must have
// the exact structural type before it can affect source or function admission.
func investigateOptionalField[T any](node map[string]interface{}, key string) (T, error) {
	var zero T
	raw, exists := node[key]
	if !exists || raw == nil {
		return zero, nil
	}
	value, ok := raw.(T)
	if !ok {
		return zero, invalidInvestigate("unsupported %s field type", key)
	}
	return value, nil
}

func (w *investigateASTWalk) queryNode(kind string, node map[string]interface{}, scope investigateCTEScope) error {
	switch kind {
	case "SELECT_NODE", "SET_OPERATION_NODE":
	case "RECURSIVE_CTE_NODE":
		return invalidInvestigate("recursive CTEs are not allowed")
	case "CTE_NODE":
		return invalidInvestigate("materialized CTEs are not allowed; drop MATERIALIZED")
	default:
		return invalidInvestigate("unsupported query form %s", kind)
	}

	// A CTE body sees the enclosing scope and the CTEs defined before it in
	// the same WITH, never itself: a non-recursive self-reference binds to a
	// catalog object of that name, so treating it as the CTE would let
	// `WITH duckdb_tables AS (SELECT * FROM duckdb_tables)` through.
	cteMap, err := investigateOptionalField[map[string]interface{}](node, "cte_map")
	if err != nil {
		return err
	}
	entries, err := investigateOptionalField[[]interface{}](cteMap, "map")
	if err != nil {
		return err
	}
	for _, raw := range entries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return invalidInvestigate("unsupported CTE definition")
		}
		name, ok := entry["key"].(string)
		if !ok || name == "" {
			return invalidInvestigate("unsupported CTE definition")
		}
		if err := w.value(entry["value"], scope); err != nil {
			return err
		}
		scope = scope.with(name)
	}
	return w.children(node, scope, "cte_map")
}

func (w *investigateASTWalk) tableRef(kind string, node map[string]interface{}, scope investigateCTEScope) error {
	switch kind {
	case "BASE_TABLE":
		return w.baseTable(node, scope)
	case "JOIN":
		// DuckDB binds any subquery in a join as LATERAL when it references
		// the other side, and the parse tree cannot tell the two apart, so
		// every joined subquery is refused; CTEs cannot be correlated.
		for _, side := range []string{"left", "right"} {
			if child, ok := node[side].(map[string]interface{}); ok && child["type"] == "SUBQUERY" {
				return invalidInvestigate("joined subqueries (including LATERAL) are not allowed; define the subquery as a CTE (WITH name AS (...)) and join the CTE")
			}
		}
		return w.children(node, scope, "")
	case "SUBQUERY", "EMPTY", "EXPRESSION_LIST":
		return w.children(node, scope, "")
	case "TABLE_FUNCTION":
		name := "unknown"
		if fn, ok := node["function"].(map[string]interface{}); ok {
			if n, ok := fn["function_name"].(string); ok {
				name = strings.ToLower(n)
			}
		}
		return invalidInvestigate("table function %s() is not allowed (%s); read from query_logs or a CTE", name, investigateFunctionRefusal(name))
	case "PIVOT":
		return invalidInvestigate("PIVOT/UNPIVOT sources are not allowed")
	default:
		return invalidInvestigate("unsupported table source %s", kind)
	}
}

func (w *investigateASTWalk) baseTable(node map[string]interface{}, scope investigateCTEScope) error {
	name, err := investigateOptionalField[string](node, "table_name")
	if err != nil {
		return err
	}
	catalog, err := investigateOptionalField[string](node, "catalog_name")
	if err != nil {
		return err
	}
	schema, err := investigateOptionalField[string](node, "schema_name")
	if err != nil {
		return err
	}
	if catalog != "" || schema != "" {
		return invalidInvestigate("schema-qualified tables are not allowed (%s); use query_logs only", strings.Trim(strings.Join([]string{catalog, schema, name}, "."), "."))
	}
	lower := strings.ToLower(name)
	if _, isCTE := scope[lower]; isCTE {
		return w.children(node, scope, "")
	}
	if lower != investigateSourceTable {
		return invalidInvestigate("source %q is not allowed; only query_logs and CTEs defined in the query can be read", name)
	}
	w.queryLogsRefs++
	if w.queryLogsRefs > maxInvestigateQueryLogsRefs {
		return invalidInvestigate("query_logs referenced more than %d times; self-joins beyond that can cartesian-product a multi-year table", maxInvestigateQueryLogsRefs)
	}
	return w.children(node, scope, "")
}

func (w *investigateASTWalk) expression(class string, node map[string]interface{}, scope investigateCTEScope) error {
	if !hasInvestigateName(investigateAllowedExpressionClasses, class) {
		return invalidInvestigate("unsupported SQL construct %s", class)
	}
	if class == "FUNCTION" || (class == "WINDOW" && node["type"] == "WINDOW_AGGREGATE") {
		if err := checkInvestigateFunction(node); err != nil {
			return err
		}
	}
	return w.children(node, scope, "")
}

// checkInvestigateFunction allows only unqualified (or parser-generated
// "main."-qualified) calls to allowlisted functions. Qualification is
// checked first because pg_catalog and attached catalogs hold macros whose
// bodies query engine metadata.
func checkInvestigateFunction(node map[string]interface{}) error {
	name, err := investigateOptionalField[string](node, "function_name")
	if err != nil {
		return err
	}
	catalog, err := investigateOptionalField[string](node, "catalog")
	if err != nil {
		return err
	}
	schema, err := investigateOptionalField[string](node, "schema")
	if err != nil {
		return err
	}
	lower := strings.ToLower(name)
	if catalog != "" || (schema != "" && !strings.EqualFold(schema, "main")) {
		return invalidInvestigate("schema-qualified function %s() is not allowed", strings.Trim(strings.Join([]string{catalog, schema, name}, "."), "."))
	}
	if _, ok := investigateAllowedFunctions[lower]; !ok {
		return invalidInvestigate("%s() is not allowed (%s)", lower, investigateFunctionRefusal(lower))
	}
	return nil
}

// validateInvestigateSQL parses sqlQuery with DuckDB's parser on conn and
// applies validateInvestigateAST to the result. json_serialize_sql only
// parses, so the untrusted text is never bound or executed here.
func validateInvestigateSQL(ctx context.Context, conn interface {
	QueryRowContext(context.Context, string, ...interface{}) *sql.Row
}, sqlQuery string) error {
	if strings.TrimSpace(sqlQuery) == "" {
		return invalidInvestigate("sql is required")
	}
	var astJSON string
	if err := conn.QueryRowContext(ctx, "SELECT json_serialize_sql(?::VARCHAR)::VARCHAR", sqlQuery).Scan(&astJSON); err != nil {
		return fmt.Errorf("parse investigation SQL: %w", err)
	}
	return validateInvestigateAST([]byte(astJSON))
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// initDuckDB opens an in-memory DuckDB instance, caps its memory budget,
// loads the embedded SQLite extension (no network access, no external
// files), and then freezes the engine configuration. The package-level
// engine is only replaced once every step has succeeded, so a failure never
// leaves a half-hardened engine serving queries.
func initDuckDB(dbPath, archiveDir string) error {
	engine, err := sql.Open("duckdb", ":memory:?threads=2&memory_limit=128MB")
	if err != nil {
		return fmt.Errorf("open DuckDB: %w", err)
	}
	if err := configureDuckDB(engine); err != nil {
		return errors.Join(err, engine.Close())
	}
	duckDB = engine
	duckDBPath = dbPath
	duckDBArchive = archiveDir
	return nil
}

func configureDuckDB(engine *sql.DB) error {
	// The embedded sqlite_scanner extension is written to a private directory
	// only long enough to LOAD it, then removed: a loaded extension stays
	// mapped after its file is unlinked, so nothing accumulates in TMPDIR
	// across restarts or engine re-inits (each extraction is 45 MB), and no
	// fixed shared path exists for another local user to plant a file at.
	extDir, err := os.MkdirTemp("", "svart-duckdb-ext-*")
	if err != nil {
		return fmt.Errorf("create extension temp dir: %w", err)
	}
	defer removeInvestigationScratch(extDir)
	if duckDBExtDir != "" {
		removeInvestigationScratch(duckDBExtDir) // clean up any previous initialized engine directory
	}
	duckDBExtDir = extDir
	extPath := filepath.Join(extDir, "v1.1.3", "linux_amd64")
	if err := os.MkdirAll(extPath, 0o700); err != nil {
		return fmt.Errorf("create extension path: %w", err)
	}
	if err := os.WriteFile(filepath.Join(extPath, "sqlite_scanner.duckdb_extension"), sqliteScannerExtension, 0o600); err != nil {
		return fmt.Errorf("write sqlite extension: %w", err)
	}

	setup := []string{
		// Buffer-manager budget; the query worker also enforces OS allocation limits.
		"SET memory_limit='128MB'",
		"SET temp_directory=''",
		// DuckDB defaults to one thread per CPU; an analyst query must not
		// compete with the resolver for every core.
		fmt.Sprintf("SET threads=%d", investigateThreads()),
		// Autoload/autoinstall would fetch extensions from extensions.duckdb.org
		// the moment a query mentioned one; the only extension we run is the
		// vendored sqlite_scanner, loaded explicitly below.
		"SET autoinstall_known_extensions=false",
		"SET autoload_known_extensions=false",
		"SET allow_community_extensions=false",
		"SET extension_directory=" + sqlStringLiteral(extDir),
		"LOAD sqlite",
		// Must stay last: nothing after this point, user SQL included, can
		// change a setting.
		"SET lock_configuration=true",
	}
	for _, stmt := range setup {
		if _, err := engine.Exec(stmt); err != nil {
			return fmt.Errorf("configure DuckDB (%s): %w", stmt, err)
		}
	}
	return nil
}

// investigateThreads leaves at least half the machine to DNS, capped at 4.
func investigateThreads() int {
	return max(1, min(4, runtime.NumCPU()/2))
}

// closeDuckDB shuts down the DuckDB instance.
func closeDuckDB() {
	if err := closeDuckDBChecked(); err != nil {
		slog.Error("investigation engine cleanup failed", "component", "investigation", "error", err)
	}
}

func closeDuckDBChecked() error {
	if duckDB != nil {
		if err := duckDB.Close(); err != nil {
			return fmt.Errorf("close investigation engine: %w", err)
		}
	}
	if duckDBExtDir != "" {
		if err := os.RemoveAll(duckDBExtDir); err != nil {
			return fmt.Errorf("remove investigation extension directory: %w", err)
		}
		duckDBExtDir = ""
	}
	return nil
}

func removeInvestigationScratch(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Error("investigation temporary directory cleanup failed", "component", "investigation", "path", path, "error", err)
	}
}

// Both HTTP cancellation and archive shutdown must remain responsive while a
// bounded ownership transition or another query holds the shared engine lock.
func lockInvestigation(ctx context.Context, stop <-chan struct{}) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !investigateMu.TryLock() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-stop:
			return errors.New("summary archive stopped while waiting for investigation")
		case <-ticker.C:
		}
	}
	return nil
}

// investigateViewSQL defines the query_logs view user SQL reads: the live
// SQLite table unioned with every Parquet archive. It names the one SQLite
// table it needs instead of ATTACHing the app database, so the rest of that
// database (admin_users, api_tokens, settings) has no name inside DuckDB.
// Schema groups are unioned by name because archives carry no id column,
// store timestamps as epoch milliseconds, and older files lack newer columns.
// Homogeneous scans load file metadata lazily inside the worker memory budget.
func investigateViewSQL(ctx context.Context, dbPath, archiveDir string) (string, error) {
	if err := checkSummaryAvailability(ctx, dbPath, archiveDir); err != nil {
		return "", fmt.Errorf("%w: %w", errInvestigateArchiveUnavailable, err)
	}
	hot := fmt.Sprintf("SELECT * FROM sqlite_scan(%s, 'query_logs')", sqlStringLiteral(dbPath))
	scans, err := investigateArchiveScans(ctx, archiveDir)
	if err != nil {
		if errors.Is(err, errInvestigateArchiveSchema) {
			return "", err
		}
		return "", fmt.Errorf("%w: %w", errInvestigateArchiveUnavailable, err)
	}
	return "CREATE OR REPLACE TEMP VIEW query_logs AS " + strings.Join(append([]string{hot}, scans...), " UNION ALL BY NAME "), nil
}

// investigateQuery is also used by internal callers with no HTTP context.
func investigateQuery(sqlQuery string, timeoutSec int) ([]string, [][]interface{}, time.Duration, error) {
	return investigateQueryContext(context.Background(), sqlQuery, timeoutSec)
}

// investigateSchema returns the column metadata of the query_logs view
// (live SQLite unioned with any Parquet archives) by inspecting the result
// column types of a LIMIT 0 query against it.
func investigateSchema(ctx context.Context) ([]InvestigationSchemaTable, error) {
	ctx, cancel := context.WithTimeout(ctx, summaryMetadataReadTimeout)
	defer cancel()
	response, err := runInvestigateProcess(ctx, investigateWorkerRequest{Operation: investigateSchemaOperation})
	if err != nil {
		return nil, err
	}
	return []InvestigationSchemaTable{{Name: "query_logs", Type: "view", Columns: response.Schema}}, nil
}

// handleAPIInvestigate godoc
// @Summary Execute investigation SQL query
// @Description Runs a constrained read-only SELECT query against the unified query_logs view (live SQLite + archived Parquet). Only one query runs at a time.
// @Tags Investigation
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param body body object true "Query request" example({"sql": "SELECT client_ip, COUNT(*) FROM query_logs GROUP BY client_ip", "timeout": 30})
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Failure 504 {object} apiResponse
// @Router /api/investigate [post]
func handleAPIInvestigate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req InvestigateRequest
	if !decodeJSONBody(w, r, maxInvestigateRequestBodyBytes, &req) {
		return
	}

	if req.SQL == "" {
		writeError(w, http.StatusBadRequest, "sql is required")
		return
	}
	if req.Timeout <= 0 {
		req.Timeout = 30
	}
	if req.Timeout > 60 {
		req.Timeout = 60
	}

	// Non-blocking concurrency check: reject immediately if another query is running
	if !investigateBusy.CompareAndSwap(false, true) {
		writeError(w, http.StatusTooManyRequests, "investigation query already in progress")
		return
	}
	defer investigateBusy.Store(false)

	cols, rows, duration, err := investigateQueryContext(r.Context(), req.SQL, req.Timeout)
	if err != nil {
		if errors.Is(err, errInvestigateArchiveUnavailable) {
			writeError(w, http.StatusServiceUnavailable, errInvestigateArchiveUnavailable.Error())
			return
		}
		if errors.Is(err, errInvestigateResource) {
			writeError(w, http.StatusUnprocessableEntity, errInvestigateResource.Error())
			return
		}
		if errors.Is(err, context.Canceled) {
			writeError(w, http.StatusRequestTimeout, "investigation request canceled")
			return
		}
		if errors.Is(err, errInvalidInvestigateQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			writeError(w, http.StatusGatewayTimeout, "query timeout exceeded")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	encodedRows, err := json.Marshal(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Investigation result cannot be encoded")
		return
	}
	writeJSON(w, http.StatusOK, InvestigationView{Columns: cols, Rows: encodedRows, RowCount: len(rows), DurationMS: duration.Milliseconds()})
}

// handleAPIInvestigateSchema godoc
// @Summary Get investigation schema
// @Description Returns column metadata for the query_logs unified view (live + archived)
// @Tags Investigation
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/investigate/schema [get]
func handleAPIInvestigateSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	tables, err := investigateSchema(r.Context())
	if err != nil {
		if errors.Is(err, errInvestigateArchiveUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			logAdmin.Error("investigation archive discovery failed", "error", err)
			writeError(w, http.StatusServiceUnavailable, errInvestigateArchiveUnavailable.Error())
			return
		}
		logAdmin.Error("investigation schema unavailable", "error", err)
		writeError(w, http.StatusInternalServerError, "investigation schema unavailable; check server storage and retry")
		return
	}

	writeJSON(w, http.StatusOK, InvestigationSchemaView{Tables: tables})
}
