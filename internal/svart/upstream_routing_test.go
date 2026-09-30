package svart

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

// TestConditionalForwardingRoutesByDomain is the D16 regression test. The
// "[/domain/]upstream" syntax (dnsmasq, AdGuard Home) was parsed but never
// routed: a router entry for .lan was just one more general upstream, so
// public queries went to the router and, worse, .lan and home.arpa names
// leaked to public resolvers.
func TestConditionalForwardingRoutesByDomain(t *testing.T) {
	defer setupTestDB(t)()
	router := startUpstreamStub(t, "udp", answerA("192.168.1.10"))
	iot := startUpstreamStub(t, "udp", answerA("10.20.0.5"))
	public := startUpstreamStub(t, "udp", answerA("203.0.113.7"))

	routerEntry := "[/lan/home.arpa/]" + router.addr
	iotEntry := "[/iot.lan/]" + iot.addr
	useUpstreams(t, routerEntry, iotEntry, public.addr)

	tests := []struct {
		qname        string
		wantUpstream string
		wantIP       string
	}{
		{"nas.lan.", routerEntry, "192.168.1.10"},
		{"Printer.LAN.", routerEntry, "192.168.1.10"},
		{"lan.", routerEntry, "192.168.1.10"},
		{"printer.home.arpa.", routerEntry, "192.168.1.10"},
		{"cam.iot.lan.", iotEntry, "10.20.0.5"},
		{"www.example.com.", public.addr, "203.0.113.7"},
		{"planet.", public.addr, "203.0.113.7"},
		{"lan.example.com.", public.addr, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.qname, func(t *testing.T) {
			resp, used, err := resolveQuery(context.Background(), clientQuery(tt.qname, dns.TypeA, 0x0707, false, false))
			if err != nil {
				t.Fatalf("resolveQuery: %v", err)
			}
			if used != tt.wantUpstream {
				t.Errorf("%s answered by %q, want %q", tt.qname, used, tt.wantUpstream)
			}
			if len(resp.Answer) != 1 || requireFixtureType[*dns.A](t, resp.Answer[0]).A.String() != tt.wantIP {
				t.Errorf("%s answer = %v, want A %s", tt.qname, resp.Answer, tt.wantIP)
			}
		})
	}

	for _, q := range public.queries() {
		name := strings.ToLower(q.Question[0].Name)
		if strings.HasSuffix(name, ".lan.") || name == "lan." || strings.HasSuffix(name, ".home.arpa.") {
			t.Errorf("public upstream received local name %s", q.Question[0].Name)
		}
	}
	for _, q := range append(router.queries(), iot.queries()...) {
		if name := strings.ToLower(q.Question[0].Name); name == "www.example.com." || name == "planet." || name == "lan.example.com." {
			t.Errorf("local upstream received public name %s", q.Question[0].Name)
		}
	}
}

// TestConditionalForwardingWithoutDefaultUpstream: a domain-restricted
// upstream must never answer outside its domains, even if it is the only
// upstream configured.
func TestConditionalForwardingWithoutDefaultUpstream(t *testing.T) {
	defer setupTestDB(t)()
	router := startUpstreamStub(t, "udp", answerA("192.168.1.10"))
	useUpstreams(t, "[/lan/]"+router.addr)

	if resp, used, err := resolveQuery(context.Background(), clientQuery("www.example.com.", dns.TypeA, 1, false, false)); err == nil {
		t.Fatalf("www.example.com was answered by %q (%v) although only .lan is routed", used, resp.Answer)
	}
	if got := len(router.queries()); got != 0 {
		t.Errorf("router received %d queries for a public name, want 0", got)
	}
	if _, used, err := resolveQuery(context.Background(), clientQuery("nas.lan.", dns.TypeA, 2, false, false)); err != nil || used != "[/lan/]"+router.addr {
		t.Errorf("nas.lan: used=%q err=%v, want the router", used, err)
	}
}

func TestParseUpstreamSpec(t *testing.T) {
	tests := []struct {
		raw  string
		want upstreamSpec
	}{
		{"9.9.9.9", upstreamSpec{protocol: "udp", address: "9.9.9.9:53"}},
		{"  tls://dns.quad9.net  ", upstreamSpec{protocol: "tls", address: "dns.quad9.net"}},
		{"https://doh.mullvad.net/dns-query", upstreamSpec{protocol: "https", address: "doh.mullvad.net/dns-query"}},
		{"[/lan/]192.168.1.1", upstreamSpec{domains: []string{"lan."}, protocol: "udp", address: "192.168.1.1:53"}},
		{"[/LAN./Home.Arpa/168.192.in-addr.arpa/]tcp://192.168.1.1:5353", upstreamSpec{
			domains:  []string{"lan.", "home.arpa.", "168.192.in-addr.arpa."},
			protocol: "tcp",
			address:  "192.168.1.1:5353",
		}},
		{"[/corp.example.com/]tls://dns.corp.example.com", upstreamSpec{domains: []string{"corp.example.com."}, protocol: "tls", address: "dns.corp.example.com"}},
	}
	for _, tt := range tests {
		got, err := parseUpstreamSpec(tt.raw)
		if err != nil {
			t.Errorf("parseUpstreamSpec(%q): %v", tt.raw, err)
			continue
		}
		if !slices.Equal(got.domains, tt.want.domains) || got.protocol != tt.want.protocol || got.address != tt.want.address {
			t.Errorf("parseUpstreamSpec(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}

	errs := map[string]string{
		"[/lan/":              "unterminated [/domain/] prefix",
		"[/lan/]":             "no server address",
		"[//]192.168.1.1":     `invalid domain ""`,
		"ftp://9.9.9.9":       `unsupported protocol "ftp"`,
		"9.9.9.9:99999":       `invalid port "99999"`,
		"tls://":              `invalid host ""`,
		"udp://a..example:53": `invalid host "a..example"`,
	}
	for raw, want := range errs {
		_, err := parseUpstreamSpec(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("parseUpstreamSpec(%q) error = %v, want it to contain %q", raw, err, want)
		}
	}
}

func TestUpstreamSnapshotForQuery(t *testing.T) {
	router := Upstream{ID: 1, Upstream: "[/lan/home.arpa/]192.168.1.1", Enabled: true}
	iot := Upstream{ID: 2, Upstream: "[/iot.lan/]10.20.0.1", Enabled: true}
	quad9 := Upstream{ID: 3, Upstream: "tls://dns.quad9.net", Enabled: true}
	mullvad := Upstream{ID: 4, Upstream: "https://doh.mullvad.net/dns-query", Enabled: true}
	disabled := Upstream{ID: 5, Upstream: "[/corp.example.com/]10.0.0.53", Enabled: false}
	broken := Upstream{ID: 6, Upstream: "ftp://9.9.9.9", Enabled: true}

	store := &upstreamStore{}
	store.snap.Store(buildUpstreamSnapshot([]Upstream{router, iot, quad9, mullvad, disabled, broken}))

	defaults := []Upstream{quad9, mullvad}
	tests := []struct {
		name string
		want []Upstream
	}{
		{"nas.lan.", []Upstream{router}},
		{"NAS.Lan.", []Upstream{router}},
		{"lan.", []Upstream{router}},
		{"cam.iot.lan.", []Upstream{iot}},
		{"iot.lan.", []Upstream{iot}},
		{"x.home.arpa.", []Upstream{router}},
		{"www.example.com.", defaults},
		{"www.corp.example.com.", defaults}, // disabled route falls back
		{"planet.", defaults},
		{".", defaults},
	}
	for _, tt := range tests {
		if got := store.forQuery(tt.name); !slices.Equal(got, tt.want) {
			t.Errorf("forQuery(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
	if got := store.getAll(); len(got) != 6 {
		t.Errorf("getAll returned %d entries, want all 6 as configured", len(got))
	}
}

// TestUpstreamAPIValidatesSpec: upstream strings are parsed at the API
// boundary; a malformed entry used to be stored and then fail (or be
// misrouted) on every query.
func TestUpstreamAPIValidatesSpec(t *testing.T) {
	defer setupTestDB(t)()

	valid := []string{
		"9.9.9.9",
		"9.9.9.9:53",
		"udp://1.1.1.1",
		"tcp://1.1.1.1:53",
		"tls://dns.quad9.net",
		"tls://dns.quad9.net:853",
		"https://dns.mullvad.net/dns-query",
		"https://cloudflare-dns.com",
		"2620:fe::fe",
		"[2620:fe::fe]:53",
		"[/lan/]192.168.1.1",
		"[/lan/home.arpa/168.192.in-addr.arpa/]tcp://192.168.1.1",
	}
	invalid := []string{
		"",
		"   ",
		"# a comment",
		"[/lan/",
		"[/lan/]",
		"[//]192.168.1.1",
		"[/bad domain/]192.168.1.1",
		"ftp://9.9.9.9",
		"tls://",
		"https://",
		"https:///dns-query",
		"9.9.9.9:99999",
		"9.9.9.9:dns",
		"udp://resolver..example:53",
	}

	post := func(upstream string) int {
		body := fmt.Sprintf(`{"upstream":%q,"enabled":true}`, upstream)
		w := httptest.NewRecorder()
		handleAPIUpstreams(w, httptest.NewRequest(http.MethodPost, "/api/upstreams", bytes.NewBufferString(body)))
		return w.Code
	}
	for _, u := range valid {
		if code := post(u); code != http.StatusCreated {
			t.Errorf("POST upstream %q = %d, want 201", u, code)
		}
	}
	for _, u := range invalid {
		if code := post(u); code != http.StatusBadRequest {
			t.Errorf("POST upstream %q = %d, want 400", u, code)
		}
	}

	id := upstreamStorage.getAll()[0].ID
	for _, u := range invalid {
		body := fmt.Sprintf(`{"upstream":%q}`, u)
		w := httptest.NewRecorder()
		handleAPIUpstreamAction(w, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/upstreams/%d", id), bytes.NewBufferString(body)))
		if w.Code != http.StatusBadRequest {
			t.Errorf("PUT upstream %q = %d, want 400", u, w.Code)
		}
	}
	if got := len(upstreamStorage.getAll()); got != len(valid) {
		t.Errorf("%d upstreams stored, want %d (only the valid ones)", got, len(valid))
	}
}

func TestBuiltinUpstreamOnlyWhenNothingIsConfigured(t *testing.T) {
	fresh := buildUpstreamSnapshot(nil)
	if !fresh.builtin || len(fresh.defaults) != 1 || fresh.defaults[0].Upstream != "https://dns.quad9.net/dns-query" {
		t.Fatalf("fresh install snapshot = %+v, want the built-in Quad9 DoH default", fresh)
	}
	allDisabled := buildUpstreamSnapshot([]Upstream{{ID: 1, Upstream: "https://dns.mullvad.net/dns-query", Enabled: false}})
	if allDisabled.builtin || len(allDisabled.defaults) != 0 {
		t.Fatalf("disabling every configured upstream is a choice; got %+v, want no upstreams", allDisabled)
	}
	configured := buildUpstreamSnapshot([]Upstream{{ID: 2, Upstream: "https://cloudflare-dns.com/dns-query", Enabled: true}})
	if configured.builtin || len(configured.defaults) != 1 || configured.defaults[0].ID != 2 {
		t.Fatalf("configured snapshot = %+v, want only the configured upstream", configured)
	}
}
