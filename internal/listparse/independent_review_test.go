package listparse

import (
	"strings"
	"testing"
)

func TestBrowserPathsNeverBecomeHostRules(t *testing.T) {
	for _, input := range []string{"example.org/path", "example.org/path$dnstype=A", "@@example.org/path", "example.org/path$important", "||example.org/path"} {
		t.Run(input, func(t *testing.T) {
			r, err := Parse(strings.NewReader(input), 1<<20, 100)
			if err != nil {
				t.Fatal(err)
			}
			rows, n := StoredRows(r, true)
			if len(rows) != 0 || n != 0 || r.Unsupported != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Reason != "URL path requires browser context" {
				t.Fatalf("browser rule broadened: rows=%v count=%d result=%+v", rows, n, r)
			}
		})
	}
}

func TestNamedURLSchemesRequireBrowserContext(t *testing.T) {
	for _, input := range []string{"https://example.org/path", "https://example.org/", "|https://example.org^", "http://example.org/", "http://secured21.*.com/|", "ftp://example.org/", "@@https://example.org/$dnstype=A"} {
		t.Run(input, func(t *testing.T) {
			r, err := Parse(strings.NewReader(input), 1<<20, 100)
			if err != nil {
				t.Fatal(err)
			}
			rows, n := StoredRows(r, true)
			if len(rows) != 0 || n != 0 || r.Unsupported != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Reason != "URL scheme requires browser context" {
				t.Fatalf("scheme rule broadened: rows=%v count=%d result=%+v", rows, n, r)
			}
		})
	}
}

func TestTrailingURLSlashIsNotAHostnameEndAnchor(t *testing.T) {
	for _, input := range []string{"://trk.*.run/", "||example.org/", "example.org/", "@@://trk.*.run/$important"} {
		t.Run(input, func(t *testing.T) {
			r, err := Parse(strings.NewReader(input), 1<<20, 100)
			if err != nil {
				t.Fatal(err)
			}
			rows, n := StoredRows(r, true)
			if len(rows) != 0 || n != 0 || r.Unsupported != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Reason != "URL path requires browser context" {
				t.Fatalf("slash rule broadened: rows=%v count=%d result=%+v", rows, n, r)
			}
		})
	}
}

func TestMixedDNSTypeExclusionsNeverBroaden(t *testing.T) {
	for _, input := range []string{"||example.org^$dnstype=A|~A", "||example.org^$dnstype=~A|A"} {
		r, err := Parse(strings.NewReader(input), 1<<20, 100)
		if err != nil {
			t.Fatal(err)
		}
		rows, n := StoredRows(r, true)
		if len(rows) != 0 || n != 0 || r.Invalid != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Reason != "dnstype includes are all excluded" {
			t.Fatalf("contradiction broadened: %+v rows=%v count=%d", r, rows, n)
		}
	}
	for _, tc := range []struct {
		input string
		want  uint16
	}{{"||example.org^$dnstype=A|AAAA|~A", 28}, {"||example.org^$dnstype=TXT|A|~TXT", 1}} {
		r, err := Parse(strings.NewReader(tc.input), 1<<20, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Rules) != 1 || len(r.Rules[0].DNSTypes) != 1 || r.Rules[0].DNSTypes[0] != tc.want || r.Rules[0].ExcludeTypes {
			t.Fatalf("mixed types=%+v want%d", r, tc.want)
		}
	}
}

func TestUnrestrictedEmptyRegexIsDiagnosed(t *testing.T) {
	for _, input := range []string{"//", "@@//", "//$important", "@@//$important"} {
		r, err := Parse(strings.NewReader(input), 1<<20, 100)
		if err != nil {
			t.Fatal(err)
		}
		rows, n := StoredRows(r, true)
		if len(rows) != 0 || n != 0 || r.Invalid != 1 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Reason != "empty regex requires dnstype or denyallow" {
			t.Fatalf("unrestricted empty regex accepted: %+v rows=%v count=%d", r, rows, n)
		}
	}
	for _, input := range []string{"//$dnstype=A", "//$denyallow=example.org"} {
		r, err := Parse(strings.NewReader(input), 1<<20, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Rules) != 1 || r.Rules[0].Kind != KindRegex || r.Rules[0].Pattern != "" || r.Invalid != 0 || r.Unsupported != 0 {
			t.Fatalf("constrained empty regex rejected: %+v", r)
		}
	}
}
