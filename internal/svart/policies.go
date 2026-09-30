package svart

import (
	"database/sql"
	"net/http"
	"strings"
)

// --- API Handlers ---

func handleAPIPoliciesRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		handleAPIGetPolicies(w, r)
	case "POST":
		handleAPICreatePolicy(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleAPIGetPolicies(w http.ResponseWriter, _ *http.Request) {
	rows, err := db.Query(`
		SELECT p.id, p.name, p.description,
			(SELECT COUNT(*) FROM policy_blocklists WHERE policy_id = p.id) as bl_count,
			(SELECT COUNT(*) FROM policy_allowlists WHERE policy_id = p.id) as al_count,
			(SELECT COUNT(*) FROM ip_ranges WHERE policy_id = p.id) +
			(SELECT COUNT(*) FROM client_groups WHERE policy_id = p.id) +
			(SELECT COUNT(*) FROM client_policies WHERE policy_id = p.id) as usage_count
		FROM policies p ORDER BY p.name`)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	var policies []PolicyView
	for rows.Next() {
		var id, blCount, alCount, usageCount int
		var name, description string
		if err := rows.Scan(&id, &name, &description, &blCount, &alCount, &usageCount); err != nil {
			writeDBError(w, err)
			return
		}
		policies = append(policies, PolicyView{ID: id, Name: name, Description: description, BlocklistCount: blCount, AllowlistCount: alCount, UsageCount: usageCount})
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	if policies == nil {
		policies = []PolicyView{}
	}
	writeJSON(w, http.StatusOK, policies)
}

func handleAPICreatePolicy(w http.ResponseWriter, r *http.Request) {
	var req CreatePolicyRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	ts, nid := syncNow()
	result, err := execPolicyWrite("INSERT INTO policies (name, description, updated_at, node_id) VALUES (?, ?, ?, ?)",
		req.Name, req.Description, ts, nid)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeError(w, http.StatusConflict, "policy name already exists")
			return
		}
		writeDBError(w, err)
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, CreatePolicyResponse{ID: id, Name: req.Name})
}

func handleAPIPolicyAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/policies/")
	parts := strings.Split(path, "/")

	policyID, validID := parsePathID(w, parts[0])
	if !validID {
		return
	}

	// /api/policies/{id}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			handleAPIGetPolicyDetail(w, r, policyID)
		case "PUT":
			handleAPIUpdatePolicy(w, r, policyID)
		case "DELETE":
			handleAPIDeletePolicy(w, r, policyID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// Sub-routes
	action := parts[1]

	// /api/policies/{id}/blocklists/{blId}
	if action == "blocklists" && len(parts) == 3 {
		blID, valid := parsePathID(w, parts[2])
		if !valid {
			return
		}
		switch r.Method {
		case "POST":
			handleAPIPolicyAddBlocklist(w, policyID, blID)
		case "DELETE":
			handleAPIPolicyRemoveBlocklist(w, policyID, blID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// /api/policies/{id}/allowlists/{alId}
	if action == "allowlists" && len(parts) == 3 {
		alID, valid := parsePathID(w, parts[2])
		if !valid {
			return
		}
		switch r.Method {
		case "POST":
			handleAPIPolicyAddAllowlist(w, policyID, alID)
		case "DELETE":
			handleAPIPolicyRemoveAllowlist(w, policyID, alID)
		default:
			writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		}
		return
	}

	// /api/policies/{id}/block-domain or /api/policies/{id}/block-domain/{domain}
	if action == "block-domain" {
		handleAPIPolicyBlockDomain(w, r, policyID, parts)
		return
	}

	// /api/policies/{id}/allow-domain or /api/policies/{id}/allow-domain/{domain}
	if action == "allow-domain" {
		handleAPIPolicyAllowDomain(w, r, policyID, parts)
		return
	}

	writeError(w, http.StatusBadRequest, "invalid action")
}

func handleAPIGetPolicyDetail(w http.ResponseWriter, r *http.Request, policyID int) {
	var name, description string
	err := db.QueryRow("SELECT name, description FROM policies WHERE id = ?", policyID).Scan(&name, &description)
	if err != nil {
		writeDBLookupError(w, err, "policy not found")
		return
	}

	// All blocklists with is_assigned flag
	blocklists, err := getAllPolicyBlocklists(policyID)
	if err != nil {
		writeDBError(w, err)
		return
	}

	// All allowlists with is_assigned flag
	allowlists, err := getAllPolicyAllowlists(policyID)
	if err != nil {
		writeDBError(w, err)
		return
	}

	isAdmin := requestIsAdmin(r)
	for i := range blocklists {
		blocklists[i].URL = urlForViewer(blocklists[i].URL, isAdmin)
	}
	for i := range allowlists {
		allowlists[i].URL = urlForViewer(allowlists[i].URL, isAdmin)
	}

	// Custom blocked domains
	customBlocked, err := getManualBlockDomainsForPolicy(policyID)
	if err != nil {
		writeDBError(w, err)
		return
	}

	// Custom allowed domains
	customAllowed, err := getManualAllowDomainsForPolicy(policyID)
	if err != nil {
		writeDBError(w, err)
		return
	}

	// Blocklist stats for assigned lists
	var assignedIDs []int
	for _, bl := range blocklists {
		if bl.IsAssigned {
			assignedIDs = append(assignedIDs, bl.ID)
		}
	}
	stats := computeBlocklistUniqueDomains(assignedIDs)

	// Assigned entities
	assignedTo, err := getPolicyAssignedEntities(policyID)
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, PolicyDetailView{
		ID:             policyID,
		Name:           name,
		Description:    description,
		Blocklists:     blocklists,
		Allowlists:     allowlists,
		CustomBlocked:  customBlocked,
		CustomAllowed:  customAllowed,
		BlocklistStats: stats,
		AssignedTo:     assignedTo,
	})
}

func handleAPIUpdatePolicy(w http.ResponseWriter, r *http.Request, policyID int) {
	var req UpdatePolicyRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		req.Name = &name
	}
	err := localMutation(snapshotPolicy, func(tx *sql.Tx) error {
		if req.Name != nil {
			if err := renameEntityTx(tx, "policies", policyID, *req.Name); err != nil {
				return err
			}
		}
		if req.Description != nil {
			ts, nid := syncNow()
			res, err := tx.Exec("UPDATE policies SET description=?,updated_at=?,node_id=? WHERE id=?", *req.Description, ts, nid, policyID)
			return checkListWrite(res, err, 1)
		}
		return nil
	})
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func handleAPIDeletePolicy(w http.ResponseWriter, _ *http.Request, policyID int) {
	var name string
	err := db.QueryRow("SELECT name FROM policies WHERE id = ?", policyID).Scan(&name)
	if err != nil {
		writeDBLookupError(w, err, "policy not found")
		return
	}

	if err := deleteLocalEntity("policies", policyID, snapshotPolicy); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

// --- Blocklist/Allowlist Toggle ---

func handleAPIPolicyAddBlocklist(w http.ResponseWriter, policyID int, blID int) {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO policy_blocklists (policy_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id",
		policyID, blID, ts, nid)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func handleAPIPolicyRemoveBlocklist(w http.ResponseWriter, policyID int, blID int) {
	if err := removeLocalRelationship("policy_blocklists", policyID, blID); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func handleAPIPolicyAddAllowlist(w http.ResponseWriter, policyID int, alID int) {
	ts, nid := syncNow()
	_, err := execPolicyWrite("INSERT INTO policy_allowlists (policy_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?) ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at,node_id=excluded.node_id",
		policyID, alID, ts, nid)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func handleAPIPolicyRemoveAllowlist(w http.ResponseWriter, policyID int, alID int) {
	if err := removeLocalRelationship("policy_allowlists", policyID, alID); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

// --- Custom Block/Allow Domain ---

func handleAPIPolicyBlockDomain(w http.ResponseWriter, r *http.Request, policyID int, parts []string) {
	handleManualRule(w, r, blockListStore, manualPolicy, policyID, parts)
}

func handleAPIPolicyAllowDomain(w http.ResponseWriter, r *http.Request, policyID int, parts []string) {
	handleManualRule(w, r, allowListStore, manualPolicy, policyID, parts)
}

// --- Helpers ---

func getOrCreatePolicyManualBlocklist(policyID int) (int64, error) {
	return getOrCreateManualList(blockListStore, manualPolicy, policyID)
}

func getOrCreatePolicyManualAllowlist(policyID int) (int64, error) {
	return getOrCreateManualList(allowListStore, manualPolicy, policyID)
}

func getAllPolicyBlocklists(policyID int) ([]PolicyBlocklistView, error) {
	rows, err := db.Query(`
		SELECT b.id, b.url, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM policy_blocklists WHERE policy_id = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b WHERE b.enabled = 1 AND b.url != '' ORDER BY b.alias`, policyID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var result []PolicyBlocklistView
	for rows.Next() {
		var id, domainCount int
		var urlStr, alias string
		var isAssigned bool
		if err := rows.Scan(&id, &urlStr, &alias, &domainCount, &isAssigned); err != nil {
			return nil, err
		}
		result = append(result, PolicyBlocklistView{ID: id, URL: urlStr, Alias: alias, DomainCount: domainCount, IsAssigned: isAssigned})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []PolicyBlocklistView{}
	}
	return result, nil
}

func getAllPolicyAllowlists(policyID int) ([]PolicyAllowlistView, error) {
	rows, err := db.Query(`
		SELECT a.id, a.url, a.alias, a.domain_count,
			EXISTS(SELECT 1 FROM policy_allowlists WHERE policy_id = ? AND allowlist_id = a.id) as is_assigned
		FROM allowlists a WHERE a.enabled = 1 AND a.url != '' ORDER BY a.alias`, policyID)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var result []PolicyAllowlistView
	for rows.Next() {
		var id, domainCount int
		var urlStr, alias string
		var isAssigned bool
		if err := rows.Scan(&id, &urlStr, &alias, &domainCount, &isAssigned); err != nil {
			return nil, err
		}
		result = append(result, PolicyAllowlistView{ID: id, URL: urlStr, Alias: alias, DomainCount: domainCount, IsAssigned: isAssigned})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if result == nil {
		result = []PolicyAllowlistView{}
	}
	return result, nil
}

func getManualBlockDomainsForPolicy(policyID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT bd.domain FROM blocked_domains bd
		JOIN blocklists b ON bd.blocklist_id = b.id
		JOIN policy_blocklists pb ON b.id = pb.blocklist_id
		WHERE pb.policy_id = ? AND b.url = ''
		ORDER BY bd.domain`, policyID)
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
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}

func getManualAllowDomainsForPolicy(policyID int) ([]string, error) {
	rows, err := db.Query(`
		SELECT ad.domain FROM allowed_domains ad
		JOIN allowlists a ON ad.allowlist_id = a.id
		JOIN policy_allowlists pa ON a.id = pa.allowlist_id
		WHERE pa.policy_id = ? AND a.url = ''
		ORDER BY ad.domain`, policyID)
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
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}

func getPolicyAssignedEntities(policyID int) (AssignedEntities, error) {
	result := AssignedEntities{Ranges: []AssignedRange{}, Groups: []PolicyReference{}, Clients: []AssignedClient{}}

	// Ranges
	rows, err := db.Query("SELECT id, name, cidr FROM ip_ranges WHERE policy_id = ?", policyID)
	if err != nil {
		return AssignedEntities{}, err
	}
	{
		defer closeQueryRows(rows)
		var ranges []AssignedRange
		for rows.Next() {
			var id int
			var name, cidr string
			if err := rows.Scan(&id, &name, &cidr); err != nil {
				return AssignedEntities{}, err
			}
			ranges = append(ranges, AssignedRange{ID: id, Name: name, CIDR: cidr})
		}
		if err := rows.Err(); err != nil {
			return AssignedEntities{}, err
		}
		if ranges != nil {
			result.Ranges = ranges
		}
	}

	// Groups
	rows2, err := db.Query("SELECT id, name FROM client_groups WHERE policy_id = ?", policyID)
	if err != nil {
		return AssignedEntities{}, err
	}
	{
		defer closeQueryRows(rows2)
		var groups []PolicyReference
		for rows2.Next() {
			var id int
			var name string
			if err := rows2.Scan(&id, &name); err != nil {
				return AssignedEntities{}, err
			}
			groups = append(groups, PolicyReference{ID: id, Name: name})
		}
		if err := rows2.Err(); err != nil {
			return AssignedEntities{}, err
		}
		if groups != nil {
			result.Groups = groups
		}
	}

	// Clients
	rows3, err := db.Query(`
		SELECT cp.client_ip, COALESCE(ca.alias, '') FROM client_policies cp
		LEFT JOIN client_aliases ca ON cp.client_ip = ca.ip_address
		WHERE cp.policy_id = ?`, policyID)
	if err != nil {
		return AssignedEntities{}, err
	}
	{
		defer closeQueryRows(rows3)
		var clients []AssignedClient
		for rows3.Next() {
			var ip, alias string
			if err := rows3.Scan(&ip, &alias); err != nil {
				return AssignedEntities{}, err
			}
			clients = append(clients, AssignedClient{IP: ip, Alias: alias})
		}
		if err := rows3.Err(); err != nil {
			return AssignedEntities{}, err
		}
		if clients != nil {
			result.Clients = clients
		}
	}

	return result, nil
}

// getClientPolicyID returns the policy_id for a client IP, or 0 if none.
