package svart

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// maxSmallAdminRequestBodyBytes caps single-field/single-record admin
// mutation bodies (settings, bootstrap servers, group/rewrite CRUD, client
// alias) — all of these are a handful of short string/bool fields and never
// legitimately approach this size.
const maxSmallAdminRequestBodyBytes = 16 * 1024

// maxBatchAdminRequestBodyBytes caps batch admin mutation bodies (group
// blocklist/member batch assignment, rewrites batch create — capped at 1000
// entries downstream). Generous enough for realistic batch sizes while still
// bounding memory use from an oversized payload.
const maxBatchAdminRequestBodyBytes = 1024 * 1024

func handleAPISetting(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	key := r.URL.Path[len("/api/settings/"):]

	var req UpdateSettingRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}
	if key == "session_secret" {
		writeError(w, http.StatusBadRequest, "session_secret is managed internally")
		return
	}
	if key == "sync_secret" {
		if err := validateSyncSecretStrength(req.Value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := validateSetting(key, req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ts, nid := syncNow()
	kind := snapshotSettings
	if key == "sync_peers" || key == "sync_secret" || key == "sync_interval" {
		kind |= snapshotSync
	}
	err := localMutation(kind, func(tx *sql.Tx) error {
		result, err := tx.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = ?", req.Value, ts, nid, key)
		return checkListWrite(result, err, 1)
	})
	if err != nil {
		writeDBError(w, err)
		return
	}

	if isNeverExposeSetting(key) {
		writeJSON(w, http.StatusOK, SensitiveSettingUpdateResponse{Key: key, Updated: true})
		return
	}
	writeJSON(w, http.StatusOK, SettingValueResponse{Key: key, Value: req.Value})
}

func handleAPIBootstrap(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "POST":
		var req AddBootstrapServerRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if err := validateHostPort(withDefaultPort(req.Server, "53")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		ts, nid := syncNow()
		err := localMutation(snapshotNone, func(tx *sql.Tx) error {
			return addBootstrapServer(tx, req.Server, ts, nid)
		})
		if err != nil {
			writeDBError(w, err)
			return
		}

		invalidateBootstrapCache()
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	case "PUT":
		var req ReplaceBootstrapServersRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		for _, server := range req.Servers {
			if err := validateHostPort(withDefaultPort(server, "53")); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		tx, err := db.Begin()
		if err != nil {
			writeDBError(w, err)
			return
		}
		defer rollbackTransaction(tx)
		ts, nid := syncNow()
		if err := replaceBootstrapServers(tx, req.Servers, ts, nid); err != nil {
			writeDBError(w, err)
			return
		}
		if err := tx.Commit(); err != nil {
			writeDBError(w, err)
			return
		}
		invalidateBootstrapCache()

		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPICacheClear godoc
// @Summary Clear DNS cache
// @Description Clears the entire DNS response cache
// @Tags Cache
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Router /api/cache/clear [post]
func handleAPICacheClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	clearDNSCache()

	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func handleAPIClient(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/clients/"):]
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	ip := parts[0]
	if net.ParseIP(ip) == nil {
		writeError(w, http.StatusBadRequest, "invalid client IP")
		return
	}

	// GET /api/clients/{ip} — client detail
	if len(parts) == 1 && r.Method == "GET" {
		alias, totalQueries, avgLatency, firstSeen, lastSeen, err := getClientDetail(ip)
		if err != nil {
			writeDBError(w, err)
			return
		}
		groups, err := getClientGroups(ip)
		if err != nil {
			writeDBError(w, err)
			return
		}
		blocklists, err := getClientBlocklists(ip)
		if err != nil {
			writeDBError(w, err)
			return
		}
		customBlocked, err := getManualBlockDomainsForClient(ip)
		if err != nil {
			writeDBError(w, err)
			return
		}
		customAllowed, err := getManualAllowDomainsForClient(ip)
		if err != nil {
			writeDBError(w, err)
			return
		}
		if customBlocked == nil {
			customBlocked = []string{}
		}
		if customAllowed == nil {
			customAllowed = []string{}
		}
		if blocklists == nil {
			blocklists = []BlocklistAssignment{}
		}
		if groups == nil {
			groups = []Group{}
		}
		var assignedIDs []int
		for _, bl := range blocklists {
			if bl.IsAssigned {
				assignedIDs = append(assignedIDs, bl.ID)
			}
		}
		resp := ClientDetailView{
			IP:                     ip,
			Alias:                  alias,
			TotalQueries:           totalQueries,
			AvgLatencyMicroseconds: avgLatency,
			FirstSeen:              firstSeen,
			LastSeen:               lastSeen,
			Groups:                 groups,
			Blocklists:             blocklists,
			BlocklistStats:         computeBlocklistUniqueDomains(assignedIDs),
			CustomBlocked:          customBlocked,
			CustomAllowed:          customAllowed,
		}
		var pid int
		if err := readDB.QueryRow("SELECT COALESCE((SELECT policy_id FROM client_policies WHERE client_ip=?),0)", ip).Scan(&pid); err != nil {
			writeDBError(w, err)
			return
		}
		if pid > 0 {
			var policyName string
			if err := db.QueryRow("SELECT name FROM policies WHERE id = ?", pid).Scan(&policyName); err != nil {
				writeDBError(w, err)
				return
			}
			resp.Policy = &PolicyReference{ID: pid, Name: policyName}
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if len(parts) < 2 {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	action := parts[1]

	if action == "alias" && r.Method == "PUT" {
		var req UpdateClientAliasRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if err := setClientAlias(ip, req.Alias); err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		return
	}

	if action == "groups" && len(parts) == 3 {
		groupID := parts[2]
		if _, valid := parsePathID(w, groupID); !valid {
			return
		}

		switch r.Method {
		case "POST":
			if err := addClientToGroup(ip, groupID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeClientFromGroup(ip, groupID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if action == "blocklists" && len(parts) == 3 {
		blocklistID := parts[2]
		if _, valid := parsePathID(w, blocklistID); !valid {
			return
		}

		switch r.Method {
		case "POST":
			if err := addClientBlocklist(ip, blocklistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeClientBlocklist(ip, blocklistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if action == "allowlists" && len(parts) == 3 {
		allowlistID := parts[2]
		if _, valid := parsePathID(w, allowlistID); !valid {
			return
		}

		switch r.Method {
		case "POST":
			if err := addClientAllowlist(ip, allowlistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeClientAllowlist(ip, allowlistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if action == "allow-domain" {
		handleAPIClientAllowDomain(w, r, ip, parts)
		return
	}

	if action == "block-domain" {
		handleAPIClientBlockDomain(w, r, ip, parts)
		return
	}

	// /api/clients/{ip}/policy/{policyId}  or  /api/clients/{ip}/policy
	if action == "policy" {
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
			_, err := execPolicyWrite(`INSERT INTO client_policies (client_ip, policy_id, updated_at, node_id) VALUES (?, ?, ?, ?)
				ON CONFLICT(client_ip) DO UPDATE SET policy_id = ?, updated_at = ?, node_id = ?`,
				ip, pid, ts, nid, pid, ts, nid)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
				var count int
				if err := tx.QueryRow("SELECT COUNT(*) FROM client_policies WHERE client_ip=?", ip).Scan(&count); err != nil {
					return err
				}
				if err := writeLocalTombstone(tx, "client_policies", ip); err != nil {
					return err
				}
				result, err := tx.Exec("DELETE FROM client_policies WHERE client_ip=?", ip)
				return checkListWrite(result, err, int64(count))
			})
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

func handleAPIGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CreateGroupRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	id, err := createGroup(req.Name)
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, CreateGroupResponse{ID: id, Name: req.Name})
}

func handleAPIGroupAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/groups/"):]
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	groupID, validID := parsePathID(w, parts[0])
	if !validID {
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			groupName, err := getGroupName(groupID)
			if err != nil {
				writeDBLookupError(w, err, "group not found")
				return
			}
			members, err := getGroupMembers(groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			blocklists, err := getGroupBlocklists(groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			customBlocked, err := getManualBlockDomainsForGroup(groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			customAllowed, err := getManualAllowDomainsForGroup(groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			if members == nil {
				members = []Member{}
			}
			if blocklists == nil {
				blocklists = []BlocklistAssignment{}
			}
			if customBlocked == nil {
				customBlocked = []string{}
			}
			if customAllowed == nil {
				customAllowed = []string{}
			}
			// Compute unique domain stats for assigned blocklists
			var assignedIDs []int
			for _, bl := range blocklists {
				if bl.IsAssigned {
					assignedIDs = append(assignedIDs, bl.ID)
				}
			}
			totalQueries, avgLatency, firstSeen, lastSeen, err := getGroupStats(groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			recentLogs, err := getGroupRecentLogs(groupID, 20)
			if err != nil {
				writeDBError(w, err)
				return
			}
			resp := GroupDetailView{
				ID:                     groupID,
				Name:                   groupName,
				Members:                members,
				Blocklists:             blocklists,
				CustomBlocked:          customBlocked,
				CustomAllowed:          customAllowed,
				BlocklistStats:         computeBlocklistUniqueDomains(assignedIDs),
				TotalQueries:           totalQueries,
				AvgLatencyMicroseconds: avgLatency,
				FirstSeen:              firstSeen,
				LastSeen:               lastSeen,
				RecentLogs:             recentLogs,
			}
			var policyID *int
			if err := db.QueryRow("SELECT policy_id FROM client_groups WHERE id = ?", groupID).Scan(&policyID); err != nil {
				writeDBError(w, err)
				return
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
			var req UpdateGroupRequest
			if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
				return
			}

			req.Name = strings.TrimSpace(req.Name)
			if req.Name == "" {
				writeError(w, http.StatusBadRequest, "name required")
				return
			}
			if err := updateGroupName(groupID, req.Name); err != nil {
				writeDBError(w, err)
				return
			}
			// Group names are part of every decision the group makes.
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := deleteGroup(groupID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// Batch endpoints: /api/groups/{id}/blocklists/batch, /api/groups/{id}/members/batch
	if len(parts) == 3 && parts[2] == "batch" && r.Method == "POST" {
		switch parts[1] {
		case "blocklists":
			handleAPIGroupBlocklistsBatch(w, r, groupID)
			return
		case "members":
			handleAPIGroupMembersBatch(w, r, groupID)
			return
		}
	}

	if len(parts) == 3 && parts[1] == "members" {
		clientIP := parts[2]
		if net.ParseIP(clientIP) == nil {
			writeError(w, http.StatusBadRequest, "invalid client IP")
			return
		}

		switch r.Method {
		case "POST":
			if err := addClientToGroup(clientIP, fmt.Sprintf("%d", groupID)); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeClientFromGroup(clientIP, fmt.Sprintf("%d", groupID)); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if len(parts) == 3 && parts[1] == "blocklists" {
		blocklistID, validID := parsePathID(w, parts[2])
		if !validID {
			return
		}

		switch r.Method {
		case "POST":
			if err := addGroupBlocklist(groupID, blocklistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeGroupBlocklist(groupID, blocklistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if len(parts) == 3 && parts[1] == "allowlists" {
		allowlistID, validID := parsePathID(w, parts[2])
		if !validID {
			return
		}

		switch r.Method {
		case "POST":
			if err := addGroupAllowlist(groupID, allowlistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			if err := removeGroupAllowlist(groupID, allowlistID); err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	if len(parts) >= 2 && parts[1] == "allow-domain" {
		handleAPIGroupAllowDomain(w, r, groupID, parts)
		return
	}

	if len(parts) >= 2 && parts[1] == "block-domain" {
		handleAPIGroupBlockDomain(w, r, groupID, parts)
		return
	}

	// /api/groups/{id}/policy/{policyId}  or  /api/groups/{id}/policy
	if len(parts) >= 2 && parts[1] == "policy" {
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
			_, err := execPolicyWrite("UPDATE client_groups SET policy_id = ?, updated_at = ?, node_id = ? WHERE id = ?", pid, ts, nid, groupID)
			if err != nil {
				writeDBError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

		case "DELETE":
			ts, nid := syncNow()
			_, err := execPolicyWrite("UPDATE client_groups SET policy_id = NULL, updated_at = ?, node_id = ? WHERE id = ?", ts, nid, groupID)
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

// handleAPIGroupBlocklistsBatch assigns multiple blocklists to a group at once.
func handleAPIGroupBlocklistsBatch(w http.ResponseWriter, r *http.Request, groupID int) {
	var req AssignGroupBlocklistsRequest
	if !decodeJSONBody(w, r, maxBatchAdminRequestBodyBytes, &req) {
		return
	}

	for _, value := range req.BlocklistIDs {
		if value <= 0 {
			writeError(w, http.StatusBadRequest, "invalid blocklist ID")
			return
		}
	}
	err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		ts, nid := syncNow()
		for _, value := range req.BlocklistIDs {
			result, err := tx.Exec("INSERT INTO group_blocklists(group_id,blocklist_id,updated_at,node_id) VALUES(?,?,?,?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", groupID, value, ts, nid)
			if err := checkListWrite(result, err, 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AssignGroupBlocklistsResponse{Assigned: len(req.BlocklistIDs)})
}

// handleAPIGroupMembersBatch adds multiple clients to a group at once.
func handleAPIGroupMembersBatch(w http.ResponseWriter, r *http.Request, groupID int) {
	var req AddGroupMembersRequest
	if !decodeJSONBody(w, r, maxBatchAdminRequestBodyBytes, &req) {
		return
	}

	for _, value := range req.ClientIPs {
		if net.ParseIP(value) == nil {
			writeError(w, http.StatusBadRequest, "invalid client IP")
			return
		}
	}
	err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		ts, nid := syncNow()
		for _, value := range req.ClientIPs {
			result, err := tx.Exec("INSERT INTO client_group_members(client_ip,group_id,updated_at,node_id) VALUES(?,?,?,?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id", value, groupID, ts, nid)
			if err := checkListWrite(result, err, 1); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, AddGroupMembersResponse{Added: len(req.ClientIPs)})
}

// handleAPIRewrites godoc
// @Summary Create a DNS rewrite rule
// @Description Creates a new DNS rewrite rule mapping a domain to specific IP addresses
// @Tags Rewrites
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param rewrite body object true "Rewrite rule with 'domain', 'ip_addresses', and 'enabled' fields"
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/rewrites [post]
func handleAPIRewrites(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CreateRewriteRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}

	req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
	if err := validateRewrite(req.Domain, req.IPAddresses); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := createRewrite(req.Domain, req.IPAddresses, req.Enabled)
	if err != nil {
		logAdmin.Error("failed to create rewrite", "domain", req.Domain, "ip_addresses", req.IPAddresses, "error", err)
		writeDBError(w, err)
		return
	}

	logAdmin.Info("rewrite created", "rewrite_id", id, "domain", req.Domain, "ip_addresses", req.IPAddresses, "enabled", req.Enabled)

	writeJSON(w, http.StatusCreated, CreateRewriteResponse{ID: id, Domain: req.Domain, IPAddresses: req.IPAddresses, Enabled: req.Enabled})
}

func handleAPIRewriteAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/rewrites/"):]
	parts := strings.Split(path, "/")

	if len(parts) != 1 {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	rewriteID, validID := parsePathID(w, parts[0])
	if !validID {
		return
	}

	switch r.Method {
	case "PUT":
		var req UpdateRewriteRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if req.Domain == "" && req.IPAddresses == "" && req.Enabled == nil {
			writeError(w, http.StatusBadRequest, "no fields to update")
			return
		}
		if req.Domain != "" {
			req.Domain = strings.ToLower(strings.TrimSpace(req.Domain))
			if !validManualDomain(req.Domain) || strings.HasPrefix(req.Domain, "*.") {
				writeError(w, http.StatusBadRequest, "valid rewrite domain required")
				return
			}
		}
		if req.IPAddresses != "" {
			if err := validateRewrite("example.com", req.IPAddresses); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		rw, err := updateLocalRewrite(rewriteID, req)
		if err != nil {
			writeDBLookupError(w, err, "rewrite not found")
			return
		}

		writeJSON(w, http.StatusOK, rw)

	case "DELETE":
		if err := deleteRewrite(rewriteID); err != nil {
			logAdmin.Error("failed to delete rewrite", "rewrite_id", rewriteID, "error", err)
			writeDBError(w, err)
			return
		}
		logAdmin.Info("rewrite deleted", "rewrite_id", rewriteID)
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIRewritesBatch godoc
// @Summary Batch create DNS rewrite rules
// @Description Creates multiple DNS rewrite rules at once (maximum 1000 per request)
// @Tags Rewrites
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param rewrites body object true "Object with 'rewrites' array, each containing 'domain', 'ip_addresses', and 'enabled'"
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/rewrites/batch [post]
func handleAPIRewritesBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req CreateRewritesBatchRequest
	if !decodeJSONBody(w, r, maxBatchAdminRequestBodyBytes, &req) {
		return
	}

	if len(req.Rewrites) > 1000 {
		writeError(w, http.StatusBadRequest, "maximum 1000 rewrites per batch")
		return
	}

	for i := range req.Rewrites {
		rw := &req.Rewrites[i]
		rw.Domain = strings.ToLower(strings.TrimSpace(rw.Domain))
		if err := validateRewrite(rw.Domain, rw.IPAddresses); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	var created []CreatedRewriteView
	err := localMutation(snapshotRewrites, func(tx *sql.Tx) error {
		for _, rw := range req.Rewrites {
			ts, nid := syncNow()
			result, err := tx.Exec("INSERT INTO rewrites(domain,target,ip_addresses,enabled,updated_at,node_id) VALUES(?,'',?,?,?,?)", rw.Domain, rw.IPAddresses, rw.Enabled, ts, nid)
			if err := checkListWrite(result, err, 1); err != nil {
				return err
			}
			id, err := result.LastInsertId()
			if err != nil {
				return err
			}
			created = append(created, CreatedRewriteView{ID: id, Domain: rw.Domain, IPAddresses: rw.IPAddresses, Enabled: rw.Enabled})
		}
		return nil
	})
	if err != nil {
		writeDBError(w, err)
		return
	}

	if created == nil {
		created = []CreatedRewriteView{}
	}
	writeJSON(w, http.StatusCreated, created)
}
