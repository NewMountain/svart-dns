package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

// policyWorld is one randomized but realistic policy configuration written
// to a test database, plus what the query generator needs to aim at it.
type policyWorld struct {
	rules    []string // every rule stored in any list, as stored
	complex  []string
	custom   []string // rules of manual (custom-rule) lists
	ips      []string // IP-literal rules, canonical
	clients  []string // client addresses that appear in the configuration
	groupIDs []int
	rangeIDs []int
	listIDs  map[bool][]int // published lists by kind (allow=true)
	manual   map[bool][]int
}

var worldListNames = map[bool][]string{
	false: {"Hagezi Pro", "OISD Big", "StevenBlack Unified", "1Hosts Lite", "AdGuard DNS", "Hagezi TIF", "EasyPrivacy", "URLhaus"},
	true:  {"Hagezi Allow", "Anudeep Whitelist", "Streaming Fixes", "Banking Sites"},
}

var worldCIDRs = []string{
	"0.0.0.0/0", "10.0.0.0/8", "10.20.0.0/16", "10.20.30.0/24", "10.20.30.40/32", "192.168.0.0/16",
	"192.168.1.0/24", "10.20.99.1/16", "::/0", "2001:db8::/32", "2001:db8:1::/48", "fd00::/8",
}

var worldClients = []string{
	"10.20.30.40", "10.20.30.41", "10.20.31.7", "10.9.8.7", "192.168.1.10", "192.168.1.23", "192.168.7.9",
	"172.16.5.4", "2001:db8:1::42", "2001:db8:2::9", "fd00::1:2", "2a00:1450::200e", "::ffff:10.20.30.77",
	"100.64.3.3", "10.20.30.200",
}

var worldGroups = []string{"Family", "Kids", "IoT jail", "Servers", "Guests", "Work laptops"}

// seedPolicyWorld writes a random configuration drawn from the generated
// domain corpus into the current test database.
func seedPolicyWorld(t testing.TB, rng *rand.Rand, corpus []string) *policyWorld {
	t.Helper()
	w := &policyWorld{listIDs: map[bool][]int{}, manual: map[bool][]int{}}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) int {
		t.Helper()
		res, err := tx.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, fixtureErr2102 := res.LastInsertId()
		if fixtureErr2102 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr2102)
		}
		return int(id)
	}
	insertList := func(allow bool, url, alias string, enabled bool, rules []string) int {
		table, rows, col := "blocklists", "blocked_domains", "blocklist_id"
		if allow {
			table, rows, col = "allowlists", "allowed_domains", "allowlist_id"
		}
		id := exec("INSERT INTO "+table+" (url, alias, enabled, domain_count) VALUES (?, ?, ?, ?)", url, alias, enabled, len(rules))
		stmt, err := tx.Prepare("INSERT INTO " + rows + " (" + col + ", domain) VALUES (?, ?)")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { checkTestClose(t, stmt) }()
		for _, r := range rules {
			if _, err := stmt.Exec(id, r); err != nil {
				t.Fatal(err)
			}
			w.rules = append(w.rules, r)
		}
		return id
	}

	// A window of the corpus so lists overlap the way real lists do.
	base := rng.IntN(len(corpus) - 3000)
	pool := corpus[base : base+3000]
	usedComplexBases := []string{}
	complexFor := func(d string) string {
		for _, b := range usedComplexBases {
			if strings.HasSuffix(d, b) || strings.HasSuffix(b, d) {
				return ""
			}
		}
		usedComplexBases = append(usedComplexBases, d)
		return []string{"ads*.", "track*.", "cdn-*.", "*-telemetry."}[rng.IntN(4)] + d
	}
	publishedRules := func(allow bool) []string {
		var rules []string
		pct := 5 + rng.IntN(40)
		if allow {
			pct = 1 + rng.IntN(6)
		}
		for _, d := range pool {
			if rng.IntN(100) < pct {
				rules = append(rules, d)
			}
			if rng.IntN(400) == 0 {
				rules = append(rules, "*."+d)
			}
		}
		for i := rng.IntN(3); i > 0; i-- {
			if c := complexFor(pool[rng.IntN(len(pool))]); c != "" {
				rules = append(rules, c)
				w.complex = append(w.complex, c)
			}
		}
		if allow {
			// Allowlisted addresses carve exceptions out of IP-literal blocks.
			for i := rng.IntN(3); i > 0 && len(w.ips) > 0; i-- {
				rules = append(rules, w.ips[rng.IntN(len(w.ips))])
			}
		} else {
			for i := rng.IntN(4); i > 0; i-- {
				v4 := fmt.Sprintf("203.0.113.%d", rng.IntN(250)+1)
				v6 := fmt.Sprintf("2001:db8:bad::%x", rng.IntN(4000)+1)
				rules = append(rules, v4, strings.ToUpper(v6))
				w.ips = append(w.ips, v4, v6)
			}
			// Rows written before normalization existed (D18).
			d := pool[rng.IntN(len(pool))]
			rules = append(rules, strings.ToUpper(d)+".", " "+d)
		}
		return rules
	}
	for _, allow := range []bool{false, true} {
		names := worldListNames[allow]
		n := 2 + rng.IntN(len(names)-2)
		for i := 0; i < n; i++ {
			id := insertList(allow, fmt.Sprintf("https://lists.example.net/%d.txt", i), names[i], true, publishedRules(allow))
			w.listIDs[allow] = append(w.listIDs[allow], id)
		}
		// A disabled list that entities still reference.
		w.listIDs[allow] = append(w.listIDs[allow], insertList(allow, "https://lists.example.net/off.txt", names[n]+" (off)", false, publishedRules(allow)))
	}
	// An enabled list that has not downloaded anything yet.
	w.listIDs[false] = append(w.listIDs[false], insertList(false, "https://lists.example.net/new.txt", "Freshly Added", true, nil))

	customRules := func() []string {
		var rules []string
		for i := 1 + rng.IntN(4); i > 0; i-- {
			d := pool[rng.IntN(len(pool))]
			switch rng.IntN(5) {
			case 0:
				rules = append(rules, "*."+d)
			case 1:
				rules = append(rules, "api."+d)
			default:
				rules = append(rules, d)
			}
		}
		return rules
	}
	pick := func(ids []int, maximum int) []int {
		var out []int
		for _, i := range rng.Perm(len(ids))[:rng.IntN(min(maximum, len(ids))+1)] {
			out = append(out, ids[i])
		}
		return out
	}
	manualList := func(allow bool, owner string) int {
		prefix := "Manual Block"
		if allow {
			prefix = "Manual"
		}
		rules := customRules()
		w.custom = append(w.custom, rules...)
		id := insertList(allow, "", fmt.Sprintf("%s (%s)", prefix, owner), true, rules)
		w.manual[allow] = append(w.manual[allow], id)
		return id
	}

	var policyIDs []int
	for i, name := range []string{"Strict", "Family", "Streaming"}[:1+rng.IntN(3)] {
		pid := exec("INSERT INTO policies (name, description) VALUES (?, ?)", name, "policy "+fmt.Sprint(i))
		policyIDs = append(policyIDs, pid)
		for _, id := range pick(w.listIDs[false], 3) {
			exec("INSERT INTO policy_blocklists (policy_id, blocklist_id) VALUES (?, ?)", pid, id)
		}
		for _, id := range pick(w.listIDs[true], 2) {
			exec("INSERT INTO policy_allowlists (policy_id, allowlist_id) VALUES (?, ?)", pid, id)
		}
		if rng.IntN(2) == 0 {
			exec("INSERT INTO policy_blocklists (policy_id, blocklist_id) VALUES (?, ?)", pid, manualList(false, "Policy: "+name))
		}
		if rng.IntN(2) == 0 {
			exec("INSERT INTO policy_allowlists (policy_id, allowlist_id) VALUES (?, ?)", pid, manualList(true, "Policy: "+name))
		}
	}
	policyOrNull := func() any {
		if rng.IntN(3) == 0 {
			return policyIDs[rng.IntN(len(policyIDs))]
		}
		return nil
	}

	for _, i := range rng.Perm(len(worldCIDRs))[:4+rng.IntN(len(worldCIDRs)-3)] {
		cidr := worldCIDRs[i]
		rid := exec("INSERT INTO ip_ranges (name, cidr, policy_id) VALUES (?, ?, ?)", "VLAN "+cidr, cidr, policyOrNull())
		w.rangeIDs = append(w.rangeIDs, rid)
		for _, id := range pick(w.listIDs[false], 3) {
			exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", rid, id)
		}
		for _, id := range pick(w.listIDs[true], 2) {
			exec("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (?, ?)", rid, id)
		}
	}
	// A stored range that does not parse is skipped, not fatal.
	exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Broken import", "10.300.0.0/16")

	for _, name := range worldGroups[:2+rng.IntN(len(worldGroups)-1)] {
		gid := exec("INSERT INTO client_groups (name, policy_id) VALUES (?, ?)", name, policyOrNull())
		w.groupIDs = append(w.groupIDs, gid)
		for _, id := range pick(w.listIDs[false], 3) {
			exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", gid, id)
		}
		for _, id := range pick(w.listIDs[true], 2) {
			exec("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (?, ?)", gid, id)
		}
		if rng.IntN(2) == 0 {
			exec("INSERT INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", gid, manualList(false, name))
		}
		if rng.IntN(3) == 0 {
			exec("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (?, ?)", gid, manualList(true, name))
		}
	}

	for _, ip := range worldClients {
		if rng.IntN(5) == 0 {
			continue
		}
		w.clients = append(w.clients, ip)
		for _, gid := range pick(w.groupIDs, 3) {
			exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", ip, gid)
		}
		for _, id := range pick(w.listIDs[false], 3) {
			exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", ip, id)
		}
		for _, id := range pick(w.listIDs[true], 2) {
			exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", ip, id)
		}
		if rng.IntN(3) == 0 {
			exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", ip, manualList(false, ip))
		}
		if rng.IntN(3) == 0 {
			exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", ip, manualList(true, ip))
		}
		if p := policyOrNull(); p != nil {
			exec("INSERT INTO client_policies (client_ip, policy_id) VALUES (?, ?)", ip, p)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return w
}

type policyQuery struct{ client, domain string }

// queries aims at every matching path: exact, parent, wildcard, complex,
// reverse ARPA, normalization, misses, and clients inside and outside ranges.
func (w *policyWorld) queries(rng *rand.Rand, n int) []policyQuery {
	clients := append(slices.Clone(w.clients), "10.20.30.99", "192.168.200.1", "2001:db8:1::ffff", "8.8.8.8", "not-an-ip", "")
	domain := func() string {
		source := w.rules
		if len(w.custom) > 0 && rng.IntN(4) == 0 {
			source = w.custom
		}
		r := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(source[rng.IntN(len(source))]), "."))
		switch rng.IntN(11) {
		case 0:
			return r
		case 1, 2:
			return "cdn" + fmt.Sprint(rng.IntN(5)) + "." + strings.TrimPrefix(r, "*.")
		case 3:
			return strings.TrimPrefix(r, "*.")
		case 4:
			if len(w.complex) > 0 {
				return strings.ReplaceAll(w.complex[rng.IntN(len(w.complex))], "*", "x"+fmt.Sprint(rng.IntN(3)))
			}
			return r
		case 5:
			if len(w.ips) > 0 {
				return arpaName(w.ips[rng.IntN(len(w.ips))])
			}
			return "1.2.3.4.in-addr.arpa"
		case 6:
			return strings.ToUpper(r) + "."
		case 7:
			return "a.b.c.d.e.f.g.h." + strings.TrimPrefix(r, "*.")
		case 8:
			return fmt.Sprintf("r%d.never-listed.example", rng.IntN(1e6))
		case 9:
			if len(w.ips) > 0 {
				return w.ips[rng.IntN(len(w.ips))]
			}
			return "localhost"
		default:
			return "x.unlisted." + strings.TrimPrefix(r, "*.")
		}
	}
	out := make([]policyQuery, n)
	for i := range out {
		out[i] = policyQuery{client: clients[rng.IntN(len(clients))], domain: domain()}
	}
	return out
}

// arpaName is the reverse-lookup name for an address.
func arpaName(ip string) string {
	a := netip.MustParseAddr(ip)
	if a.Is4() {
		b := a.As4()
		return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", b[3], b[2], b[1], b[0])
	}
	b := a.As16()
	var sb strings.Builder
	for i := 15; i >= 0; i-- {
		fmt.Fprintf(&sb, "%x.%x.", b[i]&0xf, b[i]>>4)
	}
	return sb.String() + "ip6.arpa"
}

func newEngineEvaluate(clientIP, domain string) *policycore.PolicyResult {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		addr = netip.Addr{}
	}
	return policyState.Load().Evaluate(clientIP, addr, domain, 1)
}

// policyCoverage counts which decision paths the compared results took, so
// the equivalence cannot pass vacuously.
type policyCoverage map[string]int

func (c policyCoverage) add(r *policycore.PolicyResult, responseBlocked bool) {
	if responseBlocked {
		c["response IP blocked"]++
	}
	if r == nil {
		return
	}
	c["decided by "+r.ResultSource.Tier+" tier"]++
	for _, te := range []*policycore.TierEvaluation{r.RangeEvaluation, r.GroupEvaluation, r.IPEvaluation} {
		if len(te.Entities) > 1 {
			c["several entities in a tier"]++
		}
		for _, e := range te.Entities {
			if e.PublishedList != nil {
				c[e.Tier+" published "+e.PublishedList.Action]++
				switch rule := e.PublishedList.Rule; {
				case strings.HasPrefix(rule, "*."):
					c["wildcard rule"]++
				case strings.Contains(rule, "*"):
					c["complex rule"]++
				case strings.HasSuffix(r.Domain, ".arpa"):
					c["reverse ARPA rule"]++
				case rule != r.Domain:
					c["parent rule"]++
				}
			}
			if e.CustomRule != nil {
				c[e.Tier+" custom "+e.CustomRule.Action]++
				if e.PublishedList != nil {
					c["allow overriding a block in one entity"]++
				}
			}
		}
	}
}

// digestPhase evaluates every query and response-IP set and returns a digest
// of the results, which the golden file pins.
func digestPhase(t *testing.T, qs []policyQuery, ipSets [][]string, cov policyCoverage) string {
	t.Helper()
	digest := sha256.New()
	for _, q := range qs {
		result := newEngineEvaluate(q.client, q.domain)
		if result.RecordType != 1 {
			t.Fatalf("A-query attribution has record type %d", result.RecordType)
		}
		out := mustFixture(json.Marshal(result))
		// Preserve the original decision oracle byte-for-byte. Record type is
		// new attribution metadata, asserted above, absent from that oracle.
		out = bytes.Replace(out, []byte(`"record_type":1,`), nil, 1)
		mustFixture(fmt.Fprintf(digest, "%s %s %s\n", q.client, q.domain, out))
		cov.add(result, false)
	}
	for i, ips := range ipSets {
		client := qs[i%len(qs)].client
		blocked := responseIPsBlocked(client, ips)
		mustFixture(fmt.Fprintf(digest, "%s %v %v\n", client, ips, blocked))
		cov.add(nil, blocked)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

var updatePolicyGolden = flag.Bool("update-policy-golden", false, "rewrite testdata/policy_engine.golden from the current engine")

const policyGoldenPath = "testdata/policy_engine.golden"

// checkPolicyGolden compares the per-phase output digests with the golden
// file. It was recorded from the per-tier engine the snapshot replaced, while
// both engines ran side by side and agreed on every result (commit history:
// "refactor(policy): build one immutable policy snapshot beside the per-tier
// stores"). A difference means decisions changed: find the query by
// re-running that commit's comparison, and only rewrite the file with
// -update-policy-golden when the change is intended.
func checkPolicyGolden(t *testing.T, lines []string) {
	t.Helper()
	got := strings.Join(lines, "\n") + "\n"
	if *updatePolicyGolden {
		if err := os.WriteFile(policyGoldenPath, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(policyGoldenPath)
	if err != nil {
		t.Fatalf("read %s: %v (run with -update-policy-golden to record it)", policyGoldenPath, err)
	}
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	if len(wantLines) != len(lines) {
		t.Fatalf("%s has %d phases, the test ran %d", policyGoldenPath, len(wantLines), len(lines))
	}
	for i := range lines {
		if lines[i] != wantLines[i] {
			t.Errorf("policy output changed: got %q, golden %q", lines[i], wantLines[i])
		}
	}
}

// TestPolicyEngineMatchesGolden pins every PolicyResult and response-IP
// verdict, on many random configurations, before and after entity-only,
// rename, enable/disable and list-content reloads, to the output of the
// per-tier engine the snapshot replaced. The independent generated corpus
// was re-recorded against the unchanged engine at 4f20de8 when bundled
// third-party ranking data was removed.
func TestPolicyEngineMatchesGolden(t *testing.T) {
	corpus := fixtureDomains()
	const worlds, perPhase = 24, 700
	total, cov := 0, policyCoverage{}
	var golden []string
	for seed := uint64(1); seed <= worlds; seed++ {
		t.Run(fmt.Sprintf("world%02d", seed), func(t *testing.T) {
			cleanup := setupTestDB(t)
			defer cleanup()
			// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
			rng := rand.New(rand.NewPCG(seed, 2026))
			w := seedPolicyWorld(t, rng, corpus)
			ipSets := make([][]string, 150)
			for i := range ipSets {
				ipSets[i] = []string{fmt.Sprintf("198.51.100.%d", rng.IntN(250)+1)}
				for j := rng.IntN(3); j > 0 && len(w.ips) > 0; j-- {
					ipSets[i] = append(ipSets[i], w.ips[rng.IntN(len(w.ips))])
				}
			}

			phase := func(label string, scope reloadScope, mutate func(exec func(string, ...any))) {
				if mutate != nil {
					mutate(func(q string, args ...any) {
						if _, err := db.Exec(q, args...); err != nil {
							t.Fatalf("%s: %s: %v", label, q, err)
						}
					})
				}
				if err := reloadPolicyState("equivalence "+label, scope); err != nil {
					t.Fatal(err)
				}
				qs := w.queries(rng, perPhase)
				golden = append(golden, fmt.Sprintf("world%02d %-8s %s", seed, label, digestPhase(t, qs, ipSets, cov)))
				total += len(qs)
			}
			phase("initial", reloadListContent, nil)
			phase("entities", reloadEntities, func(exec func(string, ...any)) {
				exec("UPDATE client_groups SET name = name || ' (renamed)' WHERE id = ?", w.groupIDs[0])
				exec("DELETE FROM client_group_members WHERE rowid IN (SELECT rowid FROM client_group_members LIMIT 2)")
				exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id) VALUES (?, ?)", worldClients[0], w.groupIDs[len(w.groupIDs)-1])
				exec("INSERT OR IGNORE INTO range_blocklists (range_id, blocklist_id) VALUES (?, ?)", w.rangeIDs[0], w.listIDs[false][0])
				exec("UPDATE blocklists SET alias = alias || ' v2' WHERE id = ?", w.listIDs[false][1])
				exec("DELETE FROM client_policies WHERE rowid IN (SELECT rowid FROM client_policies LIMIT 1)")
			})
			phase("toggle", reloadEntities, func(exec func(string, ...any)) {
				exec("UPDATE blocklists SET enabled = NOT enabled WHERE id IN (?, ?)", w.listIDs[false][0], w.listIDs[false][len(w.listIDs[false])-2])
				exec("UPDATE allowlists SET enabled = 0 WHERE id = ?", w.listIDs[true][0])
			})
			phase("content", reloadListContent, func(exec func(string, ...any)) {
				if ids := w.manual[false]; len(ids) > 0 {
					exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, 'tiktok.com'), (?, '*.doubleclick.net')", ids[0], ids[0])
				}
				if ids := w.manual[true]; len(ids) > 0 {
					exec("DELETE FROM allowed_domains WHERE allowlist_id = ?", ids[0])
				}
				exec("DELETE FROM blocked_domains WHERE rowid IN (SELECT rowid FROM blocked_domains WHERE blocklist_id = ? LIMIT 50)", w.listIDs[false][1])
				exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, 'ads.google.com'), (?, 'Pixel.Facebook.com.')", w.listIDs[true][1], w.listIDs[true][1])
			})
		})
	}
	// Guard against a vacuous pass: every decision path must have been hit
	// many times.
	for _, path := range []string{
		"decided by range tier", "decided by group tier", "decided by ip tier", "decided by default tier",
		"range published block", "range published allow", "group published block", "group published allow",
		"ip published block", "ip published allow", "ip custom block", "ip custom allow",
		"allow overriding a block in one entity", "several entities in a tier",
		"wildcard rule", "complex rule", "reverse ARPA rule", "parent rule", "response IP blocked",
	} {
		if cov[path] < 20 {
			t.Errorf("coverage: %q seen %d times, want at least 20", path, cov[path])
		}
	}
	checkPolicyGolden(t, golden)
	t.Logf("%d configurations, %d policy queries, %d response-IP checks; coverage %v", worlds, total, worlds*4*150, cov)
}
