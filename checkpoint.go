package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
)

type CheckpointPage struct {
	BlocklistID int      `json:"blocklist_id"`
	HistoryID   int      `json:"history_id"`
	RefreshedAt string   `json:"refreshed_at"`
	Domains     []string `json:"domains"`
	Total       int      `json:"total"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}

// Temporary B-trees spill to disk; the main and temporary page caches each
// have a 2 MiB budget. Only the requested page crosses the Go boundary.
// The reserved read connection never acquires the application's write pool.
func queryCheckpointPage(ctx context.Context, historyID int, search string, limit, offset int) (page CheckpointPage, err error) {
	page = CheckpointPage{HistoryID: historyID, Domains: []string{}, Limit: limit, Offset: offset}
	conn, err := readDB.Conn(ctx)
	if err != nil {
		return page, err
	}
	defer closeReadResource(conn)
	var cacheSize, mmapSize, tempStore, tempCacheSize int
	for _, setting := range []struct {
		name string
		dst  *int
	}{
		{"cache_size", &cacheSize}, {"mmap_size", &mmapSize}, {"temp_store", &tempStore}, {"temp.cache_size", &tempCacheSize},
	} {
		if err = conn.QueryRowContext(ctx, "PRAGMA "+setting.name).Scan(setting.dst); err != nil {
			return page, err
		}
	}
	// A canceled request still releases its scratch tables and restores pool
	// connection settings. Closing a poisoned connection prevents state leakage.
	defer func() {
		for _, q := range []string{
			"DROP TABLE IF EXISTS temp.checkpoint_changes", "DROP TABLE IF EXISTS temp.checkpoint_domains",
			fmt.Sprintf("PRAGMA cache_size=%d", cacheSize), fmt.Sprintf("PRAGMA mmap_size=%d", mmapSize), fmt.Sprintf("PRAGMA temp_store=%d", tempStore), fmt.Sprintf("PRAGMA temp.cache_size=%d", tempCacheSize),
		} {
			if _, cleanupErr := conn.ExecContext(context.Background(), q); cleanupErr != nil {
				if err == nil {
					err = cleanupErr
				}
				// database/sql discards a connection when Raw returns driver.ErrBadConn.
				if discardErr := conn.Raw(func(interface{}) error { return driver.ErrBadConn }); discardErr != nil && !errors.Is(discardErr, driver.ErrBadConn) {
					err = errors.Join(err, discardErr)
				}
				break
			}
		}
	}()
	for _, q := range []string{"PRAGMA temp_store=FILE", "PRAGMA cache_size=-2048", "PRAGMA mmap_size=0", "PRAGMA temp.cache_size=-2048"} {
		if _, err = conn.ExecContext(ctx, q); err != nil {
			return page, err
		}
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer rollbackTransaction(tx)
	if err = tx.QueryRowContext(ctx, "SELECT blocklist_id, refreshed_at FROM blocklist_history WHERE id=?", historyID).Scan(&page.BlocklistID, &page.RefreshedAt); err != nil {
		return page, err
	}
	// IDs establish commit order even when SQLite timestamps have the same
	// second or historical imports use a different timestamp representation.
	for _, step := range []struct {
		query string
		args  []interface{}
	}{
		{`CREATE TEMP TABLE checkpoint_changes(domain TEXT PRIMARY KEY, action TEXT NOT NULL CHECK(action IN ('added','removed'))) WITHOUT ROWID`, nil},
		{`INSERT INTO temp.checkpoint_changes SELECT c.domain,c.action
 FROM blocklist_history h JOIN blocklist_changelog c ON c.history_id=h.id
 WHERE h.blocklist_id=? AND h.id>? ORDER BY h.id,c.id ON CONFLICT(domain) DO NOTHING`, []interface{}{page.BlocklistID, historyID}},
		{`CREATE TEMP TABLE checkpoint_domains(domain TEXT PRIMARY KEY) WITHOUT ROWID`, nil},
		{`INSERT INTO temp.checkpoint_domains SELECT domain FROM blocked_domains b
 WHERE blocklist_id=? AND NOT EXISTS(SELECT 1 FROM temp.checkpoint_changes c WHERE c.domain=b.domain)`, []interface{}{page.BlocklistID}},
		{`INSERT INTO temp.checkpoint_domains SELECT domain FROM temp.checkpoint_changes WHERE action='removed'`, nil},
	} {
		if _, err = tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return page, err
		}
	}
	// instr implements the existing case-sensitive literal substring search;
	// LIKE would change wildcard and case semantics.
	// #nosec G202 G701 -- Source-text projection is fixed SQL; search and pagination remain bound parameters.
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM temp.checkpoint_domains WHERE instr((`+storedRuleTextSQL+`),?)>0`, search).Scan(&page.Total); err != nil {
		return page, err
	}
	// #nosec G202 G701 -- Source-text projection is fixed SQL; search and pagination remain bound parameters.
	rows, err := tx.QueryContext(ctx, `SELECT domain FROM temp.checkpoint_domains WHERE instr((`+storedRuleTextSQL+`),?)>0 ORDER BY `+storedRuleTextSQL+`,domain LIMIT ? OFFSET ?`, search, limit, offset)
	if err != nil {
		return page, err
	}
	defer closeQueryRows(rows)
	for rows.Next() {
		var domain string
		if err = rows.Scan(&domain); err != nil {
			return page, err
		}
		text, displayErr := storedRuleDisplayText(domain)
		if displayErr != nil {
			return page, displayErr
		}
		page.Domains = append(page.Domains, text)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if err = rows.Close(); err != nil {
		return page, err
	}
	return page, tx.Commit()
}
