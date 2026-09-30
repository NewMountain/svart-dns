package svart

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func compatibilityStore(kind string) (listStore, error) {
	switch kind {
	case "blocklist":
		return blockListStore, nil
	case "allowlist":
		return allowListStore, nil
	default:
		return listStore{}, fmt.Errorf("unknown list kind %q", kind)
	}
}

func decodeListCompatibilityReport(raw string) (*ListCompatibilityReport, error) {
	if raw == "" {
		return nil, nil
	}
	report := ListCompatibilityReport{Version: -1, Lines: -1, Applied: -1, Unsupported: -1, Invalid: -1}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, fmt.Errorf("read list compatibility report: %w", err)
	}
	if report.Version != 1 {
		return nil, fmt.Errorf("unsupported list compatibility report version %d", report.Version)
	}
	if _, err := time.Parse(time.RFC3339Nano, report.AssessedAt); err != nil {
		return nil, fmt.Errorf("invalid compatibility assessment time: %w", err)
	}
	if report.Lines < 0 || report.Applied < 0 || report.Unsupported < 0 || report.Invalid < 0 {
		return nil, fmt.Errorf("missing or negative compatibility report counters")
	}
	if report.Unsupported > len(report.Diagnostics) || report.Invalid != len(report.Diagnostics)-report.Unsupported {
		return nil, fmt.Errorf("compatibility diagnostics do not match rejection counts")
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Line < 1 || diagnostic.Line > report.Lines || strings.TrimSpace(diagnostic.Rule) == "" || strings.TrimSpace(diagnostic.Reason) == "" {
			return nil, fmt.Errorf("invalid compatibility diagnostic at line %d", diagnostic.Line)
		}
	}
	return &report, nil
}

func readListCompatibilityReport(q queryer, kind string, id int) (*ListCompatibilityReport, error) {
	store, err := compatibilityStore(kind)
	if err != nil {
		return nil, err
	}
	var raw string
	found := false
	err = scanRows(q, "SELECT COALESCE(g.compatibility_report,'') FROM "+store.lists+" l LEFT JOIN local_"+store.kind+"_generations g ON g.list_id=l.id AND g.url=l.url WHERE l.id=?", func(rows *sql.Rows) error {
		found = true
		return rows.Scan(&raw)
	}, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, sql.ErrNoRows
	}
	return decodeListCompatibilityReport(raw)
}

// listViewRow is shared storage projection for the two existing API view types.
type listViewRow BlocklistView

// A single statement binds metadata and its URL-qualified assessment to the
// same SQLite read snapshot, including concurrent refreshes or source edits.
func readListViewRows(q queryer, kind string) ([]listViewRow, error) {
	store, err := compatibilityStore(kind)
	if err != nil {
		return nil, err
	}
	result := []listViewRow{}
	err = scanRows(q, "SELECT l.id, l.url, l.alias, l.enabled, l.domain_count, COALESCE(l.last_updated, ''), COALESCE(l.refresh_interval, 0), COALESCE(g.compatibility_report,'') FROM "+store.lists+" l LEFT JOIN local_"+kind+"_generations g ON g.list_id=l.id AND g.url=l.url ORDER BY l.id", func(rows *sql.Rows) error {
		var row listViewRow
		var raw string
		if err := rows.Scan(&row.ID, &row.URL, &row.Alias, &row.Enabled, &row.DomainCount, &row.LastUpdated, &row.RefreshInterval, &raw); err != nil {
			return err
		}
		report, err := decodeListCompatibilityReport(raw)
		if err != nil {
			return err
		}
		row.Compatibility = compatibilitySummary(report)
		result = append(result, row)
		return nil
	})
	return result, err
}
