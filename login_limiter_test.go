package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var loginT0 = time.Date(2026, 9, 25, 21, 0, 0, 0, time.UTC)

func TestLoginLimiterUsernameBackoff(t *testing.T) {
	l := newLoginLimiter()
	now := loginT0
	for i := 1; i < loginUserFreeFailures; i++ {
		if err := l.recordFailure("admin", fmt.Sprintf("198.51.100.%d", i), now); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if w := l.retryAfter("admin", "203.0.113.9", now); w != 0 {
			t.Fatalf("locked after %d failures (%v); the first %d are free", i, w, loginUserFreeFailures)
		}
	}
	// Each failure past the allowance doubles the lockout, even from new IPs.
	for i, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second} {
		if err := l.recordFailure("admin", fmt.Sprintf("192.0.2.%d", i), now); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
		if got := l.retryAfter("admin", "203.0.113.9", now); got != want {
			t.Fatalf("failure %d: lockout %v, want %v", loginUserFreeFailures+i, got, want)
		}
		now = now.Add(want) // wait out the lockout, then fail again
	}
	if got := loginBackoff(30); got != loginBackoffMax {
		t.Fatalf("backoff cap = %v, want %v", got, loginBackoffMax)
	}
	if got := loginBackoff(9); got != 512*time.Second {
		t.Fatalf("2^9 s is under the cap; got %v", got)
	}
	if got := loginBackoff(10); got != loginBackoffMax {
		t.Fatalf("2^10 s exceeds the 15 min cap; got %v", got)
	}

	l.recordSuccess("admin")
	if w := l.retryAfter("admin", "203.0.113.9", now); w != 0 {
		t.Fatalf("successful login did not clear the username lockout: %v", w)
	}
}

func TestLoginLimiterClientCounterDecaysAndSurvivesSuccess(t *testing.T) {
	l := newLoginLimiter()
	client := "198.51.100.23"
	for i := 0; i < loginClientFreeFailures; i++ {
		if err := l.recordFailure(fmt.Sprintf("sprayed-%d", i), client, loginT0); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	if got := l.retryAfter("anyone", client, loginT0); got != time.Second {
		t.Fatalf("client lockout after %d sprayed failures = %v, want 1s", loginClientFreeFailures, got)
	}
	l.recordSuccess("sprayed-0")
	if got := l.retryAfter("anyone", client, loginT0); got != time.Second {
		t.Fatalf("a valid login from the same network cleared the spray counter: %v", got)
	}
	later := loginT0.Add(loginFailureMemory + time.Minute)
	if err := l.recordFailure("sprayed-x", client, later); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if got := l.retryAfter("anyone", client, later); got != 0 {
		t.Fatalf("counter did not decay after %v quiet: lockout %v", loginFailureMemory, got)
	}
}

func TestLoginLimiterFailsClosedWhenFull(t *testing.T) {
	l := newLoginLimiter()
	for i := 0; i < loginMaxTrackedKeys; i++ {
		l.users[fmt.Sprintf("u%d", i)] = &loginFailures{count: 1, lastFail: loginT0}
	}
	if err := l.recordFailure("one-more", "203.0.113.1", loginT0); err != errLoginLimiterFull {
		t.Fatalf("full limiter err = %v, want errLoginLimiterFull", err)
	}
	// Once the tracked failures are stale they are pruned and tracking resumes.
	if err := l.recordFailure("one-more", "203.0.113.1", loginT0.Add(loginFailureMemory+time.Second)); err != nil {
		t.Fatalf("stale entries were not pruned: %v", err)
	}
}

func TestLoginClientIdentity(t *testing.T) {
	origTrust := trustProxyHeaders
	t.Cleanup(func() { trustProxyHeaders = origTrust })

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "10.42.1.1:51514"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 198.51.100.7")
	trustProxyHeaders = false
	if got := loginClientIP(req); got != "10.42.1.1" {
		t.Fatalf("untrusted XFF: client = %q, want the TCP peer", got)
	}
	trustProxyHeaders = true
	if got := loginClientIP(req); got != "198.51.100.7" {
		t.Fatalf("trusted XFF: client = %q, want the right-most (proxy-appended) entry", got)
	}
	req.Header.Set("X-Forwarded-For", "not-an-ip")
	if got := loginClientIP(req); got != "10.42.1.1" {
		t.Fatalf("garbage XFF: client = %q, want the TCP peer", got)
	}

	for ip, want := range map[string]string{
		"192.168.1.23":                     "192.168.1.23",
		"2001:db8:85a3:12:8a2e:370:7334:1": "2001:db8:85a3:12::/64",
		"2001:db8:85a3:12:ffff::9":         "2001:db8:85a3:12::/64",
		"::ffff:192.168.1.23":              "::ffff:192.168.1.23",
	} {
		if got := loginClientKey(ip); got != want {
			t.Errorf("loginClientKey(%q) = %q, want %q", ip, got, want)
		}
	}
}

func TestLoginRefusedWhileBcryptSlotsBusy(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := createAdminUser("admin", "testpass-long-enough", RoleAdmin); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < loginBcryptConcurrency; i++ {
		loginBcryptSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < loginBcryptConcurrency; i++ {
			<-loginBcryptSlots
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // do not wait out loginBcryptWait in the test
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"admin","password":"testpass-long-enough"}`)).WithContext(ctx)
	w := httptest.NewRecorder()
	handleAPIAuthLogin(w, req)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("status %d Retry-After %q; want 429 and 1 while bcrypt capacity is exhausted", w.Code, w.Header().Get("Retry-After"))
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a session was issued without verifying the password")
	}
}
