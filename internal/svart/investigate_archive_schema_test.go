package svart

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestInvestigateArchiveGroupsPreserveAllSchemasAndLiteralNames(t *testing.T) {
	archiveDir := setupInvestigateSandbox(t)
	for i, name := range []string{"one.parquet", "one[1].parquet", "one*.parquet", "one?.parquet", "one'quote.parquet"} {
		rows := []legacyArchiveRow{{Timestamp: 1000, ClientIP: "192.0.2.1", QueryName: fmt.Sprintf("legacy-%d.example", i), QueryType: "A"}}
		if err := parquet.WriteFile(filepath.Join(archiveDir, name), rows); err != nil {
			t.Fatal(err)
		}
	}
	type reorderedArchive struct {
		QueryName      string `parquet:"query_name"`
		CoalescedCount int64  `parquet:"coalesced_count"`
		Timestamp      int64  `parquet:"timestamp"`
	}
	if err := parquet.WriteFile(filepath.Join(archiveDir, "reordered.parquet"), []reorderedArchive{{"reordered.example", 7, 2000}}); err != nil {
		t.Fatal(err)
	}
	if err := parquet.WriteFile(filepath.Join(archiveDir, "modern.parquet"), []QueryLogRow{{Timestamp: 3000, QueryName: "modern.example", CoalescedCount: 9}}); err != nil {
		t.Fatal(err)
	}
	_, rows, _, err := investigateQuery(`SELECT query_name, COALESCE(coalesced_count, 1), epoch_ms(timestamp) FROM query_logs WHERE timestamp < '1971-01-01' ORDER BY query_name`, 15)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"legacy-0.example", "1", "1000"}, {"legacy-1.example", "1", "1000"}, {"legacy-2.example", "1", "1000"}, {"legacy-3.example", "1", "1000"}, {"legacy-4.example", "1", "1000"}, {"modern.example", "9", "3000"}, {"reordered.example", "7", "2000"}}
	if got := investigateRowsAsStrings(rows); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func TestInvestigateArchiveSchemaDiscoveryCancellation(t *testing.T) {
	archiveDir := t.TempDir()
	if err := parquet.WriteFile(filepath.Join(archiveDir, "one.parquet"), []QueryLogRow{{Timestamp: 1000}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := investigateArchiveScans(ctx, archiveDir); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery: %v", err)
	}
}
