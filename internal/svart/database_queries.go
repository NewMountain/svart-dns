package svart

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	// Register the SQLite driver used by database/sql.
	_ "github.com/mattn/go-sqlite3"
)

func getDashboardStats() (totalQueries, blockedQueries, avgLatency int64, err error) {
	err = readDB.QueryRow(`SELECT COALESCE(SUM(coalesced_count),0), COALESCE(SUM(CASE WHEN blocked=1 THEN coalesced_count ELSE 0 END),0), CAST(COALESCE(AVG(CASE WHEN latency_microseconds>0 THEN latency_microseconds END),0) AS INTEGER) FROM query_logs`).Scan(&totalQueries, &blockedQueries, &avgLatency)
	return
}

func getRecentQueries(limit int) ([]QueryLog, error) {
	rows, err := readDB.Query(`
		SELECT ql.timestamp, ql.client_ip, COALESCE(ca.alias, '') as alias, ql.query_name, ql.query_type, ql.response_code, ql.latency_microseconds
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		ORDER BY ql.timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var queries []QueryLog
	for rows.Next() {
		var q QueryLog
		var timestamp, alias string
		if err := rows.Scan(&timestamp, &q.ClientIP, &alias, &q.QueryName, &q.QueryType, &q.ResponseCode, &q.LatencyMicroseconds); err != nil {
			return nil, err
		}

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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return queries, nil
}

func getTopDomains(limit int) ([]TopItem, error) {
	rows, err := readDB.Query(`
		SELECT query_name, SUM(coalesced_count) as cnt
		FROM query_logs
		GROUP BY query_name
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var items []TopItem
	for rows.Next() {
		var item TopItem
		if err := rows.Scan(&item.Domain, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func getTopClients(limit int) ([]TopItem, error) {
	rows, err := readDB.Query(`
		SELECT ql.client_ip, COALESCE(ca.alias, ql.client_ip) as display_name, SUM(ql.coalesced_count) as cnt
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		GROUP BY ql.client_ip
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var items []TopItem
	for rows.Next() {
		var item TopItem
		var displayName string
		if err := rows.Scan(&item.ClientIP, &displayName, &item.Count); err != nil {
			return nil, err
		}
		item.ClientIPRaw = item.ClientIP
		if displayName != item.ClientIP {
			item.ClientIP = displayName + " (" + item.ClientIP + ")"
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func getRewrites() ([]RewriteView, error) {
	rows, err := readDB.Query("SELECT id, domain, COALESCE(ip_addresses, ''), enabled FROM rewrites ORDER BY domain")
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var rewrites []RewriteView
	for rows.Next() {
		var rw RewriteView
		if err := rows.Scan(&rw.ID, &rw.Domain, &rw.IPAddresses, &rw.Enabled); err != nil {
			return nil, err
		}
		rewrites = append(rewrites, rw)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rewrites, nil
}

const knownClientIPsSQL = `
	SELECT client_ip FROM (
		SELECT ql.client_ip AS client_ip FROM query_logs ql GROUP BY ql.client_ip
		UNION
		SELECT ca.ip_address AS client_ip FROM client_aliases ca
		UNION
		SELECT cp.client_ip AS client_ip FROM client_policies cp
		UNION
		SELECT cb.client_ip AS client_ip FROM client_blocklists cb GROUP BY cb.client_ip
		UNION
		SELECT cal.client_ip AS client_ip FROM client_allowlists cal GROUP BY cal.client_ip
		UNION
		SELECT cgm.client_ip AS client_ip FROM client_group_members cgm GROUP BY cgm.client_ip
	) known_clients
	WHERE client_ip <> ''
`

func getAllClients() ([]Client, error) {
	rows, err := readDB.Query(`
		WITH known_clients AS (` + knownClientIPsSQL + `),
		client_activity AS (
			SELECT
				ql.client_ip,
				SUM(ql.coalesced_count) AS query_count,
				MAX(ql.timestamp) AS last_seen
			FROM query_logs ql
			GROUP BY ql.client_ip
		)
		SELECT
			kc.client_ip,
			COALESCE(ca.alias, '') AS alias,
			COALESCE(activity.query_count, 0) AS query_count,
			COALESCE(activity.last_seen, '') AS last_seen
		FROM known_clients kc
		LEFT JOIN client_aliases ca ON kc.client_ip = ca.ip_address
		LEFT JOIN client_activity activity ON kc.client_ip = activity.client_ip
		ORDER BY
			COALESCE(activity.query_count, 0) DESC,
			CASE
				WHEN COALESCE(ca.alias, '') <> '' THEN ca.alias
				ELSE kc.client_ip
			END
	`)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var clients []Client
	for rows.Next() {
		var c Client
		var lastSeen string
		if err := rows.Scan(&c.IPAddress, &c.Alias, &c.QueryCount, &lastSeen); err != nil {
			return nil, err
		}
		if lastSeen != "" {
			parsed, err := time.Parse("2006-01-02 15:04:05", lastSeen)
			if err != nil {
				c.LastSeen = lastSeen
			} else {
				c.LastSeen = parsed.Format("2006-01-02 15:04")
			}
		}
		clients = append(clients, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return clients, nil
}

func getAllGroups() ([]Group, error) {
	rows, err := readDB.Query(`
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
	defer closeQueryRows(rows)

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.MemberCount, &g.BlocklistCount); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func getClientDetail(ip string) (alias string, totalQueries, avgLatency int64, firstSeen, lastSeen string, err error) {
	err = readDB.QueryRow("SELECT COALESCE(alias, '') FROM client_aliases WHERE ip_address = ?", ip).Scan(&alias)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	err = readDB.QueryRow(`SELECT COALESCE(SUM(coalesced_count),0), CAST(COALESCE(AVG(latency_microseconds),0) AS INTEGER), COALESCE(MIN(timestamp),''), COALESCE(MAX(timestamp),'') FROM query_logs WHERE client_ip=?`, ip).Scan(&totalQueries, &avgLatency, &firstSeen, &lastSeen)
	return
}

func getClientGroups(ip string) ([]Group, error) {
	rows, err := readDB.Query(`
		SELECT cg.id, cg.name,
			EXISTS(SELECT 1 FROM client_group_members WHERE client_ip = ? AND group_id = cg.id) as is_member
		FROM client_groups cg
		ORDER BY cg.name
	`, ip)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.IsMember); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func getClientBlocklists(ip string) ([]BlocklistAssignment, error) {
	rows, err := readDB.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM client_blocklists WHERE client_ip = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, ip)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		if err := rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned); err != nil {
			return nil, err
		}
		blocklists = append(blocklists, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return blocklists, nil
}

func getGroupName(groupID int) (string, error) {
	var name string
	err := readDB.QueryRow("SELECT name FROM client_groups WHERE id = ?", groupID).Scan(&name)
	return name, err
}

func getGroupMembers(groupID int) ([]Member, error) {
	rows, err := readDB.Query(`
		SELECT
			cgm.client_ip,
			COALESCE(ca.alias, '') as alias,
			COALESCE(SUM(ql.coalesced_count), 0) as query_count,
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
	defer closeQueryRows(rows)

	var members []Member
	for rows.Next() {
		var m Member
		var lastSeen sql.NullString
		if err := rows.Scan(&m.IPAddress, &m.Alias, &m.QueryCount, &lastSeen); err != nil {
			return nil, err
		}

		if lastSeen.Valid {
			parsed, err := time.Parse("2006-01-02 15:04:05", lastSeen.String)
			if err != nil {
				m.LastSeen = lastSeen.String
			} else {
				m.LastSeen = parsed.Format("2006-01-02 15:04")
			}
		} else {
			m.LastSeen = "Never"
		}

		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func getGroupStats(groupID int) (totalQueries int64, avgLatency float64, firstSeen, lastSeen string, err error) {
	var avgNull sql.NullFloat64
	var fsNull, lsNull sql.NullString
	err = readDB.QueryRow(`
		SELECT COALESCE(SUM(ql.coalesced_count),0), AVG(ql.latency_microseconds), MIN(ql.timestamp), MAX(ql.timestamp)
		FROM query_logs ql
		JOIN client_group_members cgm ON ql.client_ip = cgm.client_ip
		WHERE cgm.group_id = ?
	`, groupID).Scan(&totalQueries, &avgNull, &fsNull, &lsNull)
	if avgNull.Valid {
		avgLatency = avgNull.Float64
	}
	if fsNull.Valid {
		firstSeen = fsNull.String
	}
	if lsNull.Valid {
		lastSeen = lsNull.String
	}
	return
}

func getRangeStats(cidr string) (totalQueries int64, avgLatency float64, firstSeen, lastSeen string, err error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return
	}

	// Get distinct client IPs from query_logs
	rows, err := readDB.Query("SELECT DISTINCT client_ip FROM query_logs")
	if err != nil {
		return
	}
	defer closeQueryRows(rows)

	var matchingIPs []string
	for rows.Next() {
		var ip string
		if err = rows.Scan(&ip); err != nil {
			return
		}
		if parsed := net.ParseIP(ip); parsed != nil && ipNet.Contains(parsed) {
			matchingIPs = append(matchingIPs, ip)
		}
	}
	if err = rows.Err(); err != nil {
		return
	}
	if len(matchingIPs) == 0 {
		return
	}

	placeholders := strings.Repeat("?,", len(matchingIPs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(matchingIPs))
	for i, ip := range matchingIPs {
		args[i] = ip
	}

	var avgNull sql.NullFloat64
	var fsNull, lsNull sql.NullString
	err = readDB.QueryRow(fmt.Sprintf(`
		SELECT COALESCE(SUM(coalesced_count),0), AVG(latency_microseconds), MIN(timestamp), MAX(timestamp)
		FROM query_logs WHERE client_ip IN (%s)
	`, placeholders), args...).Scan(&totalQueries, &avgNull, &fsNull, &lsNull)
	if avgNull.Valid {
		avgLatency = avgNull.Float64
	}
	if fsNull.Valid {
		firstSeen = fsNull.String
	}
	if lsNull.Valid {
		lastSeen = lsNull.String
	}
	return
}

func scanRecentLogs(rows *sql.Rows, loc *time.Location) ([]RecentLogView, error) {
	var logs []RecentLogView
	for rows.Next() {
		var id int64
		var ts, clientIP, alias, qn, qt, upstream string
		var blocked bool
		var latency int64
		var resultReason, resultTier, resultEntity, resultListName, blockListName string
		var rangeEntity, groupEntity string
		if err := rows.Scan(&id, &ts, &clientIP, &alias, &qn, &qt, &blocked, &upstream,
			&latency, &resultReason, &resultTier, &resultEntity, &resultListName,
			&blockListName, &rangeEntity, &groupEntity); err != nil {
			return nil, err
		}
		entry := RecentLogView{
			ID: id, Timestamp: utcToConfiguredTZ(ts, loc),
			ClientIP: clientIP, QueryName: qn, QueryType: qt,
			Blocked: blocked, Upstream: upstream,
			LatencyMicroseconds: latency,
		}
		if alias != "" {
			entry.ClientAlias = apiPtr(alias)
		}
		if resultReason != "" {
			entry.ResultReason = apiPtr(resultReason)
		}
		if resultTier != "" {
			entry.ResultTier = apiPtr(resultTier)
		}
		if resultEntity != "" {
			entry.ResultEntity = apiPtr(resultEntity)
		}
		if resultListName != "" {
			entry.ResultListName = apiPtr(resultListName)
		}
		if blockListName != "" {
			entry.BlockListName = apiPtr(blockListName)
		}
		if rangeEntity != "" {
			entry.RangeName = apiPtr(rangeEntity)
		}
		if groupEntity != "" {
			entry.GroupName = apiPtr(groupEntity)
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []RecentLogView{}
	}
	return logs, nil
}

func getGroupRecentLogs(groupID int, limit int) ([]RecentLogView, error) {
	loc, err := getConfiguredTimezone()
	if err != nil {
		return nil, err
	}
	rows, err := readDB.Query(`
		SELECT ql.id, ql.timestamp, ql.client_ip,
			COALESCE(NULLIF(ql.client_name, ''), ca.alias, '') as alias,
			ql.query_name, ql.query_type, ql.blocked, COALESCE(ql.upstream, ''),
			ql.latency_microseconds,
			COALESCE(ql.result_reason, ''), COALESCE(ql.result_tier, ''),
			COALESCE(ql.result_entity, ''), COALESCE(ql.result_list_name, ''),
			COALESCE(ql.block_list_name, ''),
			COALESCE(ql.range_entity, ''), COALESCE(ql.group_entity, '')
		FROM query_logs ql
		JOIN client_group_members cgm ON ql.client_ip = cgm.client_ip
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		WHERE cgm.group_id = ?
		ORDER BY ql.timestamp DESC LIMIT ?
	`, groupID, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	return scanRecentLogs(rows, loc)
}

func getRangeRecentLogs(cidr string, limit int) ([]RecentLogView, error) {
	loc, err := getConfiguredTimezone()
	if err != nil {
		return nil, err
	}
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	rows, err := readDB.Query("SELECT DISTINCT client_ip FROM query_logs")
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var matchingIPs []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return nil, err
		}
		if parsed := net.ParseIP(ip); parsed != nil && ipNet.Contains(parsed) {
			matchingIPs = append(matchingIPs, ip)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(matchingIPs) == 0 {
		return []RecentLogView{}, nil
	}

	placeholders := strings.Repeat("?,", len(matchingIPs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]interface{}, len(matchingIPs))
	for i, ip := range matchingIPs {
		args[i] = ip
	}
	args = append(args, limit)

	logRows, err := readDB.Query(fmt.Sprintf(`
		SELECT ql.id, ql.timestamp, ql.client_ip,
			COALESCE(NULLIF(ql.client_name, ''), ca.alias, '') as alias,
			ql.query_name, ql.query_type, ql.blocked, COALESCE(ql.upstream, ''),
			ql.latency_microseconds,
			COALESCE(ql.result_reason, ''), COALESCE(ql.result_tier, ''),
			COALESCE(ql.result_entity, ''), COALESCE(ql.result_list_name, ''),
			COALESCE(ql.block_list_name, ''),
			COALESCE(ql.range_entity, ''), COALESCE(ql.group_entity, '')
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		WHERE ql.client_ip IN (%s)
		ORDER BY ql.timestamp DESC LIMIT ?
	`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(logRows)

	return scanRecentLogs(logRows, loc)
}

func getGroupBlocklists(groupID int) ([]BlocklistAssignment, error) {
	rows, err := readDB.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM group_blocklists WHERE group_id = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var blocklists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		if err := rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned); err != nil {
			return nil, err
		}
		blocklists = append(blocklists, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return blocklists, nil
}
