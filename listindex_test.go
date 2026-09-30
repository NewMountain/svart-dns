package main

import (
	"bufio"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"

	"github.com/yeti/svart-dns/internal/policycore"
)

// fixtureDomains is generated independently of third-party ranking datasets.
// Vary suffixes and label depth so the corpus exercises parent-name matches.
func fixtureDomains() []string {
	domains := []string{"google.com", "doubleclick.net", "facebook.com", "tiktok.com"}
	suffixes := []string{"com", "net", "org", "co.uk", "com.au", "test"}
	for i := len(domains); i < 10000; i++ {
		name := fmt.Sprintf("site-%d.%s", i, suffixes[i%len(suffixes)])
		if i%3 == 0 {
			name = "cdn." + name
		}
		domains = append(domains, name)
	}
	return domains
}

type fixtureList struct {
	id    int
	rules []string
}

// fixtureLists builds overlapping lists the way real ones overlap: a shared
// core plus list-specific tails, with wildcard, complex and IP rules mixed in.
func fixtureLists(_ testing.TB, rng *rand.Rand) []fixtureList {
	domains := fixtureDomains()
	lists := []fixtureList{{id: 3}, {id: 7}, {id: 11}, {id: 12}, {id: 20}, {id: 21}}
	for i, d := range domains {
		for li := range lists {
			// List li includes a domain with probability decreasing in li, so
			// sizes differ and attribution order is exercised.
			if rng.IntN(100) < 60-li*8 || (i%97 == 0 && li == 4) {
				lists[li].rules = append(lists[li].rules, d)
			}
		}
	}
	lists[1].rules = append(lists[1].rules, "*.doubleclick.net", "*.tracking.example.org", "ads*.example.com", "*track*.example.net")
	lists[2].rules = append(lists[2].rules, "*.doubleclick.net", "93.184.216.34", "10.0.0.53")
	lists[3].rules = append(lists[3].rules, "*metrics*", "telemetry.*.example.com")
	lists[5].rules = append(lists[5].rules, "*.google.com") // wildcard over names that are also exact elsewhere
	// Same size as list 4 with a lower ID tail: exercises the ID tie-break.
	lists[4].rules = append([]string(nil), lists[0].rules[:len(lists[4].rules)]...)
	return lists
}

func buildFixtureIndex(t testing.TB, lists []fixtureList, allow bool) *policycore.Index {
	t.Helper()
	meta := make([]policycore.List, len(lists))
	for i, l := range lists {
		meta[i] = policycore.List{ID: l.id, Name: fmt.Sprintf("list-%d", l.id), Allow: allow}
	}
	b, err := policycore.NewIndexBuilder(meta)
	if err != nil {
		t.Fatal(err)
	}
	for i, l := range lists {
		for _, r := range l.rules {
			b.Add(i, r)
		}
	}
	ix, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func indexMatchFor(ix *policycore.Index, lists []fixtureList, allow bool, domain string) (bool, string, int) {
	ids := make([]int, len(lists))
	for i, l := range lists {
		ids[i] = l.id
	}
	arpa := ""
	if !allow && strings.HasSuffix(domain, ".arpa") {
		arpa = policycore.ReverseARPAToIP(domain)
	}
	var p policycore.Probe
	ix.ProbeDomain(&p, domain, arpa, 1)
	m, ok := ix.FirstMatch(&p, ix.SetFor(ids, allow), !allow)
	if !ok {
		return false, "", 0
	}
	return true, m.Rule(), ix.Lists[m.Slot].ID
}

func queryCorpus(rng *rand.Rand, lists []fixtureList, n int) []string {
	domains := []string{
		"www.google.com", "google.com", "maps.google.com", "a.b.c.doubleclick.net", "doubleclick.net",
		"ads1.example.com", "ads.example.com", "x.track-me.example.net", "cdn.metrics.io",
		"telemetry.eu.example.com", "telemetry.example.com", "34.216.184.93.in-addr.arpa",
		"53.0.0.10.in-addr.arpa", "1.2.3.4.in-addr.arpa", "never-listed.invalid", "localhost",
		"deep.a.b.c.d.e.f.g.h.i.j.k.l.m.n.o.p.tracking.example.org",
	}
	for len(domains) < n {
		l := lists[rng.IntN(len(lists))]
		d := l.rules[rng.IntN(len(l.rules))]
		switch rng.IntN(4) {
		case 0:
			domains = append(domains, d)
		case 1:
			domains = append(domains, "sub"+fmt.Sprint(rng.IntN(9))+"."+strings.TrimPrefix(d, "*."))
		case 2:
			domains = append(domains, "x."+strings.TrimPrefix(d, "*.")+".unlisted.test")
		default:
			domains = append(domains, fmt.Sprintf("r%d.miss.example", rng.IntN(1e6)))
		}
	}
	return domains
}

func TestDomainIndexConcreteMatches(t *testing.T) {
	lists := []fixtureList{
		{id: 1, rules: []string{"doubleclick.net", "ads.example.com", "*.tracker.io", "ad*.cdn.example", "203.0.113.7"}},
		{id: 2, rules: []string{"doubleclick.net"}},
	}
	ix := buildFixtureIndex(t, lists, false)
	cases := []struct {
		domain  string
		matched bool
		rule    string
		listID  int
	}{
		{"doubleclick.net", true, "doubleclick.net", 2},         // smallest list wins attribution
		{"stats.g.doubleclick.net", true, "doubleclick.net", 2}, // parent match
		{"ads.example.com", true, "ads.example.com", 1},
		{"x.ads.example.com", true, "ads.example.com", 1},
		{"example.com", false, "", 0},
		{"tracker.io", false, "", 0}, // *.x matches subdomains only
		{"a.tracker.io", true, "*.tracker.io", 1},
		{"ad7.cdn.example", true, "ad*.cdn.example", 1},
		{"7.113.0.203.in-addr.arpa", true, "203.0.113.7", 1},
		{"8.113.0.203.in-addr.arpa", false, "", 0},
	}
	for _, c := range cases {
		m, rule, id := indexMatchFor(ix, lists, false, c.domain)
		if m != c.matched || rule != c.rule || id != c.listID {
			t.Errorf("%s: got (%v %q %d), want (%v %q %d)", c.domain, m, rule, id, c.matched, c.rule, c.listID)
		}
	}
	if got := ix.Lists[0].ID; got != 2 {
		t.Fatalf("slot 0 should hold the smallest list (id 2), got id %d", got)
	}
	if ix.Lists[1].Count != 5 || ix.Lists[0].Count != 1 {
		t.Fatalf("counts: got %d and %d, want 1 and 5", ix.Lists[0].Count, ix.Lists[1].Count)
	}
}

func TestDomainIndexContainsAndRules(t *testing.T) {
	lists := []fixtureList{{id: 9, rules: []string{"a.example", "*.b.example", "c*.example", "a.example"}}}
	ix := buildFixtureIndex(t, lists, true)
	for _, r := range []string{"a.example", "*.b.example", "c*.example"} {
		if !ix.Contains(0, r) {
			t.Errorf("Contains(%q) = false", r)
		}
	}
	for _, r := range []string{"b.example", "*.a.example", "x.example"} {
		if ix.Contains(0, r) {
			t.Errorf("Contains(%q) = true", r)
		}
	}
	var got []string
	ix.Rules(0, func(r string) { got = append(got, r) })
	want := map[string]bool{"a.example": true, "*.b.example": true, "c*.example": true}
	if len(got) != len(want) {
		t.Fatalf("Rules returned %v, want %v (duplicates must collapse)", got, want)
	}
	for _, r := range got {
		if !want[r] {
			t.Errorf("unexpected rule %q", r)
		}
	}
}

func TestDomainIndexRejectsTooManyLists(t *testing.T) {
	if _, err := policycore.NewIndexBuilder(make([]policycore.List, policycore.MaxIndexLists)); err != nil {
		t.Fatalf("%d lists: %v", policycore.MaxIndexLists, err)
	}
	_, err := policycore.NewIndexBuilder(make([]policycore.List, policycore.MaxIndexLists+1))
	want := "too many enabled lists: 2049 enabled blocklists and allowlists, the list index holds at most 2048; disable or delete lists (each client, group or policy with custom rules has its own manual list)"
	if !errors.Is(err, policycore.ErrTooManyLists) || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestDomainIndexDeepNameIsBounded(t *testing.T) {
	lists := []fixtureList{{id: 1, rules: []string{"evil.example"}}}
	ix := buildFixtureIndex(t, lists, false)
	labels := make([]string, 120)
	for i := range labels {
		labels[i] = "a"
	}
	deep := strings.Join(labels, ".") + ".evil.example" // 122 labels, 252 bytes
	m, rule, id := indexMatchFor(ix, lists, false, deep)
	if !m || rule != "evil.example" || id != 1 {
		t.Fatalf("deep name: got (%v %q %d)", m, rule, id)
	}
	allocs := testing.AllocsPerRun(100, func() {
		var p policycore.Probe
		ix.ProbeDomain(&p, deep, "", 1)
		_, _ = ix.FirstMatch(&p, policycore.ListSet{1}, true)
	})
	if allocs > 1 {
		t.Fatalf("deep-name probe allocated %.0f times per run, want ≤1 (hit overflow only)", allocs)
	}
}

// BenchmarkPolicyMatch measures one probe plus one six-list match on
// realistic overlapping lists. (The per-list matcher it replaced ran 3-5 us.)
func BenchmarkPolicyMatch(b *testing.B) {
	// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
	rng := rand.New(rand.NewPCG(42, 7))
	lists := fixtureLists(b, rng)
	queries := queryCorpus(rng, lists, 4096)
	ids := make([]int, len(lists))
	for i, l := range lists {
		ids[i] = l.id
	}
	ix := buildFixtureIndex(b, lists, false)
	set := ix.SetFor(ids, false)

	b.Run("index", func(b *testing.B) {
		b.ReportAllocs()
		var p policycore.Probe
		for i := 0; b.Loop(); i++ {
			ix.ProbeDomain(&p, queries[i&4095], "", 1)
			ix.FirstMatch(&p, set, true)
		}
	})
}

// TestDomainIndexRealListFootprint reports bytes per rule on real published
// lists. Set SVART_LISTS_DIR to a directory of downloaded lists to run it.
func TestDomainIndexRealListFootprint(t *testing.T) {
	dir := os.Getenv("SVART_LISTS_DIR")
	if dir == "" {
		t.Skip("SVART_LISTS_DIR not set")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var meta []policycore.List
	var paths []string
	for i, e := range entries {
		meta = append(meta, policycore.List{ID: i + 1, Name: e.Name()})
		paths = append(paths, dir+"/"+e.Name())
	}
	b, err := policycore.NewIndexBuilder(meta)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for slot, p := range paths {
		// #nosec G304 G703 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if d := listparse.ParseDomain(sc.Text()); d != "" {
				b.Add(slot, d)
				total++
			}
		}
		checkTestClose(t, f)
	}
	ix, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	unique := ix.Exact.Count + ix.Wild.Count + len(ix.Complex)
	t.Logf("rules added %d, unique %d, index %.1f MiB, %.1f B/unique rule, %.1f B/added rule",
		total, unique, float64(ix.ApproxBytes())/(1<<20), float64(ix.ApproxBytes())/float64(unique),
		float64(ix.ApproxBytes())/float64(total))
	for _, l := range ix.Lists {
		t.Logf("  slot list %-22s %8d rules", l.Name, l.Count)
	}
}
