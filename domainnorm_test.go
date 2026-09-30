package main

import (
	"reflect"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

// TestCouldBeIPNeverHidesAnAddress: the prefilter may only skip strings that
// netip.ParseAddr rejects.
func TestReverseARPAToIPv6(t *testing.T) {
	for in, want := range map[string]string{
		"b.a.9.8.7.6.5.0.4.0.0.0.3.0.0.0.2.0.0.0.1.0.0.0.0.0.0.0.1.2.3.4.ip6.arpa": "4321:0:1:2:3:4:567:89ab",
		"1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.0.8.b.d.0.1.0.0.2.ip6.arpa": "2001:db8::1",
		"B.A.9.8.7.6.5.0.4.0.0.0.3.0.0.0.2.0.0.0.1.0.0.0.0.0.0.0.1.2.3.4.ip6.arpa": "4321:0:1:2:3:4:567:89ab",
		"8.b.d.0.1.0.0.2.ip6.arpa": "", // partial reverse zone
		"g.a.9.8.7.6.5.0.4.0.0.0.3.0.0.0.2.0.0.0.1.0.0.0.0.0.0.0.1.2.3.4.ip6.arpa": "",
		"ba.98.7.6.5.0.4.0.0.0.3.0.0.0.2.0.0.0.1.0.0.0.0.0.0.0.1.2.3.4.ip6.arpa.x": "",
		"300.64.200.193.in-addr.arpa": "",
		"30.64.200.193.in-addr.arpa":  "193.200.64.30",
	} {
		if got := policycore.ReverseARPAToIP(in); got != want {
			t.Errorf("reverseARPAToIP(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestProbeDomainSuffixes pins the label walk the matcher relies on: every
// suffix of the name is probed, nearest first, without allocating.
func TestProbeDomainSuffixes(t *testing.T) {
	levels := []string{"pagead2.g.doubleclick.net", "g.doubleclick.net", "doubleclick.net", "net"}
	var lists []fixtureList
	for i, rule := range levels {
		lists = append(lists, fixtureList{id: i + 1, rules: []string{rule}})
	}
	ix := buildFixtureIndex(t, lists, false)
	var p policycore.Probe
	ix.ProbeDomain(&p, levels[0], "", 1)
	var got []string
	for _, h := range p.Hits() {
		got = append(got, levels[0][h.Level:])
	}
	if !reflect.DeepEqual(got, levels) {
		t.Errorf("probed suffixes %q, want %q", got, levels)
	}
	if a := testing.AllocsPerRun(100, func() { ix.ProbeDomain(&p, levels[0], "", 1) }); a != 0 {
		t.Errorf("probeDomain allocated %.0f times", a)
	}
}
