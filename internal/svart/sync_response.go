package svart

import (
	"strings"
	"time"
)

// --- Build sync response ---

func buildSyncResponse(since time.Time) (*SyncResponse, error) {
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		return nil, settingsErr
	}

	sinceStr := since.UTC().Truncate(time.Second).Format(time.RFC3339Nano)
	nodeName := settings["node_name"]
	if nodeName == "" {
		nodeName = nodeID
	}
	// Advertise our peers so others can discover the mesh
	peersStr := settings["sync_peers"]
	var knownPeers []string
	if peersStr != "" {
		for _, p := range strings.Split(peersStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				knownPeers = append(knownPeers, p)
			}
		}
	}

	resp := &SyncResponse{
		NodeID:     nodeID,
		NodeName:   nodeName,
		SelfURL:    selfURL(),
		KnownPeers: knownPeers,
		ServerTime: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// queryEach avoids holding multiple *sql.Rows open (MaxOpenConns=1)
	queryEach := func(query string, args []interface{}, fn func(rows interface{ Scan(...interface{}) error }) error) error {
		rows, err := db.Query(query, args...)
		if err != nil {
			return err
		}
		defer closeQueryRows(rows)
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}

	sinceArgs := []interface{}{sinceStr}

	// Upstreams
	if err := queryEach("SELECT upstream, enabled, COALESCE(updated_at,''), COALESCE(node_id,'') FROM upstreams WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncUpstream
			if err := r.Scan(&s.Upstream, &s.Enabled, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Upstreams = append(resp.Changes.Upstreams, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Blocklists
	if err := queryEach("SELECT alias, url, enabled, COALESCE(refresh_interval,0), COALESCE(updated_at,''), COALESCE(node_id,'') FROM blocklists WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncBlocklist
			if err := r.Scan(&s.Alias, &s.URL, &s.Enabled, &s.RefreshInterval, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Blocklists = append(resp.Changes.Blocklists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Allowlists
	if err := queryEach("SELECT alias, url, enabled, COALESCE(refresh_interval,0), COALESCE(updated_at,''), COALESCE(node_id,'') FROM allowlists WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncAllowlist
			if err := r.Scan(&s.Alias, &s.URL, &s.Enabled, &s.RefreshInterval, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Allowlists = append(resp.Changes.Allowlists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Rewrites
	if err := queryEach("SELECT domain, COALESCE(ip_addresses,''), enabled, COALESCE(updated_at,''), COALESCE(node_id,'') FROM rewrites WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncRewrite
			if err := r.Scan(&s.Domain, &s.IPAddresses, &s.Enabled, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Rewrites = append(resp.Changes.Rewrites, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Settings
	if err := queryEach("SELECT key, value, COALESCE(updated_at,''), COALESCE(node_id,'') FROM settings WHERE julianday(updated_at) >= julianday(?) AND key NOT IN ('node_name','sync_peers','deleted_peers','session_secret','sync_secret')", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncSetting
			if err := r.Scan(&s.Key, &s.Value, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Settings = append(resp.Changes.Settings, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Bootstrap servers
	if err := queryEach("SELECT server, COALESCE(updated_at,''), COALESCE(node_id,'') FROM bootstrap_servers WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncBootstrap
			if err := r.Scan(&s.Server, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.BootstrapSvrs = append(resp.Changes.BootstrapSvrs, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Client aliases
	if err := queryEach("SELECT ip_address, alias, COALESCE(updated_at,''), COALESCE(node_id,'') FROM client_aliases WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncClientAlias
			if err := r.Scan(&s.IPAddress, &s.Alias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.ClientAliases = append(resp.Changes.ClientAliases, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Identity tables leave this node only when the operator opted in; see
	// syncReplicateIdentity.
	replicateIdentity := syncReplicateIdentity.Load()

	// API tokens (hash only; plaintext tokens are never replicated)
	if replicateIdentity {
		if err := queryEach("SELECT token_prefix, name, token_hash, role, COALESCE(created_at,''), COALESCE(last_used_at,''), COALESCE(updated_at,''), COALESCE(node_id,'') FROM api_tokens WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
			func(r interface{ Scan(...interface{}) error }) error {
				var s SyncAPIToken
				if err := r.Scan(&s.TokenPrefix, &s.Name, &s.TokenHash, &s.Role, &s.CreatedAt, &s.LastUsedAt, &s.UpdatedAt, &s.NodeID); err != nil {
					return err
				}
				if !storedSyncTimestamp(s.UpdatedAt).After(since) {
					return nil
				}
				resp.Changes.APITokens = append(resp.Changes.APITokens, s)
				return nil
			}); err != nil {
			return nil, err
		}

		// Admin users (includes hash for sync)
		if err := queryEach("SELECT username, password_hash, role, COALESCE(created_at,''), COALESCE(updated_at,''), COALESCE(node_id,'') FROM admin_users WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
			func(r interface{ Scan(...interface{}) error }) error {
				var s SyncAdminUser
				if err := r.Scan(&s.Username, &s.PasswordHash, &s.Role, &s.CreatedAt, &s.UpdatedAt, &s.NodeID); err != nil {
					return err
				}
				if !storedSyncTimestamp(s.UpdatedAt).After(since) {
					return nil
				}
				resp.Changes.AdminUsers = append(resp.Changes.AdminUsers, s)
				return nil
			}); err != nil {
			return nil, err
		}
	}

	// Groups (with optional policy name)
	if err := queryEach(`SELECT cg.name, COALESCE(p.name, ''), COALESCE(cg.updated_at,''), COALESCE(cg.node_id,'')
		FROM client_groups cg LEFT JOIN policies p ON cg.policy_id = p.id
		WHERE julianday(cg.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncGroup
			if err := r.Scan(&s.Name, &s.PolicyName, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Groups = append(resp.Changes.Groups, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Ranges (with optional policy name)
	if err := queryEach(`SELECT ir.cidr, ir.name, COALESCE(p.name, ''), COALESCE(ir.updated_at,''), COALESCE(ir.node_id,'')
		FROM ip_ranges ir LEFT JOIN policies p ON ir.policy_id = p.id
		WHERE julianday(ir.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncRange
			if err := r.Scan(&s.CIDR, &s.Name, &s.PolicyName, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Ranges = append(resp.Changes.Ranges, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Group members (JOIN to get group name)
	if err := queryEach(`SELECT cg.name, cgm.client_ip, COALESCE(cgm.updated_at,''), COALESCE(cgm.node_id,'')
		FROM client_group_members cgm JOIN client_groups cg ON cgm.group_id = cg.id
		WHERE julianday(cgm.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncGroupMember
			if err := r.Scan(&s.GroupName, &s.ClientIP, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.GroupMembers = append(resp.Changes.GroupMembers, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Client blocklists (JOIN to get alias)
	if err := queryEach(`SELECT cb.client_ip, b.alias, COALESCE(cb.updated_at,''), COALESCE(cb.node_id,'')
		FROM client_blocklists cb JOIN blocklists b ON cb.blocklist_id = b.id
		WHERE julianday(cb.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncClientList
			if err := r.Scan(&s.ClientIP, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.ClientBlocklists = append(resp.Changes.ClientBlocklists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Client allowlists
	if err := queryEach(`SELECT ca.client_ip, a.alias, COALESCE(ca.updated_at,''), COALESCE(ca.node_id,'')
		FROM client_allowlists ca JOIN allowlists a ON ca.allowlist_id = a.id
		WHERE julianday(ca.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncClientList
			if err := r.Scan(&s.ClientIP, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.ClientAllowlists = append(resp.Changes.ClientAllowlists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Group blocklists
	if err := queryEach(`SELECT cg.name, b.alias, COALESCE(gb.updated_at,''), COALESCE(gb.node_id,'')
		FROM group_blocklists gb JOIN client_groups cg ON gb.group_id = cg.id JOIN blocklists b ON gb.blocklist_id = b.id
		WHERE julianday(gb.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncGroupList
			if err := r.Scan(&s.GroupName, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.GroupBlocklists = append(resp.Changes.GroupBlocklists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Group allowlists
	if err := queryEach(`SELECT cg.name, a.alias, COALESCE(ga.updated_at,''), COALESCE(ga.node_id,'')
		FROM group_allowlists ga JOIN client_groups cg ON ga.group_id = cg.id JOIN allowlists a ON ga.allowlist_id = a.id
		WHERE julianday(ga.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncGroupList
			if err := r.Scan(&s.GroupName, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.GroupAllowlists = append(resp.Changes.GroupAllowlists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Range blocklists
	if err := queryEach(`SELECT ir.cidr, b.alias, COALESCE(rb.updated_at,''), COALESCE(rb.node_id,'')
		FROM range_blocklists rb JOIN ip_ranges ir ON rb.range_id = ir.id JOIN blocklists b ON rb.blocklist_id = b.id
		WHERE julianday(rb.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncRangeList
			if err := r.Scan(&s.RangeCIDR, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.RangeBlocklists = append(resp.Changes.RangeBlocklists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Range allowlists
	if err := queryEach(`SELECT ir.cidr, a.alias, COALESCE(ra.updated_at,''), COALESCE(ra.node_id,'')
		FROM range_allowlists ra JOIN ip_ranges ir ON ra.range_id = ir.id JOIN allowlists a ON ra.allowlist_id = a.id
		WHERE julianday(ra.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncRangeList
			if err := r.Scan(&s.RangeCIDR, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.RangeAllowlists = append(resp.Changes.RangeAllowlists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Manual blocked domains (url='' means user-entered custom rules)
	if err := queryEach(`SELECT b.alias, bd.domain, COALESCE(bd.updated_at,''), COALESCE(bd.node_id,'')
		FROM blocked_domains bd JOIN blocklists b ON bd.blocklist_id = b.id
		WHERE b.url = '' AND julianday(bd.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncManualDomain
			if err := r.Scan(&s.ListAlias, &s.Domain, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.BlockedDomains = append(resp.Changes.BlockedDomains, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Manual allowed domains
	if err := queryEach(`SELECT a.alias, ad.domain, COALESCE(ad.updated_at,''), COALESCE(ad.node_id,'')
		FROM allowed_domains ad JOIN allowlists a ON ad.allowlist_id = a.id
		WHERE a.url = '' AND julianday(ad.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncManualDomain
			if err := r.Scan(&s.ListAlias, &s.Domain, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.AllowedDomains = append(resp.Changes.AllowedDomains, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Policies
	if err := queryEach("SELECT name, description, COALESCE(updated_at,''), COALESCE(node_id,'') FROM policies WHERE julianday(updated_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncPolicy
			if err := r.Scan(&s.Name, &s.Description, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.Policies = append(resp.Changes.Policies, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Policy blocklists
	if err := queryEach(`SELECT p.name, b.alias, COALESCE(pb.updated_at,''), COALESCE(pb.node_id,'')
		FROM policy_blocklists pb JOIN policies p ON pb.policy_id = p.id JOIN blocklists b ON pb.blocklist_id = b.id
		WHERE julianday(pb.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncPolicyList
			if err := r.Scan(&s.PolicyName, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.PolicyBlocklists = append(resp.Changes.PolicyBlocklists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Policy allowlists
	if err := queryEach(`SELECT p.name, a.alias, COALESCE(pa.updated_at,''), COALESCE(pa.node_id,'')
		FROM policy_allowlists pa JOIN policies p ON pa.policy_id = p.id JOIN allowlists a ON pa.allowlist_id = a.id
		WHERE julianday(pa.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncPolicyList
			if err := r.Scan(&s.PolicyName, &s.ListAlias, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.PolicyAllowlists = append(resp.Changes.PolicyAllowlists, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Client policies
	if err := queryEach(`SELECT cp.client_ip, p.name, COALESCE(cp.updated_at,''), COALESCE(cp.node_id,'')
		FROM client_policies cp JOIN policies p ON cp.policy_id = p.id
		WHERE julianday(cp.updated_at) >= julianday(?)`, sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var s SyncClientPolicy
			if err := r.Scan(&s.ClientIP, &s.PolicyName, &s.UpdatedAt, &s.NodeID); err != nil {
				return err
			}
			if !storedSyncTimestamp(s.UpdatedAt).After(since) {
				return nil
			}
			resp.Changes.ClientPolicies = append(resp.Changes.ClientPolicies, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Tombstones
	if err := queryEach("SELECT table_name, natural_key, deleted_at, node_id FROM sync_tombstones WHERE julianday(deleted_at) >= julianday(?)", sinceArgs,
		func(r interface{ Scan(...interface{}) error }) error {
			var t Tombstone
			if err := r.Scan(&t.TableName, &t.NaturalKey, &t.DeletedAt, &t.NodeID); err != nil {
				return err
			}
			if t.TableName == "settings" && isNodeLocalSyncSetting(t.NaturalKey) {
				return nil
			}
			if isIdentitySyncTable(t.TableName) && !replicateIdentity {
				return nil
			}
			if !storedSyncTimestamp(t.DeletedAt).After(since) {
				return nil
			}
			resp.Tombstones = append(resp.Tombstones, t)
			return nil
		}); err != nil {
		return nil, err
	}

	return resp, nil
}
