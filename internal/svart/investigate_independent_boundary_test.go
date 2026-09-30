package svart

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
)

func TestIndependentInvestigationArchiveFailureNeverReturnsHotOnlySuccess(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)
	cold := []legacyArchiveRow{{Timestamp: time.Date(2025, 3, 14, 22, 5, 0, 0, time.UTC).UnixMilli(), ClientIP: "192.0.2.42", QueryName: "cold.example.com", QueryType: "A"}}
	if err := parquet.WriteFile(filepath.Join(archiveDir, "cold.parquet"), cold); err != nil {
		t.Fatal(err)
	}
	_, rows, _, err := investigateQuery(`SELECT count(*) FROM query_logs`, 5)
	if err != nil || fmt.Sprint(rows) != "[[6]]" {
		t.Fatalf("healthy unified result=%v err=%v", rows, err)
	}
	saved := archiveDir + ".preserved"
	if err := os.Rename(archiveDir, saved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(archiveDir); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if err := os.Rename(saved, archiveDir); err != nil {
			t.Error(err)
		}
	})
	if err := os.WriteFile(archiveDir, []byte("archive mount unavailable fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	status, response, raw := postInvestigate(t, `SELECT count(*) FROM query_logs`)
	t.Logf("unavailable archive response HTTP%d %s", status, raw)
	if status == http.StatusOK || response.Data != nil || response.Error == nil {
		t.Errorf("archive read failure must reject whole unified result, got HTTP%d %s", status, raw)
	}
	if _, err := investigateSchema(t.Context()); err == nil {
		t.Error("schema inspection silently omitted unavailable archive")
	}
}

func TestIndependentInvestigationTransportPreservesExactTypes(t *testing.T) {
	setupInvestigateSandbox(t)
	query := `SELECT 18446744073709551615::UBIGINT AS unsigned, 123456789012345678901234567890::HUGEINT AS huge, 12.34::DECIMAL(10,2) AS amount, DATE '2025-03-14' AS day, TIMESTAMP '2025-03-14 22:05:00' AS moment, [1::BIGINT, NULL, 3::BIGINT] AS numbers, {'a': 'dns', 'b': 2::BIGINT} AS record FROM query_logs LIMIT 1`
	conn, err := duckDB.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, conn) }()
	if err := prepareInvestigateView(t.Context(), t, conn); err != nil {
		t.Fatal(err)
	}
	direct, err := conn.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatal(err)
	}
	if !direct.Next() {
		t.Fatal("missing direct baseline")
	}
	want := make([]interface{}, 7)
	ptrs := make([]interface{}, 7)
	for i := range want {
		ptrs[i] = &want[i]
	}
	if err := direct.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, direct)
	columns, rows, _, err := investigateQuery(query, 5)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
		t.Fatalf("types changed: columns=%v got=%#v want=%#v err=%v", columns, rows, want, err)
	}
	status, _, raw := postInvestigate(t, query)
	if status != 200 {
		t.Fatalf("HTTP%d %s", status, raw)
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, string(encoded)) {
		t.Fatalf("JSON types changed: want %s got %s", encoded, raw)
	}
	t.Logf("exact numeric/date/list/struct JSON=%s", encoded)
}

func TestIndependentInvestigationNativeErrorIsPrivate(t *testing.T) {
	setupInvestigateSandbox(t)
	status, response, raw := postInvestigate(t, `SELECT error('independent-private-native-diagnostic') FROM query_logs LIMIT 1`)
	if status != http.StatusInternalServerError || response.Data != nil || response.Error == nil || *response.Error != "investigation query failed; check selected columns, types, and expressions" {
		t.Fatalf("native error was not safely reported: HTTP%d %s", status, raw)
	}
	if strings.Contains(raw, "independent-private-native-diagnostic") || strings.Contains(raw, duckDBPath) {
		t.Fatalf("native diagnostics escaped to HTTP: %s", raw)
	}
	t.Logf("native error HTTP%d %s", status, raw)
}
