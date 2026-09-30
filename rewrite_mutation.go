package main

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
)

func validateRewrite(domain, ips string) error {
	if !validManualDomain(domain) || strings.HasPrefix(domain, "*.") {
		return fmt.Errorf("valid rewrite domain required")
	}
	values := strings.Fields(ips)
	if len(values) == 0 {
		return fmt.Errorf("at least one IP address required")
	}
	for _, value := range values {
		if net.ParseIP(value) == nil {
			return fmt.Errorf("invalid rewrite IP address: %s", value)
		}
	}
	return nil
}
func updateLocalRewrite(id int, req UpdateRewriteRequest) (*RewriteView, error) {
	var result RewriteView
	err := localMutation(snapshotRewrites, func(tx *sql.Tx) error {
		if err := tx.QueryRow("SELECT id,domain,COALESCE(ip_addresses,''),enabled FROM rewrites WHERE id=?", id).Scan(&result.ID, &result.Domain, &result.IPAddresses, &result.Enabled); err != nil {
			return err
		}
		if req.Domain != "" {
			if err := renameEntityTx(tx, "rewrites", id, req.Domain); err != nil {
				return err
			}
			result.Domain = req.Domain
		}
		if req.IPAddresses != "" {
			result.IPAddresses = req.IPAddresses
		}
		if req.Enabled != nil {
			result.Enabled = *req.Enabled
		}
		ts, nid := syncNow()
		res, err := tx.Exec("UPDATE rewrites SET ip_addresses=?,enabled=?,updated_at=?,node_id=? WHERE id=?", result.IPAddresses, result.Enabled, ts, nid, id)
		return checkListWrite(res, err, 1)
	})
	return &result, err
}
