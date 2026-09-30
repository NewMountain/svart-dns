package svart

import (
	"database/sql"
	"errors"
	"sync/atomic"
	"time"

	// Register the SQLite driver used by database/sql.
	_ "github.com/mattn/go-sqlite3"
)

// clientAliasCache holds an ip → alias map for fast denormalization in the log writer.
var clientAliasCache atomic.Value // map[string]string

func init() {
	clientAliasCache.Store(make(map[string]string))
}

// loadClientAliasCache reads all client_aliases into the in-memory cache.
func readClientAliases(q queryer) (map[string]string, error) {
	aliases := make(map[string]string)
	err := scanRows(q, "SELECT ip_address, alias FROM client_aliases", func(rows *sql.Rows) error {
		var ip, alias string
		if err := rows.Scan(&ip, &alias); err != nil {
			return err
		}
		aliases[ip] = alias
		return nil
	})
	return aliases, err
}
func loadClientAliasCache() error {
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	aliases, err := readClientAliases(db)
	if err != nil {
		return err
	}
	clientAliasCache.Store(aliases)
	return nil
}

// getClientAliasCached returns the alias for a client IP from the in-memory cache.
func getClientAliasCached(ip string) string {
	return internalValue[map[string]string](clientAliasCache.Load())[ip]
}

var db *sql.DB
var readDB *sql.DB

func initDatabase(dbPath string) (initErr error) {
	var err error
	defer func() {
		if initErr != nil {
			if readDB != nil {
				initErr = errors.Join(initErr, readDB.Close())
				readDB = nil
			}
			if db != nil {
				initErr = errors.Join(initErr, db.Close())
				db = nil
			}
		}
	}()
	// Reserve the write transaction before taking any read snapshot. In WAL mode,
	// a separate query-log writer can otherwise advance the database between a
	// mutation's SELECT and first write, making snapshot promotion fail immediately
	// with SQLITE_BUSY even though busy_timeout is configured. readDB remains
	// deferred/read-only so historical reads do not reserve the writer.
	db, err = sql.Open("sqlite3", sqliteFileDSN(dbPath, "_journal_mode=WAL&_txlock=immediate&_synchronous=NORMAL&_busy_timeout=5000&_mmap_size=268435456&_cache_size=-64000&_foreign_keys=ON"))
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(1)

	readDB, err = sql.Open("sqlite3", sqliteFileDSN(dbPath, "_journal_mode=WAL&_busy_timeout=5000&_mmap_size=268435456&_cache_size=-64000&mode=ro"))
	if err != nil {
		return err
	}
	readDB.SetMaxOpenConns(4)

	if err := initializeSchema(); err != nil {
		return err
	}

	if err := loadRuntimeSettingsFromDB(); err != nil {
		return err
	}

	if err := initLogWriter(dbPath); err != nil {
		return err
	}

	go startLogRetentionWorker()

	return nil
}

func startLogRetentionWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cleanupOldLogs()
	}
}

func cleanupOldLogs() {
	summaryArchiveMu.Lock()
	defer summaryArchiveMu.Unlock()
	pending, err := summaryPending(db)
	if err != nil || pending {
		logDB.Error("skipping hot retention while summary ownership needs recovery", "error", err)
		return
	}
	retentionDays, err := readLogRetentionDays(db)
	if err != nil {
		logDB.Error("skipping log retention cleanup", "error", err)
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := db.Exec("DELETE FROM query_logs WHERE timestamp < ?", cutoff)
	if err != nil {
		logDB.Error("failed to clean up old logs", "error", err)
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		logDB.Error("read retention deleted row count", "error", err)
	}
	if rows > 0 {
		logDB.Info("cleaned up old query logs", "rows", rows, "retention_days", retentionDays)
	}

	// WAL checkpoint — reclaim space after cleanup
	if _, err := db.Exec("PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
		logDB.Error("WAL checkpoint failed", "error", err)
	}
}

// nodeID identifies this instance for sync. Set from NODE_ID env var or hostname.
var nodeID string

// syncNow returns the current UTC time as RFC3339Nano and the nodeID.
func syncNow() (string, string) {
	return time.Now().UTC().Format(time.RFC3339Nano), nodeID
}

// recordTombstone records a deletion for sync propagation.
func recordTombstone(tableName, naturalKey string) error {
	ts, nid := syncNow()
	result, err := db.Exec(`
		INSERT OR REPLACE INTO sync_tombstones (table_name, natural_key, deleted_at, node_id)
		VALUES (?, ?, ?, ?)
	`, tableName, naturalKey, ts, nid)
	return checkListWrite(result, err, 1)
}

func closeDatabase() (resultErr error) {
	closeArchiver()
	closeLogWriter()
	if readDB != nil {
		resultErr = errors.Join(resultErr, readDB.Close())
	}
	if db != nil {
		return errors.Join(resultErr, db.Close())
	}
	return resultErr
}

func setClientAlias(ip, alias string) error {
	return localMutation(snapshotAliases, func(tx *sql.Tx) error {
		if alias == "" {
			return deleteEntityTx(tx, "client_aliases", ip)
		}
		ts, nid := syncNow()
		result, err := tx.Exec(`INSERT INTO client_aliases(ip_address,alias,updated_at,node_id) VALUES(?,?,?,?) ON CONFLICT(ip_address) DO UPDATE SET alias=excluded.alias,updated_at=excluded.updated_at,node_id=excluded.node_id`, ip, alias, ts, nid)
		return checkListWrite(result, err, 1)
	})
}

func addClientToGroup(ip string, groupID string) error {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", ip, groupID, ts, nid)
	return err
}

func removeClientFromGroup(ip string, groupID string) error {
	return removeLocalRelationship("client_group_members", ip, groupID)
}

func addClientBlocklist(ip string, blocklistID string) error {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO client_blocklists (client_ip, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", ip, blocklistID, ts, nid)
	return err
}

func removeClientBlocklist(ip string, blocklistID string) error {
	return removeLocalRelationship("client_blocklists", ip, blocklistID)
}

func createGroup(name string) (int64, error) {
	ts, nid := syncNow()
	result, err := execPolicyWrite("INSERT INTO client_groups (name, updated_at, node_id) VALUES (?, ?, ?)", name, ts, nid)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateGroupName(groupID int, name string) error {
	return localMutation(snapshotPolicy, func(tx *sql.Tx) error { return renameEntityTx(tx, "client_groups", groupID, name) })
}

func deleteGroup(groupID int) error {
	return deleteLocalEntity("client_groups", groupID, snapshotPolicy)
}

func addGroupBlocklist(groupID int, blocklistID int) error {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO group_blocklists (group_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", groupID, blocklistID, ts, nid)
	return err
}

func removeGroupBlocklist(groupID int, blocklistID int) error {
	return removeLocalRelationship("group_blocklists", groupID, blocklistID)
}

func createRewrite(domain, ipAddresses string, enabled bool) (int64, error) {
	var id int64
	err := localMutation(snapshotRewrites, func(tx *sql.Tx) error {
		ts, nid := syncNow()
		res, err := tx.Exec("INSERT INTO rewrites(domain,target,ip_addresses,enabled,updated_at,node_id) VALUES(?,'',?,?,?,?)", domain, ipAddresses, enabled, ts, nid)
		if err := checkListWrite(res, err, 1); err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

func updateRewriteEnabled(rewriteID int, enabled bool) error {
	_, err := updateLocalRewrite(rewriteID, UpdateRewriteRequest{Enabled: &enabled})
	return err
}

func updateRewriteDomain(rewriteID int, domain string) error {
	_, err := updateLocalRewrite(rewriteID, UpdateRewriteRequest{Domain: domain})
	return err
}

func updateRewriteIPs(rewriteID int, ipAddresses string) error {
	_, err := updateLocalRewrite(rewriteID, UpdateRewriteRequest{IPAddresses: ipAddresses})
	return err
}

func deleteRewrite(rewriteID int) error {
	return deleteLocalEntity("rewrites", rewriteID, snapshotRewrites)
}
