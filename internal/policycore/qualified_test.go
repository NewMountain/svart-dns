package policycore

import (
	"fmt"
	"github.com/yeti/svart-dns/internal/listparse"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestMalformedStoredRuleRejectsBuildAndPreservesPrevious(t *testing.T) {
	lists := []List{{ID: 1, Name: "Ads"}}
	previous, err := BuildIndex(lists, func(add func(int, string)) error { add(0, "ads.example"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	next, err := previous.UpdateIndex(lists, []ListKey{{ID: 1}}, func(add func(int, string)) error { add(0, "!svart-rule-v1:not-valid-json"); return nil })
	if err == nil || next != nil {
		t.Fatalf("malformed stored rule published: index_published=%t error=%v", next != nil, err)
	}
	if !previous.Contains(previous.SlotOf(1, false), "ads.example") {
		t.Fatal("failed update lost previous rule")
	}
}

func storedRule(t *testing.T, r listparse.Rule) string {
	t.Helper()
	r.Version = 1
	row, err := listparse.EncodeStored(r)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func qualifiedIndex(t *testing.T, lists []List, rows [][]string) *Index {
	t.Helper()
	ix, err := BuildIndex(lists, func(add func(int, string)) error {
		for slot, rules := range rows {
			for _, row := range rules {
				add(slot, row)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestQualifiedRecordTypesAndFullAttribution(t *testing.T) {
	rule := storedRule(t, listparse.Rule{Text: "||ads.example^$dnstype=TXT", Pattern: "ads.example", Kind: listparse.KindSubtree, DNSTypes: []uint16{16}})
	ix := qualifiedIndex(t, []List{{ID: 1, Name: "Typed"}}, [][]string{{rule}})
	for _, tc := range []struct {
		domain string
		typ    uint16
		want   bool
	}{{"ads.example", 16, true}, {"cdn.ads.example", 16, true}, {"ads.example", 1, false}, {"ads.example", 0, false}, {"ads.example", 255, false}, {"notads.example", 16, false}} {
		var p Probe
		ix.ProbeDomain(&p, tc.domain, "", tc.typ)
		got, ok := ix.FirstMatch(&p, ix.SetFor([]int{1}, false), true)
		if ok != tc.want || ok && got.Rule() != "||ads.example^$dnstype=TXT" {
			t.Errorf("%s type%d got%q/%t want%t", tc.domain, tc.typ, got.Rule(), ok, tc.want)
		}
	}
	ex := storedRule(t, listparse.Rule{Text: "ads.example$dnstype=~CNAME", Pattern: "ads.example", Kind: listparse.KindSubtree, DNSTypes: []uint16{5}, ExcludeTypes: true})
	ix = qualifiedIndex(t, []List{{ID: 1}}, [][]string{{ex}})
	for _, tc := range []struct {
		typ  uint16
		want bool
	}{{5, false}, {1, true}, {28, true}, {255, true}, {0, true}} {
		var p Probe
		ix.ProbeDomain(&p, "ads.example", "", tc.typ)
		_, ok := ix.FirstMatch(&p, ix.SetFor([]int{1}, false), true)
		if ok != tc.want {
			t.Errorf("exclude type%d=%t want%t", tc.typ, ok, tc.want)
		}
	}
}

func TestQualifiedPriorityMatrixIsListLocal(t *testing.T) {
	makeRule := func(exception, important bool) string {
		text := "||ads.example^"
		if exception {
			text = "@@" + text
		}
		if important {
			text += "$important"
		}
		return storedRule(t, listparse.Rule{Text: text, Pattern: "ads.example", Kind: listparse.KindSubtree, Exception: exception, Important: important})
	}
	normal, exception, important, importantException := makeRule(false, false), makeRule(true, false), makeRule(false, true), makeRule(true, true)
	for _, tc := range []struct {
		name  string
		rules []string
		want  string
	}{{"normal", []string{normal}, "||ads.example^"}, {"normal exception", []string{normal, exception}, ""}, {"important beats exception", []string{important, exception}, "||ads.example^$important"}, {"important exception", []string{important, importantException}, ""}, {"all", []string{normal, exception, important, importantException}, ""}, {"legacy exception", []string{important, "@@ads.example"}, "||ads.example^$important"}, {"legacy block", []string{"ads.example", exception}, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			ix := qualifiedIndex(t, []List{{ID: 1}}, [][]string{tc.rules})
			var p Probe
			ix.ProbeDomain(&p, "cdn.ads.example", "", 1)
			m, ok := ix.FirstMatch(&p, ix.SetFor([]int{1}, false), true)
			got := ""
			if ok {
				got = m.Rule()
			}
			if got != tc.want {
				t.Fatalf("rule=%q want%q", got, tc.want)
			}
		})
	}
	ix := qualifiedIndex(t, []List{{ID: 1}, {ID: 2}, {ID: 3, Allow: true}}, [][]string{{normal, importantException}, {important}, {"ads.example"}})
	cfg := Config{Lists: ix.Lists, Clients: map[string]*ListIDs{"192.0.2.1": {Block: []int{1, 2}}, "192.0.2.2": {Block: []int{1}}, "192.0.2.3": {Block: []int{2}, Allow: []int{3}}}}
	s := BuildSnapshot(cfg, ix)
	for _, tc := range []struct{ client, want string }{{"192.0.2.1", "block"}, {"192.0.2.2", "allow"}, {"192.0.2.3", "allow"}, {"192.0.2.4", "allow"}} {
		got := s.Evaluate(tc.client, netip.MustParseAddr(tc.client), "ads.example", 1)
		if got.Result != tc.want || got.RecordType != 1 {
			t.Errorf("%s got%s type%d want%s", tc.client, got.Result, got.RecordType, tc.want)
		}
	}
}

func TestRegexExceptionAndRuleLocalDenyAllow(t *testing.T) {
	regex := storedRule(t, listparse.Rule{Text: `/^ads[0-9]+\.example$/`, Pattern: `^ads[0-9]+\.example$`, Kind: listparse.KindRegex})
	except := storedRule(t, listparse.Rule{Text: `@@/^ads42\.example$/`, Pattern: `^ads42\.example$`, Kind: listparse.KindRegex, Exception: true})
	denied := storedRule(t, listparse.Rule{Text: "||example^$denyallow=safe.example", Pattern: "example", Kind: listparse.KindSubtree, DenyAllow: []string{"safe.example"}})
	ix := qualifiedIndex(t, []List{{ID: 1}, {ID: 2}}, [][]string{{regex, except}, {denied, "safe.example"}})
	for _, tc := range []struct {
		domain string
		list   int
		want   string
	}{{"ads1.example", 1, `/^ads[0-9]+\.example$/`}, {"ads42.example", 1, ""}, {"prefixads1.example", 1, ""}, {"safe.example", 2, "safe.example"}, {"cdn.safe.example", 2, "safe.example"}, {"unsafe.example", 2, "||example^$denyallow=safe.example"}} {
		var p Probe
		ix.ProbeDomain(&p, tc.domain, "", 1)
		got := ix.RuleIn(&p, ix.SlotOf(tc.list, false))
		if got != tc.want {
			t.Errorf("%s list%d=%q want%q", tc.domain, tc.list, got, tc.want)
		}
	}
}

func TestQualifiedImmutableUpdateAndInventory(t *testing.T) {
	first := storedRule(t, listparse.Rule{Text: "ads.example$dnstype=A", Pattern: "ads.example", Kind: listparse.KindSubtree, DNSTypes: []uint16{1}})
	second := storedRule(t, listparse.Rule{Text: `/tracker[0-9]/`, Pattern: `tracker[0-9]`, Kind: listparse.KindRegex})
	lists := []List{{ID: 20}, {ID: 10}, {ID: 30}}
	prev := qualifiedIndex(t, lists, [][]string{{first, "one.example", "two.example"}, {first}, {second}})
	nextLists := []List{{ID: 10}, {ID: 20}, {ID: 40}}
	next, err := prev.UpdateIndex(nextLists, []ListKey{{ID: 20}, {ID: 40}}, func(add func(int, string)) error { add(1, second); add(2, first); return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, ix := range []*Index{prev, next} {
		slot := ix.SlotOf(10, false)
		if !ix.Contains(slot, first) || !ix.Contains(slot, "ads.example$dnstype=A") || ix.ComplexCount(slot) != 1 {
			t.Fatal("typed rule missing from inventory")
		}
		var rows []string
		ix.Rules(slot, func(row string) { rows = append(rows, row) })
		if !reflect.DeepEqual(rows, []string{"ads.example$dnstype=A"}) {
			t.Fatalf("rules=%v", rows)
		}
		var p Probe
		ix.ProbeDomain(&p, "ads.example", "", 1)
		if got := ix.RuleIn(&p, slot); got != "ads.example$dnstype=A" {
			t.Fatalf("remapped=%q", got)
		}
	}
	if prev.SlotOf(30, false) < 0 || next.SlotOf(30, false) != -1 || !prev.Contains(prev.SlotOf(20, false), "one.example") || next.Contains(next.SlotOf(20, false), "one.example") {
		t.Fatal("update mutation or stale rule")
	}
	spread := next.SpreadOf([]int{next.SlotOf(10, false), next.SlotOf(40, false)})
	if !reflect.DeepEqual(spread, []RuleSpread{{Positions: []int{0, 1}, Rules: 1}}) {
		t.Fatalf("spread=%v", spread)
	}
}

func TestQualifiedAddressTypesExceptionsAndCascade(t *testing.T) {
	address := func(text, ip string, types []uint16, exception bool) string {
		return storedRule(t, listparse.Rule{Text: text, Pattern: ip, Kind: listparse.KindAddress, DNSTypes: types, Exception: exception})
	}
	typed := address("192.0.2.9$dnstype=A", "192.0.2.9", []uint16{1}, false)
	wrong := address("192.0.2.8$dnstype=AAAA", "192.0.2.8", []uint16{28}, false)
	except := address("@@192.0.2.9$dnstype=A", "192.0.2.9", []uint16{1}, true)
	ix := qualifiedIndex(t, []List{{ID: 1}, {ID: 2}, {ID: 3, Allow: true}}, [][]string{{typed, wrong}, {typed, except}, {"192.0.2.9"}})
	s := BuildSnapshot(Config{Lists: ix.Lists, Clients: map[string]*ListIDs{"192.0.2.1": {Block: []int{1}}, "192.0.2.2": {Block: []int{2}}, "192.0.2.3": {Block: []int{1}, Allow: []int{3}}}}, ix)
	for _, tc := range []struct {
		client string
		ips    []string
		want   bool
	}{{"192.0.2.1", []string{"192.0.2.9"}, true}, {"192.0.2.1", []string{"192.0.2.8"}, false}, {"192.0.2.2", []string{"192.0.2.9"}, false}, {"192.0.2.3", []string{"192.0.2.9"}, false}, {"192.0.2.1", []string{"invalid"}, false}} {
		if got := s.ResponseIPsBlocked(tc.client, tc.ips); got != tc.want {
			t.Errorf("%s %v=%t want%t", tc.client, tc.ips, got, tc.want)
		}
	}
}

func TestQualifiedAllPriorityCombinations(t *testing.T) {
	texts := []string{"ads.example", "@@ads.example", "ads.example$important", "@@ads.example$important"}
	for present := 0; present < 16; present++ {
		var rows []string
		for rank, text := range texts {
			if present&(1<<rank) != 0 {
				rows = append(rows, storedRule(t, listparse.Rule{Text: text, Pattern: "ads.example", Kind: listparse.KindSubtree, Exception: rank%2 == 1, Important: rank >= 2}))
			}
		}
		ix := qualifiedIndex(t, []List{{ID: 1}}, [][]string{rows})
		var p Probe
		ix.ProbeDomain(&p, "ads.example", "", 1)
		got, ok := ix.FirstMatch(&p, ix.SetFor([]int{1}, false), true)
		want := ""
		switch {
		case present&8 != 0:
		case present&4 != 0:
			want = "ads.example$important"
		case present&2 != 0:
		case present&1 != 0:
			want = "ads.example"
		}
		text := ""
		if ok {
			text = got.Rule()
		}
		if text != want {
			t.Fatalf("combination%d=%q want%q", present, text, want)
		}
	}
}

func TestQualifiedProbeSpillAndReuseAreLossless(t *testing.T) {
	var rows []string
	for i := 0; i < 24; i++ {
		text := fmt.Sprintf("/^ads[0-9]{%d}\\.example$/$important", i+1)
		rows = append(rows, storedRule(t, listparse.Rule{Text: text, Pattern: fmt.Sprintf("^ads[0-9]{%d}\\.example$", i+1), Kind: listparse.KindRegex, Important: true}))
	}
	// Every suffix regex matches the same query; the final exception must survive
	// the inline candidate capacity rather than being silently dropped.
	for i := 0; i < 16; i++ {
		rows = append(rows, storedRule(t, listparse.Rule{Text: fmt.Sprintf("/ads[0-9]+\\.example(?:%s)?$/", strings.Repeat("z", i+1)), Pattern: fmt.Sprintf("ads[0-9]+\\.example(?:%s)?$", strings.Repeat("z", i+1)), Kind: listparse.KindRegex}))
	}
	rows = append(rows, storedRule(t, listparse.Rule{Text: `@@/ads[0-9]+\.example$/$important`, Pattern: `ads[0-9]+\.example$`, Kind: listparse.KindRegex, Exception: true, Important: true}))
	ix := qualifiedIndex(t, []List{{ID: 1}}, [][]string{rows})
	var p Probe
	ix.ProbeDomain(&p, "ads123.example", "", 1)
	if len(p.candidates()) != 18 {
		t.Fatalf("matching candidate count=%d want18", len(p.candidates()))
	}
	if got := ix.RuleIn(&p, ix.SlotOf(1, false)); got != "" {
		t.Fatalf("late exception lost: %q", got)
	}
	ix.ProbeDomain(&p, "unmatched.example", "", 1)
	if len(p.candidates()) != 0 || ix.RuleIn(&p, ix.SlotOf(1, false)) != "" {
		t.Fatal("probe reuse retained old matches")
	}
}

func TestQualifiedANYAndTypedFastMatchAllocateNothing(t *testing.T) {
	row := storedRule(t, listparse.Rule{Text: "ads.example$dnstype=ANY", Pattern: "ads.example", Kind: listparse.KindExact, DNSTypes: []uint16{255}})
	ix := qualifiedIndex(t, []List{{ID: 1}}, [][]string{{row}})
	lists := ix.SetFor([]int{1}, false)
	for _, tc := range []struct {
		domain string
		typ    uint16
		want   bool
	}{{"ads.example", 255, true}, {"ads.example", 1, false}, {"ads.example", 0, false}, {"cdn.ads.example", 255, false}} {
		var p Probe
		ix.ProbeDomain(&p, tc.domain, "", tc.typ)
		_, got := ix.FirstMatch(&p, lists, true)
		if got != tc.want {
			t.Fatalf("%s type%d=%t want%t", tc.domain, tc.typ, got, tc.want)
		}
	}
	if got := testing.AllocsPerRun(100, func() { var p Probe; ix.ProbeDomain(&p, "ads.example", "", 255); ix.FirstMatch(&p, lists, true) }); got != 0 {
		t.Fatalf("typed exact match allocated%g want0", got)
	}
}

func TestQualifiedIPv6AndResponseTierCascade(t *testing.T) {
	row := storedRule(t, listparse.Rule{Text: "2001:db8::9$dnstype=AAAA", Pattern: "2001:db8::9", Kind: listparse.KindAddress, DNSTypes: []uint16{28}})
	ix := qualifiedIndex(t, []List{{ID: 1}, {ID: 2, Allow: true}}, [][]string{{row, "192.0.2.9"}, {"2001:db8::9"}})
	s := BuildSnapshot(Config{Lists: ix.Lists, Ranges: []RangeConfig{{CIDR: "192.0.2.0/24", ListIDs: ListIDs{Block: []int{1}}}}, Groups: map[int]GroupConfig{1: {ListIDs: ListIDs{Allow: []int{2}}}}, Memberships: []GroupMembership{{ClientIP: "192.0.2.2", GroupID: 1}, {ClientIP: "192.0.2.3", GroupID: 1}}, Clients: map[string]*ListIDs{"192.0.2.3": {Block: []int{1}}}}, ix)
	for _, tc := range []struct {
		client string
		ips    []string
		want   bool
	}{{"192.0.2.1", []string{"2001:db8::9"}, true}, {"192.0.2.2", []string{"2001:db8::9"}, false}, {"192.0.2.3", []string{"2001:db8::9"}, true}, {"192.0.2.2", []string{"192.0.2.9", "2001:db8::9"}, false}, {"198.51.100.1", []string{"2001:db8::9"}, false}} {
		if got := s.ResponseIPsBlocked(tc.client, tc.ips); got != tc.want {
			t.Errorf("%s %v=%t want%t", tc.client, tc.ips, got, tc.want)
		}
	}
}

func TestReverseAddressExceptionsStayListLocalAndDoNotExemptAllowDomains(t *testing.T) {
	name := "9.2.0.192.in-addr.arpa"
	ix := qualifiedIndex(t, []List{{ID: 1}, {ID: 2}, {ID: 3, Allow: true}, {ID: 4, Allow: true}}, [][]string{{"192.0.2.9", "@@192.0.2.9"}, {name}, {name, "@@192.0.2.9"}, {"192.0.2.9"}})
	var p Probe
	ix.ProbeDomain(&p, name, "192.0.2.9", 12)
	if _, ok := ix.FirstMatch(&p, ix.SetFor([]int{1}, false), true); ok {
		t.Error("ordinary address exception did not exempt reverse-address block")
	}
	if m, ok := ix.FirstMatch(&p, ix.SetFor([]int{1, 2}, false), true); !ok || ix.Lists[m.Slot].ID != 2 || m.Rule() != name {
		t.Errorf("otherlist reverse block=%+v/%t", m, ok)
	}
	if m, ok := ix.FirstMatch(&p, ix.SetFor([]int{3}, true), false); !ok || m.Rule() != name {
		t.Errorf("reverse-address exception overrode literal allow-domain=%+v/%t", m, ok)
	}
	if _, ok := ix.FirstMatch(&p, ix.SetFor([]int{4}, true), false); ok {
		t.Error("address-only allow incorrectly overrides reverse query")
	}
}
