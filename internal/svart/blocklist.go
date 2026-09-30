package svart

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/miekg/dns"
)

// Blocklist stores the configuration and refresh state of a block list.
type Blocklist struct {
	ID              int    `json:"id"`
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	DomainCount     int    `json:"domain_count"`
	LastUpdated     string `json:"last_updated"`
	RefreshInterval int    `json:"refresh_interval"`
}

// isBlockedForClient is a backward-compatible wrapper around evaluatePolicy.
// It returns true if the cascading policy engine decides to block.
func isBlockedForClient(clientIP, domain string) bool {
	return evaluatePolicy(clientIP, domain, 1).Result == "block"
}

// extractResponseIPs pulls the resolved A/AAAA addresses out of a DNS response.
func extractResponseIPs(resp *dns.Msg) []string {
	var ips []string
	for _, answer := range resp.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ips = append(ips, rr.A.String())
		case *dns.AAAA:
			ips = append(ips, rr.AAAA.String())
		}
	}
	return ips
}

// handleAPIBlocklists godoc
// @Summary Create a blocklist
// @Description Creates a new blocklist with a URL and alias, then triggers an async fetch of the blocklist domains
// @Tags Blocklists
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param blocklist body object true "Blocklist with 'url', 'alias', and 'enabled' fields"
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/blocklists [post]
func handleAPIBlocklists(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CreateBlocklistRequest
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
	result, err := execPolicyWrite("INSERT INTO blocklists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)", req.URL, req.Alias, req.Enabled, refreshInterval, ts, nid)
	if err != nil {
		writeDBError(w, err)
		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		writeDBError(w, err)
		return
	}

	go func() {
		if err := refreshBlocklistByID(int(id)); err != nil {
			logBlocklist.Error("failed to fetch blocklist", "id", id, "error", err)
		}
	}()

	writeJSON(w, http.StatusCreated, CreateBlocklistResponse{ID: id, Alias: req.Alias})
}

func handleAPIBlocklistAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/blocklists/"):]
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
			handleAPIListCompatibility(w, r, "blocklist", id)
			return
		}
		if action == "domains" {
			search := r.URL.Query().Get("search")
			limit, offset, err := pageParams(r.URL.Query(), 1000, 10000)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}

			var total int
			var rows *sql.Rows
			if search != "" {
				// #nosec G202 G701 -- Source-text projection is fixed SQL; request values remain bound parameters.
				if err := readDB.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ? AND ("+storedRuleTextSQL+") LIKE '%' || ? || '%'", id, search).Scan(&total); err != nil {
					writeDBError(w, err)
					return
				}
				// #nosec G202 G701 -- Source-text projection is fixed SQL; request values remain bound parameters.
				rows, err = readDB.Query("SELECT domain FROM blocked_domains WHERE blocklist_id = ? AND ("+storedRuleTextSQL+") LIKE '%' || ? || '%' ORDER BY "+storedRuleTextSQL+",domain LIMIT ? OFFSET ?", id, search, limit, offset)
			} else {
				if err := readDB.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ?", id).Scan(&total); err != nil {
					writeDBError(w, err)
					return
				}
				// #nosec G202 G701 -- Source-text projection is fixed SQL; request values remain bound parameters.
				rows, err = readDB.Query("SELECT domain FROM blocked_domains WHERE blocklist_id = ? ORDER BY "+storedRuleTextSQL+",domain LIMIT ? OFFSET ?", id, limit, offset)
			}
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
			writeJSON(w, http.StatusOK, BlocklistDomainsPage{BlocklistID: id, Domains: domains, Total: total, Limit: limit, Offset: offset})
			return
		}
		writeError(w, http.StatusBadRequest, "Invalid action")

	case "DELETE":
		err := deleteLocalEntity("blocklists", id, snapshotPolicy)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	case "POST":
		switch action {
		case "toggle":
			ts, nid := syncNow()
			_, err := execPolicyWrite("UPDATE blocklists SET enabled = NOT enabled, updated_at = ?, node_id = ? WHERE id = ?", ts, nid, id)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		case "refresh":
			go func() {
				if err := refreshBlocklistByID(id); err != nil {
					logBlocklist.Error("failed to refresh blocklist", "id", id, "error", err)
				}
			}()
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		default:
			writeError(w, http.StatusBadRequest, "Invalid action")
		}

	case "PUT":
		var req UpdateBlocklistRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}
		if !validateListURLForAPI(w, req.URL) {
			return
		}

		err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
			ts, nid := syncNow()
			if req.URL != "" {
				res, err := tx.Exec("UPDATE blocklists SET url=?,updated_at=?,node_id=? WHERE id=?", req.URL, ts, nid, id)
				if err := checkListWrite(res, err, 1); err != nil {
					return err
				}
			}
			if req.Alias != "" {
				if err := renameEntityTx(tx, "blocklists", id, req.Alias); err != nil {
					return err
				}
			}
			if req.RefreshInterval != nil {
				res, err := tx.Exec("UPDATE blocklists SET refresh_interval=?,updated_at=?,node_id=? WHERE id=?", *req.RefreshInterval, ts, nid, id)
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

func refreshBlocklistByID(id int) error {
	return refreshListByID(blockListStore, id)
}

// --- Manual blocklist helpers (mirrors allowlist.go) ---

// getOrCreateManualBlocklist returns the manual (url=”) blocklist for a client,
// creating it if it doesn't exist.
func getOrCreateManualBlocklist(clientIP string) (int64, error) {
	return getOrCreateManualList(blockListStore, manualClient, clientIP)
}

// getOrCreateGroupManualBlocklist returns the manual blocklist for a group.
func getOrCreateGroupManualBlocklist(groupID int) (int64, error) {
	return getOrCreateManualList(blockListStore, manualGroup, groupID)
}

// --- Conflict detection ---

// domainConflicts checks whether newDomain overlaps with any existing domain.
// Overlap means one covers the other: exact match, subdomain relationship, or
// wildcard pattern covering a domain. Returns the conflicting domain if found.
func domainConflicts(existingDomains []string, newDomain string) (bool, string) {
	newLower := strings.ToLower(newDomain)
	for _, existing := range existingDomains {
		existLower := strings.ToLower(existing)

		// 1. Exact match
		if newLower == existLower {
			return true, existing
		}

		// 2. New is subdomain of existing (existing covers new)
		if isSubdomainOf(newLower, existLower) {
			return true, existing
		}

		// 3. Existing is subdomain of new (new covers existing)
		if isSubdomainOf(existLower, newLower) {
			return true, existing
		}

		// 4. Wildcard: existing *.X covers new Y.X
		if strings.HasPrefix(existLower, "*.") {
			suffix := existLower[2:]
			if newLower == suffix || strings.HasSuffix(newLower, "."+suffix) {
				return true, existing
			}
		}

		// 5. Wildcard: new *.X covers existing Y.X
		if strings.HasPrefix(newLower, "*.") {
			suffix := newLower[2:]
			if existLower == suffix || strings.HasSuffix(existLower, "."+suffix) {
				return true, existing
			}
		}
	}
	return false, ""
}

// isSubdomainOf returns true if child is a subdomain of parent.
// e.g. "app.tiktok.com" is a subdomain of "tiktok.com".
func isSubdomainOf(child, parent string) bool {
	return len(child) > len(parent)+1 && strings.HasSuffix(child, "."+parent)
}

// getManualBlockDomainsForClient returns all custom block domains for a client.
func getManualBlockDomainsForClient(clientIP string) ([]string, error) {
	rows, err := db.Query(`
		SELECT bd.domain FROM blocked_domains bd
		JOIN blocklists b ON bd.blocklist_id = b.id
		JOIN client_blocklists cb ON b.id = cb.blocklist_id
		WHERE cb.client_ip = ? AND b.url = ''
	`, clientIP)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

// getManualAllowDomainsForClient returns all custom allow domains for a client.
func getManualAllowDomainsForClient(clientIP string) ([]string, error) {
	rows, err := db.Query(`
		SELECT ad.domain FROM allowed_domains ad
		JOIN allowlists a ON ad.allowlist_id = a.id
		JOIN client_allowlists ca ON a.id = ca.allowlist_id
		WHERE ca.client_ip = ? AND a.url = ''
	`, clientIP)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

// getManualBlockDomainsForGroup returns all custom block domains for a group.
func getManualBlockDomainsForGroup(groupID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT bd.domain FROM blocked_domains bd
		JOIN blocklists b ON bd.blocklist_id = b.id
		JOIN group_blocklists gb ON b.id = gb.blocklist_id
		WHERE gb.group_id = ? AND b.url = ''
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

// getManualAllowDomainsForGroup returns all custom allow domains for a group.
func getManualAllowDomainsForGroup(groupID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT ad.domain FROM allowed_domains ad
		JOIN allowlists a ON ad.allowlist_id = a.id
		JOIN group_allowlists ga ON a.id = ga.allowlist_id
		WHERE ga.group_id = ? AND a.url = ''
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

// --- Block-domain handlers ---

// handleAPIClientBlockDomain handles POST/DELETE for manual per-client domain blocks.
func handleAPIClientBlockDomain(w http.ResponseWriter, r *http.Request, clientIP string, parts []string) {
	handleManualRule(w, r, blockListStore, manualClient, clientIP, parts)
}

// handleAPIGroupBlockDomain handles POST/DELETE for manual per-group domain blocks.
func handleAPIGroupBlockDomain(w http.ResponseWriter, r *http.Request, groupID int, parts []string) {
	handleManualRule(w, r, blockListStore, manualGroup, groupID, parts)
}
