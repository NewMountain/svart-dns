package svart

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func syncTombstoneBlocks(tx *sql.Tx, table, naturalKey, incoming string) (bool, error) {
	var deletedAt string
	err := tx.QueryRow("SELECT deleted_at FROM sync_tombstones WHERE table_name=? AND natural_key=?", table, naturalKey).Scan(&deletedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s tombstone: %w", table, err)
	}
	return !syncWriteWins(incoming, deletedAt), nil
}

type syncMissingReferenceError struct{ table string }

func (e *syncMissingReferenceError) Error() string {
	return e.table + " sync: missing reference; restore the referenced parent on the source peer; the next poll retries a full snapshot"
}

func syncReferenceError(table string, err error) error {
	if err == sql.ErrNoRows {
		return &syncMissingReferenceError{table: table}
	}
	return fmt.Errorf("%s sync: read reference: %w", table, err)
}

// An ignored statement (for example a SQLite RAISE(IGNORE) trigger) is not
// an applied change. Selected rows are locked within this transaction, so a
// zero-row mutation cannot be explained by a competing writer.
func execSyncMutation(tx *sql.Tx, query string, args ...any) error {
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read mutation result: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("mutation affected no rows; check database constraints and triggers")
	}
	return nil
}

// Only ErrNoRows permits insertion. In particular, a failed read must never
// become an INSERT attempt whose own failure could hide the original error.
func mergeSyncRow(tx *sql.Tx, table, naturalKey, incoming string,
	selectSQL string, selectArgs []any, insertSQL string, insertArgs []any,
	updateSQL string, updateArgs []any) (bool, error) {
	blocked, err := syncTombstoneBlocks(tx, table, naturalKey, incoming)
	if err != nil || blocked {
		return false, err
	}
	var stored string
	err = tx.QueryRow(selectSQL, selectArgs...).Scan(&stored)
	switch {
	case err == sql.ErrNoRows:
		err = execSyncMutation(tx, insertSQL, insertArgs...)
	case err != nil:
		return false, fmt.Errorf("read %s row: %w", table, err)
	case syncWriteWins(incoming, stored):
		err = execSyncMutation(tx, updateSQL, updateArgs...)
	default:
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("write %s row: %w", table, err)
	}
	return true, nil
}

// These predicates use the same natural-key order as recordTombstone callers.
// Subqueries preserve the harmless no-op for already-deleted parents, while
// real schema/read failures still propagate through the transaction.
// #nosec G101 -- Fixed schema deletion predicates describe token column names, not authentication material.
var syncDeletionPredicates = map[string]string{
	"upstreams":            "upstream = ?",
	"blocklists":           "alias = ?",
	"allowlists":           "alias = ?",
	"rewrites":             "domain = ?",
	"client_groups":        "name = ?",
	"ip_ranges":            "cidr = ?",
	"bootstrap_servers":    "server = ?",
	"api_tokens":           "token_prefix = ?",
	"admin_users":          "username = ?",
	"client_aliases":       "ip_address = ?",
	"settings":             "key = ?",
	"policies":             "name = ?",
	"client_policies":      "client_ip = ?",
	"client_group_members": "client_ip = ? AND group_id = (SELECT id FROM client_groups WHERE name = ?)",
	"client_blocklists":    "client_ip = ? AND blocklist_id = (SELECT id FROM blocklists WHERE alias = ?)",
	"client_allowlists":    "client_ip = ? AND allowlist_id = (SELECT id FROM allowlists WHERE alias = ?)",
	"group_blocklists":     "group_id = (SELECT id FROM client_groups WHERE name = ?) AND blocklist_id = (SELECT id FROM blocklists WHERE alias = ?)",
	"group_allowlists":     "group_id = (SELECT id FROM client_groups WHERE name = ?) AND allowlist_id = (SELECT id FROM allowlists WHERE alias = ?)",
	"range_blocklists":     "range_id = (SELECT id FROM ip_ranges WHERE cidr = ?) AND blocklist_id = (SELECT id FROM blocklists WHERE alias = ?)",
	"range_allowlists":     "range_id = (SELECT id FROM ip_ranges WHERE cidr = ?) AND allowlist_id = (SELECT id FROM allowlists WHERE alias = ?)",
	"blocked_domains":      "blocklist_id = (SELECT id FROM blocklists WHERE alias = ?) AND domain = ?",
	"allowed_domains":      "allowlist_id = (SELECT id FROM allowlists WHERE alias = ?) AND domain = ?",
	"policy_blocklists":    "policy_id = (SELECT id FROM policies WHERE name = ?) AND blocklist_id = (SELECT id FROM blocklists WHERE alias = ?)",
	"policy_allowlists":    "policy_id = (SELECT id FROM policies WHERE name = ?) AND allowlist_id = (SELECT id FROM allowlists WHERE alias = ?)",
}

// Compound keys must contain both components before entering the transaction.
func validSyncTombstoneKey(table, key string) bool {
	predicate, ok := syncDeletionPredicates[table]
	if !ok || key == "" {
		return false
	}
	if strings.Count(predicate, "?") != 2 {
		return true
	}
	parts := strings.SplitN(key, "|", 2)
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// Every matching row competes with the tombstone by timestamp, including
// relationships and manual rules. Buffer IDs before writes because a transaction
// owns one connection; check the entire result before deleting any row.
func applyTombstone(tx *sql.Tx, table, naturalKey string, deletedAt time.Time) error {
	if table == "settings" && isNodeLocalSyncSetting(naturalKey) {
		return nil
	}
	predicate, ok := syncDeletionPredicates[table]
	if !ok {
		return fmt.Errorf("unsupported tombstone table %q", table)
	}
	keys := []string{naturalKey}
	if strings.Count(predicate, "?") == 2 {
		keys = strings.SplitN(naturalKey, "|", 2)
		if len(keys) != 2 {
			return fmt.Errorf("%s tombstone requires a compound natural key", table)
		}
	}
	args := make([]any, len(keys))
	for i, k := range keys {
		args[i] = k
	}
	// #nosec G202 G701 -- Table and predicate were checked against syncDeletionPredicates; peer natural keys use placeholders.
	rows, err := tx.Query("SELECT rowid, COALESCE(updated_at,'') FROM "+table+" WHERE "+predicate, args...)
	if err != nil {
		return fmt.Errorf("read %s deletion candidates: %w", table, err)
	}
	defer closeQueryRows(rows)
	var expired []int64
	for rows.Next() {
		var id int64
		var updated string
		if err := rows.Scan(&id, &updated); err != nil {
			return fmt.Errorf("scan %s deletion candidate: %w", table, err)
		}
		if !storedSyncTimestamp(updated).After(deletedAt) {
			expired = append(expired, id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s deletion candidates: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s deletion candidates: %w", table, err)
	}
	for _, id := range expired {
		if err := execSyncMutation(tx, "DELETE FROM "+table+" WHERE rowid = ?", id); err != nil {
			return fmt.Errorf("delete %s row: %w", table, err)
		}
	}
	return nil
}

// Resolve optional references only for rows that can win. A renamed/deleted
// policy need not exist to discard a stale parent exported by an offline peer.
func syncRowAlreadyCurrent(tx *sql.Tx, table, column, key, incoming string) (bool, error) {
	var stored string
	err := tx.QueryRow("SELECT COALESCE(updated_at,'') FROM "+table+" WHERE "+column+"=?", key).Scan(&stored)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !syncWriteWins(incoming, stored), nil
}
