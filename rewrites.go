package main

import (
	"database/sql"
	"strings"
	"sync"
	"sync/atomic"
)

type Rewrite struct {
	Domain      string
	IPAddresses []string
}

// rewritesCache holds the current domain→Rewrite map. It's an atomic.Pointer
// rather than a plain sync.Map so reloads can build a fresh map off to the
// side and publish it with a single atomic swap.
//
// Previously this was a bare sync.Map reassigned under rewritesCacheMu.Lock()
// in loadRewritesFromDB (`rewritesCache = sync.Map{}`), while the DNS hot path
// (checkRewrite in dns.go) called rewritesCache.Load() with NO lock — the
// mutex protected nothing. A query racing a reload/sync-merge could observe
// a torn/being-replaced map and miss a real rewrite, leaking an internal
// hostname upstream (breaks DD-023). The atomic pointer swap makes every
// reader see either the fully-old or fully-new map, never a partial one, with
// no lock needed on the read path.
var rewritesCache atomic.Pointer[sync.Map]

func init() {
	rewritesCache.Store(&sync.Map{})
}

type syncMapSnapshot = sync.Map

func readRewrites(q queryer) (*syncMapSnapshot, error) {
	next := &sync.Map{}
	err := scanRows(q, "SELECT domain, ip_addresses FROM rewrites WHERE enabled = 1 AND ip_addresses IS NOT NULL AND ip_addresses != ''", func(rows *sql.Rows) error {
		var domain, ips string
		if err := rows.Scan(&domain, &ips); err != nil {
			return err
		}
		domain = strings.ToLower(strings.TrimSpace(domain))
		next.Store(domain, Rewrite{Domain: domain, IPAddresses: strings.Fields(ips)})
		return nil
	})
	return next, err
}
func loadRewritesFromDB() error {
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	next, err := readRewrites(db)
	if err != nil {
		return err
	}
	rewritesCache.Store(next)
	return nil
}
