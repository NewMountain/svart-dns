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

func TestLegacyParseListBodyModifiers(t *testing.T) {
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
	got, err := ParseLegacy(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		Domains:          []string{"*.loan", "ads.example.com", "ww4.*"},
		Exceptions:       []string{"ww4.ikea.com", "ww4.autotask.net"},
		Lines:            10,
		Unsupported:      5,
		ModifiersIgnored: 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed = %+v\nwant     %+v", got, want)
	}
}

func TestLegacyListRulePatterns(t *testing.T) {
	for _, tc := range []struct {
		rule      string
		want      []string
		supported bool
	}{
		{"||ads.example.com^", []string{"ads.example.com"}, true},
		{"0.0.0.0 ads.example.com", []string{"ads.example.com"}, true},
		{"pixel.example.com.", []string{"pixel.example.com"}, true},
		{"||*.top^", []string{"*.top"}, true},
		{"||glthub.", []string{"glthub.*", "*.glthub.*"}, true},
		{"||masto*.top^", []string{"masto*.top", "*.masto*.top"}, true},
		{"||adidas-outlet*", []string{"adidas-outlet*", "*.adidas-outlet*"}, true},
		{"|cisco.com|", []string{"cisco.com"}, true},
		{"://ww4.", []string{"ww4.*"}, true},
		{"://blog.*.top^", []string{"blog.*.top"}, true},
		{"://trk.*.run/", []string{"trk.*.run"}, true},
		{"http://secured21.*.com/|", []string{"secured21.*.com"}, true},
		{".booking.com-id*", []string{"*.booking.com-id*"}, true},
		{".com-id*.info^", []string{"*.com-id*.info"}, true},
		{"*austrailasale.", []string{"*austrailasale."}, true},
		{"*norge-salg.com^", []string{"*norge-salg.com"}, true},
		{"://[2600:3c06::f03c:95ff:fed9:ce5e]^", []string{"2600:3c06::f03c:95ff:fed9:ce5e"}, true},
		{"212.27.60.108", []string{"212.27.60.108"}, true},
		{"://46.148.113.", nil, false},
		{"://193.143.1.*", nil, false},
		{"://", nil, true},
		{"||", nil, true},
	} {
		got, supported := legacyRulePatterns(tc.rule)
		if !reflect.DeepEqual(got, tc.want) || supported != tc.supported {
			t.Errorf("legacyRulePatterns(%q) = %q, %v; want %q, %v", tc.rule, got, supported, tc.want, tc.supported)
		}
	}
}

func TestLegacyParseListBodyKeepsProductionRules(t *testing.T) {
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
	got, err := ParseLegacy(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"*.actor", "*.africa", "*.top", "*.bid", "*austrailasale.", "example.org"}
	if !reflect.DeepEqual(got.Domains, want) {
		t.Fatalf("domains = %q\nwant      %q", got.Domains, want)
	}
	// @@ rules and $denyallow values become the list's own exceptions;
	// dnstype=~CNAME is applied without its CNAME exclusion (two rules).
	wantExceptions := []string{"nation.africa", "caitlin.top", "reminder.top", "masto*.top", "*.masto*.top"}
	if !reflect.DeepEqual(got.Exceptions, wantExceptions) {
		t.Fatalf("exceptions = %q\nwant         %q", got.Exceptions, wantExceptions)
	}
	if got.Unsupported != 0 || got.ModifiersIgnored != 2 || got.Invalid != 0 {
		t.Fatalf("unsupported=%d modifiersIgnored=%d invalid=%d, want 0, 2, 0", got.Unsupported, got.ModifiersIgnored, got.Invalid)
	}
	// "*austrailasale." is kept byte-for-byte as earlier versions stored it,
	// and it must block what it was written for (see TestWildcardEndingInDot).
}

func TestLegacyParseListBodyCountsWhatItSkips(t *testing.T) {
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
	got, err := ParseLegacy(strings.NewReader(body), 1<<20, 1000)
	if err != nil {
		t.Fatal(err)
	}
	want := Result{
		// Unicode names are converted to punycode (D18), the form DNS queries use.
		// A $third-party rule only applies inside browsers (TestParseListBodyModifiers).
		Domains:     []string{"localhost", "ads.example.com", "tracker.example.net", "pixel.example.com", "ad*.example.org", "xn--4rr70v.example.cn", "2001:db8::1"},
		Exceptions:  []string{"allowed.example.com"},
		Lines:       16,
		Unsupported: 2,
		Invalid:     3,
		InvalidAt:   []int{12, 14, 15},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed = %+v\nwant     %+v", got, want)
	}
}

func TestLegacyParseListBodyLimitsAreExact(t *testing.T) {
	body := "0.0.0.0 ads.example.com\n0.0.0.0 track.example.com\n"
	if got, err := ParseLegacy(strings.NewReader(body), int64(len(body)), 2); err != nil || len(got.Domains) != 2 {
		t.Fatalf("body exactly at both limits: %+v, %v", got, err)
	}
	if _, err := ParseLegacy(strings.NewReader(body), int64(len(body))-1, 2); err == nil ||
		!strings.Contains(err.Error(), fmt.Sprintf("larger than %d bytes (LIST_MAX_DOWNLOAD_BYTES)", len(body)-1)) {
		t.Fatalf("one byte over: err = %v", err)
	}
	if _, err := ParseLegacy(strings.NewReader(body), 1<<20, 1); err == nil ||
		!strings.Contains(err.Error(), "more than 1 lines (LIST_MAX_LINES)") {
		t.Fatalf("one line over: err = %v", err)
	}
}

func TestLegacyParseFailuresNeverReturnPartialRules(t *testing.T) {
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
			got, err := ParseLegacy(test.reader, test.bytes, test.lines)
			if err == nil {
				t.Fatal("failed input accepted")
			}
			if !reflect.DeepEqual(got, Result{}) {
				t.Fatalf("partial result: %+v", got)
			}
		})
	}
}
