package main

import (
	"fmt"
	"net/http"
	"time"
)

// maxConfigImportBodyBytes caps the config import request body. A full
// export (upstreams, blocklists, allowlists, rewrites, policies, groups,
// ranges, settings, bootstrap servers, clients) is normally well under 1MB
// even for a large deployment; 8MB is a generous ceiling that still bounds
// memory use from an admin-authenticated but oversized/malicious payload.
const maxConfigImportBodyBytes = 8 * 1024 * 1024

// ConfigExport is the full config export schema.
type ConfigExport struct {
	Version    string            `json:"version"`
	ExportedAt string            `json:"exported_at"`
	Upstreams  []UpstreamExport  `json:"upstreams"`
	Blocklists []ListExport      `json:"blocklists"`
	Allowlists []ListExport      `json:"allowlists"`
	Rewrites   []RewriteExport   `json:"rewrites"`
	Policies   []PolicyExport    `json:"policies"`
	Groups     []GroupExport     `json:"groups"`
	Ranges     []RangeExport     `json:"ranges"`
	Settings   map[string]string `json:"settings"`
	Bootstrap  []string          `json:"bootstrap_servers"`
	Clients    []ClientExport    `json:"clients"`
}

type UpstreamExport struct {
	Upstream string `json:"upstream"`
	Enabled  bool   `json:"enabled"`
}

type ListExport struct {
	Domains         []string `json:"domains,omitempty"`
	URL             string   `json:"url"`
	Alias           string   `json:"alias"`
	Enabled         bool     `json:"enabled"`
	RefreshInterval int      `json:"refresh_interval"`
}

type RewriteExport struct {
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}

type PolicyExport struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Blocklists  []string `json:"blocklists"` // by alias
	Allowlists  []string `json:"allowlists"` // by alias
}

type GroupExport struct {
	Name       string   `json:"name"`
	Policy     string   `json:"policy,omitempty"` // by name
	Members    []string `json:"members"`
	Blocklists []string `json:"blocklists"` // by alias
	Allowlists []string `json:"allowlists"` // by alias
}

type RangeExport struct {
	Name       string   `json:"name"`
	CIDR       string   `json:"cidr"`
	Policy     string   `json:"policy,omitempty"` // by name
	Blocklists []string `json:"blocklists"`       // by alias
	Allowlists []string `json:"allowlists"`       // by alias
}

type ClientExport struct {
	IP         string   `json:"ip"`
	Alias      string   `json:"alias,omitempty"`
	Policy     string   `json:"policy,omitempty"`     // by name
	Groups     []string `json:"groups,omitempty"`     // by name
	Blocklists []string `json:"blocklists,omitempty"` // by alias
	Allowlists []string `json:"allowlists,omitempty"` // by alias
}

// handleAPIConfigExport godoc
// @Summary Export full configuration
// @Description Exports the complete server configuration including upstreams, blocklists, allowlists, rewrites, groups, ranges, settings, bootstrap servers, and clients
// @Tags Config
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 503 {object} apiResponse
// @Router /api/config/export [get]
func handleAPIConfigExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	export, err := buildConfigExport()
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, export)
}

func buildConfigExport() (*ConfigExport, error) {
	// Every component belongs to the same WAL snapshot, while writers continue
	// through their own pool. Mixing committed versions can misassign rules.
	tx, err := readDB.Begin()
	if err != nil {
		return nil, err
	}
	defer rollbackTransaction(tx)
	export := &ConfigExport{
		Version:    "1.0",
		ExportedAt: time.Now().Format(time.RFC3339),
		Upstreams:  []UpstreamExport{},
		Blocklists: []ListExport{},
		Allowlists: []ListExport{},
		Rewrites:   []RewriteExport{},
		Policies:   []PolicyExport{},
		Groups:     []GroupExport{},
		Ranges:     []RangeExport{},
		Settings:   make(map[string]string),
		Bootstrap:  []string{},
		Clients:    []ClientExport{},
	}

	// queryEach runs a query, scans each row, then closes. Only one *sql.Rows
	// is open at a time, avoiding deadlock with MaxOpenConns(1).
	queryEach := func(query string, args []interface{}, fn func(rows interface{ Scan(...interface{}) error }) error) error {
		rows, err := tx.Query(query, args...)
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

	// Upstreams
	if err := queryEach("SELECT upstream, enabled FROM upstreams ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var u UpstreamExport
			if err := r.Scan(&u.Upstream, &u.Enabled); err != nil {
				return err
			}
			export.Upstreams = append(export.Upstreams, u)
			return nil
		}); err != nil {
		return nil, err
	}

	// Blocklists
	if err := queryEach("SELECT url, alias, enabled, COALESCE(refresh_interval,0) FROM blocklists ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var l ListExport
			if err := r.Scan(&l.URL, &l.Alias, &l.Enabled, &l.RefreshInterval); err != nil {
				return err
			}
			export.Blocklists = append(export.Blocklists, l)
			return nil
		}); err != nil {
		return nil, err
	}

	// Allowlists
	if err := queryEach("SELECT url, alias, enabled, COALESCE(refresh_interval,0) FROM allowlists ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var l ListExport
			if err := r.Scan(&l.URL, &l.Alias, &l.Enabled, &l.RefreshInterval); err != nil {
				return err
			}
			export.Allowlists = append(export.Allowlists, l)
			return nil
		}); err != nil {
		return nil, err
	}

	for _, spec := range []struct {
		lists         *[]ListExport
		table, column string
	}{
		{&export.Blocklists, "blocked_domains", "blocklist_id"},
		{&export.Allowlists, "allowed_domains", "allowlist_id"},
	} {
		table := "blocklists"
		if spec.table == "allowed_domains" {
			table = "allowlists"
		}
		byAlias := make(map[string]int)
		for i, list := range *spec.lists {
			byAlias[list.Alias] = i
		}
		if err := queryEach("SELECT l.alias,d.domain FROM "+spec.table+" d LEFT JOIN "+table+" l ON l.id=d."+spec.column+" WHERE l.url='' OR l.id IS NULL ORDER BY l.alias,d.domain", nil,
			func(r interface{ Scan(...interface{}) error }) error {
				var alias, domain string
				if err := r.Scan(&alias, &domain); err != nil {
					return err
				}
				i, ok := byAlias[alias]
				if !ok {
					return fmt.Errorf("export %s: missing list alias %q", spec.table, alias)
				}
				(*spec.lists)[i].Domains = append((*spec.lists)[i].Domains, domain)
				return nil
			}); err != nil {
			return nil, err
		}
	}

	// Rewrites
	if err := queryEach("SELECT domain, COALESCE(ip_addresses, ''), enabled FROM rewrites ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var rw RewriteExport
			if err := r.Scan(&rw.Domain, &rw.IPAddresses, &rw.Enabled); err != nil {
				return err
			}
			export.Rewrites = append(export.Rewrites, rw)
			return nil
		}); err != nil {
		return nil, err
	}

	// Settings
	if err := queryEach("SELECT key, value FROM settings ORDER BY key", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var k, v string
			if err := r.Scan(&k, &v); err != nil {
				return err
			}
			if isNeverExposeSetting(k) {
				return nil
			}
			export.Settings[k] = v
			return nil
		}); err != nil {
		return nil, err
	}

	// Bootstrap servers
	if err := queryEach("SELECT server FROM bootstrap_servers ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var s string
			if err := r.Scan(&s); err != nil {
				return err
			}
			export.Bootstrap = append(export.Bootstrap, s)
			return nil
		}); err != nil {
		return nil, err
	}

	// Aliases of every list, enabled or not: a disabled list stays assigned.
	blocklistAliases, allowlistAliases := map[int]string{}, map[int]string{}
	for _, t := range []struct {
		query   string
		aliases map[int]string
	}{
		{"SELECT id, alias FROM blocklists", blocklistAliases},
		{"SELECT id, alias FROM allowlists", allowlistAliases},
	} {
		if err := queryEach(t.query, nil, func(r interface{ Scan(...interface{}) error }) error {
			var id int
			var alias string
			if err := r.Scan(&id, &alias); err != nil {
				return err
			}
			t.aliases[id] = alias
			return nil
		}); err != nil {
			return nil, err
		}
	}

	// Policies — export with list assignments by alias
	type policyInfo struct {
		id          int
		name        string
		description string
	}
	var policies []policyInfo
	if err := queryEach("SELECT id, name, description FROM policies ORDER BY name", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var p policyInfo
			if err := r.Scan(&p.id, &p.name, &p.description); err != nil {
				return err
			}
			policies = append(policies, p)
			return nil
		}); err != nil {
		return nil, err
	}

	policiesByID := make(map[int]bool, len(policies))
	for _, item := range policies {
		policiesByID[item.id] = true
	}

	policyBlocklists := make(map[int][]string)
	if err := queryEach("SELECT policy_id, blocklist_id FROM policy_blocklists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var pid, bid int
			if err := r.Scan(&pid, &bid); err != nil {
				return err
			}
			if !policiesByID[pid] {
				return fmt.Errorf("export policies assignments: missing parent %d", pid)
			}
			alias, ok := blocklistAliases[bid]
			if !ok {
				return fmt.Errorf("export policy assignments: missing list %d", bid)
			}
			policyBlocklists[pid] = append(policyBlocklists[pid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	policyAllowlists := make(map[int][]string)
	if err := queryEach("SELECT policy_id, allowlist_id FROM policy_allowlists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var pid, aid int
			if err := r.Scan(&pid, &aid); err != nil {
				return err
			}
			if !policiesByID[pid] {
				return fmt.Errorf("export policies assignments: missing parent %d", pid)
			}
			alias, ok := allowlistAliases[aid]
			if !ok {
				return fmt.Errorf("export policy assignments: missing list %d", aid)
			}
			policyAllowlists[pid] = append(policyAllowlists[pid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	// Build policy name lookup for entity references
	policyNames := make(map[int]string)
	for _, p := range policies {
		policyNames[p.id] = p.name
		pe := PolicyExport{
			Name:        p.name,
			Description: p.description,
			Blocklists:  orEmpty(policyBlocklists[p.id]),
			Allowlists:  orEmpty(policyAllowlists[p.id]),
		}
		export.Policies = append(export.Policies, pe)
	}

	// Groups — fetch all sub-data in bulk (3 queries) instead of N+1
	type groupInfo struct {
		id       int
		name     string
		policyID int
	}
	var groups []groupInfo
	if err := queryEach("SELECT id, name, COALESCE(policy_id, 0) FROM client_groups ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var g groupInfo
			if err := r.Scan(&g.id, &g.name, &g.policyID); err != nil {
				return err
			}
			groups = append(groups, g)
			return nil
		}); err != nil {
		return nil, err
	}

	groupsByID := make(map[int]bool, len(groups))
	for _, item := range groups {
		groupsByID[item.id] = true
	}

	// Bulk fetch group members, blocklists, allowlists
	groupMembers := make(map[int][]string)
	if err := queryEach("SELECT group_id, client_ip FROM client_group_members ORDER BY group_id, client_ip", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var gid int
			var ip string
			if err := r.Scan(&gid, &ip); err != nil {
				return err
			}
			if !groupsByID[gid] {
				return fmt.Errorf("export groups assignments: missing parent %d", gid)
			}
			groupMembers[gid] = append(groupMembers[gid], ip)
			return nil
		}); err != nil {
		return nil, err
	}

	groupBlocklists := make(map[int][]string)
	if err := queryEach("SELECT group_id, blocklist_id FROM group_blocklists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var gid, bid int
			if err := r.Scan(&gid, &bid); err != nil {
				return err
			}
			if !groupsByID[gid] {
				return fmt.Errorf("export groups assignments: missing parent %d", gid)
			}
			alias, ok := blocklistAliases[bid]
			if !ok {
				return fmt.Errorf("export group assignments: missing list %d", bid)
			}
			groupBlocklists[gid] = append(groupBlocklists[gid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	groupAllowlists := make(map[int][]string)
	if err := queryEach("SELECT group_id, allowlist_id FROM group_allowlists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var gid, aid int
			if err := r.Scan(&gid, &aid); err != nil {
				return err
			}
			if !groupsByID[gid] {
				return fmt.Errorf("export groups assignments: missing parent %d", gid)
			}
			alias, ok := allowlistAliases[aid]
			if !ok {
				return fmt.Errorf("export group assignments: missing list %d", aid)
			}
			groupAllowlists[gid] = append(groupAllowlists[gid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	for _, g := range groups {
		if g.policyID != 0 {
			if _, ok := policyNames[g.policyID]; !ok {
				return nil, fmt.Errorf("export groups: missing policy %d", g.policyID)
			}
		}
		ge := GroupExport{
			Name:       g.name,
			Policy:     policyNames[g.policyID],
			Members:    orEmpty(groupMembers[g.id]),
			Blocklists: orEmpty(groupBlocklists[g.id]),
			Allowlists: orEmpty(groupAllowlists[g.id]),
		}
		export.Groups = append(export.Groups, ge)
	}

	// Ranges — bulk fetch sub-data (2 queries) instead of N+1
	type rangeInfo struct {
		id       int
		name     string
		cidr     string
		policyID int
	}
	var ranges []rangeInfo
	if err := queryEach("SELECT id, name, cidr, COALESCE(policy_id, 0) FROM ip_ranges ORDER BY id", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ri rangeInfo
			if err := r.Scan(&ri.id, &ri.name, &ri.cidr, &ri.policyID); err != nil {
				return err
			}
			ranges = append(ranges, ri)
			return nil
		}); err != nil {
		return nil, err
	}

	rangesByID := make(map[int]bool, len(ranges))
	for _, item := range ranges {
		rangesByID[item.id] = true
	}

	rangeBlocklists := make(map[int][]string)
	if err := queryEach("SELECT range_id, blocklist_id FROM range_blocklists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var rid, bid int
			if err := r.Scan(&rid, &bid); err != nil {
				return err
			}
			if !rangesByID[rid] {
				return fmt.Errorf("export ranges assignments: missing parent %d", rid)
			}
			alias, ok := blocklistAliases[bid]
			if !ok {
				return fmt.Errorf("export range assignments: missing list %d", bid)
			}
			rangeBlocklists[rid] = append(rangeBlocklists[rid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	rangeAllowlists := make(map[int][]string)
	if err := queryEach("SELECT range_id, allowlist_id FROM range_allowlists", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var rid, aid int
			if err := r.Scan(&rid, &aid); err != nil {
				return err
			}
			if !rangesByID[rid] {
				return fmt.Errorf("export ranges assignments: missing parent %d", rid)
			}
			alias, ok := allowlistAliases[aid]
			if !ok {
				return fmt.Errorf("export range assignments: missing list %d", aid)
			}
			rangeAllowlists[rid] = append(rangeAllowlists[rid], alias)
			return nil
		}); err != nil {
		return nil, err
	}

	for _, ri := range ranges {
		if ri.policyID != 0 {
			if _, ok := policyNames[ri.policyID]; !ok {
				return nil, fmt.Errorf("export ranges: missing policy %d", ri.policyID)
			}
		}
		re := RangeExport{
			Name:       ri.name,
			CIDR:       ri.cidr,
			Policy:     policyNames[ri.policyID],
			Blocklists: orEmpty(rangeBlocklists[ri.id]),
			Allowlists: orEmpty(rangeAllowlists[ri.id]),
		}
		export.Ranges = append(export.Ranges, re)
	}

	// Clients (those with aliases, group memberships, or list assignments)
	clientMap := make(map[string]*ClientExport)

	if err := queryEach("SELECT ip_address, alias FROM client_aliases ORDER BY ip_address", nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ip, alias string
			if err := r.Scan(&ip, &alias); err != nil {
				return err
			}
			getOrCreateClientExport(clientMap, ip).Alias = alias
			return nil
		}); err != nil {
		return nil, err
	}

	if err := queryEach(`SELECT cgm.client_ip, cg.name FROM client_group_members cgm
		LEFT JOIN client_groups cg ON cgm.group_id = cg.id
		ORDER BY cgm.client_ip, cg.name`, nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ip, groupName string
			if err := r.Scan(&ip, &groupName); err != nil {
				return err
			}
			ce := getOrCreateClientExport(clientMap, ip)
			ce.Groups = append(ce.Groups, groupName)
			return nil
		}); err != nil {
		return nil, err
	}

	if err := queryEach(`SELECT cb.client_ip, b.alias FROM client_blocklists cb
		LEFT JOIN blocklists b ON cb.blocklist_id = b.id
		ORDER BY cb.client_ip, b.alias`, nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ip, blAlias string
			if err := r.Scan(&ip, &blAlias); err != nil {
				return err
			}
			ce := getOrCreateClientExport(clientMap, ip)
			ce.Blocklists = append(ce.Blocklists, blAlias)
			return nil
		}); err != nil {
		return nil, err
	}

	if err := queryEach(`SELECT ca.client_ip, a.alias FROM client_allowlists ca
		LEFT JOIN allowlists a ON ca.allowlist_id = a.id
		ORDER BY ca.client_ip, a.alias`, nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ip, alAlias string
			if err := r.Scan(&ip, &alAlias); err != nil {
				return err
			}
			ce := getOrCreateClientExport(clientMap, ip)
			ce.Allowlists = append(ce.Allowlists, alAlias)
			return nil
		}); err != nil {
		return nil, err
	}

	if err := queryEach(`SELECT cp.client_ip, p.name FROM client_policies cp
		LEFT JOIN policies p ON cp.policy_id = p.id
		ORDER BY cp.client_ip`, nil,
		func(r interface{ Scan(...interface{}) error }) error {
			var ip, pName string
			if err := r.Scan(&ip, &pName); err != nil {
				return err
			}
			getOrCreateClientExport(clientMap, ip).Policy = pName
			return nil
		}); err != nil {
		return nil, err
	}

	for _, ce := range clientMap {
		if ce.Groups == nil {
			ce.Groups = []string{}
		}
		if ce.Blocklists == nil {
			ce.Blocklists = []string{}
		}
		if ce.Allowlists == nil {
			ce.Allowlists = []string{}
		}
		export.Clients = append(export.Clients, *ce)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return export, nil
}

// orEmpty returns s if non-nil, otherwise an empty string slice (for clean JSON).
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func getOrCreateClientExport(m map[string]*ClientExport, ip string) *ClientExport {
	if ce, ok := m[ip]; ok {
		return ce
	}
	ce := &ClientExport{IP: ip}
	m[ip] = ce
	return ce
}
