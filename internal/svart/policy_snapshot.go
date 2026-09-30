package svart

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yeti/svart-dns/internal/listparse"

	"github.com/yeti/svart-dns/internal/policycore"
)

// queryer is what reading the policy configuration needs: a *sql.Tx in
// production, so every table is read from one consistent database snapshot.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// scanRows runs query and calls scan for each row, returning the first error.
func scanRows(q queryer, query string, scan func(*sql.Rows) error, args ...any) error {
	rows, err := q.Query(query, args...)
	if err != nil {
		return err
	}
	defer closeQueryRows(rows)
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// readPolicyConfig reads every table the policy snapshot is built from.
func readPolicyConfig(q queryer) (policycore.Config, error) {
	cfg := policycore.Config{
		Groups:   make(map[int]policycore.GroupConfig),
		Clients:  make(map[string]*policycore.ListIDs),
		Policies: make(map[int]*policycore.ListIDs),
	}
	entry := func(m map[string]*policycore.ListIDs, key string) *policycore.ListIDs {
		if m[key] == nil {
			m[key] = &policycore.ListIDs{}
		}
		return m[key]
	}
	policy := func(id int) *policycore.ListIDs {
		if cfg.Policies[id] == nil {
			cfg.Policies[id] = &policycore.ListIDs{}
		}
		return cfg.Policies[id]
	}
	rangeIndex := map[int]int{}
	withRange := func(id int, add func(*policycore.RangeConfig)) {
		if i, ok := rangeIndex[id]; ok {
			add(&cfg.Ranges[i])
		}
	}
	withGroup := func(id int, add func(*policycore.GroupConfig)) {
		if g, ok := cfg.Groups[id]; ok {
			add(&g)
			cfg.Groups[id] = g
		}
	}

	steps := []struct {
		what, query string
		scan        func(*sql.Rows) error
	}{
		{"blocklists", "SELECT id, alias, url = '', domain_count FROM blocklists WHERE enabled = 1 ORDER BY id", func(r *sql.Rows) error {
			l := policycore.List{}
			err := r.Scan(&l.ID, &l.Name, &l.Manual, &l.Count)
			cfg.Lists = append(cfg.Lists, l)
			return err
		}},
		{"allowlists", "SELECT id, alias, url = '', domain_count FROM allowlists WHERE enabled = 1 ORDER BY id", func(r *sql.Rows) error {
			l := policycore.List{Allow: true}
			err := r.Scan(&l.ID, &l.Name, &l.Manual, &l.Count)
			cfg.Lists = append(cfg.Lists, l)
			return err
		}},
		{"policy blocklists", "SELECT policy_id, blocklist_id FROM policy_blocklists", func(r *sql.Rows) error {
			var pid, id int
			err := r.Scan(&pid, &id)
			policy(pid).Block = append(policy(pid).Block, id)
			return err
		}},
		{"policy allowlists", "SELECT policy_id, allowlist_id FROM policy_allowlists", func(r *sql.Rows) error {
			var pid, id int
			err := r.Scan(&pid, &id)
			policy(pid).Allow = append(policy(pid).Allow, id)
			return err
		}},
		{"ranges", "SELECT id, name, cidr, COALESCE(policy_id, 0) FROM ip_ranges ORDER BY id", func(r *sql.Rows) error {
			var rc policycore.RangeConfig
			err := r.Scan(&rc.ID, &rc.Name, &rc.CIDR, &rc.PolicyID)
			rangeIndex[rc.ID] = len(cfg.Ranges)
			cfg.Ranges = append(cfg.Ranges, rc)
			return err
		}},
		{"range blocklists", "SELECT range_id, blocklist_id FROM range_blocklists", func(r *sql.Rows) error {
			var rid, id int
			err := r.Scan(&rid, &id)
			withRange(rid, func(rc *policycore.RangeConfig) { rc.Block = append(rc.Block, id) })
			return err
		}},
		{"range allowlists", "SELECT range_id, allowlist_id FROM range_allowlists", func(r *sql.Rows) error {
			var rid, id int
			err := r.Scan(&rid, &id)
			withRange(rid, func(rc *policycore.RangeConfig) { rc.Allow = append(rc.Allow, id) })
			return err
		}},
		{"groups", "SELECT id, name, COALESCE(policy_id, 0) FROM client_groups", func(r *sql.Rows) error {
			var id int
			var g policycore.GroupConfig
			err := r.Scan(&id, &g.Name, &g.PolicyID)
			cfg.Groups[id] = g
			return err
		}},
		{"group blocklists", "SELECT group_id, blocklist_id FROM group_blocklists", func(r *sql.Rows) error {
			var gid, id int
			err := r.Scan(&gid, &id)
			withGroup(gid, func(g *policycore.GroupConfig) { g.Block = append(g.Block, id) })
			return err
		}},
		{"group allowlists", "SELECT group_id, allowlist_id FROM group_allowlists", func(r *sql.Rows) error {
			var gid, id int
			err := r.Scan(&gid, &id)
			withGroup(gid, func(g *policycore.GroupConfig) { g.Allow = append(g.Allow, id) })
			return err
		}},
		{"group members", "SELECT cgm.client_ip, cgm.group_id FROM client_group_members cgm JOIN client_groups cg ON cgm.group_id = cg.id", func(r *sql.Rows) error {
			var m policycore.GroupMembership
			err := r.Scan(&m.ClientIP, &m.GroupID)
			cfg.Memberships = append(cfg.Memberships, m)
			return err
		}},
		{"client blocklists", "SELECT client_ip, blocklist_id FROM client_blocklists WHERE client_ip <> ''", func(r *sql.Rows) error {
			var ip string
			var id int
			err := r.Scan(&ip, &id)
			e := entry(cfg.Clients, ip)
			e.Block = append(e.Block, id)
			return err
		}},
		{"client allowlists", "SELECT client_ip, allowlist_id FROM client_allowlists WHERE client_ip <> ''", func(r *sql.Rows) error {
			var ip string
			var id int
			err := r.Scan(&ip, &id)
			e := entry(cfg.Clients, ip)
			e.Allow = append(e.Allow, id)
			return err
		}},
		{"client policies", "SELECT client_ip, COALESCE(policy_id, 0) FROM client_policies WHERE client_ip <> ''", func(r *sql.Rows) error {
			var ip string
			var pid int
			err := r.Scan(&ip, &pid)
			entry(cfg.Clients, ip).PolicyID = pid
			return err
		}},
	}
	for _, s := range steps {
		if err := scanRows(q, s.query, s.scan); err != nil {
			return policycore.Config{}, fmt.Errorf("read %s: %w", s.what, err)
		}
	}
	validRanges := cfg.Ranges[:0]
	for _, r := range cfg.Ranges {
		if _, err := policycore.ParseRangeCIDR(r.CIDR); err != nil {
			logDB.Warn("skipping invalid CIDR", "cidr", r.CIDR, "range_id", r.ID, "error", err)
			continue
		}
		validRanges = append(validRanges, r)
	}
	cfg.Ranges = validRanges

	return cfg, nil
}

// listRuleFeed streams the stored rules of every list in lists from the
// database straight into the index builder: no per-list copy is ever held.
// Lists are read one at a time, so rows of disabled lists are never read.
// Rows are normalized here because rows written before normalization existed
// (D18) must match the way queries arrive.
func listRuleFeed(q queryer, lists []policycore.List) func(add func(slot int, rule string)) error {
	return func(add func(slot int, rule string)) error {
		var raw sql.RawBytes
		for slot, l := range lists {
			table, column := "blocked_domains", "blocklist_id"
			if l.Allow {
				table, column = "allowed_domains", "allowlist_id"
			}
			err := scanRows(q, "SELECT domain FROM "+table+" WHERE "+column+" = ?", func(r *sql.Rows) error {
				if err := r.Scan(&raw); err != nil {
					return err
				}
				if l.Manual && reservedStoredRule(string(raw)) {
					return fmt.Errorf("manual list %d contains a reserved encoded rule", l.ID)
				}
				rule, err := listparse.NormalizeStored(string(raw))
				if err != nil {
					return fmt.Errorf("decode list %d rule: %w", l.ID, err)
				}
				if rule != "" {
					add(slot, rule)
				}
				return nil
			}, l.ID)
			if err != nil {
				return fmt.Errorf("read %s of list %d: %w", table, l.ID, err)
			}
		}
		return nil
	}
}

// reloadScope says what a mutation may have changed.
type reloadScope int

const (
	// reloadEntities: ranges, groups, clients, policies or list assignments.
	// The previous index is reused unless the set of enabled lists changed.
	reloadEntities reloadScope = iota
	// reloadListContent: the rules stored for some list changed (refresh,
	// custom rule, import, sync); the index is rebuilt from the database.
	reloadListContent
)

var (
	policyState    atomic.Pointer[policycore.Snapshot]
	policyReloadMu sync.Mutex
	// Guarded by policyReloadMu: a failed publication leaves saved changes
	// outside the live snapshot, beyond any later caller's changed-list hint.
	policyNeedsFullReload bool
)

func init() {
	policyState.Store(emptyPolicySnapshot())
}

func emptyPolicySnapshot() *policycore.Snapshot {
	ix, err := policycore.BuildIndex(nil, func(func(int, string)) error { return nil })
	if err != nil {
		panic(err) // an empty index cannot fail to build
	}
	return policycore.BuildSnapshot(policycore.Config{}, ix)
}

// reloadPolicyState is the one way in-memory policy changes: it reads the
// configuration from the database, builds a complete new snapshot and
// publishes it atomically. On any error the previous snapshot stays active
// and the error is returned and logged; a half-built state is never served.
func reloadPolicyState(reason string, scope reloadScope, changed ...policycore.ListKey) error {
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	start := time.Now()
	if policyNeedsFullReload {
		scope, changed = reloadListContent, nil
	}
	snap, rebuilt, err := readPolicySnapshot(policyState.Load(), scope, changed...)
	if err != nil {
		policyNeedsFullReload = true
		policyReloadErrors.Inc()
		err = fmt.Errorf("policy reload (%s) failed, previous policy stays active: %w", reason, err)
		logPolicy.Error("policy reload failed", "reason", reason, "error", err)
		return err
	}
	policyState.Store(snap)
	policyNeedsFullReload = false
	// Bump the cache generation only after publishing: an evaluation that
	// read the old generation cannot store a result any more (policycache.go).
	policyCache.Clear()
	updateListMetrics(snap.Index)
	logPolicy.Info("policy reloaded", "reason", reason, "index_rebuilt", rebuilt,
		"lists", len(snap.Index.Lists), "ranges", len(snap.Ranges), "clients", len(snap.Clients),
		"index_bytes", snap.Index.ApproxBytes(), "duration_ms", time.Since(start).Milliseconds())
	return nil
}

func readPolicySnapshot(prev *policycore.Snapshot, scope reloadScope, changed ...policycore.ListKey) (*policycore.Snapshot, bool, error) {
	if readDB == nil {
		return nil, false, errors.New("database not open")
	}
	tx, err := readDB.Begin()
	if err != nil {
		return nil, false, err
	}
	defer rollbackTransaction(tx)
	return buildPolicySnapshot(tx, prev, scope, changed...)
}

func buildPolicySnapshot(q queryer, prev *policycore.Snapshot, scope reloadScope, changed ...policycore.ListKey) (*policycore.Snapshot, bool, error) {
	cfg, err := readPolicyConfig(q)
	if err != nil {
		return nil, false, err
	}
	ix := prev.Index.WithLists(cfg.Lists)
	if ix == nil || scope == reloadListContent {
		if scope == reloadEntities || len(changed) > 0 {
			if updated, eligible, updateErr := readIncrementalIndex(q, prev.Index, cfg.Lists, changed); eligible {
				if updateErr != nil {
					return nil, false, updateErr
				}
				return policycore.BuildSnapshot(cfg, updated), true, nil
			}
		}
		if ix, err = policycore.BuildIndex(cfg.Lists, listRuleFeed(q, cfg.Lists)); err != nil {
			return nil, false, err
		}
		return policycore.BuildSnapshot(cfg, ix), true, nil
	}
	return policycore.BuildSnapshot(cfg, ix), false, nil
}
