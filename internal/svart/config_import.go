package svart

import (
	"database/sql"
	"net/http"
)

// handleAPIConfigImport godoc
// @Summary Import configuration
// @Description Validates and applies an exported configuration, merging with existing data. Reloads all in-memory stores after import.
// @Tags Config
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param config body ConfigExport true "Configuration export to import"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/config/import [post]
func handleAPIConfigImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var config ConfigExport
	if !decodeJSONBody(w, r, maxConfigImportBodyBytes, &config) {
		return
	}
	for _, l := range append(append([]ListExport{}, config.Blocklists...), config.Allowlists...) {
		if !validateListURLForAPI(w, l.URL) {
			return
		}
	}
	for key, value := range config.Settings {
		if err := validateSetting(key, value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	kind := snapshotPolicy | snapshotListContent | snapshotAliases | snapshotRewrites | snapshotUpstreams
	for key := range config.Settings {
		if isRuntimeSettingKey(key) || key == "logging_enabled" {
			kind |= snapshotSettings
		}
		if key == "sync_peers" || key == "sync_secret" || key == "sync_interval" {
			kind |= snapshotSync
		}
	}
	validationFailed := false
	err := localMutation(kind, func(tx *sql.Tx) error {
		if err := validateConfigImport(tx, &config); err != nil {
			validationFailed = true
			return err
		}
		return importConfigRows(tx, config)
	})
	if err != nil {
		if validationFailed {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if len(config.Bootstrap) > 0 {
		invalidateBootstrapCache()
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

func importConfigRows(tx *sql.Tx, config ConfigExport) error {
	ts, nid := syncNow()

	// Import upstreams
	for _, u := range config.Upstreams {
		var exists int
		if err := tx.QueryRow("SELECT COUNT(*) FROM upstreams WHERE upstream = ?", u.Upstream).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)", u.Upstream, u.Enabled, ts, nid); err != nil {
				return err
			}
		}
	}

	// Import blocklists
	for _, bl := range config.Blocklists {
		var exists int
		if err := tx.QueryRow("SELECT COUNT(*) FROM blocklists WHERE alias = ?", bl.Alias).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec("INSERT INTO blocklists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)", bl.URL, bl.Alias, bl.Enabled, bl.RefreshInterval, ts, nid); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec("UPDATE blocklists SET url = ?, enabled = ?, refresh_interval = ?, updated_at = ?, node_id = ? WHERE alias = ?", bl.URL, bl.Enabled, bl.RefreshInterval, ts, nid, bl.Alias); err != nil {
				return err
			}
		}
	}

	// Import allowlists
	for _, al := range config.Allowlists {
		var exists int
		if err := tx.QueryRow("SELECT COUNT(*) FROM allowlists WHERE alias = ?", al.Alias).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec("INSERT INTO allowlists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)", al.URL, al.Alias, al.Enabled, al.RefreshInterval, ts, nid); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec("UPDATE allowlists SET url = ?, enabled = ?, refresh_interval = ?, updated_at = ?, node_id = ? WHERE alias = ?", al.URL, al.Enabled, al.RefreshInterval, ts, nid, al.Alias); err != nil {
				return err
			}
		}
	}

	for _, spec := range []struct {
		lists                 []ListExport
		table, parent, column string
	}{
		{config.Blocklists, "blocked_domains", "blocklists", "blocklist_id"},
		{config.Allowlists, "allowed_domains", "allowlists", "allowlist_id"},
	} {
		for _, list := range spec.lists {
			if list.URL != "" {
				continue
			}
			for _, domain := range list.Domains {
				// #nosec G202 G701 -- Identifiers come from the two fixed blocklist/allowlist specifications above; imported values use placeholders.
				if _, err := tx.Exec("INSERT INTO "+spec.table+" ("+spec.column+",domain,updated_at,node_id) SELECT l.id,?,?,? FROM "+spec.parent+" l WHERE l.alias=? AND NOT EXISTS(SELECT 1 FROM "+spec.table+" d WHERE d."+spec.column+"=l.id AND d.domain=?)", domain, ts, nid, list.Alias, domain); err != nil {
					return err
				}
			}
			// #nosec G202 G701 -- Identifiers come from the two fixed blocklist/allowlist specifications above; imported values use placeholders.
			if _, err := tx.Exec("UPDATE "+spec.parent+" SET domain_count=(SELECT COUNT(*) FROM "+spec.table+" d WHERE d."+spec.column+"="+spec.parent+".id) WHERE alias=?", list.Alias); err != nil {
				return err
			}
		}
	}

	// Import rewrites
	for _, rw := range config.Rewrites {
		var exists int
		if err := tx.QueryRow("SELECT COUNT(*) FROM rewrites WHERE domain = ?", rw.Domain).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := tx.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled, updated_at, node_id) VALUES (?, '', ?, ?, ?, ?)", rw.Domain, rw.IPAddresses, rw.Enabled, ts, nid); err != nil {
				return err
			}
		}
	}

	// Import settings
	for key, value := range config.Settings {
		if isNeverExposeSetting(key) {
			continue
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO settings (key, value, updated_at, node_id) VALUES (?, ?, ?, ?)", key, value, ts, nid); err != nil {
			return err
		}
	}

	// Import bootstrap servers
	if len(config.Bootstrap) > 0 {
		if err := replaceBootstrapServers(tx, config.Bootstrap, ts, nid); err != nil {
			return err
		}
	}

	// Import policies (before groups/ranges/clients which reference them)
	for _, p := range config.Policies {
		var pid int64
		err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", p.Name).Scan(&pid)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows {
			result, err := tx.Exec("INSERT INTO policies (name, description, updated_at, node_id) VALUES (?, ?, ?, ?)", p.Name, p.Description, ts, nid)
			if err != nil {
				return err
			}
			pid, err = result.LastInsertId()
			if err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec("UPDATE policies SET description = ?, updated_at = ?, node_id = ? WHERE id = ?", p.Description, ts, nid, pid); err != nil {
				return err
			}
		}

		for _, blAlias := range p.Blocklists {
			var blID int
			if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", blAlias).Scan(&blID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO policy_blocklists (policy_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", pid, blID, ts, nid); err != nil {
				return err
			}

		}

		for _, alAlias := range p.Allowlists {
			var alID int
			if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", alAlias).Scan(&alID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO policy_allowlists (policy_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", pid, alID, ts, nid); err != nil {
				return err
			}

		}
	}

	// Import groups
	for _, g := range config.Groups {
		var gid int64
		err := tx.QueryRow("SELECT id FROM client_groups WHERE name = ?", g.Name).Scan(&gid)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows {
			result, err := tx.Exec("INSERT INTO client_groups (name, updated_at, node_id) VALUES (?, ?, ?)", g.Name, ts, nid)
			if err != nil {
				return err
			}
			gid, err = result.LastInsertId()
			if err != nil {
				return err
			}
		}

		// Resolve and set policy
		if g.Policy != "" {
			var pid int
			if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", g.Policy).Scan(&pid); err != nil {
				return err
			}

			if _, err := tx.Exec("UPDATE client_groups SET policy_id = ?, updated_at = ?, node_id = ? WHERE id = ?", pid, ts, nid, gid); err != nil {
				return err
			}

		}

		for _, memberIP := range g.Members {
			if _, err := tx.Exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?)", memberIP, gid, ts, nid); err != nil {
				return err
			}
		}

		for _, blAlias := range g.Blocklists {
			var blID int
			if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", blAlias).Scan(&blID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO group_blocklists (group_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", gid, blID, ts, nid); err != nil {
				return err
			}

		}

		for _, alAlias := range g.Allowlists {
			var alID int
			if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", alAlias).Scan(&alID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO group_allowlists (group_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", gid, alID, ts, nid); err != nil {
				return err
			}

		}
	}

	// Import ranges
	for _, rng := range config.Ranges {
		var rid int64
		err := tx.QueryRow("SELECT id FROM ip_ranges WHERE cidr = ?", rng.CIDR).Scan(&rid)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows {
			result, err := tx.Exec("INSERT INTO ip_ranges (name, cidr, updated_at, node_id) VALUES (?, ?, ?, ?)", rng.Name, rng.CIDR, ts, nid)
			if err != nil {
				return err
			}
			rid, err = result.LastInsertId()
			if err != nil {
				return err
			}
		}

		// Resolve and set policy
		if rng.Policy != "" {
			var pid int
			if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", rng.Policy).Scan(&pid); err != nil {
				return err
			}

			if _, err := tx.Exec("UPDATE ip_ranges SET policy_id = ?, updated_at = ?, node_id = ? WHERE id = ?", pid, ts, nid, rid); err != nil {
				return err
			}

		}

		for _, blAlias := range rng.Blocklists {
			var blID int
			if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", blAlias).Scan(&blID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO range_blocklists (range_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", rid, blID, ts, nid); err != nil {
				return err
			}

		}

		for _, alAlias := range rng.Allowlists {
			var alID int
			if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", alAlias).Scan(&alID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO range_allowlists (range_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", rid, alID, ts, nid); err != nil {
				return err
			}

		}
	}

	// Import client config
	for _, c := range config.Clients {
		if c.Alias != "" {
			if _, err := tx.Exec(`INSERT INTO client_aliases (ip_address, alias, updated_at, node_id)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(ip_address) DO UPDATE SET alias = ?, updated_at = ?, node_id = ?`,
				c.IP, c.Alias, ts, nid, c.Alias, ts, nid); err != nil {
				return err
			}
		}

		// Resolve and set policy
		if c.Policy != "" {
			var pid int
			if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", c.Policy).Scan(&pid); err != nil {
				return err
			}

			if _, err := tx.Exec(`INSERT INTO client_policies (client_ip, policy_id, updated_at, node_id) VALUES (?, ?, ?, ?)
					ON CONFLICT(client_ip) DO UPDATE SET policy_id = ?, updated_at = ?, node_id = ?`,
				c.IP, pid, ts, nid, pid, ts, nid); err != nil {
				return err
			}

		}

		for _, groupName := range c.Groups {
			var gid int
			if err := tx.QueryRow("SELECT id FROM client_groups WHERE name = ?", groupName).Scan(&gid); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?)", c.IP, gid, ts, nid); err != nil {
				return err
			}

		}

		for _, blAlias := range c.Blocklists {
			var blID int
			if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", blAlias).Scan(&blID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO client_blocklists (client_ip, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", c.IP, blID, ts, nid); err != nil {
				return err
			}

		}

		for _, alAlias := range c.Allowlists {
			var alID int
			if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", alAlias).Scan(&alID); err != nil {
				return err
			}

			if _, err := tx.Exec("INSERT OR IGNORE INTO client_allowlists (client_ip, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", c.IP, alID, ts, nid); err != nil {
				return err
			}

		}
	}
	return nil
}
