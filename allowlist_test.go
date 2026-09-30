package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsAllowedForClient(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create allowlist with various domain types
	result, fixtureErr16 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Test Allowlist", true)
	if fixtureErr16 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr16)
	}
	listID, fixtureErr18 := result.LastInsertId()
	if fixtureErr18 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr18)
	}

	domains := []string{
		"accounts.google.com",
		"login.microsoftonline.com",
		"*.github.com",
	}
	for _, d := range domains {
		if _, fixtureErr26 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, d); fixtureErr26 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr26)
		}
	}

	// Assign to client and seed query log
	if _, fixtureErr30 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr30 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr30)
	}
	if _, fixtureErr32 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr32 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr32)
	}

	mustReloadPolicy(t)

	tests := []struct {
		name     string
		clientIP string
		domain   string
		allowed  bool
	}{
		{
			name:     "exact match allowed",
			clientIP: "10.42.1.42",
			domain:   "accounts.google.com.",
			allowed:  true,
		},
		{
			name:     "subdomain of allowed domain",
			clientIP: "10.42.1.42",
			domain:   "sub.accounts.google.com.",
			allowed:  true,
		},
		{
			name:     "wildcard match allowed",
			clientIP: "10.42.1.42",
			domain:   "api.github.com.",
			allowed:  true,
		},
		{
			name:     "not in allowlist",
			clientIP: "10.42.1.42",
			domain:   "facebook.com.",
			allowed:  false,
		},
		{
			name:     "unknown client not allowed",
			clientIP: "192.168.1.1",
			domain:   "accounts.google.com.",
			allowed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAllowedForClient(tt.clientIP, tt.domain)
			if result != tt.allowed {
				t.Errorf("isAllowedForClient(%q, %q) = %v, want %v", tt.clientIP, tt.domain, result, tt.allowed)
			}
		})
	}
}

func TestIsAllowedForConfiguredClientWithoutQueryLogs(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr88 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Configured Allowlist", true)
	if fixtureErr88 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr88)
	}
	listID, fixtureErr90 := result.LastInsertId()
	if fixtureErr90 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr90)
	}

	if _, fixtureErr92 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, "safe.example.com"); fixtureErr92 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr92)
	}
	if _, fixtureErr93 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.52", listID); fixtureErr93 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr93)
	}

	mustReloadPolicy(t)

	if !isAllowedForClient("10.42.1.52", "safe.example.com.") {
		t.Fatal("expected configured client to allow domains before first traffic")
	}
}

func TestAllowlistDisabled(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create a disabled allowlist
	result, fixtureErr107 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Disabled Allowlist", false)
	if fixtureErr107 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr107)
	}
	listID, fixtureErr109 := result.LastInsertId()
	if fixtureErr109 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr109)
	}

	if _, fixtureErr111 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, "should-not-allow.com"); fixtureErr111 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr111)
	}

	if _, fixtureErr113 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr113 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr113)
	}
	if _, fixtureErr115 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr115 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr115)
	}

	mustReloadPolicy(t)

	if isAllowedForClient("10.42.1.42", "should-not-allow.com.") {
		t.Error("disabled allowlist should not allow domains")
	}
}

func TestManualAllowlistCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Seed a client in query_logs
	if _, fixtureErr129 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr129 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr129)
	}

	// Create manual allowlist via helper
	alID, err := getOrCreateManualAllowlist("10.42.1.42")
	if err != nil {
		t.Fatalf("failed to create manual allowlist: %v", err)
	}
	if alID == 0 {
		t.Fatal("expected non-zero allowlist ID")
	}

	// Add a domain
	if _, fixtureErr142 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", alID, "safe.example.com"); fixtureErr142 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr142)
	}
	mustReloadPolicy(t)

	if !isAllowedForClient("10.42.1.42", "safe.example.com") {
		t.Error("expected safe.example.com to be allowed after adding to manual allowlist")
	}

	// Remove the domain
	if _, fixtureErr150 := db.Exec("DELETE FROM allowed_domains WHERE allowlist_id = ? AND domain = ?", alID, "safe.example.com"); fixtureErr150 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr150)
	}
	mustReloadPolicy(t)

	if isAllowedForClient("10.42.1.42", "safe.example.com") {
		t.Error("expected safe.example.com to NOT be allowed after removal")
	}

	// getOrCreateManualAllowlist should return the same ID on second call
	alID2, fixtureErr158 := getOrCreateManualAllowlist("10.42.1.42")
	if fixtureErr158 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr158)
	}
	if alID2 != alID {
		t.Errorf("expected same allowlist ID on second call, got %d vs %d", alID, alID2)
	}
}

func TestAllowlistViaGroup(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create allowlist
	result, fixtureErr169 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Group Allowlist", true)
	if fixtureErr169 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr169)
	}
	listID, fixtureErr171 := result.LastInsertId()
	if fixtureErr171 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr171)
	}

	if _, fixtureErr173 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, "group-allowed.example.com"); fixtureErr173 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr173)
	}

	// Create group, add member, assign allowlist to group
	groupResult, fixtureErr176 := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "TestGroup")
	if fixtureErr176 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr176)
	}
	groupID, fixtureErr177 := groupResult.LastInsertId()
	if fixtureErr177 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr177)
	}

	if _, fixtureErr179 := db.Exec("INSERT INTO client_group_members (client_ip, group_id) VALUES (?, ?)", "10.42.1.43", groupID); fixtureErr179 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr179)
	}
	if _, fixtureErr180 := db.Exec("INSERT INTO group_allowlists (group_id, allowlist_id) VALUES (?, ?)", groupID, listID); fixtureErr180 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr180)
	}

	// Seed query log
	if _, fixtureErr183 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.43", "test.com.", "A", "NOERROR"); fixtureErr183 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr183)
	}

	mustReloadPolicy(t)

	if !isAllowedForClient("10.42.1.43", "group-allowed.example.com") {
		t.Error("expected group-allowed.example.com to be allowed via group assignment")
	}
}

func TestIsAllowedForClientCacheEffectiveness(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr197 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Cache Test", true)
	if fixtureErr197 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr197)
	}
	listID, fixtureErr199 := result.LastInsertId()
	if fixtureErr199 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr199)
	}
	if _, fixtureErr200 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, "cached-allow.com"); fixtureErr200 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr200)
	}
	if _, fixtureErr201 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr201 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr201)
	}
	if _, fixtureErr203 := db.Exec("INSERT INTO client_allowlists (client_ip, allowlist_id) VALUES (?, ?)", "10.42.1.42", listID); fixtureErr203 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr203)
	}

	mustReloadPolicy(t)

	// Clear policy cache
	policyCache.Clear()

	// First call populates cache
	result1 := isAllowedForClient("10.42.1.42", "cached-allow.com")

	cacheKey := "10.42.1.42:cached-allow.com"
	cachedResult, ok := policyCache.Load(testPolicyKey(cacheKey))
	if !ok {
		t.Fatal("expected policy cache to be populated after first call")
	}
	if (cachedResult.Result == "allow") != result1 {
		t.Error("cached value doesn't match result")
	}

	// Second call should be consistent
	result2 := isAllowedForClient("10.42.1.42", "cached-allow.com")
	if result1 != result2 {
		t.Error("inconsistent results between cached and uncached calls")
	}
}

func TestHandleAPIGetAllowlists(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Seed an allowlist
	if _, fixtureErr234 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Test Allow", true); fixtureErr234 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr234)
	}

	req := httptest.NewRequest("GET", "/api/allowlists", nil)
	w := httptest.NewRecorder()

	handleAPIGetAllowlists(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	lists, fixtureErr246 := data.([]interface{})
	if !fixtureErr246 {
		t.Fatalf("unexpected fixture value type: %T", data)
	}
	if len(lists) != 1 {
		t.Errorf("expected 1 allowlist, got %d", len(lists))
	}
}

func TestHandleAPICreateAllowlist(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	body := bytes.NewBufferString(`{"url":"","alias":"Manual Test","enabled":true}`)
	req := httptest.NewRequest("POST", "/api/allowlists", body)
	w := httptest.NewRecorder()

	handleAPICreateAllowlist(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	var resp apiResponse
	if fixtureErr267 := json.NewDecoder(w.Body).Decode(&resp); fixtureErr267 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr267)
	}
	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be a map")
	}
	id, ok := data["id"].(float64)
	if !ok {
		t.Fatalf("expected numeric allowlist ID, got %T", data["id"])
	}
	if id < 1 {
		t.Error("expected positive allowlist ID")
	}
	if data["alias"] != "Manual Test" {
		t.Errorf("expected alias 'Manual Test', got %v", data["alias"])
	}
}

func TestHandleAPIAllowlistActionToggle(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr284 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Toggle Test", true)
	if fixtureErr284 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr284)
	}
	id, fixtureErr285 := result.LastInsertId()
	if fixtureErr285 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr285)
	}

	req := httptest.NewRequest("POST", "/api/allowlists/"+itoa(int(id))+"/toggle", nil)
	w := httptest.NewRecorder()
	handleAPIAllowlistAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify it was toggled
	var enabled bool
	if fixtureErr297 := db.QueryRow("SELECT enabled FROM allowlists WHERE id = ?", id).Scan(&enabled); fixtureErr297 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr297)
	}
	if enabled {
		t.Error("expected allowlist to be disabled after toggle")
	}
}

func TestHandleAPIAllowlistActionDelete(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr307 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Delete Test", true)
	if fixtureErr307 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr307)
	}
	id, fixtureErr308 := result.LastInsertId()
	if fixtureErr308 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr308)
	}

	req := httptest.NewRequest("DELETE", "/api/allowlists/"+itoa(int(id)), nil)
	w := httptest.NewRecorder()
	handleAPIAllowlistAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var count int
	if fixtureErr319 := db.QueryRow("SELECT COUNT(*) FROM allowlists WHERE id = ?", id).Scan(&count); fixtureErr319 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr319)
	}
	if count != 0 {
		t.Error("expected allowlist to be deleted")
	}
}

func TestHandleAPIAllowlistActionUpdate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, fixtureErr329 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Update Test", true)
	if fixtureErr329 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr329)
	}
	id, fixtureErr330 := result.LastInsertId()
	if fixtureErr330 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr330)
	}

	body := bytes.NewBufferString(`{"alias":"Updated Alias"}`)
	req := httptest.NewRequest("PUT", "/api/allowlists/"+itoa(int(id)), body)
	w := httptest.NewRecorder()
	handleAPIAllowlistAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var alias string
	if fixtureErr342 := db.QueryRow("SELECT alias FROM allowlists WHERE id = ?", id).Scan(&alias); fixtureErr342 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr342)
	}
	if alias != "Updated Alias" {
		t.Errorf("expected alias 'Updated Alias', got %q", alias)
	}
}

func TestHandleAPIClientAllowDomainCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Seed client
	if _, fixtureErr353 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr353 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr353)
	}

	// POST: add domain
	body := bytes.NewBufferString(`{"domain":"manual-allow.example.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/10.42.1.42/allow-domain", body)
	w := httptest.NewRecorder()
	handleAPIClient(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST, got %d", w.Code)
	}

	if !isAllowedForClient("10.42.1.42", "manual-allow.example.com") {
		t.Error("expected domain to be allowed after adding")
	}

	// DELETE: remove domain
	req = httptest.NewRequest("DELETE", "/api/clients/10.42.1.42/allow-domain/manual-allow.example.com", nil)
	w = httptest.NewRecorder()
	handleAPIClient(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", w.Code)
	}

	if isAllowedForClient("10.42.1.42", "manual-allow.example.com") {
		t.Error("expected domain to NOT be allowed after removal")
	}
}

func TestHandleAPIGroupAllowDomainCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, err := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "Allow Domain Group")
	if err != nil {
		t.Fatalf("seed group: %v", err)
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("group ID: %v", err)
	}
	domain := "manual-group-allow.example.com"

	body := bytes.NewBufferString(`{"domain":"manual-group-allow.example.com"}`)
	req := httptest.NewRequest("POST", "/api/groups/"+itoa(int(groupID))+"/allow-domain", body)
	w := httptest.NewRecorder()
	handleAPIGroupAction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST, got %d: %s", w.Code, w.Body.String())
	}

	domains, err := getManualAllowDomainsForGroup(int(groupID))
	if err != nil {
		t.Fatalf("list group allow domains: %v", err)
	}
	if len(domains) != 1 || domains[0] != domain {
		t.Fatalf("expected group allow domain %q, got %v", domain, domains)
	}

	req = httptest.NewRequest("DELETE", "/api/groups/"+itoa(int(groupID))+"/allow-domain/"+domain, nil)
	w = httptest.NewRecorder()
	handleAPIGroupAction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d: %s", w.Code, w.Body.String())
	}

	domains, err = getManualAllowDomainsForGroup(int(groupID))
	if err != nil {
		t.Fatalf("list group allow domains after DELETE: %v", err)
	}
	if len(domains) != 0 {
		t.Fatalf("expected group allow domain to be deleted, got %v", domains)
	}
}

func TestHandleAPIClientAllowlistAssignment(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create allowlist and client
	result, fixtureErr435 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Assign Test", true)
	if fixtureErr435 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr435)
	}
	listID, fixtureErr436 := result.LastInsertId()
	if fixtureErr436 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr436)
	}
	if _, fixtureErr437 := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", listID, "assigned-allow.com"); fixtureErr437 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr437)
	}
	if _, fixtureErr438 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, ?, ?, ?)",
		"10.42.1.42", "test.com.", "A", "NOERROR"); fixtureErr438 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr438)
	}

	// POST: assign allowlist to client
	req := httptest.NewRequest("POST", "/api/clients/10.42.1.42/allowlists/"+itoa(int(listID)), nil)
	w := httptest.NewRecorder()
	handleAPIClient(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST assign, got %d", w.Code)
	}

	if !isAllowedForClient("10.42.1.42", "assigned-allow.com") {
		t.Error("expected domain to be allowed after assigning allowlist")
	}

	// DELETE: unassign
	req = httptest.NewRequest("DELETE", "/api/clients/10.42.1.42/allowlists/"+itoa(int(listID)), nil)
	w = httptest.NewRecorder()
	handleAPIClient(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE unassign, got %d", w.Code)
	}
}

func TestHandleAPIGroupAllowlistAssignment(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create allowlist and group
	result, fixtureErr469 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)", "", "Group Assign", true)
	if fixtureErr469 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr469)
	}
	listID, fixtureErr470 := result.LastInsertId()
	if fixtureErr470 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr470)
	}
	groupResult, fixtureErr471 := db.Exec("INSERT INTO client_groups (name) VALUES (?)", "AllowGroup")
	if fixtureErr471 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr471)
	}
	groupID, fixtureErr472 := groupResult.LastInsertId()
	if fixtureErr472 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr472)
	}

	// POST: assign allowlist to group
	req := httptest.NewRequest("POST", "/api/groups/"+itoa(int(groupID))+"/allowlists/"+itoa(int(listID)), nil)
	w := httptest.NewRecorder()
	handleAPIGroupAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST assign, got %d", w.Code)
	}

	// Verify assignment
	var count int
	if fixtureErr485 := db.QueryRow("SELECT COUNT(*) FROM group_allowlists WHERE group_id = ? AND allowlist_id = ?", groupID, listID).Scan(&count); fixtureErr485 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr485)
	}
	if count != 1 {
		t.Error("expected allowlist to be assigned to group")
	}

	// DELETE: unassign
	req = httptest.NewRequest("DELETE", "/api/groups/"+itoa(int(groupID))+"/allowlists/"+itoa(int(listID)), nil)
	w = httptest.NewRecorder()
	handleAPIGroupAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE unassign, got %d", w.Code)
	}

	if fixtureErr499 := db.QueryRow("SELECT COUNT(*) FROM group_allowlists WHERE group_id = ? AND allowlist_id = ?", groupID, listID).Scan(&count); fixtureErr499 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr499)
	}
	if count != 0 {
		t.Error("expected allowlist to be unassigned from group")
	}
}
