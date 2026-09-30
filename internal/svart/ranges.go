package svart

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// --- API Handlers ---

func handleAPIRangesRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		handleAPIGetRanges(w, r)
	case "POST":
		handleAPICreateRange(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIGetRanges godoc
// @Summary List all IP ranges
// @Description Returns all configured IP ranges (CIDRs) with their ID, name, CIDR, and creation timestamp
// @Tags Ranges
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/ranges [get]
func handleAPIGetRanges(w http.ResponseWriter, _ *http.Request) {
	rows, err := db.Query("SELECT id, name, cidr, COALESCE(created_at, '') FROM ip_ranges ORDER BY id")
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	var ranges []RangeView
	for rows.Next() {
		var id int
		var name, cidr, createdAt string
		if err := rows.Scan(&id, &name, &cidr, &createdAt); err != nil {
			writeDBError(w, err)
			return
		}
		ranges = append(ranges, RangeView{ID: id, Name: name, CIDR: cidr, CreatedAt: createdAt})
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}

	if ranges == nil {
		ranges = []RangeView{}
	}
	writeJSON(w, http.StatusOK, ranges)
}

// handleAPICreateRange godoc
// @Summary Create an IP range
// @Description Creates a new IP range with a name and CIDR notation. The CIDR is validated before creation.
// @Tags Ranges
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param range body object true "IP range with 'name' and 'cidr' fields"
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/ranges [post]
func handleAPICreateRange(w http.ResponseWriter, r *http.Request) {
	var req CreateRangeRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}

	// Validate CIDR
	_, _, err := net.ParseCIDR(req.CIDR)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid CIDR: %v", err))
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	ts, nid := syncNow()
	result, err := execPolicyWrite("INSERT INTO ip_ranges (name, cidr, updated_at, node_id) VALUES (?, ?, ?, ?)", req.Name, req.CIDR, ts, nid)
	if err != nil {
		writeDBError(w, err)
		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, CreateRangeResponse{ID: id})
}

func getAllRangeBlocklists(rangeID int) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM range_blocklists WHERE range_id = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, rangeID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var lists []BlocklistAssignment
	for rows.Next() {
		var bl BlocklistAssignment
		if err := rows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned); err != nil {
			return nil, err
		}
		lists = append(lists, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lists, nil
}

func getRangeAllowlists(rangeID int) ([]BlocklistAssignment, error) {
	rows, err := db.Query(`
		SELECT a.id, a.alias, a.domain_count
		FROM allowlists a
		JOIN range_allowlists ra ON a.id = ra.allowlist_id
		WHERE ra.range_id = ? AND a.enabled = 1
		ORDER BY a.alias
	`, rangeID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var lists []BlocklistAssignment
	for rows.Next() {
		var al BlocklistAssignment
		if err := rows.Scan(&al.ID, &al.Alias, &al.DomainCount); err != nil {
			return nil, err
		}
		al.Source = "range"
		lists = append(lists, al)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lists, nil
}

func handleAPIRangeAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/ranges/"):]
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	rangeID, validID := parsePathID(w, parts[0])
	if !validID {
		return
	}

	// /api/ranges/{id}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			var name, cidr, createdAt string
			var policyID *int
			err := db.QueryRow("SELECT name, cidr, created_at, policy_id FROM ip_ranges WHERE id = ?", rangeID).Scan(&name, &cidr, &createdAt, &policyID)
			if err != nil {
				writeDBLookupError(w, err, "range not found")
				return
			}
			blocklists, err := getAllRangeBlocklists(rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			allowlists, err := getRangeAllowlists(rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			if blocklists == nil {
				blocklists = []BlocklistAssignment{}
			}
			if allowlists == nil {
				allowlists = []BlocklistAssignment{}
			}
			// Compute unique domain stats for assigned blocklists
			var assignedIDs []int
			for _, bl := range blocklists {
				if bl.IsAssigned {
					assignedIDs = append(assignedIDs, bl.ID)
				}
			}
			totalQueries, avgLatency, firstSeen, lastSeen, err := getRangeStats(cidr)
			if err != nil {
				writeDBError(w, err)
				return
			}
			recentLogs, err := getRangeRecentLogs(cidr, 20)
			if err != nil {
				writeDBError(w, err)
				return
			}
			customBlocked, err := readManualDomains(db, blockListStore, manualRange, rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			customAllowed, err := readManualDomains(db, allowListStore, manualRange, rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			resp := RangeDetailView{
				CustomBlocked: customBlocked, CustomAllowed: customAllowed,
				ID:                     rangeID,
				Name:                   name,
				CIDR:                   cidr,
				CreatedAt:              createdAt,
				Blocklists:             blocklists,
				Allowlists:             allowlists,
				BlocklistStats:         computeBlocklistUniqueDomains(assignedIDs),
				TotalQueries:           totalQueries,
				AvgLatencyMicroseconds: avgLatency,
				FirstSeen:              firstSeen,
				LastSeen:               lastSeen,
				RecentLogs:             recentLogs,
			}
			if policyID != nil {
				var policyName string
				if err := db.QueryRow("SELECT name FROM policies WHERE id = ?", *policyID).Scan(&policyName); err != nil {
					writeDBError(w, err)
					return
				}
				resp.Policy = &PolicyReference{ID: *policyID, Name: policyName}
			}
			writeJSON(w, http.StatusOK, resp)

		case "PUT":
			var req UpdateRangeRequest
			if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
				return
			}
			if req.CIDR != "" {
				if _, _, err := net.ParseCIDR(req.CIDR); err != nil {
					writeError(w, http.StatusBadRequest, "invalid CIDR")
					return
				}
			}
			err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
				ts, nid := syncNow()
				if req.CIDR != "" {
					if err := renameEntityTx(tx, "ip_ranges", rangeID, req.CIDR); err != nil {
						return err
					}
				}
				if req.Name != "" {
					res, err := tx.Exec("UPDATE ip_ranges SET name=?,updated_at=?,node_id=? WHERE id=?", req.Name, ts, nid, rangeID)
					if err := checkListWrite(res, err, 1); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			err := deleteLocalEntity("ip_ranges", rangeID, snapshotPolicy)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if parts[1] == "block-domain" {
		handleAPIRangeBlockDomain(w, r, rangeID, parts)
		return
	}
	if parts[1] == "allow-domain" {
		handleAPIRangeAllowDomain(w, r, rangeID, parts)
		return
	}
	// /api/ranges/{id}/blocklists/{blocklistId}
	if len(parts) == 3 && parts[1] == "blocklists" {
		blocklistID, validID := parsePathID(w, parts[2])
		if !validID {
			return
		}

		switch r.Method {
		case "POST":
			ts, nid := syncNow()
			_, err := execPolicyWrite("INSERT INTO range_blocklists (range_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", rangeID, blocklistID, ts, nid)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			err := removeLocalRelationship("range_blocklists", rangeID, blocklistID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// /api/ranges/{id}/allowlists/{allowlistId}
	if len(parts) == 3 && parts[1] == "allowlists" {
		allowlistID, validID := parsePathID(w, parts[2])
		if !validID {
			return
		}

		switch r.Method {
		case "POST":
			ts, nid := syncNow()
			_, err := execPolicyWrite("INSERT INTO range_allowlists (range_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", rangeID, allowlistID, ts, nid)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			err := removeLocalRelationship("range_allowlists", rangeID, allowlistID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// /api/ranges/{id}/policy/{policyId}  or  /api/ranges/{id}/policy
	if parts[1] == "policy" {
		switch r.Method {
		case "POST":
			if len(parts) < 3 {
				writeError(w, http.StatusBadRequest, "policy ID required")
				return
			}
			pid, validID := parsePathID(w, parts[2])
			if !validID {
				return
			}
			ts, nid := syncNow()
			_, err := execPolicyWrite("UPDATE ip_ranges SET policy_id = ?, updated_at = ?, node_id = ? WHERE id = ?", pid, ts, nid, rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			ts, nid := syncNow()
			_, err := execPolicyWrite("UPDATE ip_ranges SET policy_id = NULL, updated_at = ?, node_id = ? WHERE id = ?", ts, nid, rangeID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	writeError(w, http.StatusBadRequest, "Invalid action")
}

func handleAPIRangeBlockDomain(w http.ResponseWriter, r *http.Request, id int, parts []string) {
	handleManualRule(w, r, blockListStore, manualRange, id, parts)
}
func handleAPIRangeAllowDomain(w http.ResponseWriter, r *http.Request, id int, parts []string) {
	handleManualRule(w, r, allowListStore, manualRange, id, parts)
}
