package svart

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

func TestPrivateDestinationReason(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.53.0.1", "::1", "::ffff:127.0.0.1",
		"10.42.1.6", "172.16.0.1", "172.31.255.255", "192.168.1.1", "fd00::6", "fc00::1",
		"169.254.169.254", "fe80::1", "100.64.0.1", "100.100.100.200",
		"0.0.0.0", "0.1.2.3", "::", "224.0.0.251", "239.255.255.250", "ff02::1",
		"255.255.255.255", "240.0.0.1", "192.0.0.170", "198.18.0.1",
		"192.0.2.10", "198.51.100.7", "203.0.113.5", "2001:db8::1", "100::1", "2001:0:4136:e378::1",
		"64:ff9b::a9fe:a9fe", // NAT64 of 169.254.169.254
		"2002:7f00:1::1",     // 6to4 of 127.0.0.1
		"2002:c0a8:101::1",   // 6to4 of 192.168.1.1
	}
	for _, s := range blocked {
		if reason := privateDestinationReason(netip.MustParseAddr(s)); reason == "" {
			t.Errorf("%s is allowed; want it refused", s)
		}
	}
	public := []string{"1.1.1.1", "9.9.9.9", "104.16.132.229", "2606:4700:4700::1111", "2a07:a8c0::", "64:ff9b::101:101"}
	for _, s := range public {
		if reason := privateDestinationReason(netip.MustParseAddr(s)); reason != "" {
			t.Errorf("%s refused as %s; want allowed", s, reason)
		}
	}
	if got := privateDestinationReason(netip.MustParseAddr("64:ff9b::a9fe:a9fe")); got != "a link-local address (includes cloud metadata at 169.254.169.254) embedded in an IPv6 translation address" {
		t.Errorf("NAT64 reason = %q", got)
	}
}

func TestGuardListDestinationChecksResolvedAddress(t *testing.T) {
	if err := guardListDestination("tcp4", "104.16.132.229:443", nil); err != nil {
		t.Fatalf("public address refused: %v", err)
	}
	err := guardListDestination("tcp4", "169.254.169.254:80", nil)
	want := "list URL resolves to 169.254.169.254 is a link-local address (includes cloud metadata at 169.254.169.254); " +
		"refusing to fetch lists from private or special destinations (set ALLOW_PRIVATE_LIST_URLS=true to allow list servers on your own network)"
	if err == nil || err.Error() != want {
		t.Fatalf("metadata dial err = %v, want %q", err, want)
	}

	// A hostname is judged by what it resolves to at connect time, not by
	// its spelling: "localhost" here stands in for any rebinding name.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, port, fixtureErr2370 := net.SplitHostPort(srv.Listener.Addr().String())
	if fixtureErr2370 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr2370)
	}
	deny := context.WithValue(context.Background(), listFetchPolicyKey{}, listFetchPolicy{})
	if conn, err := guardedListDialContext(deny, "tcp", "localhost:"+port); err == nil {
		checkTestClose(t, conn)
		t.Fatal("dial to a name resolving to loopback succeeded without ALLOW_PRIVATE_LIST_URLS")
	} else if !strings.Contains(err.Error(), "a loopback address") {
		t.Fatalf("dial err = %v, want the loopback refusal", err)
	}
	allow := context.WithValue(context.Background(), listFetchPolicyKey{}, listFetchPolicy{allowPrivate: true})
	conn, err := guardedListDialContext(allow, "tcp", "localhost:"+port)
	if err != nil {
		t.Fatalf("dial with ALLOW_PRIVATE_LIST_URLS=true: %v", err)
	}
	checkTestClose(t, conn)
}

func TestValidateListURL(t *testing.T) {
	ok := []string{
		"https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt",
		"http://sbc.io/hosts/hosts",
		"HTTPS://big.oisd.nl/",
		"https://svc:token@lists.example.com/private.txt",
		"https://[2606:4700:4700::1111]/list.txt",
	}
	for _, u := range ok {
		if err := validateListURL(u, false); err != nil {
			t.Errorf("validateListURL(%q) = %v, want ok", u, err)
		}
	}
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	bad := map[string]string{
		"file:///etc/passwd":             `list URL must use http or https, got scheme "file"`,
		"ftp://ftp.example.com/list":     `list URL must use http or https, got scheme "ftp"`,
		"lists.example.com/hosts":        `list URL must use http or https, got scheme ""`,
		"https:///no-host":               "list URL has no host",
		"http://localhost:8080/list.txt": `list URL host "localhost" is a loopback name; refusing to fetch lists from private destinations (set ALLOW_PRIVATE_LIST_URLS=true to allow list servers on your own network)`,
		"http://10.42.1.20/hosts":        "list URL host 10.42.1.20 is a private-network address (RFC 1918 / IPv6 ULA); refusing to fetch lists from private or special destinations (set ALLOW_PRIVATE_LIST_URLS=true to allow list servers on your own network)",
	}
	for u, want := range bad {
		if err := validateListURL(u, false); err == nil || err.Error() != want {
			t.Errorf("validateListURL(%q) = %v, want %q", u, err, want)
		}
	}
	if err := validateListURL("http://10.42.1.20/hosts", true); err != nil {
		t.Errorf("private host with opt-in: %v", err)
	}
	if err := validateListURL("file:///etc/passwd", true); err == nil {
		t.Error("opt-in must not allow non-http schemes")
	}
}

func TestListRefreshRedirects(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	hops := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/to-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/loop":
			hops++
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/moved":
			http.Redirect(w, r, "/hosts", http.StatusMovedPermanently)
		case "/hosts":
			if _, err := fmt.Fprintln(w, "0.0.0.0 ads.example.com"); err != nil {
				t.Errorf("fixture operation failed: %v", err)
			}
		}
	}))
	defer srv.Close()

	for path, want := range map[string]string{
		"/to-file": `list URL redirected to scheme "file"; only http and https are fetched`,
		"/loop":    "list URL redirected more than 5 times",
	} {
		_, err := fetchList(context.Background(), srv.URL+path)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("fetch %s: err = %v, want %q", path, err, want)
		}
	}
	if hops != listRefreshMaxRedirects {
		t.Errorf("followed %d redirects, want %d", hops, listRefreshMaxRedirects)
	}
	got, err := fetchList(context.Background(), srv.URL+"/moved")
	if err != nil || !reflect.DeepEqual(got.Domains, []string{"ads.example.com"}) {
		t.Fatalf("ordinary redirect: %+v, %v", got, err)
	}
}

func TestListFetchPolicyFromEnv(t *testing.T) {
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "")
	t.Setenv("LIST_MAX_DOWNLOAD_BYTES", "")
	t.Setenv("LIST_MAX_LINES", "")
	if p, err := listFetchPolicyFromEnv(); err != nil || p != (listFetchPolicy{maxBytes: 256 << 20, maxLines: 5_000_000}) {
		t.Fatalf("defaults = %+v, %v", p, err)
	}
	for env, val := range map[string]string{
		"ALLOW_PRIVATE_LIST_URLS": "yes please",
		"LIST_MAX_DOWNLOAD_BYTES": "64MB",
		"LIST_MAX_LINES":          "-1",
	} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, val)
			if _, err := listFetchPolicyFromEnv(); err == nil || !strings.Contains(err.Error(), env) {
				t.Fatalf("%s=%q: err = %v, want an error naming the variable", env, val, err)
			}
		})
	}
}

// TestParseListBodyKeepsProductionRules pins the rules real published lists
// rely on (Hagezi spam-tlds, Dandelion Sprout's AdGuard Home list). The first
// version of the stricter parser dropped every rule with a $modifier (165
// whole-TLD blocks, which versions before it skipped too) and rewrote wildcard
// patterns ending in a dot.
func TestWildcardEndingInDot(t *testing.T) {
	cases := []struct {
		pattern, domain string
		want            bool
	}{
		{"*austrailasale.", "austrailasale.com", true},
		{"*austrailasale.", "shop.austrailasale.co.uk", true},
		{"*austrailasale.", "austrailasale", false},
		{"*austrailasale.", "notaustrailasale-shop.com", false},
		{"*factoryoutletuk.", "www.factoryoutletuk.store", true},
		// Patterns not ending in "." keep their end anchor.
		{"ad*.example.org", "ad1.example.org", true},
		{"ad*.example.org", "ad1.example.org.evil.test", false},
		{"telemetry.*.example.com", "telemetry.eu.example.com", true},
	}
	for _, c := range cases {
		r := policycore.NewComplexRule(c.pattern, 0)
		if got := r.Matches(c.domain); got != c.want {
			t.Errorf("index rule %q matches %q = %v, want %v", c.pattern, c.domain, got, c.want)
		}
	}
}

// TestListRulePatterns pins the AdGuard syntax translation on lines taken
// from Dandelion Sprout's Anti-Malware list and Hagezi's lists.
