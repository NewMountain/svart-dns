package main

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/yeti/svart-dns/internal/policycore"
)

type localSnapshot uint16

const (
	snapshotNone   localSnapshot = 0
	snapshotPolicy localSnapshot = 1 << iota
	snapshotAliases
	snapshotRewrites
	snapshotAuth
	snapshotSettings
	snapshotSync
	snapshotUpstreams
	snapshotListContent
)

// Snapshot validation belongs inside the writer transaction. Publication cannot
// fail after commit, and a rejected write/read/commit leaves all live state intact.
func localMutation(kind localSnapshot, change func(*sql.Tx) error, changed ...policycore.ListKey) error {
	return localMutationLists(kind, change, func() []policycore.ListKey { return changed })
}

func localMutationLists(kind localSnapshot, change func(*sql.Tx) error, changedLists func() []policycore.ListKey) error {
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	if err := change(tx); err != nil {
		return err
	}
	changed := changedLists()
	var policy *policycore.Snapshot
	var aliases map[string]string
	var rewrites *syncMapSnapshot
	var users, tokens int
	var settings runtimeSettingsSnapshot
	var logging bool
	var syncSettings map[string]string
	var upstreams []Upstream
	if kind&snapshotPolicy != 0 {
		scope := reloadEntities
		if len(changed) > 0 || policyNeedsFullReload || kind&snapshotListContent != 0 {
			scope = reloadListContent
		}
		if policyNeedsFullReload {
			changed = nil
		}
		policy, _, err = buildPolicySnapshot(tx, policyState.Load(), scope, changed...)
		if err != nil {
			return err
		}
	}
	if kind&snapshotUpstreams != 0 {
		upstreams, err = readUpstreams(tx)
		if err != nil {
			return err
		}
	}
	if kind&snapshotAliases != 0 {
		aliases, err = readClientAliases(tx)
		if err != nil {
			return err
		}
	}
	if kind&snapshotRewrites != 0 {
		rewrites, err = readRewrites(tx)
		if err != nil {
			return err
		}
	}
	if kind&snapshotAuth != 0 {
		users, tokens, err = readAuthCounts(tx)
		if err != nil {
			return err
		}
	}
	if kind&snapshotSettings != 0 {
		settings, err = readRuntimeSettings(tx)
		if err != nil {
			return err
		}
		logging, err = readLoggingSetting(tx)
		if err != nil {
			return err
		}
	}
	if kind&snapshotSync != 0 {
		syncSettings, err = readSyncSettingsFrom(tx)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if kind&snapshotUpstreams != 0 {
		upstreamStorage.publish(upstreams)
	}
	if kind&snapshotSettings != 0 {
		runtimeSettings.Store(settings)
		loggingEnabled.Store(logging)
	}
	if policy != nil {
		policyState.Store(policy)
		policyNeedsFullReload = false
		policyCache.Clear()
		updateListMetrics(policy.Index)
	}
	if aliases != nil {
		clientAliasCache.Store(aliases)
	}
	if rewrites != nil {
		rewritesCache.Store(rewrites)
	}
	if kind&snapshotAuth != 0 {
		publishAuthCounts(users, tokens)
	}
	if syncSettings != nil {
		restartSyncWithSettings(syncSettings)
	}
	return nil
}

func writeLocalTombstone(tx *sql.Tx, table, key string) error {
	ts, nid := syncNow()
	result, err := tx.Exec(`INSERT OR REPLACE INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) VALUES(?,?,?,?)`, table, key, ts, nid)
	return checkListWrite(result, err, 1)
}

type localEntity struct{ key, id string }

var localEntities = map[string]localEntity{
	"upstreams": {"upstream", "id"}, "client_groups": {"name", "id"}, "policies": {"name", "id"}, "ip_ranges": {"cidr", "id"},
	"blocklists": {"alias", "id"}, "allowlists": {"alias", "id"}, "rewrites": {"domain", "id"},
	"client_aliases": {"ip_address", "ip_address"}, "admin_users": {"username", "id"}, "api_tokens": {"token_prefix", "id"},
}

type localRelationship struct{ table, leftTable, leftColumn, rightTable, rightColumn string }

var localRelationships = []localRelationship{
	{"client_group_members", "", "client_ip", "client_groups", "group_id"},
	{"client_blocklists", "", "client_ip", "blocklists", "blocklist_id"},
	{"client_allowlists", "", "client_ip", "allowlists", "allowlist_id"},
	{"group_blocklists", "client_groups", "group_id", "blocklists", "blocklist_id"},
	{"group_allowlists", "client_groups", "group_id", "allowlists", "allowlist_id"},
	{"range_blocklists", "ip_ranges", "range_id", "blocklists", "blocklist_id"},
	{"range_allowlists", "ip_ranges", "range_id", "allowlists", "allowlist_id"},
	{"policy_blocklists", "policies", "policy_id", "blocklists", "blocklist_id"},
	{"policy_allowlists", "policies", "policy_id", "allowlists", "allowlist_id"},
	{"blocked_domains", "blocklists", "blocklist_id", "", "domain"},
	{"allowed_domains", "allowlists", "allowlist_id", "", "domain"},
	{"client_policies", "", "client_ip", "policies", "policy_id"},
}

func (r localRelationship) keysQuery() string {
	left, right := "j."+r.leftColumn, "j."+r.rightColumn
	joins := ""
	if r.leftTable != "" {
		left = "l." + localEntities[r.leftTable].key
		joins += " JOIN " + r.leftTable + " l ON l.id=j." + r.leftColumn
	}
	if r.rightTable != "" {
		right = "r." + localEntities[r.rightTable].key
		joins += " JOIN " + r.rightTable + " r ON r.id=j." + r.rightColumn
	}
	key := left + " || '|' || " + right
	if r.table == "client_policies" {
		key = left
	}
	return "SELECT " + key + " FROM " + r.table + " j" + joins
}

func recordLocalKeys(tx *sql.Tx, table, query string, args ...any) error {
	var count int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM ("+query+")", args...).Scan(&count); err != nil {
		return err
	}
	ts, nid := syncNow()
	// #nosec G202 G701 -- SQL fragments come only from localEntities/localRelationships and literal caller clauses; all request values are bound.
	result, err := tx.Exec("INSERT OR REPLACE INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) SELECT ?,keys.*,?,? FROM ("+query+") keys", append([]any{table, ts, nid}, args...)...)
	return checkListWrite(result, err, count)
}

func recordEntityTombstones(tx *sql.Tx, table, key string, id any) error {
	if err := writeLocalTombstone(tx, table, key); err != nil {
		return err
	}
	for _, r := range localRelationships {
		column := ""
		if r.leftTable == table {
			column = r.leftColumn
		}
		if r.rightTable == table {
			column = r.rightColumn
		}
		if column == "" {
			continue
		}
		query := r.keysQuery() + " WHERE j." + column + "=?"
		if r.table == "blocked_domains" || r.table == "allowed_domains" {
			query += " AND l.url=''"
		}
		if err := recordLocalKeys(tx, r.table, query, id); err != nil {
			return err
		}
	}
	return nil
}

func deleteEntityTx(tx *sql.Tx, table string, id any) error {
	spec := localEntities[table]
	var key string
	if err := tx.QueryRow("SELECT "+spec.key+" FROM "+table+" WHERE "+spec.id+"=?", id).Scan(&key); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if err := recordEntityTombstones(tx, table, key, id); err != nil {
		return err
	}
	if table == "policies" {
		ts, nid := syncNow()
		for _, parent := range []string{"ip_ranges", "client_groups"} {
			if err := updateMatchingRows(tx, parent, "policy_id=NULL,updated_at=?,node_id=?", "policy_id=?", []any{ts, nid}, id); err != nil {
				return err
			}
		}
	}
	// #nosec G202 G701 -- SQL fragments come only from localEntities/localRelationships and literal caller clauses; all request values are bound.
	result, err := tx.Exec("DELETE FROM "+table+" WHERE "+spec.id+"=?", id)
	return checkListWrite(result, err, 1)
}

func deleteLocalEntity(table string, id any, kind localSnapshot) error {
	return localMutation(kind, func(tx *sql.Tx) error { return deleteEntityTx(tx, table, id) })
}

func removeLocalRelationship(table string, left, right any) error {
	return localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		for _, r := range localRelationships {
			if r.table != table {
				continue
			}
			where := " WHERE j." + r.leftColumn + "=? AND j." + r.rightColumn + "=?"
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM "+table+" j"+where, left, right).Scan(&count); err != nil {
				return err
			}
			leftKey, err := localNaturalPart(tx, r.leftTable, left)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			rightKey, err := localNaturalPart(tx, r.rightTable, right)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			key := leftKey + "|" + rightKey
			if table == "client_policies" {
				key = leftKey
			}
			if err := writeLocalTombstone(tx, table, key); err != nil {
				return err
			}

			// #nosec G202 G701 -- SQL fragments come only from localEntities/localRelationships and literal caller clauses; all request values are bound.
			result, err := tx.Exec("DELETE FROM "+table+" AS j"+where, left, right)
			return checkListWrite(result, err, int64(count))
		}
		return fmt.Errorf("unknown local relationship %s", table)
	})
}

func renameEntityTx(tx *sql.Tx, table string, id any, key string) error {
	spec := localEntities[table]
	var old string
	if err := tx.QueryRow("SELECT "+spec.key+" FROM "+table+" WHERE "+spec.id+"=?", id).Scan(&old); err != nil {
		return err
	}
	if old == key {
		return nil
	}
	if err := recordEntityTombstones(tx, table, old, id); err != nil {
		return err
	}
	ts, nid := syncNow()
	// #nosec G202 G701 -- SQL fragments come only from localEntities/localRelationships and literal caller clauses; all request values are bound.
	result, err := tx.Exec("UPDATE "+table+" SET "+spec.key+"=?,updated_at=?,node_id=? WHERE "+spec.id+"=?", key, ts, nid, id)
	if err := checkListWrite(result, err, 1); err != nil {
		return err
	}
	if table == "policies" {
		for _, parent := range []string{"client_groups", "ip_ranges"} {
			if err := updateMatchingRows(tx, parent, "updated_at=?,node_id=?", "policy_id=?", []any{ts, nid}, id); err != nil {
				return err
			}
		}
	}
	// Every changed natural relationship must outrank the old-key tombstone.
	for _, r := range localRelationships {
		column := ""
		if r.leftTable == table {
			column = r.leftColumn
		}
		if r.rightTable == table {
			column = r.rightColumn
		}
		if column == "" {
			continue
		}
		if err := updateMatchingRows(tx, r.table, "updated_at=?,node_id=?", column+"=?", []any{ts, nid}, id); err != nil {
			return err
		}
	}
	return nil
}

func execPolicyWrite(query string, args ...any) (sql.Result, error) {
	var result sql.Result
	err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		var err error
		result, err = tx.Exec(query, args...)
		return checkListWrite(result, err, 1)
	})
	return result, err
}

func updateMatchingRows(tx *sql.Tx, table, sets, where string, values []any, args ...any) error {
	var count int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+where, args...).Scan(&count); err != nil {
		return err
	}
	// #nosec G202 G701 -- SQL fragments come only from localEntities/localRelationships and literal caller clauses; all request values are bound.
	result, err := tx.Exec("UPDATE "+table+" SET "+sets+" WHERE "+where, append(values, args...)...)
	return checkListWrite(result, err, count)
}

func localNaturalPart(tx *sql.Tx, table string, value any) (string, error) {
	if table == "" {
		return fmt.Sprint(value), nil
	}
	spec := localEntities[table]
	var key string
	err := tx.QueryRow("SELECT "+spec.key+" FROM "+table+" WHERE "+spec.id+"=?", value).Scan(&key)
	return key, err
}
