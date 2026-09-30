package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func setListRefreshHTTPClientForTest(t *testing.T, client *http.Client) {
	t.Helper()
	previous := listRefreshHTTPClient
	listRefreshHTTPClient = client
	t.Cleanup(func() {
		listRefreshHTTPClient = previous
	})
}

func TestRefreshBlocklistByIDHonorsHTTPTimeout(t *testing.T) {
	// The list server is a local httptest server; fetching it needs the
	// same explicit opt-in an operator with a LAN list server sets.
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(75 * time.Millisecond)
		if _, err := fmt.Fprintln(w, "0.0.0.0 slow.example.com"); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}))
	defer srv.Close()

	setListRefreshHTTPClientForTest(t, &http.Client{Timeout: 20 * time.Millisecond})

	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", srv.URL, "Slow Blocklist", true)
	if err != nil {
		t.Fatalf("failed to insert blocklist: %v", err)
	}
	listID, fixtureErr1154 := result.LastInsertId()
	if fixtureErr1154 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr1154)
	}

	start := time.Now()
	err = refreshBlocklistByID(int(listID))
	if err == nil {
		t.Fatal("expected refresh to fail on timeout")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected timeout quickly, took %s", elapsed)
	}
	if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM blocked_domains WHERE blocklist_id = ?", listID).Scan(&count); err != nil {
		t.Fatalf("failed to count blocked domains: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected timed-out refresh to leave blocklist empty, found %d domains", count)
	}
}

func TestRefreshAllowlistByIDAcceptsLargeLines(t *testing.T) {
	// The list server is a local httptest server; fetching it needs the
	// same explicit opt-in an operator with a LAN list server sets.
	t.Setenv("ALLOW_PRIVATE_LIST_URLS", "true")
	cleanup := setupTestDB(t)
	defer cleanup()

	largeLine := "0.0.0.0 " + strings.Repeat(" ", 80*1024) + "oversized.example.com"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintln(w, largeLine); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}))
	defer srv.Close()

	result, err := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", srv.URL, "Large Allowlist", true)
	if err != nil {
		t.Fatalf("failed to insert allowlist: %v", err)
	}
	listID, fixtureErr2703 := result.LastInsertId()
	if fixtureErr2703 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr2703)
	}

	if err := refreshAllowlistByID(int(listID)); err != nil {
		t.Fatalf("refresh failed: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM allowed_domains WHERE allowlist_id = ?", listID).Scan(&count); err != nil {
		t.Fatalf("failed to count allowed domains: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 allowed domain after refresh, found %d", count)
	}

	var domain string
	if err := db.QueryRow("SELECT domain FROM allowed_domains WHERE allowlist_id = ?", listID).Scan(&domain); err != nil {
		t.Fatalf("failed to fetch stored domain: %v", err)
	}
	if domain != "oversized.example.com" {
		t.Fatalf("expected oversized.example.com, got %q", domain)
	}
}
