package svart

import (
	"database/sql"
	"fmt"
	"time"
)

// applySyncResponse applies admitted remote changes using LWW per row.
func applySyncResponse(resp *SyncResponse) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	// LWW helper: check by natural key, compare updated_at, insert or update
	// lww returns true if the row was inserted or updated (i.e. a change was applied).
	lww := func(table, naturalKeyCol, naturalKeyVal, incomingUpdatedAt string,
		insertSQL string, insertArgs []interface{},
		updateSQL string, updateArgs []interface{}) (bool, error) {
		return mergeSyncRow(tx, table, naturalKeyVal, incomingUpdatedAt,
			fmt.Sprintf("SELECT COALESCE(updated_at,'') FROM %s WHERE %s = ?", table, naturalKeyCol), []any{naturalKeyVal},
			insertSQL, insertArgs, updateSQL, updateArgs)
	}
	// Track per-table change counts for logging
	changes := make(map[string]int)

	// 1. Settings
	needsSyncRestart := false
	runtimeSettingsChanged := false
	loggingSettingChanged := false
	for _, s := range resp.Changes.Settings {
		// Per-node config — never replicate
		if isNodeLocalSyncSetting(s.Key) {
			continue
		}
		if err := validateSetting(s.Key, s.Value); err != nil {
			return fmt.Errorf("settings sync: %w", err)
		}
		changed, err := lww("settings", "key", s.Key, s.UpdatedAt,
			"INSERT INTO settings (key, value, updated_at, node_id) VALUES (?, ?, ?, ?)",
			[]interface{}{s.Key, s.Value, s.UpdatedAt, s.NodeID},
			"UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = ?",
			[]interface{}{s.Value, s.UpdatedAt, s.NodeID, s.Key},
		)
		if err != nil {
			return fmt.Errorf("settings sync: %w", err)
		}
		if changed {
			changes["settings"]++
			if isRuntimeSettingKey(s.Key) {
				runtimeSettingsChanged = true
			}
			if s.Key == "logging_enabled" {
				loggingSettingChanged = true
			}
			if s.Key == "sync_interval" {
				needsSyncRestart = true
			}
		}
	}

	// 2. Bootstrap servers
	for _, s := range resp.Changes.BootstrapSvrs {
		if changed, err := lww("bootstrap_servers", "server", s.Server, s.UpdatedAt,
			"INSERT INTO bootstrap_servers (server, updated_at, node_id) VALUES (?, ?, ?)",
			[]interface{}{s.Server, s.UpdatedAt, s.NodeID},
			"UPDATE bootstrap_servers SET updated_at = ?, node_id = ? WHERE server = ?",
			[]interface{}{s.UpdatedAt, s.NodeID, s.Server},
		); err != nil {
			return fmt.Errorf("bootstrap sync: %w", err)
		} else if changed {
			changes["bootstrap_servers"]++
		}
	}

	// 3. Client aliases
	for _, s := range resp.Changes.ClientAliases {
		if changed, err := lww("client_aliases", "ip_address", s.IPAddress, s.UpdatedAt,
			"INSERT INTO client_aliases (ip_address, alias, updated_at, node_id) VALUES (?, ?, ?, ?)",
			[]interface{}{s.IPAddress, s.Alias, s.UpdatedAt, s.NodeID},
			"UPDATE client_aliases SET alias = ?, updated_at = ?, node_id = ? WHERE ip_address = ?",
			[]interface{}{s.Alias, s.UpdatedAt, s.NodeID, s.IPAddress},
		); err != nil {
			return fmt.Errorf("client_aliases sync: %w", err)
		} else if changed {
			changes["clients"]++
		}
	}

	// 4. Upstreams
	for _, s := range resp.Changes.Upstreams {
		if changed, err := lww("upstreams", "upstream", s.Upstream, s.UpdatedAt,
			"INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)",
			[]interface{}{s.Upstream, s.Enabled, s.UpdatedAt, s.NodeID},
			"UPDATE upstreams SET enabled = ?, updated_at = ?, node_id = ? WHERE upstream = ?",
			[]interface{}{s.Enabled, s.UpdatedAt, s.NodeID, s.Upstream},
		); err != nil {
			return fmt.Errorf("upstreams sync: %w", err)
		} else if changed {
			changes["upstreams"]++
		}
	}

	// 5. Blocklists
	for _, s := range resp.Changes.Blocklists {
		if changed, err := lww("blocklists", "alias", s.Alias, s.UpdatedAt,
			"INSERT INTO blocklists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)",
			[]interface{}{s.URL, s.Alias, s.Enabled, s.RefreshInterval, s.UpdatedAt, s.NodeID},
			"UPDATE blocklists SET url = ?, enabled = ?, refresh_interval = ?, updated_at = ?, node_id = ? WHERE alias = ?",
			[]interface{}{s.URL, s.Enabled, s.RefreshInterval, s.UpdatedAt, s.NodeID, s.Alias},
		); err != nil {
			return fmt.Errorf("blocklists sync: %w", err)
		} else if changed {
			changes["blocklists"]++
		}
	}

	// 6. Allowlists
	for _, s := range resp.Changes.Allowlists {
		if changed, err := lww("allowlists", "alias", s.Alias, s.UpdatedAt,
			"INSERT INTO allowlists (url, alias, enabled, refresh_interval, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)",
			[]interface{}{s.URL, s.Alias, s.Enabled, s.RefreshInterval, s.UpdatedAt, s.NodeID},
			"UPDATE allowlists SET url = ?, enabled = ?, refresh_interval = ?, updated_at = ?, node_id = ? WHERE alias = ?",
			[]interface{}{s.URL, s.Enabled, s.RefreshInterval, s.UpdatedAt, s.NodeID, s.Alias},
		); err != nil {
			return fmt.Errorf("allowlists sync: %w", err)
		} else if changed {
			changes["allowlists"]++
		}
	}

	// 7. Policies (must come before Groups and Ranges which reference policy_id)
	for _, s := range resp.Changes.Policies {
		if changed, err := lww("policies", "name", s.Name, s.UpdatedAt,
			"INSERT INTO policies (name, description, updated_at, node_id) VALUES (?, ?, ?, ?)",
			[]interface{}{s.Name, s.Description, s.UpdatedAt, s.NodeID},
			"UPDATE policies SET description = ?, updated_at = ?, node_id = ? WHERE name = ?",
			[]interface{}{s.Description, s.UpdatedAt, s.NodeID, s.Name},
		); err != nil {
			return fmt.Errorf("policies sync: %w", err)
		} else if changed {
			changes["policies"]++
		}
	}

	// 8. Policy blocklists
	for _, s := range resp.Changes.PolicyBlocklists {
		blocked, err := syncTombstoneBlocks(tx, "policy_blocklists", s.PolicyName+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var policyID, blID int
		if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", s.PolicyName).Scan(&policyID); err != nil {
			return syncReferenceError("policy_blocklists", err)
		}
		if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", s.ListAlias).Scan(&blID); err != nil {
			return syncReferenceError("policy_blocklists", err)
		}
		changed, err := mergeSyncRow(tx, "policy_blocklists", s.PolicyName+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM policy_blocklists WHERE policy_id = ? AND blocklist_id = ?", []any{policyID, blID},
			"INSERT INTO policy_blocklists (policy_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{policyID, blID, s.UpdatedAt, s.NodeID},
			"UPDATE policy_blocklists SET updated_at = ?, node_id = ? WHERE policy_id = ? AND blocklist_id = ?", []any{s.UpdatedAt, s.NodeID, policyID, blID})
		if err != nil {
			return err
		}
		if changed {
			changes["policy_blocklists"]++
		}
	}

	// 9. Policy allowlists
	for _, s := range resp.Changes.PolicyAllowlists {
		blocked, err := syncTombstoneBlocks(tx, "policy_allowlists", s.PolicyName+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var policyID, alID int
		if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", s.PolicyName).Scan(&policyID); err != nil {
			return syncReferenceError("policy_allowlists", err)
		}
		if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", s.ListAlias).Scan(&alID); err != nil {
			return syncReferenceError("policy_allowlists", err)
		}
		changed, err := mergeSyncRow(tx, "policy_allowlists", s.PolicyName+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM policy_allowlists WHERE policy_id = ? AND allowlist_id = ?", []any{policyID, alID},
			"INSERT INTO policy_allowlists (policy_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{policyID, alID, s.UpdatedAt, s.NodeID},
			"UPDATE policy_allowlists SET updated_at = ?, node_id = ? WHERE policy_id = ? AND allowlist_id = ?", []any{s.UpdatedAt, s.NodeID, policyID, alID})
		if err != nil {
			return err
		}
		if changed {
			changes["policy_allowlists"]++
		}
	}

	// 10. Client policies
	for _, s := range resp.Changes.ClientPolicies {
		blocked, err := syncTombstoneBlocks(tx, "client_policies", s.ClientIP, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var policyID int
		if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", s.PolicyName).Scan(&policyID); err != nil {
			return syncReferenceError("client_policies", err)
		}
		changed, err := mergeSyncRow(tx, "client_policies", s.ClientIP, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM client_policies WHERE client_ip = ?", []any{s.ClientIP},
			"INSERT INTO client_policies (client_ip, policy_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{s.ClientIP, policyID, s.UpdatedAt, s.NodeID},
			"UPDATE client_policies SET policy_id = ?, updated_at = ?, node_id = ? WHERE client_ip = ?", []any{policyID, s.UpdatedAt, s.NodeID, s.ClientIP})
		if err != nil {
			return err
		}
		if changed {
			changes["client_policies"]++
		}
	}

	// 11. Groups (with policy_name resolution)
	for _, s := range resp.Changes.Groups {
		blocked, err := syncTombstoneBlocks(tx, "client_groups", s.Name, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		current, err := syncRowAlreadyCurrent(tx, "client_groups", "name", s.Name, s.UpdatedAt)
		if err != nil {
			return err
		}
		if current {
			continue
		}
		var policyID interface{}
		if s.PolicyName != "" {
			var pid int
			if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", s.PolicyName).Scan(&pid); err != nil {
				return syncReferenceError("policy assignment", err)
			}
			policyID = pid
		}
		if changed, err := lww("client_groups", "name", s.Name, s.UpdatedAt,
			"INSERT INTO client_groups (name, policy_id, updated_at, node_id) VALUES (?, ?, ?, ?)",
			[]interface{}{s.Name, policyID, s.UpdatedAt, s.NodeID},
			"UPDATE client_groups SET policy_id = ?, updated_at = ?, node_id = ? WHERE name = ?",
			[]interface{}{policyID, s.UpdatedAt, s.NodeID, s.Name},
		); err != nil {
			return fmt.Errorf("groups sync: %w", err)
		} else if changed {
			changes["groups"]++
		}
	}

	// 8. Ranges (with policy_name resolution)
	for _, s := range resp.Changes.Ranges {
		blocked, err := syncTombstoneBlocks(tx, "ip_ranges", s.CIDR, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		current, err := syncRowAlreadyCurrent(tx, "ip_ranges", "cidr", s.CIDR, s.UpdatedAt)
		if err != nil {
			return err
		}
		if current {
			continue
		}
		var policyID interface{}
		if s.PolicyName != "" {
			var pid int
			if err := tx.QueryRow("SELECT id FROM policies WHERE name = ?", s.PolicyName).Scan(&pid); err != nil {
				return syncReferenceError("policy assignment", err)
			}
			policyID = pid
		}
		if changed, err := lww("ip_ranges", "cidr", s.CIDR, s.UpdatedAt,
			"INSERT INTO ip_ranges (name, cidr, policy_id, updated_at, node_id) VALUES (?, ?, ?, ?, ?)",
			[]interface{}{s.Name, s.CIDR, policyID, s.UpdatedAt, s.NodeID},
			"UPDATE ip_ranges SET name = ?, policy_id = ?, updated_at = ?, node_id = ? WHERE cidr = ?",
			[]interface{}{s.Name, policyID, s.UpdatedAt, s.NodeID, s.CIDR},
		); err != nil {
			return fmt.Errorf("ranges sync: %w", err)
		} else if changed {
			changes["ranges"]++
		}
	}

	// 9. Rewrites
	for _, s := range resp.Changes.Rewrites {
		if changed, err := lww("rewrites", "domain", s.Domain, s.UpdatedAt,
			"INSERT INTO rewrites (domain, target, ip_addresses, enabled, updated_at, node_id) VALUES (?, '', ?, ?, ?, ?)",
			[]interface{}{s.Domain, s.IPAddresses, s.Enabled, s.UpdatedAt, s.NodeID},
			"UPDATE rewrites SET ip_addresses = ?, enabled = ?, updated_at = ?, node_id = ? WHERE domain = ?",
			[]interface{}{s.IPAddresses, s.Enabled, s.UpdatedAt, s.NodeID, s.Domain},
		); err != nil {
			return fmt.Errorf("rewrites sync: %w", err)
		} else if changed {
			changes["rewrites"]++
		}
	}

	// 10. API tokens
	for _, s := range resp.Changes.APITokens {
		if changed, err := lww("api_tokens", "token_prefix", s.TokenPrefix, s.UpdatedAt,
			"INSERT INTO api_tokens (name, token_prefix, token_hash, token, role, created_at, last_used_at, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			[]interface{}{s.Name, s.TokenPrefix, s.TokenHash, "", s.Role, s.CreatedAt, s.LastUsedAt, s.UpdatedAt, s.NodeID},
			"UPDATE api_tokens SET name = ?, token_hash = ?, token = ?, role = ?, last_used_at = ?, updated_at = ?, node_id = ? WHERE token_prefix = ?",
			[]interface{}{s.Name, s.TokenHash, "", s.Role, s.LastUsedAt, s.UpdatedAt, s.NodeID, s.TokenPrefix},
		); err != nil {
			return fmt.Errorf("api_tokens sync: %w", err)
		} else if changed {
			changes["tokens"]++
		}
	}

	// 10b. Admin users
	for _, s := range resp.Changes.AdminUsers {
		if changed, err := lww("admin_users", "username", s.Username, s.UpdatedAt,
			"INSERT INTO admin_users (username, password_hash, role, created_at, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?)",
			[]interface{}{s.Username, s.PasswordHash, s.Role, s.CreatedAt, s.UpdatedAt, s.NodeID},
			"UPDATE admin_users SET password_hash = ?, role = ?, updated_at = ?, node_id = ? WHERE username = ?",
			[]interface{}{s.PasswordHash, s.Role, s.UpdatedAt, s.NodeID, s.Username},
		); err != nil {
			return fmt.Errorf("admin_users sync: %w", err)
		} else if changed {
			changes["users"]++
		}
	}

	// --- Junction tables: resolve natural keys to local IDs ---

	// 11. Group members
	for _, s := range resp.Changes.GroupMembers {
		blocked, err := syncTombstoneBlocks(tx, "client_group_members", s.ClientIP+"|"+s.GroupName, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var groupID int
		if err := tx.QueryRow("SELECT id FROM client_groups WHERE name = ?", s.GroupName).Scan(&groupID); err != nil {
			return syncReferenceError("client_group_members", err)
		}
		changed, err := mergeSyncRow(tx, "client_group_members", s.ClientIP+"|"+s.GroupName, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM client_group_members WHERE group_id = ? AND client_ip = ?", []any{groupID, s.ClientIP},
			"INSERT INTO client_group_members (client_ip, group_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{s.ClientIP, groupID, s.UpdatedAt, s.NodeID},
			"UPDATE client_group_members SET updated_at = ?, node_id = ? WHERE group_id = ? AND client_ip = ?", []any{s.UpdatedAt, s.NodeID, groupID, s.ClientIP})
		if err != nil {
			return err
		}
		if changed {
			changes["client_group_members"]++
		}
	}

	// 12. Client blocklists
	for _, s := range resp.Changes.ClientBlocklists {
		blocked, err := syncTombstoneBlocks(tx, "client_blocklists", s.ClientIP+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var blID int
		if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", s.ListAlias).Scan(&blID); err != nil {
			return syncReferenceError("client_blocklists", err)
		}
		changed, err := mergeSyncRow(tx, "client_blocklists", s.ClientIP+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM client_blocklists WHERE client_ip = ? AND blocklist_id = ?", []any{s.ClientIP, blID},
			"INSERT INTO client_blocklists (client_ip, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{s.ClientIP, blID, s.UpdatedAt, s.NodeID},
			"UPDATE client_blocklists SET updated_at = ?, node_id = ? WHERE client_ip = ? AND blocklist_id = ?", []any{s.UpdatedAt, s.NodeID, s.ClientIP, blID})
		if err != nil {
			return err
		}
		if changed {
			changes["client_blocklists"]++
		}
	}

	// 13. Client allowlists
	for _, s := range resp.Changes.ClientAllowlists {
		blocked, err := syncTombstoneBlocks(tx, "client_allowlists", s.ClientIP+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var alID int
		if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", s.ListAlias).Scan(&alID); err != nil {
			return syncReferenceError("client_allowlists", err)
		}
		changed, err := mergeSyncRow(tx, "client_allowlists", s.ClientIP+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM client_allowlists WHERE client_ip = ? AND allowlist_id = ?", []any{s.ClientIP, alID},
			"INSERT INTO client_allowlists (client_ip, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{s.ClientIP, alID, s.UpdatedAt, s.NodeID},
			"UPDATE client_allowlists SET updated_at = ?, node_id = ? WHERE client_ip = ? AND allowlist_id = ?", []any{s.UpdatedAt, s.NodeID, s.ClientIP, alID})
		if err != nil {
			return err
		}
		if changed {
			changes["client_allowlists"]++
		}
	}

	// 14. Group blocklists
	for _, s := range resp.Changes.GroupBlocklists {
		blocked, err := syncTombstoneBlocks(tx, "group_blocklists", s.GroupName+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var groupID, blID int
		if err := tx.QueryRow("SELECT id FROM client_groups WHERE name = ?", s.GroupName).Scan(&groupID); err != nil {
			return syncReferenceError("group_blocklists", err)
		}
		if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", s.ListAlias).Scan(&blID); err != nil {
			return syncReferenceError("group_blocklists", err)
		}
		changed, err := mergeSyncRow(tx, "group_blocklists", s.GroupName+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM group_blocklists WHERE group_id = ? AND blocklist_id = ?", []any{groupID, blID},
			"INSERT INTO group_blocklists (group_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{groupID, blID, s.UpdatedAt, s.NodeID},
			"UPDATE group_blocklists SET updated_at = ?, node_id = ? WHERE group_id = ? AND blocklist_id = ?", []any{s.UpdatedAt, s.NodeID, groupID, blID})
		if err != nil {
			return err
		}
		if changed {
			changes["group_blocklists"]++
		}
	}

	// 15. Group allowlists
	for _, s := range resp.Changes.GroupAllowlists {
		blocked, err := syncTombstoneBlocks(tx, "group_allowlists", s.GroupName+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var groupID, alID int
		if err := tx.QueryRow("SELECT id FROM client_groups WHERE name = ?", s.GroupName).Scan(&groupID); err != nil {
			return syncReferenceError("group_allowlists", err)
		}
		if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", s.ListAlias).Scan(&alID); err != nil {
			return syncReferenceError("group_allowlists", err)
		}
		changed, err := mergeSyncRow(tx, "group_allowlists", s.GroupName+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM group_allowlists WHERE group_id = ? AND allowlist_id = ?", []any{groupID, alID},
			"INSERT INTO group_allowlists (group_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{groupID, alID, s.UpdatedAt, s.NodeID},
			"UPDATE group_allowlists SET updated_at = ?, node_id = ? WHERE group_id = ? AND allowlist_id = ?", []any{s.UpdatedAt, s.NodeID, groupID, alID})
		if err != nil {
			return err
		}
		if changed {
			changes["group_allowlists"]++
		}
	}

	// 16. Range blocklists
	for _, s := range resp.Changes.RangeBlocklists {
		blocked, err := syncTombstoneBlocks(tx, "range_blocklists", s.RangeCIDR+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var rangeID, blID int
		if err := tx.QueryRow("SELECT id FROM ip_ranges WHERE cidr = ?", s.RangeCIDR).Scan(&rangeID); err != nil {
			return syncReferenceError("range_blocklists", err)
		}
		if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ?", s.ListAlias).Scan(&blID); err != nil {
			return syncReferenceError("range_blocklists", err)
		}
		changed, err := mergeSyncRow(tx, "range_blocklists", s.RangeCIDR+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM range_blocklists WHERE range_id = ? AND blocklist_id = ?", []any{rangeID, blID},
			"INSERT INTO range_blocklists (range_id, blocklist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{rangeID, blID, s.UpdatedAt, s.NodeID},
			"UPDATE range_blocklists SET updated_at = ?, node_id = ? WHERE range_id = ? AND blocklist_id = ?", []any{s.UpdatedAt, s.NodeID, rangeID, blID})
		if err != nil {
			return err
		}
		if changed {
			changes["range_blocklists"]++
		}
	}

	// 17. Range allowlists
	for _, s := range resp.Changes.RangeAllowlists {
		blocked, err := syncTombstoneBlocks(tx, "range_allowlists", s.RangeCIDR+"|"+s.ListAlias, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var rangeID, alID int
		if err := tx.QueryRow("SELECT id FROM ip_ranges WHERE cidr = ?", s.RangeCIDR).Scan(&rangeID); err != nil {
			return syncReferenceError("range_allowlists", err)
		}
		if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ?", s.ListAlias).Scan(&alID); err != nil {
			return syncReferenceError("range_allowlists", err)
		}
		changed, err := mergeSyncRow(tx, "range_allowlists", s.RangeCIDR+"|"+s.ListAlias, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM range_allowlists WHERE range_id = ? AND allowlist_id = ?", []any{rangeID, alID},
			"INSERT INTO range_allowlists (range_id, allowlist_id, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{rangeID, alID, s.UpdatedAt, s.NodeID},
			"UPDATE range_allowlists SET updated_at = ?, node_id = ? WHERE range_id = ? AND allowlist_id = ?", []any{s.UpdatedAt, s.NodeID, rangeID, alID})
		if err != nil {
			return err
		}
		if changed {
			changes["range_allowlists"]++
		}
	}

	// 18. Manual blocked domains
	for _, s := range resp.Changes.BlockedDomains {
		blocked, err := syncTombstoneBlocks(tx, "blocked_domains", s.ListAlias+"|"+s.Domain, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var blID int
		if err := tx.QueryRow("SELECT id FROM blocklists WHERE alias = ? AND url = ''", s.ListAlias).Scan(&blID); err != nil {
			return syncReferenceError("blocked_domains", err)
		}
		changed, err := mergeSyncRow(tx, "blocked_domains", s.ListAlias+"|"+s.Domain, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM blocked_domains WHERE blocklist_id = ? AND domain = ?", []any{blID, s.Domain},
			"INSERT INTO blocked_domains (blocklist_id, domain, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{blID, s.Domain, s.UpdatedAt, s.NodeID},
			"UPDATE blocked_domains SET updated_at = ?, node_id = ? WHERE blocklist_id = ? AND domain = ?", []any{s.UpdatedAt, s.NodeID, blID, s.Domain})
		if err != nil {
			return err
		}
		if changed {
			changes["blocked_domains"]++
		}
	}

	// 19. Manual allowed domains
	for _, s := range resp.Changes.AllowedDomains {
		blocked, err := syncTombstoneBlocks(tx, "allowed_domains", s.ListAlias+"|"+s.Domain, s.UpdatedAt)
		if err != nil {
			return err
		}
		if blocked {
			continue
		}
		var alID int
		if err := tx.QueryRow("SELECT id FROM allowlists WHERE alias = ? AND url = ''", s.ListAlias).Scan(&alID); err != nil {
			return syncReferenceError("allowed_domains", err)
		}
		changed, err := mergeSyncRow(tx, "allowed_domains", s.ListAlias+"|"+s.Domain, s.UpdatedAt,
			"SELECT COALESCE(updated_at,'') FROM allowed_domains WHERE allowlist_id = ? AND domain = ?", []any{alID, s.Domain},
			"INSERT INTO allowed_domains (allowlist_id, domain, updated_at, node_id) VALUES (?, ?, ?, ?)", []any{alID, s.Domain, s.UpdatedAt, s.NodeID},
			"UPDATE allowed_domains SET updated_at = ?, node_id = ? WHERE allowlist_id = ? AND domain = ?", []any{s.UpdatedAt, s.NodeID, alID, s.Domain})
		if err != nil {
			return err
		}
		if changed {
			changes["allowed_domains"]++
		}
	}

	// 20. Tombstones — hard-delete locally, record tombstone for propagation
	for _, t := range resp.Tombstones {
		if t.TableName == "settings" && isNodeLocalSyncSetting(t.NaturalKey) {
			continue
		}
		deletedAt := storedSyncTimestamp(t.DeletedAt)
		if err := recordSyncTombstone(tx, t, deletedAt); err != nil {
			return fmt.Errorf("tombstone sync: %w", err)
		}
		if err := applyTombstone(tx, t.TableName, t.NaturalKey, deletedAt); err != nil {
			return fmt.Errorf("tombstone sync: %w", err)
		}
		if t.TableName == "bootstrap_servers" {
			changes["bootstrap_servers"]++
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Log merge details — only the tables that actually changed
	if len(changes) > 0 {
		attrs := make([]any, 0, len(changes)*2)
		for table, count := range changes {
			attrs = append(attrs, table, count)
		}
		logSync.Info("merge applied", attrs...)
	}

	if changes["bootstrap_servers"] > 0 {
		invalidateBootstrapCache()
	}
	if runtimeSettingsChanged {
		if err := loadRuntimeSettingsFromDB(); err != nil {
			return err
		}
	}
	if loggingSettingChanged {
		if err := loadLoggingSetting(); err != nil {
			return err
		}
	}

	// Reload all in-memory stores. The merge is committed either way; a
	// policy that cannot be rebuilt is logged and reported to the poller.
	if err := loadUpstreamsFromDB(); err != nil {
		return err
	}
	if err := loadRewritesFromDB(); err != nil {
		return err
	}
	if err := loadClientAliasCache(); err != nil {
		return err
	}
	if _, _, err := refreshAuthCaches(); err != nil {
		return err
	}
	scope := reloadEntities
	if syncTouchesListContent(resp) {
		scope = reloadListContent
	}
	policyErr := reloadPolicyState("sync merge", scope)

	if needsSyncRestart {
		go restartSync() // async to avoid deadlock (merge runs inside sync worker)
	}

	return policyErr
}

// syncTouchesListContent reports whether a merge may have changed the rules
// stored for a list (manual rules replicate; published list rules are
// downloaded by each node), so the list index must be rebuilt. Enabled-list
// changes are detected by the reload itself.
func syncTouchesListContent(resp *SyncResponse) bool {
	if len(resp.Changes.BlockedDomains) > 0 || len(resp.Changes.AllowedDomains) > 0 {
		return true
	}
	for _, t := range resp.Tombstones {
		switch t.TableName {
		case "blocked_domains", "allowed_domains":
			return true
		}
	}
	return false
}

// recordSyncTombstone stores a tombstone for propagation to other peers,
// keeping the latest deletion per natural key (compared as instants, not bytes).
func recordSyncTombstone(tx *sql.Tx, t Tombstone, deletedAt time.Time) error {
	var existing string
	err := tx.QueryRow("SELECT COALESCE(deleted_at,'') FROM sync_tombstones WHERE table_name = ? AND natural_key = ?",
		t.TableName, t.NaturalKey).Scan(&existing)
	if err == nil && !deletedAt.After(storedSyncTimestamp(existing)) {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	err = execSyncMutation(tx, "INSERT OR REPLACE INTO sync_tombstones (table_name, natural_key, deleted_at, node_id) VALUES (?, ?, ?, ?)",
		t.TableName, t.NaturalKey, t.DeletedAt, t.NodeID)
	return err
}
