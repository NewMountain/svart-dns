package listparse

import (
	"reflect"
	"strings"
	"testing"
)

func TestProductionHostsCommentsDoNotChangeRuleGrammar(t *testing.T) {
	for _, line := range []string{
		"0.0.0.0 docs.pipenv.org # https://github.com/StevenBlack/hosts/issues/1635",
		"0.0.0.0 docs.pipenv.org #[Win32/InstallCore.NR]",
		"127.0.0.1 docs.pipenv.org # $client=private,$dnstype=TXT /regex/",
		"0.0.0.0 docs.pipenv.org # literal | pipe * wildcard",
		"127.0.0.1\tdocs.pipenv.org # https://example.org/path",
	} {
		got, err := Parse(strings.NewReader(line), 1<<20, 10)
		want := Result{Domains: []string{"docs.pipenv.org"}, Lines: 1}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("parse %q=%+v err=%v want=%+v", line, got, err, want)
		}
	}
}

func TestHostsCommentsKeepQualifiedProvenanceAndDoNotEraseRealPaths(t *testing.T) {
	const line = "0.0.0.0 ads.example$dnstype=TXT # documentation https://example.org/path $client=private"
	got, err := Parse(strings.NewReader(line), 1<<20, 10)
	want := Result{Lines: 1, Rules: []Rule{{Version: 1, Text: line, Pattern: "ads.example", Kind: KindSubtree, DNSTypes: []uint16{16}}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("qualified=%+v err=%v want=%+v", got, err, want)
	}
	bad := "0.0.0.0 ads.example/tracker # trailing comment"
	got, err = Parse(strings.NewReader(bad), 1<<20, 10)
	if err != nil || got.Unsupported != 1 || len(got.Domains) != 0 || len(got.Rules) != 0 || !reflect.DeepEqual(got.Diagnostics, []Diagnostic{{Line: 1, Rule: bad, Reason: "URL path requires browser context"}}) {
		t.Fatalf("path=%+v err=%v", got, err)
	}
}

func TestReservedDNSRecordTypeIsNotAFilterType(t *testing.T) {
	got, err := Parse(strings.NewReader("||ads.example^$dnstype=RESERVED\n||ads.example^$dnstype=~RESERVED"), 1<<20, 10)
	want := Result{Lines: 2, Invalid: 2, InvalidAt: []int{1, 2}, Diagnostics: []Diagnostic{
		{Line: 1, Rule: "||ads.example^$dnstype=RESERVED", Reason: "unknown DNS type: RESERVED"},
		{Line: 2, Rule: "||ads.example^$dnstype=~RESERVED", Reason: "unknown DNS type: RESERVED"},
	}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("reserved=%+v err=%v want=%+v", got, err, want)
	}
}

func TestHostsCommentCannotTurnMissingHostnameIntoAddress(t *testing.T) {
	for _, line := range []string{"0.0.0.0 # https://example.org", "127.0.0.1\t# $client=private"} {
		got, err := Parse(strings.NewReader(line), 1<<20, 10)
		if err != nil || got.Invalid != 1 || len(got.Domains) != 0 || len(got.Rules) != 0 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Rule != line {
			t.Fatalf("missing host %q: %+v err=%v", line, got, err)
		}
	}
}
