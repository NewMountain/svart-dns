package listparse

import (
	"net/netip"
	"testing"
)

func TestCouldBeIPNeverHidesAnAddress(t *testing.T) {
	for _, s := range []string{
		"198.51.100.23", "2001:DB8:0::BAD", "::ffff:192.0.2.1", "fe80::1%eth0", "::", "0.0.0.0",
		"ads.google.com", "0--foodwarez.da.ru", "1.2.3.4.in-addr.arpa", "192.0.2.1%eth0", "10.0.0.256", "",
	} {
		if _, err := netip.ParseAddr(s); err == nil && !couldBeIP(s) {
			t.Errorf("couldBeIP(%q) = false for a valid address", s)
		}
	}
	for _, s := range []string{"ads.google.com", "0--foodwarez.da.ru", "xn--bcher-kva.example"} {
		if couldBeIP(s) {
			t.Errorf("couldBeIP(%q) = true for a name", s)
		}
	}
}

// Reference values from Python's punycode/idna codecs and RFC 3492 section 7.1.
func TestPunycodeEncode(t *testing.T) {
	for in, want := range map[string]string{
		"bücher":      "bcher-kva",
		"münchen":     "mnchen-3ya",
		"münchen-ads": "mnchen-ads-9db",
		"例え":          "r8jz45g",
		// RFC 3492 7.1 (A) Arabic (Egyptian)
		"ليهمابتكلموشعربي؟": "egbpdaj6bu4bxfgehfvwxn",
		// RFC 3492 7.1 (L) 3<nen>B<gumi><kinpachi><sensei>
		"3年B組金八先生": "3B-ww4c5e180e575a65lsy2b",
	} {
		got, ok := punycodeEncode(in)
		if !ok || got != want {
			t.Errorf("punycodeEncode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := punycodeEncode("bad\xffutf8"); ok {
		t.Errorf("invalid UTF-8 must be rejected")
	}
}

func TestNormalizeListEntry(t *testing.T) {
	for in, want := range map[string]string{
		"tracker.example.":       "tracker.example",
		"  Ads.Example.COM  ":    "ads.example.com",
		"bücher.example":         "xn--bcher-kva.example",
		"BÜCHER.example.":        "xn--bcher-kva.example",
		"*.münchen-ads.example":  "*.xn--mnchen-ads-9db.example",
		"ad*.münchen*.example":   "ad*.münchen*.example", // complex wildcard label left as is
		"2001:DB8:0:0:0:0:0:BAD": "2001:db8::bad",
		"::ffff:198.51.100.7":    "198.51.100.7",
		"193.200.64.30":          "193.200.64.30",
		"xn--bcher-kva.example":  "xn--bcher-kva.example",
		".":                      "",
		"":                       "",
		"doubleclick.net":        "doubleclick.net",
	} {
		if got := NormalizeEntry(in); got != want {
			t.Errorf("NormalizeEntry(%q) = %q, want %q", in, got, want)
		}
	}
}
