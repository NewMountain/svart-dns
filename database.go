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

func getDashboardStats() (totalQueries, blockedQueries, avgLatency int64) {
	var avgLatencyNull sql.NullFloat64
	db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&totalQueries)
	db.QueryRow("SELECT COUNT(*) FROM query_logs WHERE blocked = 1").Scan(&blockedQueries)
	db.QueryRow("SELECT AVG(latency_ms) FROM query_logs WHERE latency_ms > 0").Scan(&avgLatencyNull)
	if avgLatencyNull.Valid {
		avgLatency = int64(avgLatencyNull.Float64)
	}
	return
}

func getRecentQueries(limit int) ([]QueryLog, error) {
	rows, err := db.Query(`
		SELECT ql.timestamp, ql.client_ip, COALESCE(ca.alias, '') as alias, ql.query_name, ql.query_type, ql.response_code, ql.latency_ms 
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		ORDER BY ql.timestamp DESC 
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []QueryLog
	for rows.Next() {
		var q QueryLog
		var timestamp, alias string
		rows.Scan(&timestamp, &q.ClientIP, &alias, &q.QueryName, &q.QueryType, &q.ResponseCode, &q.LatencyMs)

		t, err := time.Parse("2006-01-02 15:04:05", timestamp)
		if err != nil {
			t, err = time.Parse(time.RFC3339, timestamp)
		}
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", timestamp)
		}

		if err != nil {
			q.Timestamp = timestamp
		} else {
			q.Timestamp = t.Format("Jan 2 15:04:05")
		}

		q.ClientIPRaw = q.ClientIP
		if alias != "" {
			q.ClientIP = alias + " (" + q.ClientIP + ")"
		}

		queries = append(queries, q)
	}
	return queries, nil
}

func getTopDomains(limit int) ([]TopItem, error) {
	rows, err := db.Query(`
		SELECT query_name, COUNT(*) as cnt 
		FROM query_logs 
		GROUP BY query_name 
		ORDER BY cnt DESC 
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TopItem
	for rows.Next() {
		var item TopItem
		rows.Scan(&item.Domain, &item.Count)
		items = append(items, item)
	}
	return items, nil
}

func getTopClients(limit int) ([]TopItem, error) {
	rows, err := db.Query(`
		SELECT ql.client_ip, COALESCE(ca.alias, ql.client_ip) as display_name, COUNT(*) as cnt 
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		GROUP BY ql.client_ip 
		ORDER BY cnt DESC 
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TopItem
	for rows.Next() {
		var item TopItem
		var displayName string
		rows.Scan(&item.ClientIP, &displayName, &item.Count)
		item.ClientIPRaw = item.ClientIP
		if displayName != item.ClientIP {
			item.ClientIP = displayName + " (" + item.ClientIP + ")"
		}
		items = append(items, item)
	}
	return items, nil
}

func getRewrites() ([]RewriteView, error) {
	rows, err := db.Query("SELECT id, domain, COALESCE(ip_addresses, ''), enabled FROM rewrites ORDER BY domain")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rewrites []RewriteView
	for rows.Next() {
		var rw RewriteView
		rows.Scan(&rw.ID, &rw.Domain, &rw.IPAddresses, &rw.Enabled)
		rewrites = append(rewrites, rw)
	}
	return rewrites, nil
}

func getAllClients() ([]Client, error) {
	rows, err := db.Query(`
		SELECT 
			ql.client_ip,
			COALESCE(ca.alias, '') as alias,
			COUNT(*) as query_count,
			MAX(ql.timestamp) as last_seen
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		GROUP BY ql.client_ip
		ORDER BY query_count DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		var lastSeen string
		rows.Scan(&c.IPAddress, &c.Alias, &c.QueryCount, &lastSeen)
		t, _ := time.Parse("2006-01-02 15:04:05", lastSeen)
		c.LastSeen = t.Format("2006-01-02 15:04")
		clients = append(clients, c)
	}
	return clients, nil
}

func getAllGroups() ([]Group, error) {
	rows, err := db.Query(`
		SELECT 
			g.id,
			g.name,
			COUNT(DISTINCT cgm.client_ip) as member_count,
			COUNT(DISTINCT gb.blocklist_id) as blocklist_count
		FROM client_groups g
		LEFT JOIN client_group_members cgm ON g.id = cgm.group_id
		LEFT JOIN group_blocklists gb ON g.id = gb.group_id
		GROUP BY g.id
		ORDER BY g.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		rows.Scan(&g.ID, &g.Name, &g.MemberCount, &g.BlocklistCount)
		groups = append(groups, g)
	}
	return groups, nil
}

func getClientDetail(ip string) (alias string, totalQueries, avgLatency int64, firstSeen, lastSeen string) {
	var avgLatencyNull sql.NullFloat64
	db.QueryRow("SELECT COALESCE(alias, '') FROM client_aliases WHERE ip_address = ?", ip).Scan(&alias)
	db.QueryRow("SELECT COUNT(*), AVG(latency_ms), MIN(timestamp), MAX(timestamp) FROM query_logs WHERE client_ip = ?", ip).Scan(&totalQueries, &avgLatencyNull, &firstSeen, &lastSeen)
	if avgLatencyNull.Valid {
		avgLatency = int64(avgLatencyNull.Float64)
	}
	return
}

func getClientQueries(ip string, limit int) ([]QueryLog, error) {
	rows, err := db.Query(`
		SELECT timestamp, query_name, query_type, response_code, latency_ms 
		FROM query_logs 
		WHERE client_ip = ?
		ORDER BY timestamp DESC 
		LIMIT ?
	`, ip, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []QueryLog
	for rows.Next() {
		var q QueryLog
		var timestamp string
		rows.Scan(&timestamp, &q.QueryName, &q.QueryType, &q.ResponseCode, &q.LatencyMs)

		t, err := time.Parse("2006-01-02 15:04:05", timestamp)
		if err != nil {
			t, err = time.Parse(time.RFC3339, timestamp)
		}
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", timestamp)
		}

		if err != nil {
			q.Timestamp = timestamp
		} else {
			q.Timestamp = t.Format("Jan 2 15:04:05")
		}

		queries = append(queries, q)
	}
	return queries, nil
}

func getClientGroups(ip string) ([]Group, error) {
	rows, err := db.Query(`
		SELECT cg.id, cg.name, 
			EXISTS(SELECT 1 FROM client_group_members WHERE client_ip = ? AND group_id = cg.id) as is_member
		FROM client_groups cg
		ORDER BY cg.name
	`, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		rows.Scan(&g.ID, &g.Name, &g.IsMember)
		groups = append(groups, g)
	}
	return groups, nil
}

func getClientBlocklists(ip string) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM client_blocklists WHERE client_ip = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned)
		blocklists = append(blocklists, bl)
	}
	return blocklists, nil
}

func getClientActiveBlocklists(ip string) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT DISTINCT b.id, b.alias, b.domain_count,
			CASE 
				WHEN EXISTS(SELECT 1 FROM client_blocklists cb WHERE cb.client_ip = ? AND cb.blocklist_id = b.id) THEN 'direct'
				ELSE 'group'
			END as source
		FROM blocklists b
		WHERE b.enabled = 1 AND (
			EXISTS(SELECT 1 FROM client_blocklists cb WHERE cb.client_ip = ? AND cb.blocklist_id = b.id)
			OR EXISTS(
				SELECT 1 FROM group_blocklists gb 
				JOIN client_group_members cgm ON gb.group_id = cgm.group_id 
				WHERE cgm.client_ip = ? AND gb.blocklist_id = b.id
			)
		)
		ORDER BY b.alias
	`, ip, ip, ip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.Source)
		blocklists = append(blocklists, bl)
	}
	return blocklists, nil
}

func getGroupName(groupID int) (string, error) {
	var name string
	err := db.QueryRow("SELECT name FROM client_groups WHERE id = ?", groupID).Scan(&name)
	return name, err
}

func getGroupMembers(groupID int) ([]Member, error) {
	rows, err := db.Query(`
		SELECT 
			cgm.client_ip,
			COALESCE(ca.alias, '') as alias,
			COUNT(ql.id) as query_count,
			MAX(ql.timestamp) as last_seen
		FROM client_group_members cgm
		LEFT JOIN client_aliases ca ON cgm.client_ip = ca.ip_address
		LEFT JOIN query_logs ql ON cgm.client_ip = ql.client_ip
		WHERE cgm.group_id = ?
		GROUP BY cgm.client_ip
		ORDER BY query_count DESC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []Member
	for rows.Next() {
		var m Member
		var lastSeen sql.NullString
		rows.Scan(&m.IPAddress, &m.Alias, &m.QueryCount, &lastSeen)

		if lastSeen.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastSeen.String)
			m.LastSeen = t.Format("2006-01-02 15:04")
		} else {
			m.LastSeen = "Never"
		}

		members = append(members, m)
	}
	return members, nil
}

func getAllClientsForGroup(groupID int) ([]Client, error) {
	rows, err := db.Query(`
		SELECT DISTINCT 
			ql.client_ip,
			COALESCE(ca.alias, '') as alias,
			EXISTS(SELECT 1 FROM client_group_members WHERE client_ip = ql.client_ip AND group_id = ?) as is_member
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		ORDER BY ql.client_ip
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		rows.Scan(&c.IPAddress, &c.Alias, &c.IsMember)
		clients = append(clients, c)
	}
	return clients, nil
}

func getGroupBlocklists(groupID int) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM group_blocklists WHERE group_id = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned)
		blocklists = append(blocklists, bl)
	}
	return blocklists, nil
}

func getGroupActiveBlocklists(groupID int) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT b.id, b.alias, b.domain_count
		FROM blocklists b
		JOIN group_blocklists gb ON b.id = gb.blocklist_id
		WHERE gb.group_id = ? AND b.enabled = 1
		ORDER BY b.alias
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount)
		bl.Source = "group"
		blocklists = append(blocklists, bl)
	}
	return blocklists, nil
}

func getSettings() (cacheTTL, bootstrapTTL, logRetention string) {
	db.QueryRow("SELECT value FROM settings WHERE key = 'cache_ttl'").Scan(&cacheTTL)
	db.QueryRow("SELECT value FROM settings WHERE key = 'bootstrap_ttl'").Scan(&bootstrapTTL)
	db.QueryRow("SELECT value FROM settings WHERE key = 'log_retention_days'").Scan(&logRetention)
	return
}

func setClientAlias(ip, alias string) error {
	if alias == "" {
		_, err := db.Exec("DELETE FROM client_aliases WHERE ip_address = ?", ip)
		return err
	}
	_, err := db.Exec(`
		INSERT INTO client_aliases (ip_address, alias, updated_at) 
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(ip_address) DO UPDATE SET alias = ?, updated_at = CURRENT_TIMESTAMP
	`, ip, alias, alias)
	return err
}

func addClientToGroup(ip string, groupID string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id) VALUES (?, ?)", ip, groupID)
	return err
}

func removeClientFromGroup(ip string, groupID string) error {
	_, err := db.Exec("DELETE FROM client_group_members WHERE client_ip = ? AND group_id = ?", ip, groupID)
	return err
}

func addClientBlocklist(ip string, blocklistID string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", ip, blocklistID)
	return err
}

func removeClientBlocklist(ip string, blocklistID string) error {
	_, err := db.Exec("DELETE FROM client_blocklists WHERE client_ip = ? AND blocklist_id = ?", ip, blocklistID)
	return err
}

func createGroup(name string) (int64, error) {
	result, err := db.Exec("INSERT INTO client_groups (name) VALUES (?)", name)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateGroupName(groupID int, name string) error {
	_, err := db.Exec("UPDATE client_groups SET name = ? WHERE id = ?", name, groupID)
	return err
}

func deleteGroup(groupID int) error {
	_, err := db.Exec("DELETE FROM client_groups WHERE id = ?", groupID)
	return err
}

func addGroupBlocklist(groupID int, blocklistID int) error {
	_, err := db.Exec("INSERT OR IGNORE INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", groupID, blocklistID)
	return err
}

func removeGroupBlocklist(groupID int, blocklistID int) error {
	_, err := db.Exec("DELETE FROM group_blocklists WHERE group_id = ? AND blocklist_id = ?", groupID, blocklistID)
	return err
}

func createRewrite(domain, ipAddresses string, enabled bool) (int64, error) {
	result, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		domain, "", ipAddresses, enabled)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func updateRewriteEnabled(rewriteID int, enabled bool) error {
	_, err := db.Exec("UPDATE rewrites SET enabled = ? WHERE id = ?", enabled, rewriteID)
	return err
}

func updateRewriteDomain(rewriteID int, domain string) error {
	_, err := db.Exec("UPDATE rewrites SET domain = ? WHERE id = ?", domain, rewriteID)
	return err
}

func updateRewriteIPs(rewriteID int, ipAddresses string) error {
	_, err := db.Exec("UPDATE rewrites SET ip_addresses = ? WHERE id = ?", ipAddresses, rewriteID)
	return err
}

func deleteRewrite(rewriteID int) error {
	_, err := db.Exec("DELETE FROM rewrites WHERE id = ?", rewriteID)
	return err
}
