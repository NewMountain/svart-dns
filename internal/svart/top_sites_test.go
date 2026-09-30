package svart

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDomainsTopSites(t *testing.T) {
	cases := []struct {
		name, csv, limit string
		status           int
		domains          []string
		problem          string
	}{
		{"ranked", "1,google.com\n2,wikipedia.org\n3,github.com\n", "2", 200, []string{"google.com", "wikipedia.org"}, ""},
		{"short source", "1,google.com\n3,wikipedia.org\n", "100", 200, []string{"google.com", "wikipedia.org"}, ""},
		{"normalization", "1,GOOGLE.COM.\r\n2,\"wikipedia.org\"\r\n", "2", 200, []string{"google.com", "wikipedia.org"}, ""},
		{"absent", "", "2", 503, nil, "TOP_SITES_PATH is unset"},
		{"empty", "\n", "2", 503, nil, "contains no domains"},
		{"header", "rank,domain\n1,google.com\n", "2", 503, nil, "row 1: rank must be a positive integer"},
		{"malformed tail", "1,google.com\n2,wikipedia.org\nmalformed\n", "1", 503, nil, "row 3: expected rank,domain"},
		{"bad CSV", "1,google.com\n2,\"wikipedia.org\n", "1", 503, nil, "row 2: invalid CSV"},
		{"rank order", "2,google.com\n1,wikipedia.org\n", "1", 503, nil, "row 2: ranks must be strictly increasing"},
		{"zero rank", "0,google.com\n", "1", 503, nil, "row 1: rank must be a positive integer"},
		{"bad domain", "1,https://google.com\n", "1", 503, nil, "row 1: invalid domain"},
		{"empty domain", "1,\n", "1", 503, nil, "row 1: invalid domain"},
		{"IP", "1,192.0.2.1\n", "1", 503, nil, "row 1: invalid domain"},
		{"bad label", "1,-google.com\n", "1", 503, nil, "row 1: invalid domain"},
		{"long label", "1," + strings.Repeat("a", 64) + ".com\n", "1", 503, nil, "row 1: invalid domain"},
		{"bad limit", "1,google.com\n", "10001", 400, nil, "limit"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			path := ""
			if tt.csv != "" {
				path = filepath.Join(t.TempDir(), "ranking.csv")
				if err := os.WriteFile(path, []byte(tt.csv), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("TOP_SITES_PATH", path)
			w := httptest.NewRecorder()
			handleAPIAnalysisDomains(w, httptest.NewRequest(http.MethodGet, "/api/analysis/domains?source=top-sites&limit="+tt.limit, nil))
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tt.status, w.Body.String())
			}
			var response struct {
				Data  *domainsResponse `json:"data"`
				Error *string          `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if tt.status == 200 {
				want := domainsResponse{Source: "top-sites", Count: len(tt.domains), Domains: tt.domains}
				if response.Error != nil || response.Data == nil || !reflect.DeepEqual(*response.Data, want) {
					t.Fatalf("response %s, want %+v", w.Body.String(), want)
				}
			} else if response.Data != nil || response.Error == nil || !strings.Contains(*response.Error, tt.problem) {
				t.Fatalf("response %s, want unavailable error containing %q and no data", w.Body.String(), tt.problem)
			}
		})
	}
}

func TestDomainsTopSitesUnavailableFile(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.csv"), t.TempDir()} {
		t.Setenv("TOP_SITES_PATH", path)
		w := httptest.NewRecorder()
		handleAPIAnalysisDomains(w, httptest.NewRequest(http.MethodGet, "/api/analysis/domains?source=top-sites", nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "TOP_SITES_PATH") || strings.Contains(w.Body.String(), path) {
			t.Fatalf("want actionable 503 without filesystem path, got %d: %s", w.Code, w.Body.String())
		}
	}
}

func TestDomainsTopSitesReadsUpdatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.csv")
	t.Setenv("TOP_SITES_PATH", path)
	for _, domain := range []string{"google.com", "wikipedia.org"} {
		if err := os.WriteFile(path, []byte("1,"+domain+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handleAPIAnalysisDomains(w, httptest.NewRequest(http.MethodGet, "/api/analysis/domains?source=top-sites", nil))
		want := fmt.Sprintf("\"domains\":[%q]", domain)
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("want current source %s, got %d: %s", want, w.Code, w.Body.String())
		}
	}
}

func TestTopSitesRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.csv")
	// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("\n"), maxTopSitesBytes); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	domains, err := getTopSites(path, 1)
	if err == nil || domains != nil || !strings.Contains(err.Error(), "exceeds the 67108864-byte source budget") {
		t.Fatalf("got %v, %v", domains, err)
	}
}

func TestTopSitesValidatesBeyondTenThousandRows(t *testing.T) {
	var source strings.Builder
	for i := 1; i <= 10001; i++ {
		if _, err := fmt.Fprintf(&source, "%d,site-%d.example\n", i, i); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	domains, err := parseTopSites(strings.NewReader(source.String()), 1)
	if err != nil || !reflect.DeepEqual(domains, []string{"site-1.example"}) {
		t.Fatalf("got %v, %v", domains, err)
	}
	if _, err := source.WriteString("10002,https://invalid.example\n"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	domains, err = parseTopSites(strings.NewReader(source.String()), 1)
	if domains != nil || err == nil || !strings.Contains(err.Error(), "row 10002: invalid domain") {
		t.Fatalf("got %v, %v", domains, err)
	}
}

func TestTopSitesRejectsOversizedRecord(t *testing.T) {
	domains, err := parseTopSites(strings.NewReader("1,google.com\n2,"+strings.Repeat("a", 1024)), 1)
	if domains != nil || err == nil || err.Error() != "cannot read complete CSV at row 2; check storage and keep each record below 1024 bytes" {
		t.Fatalf("got %v, %v", domains, err)
	}
}

type unreadableTopSites struct{}

func (unreadableTopSites) Read([]byte) (int, error) { return 0, fmt.Errorf("test storage failure") }

func TestTopSitesRejectsReadError(t *testing.T) {
	domains, err := parseTopSites(unreadableTopSites{}, 1)
	if domains != nil || err == nil || err.Error() != "cannot read complete CSV at row 1; check storage and keep each record below 1024 bytes" {
		t.Fatalf("got %v, %v", domains, err)
	}
}
