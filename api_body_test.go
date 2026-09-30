package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDecodeJSONBody(t *testing.T) {
	type rewrite struct {
		Domain string `json:"domain"`
		IP     string `json:"ip"`
	}
	tests := []struct {
		name       string
		body       string
		limit      int64
		wantOK     bool
		wantStatus int
		wantError  string
		want       rewrite
	}{
		{name: "single object", body: `{"domain":"nas.home.arpa","ip":"10.42.1.20"}`, limit: 1024, wantOK: true,
			want: rewrite{Domain: "nas.home.arpa", IP: "10.42.1.20"}},
		{name: "trailing whitespace is fine", body: "{\"domain\":\"nas.home.arpa\"}\r\n\t ", limit: 1024, wantOK: true,
			want: rewrite{Domain: "nas.home.arpa"}},
		{name: "second JSON value", body: `{"domain":"a.example"}{"domain":"b.example"}`, limit: 1024,
			wantStatus: http.StatusBadRequest, wantError: "invalid JSON request body: expected exactly one JSON value, found more"},
		{name: "text/plain form smuggling suffix", body: `{"domain":"a.example","x":"="}` + "\r\nz=1", limit: 1024,
			wantStatus: http.StatusBadRequest, wantError: "invalid JSON request body: invalid character 'z' looking for beginning of value"},
		{name: "malformed", body: `{"domain":`, limit: 1024,
			wantStatus: http.StatusBadRequest, wantError: "invalid JSON request body: unexpected EOF"},
		{name: "wrong type", body: `{"domain":42}`, limit: 1024,
			wantStatus: http.StatusBadRequest, wantError: "invalid JSON request body: field \"domain\" must be string"},
		{name: "over the limit", body: `{"domain":"` + strings.Repeat("a", 64) + `"}`, limit: 32,
			wantStatus: http.StatusRequestEntityTooLarge, wantError: "request body exceeds the 32-byte limit for this endpoint; split the request into smaller ones"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/rewrites", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			var got rewrite
			ok := decodeJSONBody(w, req, tt.limit, &got)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v; body %s", ok, tt.wantOK, w.Body.String())
			}
			if tt.wantOK {
				if got != tt.want {
					t.Fatalf("decoded %+v, want %+v", got, tt.want)
				}
				return
			}
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if msg := secErrorOf(w.Body.String()); msg != "error="+tt.wantError {
				t.Fatalf("error = %q, want %q", msg, tt.wantError)
			}
		})
	}
}

func TestIntQueryParam(t *testing.T) {
	tests := []struct {
		query   string
		want    int
		wantErr string
	}{
		{query: "", want: 100},
		{query: "limit=1", want: 1},
		{query: "limit=5000", want: 5000},
		{query: "limit=0", wantErr: "limit must be between 1 and 5000, got 0"},
		{query: "limit=-5", wantErr: "limit must be between 1 and 5000, got -5"},
		{query: "limit=5001", wantErr: "limit must be between 1 and 5000, got 5001"},
		{query: "limit=ten", wantErr: `limit must be an integer, got "ten"`},
		{query: "limit=1e3", wantErr: `limit must be an integer, got "1e3"`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			q, fixtureErr3113 := url.ParseQuery(tt.query)
			if fixtureErr3113 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr3113)
			}
			got, err := intQueryParam(q, "limit", 100, 1, 5000)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	for in, want := range map[string]string{
		"https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt": "https://cdn.jsdelivr.net/gh/hagezi/dns-blocklists@latest/hosts/pro.txt",
		"https://big.oisd.nl/":                             "https://big.oisd.nl/",
		"https://svc:s3cr3t@lists.example.com/pro.txt":     "https://REDACTED@lists.example.com/pro.txt",
		"https://lists.example.com/pro.txt?token=abc123":   "https://lists.example.com/pro.txt?REDACTED",
		"https://acct@dns.example.net/dns-query?key=k3y#x": "https://REDACTED@dns.example.net/dns-query?REDACTED#REDACTED",
		"https://lists.example.com/pro.txt?":               "https://lists.example.com/pro.txt?REDACTED",
		"9.9.9.9:53":                                       "9.9.9.9:53",
		"tls://dns.quad9.net":                              "tls://dns.quad9.net",
		"https://dns.mullvad.net/dns-query":                "https://dns.mullvad.net/dns-query",
		"user:pass@10.42.1.2:53":                           "[redacted: unparseable URL containing credentials or a query]",
		"":                                                 "",
	} {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	if got := urlForViewer("https://svc:s3cr3t@lists.example.com/pro.txt", true); got != "https://svc:s3cr3t@lists.example.com/pro.txt" {
		t.Errorf("admins must see the full URL, got %q", got)
	}
}

func TestLimitRequestBodiesCapsHandlersThatReadRawBodies(t *testing.T) {
	var readErr error
	h := limitRequestBodies(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32*1024)
		for readErr == nil {
			_, readErr = r.Body.Read(buf)
		}
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(strings.Repeat("a", maxRequestBodyBytes+1)))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if readErr == nil || !strings.Contains(readErr.Error(), "request body too large") {
		t.Fatalf("raw read of an oversized body: err = %v, want http.MaxBytesError", readErr)
	}
}

func TestDecodeJSONTypeErrorUsesWireFieldNames(t *testing.T) {
	check := func(t *testing.T, rr *httptest.ResponseRecorder, field string) {
		t.Helper()
		expected := "{\"data\":null,\"error\":\"invalid JSON request body: field \\\"" + field + "\\\" must be string\",\"error_code\":\"invalid_request\"}\n"
		if rr.Code != 400 || rr.Body.String() != expected {
			t.Fatalf("status=%d body=%s want=%s", rr.Code, rr.Body.String(), expected)
		}
	}
	t.Run("named DTO", func(t *testing.T) {
		rr := httptest.NewRecorder()
		var body ClientBlockDomainRequest
		decodeJSONBody(rr, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(`{"domain":false}`)), 1024, &body)
		check(t, rr, "domain")
	})
	t.Run("nested anonymous DTO", func(t *testing.T) {
		rr := httptest.NewRecorder()
		var body struct {
			Rule struct {
				Domain string `json:"domain"`
			} `json:"rule"`
		}
		decodeJSONBody(rr, httptest.NewRequest(http.MethodPost, "/fixture", strings.NewReader(`{"rule":{"domain":[]}}`)), 1024, &body)
		check(t, rr, "rule.domain")
	})
}
