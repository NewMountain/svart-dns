package main

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func serveThrough(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	if logAdmin == nil {
		// Rejection paths log; the component loggers exist only after initLogging.
		initLogging()
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "ok")
})

func TestSecurityHeadersSendHSTSOnlyOverHTTPS(t *testing.T) {
	origTrust := trustProxyHeaders
	t.Cleanup(func() { trustProxyHeaders = origTrust })
	h := securityHeaders(okHandler)

	direct := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	direct.TLS = &tls.ConnectionState{}
	if got := serveThrough(h, direct).Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("direct TLS HSTS = %q, want max-age=31536000 (no includeSubDomains)", got)
	}

	proxied := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")
	trustProxyHeaders = false
	if got := serveThrough(h, proxied).Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("untrusted X-Forwarded-Proto produced HSTS %q", got)
	}
	trustProxyHeaders = true
	if got := serveThrough(h, proxied).Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Fatalf("trusted proxy HTTPS HSTS = %q", got)
	}
}

func TestSecurityHeadersSurviveHandlerErrors(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusUnauthorized, "valid API key or session required")
	}))
	w := serveThrough(h, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if w.Code != http.StatusUnauthorized || w.Header().Get("X-Frame-Options") != "DENY" ||
		w.Header().Get("Content-Security-Policy") != adminContentSecurityPolicy {
		t.Fatalf("error response headers = %v", w.Header())
	}
}

func TestCrossOriginGuard(t *testing.T) {
	h := crossOriginGuard(okHandler)
	tests := []struct {
		name       string
		method     string
		headers    map[string]string
		body       string
		wantStatus int
	}{
		{name: "GET from anywhere passes", method: http.MethodGet,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, wantStatus: 200},
		{name: "SPA same-origin fetch", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://svart.home.arpa:3000", "Content-Type": "application/json"}, wantStatus: 200},
		{name: "JSON with charset parameter", method: http.MethodPut, body: `{}`,
			headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Content-Type": "application/json; charset=utf-8"}, wantStatus: 200},
		{name: "user-typed navigation (Sec-Fetch-Site none)", method: http.MethodPost,
			headers: map[string]string{"Sec-Fetch-Site": "none"}, wantStatus: 200},
		{name: "same-site sibling app", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://grafana.home.arpa", "Content-Type": "application/json"}, wantStatus: 403},
		{name: "cross-site DELETE without body", method: http.MethodDelete,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, wantStatus: 403},
		{name: "old browser, matching Origin", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"Origin": "http://svart.home.arpa:3000", "Content-Type": "application/json"}, wantStatus: 200},
		{name: "old browser, foreign Origin", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"Origin": "http://printer.lan", "Content-Type": "application/json"}, wantStatus: 403},
		{name: "sandboxed iframe (Origin null)", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"Origin": "null", "Content-Type": "application/json"}, wantStatus: 403},
		{name: "text/plain body", method: http.MethodPost, body: `{"role":"admin"}`,
			headers: map[string]string{"Content-Type": "text/plain"}, wantStatus: 415},
		{name: "urlencoded body", method: http.MethodPost, body: `a=b`,
			headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, wantStatus: 415},
		{name: "missing Content-Type with body", method: http.MethodPost, body: `{}`, wantStatus: 415},
		{name: "API key script with form Content-Type", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"X-Api-Key": "sv_0123456789abcdef", "Content-Type": "application/x-www-form-urlencoded"}, wantStatus: 200},
		{name: "API key even with cross-site metadata", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"X-Api-Key": "sv_0123456789abcdef", "Sec-Fetch-Site": "cross-site"}, wantStatus: 200},
		{name: "peer sync request", method: http.MethodPost, body: `{}`,
			headers: map[string]string{"X-Sync-Key": "peer-secret"}, wantStatus: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://svart.home.arpa:3000/api/users", strings.NewReader(tt.body))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if w := serveThrough(h, req); w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestParseAdminAllowedHosts(t *testing.T) {
	got, err := parseAdminAllowedHosts(" svart.home.arpa , 10.42.1.6:3000,[fd00::6]:443, Svart-b.Home.Arpa. ")
	if err != nil {
		t.Fatal(err)
	}
	want := hostAllowlist{
		{name: "svart.home.arpa"}, {name: "10.42.1.6", port: "3000"}, {name: "fd00::6", port: "443"}, {name: "svart-b.home.arpa"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed %+v, want %+v", got, want)
	}
	if l, err := parseAdminAllowedHosts(""); l != nil || err != nil {
		t.Fatalf("empty = %v, %v; want nil allowlist (allow all)", l, err)
	}
	for raw, wantErr := range map[string]string{
		"https://svart.home.arpa": `ADMIN_ALLOWED_HOSTS entry "https://svart.home.arpa" is not a host[:port]; list bare names like svart.home.arpa or 192.168.1.2:3000`,
		" , ":                     "ADMIN_ALLOWED_HOSTS is set but lists no hosts; unset it to allow every Host",
		":3000":                   `ADMIN_ALLOWED_HOSTS entry ":3000" has no host name`,
	} {
		if _, err := parseAdminAllowedHosts(raw); err == nil || err.Error() != wantErr {
			t.Errorf("parse(%q) err = %v, want %q", raw, err, wantErr)
		}
	}

	for host, want := range map[string]bool{
		"svart.home.arpa":         true,
		"SVART.HOME.ARPA:8443":    true, // port-less entry matches any port
		"svart.home.arpa.":        true,
		"10.42.1.6:3000":          true,
		"10.42.1.6:3001":          false,
		"10.42.1.6":               false,
		"[fd00::6]:443":           true,
		"rebind.attacker.example": false,
		"":                        false,
	} {
		if got.allows(host) != want {
			t.Errorf("allows(%q) = %v, want %v", host, !want, want)
		}
	}
}

// The docs page is served under the admin CSP; it must not try to reach any
// third-party host (RapiDoc fetches Google Fonts unless told not to).
func TestDocsPageRequestsNoThirdPartyResources(t *testing.T) {
	if !strings.Contains(docsHTML, `load-fonts="false"`) {
		t.Fatal(`web/static/docs.html must set load-fonts="false" on <rapi-doc>`)
	}
	if m := regexp.MustCompile(`(?i)(src|href|spec-url)="(https?:)?//`).FindString(docsHTML); m != "" {
		t.Fatalf("docs page references an external resource: %s", m)
	}
}
