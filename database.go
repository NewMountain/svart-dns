package main

import (
	"database/sql"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func initDatabase(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(1)

	if err := createTables(); err != nil {
		return err
	}

	if err := migrateSchema(); err != nil {
		return err
	}

	go startLogRetentionWorker()

	return nil
}

func createTables() error {
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

	CREATE TABLE IF NOT EXISTS query_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		client_ip TEXT NOT NULL,
		query_name TEXT NOT NULL,
		query_type TEXT NOT NULL,
		response_code TEXT,
		blocked BOOLEAN DEFAULT 0,
		upstream TEXT,
		latency_ms INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_query_logs_timestamp ON query_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_query_logs_client_ip ON query_logs(client_ip);
	CREATE INDEX IF NOT EXISTS idx_query_logs_query_name ON query_logs(query_name);
	CREATE INDEX IF NOT EXISTS idx_query_logs_blocked ON query_logs(blocked);

	INSERT OR IGNORE INTO settings (key, value) VALUES ('log_retention_days', '7');
	INSERT OR IGNORE INTO settings (key, value) VALUES ('server_version', '1.0.0');
	INSERT OR IGNORE INTO settings (key, value) VALUES ('strategy', 'load-balance');
	INSERT OR IGNORE INTO settings (key, value) VALUES ('cache_ttl', '3600');
	INSERT OR IGNORE INTO settings (key, value) VALUES ('bootstrap_ttl', '3600');
	
	INSERT OR IGNORE INTO bootstrap_servers (id, server) VALUES (1, '9.9.9.9:53');
	`

	_, err := db.Exec(schema)
	return err
}

func startLogRetentionWorker() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		cleanupOldLogs()
	}
}

func cleanupOldLogs() {
	var retentionDays int
	err := db.QueryRow("SELECT value FROM settings WHERE key = 'log_retention_days'").Scan(&retentionDays)
	if err != nil {
		log.Printf("Error reading log retention setting: %v", err)
		retentionDays = 7
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := db.Exec("DELETE FROM query_logs WHERE timestamp < ?", cutoff)
	if err != nil {
		log.Printf("Error cleaning up old logs: %v", err)
		return
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("Cleaned up %d old query logs (retention: %d days)", rows, retentionDays)
	}
}

func migrateSchema() error {
	var columnExists bool
	row := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('blocklists') WHERE name='url'")
	row.Scan(&columnExists)

	if !columnExists {
		log.Println("Migrating blocklists table schema...")

		_, err := db.Exec(`
			ALTER TABLE blocklists RENAME TO blocklists_old;
			
			CREATE TABLE blocklists (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				url TEXT NOT NULL,
				alias TEXT NOT NULL,
				enabled BOOLEAN NOT NULL DEFAULT 1,
				domain_count INTEGER DEFAULT 0,
				last_updated DATETIME,
				created_at DATETIME DEFAULT CURRENT_TIMESTAMP
			);
			
			INSERT INTO blocklists (id, url, alias, enabled, created_at)
			SELECT id, pattern, pattern, enabled, created_at FROM blocklists_old;
			
			DROP TABLE blocklists_old;
		`)

		if err != nil {
			return err
		}

		log.Println("Blocklists table migration complete")
	}

	var ipAddressesExists bool
	row = db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('rewrites') WHERE name='ip_addresses'")
	row.Scan(&ipAddressesExists)

	if !ipAddressesExists {
		log.Println("Adding ip_addresses column to rewrites table...")
		_, err := db.Exec("ALTER TABLE rewrites ADD COLUMN ip_addresses TEXT")
		if err != nil {
			return err
		}
		log.Println("Rewrites table migration complete")
	}

	return nil
}

func closeDatabase() error {
	if db != nil {
		return db.Close()
	}
	return nil
}
