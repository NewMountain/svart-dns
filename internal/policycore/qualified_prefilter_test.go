package policycore

import (
	"testing"

	"github.com/yeti/svart-dns/internal/listparse"
)

func TestQualifiedNecessaryLiteralAndAdversarialMatches(t *testing.T) {
	cases := []struct {
		kind             listparse.RuleKind
		pattern, literal string
		matches, misses  []string
	}{
		{listparse.KindMask, "||ads.*.example^", ".example", []string{"ads.a.example", "cdn.ads.a.example"}, []string{"notads.a.example", "ads.a.example.evil"}},
		{listparse.KindMask, "|ads*example|", "example", []string{"adsexample", "ads.a.example"}, []string{"x.adsexample", "adsexample.evil"}},
		{listparse.KindMask, "foo*bar", "foo", []string{"xfoobarx", "foo.bar"}, []string{"foo", "barfoo"}},
		{listparse.KindRegex, `^(ads|track)\d+\.example$`, ".example", []string{"ads12.example", "TRACK1.EXAMPLE"}, []string{"evilads12.example", "ads.example"}},
		{listparse.KindRegex, `^(foo)?bar$`, "bar", []string{"bar", "foobar"}, []string{"foo", "barfoo"}},
		{listparse.KindRegex, `^(foo)*bar$`, "bar", []string{"bar", "foofoobar"}, []string{"foo"}},
		{listparse.KindRegex, `^(foo){0,3}bar$`, "bar", []string{"bar", "foofoobar"}, []string{"foofoofoofoobar"}},
		{listparse.KindRegex, `^(foo){2,3}bar$`, "foo", []string{"foofoobar", "foofoofoobar"}, []string{"bar", "foobar"}},
		{listparse.KindRegex, `^(longone|two)$`, "", []string{"longone", "two"}, []string{"longtwo"}},
		{listparse.KindRegex, `^(?:x\.)?a\.b\+c$`, "a.b+c", []string{"a.b+c", "x.a.b+c"}, []string{"aZb+c", "a.bbc"}},
		{listparse.KindRegex, `^kilo$`, "kilo", []string{"kilo", "KILO", "Kilo"}, []string{"xkilo"}},
		{listparse.KindRegex, `^some$`, "some", []string{"some", "ſome"}, []string{"xsome"}},
		{listparse.KindRegex, `^ſome$`, "some", []string{"some", "ſome"}, []string{"xsome"}},
		{listparse.KindRegex, `(?-i:^UPPER$)`, "upper", []string{"UPPER"}, []string{"upper"}},
		{listparse.KindRegex, `.*`, "", []string{"", "anything"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			row := storedRule(t, listparse.Rule{Text: tc.pattern, Pattern: tc.pattern, Kind: tc.kind})
			ix := qualifiedIndex(t, []List{{ID: 1}}, [][]string{{row}})
			r := &ix.Qualified[0]
			if r.requiredLiteral != tc.literal {
				t.Fatalf("literal=%q want %q", r.requiredLiteral, tc.literal)
			}
			for _, name := range tc.matches {
				var p Probe
				ix.ProbeDomain(&p, name, "", 1)
				if len(p.candidates()) != 1 {
					t.Errorf("lost match %q", name)
				}
			}
			for _, name := range tc.misses {
				var p Probe
				ix.ProbeDomain(&p, name, "", 1)
				if len(p.candidates()) != 0 {
					t.Errorf("false match %q", name)
				}
			}
			names := []string{""}
			for size := 0; size < 4; size++ {
				start := len(names)
				for _, name := range append([]string(nil), names...) {
					if len(name) != size {
						continue
					}
					for _, ch := range "ab.ks1" {
						names = append(names, name+string(ch))
					}
				}
				if len(names) == start {
					break
				}
			}
			for _, name := range names {
				var p Probe
				ix.ProbeDomain(&p, name, "", 1)
				if got, want := len(p.candidates()) == 1, r.compiled.MatchString(name); got != want {
					t.Fatalf("prefilter changed authoritative regexp match on %q: %t want %t", name, got, want)
				}
			}
		})
	}
}
