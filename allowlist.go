package main

import (
	"database/sql"
	"net/http"
	"strings"
)

type Allowlist struct {
	ID              int    `json:"id"`
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	DomainCount     int    `json:"domain_count"`
	LastUpdated     string `json:"last_updated"`
	RefreshInterval int    `json:"refresh_interval"`
}

// isAllowedForClient is a backward-compatible wrapper around evaluatePolicy.
// It returns true if the cascading policy engine explicitly allows the domain
// (not the default allow when no rules match).
func isAllowedForClient(clientIP, domain string) bool {
	result := evaluatePolicy(clientIP, domain, 1)
	return result.Result == "allow" && result.ResultSource != nil && result.ResultSource.Tier != "default"
}

func refreshAllowlistByID(id int) error {
	return refreshListByID(allowListStore, id)
}

// --- API Handlers ---

func handleAPIAllowlistsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetAllowlists(w, r)
	} else {
		handleAPICreateAllowlist(w, r)
	}
}

// handleAPIGetAllowlists godoc
// @Summary List all allowlists
// @Description Returns all configured allowlists with their ID, URL, alias, enabled status, domain count, and last updated timestamp
// @Tags Allowlists
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/allowlists [get]
func handleAPIGetAllowlists(w http.ResponseWriter, r *http.Request) {
	rows, err := readListViewRows(readDB, "allowlist")
	if err != nil {
		writeDBError(w, err)
		return
	}
	isAdmin := requestIsAdmin(r)
	allowlists := make([]AllowlistView, 0, len(rows))
	for _, row := range rows {
		row.URL = urlForViewer(row.URL, isAdmin)
		allowlists = append(allowlists, AllowlistView(row))
	}
	writeJSON(w, http.StatusOK, allowlists)
}

// handleAPICreateAllowlist godoc
// @Summary Create an allowlist
// @Description Creates a new allowlist with a URL and alias, then triggers an async fetch of the allowlist domains if URL is provided
// @Tags Allowlists
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param allowlist body object true "Allowlist with 'url', 'alias', and 'enabled' fields"
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/allowlists [post]
func handleAPICreateAllowlist(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CreateAllowlistRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}
	if !validateListURLForAPI(w, req.URL) {
		return
	}

	refreshInterval := 604800 // default: 7 days
	if req.RefreshInterval != nil {
		refreshInterval = *req.RefreshInterval
	}

	ts, nid := syncNow()
	result, err := execPolicyWrite("INSERT INTO allowlists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)", req.URL, req.Alias, req.Enabled, refreshInterval, ts, nid)
	if err != nil {
		writeDBError(w, err)
		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		writeDBError(w, err)
		return
	}

	if req.URL != "" {
		go func() {
			if err := refreshAllowlistByID(int(id)); err != nil {
				logAllowlist.Error("failed to fetch allowlist", "id", id, "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusCreated, CreateAllowlistResponse{ID: id, Alias: req.Alias})
}

func handleAPIAllowlistAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/allowlists/"):]
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	id, validID := parsePathID(w, parts[0])
	if !validID {
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case "GET":
		if action == "compatibility" {
			handleAPIListCompatibility(w, r, "allowlist", id)
			return
		}
		if action == "domains" {
			limit, offset, err := pageParams(r.URL.Query(), 1000, 10000)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			var total int
			if err := readDB.QueryRow("SELECT COUNT(*) FROM allowed_domains WHERE allowlist_id = ?", id).Scan(&total); err != nil {
				writeDBError(w, err)
				return
			}

			// #nosec G202 G701 -- Source-text projection is fixed SQL; request values remain bound parameters.
			rows, err := readDB.Query("SELECT domain FROM allowed_domains WHERE allowlist_id = ? ORDER BY "+storedRuleTextSQL+",domain LIMIT ? OFFSET ?", id, limit, offset)
			if err != nil {
				writeDBError(w, err)
				return
			}
			defer closeQueryRows(rows)

			var domains []string
			for rows.Next() {
				var d string
				if err := rows.Scan(&d); err != nil {
					writeDBError(w, err)
					return
				}
				text, err := storedRuleDisplayText(d)
				if err != nil {
					writeDBError(w, err)
					return
				}
				domains = append(domains, text)
			}
			if err := rows.Err(); err != nil {
				writeDBError(w, err)
				return
			}
			if domains == nil {
				domains = []string{}
			}
			writeJSON(w, http.StatusOK, AllowlistDomainsPage{AllowlistID: id, Domains: domains, Total: total, Limit: limit, Offset: offset})
			return
		}
		writeError(w, http.StatusBadRequest, "Invalid action")

	case "DELETE":
		err := deleteLocalEntity("allowlists", id, snapshotPolicy)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	case "POST":
		switch action {
		case "toggle":
			ts, nid := syncNow()
			_, err := execPolicyWrite("UPDATE allowlists SET enabled = NOT enabled, updated_at = ?, node_id = ? WHERE id = ?", ts, nid, id)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		case "refresh":
			go func() {
				if err := refreshAllowlistByID(id); err != nil {
					logAllowlist.Error("failed to refresh allowlist", "id", id, "error", err)
				}
			}()
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		default:
			writeError(w, http.StatusBadRequest, "Invalid action")
		}

	case "PUT":
		var req UpdateAllowlistRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}
		if !validateListURLForAPI(w, req.URL) {
			return
		}

		err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
			ts, nid := syncNow()
			if req.URL != "" {
				res, err := tx.Exec("UPDATE allowlists SET url=?,updated_at=?,node_id=? WHERE id=?", req.URL, ts, nid, id)
				if err := checkListWrite(res, err, 1); err != nil {
					return err
				}
			}
			if req.Alias != "" {
				if err := renameEntityTx(tx, "allowlists", id, req.Alias); err != nil {
					return err
				}
			}
			if req.RefreshInterval != nil {
				res, err := tx.Exec("UPDATE allowlists SET refresh_interval=?,updated_at=?,node_id=? WHERE id=?", *req.RefreshInterval, ts, nid, id)
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

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// --- DB helpers for allowlist assignments ---

func addClientAllowlist(ip string, allowlistID string) error {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO client_allowlists (client_ip, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", ip, allowlistID, ts, nid)
	return err
}

func removeClientAllowlist(ip string, allowlistID string) error {
	return removeLocalRelationship("client_allowlists", ip, allowlistID)
}

func addGroupAllowlist(groupID int, allowlistID int) error {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO group_allowlists (group_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", groupID, allowlistID, ts, nid)
	return err
}

func removeGroupAllowlist(groupID int, allowlistID int) error {
	return removeLocalRelationship("group_allowlists", groupID, allowlistID)
}

// getOrCreateManualAllowlist returns the manual (url=”) allowlist for a client,
// creating it if it doesn't exist.
func getOrCreateManualAllowlist(clientIP string) (int64, error) {
	return getOrCreateManualList(allowListStore, manualClient, clientIP)
}

// getOrCreateGroupManualAllowlist returns the manual allowlist for a group.
func getOrCreateGroupManualAllowlist(groupID int) (int64, error) {
	return getOrCreateManualList(allowListStore, manualGroup, groupID)
}

// handleAPIClientAllowDomain handles POST/DELETE for manual per-client domain allows.
func handleAPIClientAllowDomain(w http.ResponseWriter, r *http.Request, clientIP string, parts []string) {
	handleManualRule(w, r, allowListStore, manualClient, clientIP, parts)
}

// handleAPIGroupAllowDomain handles POST/DELETE for manual per-group domain allows.
func handleAPIGroupAllowDomain(w http.ResponseWriter, r *http.Request, groupID int, parts []string) {
	handleManualRule(w, r, allowListStore, manualGroup, groupID, parts)
}
