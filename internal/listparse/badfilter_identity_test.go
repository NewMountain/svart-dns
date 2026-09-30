package listparse

import (
	"strings"
	"testing"
)

func TestBadfilterCanonicalizesSupportedModifierIdentity(t *testing.T) {
	for _, pair := range [][2]string{
		{"||example.org^$important,dnstype=A", "||example.org^$dnstype=A,important,badfilter"},
		{"||example.org^$dnstype=A|AAAA", "||example.org^$dnstype=AAAA|A,badfilter"},
		{"||example.org^$dnstype=A|AAAA", "||example.org^$dnstype=a|aaaa,badfilter"},
		{"||example.org^$dnstype=~A|~AAAA", "||example.org^$dnstype=~aaaa|~a,badfilter"},
		{"||example.org^$denyallow=a.example.org|b.example.org,important", "||example.org^$important,denyallow=B.EXAMPLE.ORG|a.example.org,badfilter"},
		{"@@/Track[A-Z]+\\.example/$dnstype=TXT,important", "@@/Track[A-Z]+\\.example/$important,dnstype=txt,badfilter"},
	} {
		t.Run(pair[0], func(t *testing.T) {
			got, err := Parse(strings.NewReader(strings.Join(pair[:], "\n")), 1<<20, 10)
			if err != nil || len(got.Domains)+len(got.Exceptions)+len(got.Rules) != 0 || got.Unsupported+got.Invalid != 0 {
				t.Fatalf("equivalent badfilter did not cancel: %+v err=%v", got, err)
			}
		})
	}
}

func TestBadfilterKeepsPatternCaseAnchorsAndRestrictionsDistinct(t *testing.T) {
	for _, pair := range [][2]string{
		{"||EXAMPLE.org^$dnstype=A", "||example.org^$dnstype=A,badfilter"},
		{"||example.org^$dnstype=A", "||example.org^$dnstype=AAAA,badfilter"},
		{"||example.org^$dnstype=A", "||example.org^$dnstype=~A,badfilter"},
		{"||example.org^$important", "||example.org^$badfilter"},
		{"|example.org|", "||example.org^$badfilter"},
		{"@@||example.org^$dnstype=A", "||example.org^$dnstype=A,badfilter"},
		{"/Track[0-9]+\\.example/", "/track[0-9]+\\.example/$badfilter"},
		{"||example.org^$denyallow=a.example.org", "||example.org^$denyallow=b.example.org,badfilter"},
	} {
		t.Run(pair[0]+pair[1], func(t *testing.T) {
			got, err := Parse(strings.NewReader(strings.Join(pair[:], "\n")), 1<<20, 10)
			if err != nil || len(got.Domains)+len(got.Exceptions)+len(got.Rules) != 1 || got.Unsupported+got.Invalid != 0 {
				t.Fatalf("different rule canceled: %+v err=%v", got, err)
			}
		})
	}
}

func TestUppercaseModifierNamesSkipTheWholeRule(t *testing.T) {
	for _, modifier := range []string{"IMPORTANT", "DNSTYPE=A", "DENYALLOW=ok.example.org", "BADFILTER"} {
		text := "||example.org^$" + modifier
		got, err := Parse(strings.NewReader(text), 1<<20, 10)
		if err != nil || got.Unsupported != 1 || got.Invalid != 0 || len(got.Domains)+len(got.Rules) != 0 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Rule != text || got.Diagnostics[0].Reason != "unsupported modifier: "+strings.Split(modifier, "=")[0] {
			t.Fatalf("uppercase modifier accepted: %+v err=%v", got, err)
		}
	}
	got, err := Parse(strings.NewReader("||example.org^\n||example.org^$BADFILTER"), 1<<20, 10)
	if err != nil || len(got.Domains) != 1 || got.Domains[0] != "example.org" || got.Unsupported != 1 {
		t.Fatalf("unsupported badfilter changed valid rule: %+v err=%v", got, err)
	}
}
