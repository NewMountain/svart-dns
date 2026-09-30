package listparse

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestParseListBodyModifiers(t *testing.T) {
	body := strings.Join([]string{
		"|google.com|$dnstype=TXT",
		"||*.loan^$dnstype=~CNAME",
		"||tracker.example.net^$client=192.168.1.20",
		"||cdn.example.org^$third-party",
		"||ads.example.com^$important",
		"23.22.126.183$all,ipaddress",
		"||amazonaws.com^",
		"||amazonaws.com^$badfilter",
		"||rewrite.example.com^$dnsrewrite=192.0.2.1",
		"://ww4.$denyallow=ww4.ikea.com|ww4.autotask.net",
	}, "\n")
	got, err := Parse(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Domains: []string{},
		Rules: []Rule{
			{Version: 1, Text: "|google.com|$dnstype=TXT", Pattern: "google.com", Kind: KindExact, DNSTypes: []uint16{16}},
			{Version: 1, Text: "||*.loan^$dnstype=~CNAME", Pattern: "||*.loan^", Kind: KindMask, DNSTypes: []uint16{5}, ExcludeTypes: true},
			{Version: 1, Text: "||ads.example.com^$important", Pattern: "ads.example.com", Kind: KindSubtree, Important: true},
			{Version: 1, Text: "://ww4.$denyallow=ww4.ikea.com|ww4.autotask.net", Pattern: "|ww4.", Kind: KindMask, DenyAllow: []string{"ww4.autotask.net", "ww4.ikea.com"}},
		},
		Diagnostics: []Diagnostic{
			{Line: 3, Rule: "||tracker.example.net^$client=192.168.1.20", Reason: "unsupported modifier: client"},
			{Line: 4, Rule: "||cdn.example.org^$third-party", Reason: "unsupported modifier: third-party"},
			{Line: 6, Rule: "23.22.126.183$all,ipaddress", Reason: "unsupported modifier: all"},
			{Line: 9, Rule: "||rewrite.example.com^$dnsrewrite=192.0.2.1", Reason: "unsupported modifier: dnsrewrite"},
		},
		Lines: 10, Unsupported: 4,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed = %+v\nwant     %+v", got, want)
	}
}

func TestListRulePatterns(t *testing.T) {
	for _, tc := range []struct {
		input    string
		kind     RuleKind
		pattern  string
		ordinary []string
	}{
		{"||ads.example.com^", KindSubtree, "ads.example.com", []string{"ads.example.com"}},
		{"0.0.0.0 ads.example.com", KindSubtree, "ads.example.com", []string{"ads.example.com"}},
		{"pixel.example.com.", KindSubtree, "pixel.example.com", []string{"pixel.example.com"}},
		{"||*.top^", KindMask, "||*.top^", []string{"*.top"}},
		{"||glthub.", KindMask, "||glthub.", nil},
		{"||masto*.top^", KindMask, "||masto*.top^", nil},
		{"||adidas-outlet*", KindMask, "||adidas-outlet*", nil},
		{"|cisco.com|", KindExact, "cisco.com", nil},
		{"://ww4.", KindMask, "|ww4.", nil},
		{"://blog.*.top^", KindMask, "|blog.*.top^", nil},
		{".booking.com-id*", KindMask, ".booking.com-id*", nil},
		{".com-id*.info^", KindMask, ".com-id*.info^", nil},
		{"*austrailasale.", KindMask, "*austrailasale.", nil},
		{"*norge-salg.com^", KindMask, "*norge-salg.com^", nil},
		{"://[2600:3c06::f03c:95ff:fed9:ce5e]^", KindAddress, "2600:3c06::f03c:95ff:fed9:ce5e", []string{"2600:3c06::f03c:95ff:fed9:ce5e"}},
		{"212.27.60.108", KindAddress, "212.27.60.108", []string{"212.27.60.108"}},
	} {
		got, reason, unsupported, err := parseTypedRule(tc.input)
		if err != nil || reason != "" || unsupported || got.rule.Kind != tc.kind || got.rule.Pattern != tc.pattern || !reflect.DeepEqual(got.ordinary, tc.ordinary) {
			t.Errorf("parse(%q)=%+v reason=%s unsupported=%t err=%v; expected kind=%s pattern=%s ordinary=%v", tc.input, got, reason, unsupported, err, tc.kind, tc.pattern, tc.ordinary)
		}
	}
	for _, input := range []string{"://46.148.113.", "://193.143.1.*"} {
		_, reason, unsupported, err := parseTypedRule(input)
		if reason != "address ranges are unsupported" || !unsupported || err != nil {
			t.Errorf("parse(%q) reason=%s unsupported=%t err=%v", input, reason, unsupported, err)
		}
	}
	for _, input := range []string{"://", "||"} {
		_, reason, unsupported, err := parseTypedRule(input)
		if reason != "invalid hostname mask" || unsupported || err != nil {
			t.Errorf("parse(%q) reason=%s unsupported=%t err=%v", input, reason, unsupported, err)
		}
	}
}

// TestParseListBodyModifiers: a rule is applied only when it can be applied
// as written or more narrowly. Applying "|google.com|$dnstype=TXT" as a
// whole-name block would take google.com off the network.

func TestParseListBodyKeepsProductionRules(t *testing.T) {
	body := strings.Join([]string{
		"! Title: Spam TLDs",
		"||*.actor^",
		"||*.africa^$denyallow=nation.africa",
		"||*.top^$dnstype=~CNAME,denyallow=caitlin.top|reminder.top",
		"||*.bid^$dnstype=~CNAME",
		"*austrailasale.",
		"@@||masto*.top^",
		"example.org.",
	}, "\n")
	got, err := Parse(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Domains: []string{"*.actor", "example.org"},
		Rules: []Rule{
			{Version: 1, Text: "||*.africa^$denyallow=nation.africa", Pattern: "||*.africa^", Kind: KindMask, DenyAllow: []string{"nation.africa"}},
			{Version: 1, Text: "||*.top^$dnstype=~CNAME,denyallow=caitlin.top|reminder.top", Pattern: "||*.top^", Kind: KindMask, DNSTypes: []uint16{5}, ExcludeTypes: true, DenyAllow: []string{"caitlin.top", "reminder.top"}},
			{Version: 1, Text: "||*.bid^$dnstype=~CNAME", Pattern: "||*.bid^", Kind: KindMask, DNSTypes: []uint16{5}, ExcludeTypes: true},
			{Version: 1, Text: "*austrailasale.", Pattern: "*austrailasale.", Kind: KindMask},
			{Version: 1, Text: "@@||masto*.top^", Pattern: "||masto*.top^", Kind: KindMask, Exception: true},
		},
		Lines: 8,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed=%+v want=%+v", got, want)
	}
}

// TestWildcardEndingInDot: an ABP pattern ending in "." is not end-anchored
// (the name continues past the dot). The old matcher anchored the last
// segment as a suffix, so these rules could never match a query name.

func TestParseListBodyCountsWhatItSkips(t *testing.T) {
	body := strings.Join([]string{
		"! Title: Mixed list",
		"# comment",
		"",
		"127.0.0.1 localhost",
		"0.0.0.0 Ads.Example.COM",
		"||tracker.example.net^",
		"||cdn.example.org^$third-party",
		"@@||allowed.example.com^",
		"/banner[0-9]+\\.example\\.com/",
		"pixel.example.com.",
		"ad*.example.org",
		"::1 localhost",
		"广告.example.cn",
		"a..example.com",
		strings.Repeat("a", 64) + ".example.com",
		"2001:db8::1",
	}, "\n")
	got, err := Parse(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Domains:    []string{"localhost", "ads.example.com", "tracker.example.net", "pixel.example.com", "xn--4rr70v.example.cn", "2001:db8::1"},
		Exceptions: []string{"allowed.example.com"},
		Rules: []Rule{
			{Version: 1, Text: `/banner[0-9]+\.example\.com/`, Pattern: `banner[0-9]+\.example\.com`, Kind: KindRegex},
			{Version: 1, Text: "ad*.example.org", Pattern: "ad*.example.org", Kind: KindMask},
		},
		Diagnostics: []Diagnostic{
			{Line: 7, Rule: "||cdn.example.org^$third-party", Reason: "unsupported modifier: third-party"},
			{Line: 12, Rule: "::1 localhost", Reason: "invalid hostname rule"},
			{Line: 14, Rule: "a..example.com", Reason: "invalid hostname rule"},
			{Line: 15, Rule: strings.Repeat("a", 64) + ".example.com", Reason: "invalid hostname rule"},
		},
		Lines: 16, Unsupported: 1, Invalid: 3, InvalidAt: []int{12, 14, 15},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed = %+v\nwant     %+v", got, want)
	}
}

func TestParseListBodyLimitsAreExact(t *testing.T) {
	body := "0.0.0.0 ads.example.com\n0.0.0.0 track.example.com\n"
	if got, err := Parse(strings.NewReader(body), int64(len(body)), 2); err != nil || len(got.Domains) != 2 {
		t.Fatalf("body exactly at both limits: %+v, %v", got, err)
	}
	if _, err := Parse(strings.NewReader(body), int64(len(body))-1, 2); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprintf("larger than %d bytes (LIST_MAX_DOWNLOAD_BYTES)", len(body)-1)) {
		t.Fatalf("one byte over: err = %v", err)
	}
	if _, err := Parse(strings.NewReader(body), 1<<20, 1); err == nil ||
		!strings.Contains(err.Error(), "more than 1 lines (LIST_MAX_LINES)") {
		t.Fatalf("one line over: err = %v", err)
	}
}

func TestParseDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic domain with prefix and suffix",
			input:    "||example.com^",
			expected: "example.com",
		},
		{
			name:     "domain with subdomain",
			input:    "||ads.example.com^",
			expected: "ads.example.com",
		},
		{
			name:     "domain with path",
			input:    "||example.com/ads^",
			expected: "example.com",
		},
		{
			name:     "domain with modifiers",
			input:    "||example.com^$third-party",
			expected: "example.com",
		},
		{
			name:     "IP address",
			input:    "||103.224.182.210^",
			expected: "103.224.182.210",
		},
		{
			name:     "IP address variant",
			input:    "||193.200.64.30^",
			expected: "193.200.64.30",
		},
		{
			name:     "domain with single pipe",
			input:    "|http://example.com^",
			expected: "http:",
		},
		{
			name:     "domain with trailing pipe",
			input:    "||example.com^|",
			expected: "example.com",
		},
		{
			name:     "uppercase domain",
			input:    "||EXAMPLE.COM^",
			expected: "example.com",
		},
		{
			name:     "domain with multiple path segments",
			input:    "||example.com/path/to/resource^",
			expected: "example.com",
		},
		{
			name:     "domain with complex modifiers",
			input:    "||example.com^$script,third-party,domain=~example.org",
			expected: "example.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "domain with space (invalid)",
			input:    "||example .com^",
			expected: "",
		},
		{
			name:     "wildcard domain",
			input:    "||*.example.com^",
			expected: "*.example.com",
		},
		{
			name:     "hosts file format with 127.0.0.1",
			input:    "127.0.0.1 example.com",
			expected: "example.com",
		},
		{
			name:     "hosts file format with 0.0.0.0",
			input:    "0.0.0.0 ads.example.com",
			expected: "ads.example.com",
		},
		{
			name:     "hosts file format with comment",
			input:    "127.0.0.1 tracking.example.com # tracking",
			expected: "tracking.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseDomain(tt.input)
			if result != tt.expected {
				t.Errorf("ParseDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseFailuresNeverReturnPartialRules(t *testing.T) {
	body := "ads.example\n@@safe.ads.example\n"
	for _, test := range []struct {
		name   string
		reader io.Reader
		bytes  int64
		lines  int
	}{
		{"byte limit", strings.NewReader(body), int64(len(body) - 1), 100},
		{"line limit", strings.NewReader(body), 1 << 20, 1},
		{"read failure", io.MultiReader(strings.NewReader(body), iotest.ErrReader(errors.New("source disconnected"))), 1 << 20, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.reader, test.bytes, test.lines)
			if err == nil {
				t.Fatal("failed input accepted")
			}
			if !reflect.DeepEqual(got, Result{}) {
				t.Fatalf("partial result: %+v", got)
			}
		})
	}
}

func TestStoredRowsPreserveListLocalExceptions(t *testing.T) {
	list := Result{Domains: []string{"ads.example", "ads.example", "safe.ads.example"}, Exceptions: []string{"safe.ads.example", "safe.ads.example"}}
	for _, test := range []struct {
		block bool
		rows  []string
		count int
	}{
		{true, []string{"ads.example", "safe.ads.example", "@@safe.ads.example"}, 2},
		{false, []string{"ads.example", "safe.ads.example"}, 2},
	} {
		rows, count := StoredRows(list, test.block)
		if !reflect.DeepEqual(rows, test.rows) || count != test.count {
			t.Fatalf("block=%t rows=%v count=%d", test.block, rows, count)
		}
	}
}
