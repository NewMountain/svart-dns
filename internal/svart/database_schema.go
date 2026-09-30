package svart

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// Schema creation and migration share one transaction: a rejected legacy state
// must not gain partial tables, defaults or rewritten relationships on startup.
func initializeSchema() error {
	return schemaTransaction(func(tx *sql.Tx) error {
		if err := createTablesTx(tx); err != nil {
			return err
		}
		if err := migrateSchemaTx(tx); err != nil {
			return err
		}
		if err := initListWriterCapability(tx); err != nil {
			return err
		}
		return initLocalListGenerations(tx)
	})
}

func migrateSchema() error { return schemaTransaction(migrateSchemaTx) }

func schemaTransaction(apply func(*sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer rollbackTransaction(tx)
	if err := apply(tx); err != nil {
		return fmt.Errorf("schema migration refused; existing data retained; back up the database and reconcile the reported schema or relationship before restarting: %w", migrationDiagnostic(err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration; existing data retained: %w", migrationDiagnostic(err))
	}
	return nil
}

func migrationColumnExists(tx *sql.Tx, table, column string) (bool, error) {
	var tableExists, columnExists bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)", table).Scan(&tableExists); err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	if !tableExists {
		return false, fmt.Errorf("required table %s is missing or is not a table", table)
	}
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=?)", table, column).Scan(&columnExists); err != nil {
		return false, fmt.Errorf("inspect column %s.%s: %w", table, column, err)
	}
	return columnExists, nil
}

func migrationAddColumn(tx *sql.Tx, table, column, definition string) error {
	exists, err := migrationColumnExists(tx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := tx.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

// RAISE(IGNORE) is successful SQL but an incomplete migration. Count matched
// rows before the write, then require that SQLite actually updated them all.
func migrationUpdate(tx *sql.Tx, statement string, args ...any) error {
	table := strings.Fields(statement)[1]
	predicate := statement[strings.LastIndex(statement, " WHERE ")+7:]
	var expected int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE " + predicate).Scan(&expected); err != nil {
		return fmt.Errorf("inspect backfill %s: %w", table, err)
	}
	result, err := tx.Exec(statement, args...)
	if err != nil {
		return fmt.Errorf("backfill %s: %w", table, err)
	}
	actual, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify backfill %s: %w", table, err)
	}
	if actual != expected {
		return fmt.Errorf("incomplete backfill %s: updated %d of %d rows; inspect migration triggers", table, actual, expected)
	}
	return nil
}

func migrateSchemaTx(tx *sql.Tx) error {
	urlExists, err := migrationColumnExists(tx, "blocklists", "url")
	if err != nil {
		return err
	}
	if !urlExists {
		patternExists, err := migrationColumnExists(tx, "blocklists", "pattern")
		if err != nil {
			return err
		}
		if !patternExists {
			return errors.New("legacy blocklists has neither url nor pattern; restore the missing schema before retrying")
		}
		// Additive upgrade retains every old field, integer ID and dependent row.
		if _, err := tx.Exec("ALTER TABLE blocklists RENAME COLUMN pattern TO url"); err != nil {
			return fmt.Errorf("rename legacy blocklist pattern: %w", err)
		}
		aliasExists, err := migrationColumnExists(tx, "blocklists", "alias")
		if err != nil {
			return err
		}
		if !aliasExists {
			if err := migrationAddColumn(tx, "blocklists", "alias", "TEXT NOT NULL DEFAULT ''"); err != nil {
				return err
			}
			if err := migrationUpdate(tx, "UPDATE blocklists SET alias=url WHERE 1=1"); err != nil {
				return err
			}
		}
		if err := migrationAddColumn(tx, "blocklists", "domain_count", "INTEGER DEFAULT 0"); err != nil {
			return err
		}
		if err := migrationAddColumn(tx, "blocklists", "last_updated", "DATETIME"); err != nil {
			return err
		}
	}
	hasLatencyMs, err := migrationColumnExists(tx, "query_logs", "latency_ms")
	if err != nil {
		return err
	}
	if hasLatencyMs {
		if _, err := tx.Exec("ALTER TABLE query_logs RENAME COLUMN latency_ms TO latency_microseconds"); err != nil {
			return fmt.Errorf("rename legacy latency column: %w", err)
		}
	}
	columns := []struct {
		table  string
		column string
		def    string
	}{
		// Tables needing both updated_at and node_id
		{"upstreams", "updated_at", "DATETIME DEFAULT ''"},
		{"upstreams", "node_id", "TEXT DEFAULT ''"},
		{"blocklists", "updated_at", "DATETIME DEFAULT ''"},
		{"blocklists", "node_id", "TEXT DEFAULT ''"},
		{"allowlists", "updated_at", "DATETIME DEFAULT ''"},
		{"allowlists", "node_id", "TEXT DEFAULT ''"},
		{"rewrites", "updated_at", "DATETIME DEFAULT ''"},
		{"rewrites", "node_id", "TEXT DEFAULT ''"},
		{"client_groups", "updated_at", "DATETIME DEFAULT ''"},
		{"client_groups", "node_id", "TEXT DEFAULT ''"},
		{"ip_ranges", "updated_at", "DATETIME DEFAULT ''"},
		{"ip_ranges", "node_id", "TEXT DEFAULT ''"},
		{"bootstrap_servers", "updated_at", "DATETIME DEFAULT ''"},
		{"bootstrap_servers", "node_id", "TEXT DEFAULT ''"},
		{"api_tokens", "token", "TEXT NOT NULL DEFAULT ''"},
		{"api_tokens", "updated_at", "DATETIME DEFAULT ''"},
		{"api_tokens", "node_id", "TEXT DEFAULT ''"},
		{"client_blocklists", "updated_at", "DATETIME DEFAULT ''"},
		{"client_blocklists", "node_id", "TEXT DEFAULT ''"},
		{"client_allowlists", "updated_at", "DATETIME DEFAULT ''"},
		{"client_allowlists", "node_id", "TEXT DEFAULT ''"},
		{"group_blocklists", "updated_at", "DATETIME DEFAULT ''"},
		{"group_blocklists", "node_id", "TEXT DEFAULT ''"},
		{"group_allowlists", "updated_at", "DATETIME DEFAULT ''"},
		{"group_allowlists", "node_id", "TEXT DEFAULT ''"},
		{"range_blocklists", "updated_at", "DATETIME DEFAULT ''"},
		{"range_blocklists", "node_id", "TEXT DEFAULT ''"},
		{"range_allowlists", "updated_at", "DATETIME DEFAULT ''"},
		{"range_allowlists", "node_id", "TEXT DEFAULT ''"},
		{"client_group_members", "updated_at", "DATETIME DEFAULT ''"},
		{"client_group_members", "node_id", "TEXT DEFAULT ''"},
		// Tables that already have updated_at, only need node_id
		{"settings", "node_id", "TEXT DEFAULT ''"},
		{"client_aliases", "node_id", "TEXT DEFAULT ''"},
		// blocked_domains and allowed_domains for manual domain sync
		{"blocked_domains", "updated_at", "DATETIME DEFAULT ''"},
		{"blocked_domains", "node_id", "TEXT DEFAULT ''"},
		{"allowed_domains", "updated_at", "DATETIME DEFAULT ''"},
		{"allowed_domains", "node_id", "TEXT DEFAULT ''"},
		{"query_logs", "block_tier", "TEXT"},
		{"query_logs", "block_rule", "TEXT"},
		{"rewrites", "ip_addresses", "TEXT"},
		{"query_logs", "block_source", "TEXT"},
		{"query_logs", "block_list_id", "INTEGER"},
		{"query_logs", "block_list_name", "TEXT"},
		{"ip_ranges", "policy_id", "INTEGER DEFAULT NULL"},
		{"client_groups", "policy_id", "INTEGER DEFAULT NULL"},
		{"blocklists", "refresh_interval", "INTEGER DEFAULT 604800"},
		{"allowlists", "refresh_interval", "INTEGER DEFAULT 604800"},
		{"query_logs", "coalesced_count", "INTEGER DEFAULT 1"},
		{"query_logs", "result", "TEXT DEFAULT ''"},
		{"query_logs", "result_reason", "TEXT DEFAULT ''"},
		{"query_logs", "client_name", "TEXT DEFAULT ''"},
		{"query_logs", "policy_json", "TEXT DEFAULT ''"},
		{"query_logs", "result_tier", "TEXT DEFAULT ''"},
		{"query_logs", "result_entity", "TEXT DEFAULT ''"},
		{"query_logs", "result_is_published", "BOOLEAN DEFAULT 0"},
		{"query_logs", "result_rule", "TEXT DEFAULT ''"},
		{"query_logs", "result_list_id", "INTEGER DEFAULT 0"},
		{"query_logs", "result_list_name", "TEXT DEFAULT ''"},
		{"query_logs", "range_result", "TEXT DEFAULT ''"},
		{"query_logs", "range_entity", "TEXT DEFAULT ''"},
		{"query_logs", "range_is_published", "BOOLEAN DEFAULT 0"},
		{"query_logs", "range_rule", "TEXT DEFAULT ''"},
		{"query_logs", "range_list_id", "INTEGER DEFAULT 0"},
		{"query_logs", "range_list_name", "TEXT DEFAULT ''"},
		{"query_logs", "group_result", "TEXT DEFAULT ''"},
		{"query_logs", "group_entity", "TEXT DEFAULT ''"},
		{"query_logs", "group_is_published", "BOOLEAN DEFAULT 0"},
		{"query_logs", "group_rule", "TEXT DEFAULT ''"},
		{"query_logs", "group_list_id", "INTEGER DEFAULT 0"},
		{"query_logs", "group_list_name", "TEXT DEFAULT ''"},
		{"query_logs", "ip_result", "TEXT DEFAULT ''"},
		{"query_logs", "ip_entity", "TEXT DEFAULT ''"},
		{"query_logs", "ip_is_published", "BOOLEAN DEFAULT 0"},
		{"query_logs", "ip_rule", "TEXT DEFAULT ''"},
		{"query_logs", "ip_list_id", "INTEGER DEFAULT 0"},
		{"query_logs", "ip_list_name", "TEXT DEFAULT ''"},
	}
	for _, column := range columns {
		if err := migrationAddColumn(tx, column.table, column.column, column.def); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS sync_tombstones (
			table_name TEXT NOT NULL,
			natural_key TEXT NOT NULL,
			deleted_at DATETIME NOT NULL,
			node_id TEXT NOT NULL,
			PRIMARY KEY (table_name, natural_key)
		)
	`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS blocklist_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			blocklist_id INTEGER NOT NULL,
			refreshed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			previous_count INTEGER DEFAULT 0,
			new_count INTEGER DEFAULT 0,
			added_count INTEGER DEFAULT 0,
			removed_count INTEGER DEFAULT 0,
			sample_added TEXT DEFAULT '',
			sample_removed TEXT DEFAULT ''
		)
	`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS blocklist_changelog (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_id INTEGER NOT NULL,
			domain TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('added', 'removed')),
			FOREIGN KEY (history_id) REFERENCES blocklist_history(id) ON DELETE CASCADE
		)
	`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS admin_users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'admin' CHECK(role IN ('readonly', 'admin')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT '',
			node_id TEXT DEFAULT ''
		)
	`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_query_logs_result ON query_logs(result)`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_query_logs_result_reason ON query_logs(result_reason)`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_blocklist_history_list ON blocklist_history(blocklist_id)`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_blocklist_changelog_history ON blocklist_changelog(history_id)`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_blocklist_changelog_domain ON blocklist_changelog(domain)`); err != nil {
		return fmt.Errorf("create migration table or index: %w", err)
	}
	naturalKeys := []struct {
		table, column, index string
	}{
		{"bootstrap_servers", "server", "idx_bootstrap_servers_unique"},
		{"upstreams", "upstream", "idx_upstreams_unique"},
		{"blocklists", "alias", "idx_blocklists_alias_unique"},
		{"allowlists", "alias", "idx_allowlists_alias_unique"},
		{"rewrites", "domain", "idx_rewrites_domain_unique"},
		{"api_tokens", "name", "idx_api_tokens_name_unique"},
	}
	for _, nk := range naturalKeys {
		var duplicates int
		if err := tx.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM (SELECT %s FROM %s GROUP BY %s HAVING COUNT(*)>1)", nk.column, nk.table, nk.column)).Scan(&duplicates); err != nil {
			return fmt.Errorf("inspect natural key %s.%s: %w", nk.table, nk.column, err)
		}
		if duplicates > 0 {
			return fmt.Errorf("%s.%s has %d conflicting natural keys; reconcile aliases or identities explicitly while retaining every record and relationship", nk.table, nk.column, duplicates)
		}
		if _, err := tx.Exec(fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s(%s)", nk.index, nk.table, nk.column)); err != nil {
			return fmt.Errorf("create %s natural-key index: %w", nk.table, err)
		}

		var valid bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pragma_index_list(?) WHERE name=? AND "unique"=1 AND partial=0) AND (SELECT COUNT(*) FROM pragma_index_info(?))=1 AND EXISTS(SELECT 1 FROM pragma_index_info(?) WHERE name=?)`, nk.table, nk.index, nk.index, nk.index, nk.column).Scan(&valid); err != nil {
			return fmt.Errorf("verify %s natural-key index: %w", nk.table, err)
		}
		if !valid {
			return fmt.Errorf("index %s does not uniquely enforce %s.%s; reconcile the index definition", nk.index, nk.table, nk.column)
		}
	}
	for _, query := range []string{"SELECT table_name,natural_key,deleted_at,node_id FROM sync_tombstones LIMIT 0",
		"SELECT id,blocklist_id,refreshed_at,previous_count,new_count,added_count,removed_count,sample_added,sample_removed FROM blocklist_history LIMIT 0",
		"SELECT id,history_id,domain,action FROM blocklist_changelog LIMIT 0",
		"SELECT id,upstream,enabled,created_at FROM upstreams LIMIT 0",
		"SELECT id,domain,target,ip_addresses,enabled,created_at FROM rewrites LIMIT 0",
		"SELECT key,value,updated_at FROM settings LIMIT 0",
		"SELECT id,server,created_at FROM bootstrap_servers LIMIT 0",
		"SELECT ip_address,alias,updated_at FROM client_aliases LIMIT 0",
		"SELECT id,name,created_at FROM client_groups LIMIT 0",
		"SELECT client_ip,group_id FROM client_group_members LIMIT 0",
		"SELECT id,url,alias,enabled,domain_count,last_updated,created_at FROM blocklists LIMIT 0",
		"SELECT id,blocklist_id,domain FROM blocked_domains LIMIT 0",
		"SELECT client_ip,blocklist_id FROM client_blocklists LIMIT 0",
		"SELECT group_id,blocklist_id FROM group_blocklists LIMIT 0",
		"SELECT id,url,alias,enabled,domain_count,last_updated,created_at FROM allowlists LIMIT 0",
		"SELECT id,allowlist_id,domain FROM allowed_domains LIMIT 0",
		"SELECT client_ip,allowlist_id FROM client_allowlists LIMIT 0",
		"SELECT group_id,allowlist_id FROM group_allowlists LIMIT 0",
		"SELECT id,name,cidr,created_at FROM ip_ranges LIMIT 0",
		"SELECT range_id,blocklist_id FROM range_blocklists LIMIT 0",
		"SELECT range_id,allowlist_id FROM range_allowlists LIMIT 0",
		"SELECT id,timestamp,client_ip,query_name,query_type,response_code,blocked,upstream,latency_microseconds FROM query_logs LIMIT 0",
		"SELECT id,name,token_prefix,token_hash,token,role,created_at,last_used_at FROM api_tokens LIMIT 0",
		"SELECT id,username,password_hash,role,created_at,updated_at,node_id FROM admin_users LIMIT 0",
		"SELECT id_hash,username,credential_fp,created_at,last_seen_at,expires_at FROM sessions LIMIT 0",
		"SELECT id,name,description,created_at,updated_at,node_id FROM policies LIMIT 0",
		"SELECT policy_id,blocklist_id,updated_at,node_id FROM policy_blocklists LIMIT 0",
		"SELECT policy_id,allowlist_id,updated_at,node_id FROM policy_allowlists LIMIT 0",
		"SELECT client_ip,policy_id,updated_at,node_id FROM client_policies LIMIT 0"} {
		rows, err := tx.Query(query)
		if err != nil {
			return fmt.Errorf("verify required schema: %w", err)
		}
		if err := rows.Err(); err != nil {
			closeQueryRows(rows)
			return fmt.Errorf("inspect required schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close schema inspection: %w", err)
		}
	}
	if err := migrationCheckForeignKeys(tx); err != nil {
		return err
	}
	junctionBackfills := []string{
		`UPDATE range_blocklists SET updated_at = (SELECT COALESCE(NULLIF(ir.updated_at, ''), datetime('now')) FROM ip_ranges ir WHERE ir.id = range_blocklists.range_id), node_id = COALESCE(NULLIF(node_id, ''), (SELECT ir.node_id FROM ip_ranges ir WHERE ir.id = range_blocklists.range_id), '') WHERE updated_at = '' OR updated_at IS NULL`,
		`UPDATE range_allowlists SET updated_at = (SELECT COALESCE(NULLIF(ir.updated_at, ''), datetime('now')) FROM ip_ranges ir WHERE ir.id = range_allowlists.range_id), node_id = COALESCE(NULLIF(node_id, ''), (SELECT ir.node_id FROM ip_ranges ir WHERE ir.id = range_allowlists.range_id), '') WHERE updated_at = '' OR updated_at IS NULL`,
		`UPDATE group_blocklists SET updated_at = (SELECT COALESCE(NULLIF(cg.updated_at, ''), datetime('now')) FROM client_groups cg WHERE cg.id = group_blocklists.group_id), node_id = COALESCE(NULLIF(node_id, ''), (SELECT cg.node_id FROM client_groups cg WHERE cg.id = group_blocklists.group_id), '') WHERE updated_at = '' OR updated_at IS NULL`,
		`UPDATE group_allowlists SET updated_at = (SELECT COALESCE(NULLIF(cg.updated_at, ''), datetime('now')) FROM client_groups cg WHERE cg.id = group_allowlists.group_id), node_id = COALESCE(NULLIF(node_id, ''), (SELECT cg.node_id FROM client_groups cg WHERE cg.id = group_allowlists.group_id), '') WHERE updated_at = '' OR updated_at IS NULL`,
		`UPDATE client_blocklists SET updated_at = datetime('now'), node_id = COALESCE(node_id, '') WHERE updated_at = '' OR updated_at IS NULL`,
		`UPDATE client_allowlists SET updated_at = datetime('now'), node_id = COALESCE(node_id, '') WHERE updated_at = '' OR updated_at IS NULL`,
	}
	for _, statement := range junctionBackfills {
		if err := migrationUpdate(tx, statement); err != nil {
			return err
		}
	}
	ts, nid := syncNow()
	if err := migrationUpdate(tx, "UPDATE api_tokens SET token = '', updated_at = ?, node_id = ? WHERE COALESCE(token, '') <> ''", ts, nid); err != nil {
		return err
	}
	return nil
}

func migrationCheckForeignKeys(tx *sql.Tx) error {
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("inspect legacy relationships: %w", err)
	}
	defer closeQueryRows(rows)
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var constraint int
		if err := rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			return fmt.Errorf("read legacy relationship: %w", err)
		}
		return fmt.Errorf("orphan relationship in %s rowid %d referencing %s; recover the missing parent or reconcile the relationship explicitly", table, rowID.Int64, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate legacy relationships: %w", err)
	}
	return rows.Close()
}

func createTablesTx(tx *sql.Tx) error {
	schema := `
	CREATE TABLE IF NOT EXISTS upstreams (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		upstream TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS rewrites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		domain TEXT NOT NULL,
		target TEXT NOT NULL,
		ip_addresses TEXT,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bootstrap_servers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_aliases (
		ip_address TEXT PRIMARY KEY,
		alias TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_groups (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS client_group_members (
		client_ip TEXT NOT NULL,
		group_id INTEGER NOT NULL,
		PRIMARY KEY (client_ip, group_id),
		FOREIGN KEY (group_id) REFERENCES client_groups(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS blocklists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		alias TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		domain_count INTEGER DEFAULT 0,
		last_updated DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS blocked_domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		blocklist_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		FOREIGN KEY (blocklist_id) REFERENCES blocklists(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS client_blocklists (
		client_ip TEXT NOT NULL,
		blocklist_id INTEGER NOT NULL,
		PRIMARY KEY (client_ip, blocklist_id),
		FOREIGN KEY (blocklist_id) REFERENCES blocklists(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS group_blocklists (
		group_id INTEGER NOT NULL,
		blocklist_id INTEGER NOT NULL,
		PRIMARY KEY (group_id, blocklist_id),
		FOREIGN KEY (group_id) REFERENCES client_groups(id) ON DELETE CASCADE,
		FOREIGN KEY (blocklist_id) REFERENCES blocklists(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_blocked_domains_domain ON blocked_domains(domain);
	CREATE INDEX IF NOT EXISTS idx_blocked_domains_blocklist ON blocked_domains(blocklist_id);

	CREATE TABLE IF NOT EXISTS allowlists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL DEFAULT '',
		alias TEXT NOT NULL,
		enabled BOOLEAN NOT NULL DEFAULT 1,
		domain_count INTEGER DEFAULT 0,
		last_updated DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS allowed_domains (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		allowlist_id INTEGER NOT NULL,
		domain TEXT NOT NULL,
		FOREIGN KEY (allowlist_id) REFERENCES allowlists(id) ON DELETE CASCADE
	);
	CREATE INDEX IF NOT EXISTS idx_allowed_domains_domain ON allowed_domains(domain);
	CREATE INDEX IF NOT EXISTS idx_allowed_domains_allowlist ON allowed_domains(allowlist_id);
	CREATE TABLE IF NOT EXISTS client_allowlists (
		client_ip TEXT NOT NULL,
		allowlist_id INTEGER NOT NULL,
		PRIMARY KEY (client_ip, allowlist_id),
		FOREIGN KEY (allowlist_id) REFERENCES allowlists(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS group_allowlists (
		group_id INTEGER NOT NULL,
		allowlist_id INTEGER NOT NULL,
		PRIMARY KEY (group_id, allowlist_id),
		FOREIGN KEY (group_id) REFERENCES client_groups(id) ON DELETE CASCADE,
		FOREIGN KEY (allowlist_id) REFERENCES allowlists(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS ip_ranges (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		cidr TEXT NOT NULL UNIQUE,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS range_blocklists (
		range_id INTEGER NOT NULL,
		blocklist_id INTEGER NOT NULL,
		PRIMARY KEY (range_id, blocklist_id),
		FOREIGN KEY (range_id) REFERENCES ip_ranges(id) ON DELETE CASCADE,
		FOREIGN KEY (blocklist_id) REFERENCES blocklists(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS range_allowlists (
		range_id INTEGER NOT NULL,
		allowlist_id INTEGER NOT NULL,
		PRIMARY KEY (range_id, allowlist_id),
		FOREIGN KEY (range_id) REFERENCES ip_ranges(id) ON DELETE CASCADE,
		FOREIGN KEY (allowlist_id) REFERENCES allowlists(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS query_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		client_ip TEXT NOT NULL,
		query_name TEXT NOT NULL,
		query_type TEXT NOT NULL,
		response_code TEXT,
		blocked BOOLEAN DEFAULT 0,
		upstream TEXT,
		latency_microseconds INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_query_logs_timestamp ON query_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_query_logs_client_ip ON query_logs(client_ip);
	CREATE INDEX IF NOT EXISTS idx_query_logs_query_name ON query_logs(query_name);
	CREATE INDEX IF NOT EXISTS idx_query_logs_blocked ON query_logs(blocked);

	INSERT INTO settings (key, value, updated_at) VALUES ('log_retention_days', '1095', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('server_version', '1.0.0', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('strategy', 'weighted', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('cache_ttl', '3600', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('denied_ttl', '3600', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('bootstrap_ttl', '3600', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('timezone', 'UTC', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('logging_enabled', 'true', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('node_name', '', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('sync_peers', '', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('sync_secret', '', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('sync_interval', '', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('deleted_peers', '', '') ON CONFLICT(key) DO NOTHING;
	INSERT INTO settings (key, value, updated_at) VALUES ('session_secret', '', '') ON CONFLICT(key) DO NOTHING;

	INSERT INTO bootstrap_servers (id, server) SELECT 1, '9.9.9.9:53' WHERE NOT EXISTS (SELECT 1 FROM bootstrap_servers WHERE id=1 OR server='9.9.9.9:53');

	CREATE TABLE IF NOT EXISTS api_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		token_prefix TEXT NOT NULL,
		token_hash TEXT NOT NULL,
		token TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'readonly' CHECK(role IN ('readonly', 'admin')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS admin_users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'admin' CHECK(role IN ('readonly', 'admin')),
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT '',
		node_id TEXT DEFAULT ''
	);

	-- Browser sessions (session.go). Node-local: sync.go never replicates this
	-- table. id_hash is the SHA-256 of the cookie token, never the token.
	CREATE TABLE IF NOT EXISTS sessions (
		id_hash TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		credential_fp TEXT NOT NULL,
		created_at TEXT NOT NULL,
		last_seen_at TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);

	CREATE TABLE IF NOT EXISTS policies (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT '',
		node_id TEXT DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS policy_blocklists (
		policy_id INTEGER NOT NULL,
		blocklist_id INTEGER NOT NULL,
		updated_at DATETIME DEFAULT '',
		node_id TEXT DEFAULT '',
		PRIMARY KEY (policy_id, blocklist_id),
		FOREIGN KEY (policy_id) REFERENCES policies(id) ON DELETE CASCADE,
		FOREIGN KEY (blocklist_id) REFERENCES blocklists(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS policy_allowlists (
		policy_id INTEGER NOT NULL,
		allowlist_id INTEGER NOT NULL,
		updated_at DATETIME DEFAULT '',
		node_id TEXT DEFAULT '',
		PRIMARY KEY (policy_id, allowlist_id),
		FOREIGN KEY (policy_id) REFERENCES policies(id) ON DELETE CASCADE,
		FOREIGN KEY (allowlist_id) REFERENCES allowlists(id) ON DELETE CASCADE
	);
	CREATE TABLE IF NOT EXISTS client_policies (
		client_ip TEXT PRIMARY KEY,
		policy_id INTEGER,
		updated_at DATETIME DEFAULT '',
		node_id TEXT DEFAULT '',
		FOREIGN KEY (policy_id) REFERENCES policies(id) ON DELETE SET NULL
	);
	`

	if _, err := tx.Exec(schema); err != nil {
		return fmt.Errorf("initialize schema: %w", err)
	}
	for _, key := range []string{"log_retention_days", "server_version", "strategy", "cache_ttl", "denied_ttl", "bootstrap_ttl", "timezone", "logging_enabled", "node_name", "sync_peers", "sync_secret", "sync_interval", "deleted_peers", "session_secret"} {
		var exists bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM settings WHERE key=?)", key).Scan(&exists); err != nil {
			return fmt.Errorf("verify default setting %s: %w", key, err)
		}
		if !exists {
			return fmt.Errorf("default setting %s was not inserted; inspect settings triggers", key)
		}
	}
	var exists bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM bootstrap_servers WHERE id=1 OR server='9.9.9.9:53')").Scan(&exists); err != nil {
		return fmt.Errorf("verify bootstrap initialization: %w", err)
	}
	if !exists {
		return errors.New("bootstrap server was not inserted; inspect bootstrap_servers triggers")
	}
	return nil
}

// SQLite trigger messages can contain stored credentials. Retain actionable
// engine codes without copying user-controlled text into startup logs.
func migrationDiagnostic(err error) error {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return fmt.Errorf("%sSQLite migration failure (code %d, extended code %d); inspect schema constraints and triggers", strings.TrimSuffix(err.Error(), sqliteErr.Error()), sqliteErr.Code, sqliteErr.ExtendedCode)
	}
	return err
}

// Download state belongs to this node and never enters sync payloads.
func initLocalListGenerations(tx *sql.Tx) error {
	for _, store := range []listStore{blockListStore, allowListStore} {
		table := "local_" + store.kind + "_generations"
		if _, err := tx.Exec("CREATE TABLE IF NOT EXISTS " + table + " (list_id INTEGER PRIMARY KEY REFERENCES " + store.lists + "(id) ON DELETE CASCADE, url TEXT NOT NULL)"); err != nil {
			return fmt.Errorf("initialize %s: %w", table, err)
		}
		if err := migrationAddColumn(tx, table, "compatibility_report", "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
		for _, column := range []string{"list_id", "url", "compatibility_report"} {
			exists, err := migrationColumnExists(tx, table, column)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("required column %s.%s is missing", table, column)
			}
		}
		// Older binaries do not know about compatibility reports. Every refresh
		// updates last_updated (even in the same clock tick), so invalidate the
		// old assessment here. New writers store the replacement report later
		// in the same transaction, after replacing rules and refresh metadata.
		if _, err := tx.Exec("CREATE TRIGGER IF NOT EXISTS invalidate_" + store.kind + "_compatibility AFTER UPDATE OF last_updated ON " + store.lists + " BEGIN UPDATE " + table + " SET compatibility_report='' WHERE list_id=NEW.id AND compatibility_report<>''; END"); err != nil {
			return fmt.Errorf("initialize %s compatibility invalidation: %w", store.kind, err)
		}
	}
	return nil
}
