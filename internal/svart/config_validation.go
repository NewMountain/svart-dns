package svart

import (
	"database/sql"
	"fmt"
	"net"
	"strings"

	"github.com/miekg/dns"

	"github.com/yeti/svart-dns/internal/policycore"
)

// Validate references against the complete incoming payload plus the transaction's
// existing configuration, before applying any part of the merge.
func validateConfigImport(tx *sql.Tx, c *ConfigExport) error {
	sets := map[string]map[string]bool{"blocklists": {}, "allowlists": {}, "policies": {}, "client_groups": {}}
	for table, names := range sets {
		column := "name"
		if table == "blocklists" || table == "allowlists" {
			column = "alias"
		}
		// #nosec G202 G701 -- Table and column names come only from the fixed reference-set map; no imported identifier is interpolated.
		rows, err := tx.Query("SELECT " + column + " FROM " + table)
		if err != nil {
			return fmt.Errorf("read import references: %w", err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				closeQueryRows(rows)
				return err
			}
			names[name] = true
		}
		err = rows.Err()
		closeQueryRows(rows)
		if err != nil {
			return err
		}
	}
	add := func(table, name string) error {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("%s name must not be empty", table)
		}
		sets[table][name] = true
		return nil
	}
	for _, lists := range [][]ListExport{c.Blocklists, c.Allowlists} {
		for _, list := range lists {
			if len(list.Domains) > 0 && list.URL != "" {
				return fmt.Errorf("inline domains require a manual list with an empty URL")
			}
			for _, domain := range list.Domains {
				if reservedStoredRule(domain) {
					return fmt.Errorf("manual list %q contains a reserved encoded rule", list.Alias)
				}
				if strings.TrimSpace(domain) == "" || strings.ContainsAny(domain, "\r\n\x00") {
					return fmt.Errorf("manual list %q contains an empty or multiline rule", list.Alias)
				}
			}
		}
	}

	for _, l := range c.Blocklists {
		if err := add("blocklists", l.Alias); err != nil {
			return err
		}
		if l.RefreshInterval < 0 {
			return fmt.Errorf("blocklist refresh_interval must not be negative")
		}
	}
	for _, l := range c.Allowlists {
		if err := add("allowlists", l.Alias); err != nil {
			return err
		}
		if l.RefreshInterval < 0 {
			return fmt.Errorf("allowlist refresh_interval must not be negative")
		}
	}
	for _, p := range c.Policies {
		if err := add("policies", p.Name); err != nil {
			return err
		}
	}
	for _, g := range c.Groups {
		if err := add("client_groups", g.Name); err != nil {
			return err
		}
	}
	ref := func(table, name string) error {
		if !sets[table][name] {
			return fmt.Errorf("unknown %s reference %q", table, name)
		}
		return nil
	}
	lists := func(block, allow []string) error {
		for _, n := range block {
			if err := ref("blocklists", n); err != nil {
				return err
			}
		}
		for _, n := range allow {
			if err := ref("allowlists", n); err != nil {
				return err
			}
		}
		return nil
	}
	policy := func(name string) error {
		if name != "" {
			return ref("policies", name)
		}
		return nil
	}
	ip := func(raw string) error {
		if net.ParseIP(raw) == nil {
			return fmt.Errorf("invalid client IP %q", raw)
		}
		return nil
	}
	for _, u := range c.Upstreams {
		if _, err := parseUpstreamSpec(u.Upstream); err != nil {
			return err
		}
	}
	for _, s := range c.Bootstrap {
		if err := validateHostPort(withDefaultPort(s, "53")); err != nil {
			return fmt.Errorf("invalid bootstrap server: %w", err)
		}
	}
	for _, r := range c.Rewrites {
		if _, ok := dns.IsDomainName(r.Domain); !ok || strings.TrimSpace(r.Domain) == "" || strings.ContainsAny(r.Domain, " \t\n") {
			return fmt.Errorf("invalid rewrite domain %q", r.Domain)
		}
		addresses := strings.Fields(r.IPAddresses)
		if len(addresses) == 0 {
			return fmt.Errorf("rewrite %q requires IP addresses", r.Domain)
		}
		for _, address := range addresses {
			if err := ip(address); err != nil {
				return err
			}
		}
	}
	for _, p := range c.Policies {
		if err := lists(p.Blocklists, p.Allowlists); err != nil {
			return err
		}
	}
	for _, g := range c.Groups {
		if err := policy(g.Policy); err != nil {
			return err
		}
		if err := lists(g.Blocklists, g.Allowlists); err != nil {
			return err
		}
		for _, m := range g.Members {
			if err := ip(m); err != nil {
				return err
			}
		}
	}
	for _, r := range c.Ranges {
		if _, err := policycore.ParseRangeCIDR(r.CIDR); err != nil {
			return fmt.Errorf("invalid range CIDR: %w", err)
		}
		if err := policy(r.Policy); err != nil {
			return err
		}
		if err := lists(r.Blocklists, r.Allowlists); err != nil {
			return err
		}
	}
	for _, client := range c.Clients {
		if err := ip(client.IP); err != nil {
			return err
		}
		if err := policy(client.Policy); err != nil {
			return err
		}
		if err := lists(client.Blocklists, client.Allowlists); err != nil {
			return err
		}
		for _, g := range client.Groups {
			if err := ref("client_groups", g); err != nil {
				return err
			}
		}
	}
	return nil
}
