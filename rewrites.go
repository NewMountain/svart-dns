package main

import (
	"log"
	"strings"
	"sync"
)

type Rewrite struct {
	Domain      string
	IPAddresses []string
}

var (
	rewritesCache   sync.Map
	rewritesCacheMu sync.RWMutex
)

func loadRewritesFromDB() error {
	rows, err := db.Query("SELECT domain, ip_addresses FROM rewrites WHERE enabled = 1 AND ip_addresses IS NOT NULL AND ip_addresses != ''")
	if err != nil {
		return err
	}
	defer rows.Close()

	rewritesCacheMu.Lock()
	defer rewritesCacheMu.Unlock()

	rewritesCache = sync.Map{}

	for rows.Next() {
		var domain, ipAddresses string
		if err := rows.Scan(&domain, &ipAddresses); err != nil {
			continue
		}

		domain = strings.ToLower(strings.TrimSpace(domain))
		ips := strings.Fields(ipAddresses)

		if len(ips) > 0 {
			rewritesCache.Store(domain, Rewrite{
				Domain:      domain,
				IPAddresses: ips,
			})
		}
	}

	log.Printf("Loaded rewrites into memory cache")
	return nil
}
