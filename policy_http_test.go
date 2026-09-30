package main

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"testing"
)

func TestPolicyEvaluateAPI(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	tests := []struct {
		name       string
		clientIP   string
		domain     string
		wantCode   int
		wantResult string
	}{
		{
			name:       "blocked by range",
			clientIP:   "10.42.3.50",
			domain:     "ads.google.com",
			wantCode:   http.StatusOK,
			wantResult: "block",
		},
		{
			name:       "allowed by IP tier (override group block)",
			clientIP:   "10.42.1.42",
			domain:     "reddit.com",
			wantCode:   http.StatusOK,
			wantResult: "allow",
		},
		{
			name:       "not in any policy",
			clientIP:   "10.42.1.42",
			domain:     "example.org",
			wantCode:   http.StatusOK,
			wantResult: "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/policy/evaluate?client_ip="+tt.clientIP+"&domain="+tt.domain, nil)
			w := httptest.NewRecorder()
			handleAPIPolicyEvaluate(w, req)

			if w.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d", tt.wantCode, w.Code)
			}

			data, apiErr := decodeAPIResponse(t, w.Body)
			if apiErr != nil {
				t.Fatalf("unexpected error: %s", *apiErr)
			}

			m, fixtureErr383 := data.(map[string]interface{})
			if !fixtureErr383 {
				t.Fatalf("unexpected fixture value type: %T", data)
			}
			gotResult, fixtureErr384 := m["result"].(string)
			if !fixtureErr384 {
				t.Fatalf("unexpected fixture value type: %T", m["result"])
			}
			if gotResult != tt.wantResult {
				t.Errorf("expected result=%q, got %q", tt.wantResult, gotResult)
			}

			// Verify client_ip and domain are in the response
			if m["client_ip"] != tt.clientIP {
				t.Errorf("expected client_ip=%q in response", tt.clientIP)
			}
			if m["domain"] != tt.domain {
				t.Errorf("expected domain=%q in response", tt.domain)
			}
		})
	}
}

func TestPolicyEvaluateAPIMissingParams(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/policy/evaluate", nil)
	w := httptest.NewRecorder()
	handleAPIPolicyEvaluate(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing params, got %d", w.Code)
	}
}

func TestPolicyEvaluateAPIWrongMethod(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/policy/evaluate", nil)
	w := httptest.NewRecorder()
	handleAPIPolicyEvaluate(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

// TestIsBlockedForClient_BackwardCompat verifies the thin wrapper still works
// with the existing test suite patterns.
func TestIsBlockedForClient_BackwardCompat(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	if !isBlockedForClient("10.42.1.42", "ads.google.com.") {
		t.Error("expected ads.google.com to be blocked via isBlockedForClient wrapper")
	}

	if isBlockedForClient("10.42.1.42", "github.com.") {
		t.Error("expected github.com to NOT be blocked")
	}
}

// --- Phase 2.6: block-domain CRUD + conflict prevention tests ---

func TestClientBlockDomain_CRUD(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	clientIP := "10.42.1.42"
	domain := "sketchy-tracker.io"

	result := evaluatePolicyFull(clientIP, domain, 1)
	if result.Result == "block" {
		t.Fatal("expected not blocked initially")
	}

	body := strings.NewReader(`{"domain":"sketchy-tracker.io"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIClient(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST block-domain: expected 200, got %d — %s", w.Code, w.Body.String())
	}

	policyCache.Clear()
	result = evaluatePolicyFull(clientIP, domain, 1)
	if result.Result != "block" {
		t.Error("expected blocked after adding custom block")
	}
	if resultTier(result) != "ip" {
		t.Errorf("expected tier 'ip', got %q", resultTier(result))
	}

	req = httptest.NewRequest("DELETE", "/api/clients/"+clientIP+"/block-domain/"+domain, nil)
	w = httptest.NewRecorder()
	handleAPIClient(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE block-domain: expected 200, got %d", w.Code)
	}

	policyCache.Clear()
	result = evaluatePolicyFull(clientIP, domain, 1)
	if result.Result == "block" {
		t.Error("expected not blocked after removing custom block")
	}
}

func TestGroupBlockDomain_CRUD(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	clientIP := "10.42.1.43"
	domain := "rogue-iot.example.com"

	result := evaluatePolicyFull(clientIP, domain, 1)
	if result.Result == "block" {
		t.Fatal("expected not blocked initially")
	}

	body := strings.NewReader(`{"domain":"rogue-iot.example.com"}`)
	req := httptest.NewRequest("POST", "/api/groups/1/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIGroupAction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST group block-domain: expected 200, got %d — %s", w.Code, w.Body.String())
	}

	policyCache.Clear()
	result = evaluatePolicyFull(clientIP, domain, 1)
	if result.Result != "block" {
		t.Error("expected blocked after adding group custom block")
	}
	if resultTier(result) != "group" {
		t.Errorf("expected tier 'group', got %q", resultTier(result))
	}

	req = httptest.NewRequest("DELETE", "/api/groups/1/block-domain/"+domain, nil)
	w = httptest.NewRecorder()
	handleAPIGroupAction(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE group block-domain: expected 200, got %d", w.Code)
	}

	policyCache.Clear()
	result = evaluatePolicyFull(clientIP, domain, 1)
	if result.Result == "block" {
		t.Error("expected not blocked after removing group custom block")
	}
}

func TestConflict_ClientAllowThenBlock(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	clientIP := "10.42.1.50"
	if _, fixtureErr531 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", clientIP); fixtureErr531 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr531)
	}

	body := strings.NewReader(`{"domain":"facebook.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/allow-domain", body)
	w := httptest.NewRecorder()
	handleAPIClientAllowDomain(w, req, clientIP, []string{clientIP, "allow-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("allow POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"facebook.com"}`)
	req = httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w = httptest.NewRecorder()
	handleAPIClientBlockDomain(w, req, clientIP, []string{clientIP, "block-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — %s", w.Code, w.Body.String())
	}
}

func TestConflict_ClientBlockThenAllow(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	clientIP := "10.42.1.50"
	if _, fixtureErr555 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", clientIP); fixtureErr555 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr555)
	}

	body := strings.NewReader(`{"domain":"tiktok.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIClientBlockDomain(w, req, clientIP, []string{clientIP, "block-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("block POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"tiktok.com"}`)
	req = httptest.NewRequest("POST", "/api/clients/"+clientIP+"/allow-domain", body)
	w = httptest.NewRecorder()
	handleAPIClientAllowDomain(w, req, clientIP, []string{clientIP, "allow-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — %s", w.Code, w.Body.String())
	}
}

func TestConflict_GroupAllowThenBlock(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, fixtureErr578 := db.Exec("INSERT INTO client_groups (name) VALUES ('TestGroup')"); fixtureErr578 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr578)
	}

	body := strings.NewReader(`{"domain":"instagram.com"}`)
	req := httptest.NewRequest("POST", "/api/groups/1/allow-domain", body)
	w := httptest.NewRecorder()
	handleAPIGroupAllowDomain(w, req, 1, []string{"1", "allow-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("group allow POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"instagram.com"}`)
	req = httptest.NewRequest("POST", "/api/groups/1/block-domain", body)
	w = httptest.NewRecorder()
	handleAPIGroupBlockDomain(w, req, 1, []string{"1", "block-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — %s", w.Code, w.Body.String())
	}
}

func TestConflict_GroupBlockThenAllow(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, fixtureErr601 := db.Exec("INSERT INTO client_groups (name) VALUES ('TestGroup')"); fixtureErr601 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr601)
	}

	body := strings.NewReader(`{"domain":"snapchat.com"}`)
	req := httptest.NewRequest("POST", "/api/groups/1/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIGroupBlockDomain(w, req, 1, []string{"1", "block-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("group block POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"snapchat.com"}`)
	req = httptest.NewRequest("POST", "/api/groups/1/allow-domain", body)
	w = httptest.NewRecorder()
	handleAPIGroupAllowDomain(w, req, 1, []string{"1", "allow-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d — %s", w.Code, w.Body.String())
	}
}

func TestConflict_WildcardOverlap(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	clientIP := "10.42.1.50"
	if _, fixtureErr625 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", clientIP); fixtureErr625 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr625)
	}

	body := strings.NewReader(`{"domain":"*.tiktok.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/allow-domain", body)
	w := httptest.NewRecorder()
	handleAPIClientAllowDomain(w, req, clientIP, []string{clientIP, "allow-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("allow POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"app.tiktok.com"}`)
	req = httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w = httptest.NewRecorder()
	handleAPIClientBlockDomain(w, req, clientIP, []string{clientIP, "block-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for wildcard overlap, got %d — %s", w.Code, w.Body.String())
	}
}

func TestConflict_WildcardOverlapReverse(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	clientIP := "10.42.1.50"
	if _, fixtureErr649 := db.Exec("INSERT INTO query_logs (client_ip, query_name, query_type, response_code) VALUES (?, 'test.com.', 'A', 'NOERROR')", clientIP); fixtureErr649 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr649)
	}

	body := strings.NewReader(`{"domain":"*.tiktok.com"}`)
	req := httptest.NewRequest("POST", "/api/clients/"+clientIP+"/block-domain", body)
	w := httptest.NewRecorder()
	handleAPIClientBlockDomain(w, req, clientIP, []string{clientIP, "block-domain"})
	if w.Code != http.StatusOK {
		t.Fatalf("block POST expected 200, got %d — %s", w.Code, w.Body.String())
	}

	body = strings.NewReader(`{"domain":"app.tiktok.com"}`)
	req = httptest.NewRequest("POST", "/api/clients/"+clientIP+"/allow-domain", body)
	w = httptest.NewRecorder()
	handleAPIClientAllowDomain(w, req, clientIP, []string{clientIP, "allow-domain"})
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for reverse wildcard overlap, got %d — %s", w.Code, w.Body.String())
	}
}

func TestPolicyEvaluateAPI_EntityDetail(t *testing.T) {
	cleanup := setupPolicyTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/policy/evaluate?client_ip=10.42.1.42&domain=ads.google.com", nil)
	w := httptest.NewRecorder()
	handleAPIPolicyEvaluate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("unexpected error: %s", *apiErr)
	}

	m, fixtureErr685 := data.(map[string]interface{})
	if !fixtureErr685 {
		t.Fatalf("unexpected fixture value type: %T", data)
	}

	// Verify range_evaluation is present with entities
	rangeEval, ok := m["range_evaluation"].(map[string]interface{})
	if !ok {
		t.Fatal("expected range_evaluation to be present")
	}
	entities, ok := rangeEval["entities"].([]interface{})
	if !ok || len(entities) == 0 {
		t.Fatal("expected non-empty entities in range evaluation")
	}

	// Verify at least one entity has a result
	foundResult := false
	for _, e := range entities {
		em, fixtureErr700 := e.(map[string]interface{})
		if !fixtureErr700 {
			t.Fatalf("unexpected fixture value type: %T", e)
		}
		if em["result"] != "" {
			foundResult = true
			break
		}
	}
	if !foundResult {
		t.Error("expected at least one entity with a result in range evaluation")
	}
}

// --- Attribution tests ---
