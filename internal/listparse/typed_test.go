package listparse

import (
	"reflect"
	"strings"
	"testing"
)

func TestTypedRulesPreserveRestrictions(t *testing.T) {
	body := "legacy.example\n|google.com|$dnstype=TXT\n@@/Track[A-Z]+\\.example$/$important\n||example.com^$denyallow=safe.example.com\n||example.com^$dnstype=~CNAME\n"
	got, err := Parse(strings.NewReader(body), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Domains, []string{"legacy.example"}) || len(got.Rules) != 4 || got.Unsupported != 0 || got.ModifiersIgnored != 0 {
		t.Fatalf("parsed %+v", got)
	}
	want := []Rule{
		{Version: 1, Text: "|google.com|$dnstype=TXT", Pattern: "google.com", Kind: KindExact, DNSTypes: []uint16{16}},
		{Version: 1, Text: "@@/Track[A-Z]+\\.example$/$important", Pattern: "Track[A-Z]+\\.example$", Kind: KindRegex, Exception: true, Important: true},
		{Version: 1, Text: "||example.com^$denyallow=safe.example.com", Pattern: "example.com", Kind: KindSubtree, DenyAllow: []string{"safe.example.com"}},
		{Version: 1, Text: "||example.com^$dnstype=~CNAME", Pattern: "example.com", Kind: KindSubtree, DNSTypes: []uint16{5}, ExcludeTypes: true},
	}
	if !reflect.DeepEqual(got.Rules, want) {
		t.Fatalf("rules=%+v want=%+v", got.Rules, want)
	}
}

func TestUnsupportedRulesNeverBroadenAndDiagnosticsKeepEveryLine(t *testing.T) {
	body := strings.Repeat("||google.com^$client=~private\n", 12) + "||apple.com^$ctag=~device_pc\n||example.org^$script\n||example.org^$match-case\n"
	got, err := Parse(strings.NewReader(body), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if got.Unsupported != 15 || len(got.Diagnostics) != 15 || len(got.Domains) != 0 || len(got.Rules) != 0 {
		t.Fatalf("parsed %+v", got)
	}
	if got.Diagnostics[11].Line != 12 || got.Diagnostics[11].Rule != "||google.com^$client=~private" || got.Diagnostics[11].Reason != "unsupported modifier: client" {
		t.Fatalf("diagnostic=%+v", got.Diagnostics[11])
	}
}

func TestBadfilterCancelsOnlyIdenticalQualifiedRule(t *testing.T) {
	body := "||example.com^$dnstype=TXT\n||example.com^$dnstype=A\n||example.com^$dnstype=TXT,badfilter\n@@||example.com^\n||example.com^$important\n||example.com^$badfilter\n"
	got, err := Parse(strings.NewReader(body), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 2 || got.Rules[0].Text != "||example.com^$dnstype=A" || got.Rules[1].Text != "||example.com^$important" || !reflect.DeepEqual(got.Exceptions, []string{"example.com"}) {
		t.Fatalf("parsed=%+v", got)
	}
}

func TestInvalidRegexRejectsWholeRefresh(t *testing.T) {
	got, err := Parse(strings.NewReader("safe.example\n/[unclosed/\n"), 1<<20, 100)
	if err == nil || !strings.Contains(err.Error(), "line 2: invalid regular expression") || !reflect.DeepEqual(got, Result{}) {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestTypedStoragePreservesRegexAndAllowlistBehavior(t *testing.T) {
	r := Rule{Version: 1, Text: "@@/[A-Z]+\\.example/$important", Pattern: "[A-Z]+\\.example", Kind: KindRegex, Exception: true, Important: true}
	encoded, err := EncodeStored(r)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := NormalizeStored(encoded)
	if err != nil || normalized != encoded {
		t.Fatalf("normalize=%q err=%v", normalized, err)
	}
	decoded, err := DecodeStored(encoded)
	if err != nil || !reflect.DeepEqual(decoded, r) {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	rows, count := StoredRows(Result{Rules: []Rule{r}}, false)
	if count != 1 || len(rows) != 1 {
		t.Fatalf("rows=%v count=%d", rows, count)
	}
	allowed, err := DecodeStored(rows[0])
	if err != nil || allowed.Exception || allowed.Text != r.Text {
		t.Fatalf("allow=%+v err=%v", allowed, err)
	}
	for _, bad := range []string{"!svart-rule-v2:{}", StoredPrefix + `{"version":2}`, StoredPrefix + `{"version":1,"kind":"wat","pattern":"example.com"}`, StoredPrefix + `{"version":1,"kind":"exact","pattern":"example.com","extra":true}`} {
		if _, err := DecodeStored(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestMaskAnchorsAndRegexDollar(t *testing.T) {
	for _, tc := range []struct{ input, yes, no string }{
		{"|example.org|", "example.org", "www.example.org"},
		{"|example", "example.org", "sub.example"},
		{"ample.org|", "example.org", "example.org.com"},
		{"||example.org", "sub.example.org.com", "notexample.org"},
		{"||example.org^", "sub.example.org", "example.org.com"},
		{".example.org", "sub.example.org.com", "example.org"},
		{"/track[0-9]+\\.example$/", "track12.example", "track12.example.com"},
		{"*$dnstype=AAAA", "anything.example", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tc.input), 1<<20, 100)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ := StoredRows(got, true)
			if len(rows) != 1 {
				t.Fatalf("rows=%v result=%+v", rows, got)
			}
			r, err := DecodeStored(rows[0])
			if err != nil {
				t.Fatal(err)
			}
			re, err := CompilePattern(r)
			if err != nil {
				t.Fatal(err)
			}
			if !re.MatchString(tc.yes) {
				t.Errorf("%s misses %s", re, tc.yes)
			}
			if tc.no != "" && re.MatchString(tc.no) {
				t.Errorf("%s matches %s", re, tc.no)
			}
		})
	}
}

func TestDNSTypeValidationAndMixedPositiveSemantics(t *testing.T) {
	got, err := Parse(strings.NewReader("||example.com^$dnstype=~CNAME|txt|TXT\n||invalid.example^$dnstype=NOTATYPE\n"), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 || !reflect.DeepEqual(got.Rules[0].DNSTypes, []uint16{16}) || got.Rules[0].ExcludeTypes || got.Invalid != 1 || len(got.Diagnostics) != 1 {
		t.Fatalf("parsed=%+v", got)
	}
}

func TestAddressRangesAndBrowserPathsNeverBecomeWholeHostRules(t *testing.T) {
	body := "192.0.2.0/24\n://192.0.2.\n||example.com/tracker\n||example.com^$dnstype=\n"
	got, err := Parse(strings.NewReader(body), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Lines: 4, Unsupported: 3, Invalid: 1, InvalidAt: []int{4}, Diagnostics: []Diagnostic{
		{Line: 1, Rule: "192.0.2.0/24", Reason: "address ranges are unsupported"},
		{Line: 2, Rule: "://192.0.2.", Reason: "address ranges are unsupported"},
		{Line: 3, Rule: "||example.com/tracker", Reason: "URL path requires browser context"},
		{Line: 4, Rule: "||example.com^$dnstype=", Reason: "dnstype requires record types"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed=%+v want=%+v", got, want)
	}
}

func TestBadfilterDoesNotConflateEquivalentOrHostsSyntax(t *testing.T) {
	body := "||example.com^\nexample.com\n||example.com^$badfilter\n0.0.0.0 hosts.example\n0.0.0.0 hosts.example$badfilter\n"
	got, err := Parse(strings.NewReader(body), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{Domains: []string{"example.com", "hosts.example"}, Lines: 5, Unsupported: 1, Diagnostics: []Diagnostic{{Line: 5, Rule: "0.0.0.0 hosts.example$badfilter", Reason: "badfilter on hosts syntax"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed=%+v want=%+v", got, want)
	}
}

func TestTrailingDotMaskKeepsLabelBoundary(t *testing.T) {
	for _, text := range []string{"||glthub.", "://ww4."} {
		got, err := Parse(strings.NewReader(text), 1<<20, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Rules) != 1 {
			t.Fatalf("parsed=%+v", got)
		}
		re, err := CompilePattern(got.Rules[0])
		if err != nil {
			t.Fatal(err)
		}
		yes, no := "glthub.example", "glthubtracker.example"
		if text == "://ww4." {
			yes, no = "ww4.example", "ww4tracker.example"
		}
		if !re.MatchString(yes) || re.MatchString(no) {
			t.Fatalf("mask=%s yes=%s no=%s", re, yes, no)
		}
	}
}

func TestEmptyRegexCompilesAndRoundTripsWithoutPartialPublication(t *testing.T) {
	got, err := Parse(strings.NewReader("//$dnstype=A"), 1<<20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rules) != 1 || got.Rules[0].Kind != KindRegex || got.Rules[0].Pattern != "" {
		t.Fatalf("parsed=%+v", got)
	}
	rows, count := StoredRows(got, true)
	if len(rows) != 1 || count != 1 {
		t.Fatalf("rows=%v count=%d", rows, count)
	}
	r, err := DecodeStored(rows[0])
	if err != nil || r.Text != "//$dnstype=A" || r.Pattern != "" {
		t.Fatalf("rule=%+v err=%v", r, err)
	}
}

func TestLegacyExceptionNormalizationKeepsPrefix(t *testing.T) {
	got, err := NormalizeStored(" @@BÜCHER.example. ")
	if err != nil || got != "@@xn--bcher-kva.example" {
		t.Fatalf("normalized=%q err=%v", got, err)
	}
	r, err := DecodeStored(got)
	if err != nil || r.Kind != KindSubtree || r.Pattern != "xn--bcher-kva.example" || !r.Exception {
		t.Fatalf("rule=%+v err=%v", r, err)
	}
}
