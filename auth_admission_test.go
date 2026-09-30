package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/miekg/dns"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAPITokenHTTPBcryptSaturation(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	plaintext, _, err := createAPIToken("monitoring", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiAuth(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, "authenticated") }))
	defer server.Close()
	request := func(key string) (int, string, string) {
		t.Helper()
		req, fixtureErr618 := http.NewRequest(http.MethodGet, server.URL, nil)
		if fixtureErr618 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr618)
		}
		req.Header.Set("X-Api-Key", key)
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { checkTestClose(t, response.Body) }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), key) {
			t.Fatal("credential reflected in response")
		}
		return response.StatusCode, string(body), response.Header.Get("Retry-After")
	}
	if status, _, _ := request(plaintext); status != 200 {
		t.Fatalf("warm valid token = %d; want 200", status)
	}
	for i := 0; i < loginBcryptConcurrency; i++ {
		loginBcryptSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < loginBcryptConcurrency; i++ {
			<-loginBcryptSlots
		}
	}()
	status, body, retry := request(plaintext[:11] + strings.Repeat("0", 56))
	if status != 429 || retry != "1" || body != "{\"data\":null,\"error\":\"API token verification busy; retry later\",\"error_code\":\"rate_limited\"}\n" {
		t.Fatalf("saturated response = %d %q retry=%q; want 429 safe error and retry=1", status, body, retry)
	}
	if status, _, _ := request(plaintext); status != 200 {
		t.Fatalf("cached token during saturation = %d; want 200", status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := db.ExecContext(ctx, "INSERT INTO settings (key,value) VALUES ('token-admission-probe','ready')"); err != nil {
		t.Fatalf("admin DB unavailable: %v", err)
	}
}

func TestAPITokenAdmissionRateWindow(t *testing.T) {
	tokenVerifications = tokenAdmission{}
	t.Cleanup(func() { tokenVerifications = tokenAdmission{} })
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		release, ok := acquireTokenBcryptSlot(now)
		if !ok {
			t.Fatalf("attempt %d rejected; want first four admitted", i+1)
		}
		release()
	}
	if _, ok := acquireTokenBcryptSlot(now.Add(time.Second - time.Nanosecond)); ok {
		t.Fatal("fifth attempt admitted before one second")
	}
	release, ok := acquireTokenBcryptSlot(now.Add(time.Second))
	if !ok {
		t.Fatal("new one-second window rejected")
	}
	release()
}

func TestAPITokenHTTPFloodLeavesDNSAndWriterAvailable(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	var authLogs bytes.Buffer
	oldLogger := logAuth
	logAuth = slog.New(slog.NewJSONHandler(&authLogs, nil))
	t.Cleanup(func() { logAuth = oldLogger })
	key, token, err := createAPIToken("monitoring", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	// A slow but genuine bcrypt hash keeps comparisons active while we probe.
	hash, err := bcrypt.GenerateFromPassword([]byte(key), 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE api_tokens SET token_hash=? WHERE id=?", string(hash), token.ID); err != nil {
		t.Fatal(err)
	}
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})
	udp, _ := secdnsServe(t)
	server := httptest.NewServer(apiAuth(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, "authenticated") }))
	defer server.Close()
	start := make(chan struct{})
	results := make(chan int, 32)
	failures := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func(i int) {
			<-start
			req, fixtureErr3862 := http.NewRequest(http.MethodGet, server.URL, nil)
			if fixtureErr3862 != nil {
				t.Errorf("fixture operation failed: %v", fixtureErr3862)
			}
			req.Header.Set("X-Api-Key", key[:11]+fmt.Sprintf("%056x", i))
			response, err := server.Client().Do(req)
			if err != nil {
				failures <- err
				results <- 0
				return
			}
			defer func() { checkTestClose(t, response.Body) }()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				failures <- err
			}
			expected := "{\"data\":null,\"error\":\"invalid API key\",\"error_code\":\"unauthorized\"}\n"
			if response.StatusCode == 429 {
				expected = "{\"data\":null,\"error\":\"API token verification busy; retry later\",\"error_code\":\"rate_limited\"}\n"
				if response.Header.Get("Retry-After") != "1" {
					failures <- fmt.Errorf("missing retry header")
				}
			}
			if string(body) != expected {
				failures <- fmt.Errorf("unexpected safe response: %d %s", response.StatusCode, body)
			}
			results <- response.StatusCode
		}(i)
	}
	close(start)
	deadline := time.Now().Add(3 * time.Second)
	for len(loginBcryptSlots) < 4 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := len(loginBcryptSlots); got != 4 {
		t.Errorf("active bcrypt comparisons = %d; want 4", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	_, err = db.ExecContext(ctx, "INSERT INTO settings (key,value) VALUES ('flood-probe','ready')")
	cancel()
	if err != nil {
		t.Errorf("writer starved while bcrypt active: %v", err)
	}
	for i := 0; i < 10; i++ {
		q := new(dns.Msg)
		q.SetQuestion("printer.home.arpa.", dns.TypeA)
		begin := time.Now()
		r := secdnsExchange(t, "udp", udp, q)
		if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 1 || requireFixtureType[*dns.A](t, r.Answer[0]).A.String() != "192.168.1.40" {
			t.Errorf("DNS response = %v", r)
		}
		if elapsed := time.Since(begin); elapsed > 200*time.Millisecond {
			t.Errorf("DNS took %v under token flood; limit 200ms", elapsed)
		}
	}
	counts := map[int]int{}
	for i := 0; i < 32; i++ {
		counts[<-results]++
	}
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if counts[401] != 4 || counts[429] != 28 {
		t.Fatalf("flood statuses = %v; want 4 unauthorized, 28 rate limited", counts)
	}
	if strings.Contains(authLogs.String(), key) || strings.Contains(authLogs.String(), string(hash)) {
		t.Fatal("authentication log leaked credential material")
	}
	t.Logf("32 concurrent HTTP requests: 4 bcrypt admissions, 28 HTTP 429; writer and ten UDP DNS probes under 200ms")
}

func TestAPITokenRevocationInvalidatesCachedAndInflightValidation(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	key, token, err := createAPIToken("monitoring", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	got, err := validateAPIKey(context.Background(), key)
	if err != nil || got == nil || got.ID != token.ID {
		t.Fatalf("valid token = %v, %v", got, err)
	}
	if err := revokeAPIToken(token.ID); err != nil {
		t.Fatal(err)
	}
	got, err = validateAPIKey(context.Background(), key)
	if err != nil || got != nil {
		t.Fatalf("revoked cached token = %v, %v; want nil, nil", got, err)
	}
	key, token, err = createAPIToken("new monitoring", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(key), 14)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE api_tokens SET token_hash=? WHERE id=?", string(hash), token.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan *APIToken, 1)
	go func() {
		got, fixtureErr7250 := validateAPIKey(context.Background(), key)
		if fixtureErr7250 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr7250)
		}
		result <- got
	}()
	deadline := time.Now().Add(2 * time.Second)
	for len(loginBcryptSlots) == 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if len(loginBcryptSlots) != 1 {
		t.Fatal("validation never acquired bcrypt admission")
	}
	if err := revokeAPIToken(token.ID); err != nil {
		t.Fatal(err)
	}
	if got := <-result; got != nil {
		t.Fatalf("in-flight revoked token authenticated as %v", got)
	}
	if _, ok := tokenCache.Load(sha256.Sum256([]byte(key))); ok {
		t.Fatal("in-flight validation repopulated revoked cache")
	}
}
