package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// handleAPIQueryLogs godoc
// @Summary Get query logs
// @Description Returns paginated, filterable DNS query logs with full decision audit trail
// @Tags Query Logs
// @Security ApiKeyAuth
// @Produce json
// @Param limit query int false "Number of results per page (1-5000, default 100)"
// @Param offset query int false "Offset for pagination (default 0)"
// @Param client_ip query string false "Filter by client IP address"
// @Param domain query string false "Filter by domain name (substring match)"
// @Param blocked query string false "Filter by blocked status (true/false or 1/0)"
// @Param query_type query string false "Filter by DNS query type (A, AAAA, CNAME, etc.)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/query-logs [get]
func buildQueryLogsFilter(q url.Values) (string, []interface{}, error) {
	var filters []string
	var args []interface{}

	if clientIP := q.Get("client_ip"); clientIP != "" {
		filters = append(filters, "ql.client_ip = ?")
		args = append(args, clientIP)
	}
	if domain := q.Get("domain"); domain != "" {
		filters = append(filters, "ql.query_name LIKE ?")
		args = append(args, "%"+domain+"%")
	}
	switch blocked := q.Get("blocked"); blocked {
	case "true", "1":
		filters = append(filters, "ql.blocked = 1")
	case "false", "0":
		filters = append(filters, "ql.blocked = 0")
	}
	if queryType := q.Get("query_type"); queryType != "" {
		filters = append(filters, "ql.query_type = ?")
		args = append(args, queryType)
	}
	if resultReason := q.Get("result_reason"); resultReason != "" {
		filters = append(filters, "ql.result_reason = ?")
		args = append(args, resultReason)
	}
	if groupID := q.Get("group_id"); groupID != "" {
		filters = append(filters, "ql.client_ip IN (SELECT client_ip FROM client_group_members WHERE group_id = ?)")
		args = append(args, groupID)
	}
	if rangeID := q.Get("range_id"); rangeID != "" {
		var rangeMatchedIPs []interface{}
		var cidr string
		err := readDB.QueryRow("SELECT cidr FROM ip_ranges WHERE id = ?", rangeID).Scan(&cidr)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", nil, err
		}
		if err == nil {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return "", nil, err
			}
			ipRows, err := readDB.Query("SELECT DISTINCT client_ip FROM query_logs")
			if err != nil {
				return "", nil, err
			}
			defer closeQueryRows(ipRows)
			for ipRows.Next() {
				var ip string
				if err := ipRows.Scan(&ip); err != nil {
					return "", nil, err
				}
				if parsed := net.ParseIP(ip); parsed != nil && ipNet.Contains(parsed) {
					rangeMatchedIPs = append(rangeMatchedIPs, ip)
				}
			}
			if err := ipRows.Err(); err != nil {
				return "", nil, err
			}
		}

		if len(rangeMatchedIPs) > 0 {
			placeholders := strings.Repeat("?,", len(rangeMatchedIPs))
			placeholders = placeholders[:len(placeholders)-1]
			filters = append(filters, "ql.client_ip IN ("+placeholders+")")
			args = append(args, rangeMatchedIPs...)
		} else {
			filters = append(filters, "1=0")
		}
	}

	if len(filters) == 0 {
		return "1=1", args, nil
	}
	return strings.Join(filters, " AND "), args, nil
}

// maxQueryLogPageSize matches the largest page the Logs view offers (5k rows).
const maxQueryLogPageSize = 5000

func handleAPIQueryLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	q := r.URL.Query()

	limit, offset, err := pageParams(q, 100, maxQueryLogPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	filterClause, filterArgs, err := buildQueryLogsFilter(q)
	if err != nil {
		writeDBError(w, err)
		return
	}

	query := `SELECT ql.id, ql.timestamp, ql.client_ip,
		COALESCE(NULLIF(ql.client_name, ''), ca.alias, '') as alias,
		ql.query_name, ql.query_type, ql.response_code, ql.blocked, COALESCE(ql.upstream, ''), ql.latency_microseconds,
		COALESCE(ql.block_tier, ''), COALESCE(ql.block_rule, ''),
		COALESCE(ql.block_source, ''), COALESCE(ql.block_list_id, 0), COALESCE(ql.block_list_name, ''),
		COALESCE(ql.result, ''), COALESCE(ql.result_reason, ''),
		COALESCE(ql.result_tier, ''), COALESCE(ql.result_entity, ''),
		COALESCE(ql.result_is_published, 0), COALESCE(ql.result_rule, ''),
		COALESCE(ql.result_list_id, 0), COALESCE(ql.result_list_name, ''),
		COALESCE(ql.range_result, ''), COALESCE(ql.range_entity, ''),
		COALESCE(ql.group_result, ''), COALESCE(ql.group_entity, ''),
		COALESCE(ql.ip_result, ''), COALESCE(ql.ip_entity, '')
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		WHERE ` + filterClause

	query += " ORDER BY ql.timestamp DESC LIMIT ? OFFSET ?"
	args := append(append([]interface{}{}, filterArgs...), limit, offset)

	loc, err := getConfiguredTimezone()
	if err != nil {
		writeDBError(w, err)
		return
	}
	isAdmin := requestIsAdmin(r)

	rows, err := readDB.Query(query, args...)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	var logs []QueryLogView
	for rows.Next() {
		var id int
		var timestamp, clientIP, alias, queryName, queryType, responseCode, upstream, blockTier, blockRule string
		var blockSource, blockListName string
		var blockListID int
		var blocked bool
		var latencyMicroseconds int64
		var result, resultReason, resultTier, resultEntity, resultRule, resultListName string
		var resultIsPublished bool
		var resultListID int
		var rangeResult, rangeEntity, groupResult, groupEntity, ipResult, ipEntity string
		if err := rows.Scan(&id, &timestamp, &clientIP, &alias, &queryName, &queryType, &responseCode, &blocked, &upstream, &latencyMicroseconds,
			&blockTier, &blockRule, &blockSource, &blockListID, &blockListName,
			&result, &resultReason,
			&resultTier, &resultEntity, &resultIsPublished, &resultRule, &resultListID, &resultListName,
			&rangeResult, &rangeEntity, &groupResult, &groupEntity, &ipResult, &ipEntity,
		); err != nil {
			writeDBError(w, err)
			return
		}
		entry := QueryLogView{
			ID:                  id,
			Timestamp:           utcToConfiguredTZ(timestamp, loc),
			ClientIP:            clientIP,
			QueryName:           queryName,
			QueryType:           queryType,
			ResponseCode:        responseCode,
			Blocked:             blocked,
			Upstream:            urlForViewer(upstream, isAdmin),
			LatencyMicroseconds: latencyMicroseconds,
		}
		if alias != "" {
			entry.ClientAlias = apiPtr(alias)
		}
		// Legacy columns
		if blockTier != "" {
			entry.BlockTier = apiPtr(blockTier)
		}
		if blockRule != "" {
			entry.BlockRule = apiPtr(blockRule)
		}
		if blockSource != "" {
			entry.BlockSource = apiPtr(blockSource)
		}
		if blockListID > 0 {
			entry.BlockListID = apiPtr(blockListID)
			entry.BlockListName = apiPtr(blockListName)
		}
		// Rich policy columns
		if result != "" {
			entry.Result = apiPtr(result)
		}
		if resultReason != "" {
			entry.ResultReason = apiPtr(resultReason)
		}
		if resultTier != "" {
			entry.ResultTier = apiPtr(resultTier)
			entry.ResultEntity = apiPtr(resultEntity)
			entry.ResultIsPublished = apiPtr(resultIsPublished)
			entry.ResultRule = apiPtr(resultRule)
			if resultListID > 0 {
				entry.ResultListID = apiPtr(resultListID)
				entry.ResultListName = apiPtr(resultListName)
			}
		}
		if rangeResult != "" {
			entry.RangeResult = apiPtr(rangeResult)
		}
		if rangeEntity != "" {
			entry.RangeName = apiPtr(rangeEntity)
		}
		if groupResult != "" {
			entry.GroupResult = apiPtr(groupResult)
		}
		if groupEntity != "" {
			entry.GroupName = apiPtr(groupEntity)
		}
		if ipResult != "" {
			entry.IPResult = apiPtr(ipResult)
			entry.IPEntity = apiPtr(ipEntity)
		}
		logs = append(logs, entry)
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}

	if logs == nil {
		logs = []QueryLogView{}
	}

	// Get total count for pagination
	countQuery := "SELECT COALESCE(SUM(ql.coalesced_count), 0) FROM query_logs ql WHERE " + filterClause

	var total int
	if err := readDB.QueryRow(countQuery, filterArgs...).Scan(&total); err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, QueryLogPage{
		Logs:   logs,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// handleAPIQueryLogDetail godoc
// @Summary Get query log detail with full policy evaluation
// @Description Returns a single query log entry including nested policy evaluation data
// @Tags Query Logs
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "Query log ID"
// @Success 200 {object} apiResponse
// @Failure 404 {object} apiResponse
// @Router /api/query-logs/{id} [get]
func handleAPIQueryLogDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Extract ID from path: /api/query-logs/123
	path := strings.TrimPrefix(r.URL.Path, "/api/query-logs/")
	id, err := strconv.Atoi(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query log ID")
		return
	}

	loc, err := getConfiguredTimezone()
	if err != nil {
		writeDBError(w, err)
		return
	}

	var timestamp, clientIP, queryName, queryType, responseCode, upstream string
	var blocked bool
	var latencyMicroseconds int64
	var blockTier, blockRule, blockSource, blockListName string
	var blockListID int
	var result, resultReason, clientName, policyJSON string
	var resultTier, resultEntity, resultRule, resultListName string
	var resultIsPublished bool
	var resultListID int
	var rangeResult, rangeEntity, rangeRule, rangeListName string
	var rangeIsPublished bool
	var rangeListID int
	var groupResult, groupEntity, groupRule, groupListName string
	var groupIsPublished bool
	var groupListID int
	var ipResult, ipEntity, ipRule, ipListName string
	var ipIsPublished bool
	var ipListID int

	err = readDB.QueryRow(`SELECT timestamp, client_ip, query_name, query_type, response_code,
		blocked, COALESCE(upstream, ''), latency_microseconds,
		COALESCE(block_tier, ''), COALESCE(block_rule, ''), COALESCE(block_source, ''),
		COALESCE(block_list_id, 0), COALESCE(block_list_name, ''),
		COALESCE(result, ''), COALESCE(result_reason, ''), COALESCE(client_name, ''), COALESCE(policy_json, ''),
		COALESCE(result_tier, ''), COALESCE(result_entity, ''), COALESCE(result_is_published, 0),
		COALESCE(result_rule, ''), COALESCE(result_list_id, 0), COALESCE(result_list_name, ''),
		COALESCE(range_result, ''), COALESCE(range_entity, ''), COALESCE(range_is_published, 0),
		COALESCE(range_rule, ''), COALESCE(range_list_id, 0), COALESCE(range_list_name, ''),
		COALESCE(group_result, ''), COALESCE(group_entity, ''), COALESCE(group_is_published, 0),
		COALESCE(group_rule, ''), COALESCE(group_list_id, 0), COALESCE(group_list_name, ''),
		COALESCE(ip_result, ''), COALESCE(ip_entity, ''), COALESCE(ip_is_published, 0),
		COALESCE(ip_rule, ''), COALESCE(ip_list_id, 0), COALESCE(ip_list_name, '')
		FROM query_logs WHERE id = ?`, id).Scan(
		&timestamp, &clientIP, &queryName, &queryType, &responseCode,
		&blocked, &upstream, &latencyMicroseconds,
		&blockTier, &blockRule, &blockSource, &blockListID, &blockListName,
		&result, &resultReason, &clientName, &policyJSON,
		&resultTier, &resultEntity, &resultIsPublished, &resultRule, &resultListID, &resultListName,
		&rangeResult, &rangeEntity, &rangeIsPublished, &rangeRule, &rangeListID, &rangeListName,
		&groupResult, &groupEntity, &groupIsPublished, &groupRule, &groupListID, &groupListName,
		&ipResult, &ipEntity, &ipIsPublished, &ipRule, &ipListID, &ipListName,
	)
	if err != nil {
		writeDBLookupError(w, err, "query log not found")
		return
	}

	entry := QueryLogDetailView{
		ID:                  id,
		Timestamp:           utcToConfiguredTZ(timestamp, loc),
		ClientIP:            clientIP,
		ClientName:          clientName,
		QueryName:           queryName,
		QueryType:           queryType,
		ResponseCode:        responseCode,
		Blocked:             blocked,
		Upstream:            urlForViewer(upstream, requestIsAdmin(r)),
		LatencyMicroseconds: latencyMicroseconds,
		Result:              result,
		ResultReason:        resultReason,
		ResultTier:          resultTier,
		ResultEntity:        resultEntity,
		ResultIsPublished:   resultIsPublished,
		ResultRule:          resultRule,
		ResultListID:        resultListID,
		ResultListName:      resultListName,
		RangeResult:         rangeResult,
		RangeEntity:         rangeEntity,
		RangeIsPublished:    rangeIsPublished,
		RangeRule:           rangeRule,
		RangeListID:         rangeListID,
		RangeListName:       rangeListName,
		GroupResult:         groupResult,
		GroupEntity:         groupEntity,
		GroupIsPublished:    groupIsPublished,
		GroupRule:           groupRule,
		GroupListID:         groupListID,
		GroupListName:       groupListName,
		IPResult:            ipResult,
		IPEntity:            ipEntity,
		IPIsPublished:       ipIsPublished,
		IPRule:              ipRule,
		IPListID:            ipListID,
		IPListName:          ipListName,
	}

	// Parse stored policy_json when present; otherwise synthesize a compact policy
	// view from the flattened decision columns so default-allow rows still render.
	if policyJSON != "" {
		var policyObj *queryLogPolicySnapshot
		if err := json.Unmarshal([]byte(policyJSON), &policyObj); err != nil {
			writeDBError(w, err)
			return
		}
		entry.Policy = policyObj
	} else {
		entry.Policy = buildQueryLogPolicySnapshot(queryLogEntry{
			blocked:           blocked,
			upstream:          upstream,
			result:            result,
			resultReason:      resultReason,
			resultTier:        resultTier,
			resultEntity:      resultEntity,
			resultIsPublished: resultIsPublished,
			resultRule:        resultRule,
			resultListID:      resultListID,
			resultListName:    resultListName,
			rangeResult:       rangeResult,
			rangeEntity:       rangeEntity,
			rangeIsPublished:  rangeIsPublished,
			rangeRule:         rangeRule,
			rangeListID:       rangeListID,
			rangeListName:     rangeListName,
			groupResult:       groupResult,
			groupEntity:       groupEntity,
			groupIsPublished:  groupIsPublished,
			groupRule:         groupRule,
			groupListID:       groupListID,
			groupListName:     groupListName,
			ipResult:          ipResult,
			ipEntity:          ipEntity,
			ipIsPublished:     ipIsPublished,
			ipRule:            ipRule,
			ipListID:          ipListID,
			ipListName:        ipListName,
		})
	}

	writeJSON(w, http.StatusOK, entry)
}
