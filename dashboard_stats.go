package main

import (
	"net"
	"net/http"
	"strings"
	"time"
)

type DashboardSummary struct {
	TotalQueries           int64 `json:"total_queries"`
	BlockedQueries         int64 `json:"blocked_queries"`
	AllowedQueries         int64 `json:"allowed_queries"`
	AvgLatencyMicroseconds int64 `json:"avg_latency_microseconds"`
	AvgLatencyMS           int64 `json:"avg_latency_ms"`
	ActiveClients          int64 `json:"active_clients"`
	CacheHits              int64 `json:"cache_hits"`
	ServfailCount          int64 `json:"servfail_count"`
	RewriteCount           int64 `json:"rewrite_count"`
	CustomBlocks           int64 `json:"custom_blocks"`
	CustomAllows           int64 `json:"custom_allows"`
	RewriteHits            int64 `json:"rewrite_hits"`
}

type DashboardTimeseriesPoint struct {
	Timestamp string `json:"timestamp"`
	Queries   int64  `json:"queries"`
	Blocked   int64  `json:"blocked"`
}

type DashboardLatencyPoint struct {
	Timestamp    string `json:"timestamp"`
	AvgLatencyUS int64  `json:"avg_latency_us"`
	MaxLatencyUS int64  `json:"max_latency_us"`
}

type DashboardTopClient struct {
	ClientIP string `json:"client_ip"`
	Alias    string `json:"alias"`
	Allowed  int64  `json:"allowed"`
	Blocked  int64  `json:"blocked"`
}

type DashboardBlockSource struct {
	ListName   string  `json:"list_name"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

type DashboardUpstreamUsage struct {
	Upstream   string  `json:"upstream"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

type DashboardTopDomain struct {
	Domain string `json:"domain"`
	Count  int64  `json:"count"`
}

type DashboardTopDomains struct {
	Domains []DashboardTopDomain `json:"domains"`
	Total   int64                `json:"total"`
}

type DashboardServfailClient struct {
	ClientIP   string               `json:"client_ip"`
	Alias      string               `json:"alias"`
	Count      int64                `json:"count"`
	TopDomains []DashboardTopDomain `json:"top_domains"`
}

type DashboardSnapshot struct {
	Summary       DashboardSummary           `json:"summary"`
	Timeseries    []DashboardTimeseriesPoint `json:"timeseries"`
	Latency       []DashboardLatencyPoint    `json:"latency"`
	TopClients    []DashboardTopClient       `json:"top_clients"`
	BlockSources  []DashboardBlockSource     `json:"block_sources"`
	UpstreamUsage []DashboardUpstreamUsage   `json:"upstream_usage"`
	TopPermitted  DashboardTopDomains        `json:"top_permitted"`
	TopBlocked    DashboardTopDomains        `json:"top_blocked"`
	Servfails     []DashboardServfailClient  `json:"servfails"`
	System        []SystemSample             `json:"system"`
}

func queryDashboardSummary(since string) (DashboardSummary, error) {
	var summary DashboardSummary
	var avgLatency float64
	err := readDB.QueryRow(`WITH recent AS (
		SELECT client_ip, blocked, upstream, latency_microseconds, response_code,
			result_reason, block_list_id, block_rule, coalesced_count
		FROM query_logs INDEXED BY idx_query_logs_timestamp
		WHERE timestamp > ?
	)
	SELECT
		COALESCE(SUM(coalesced_count), 0),
		COALESCE(SUM(CASE WHEN blocked=1 THEN coalesced_count ELSE 0 END), 0),
		COALESCE(AVG(CASE WHEN latency_microseconds>0 AND response_code != 'SERVFAIL' THEN latency_microseconds END), 0),
		COUNT(DISTINCT client_ip),
		COALESCE(SUM(CASE WHEN upstream = 'cache' THEN coalesced_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN response_code = 'SERVFAIL' THEN coalesced_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN upstream = 'rewrite' THEN coalesced_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN (CASE WHEN result_reason != '' THEN result_reason = 'custom_block' ELSE blocked=1 AND block_list_id=0 AND block_rule != '' END) THEN coalesced_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN (CASE WHEN result_reason != '' THEN result_reason = 'custom_allow' ELSE blocked=0 AND block_list_id=0 AND block_rule != '' END) THEN coalesced_count ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN (CASE WHEN result_reason != '' THEN result_reason = 'rewrite' ELSE upstream='rewrite' END) THEN coalesced_count ELSE 0 END), 0)
	FROM recent`, since).Scan(
		&summary.TotalQueries,
		&summary.BlockedQueries,
		&avgLatency,
		&summary.ActiveClients,
		&summary.CacheHits,
		&summary.ServfailCount,
		&summary.RewriteCount,
		&summary.CustomBlocks,
		&summary.CustomAllows,
		&summary.RewriteHits,
	)
	if err != nil {
		return DashboardSummary{}, err
	}

	summary.AllowedQueries = summary.TotalQueries - summary.BlockedQueries
	summary.AvgLatencyMicroseconds = int64(avgLatency)
	summary.AvgLatencyMS = summary.AvgLatencyMicroseconds / 1000
	return summary, nil
}

func queryDashboardTimeseries(window time.Duration, since string, buckets int) ([]DashboardTimeseriesPoint, error) {
	loc, err := getConfiguredTimezone()
	if err != nil {
		return nil, err
	}

	bucketSec := int(window.Seconds()) / buckets
	query := `
		SELECT
			datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch') as bucket_time,
			COALESCE(SUM(coalesced_count), 0) as queries,
			COALESCE(SUM(CASE WHEN blocked=1 THEN coalesced_count ELSE 0 END), 0) as blocked
		FROM query_logs INDEXED BY idx_query_logs_timestamp
		WHERE timestamp > ?
		GROUP BY (CAST(strftime('%s', timestamp) AS INTEGER) / ?)
		ORDER BY bucket_time ASC`

	rows, err := readDB.Query(query, bucketSec, bucketSec, since, bucketSec)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardTimeseriesPoint, 0)
	for rows.Next() {
		var ts string
		var queries, blocked int64
		if err := rows.Scan(&ts, &queries, &blocked); err != nil {
			return nil, err
		}
		result = append(result, DashboardTimeseriesPoint{
			Timestamp: utcToConfiguredTZ(ts, loc),
			Queries:   queries,
			Blocked:   blocked,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func queryDashboardLatency(window time.Duration, since string, buckets int) ([]DashboardLatencyPoint, error) {
	loc, err := getConfiguredTimezone()
	if err != nil {
		return nil, err
	}

	bucketSec := int(window.Seconds()) / buckets
	query := `
		SELECT
			datetime((CAST(strftime('%s', timestamp) AS INTEGER) / ?) * ?, 'unixepoch') as bucket_time,
			COALESCE(AVG(CASE WHEN latency_microseconds>0 THEN latency_microseconds END), 0) as avg_lat,
			COALESCE(AVG(CASE WHEN latency_microseconds>0 AND upstream NOT IN ('', 'cache') THEN latency_microseconds END), 0) as upstream_avg_lat
		FROM query_logs INDEXED BY idx_query_logs_timestamp
		WHERE timestamp > ? AND response_code != 'SERVFAIL'
		GROUP BY (CAST(strftime('%s', timestamp) AS INTEGER) / ?)
		ORDER BY bucket_time ASC`

	rows, err := readDB.Query(query, bucketSec, bucketSec, since, bucketSec)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardLatencyPoint, 0)
	for rows.Next() {
		var ts string
		var avgLat, maxLat float64
		if err := rows.Scan(&ts, &avgLat, &maxLat); err != nil {
			return nil, err
		}
		result = append(result, DashboardLatencyPoint{
			Timestamp:    utcToConfiguredTZ(ts, loc),
			AvgLatencyUS: int64(avgLat),
			MaxLatencyUS: int64(maxLat),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func queryDashboardTopClients(since string, limit int) ([]DashboardTopClient, error) {
	rows, err := readDB.Query(`
		WITH recent AS (
			SELECT client_ip, client_name, blocked, coalesced_count
			FROM query_logs INDEXED BY idx_query_logs_timestamp
			WHERE timestamp > ?
		),
		grouped AS (
			SELECT
				client_ip,
				MAX(NULLIF(client_name, '')) AS client_name,
				COALESCE(SUM(CASE WHEN blocked=0 THEN coalesced_count ELSE 0 END), 0) AS allowed,
				COALESCE(SUM(CASE WHEN blocked=1 THEN coalesced_count ELSE 0 END), 0) AS blocked
			FROM recent
			GROUP BY client_ip
		)
		SELECT g.client_ip,
			COALESCE(g.client_name, COALESCE(ca.alias, '')) AS alias,
			g.allowed,
			g.blocked
		FROM grouped g
		LEFT JOIN client_aliases ca ON g.client_ip = ca.ip_address
		ORDER BY (g.allowed + g.blocked) DESC, g.client_ip
		LIMIT ?`, since, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardTopClient, 0)
	for rows.Next() {
		var client DashboardTopClient
		if err := rows.Scan(&client.ClientIP, &client.Alias, &client.Allowed, &client.Blocked); err != nil {
			return nil, err
		}
		result = append(result, client)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func queryDashboardBlockSources(since string) ([]DashboardBlockSource, error) {
	rows, err := readDB.Query(`
		WITH grouped AS (
			SELECT CASE
				WHEN result_list_name != '' THEN result_list_name
				WHEN result_reason = 'custom_block' THEN 'Custom Rule'
				ELSE COALESCE(NULLIF(block_list_name, ''), COALESCE(NULLIF(block_source, ''), 'Custom'))
			END AS source,
				COALESCE(SUM(coalesced_count), 0) AS cnt
			FROM query_logs INDEXED BY idx_query_logs_timestamp
			WHERE blocked=1 AND timestamp > ?
			GROUP BY source
		)
		SELECT source, cnt, COALESCE(SUM(cnt) OVER (), 0) AS total
		FROM grouped
		ORDER BY cnt DESC, source
		LIMIT 10`, since)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardBlockSource, 0)
	for rows.Next() {
		var totalBlocked int64
		var source DashboardBlockSource
		if err := rows.Scan(&source.ListName, &source.Count, &totalBlocked); err != nil {
			return nil, err
		}
		if totalBlocked > 0 {
			source.Percentage = float64(source.Count) / float64(totalBlocked) * 100
		}
		result = append(result, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func queryDashboardUpstreamUsage(since string) ([]DashboardUpstreamUsage, error) {
	rows, err := readDB.Query(`
		WITH grouped AS (
			SELECT upstream, COALESCE(SUM(coalesced_count), 0) AS cnt
			FROM query_logs INDEXED BY idx_query_logs_timestamp
			WHERE blocked=0 AND timestamp > ? AND upstream NOT IN ('', 'cache')
			GROUP BY upstream
		)
		SELECT upstream, cnt, COALESCE(SUM(cnt) OVER (), 0) AS total
		FROM grouped
		ORDER BY cnt DESC, upstream
		LIMIT 10`, since)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardUpstreamUsage, 0)
	for rows.Next() {
		var totalAllowed int64
		var usage DashboardUpstreamUsage
		if err := rows.Scan(&usage.Upstream, &usage.Count, &totalAllowed); err != nil {
			return nil, err
		}
		// Redacted for every caller: dashboard responses are cached by URL
		// and shared between admin and readonly viewers.
		usage.Upstream = redactURL(usage.Upstream)
		if totalAllowed > 0 {
			usage.Percentage = float64(usage.Count) / float64(totalAllowed) * 100
		}
		result = append(result, usage)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func dashboardExcludedDomains() []string {
	excluded := make([]string, 0)
	for _, upstream := range upstreamStorage.getAll() {
		_, addr := parseUpstreamAddress(upstream.Upstream)
		host := addr
		if h, _, err := net.SplitHostPort(addr); err == nil {
			host = h
		}
		if idx := strings.Index(host, "/"); idx > 0 {
			host = host[:idx]
		}
		if host != "" && net.ParseIP(host) == nil {
			excluded = append(excluded, host+".")
		}
	}
	return excluded
}

func queryDashboardTopDomains(since string, blocked *bool, limit int) (DashboardTopDomains, error) {
	where := "timestamp > ?"
	args := []interface{}{since}
	if blocked != nil {
		where += " AND blocked = ?"
		args = append(args, *blocked)
	}
	for _, domain := range dashboardExcludedDomains() {
		where += " AND query_name != ?"
		args = append(args, domain)
	}

	topArgs := append(append([]interface{}{}, args...), limit)
	rows, err := readDB.Query(
		"WITH grouped AS ("+
			"SELECT query_name, COALESCE(SUM(coalesced_count), 0) AS cnt "+
			"FROM query_logs INDEXED BY idx_query_logs_timestamp WHERE "+where+" GROUP BY query_name"+
			") "+
			"SELECT query_name, cnt, COALESCE(SUM(cnt) OVER (), 0) AS total FROM grouped ORDER BY cnt DESC, query_name LIMIT ?",
		topArgs...,
	)
	if err != nil {
		return DashboardTopDomains{}, err
	}
	defer closeQueryRows(rows)

	result := DashboardTopDomains{Domains: make([]DashboardTopDomain, 0)}
	for rows.Next() {
		var domain DashboardTopDomain
		var total int64
		if err := rows.Scan(&domain.Domain, &domain.Count, &total); err != nil {
			return DashboardTopDomains{}, err
		}
		result.Total = total
		result.Domains = append(result.Domains, domain)
	}
	if err := rows.Err(); err != nil {
		return DashboardTopDomains{}, err
	}
	return result, nil
}

func queryDashboardServfails(since string) ([]DashboardServfailClient, error) {
	rows, err := readDB.Query(`
		WITH recent_servfails AS (
			SELECT client_ip, client_name, query_name, coalesced_count
			FROM query_logs INDEXED BY idx_query_logs_timestamp
			WHERE response_code = 'SERVFAIL' AND timestamp > ?
		),
		client_totals AS (
			SELECT
				client_ip,
				MAX(NULLIF(client_name, '')) AS client_name,
				COALESCE(SUM(coalesced_count), 0) AS cnt
			FROM recent_servfails
			GROUP BY client_ip
		),
		top_clients AS (
			SELECT
				ct.client_ip,
				COALESCE(ct.client_name, COALESCE(ca.alias, '')) AS alias,
				ct.cnt
			FROM client_totals ct
			LEFT JOIN client_aliases ca ON ct.client_ip = ca.ip_address
			ORDER BY ct.cnt DESC, ct.client_ip
			LIMIT 10
		),
		ranked_domains AS (
			SELECT
				domain_counts.client_ip,
				domain_counts.query_name,
				domain_counts.cnt,
				ROW_NUMBER() OVER (
					PARTITION BY domain_counts.client_ip
					ORDER BY domain_counts.cnt DESC, domain_counts.query_name
				) AS rn
			FROM (
				SELECT rs.client_ip, rs.query_name, COALESCE(SUM(rs.coalesced_count), 0) AS cnt
				FROM recent_servfails rs
				INNER JOIN top_clients tc ON tc.client_ip = rs.client_ip
				GROUP BY rs.client_ip, rs.query_name
			) AS domain_counts
		)
		SELECT
			tc.client_ip,
			tc.alias,
			tc.cnt,
			COALESCE(rd.query_name, ''),
			COALESCE(rd.cnt, 0)
		FROM top_clients tc
		LEFT JOIN ranked_domains rd ON rd.client_ip = tc.client_ip AND rd.rn <= 5
		ORDER BY tc.cnt DESC, tc.client_ip, rd.rn`, since)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	result := make([]DashboardServfailClient, 0)
	var current *DashboardServfailClient
	for rows.Next() {
		var clientIP, alias, domain string
		var count, domainCount int64
		if err := rows.Scan(&clientIP, &alias, &count, &domain, &domainCount); err != nil {
			return nil, err
		}
		if current == nil || current.ClientIP != clientIP {
			result = append(result, DashboardServfailClient{
				ClientIP:   clientIP,
				Alias:      alias,
				Count:      count,
				TopDomains: make([]DashboardTopDomain, 0),
			})
			current = &result[len(result)-1]
		}
		if domain != "" {
			current.TopDomains = append(current.TopDomains, DashboardTopDomain{
				Domain: domain,
				Count:  domainCount,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func getDashboardSnapshot(window time.Duration, buckets, clientLimit, domainLimit int) (DashboardSnapshot, error) {
	since := windowSince(window)
	summary, err := queryDashboardSummary(since)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	timeseries, err := queryDashboardTimeseries(window, since, buckets)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	latency, err := queryDashboardLatency(window, since, buckets)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	topClients, err := queryDashboardTopClients(since, clientLimit)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	blockSources, err := queryDashboardBlockSources(since)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	upstreamUsage, err := queryDashboardUpstreamUsage(since)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	blockedFalse := false
	topPermitted, err := queryDashboardTopDomains(since, &blockedFalse, domainLimit)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	blockedTrue := true
	topBlocked, err := queryDashboardTopDomains(since, &blockedTrue, domainLimit)
	if err != nil {
		return DashboardSnapshot{}, err
	}
	servfails, err := queryDashboardServfails(since)
	if err != nil {
		return DashboardSnapshot{}, err
	}

	return DashboardSnapshot{
		Summary:       summary,
		Timeseries:    timeseries,
		Latency:       latency,
		TopClients:    topClients,
		BlockSources:  blockSources,
		UpstreamUsage: upstreamUsage,
		TopPermitted:  topPermitted,
		TopBlocked:    topBlocked,
		Servfails:     servfails,
		System:        getSystemSamples(),
	}, nil
}

func handleAPIStatsDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	q := r.URL.Query()
	buckets, err := intQueryParam(q, "buckets", 24, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	clientLimit, err := intQueryParam(q, "client_limit", 8, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domainLimit, err := intQueryParam(q, "domain_limit", 50, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot, err := getDashboardSnapshot(parseTimeWindow(r), buckets, clientLimit, domainLimit)
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}
