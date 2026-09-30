package main

import (
	"context"

	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	"strings"
	"testing"
	"time"
)

func TestCreateAndValidateToken(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	plaintext, token, err := createAPIToken("Test Token", "admin")
	if err != nil {
		t.Fatalf("createAPIToken failed: %v", err)
	}

	// sv_ (3) + 64 hex chars = 67
	if len(plaintext) != 67 {
		t.Errorf("expected 67 char token (sv_ + 64 hex), got %d", len(plaintext))
	}

	if !strings.HasPrefix(plaintext, "sv_") {
		t.Errorf("expected token to start with sv_, got %q", plaintext[:6])
	}

	if token.Name != "Test Token" {
		t.Errorf("expected name 'Test Token', got %q", token.Name)
	}

	if token.Role != "admin" {
		t.Errorf("expected role 'admin', got %q", token.Role)
	}

	if token.TokenPrefix != plaintext[:11] {
		t.Errorf("prefix mismatch: expected %q, got %q", plaintext[:11], token.TokenPrefix)
	}

	var storedToken string
	if err := db.QueryRow("SELECT COALESCE(token, '') FROM api_tokens WHERE id = ?", token.ID).Scan(&storedToken); err != nil {
		t.Fatalf("query stored token: %v", err)
	}
	if storedToken != "" {
		t.Errorf("expected plaintext token column to be empty, got %q", storedToken)
	}

	// Validate with correct key
	validated, fixtureErr57 := validateAPIKey(context.Background(), plaintext)
	if fixtureErr57 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr57)
	}
	if validated == nil {
		t.Fatal("validateAPIKey returned nil for correct key")
	}
	if validated.ID != token.ID {
		t.Errorf("validated ID mismatch: expected %d, got %d", token.ID, validated.ID)
	}

	// Reject wrong key
	bad, fixtureErr66 := validateAPIKey(context.Background(), "sv_0000000000000000000000000000000000000000000000000000000000000000")
	if fixtureErr66 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr66)
	}
	if bad != nil {
		t.Error("validateAPIKey should return nil for wrong key")
	}
}

func TestTokenPrefixFiltering(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create multiple tokens
	pt1, _, fixtureErr77 := createAPIToken("Token A", "admin")
	if fixtureErr77 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr77)
	}
	pt2, _, fixtureErr78 := createAPIToken("Token B", "readonly")
	if fixtureErr78 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr78)
	}

	// Both should validate correctly
	v1, fixtureErr81 := validateAPIKey(context.Background(), pt1)
	if fixtureErr81 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr81)
	}
	v2, fixtureErr82 := validateAPIKey(context.Background(), pt2)
	if fixtureErr82 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr82)
	}

	if v1 == nil || v1.Name != "Token A" {
		t.Error("Token A validation failed")
	}
	if v2 == nil || v2.Name != "Token B" {
		t.Error("Token B validation failed")
	}
}

func TestAuthMiddlewareRequiresCredentialsWhenNoUsersExist(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// No tokens + no users should fail closed.
	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	req := httptest.NewRequest("POST", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no users and no credentials, got %d", w.Code)
	}
}

func TestAuthMiddlewareReadonly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	plaintext, _, fixtureErr114 := createAPIToken("Readonly Token", "readonly")
	if fixtureErr114 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr114)
	}

	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	// GET with readonly token should pass
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Api-Key", plaintext)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for readonly GET, got %d", w.Code)
	}

	// POST with readonly token should be 403
	req = httptest.NewRequest("POST", "/api/test", nil)
	req.Header.Set("X-Api-Key", plaintext)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for readonly POST, got %d", w.Code)
	}
}

func TestAuthMiddlewareAdmin(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	plaintext, _, fixtureErr145 := createAPIToken("Admin Token", "admin")
	if fixtureErr145 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr145)
	}

	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	// POST with admin token should pass
	req := httptest.NewRequest("POST", "/api/test", nil)
	req.Header.Set("X-Api-Key", plaintext)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin POST, got %d", w.Code)
	}

	// DELETE with admin token should pass
	req = httptest.NewRequest("DELETE", "/api/test", nil)
	req.Header.Set("X-Api-Key", plaintext)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin DELETE, got %d", w.Code)
	}
}

func TestAuthMiddlewareNoKeyWithUsers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Seed an admin user to enable auth
	_, err := createAdminUser("admin", "password123", RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create admin user: %v", err)
	}

	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no key, got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalidKey(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Invalid API key should always get 401, even without admin users
	handler := apiAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Api-Key", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with invalid key, got %d", w.Code)
	}
}

func TestAnalysisEndpointsReadonly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	plaintext, _, fixtureErr218 := createAPIToken("Readonly", "readonly")
	if fixtureErr218 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr218)
	}

	handler := readonlyAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	// POST with readonly token should pass (analysis endpoints are read-only)
	req := httptest.NewRequest("POST", "/api/analysis/matrix", nil)
	req.Header.Set("X-Api-Key", plaintext)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for readonly POST to analysis, got %d", w.Code)
	}
}

func TestTokenCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create
	_, token, err := createAPIToken("Lifecycle Token", "admin")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// List
	tokens, err := listAPITokens()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token, got %d", len(tokens))
	}
	if tokens[0].Name != "Lifecycle Token" {
		t.Errorf("expected 'Lifecycle Token', got %q", tokens[0].Name)
	}
	if tokens[0].Token != "" {
		t.Errorf("expected listed tokens to omit plaintext, got %q", tokens[0].Token)
	}

	// Revoke
	if err := revokeAPIToken(token.ID); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	var fixtureErr265 error
	tokens, fixtureErr265 = listAPITokens()
	if fixtureErr265 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr265)
	}
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens after revoke, got %d", len(tokens))
	}

	// Revoke non-existent
	err = revokeAPIToken(999)
	if err == nil {
		t.Error("expected error revoking non-existent token")
	}
}

func TestTokenAPIEndpoints(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// POST /api/tokens — create
	body := `{"name": "Test API Token", "role": "admin"}`
	req := httptest.NewRequest("POST", "/api/tokens", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPITokensRouter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 creating token, got %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		Data struct {
			Token string `json:"token"`
			ID    int    `json:"id"`
			Name  string `json:"name"`
			Role  string `json:"role"`
		} `json:"data"`
	}
	if fixtureErr299 := json.Unmarshal(w.Body.Bytes(), &createResp); fixtureErr299 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr299)
	}

	if createResp.Data.Token == "" {
		t.Error("expected plaintext token in response")
	}
	if createResp.Data.Name != "Test API Token" {
		t.Errorf("expected name 'Test API Token', got %q", createResp.Data.Name)
	}

	// GET /api/tokens — list
	req = httptest.NewRequest("GET", "/api/tokens", nil)
	w = httptest.NewRecorder()
	handleAPITokensRouter(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 listing tokens, got %d", w.Code)
	}

	var listResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	if fixtureErr320 := json.Unmarshal(w.Body.Bytes(), &listResp); fixtureErr320 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr320)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 token, got %d", len(listResp.Data))
	}
	if _, ok := listResp.Data[0]["token"]; ok {
		t.Fatalf("expected token list to omit plaintext token, got %v", listResp.Data[0]["token"])
	}

	// DELETE /api/tokens/{id}
	req = httptest.NewRequest("DELETE", "/api/tokens/1", nil)
	w = httptest.NewRecorder()
	handleAPITokenAction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 deleting token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminAuthMiddleware(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	readonlyPT, _, fixtureErr342 := createAPIToken("Readonly", "readonly")
	if fixtureErr342 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr342)
	}
	adminPT, _, fixtureErr343 := createAPIToken("Admin", "admin")
	if fixtureErr343 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr343)
	}

	handler := adminAuth(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, "ok")
	})

	// Readonly token should be 403
	req := httptest.NewRequest("GET", "/api/tokens", nil)
	req.Header.Set("X-Api-Key", readonlyPT)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for readonly on admin endpoint, got %d", w.Code)
	}

	// Admin token should pass
	req = httptest.NewRequest("GET", "/api/tokens", nil)
	req.Header.Set("X-Api-Key", adminPT)
	w = httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin on admin endpoint, got %d", w.Code)
	}
}

func TestMigrateSchemaScrubsLegacyPlaintextTokens(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	plaintext, token, err := createAPIToken("Legacy Token", "readonly")
	if err != nil {
		t.Fatalf("createAPIToken failed: %v", err)
	}

	if _, err := db.Exec("UPDATE api_tokens SET token = ?, updated_at = '', node_id = '' WHERE id = ?", plaintext, token.ID); err != nil {
		t.Fatalf("seed legacy plaintext token: %v", err)
	}

	if err := migrateSchema(); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var storedToken, updatedAt string
	if err := db.QueryRow("SELECT COALESCE(token, ''), COALESCE(updated_at, '') FROM api_tokens WHERE id = ?", token.ID).Scan(&storedToken, &updatedAt); err != nil {
		t.Fatalf("query scrubbed token: %v", err)
	}
	if storedToken != "" {
		t.Fatalf("expected migration to scrub plaintext token, got %q", storedToken)
	}
	if updatedAt == "" {
		t.Fatal("expected migration to stamp updated_at when scrubbing plaintext tokens")
	}

	validated, validationErr := validateAPIKey(context.Background(), plaintext)
	if validationErr != nil {
		t.Fatalf("validate fixture API key: %v", validationErr)
	}
	if validated == nil {
		t.Fatal("expected scrubbed token to remain valid")
	}
}

// --- Admin user management tests ---

func TestAdminUserCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create
	user, err := createAdminUser("sam", "hunter2", RoleAdmin)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if user.Username != "sam" {
		t.Errorf("expected username 'sam', got %q", user.Username)
	}
	if user.Role != RoleAdmin {
		t.Errorf("expected role 'admin', got %q", user.Role)
	}
	if !hasUsers.Load() {
		t.Error("expected hasUsers to be true after creating user")
	}

	// List
	users, err := listAdminUsers()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Username != "sam" {
		t.Errorf("expected 'sam', got %q", users[0].Username)
	}

	// Demoting the only admin is refused (A10); it succeeds once another admin exists.
	if err := updateAdminUser(user.ID, RoleReadonly, ""); err != errDemoteLastAdmin {
		t.Fatalf("demoting the last admin: err = %v, want errDemoteLastAdmin", err)
	}
	secondUser, err := createAdminUser("backup-admin", "secret", RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create second admin: %v", err)
	}
	if err := updateAdminUser(user.ID, RoleReadonly, ""); err != nil {
		t.Fatalf("update role failed: %v", err)
	}
	var fixtureErr447 error
	users, fixtureErr447 = listAdminUsers()
	if fixtureErr447 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr447)
	}
	if users[0].Role != RoleReadonly {
		t.Errorf("expected role 'readonly' after update, got %q", users[0].Role)
	}
	if err := updateAdminUser(secondUser.ID, RoleReadonly, ""); err != errDemoteLastAdmin {
		t.Fatalf("demoting the remaining admin: err = %v, want errDemoteLastAdmin", err)
	}

	// Update password
	if err := updateAdminUser(user.ID, "", "newpassword"); err != nil {
		t.Fatalf("update password failed: %v", err)
	}

	// Verify new password works
	_, hash, _, _, found, fixtureErr461 := getAdminUserByUsername("sam")
	if fixtureErr461 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr461)
	}
	if !found {
		t.Fatal("user not found after update")
	}
	if hash == "" {
		t.Error("expected non-empty password hash")
	}

	// Duplicate username
	_, err = createAdminUser("sam", "anotherpass", RoleAdmin)
	if err == nil {
		t.Error("expected error for duplicate username")
	}

	// Delete while another admin still exists
	if err := deleteAdminUser(user.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	var fixtureErr479 error
	users, fixtureErr479 = listAdminUsers()
	if fixtureErr479 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr479)
	}
	if len(users) != 1 {
		t.Errorf("expected 1 user after delete, got %d", len(users))
	}
	if !hasUsers.Load() {
		t.Error("expected hasUsers to remain true while at least one user exists")
	}

	// Deleting the last remaining admin should fail.
	err = deleteAdminUser(secondUser.ID)
	if err == nil {
		t.Fatal("expected error deleting last admin user")
	}
	if err != errDeleteLastAdmin {
		t.Fatalf("expected errDeleteLastAdmin, got %v", err)
	}

	var fixtureErr496 error
	users, fixtureErr496 = listAdminUsers()
	if fixtureErr496 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr496)
	}
	if len(users) != 1 {
		t.Errorf("expected last admin to remain after failed delete, got %d users", len(users))
	}

	// Delete non-existent
	err = deleteAdminUser(999)
	if err == nil {
		t.Error("expected error deleting non-existent user")
	}
}

// Server-side session tests. These replace the tests of the former
// username:HMAC(passwordHash, sessionSecret) cookie, whose deterministic,
// logout-surviving value was finding A4 in design/SECURITY-REVIEW-2026-09.md.

func withSessionClock(t *testing.T, start time.Time) *time.Time {
	t.Helper()
	now := start
	prev := sessionNow
	sessionNow = func() time.Time { return now }
	t.Cleanup(func() { sessionNow = prev })
	return &now
}

func TestUserAPIEndpoints(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// POST /api/users — create
	body := `{"username": "testuser", "password": "testpass123", "role": "admin"}`
	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(body))
	w := httptest.NewRecorder()
	handleAPIUsersRouter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		Data AdminUser `json:"data"`
	}
	if fixtureErr1024 := json.Unmarshal(w.Body.Bytes(), &createResp); fixtureErr1024 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1024)
	}
	if createResp.Data.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %q", createResp.Data.Username)
	}

	// GET /api/users — list
	req = httptest.NewRequest("GET", "/api/users", nil)
	w = httptest.NewRecorder()
	handleAPIUsersRouter(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var listResp struct {
		Data []AdminUser `json:"data"`
	}
	if fixtureErr1041 := json.Unmarshal(w.Body.Bytes(), &listResp); fixtureErr1041 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1041)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 user, got %d", len(listResp.Data))
	}

	// PUT /api/users/{id} — demoting the only admin is refused (A10)...
	body = `{"role": "readonly"}`
	req = httptest.NewRequest("PUT", "/api/users/1", strings.NewReader(body))
	w = httptest.NewRecorder()
	handleAPIUserAction(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 demoting the last admin, got %d: %s", w.Code, w.Body.String())
	}

	// ...and succeeds once another admin exists.
	_, err := createAdminUser("backup-admin", "secret", RoleAdmin)
	if err != nil {
		t.Fatalf("failed to create second admin: %v", err)
	}
	req = httptest.NewRequest("PUT", "/api/users/1", strings.NewReader(body))
	w = httptest.NewRecorder()
	handleAPIUserAction(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 updating user, got %d: %s", w.Code, w.Body.String())
	}

	// A missing user is a 404, not a silent success.
	req = httptest.NewRequest("PUT", "/api/users/999", strings.NewReader(body))
	w = httptest.NewRecorder()
	handleAPIUserAction(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 updating a missing user, got %d: %s", w.Code, w.Body.String())
	}

	// DELETE /api/users/{id} — delete while another admin exists
	req = httptest.NewRequest("DELETE", "/api/users/1", nil)
	w = httptest.NewRecorder()
	handleAPIUserAction(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 deleting user, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSelfDeletePrevention(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	serverStart = time.Now()

	user, err := createAdminUser("admin", "testpass", RoleAdmin)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	_, hash, _, _, _, fixtureErr1096 := getAdminUserByUsername("admin")
	if fixtureErr1096 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1096)
	}
	cookieVal, _, err := createSession("admin", hash)
	if err != nil {
		t.Fatal(err)
	}

	// Try to delete self via session cookie
	idStr := strings.TrimSpace(func() string {
		b, fixtureErr1103 := json.Marshal(user.ID)
		if fixtureErr1103 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr1103)
		}
		return string(b)
	}())
	req := httptest.NewRequest("DELETE", "/api/users/"+idStr, nil)
	req.AddCookie(&http.Cookie{Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, Name: "svart_session", Value: cookieVal})
	w := httptest.NewRecorder()
	handleAPIUserAction(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for self-delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteLastAdminRejectedForAPITokenCaller(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	user, err := createAdminUser("admin", "testpass", RoleAdmin)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	token, _, err := createAPIToken("Admin Token", RoleAdmin)
	if err != nil {
		t.Fatalf("createAPIToken failed: %v", err)
	}

	req := httptest.NewRequest("DELETE", "/api/users/1", nil)
	req.Header.Set("X-Api-Key", token)
	w := httptest.NewRecorder()

	adminAuth(handleAPIUserAction)(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 deleting last admin via token, got %d: %s", w.Code, w.Body.String())
	}

	users, fixtureErr1138 := listAdminUsers()
	if fixtureErr1138 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1138)
	}
	if len(users) != 1 || users[0].ID != user.ID {
		t.Fatalf("expected last admin user to remain after rejected delete, got %+v", users)
	}
}

func TestHandleAPIAuthLoginFailsClosedWithoutUsers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"svart"}`))
	w := httptest.NewRecorder()

	handleAPIAuthLogin(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when no admin users exist, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleAPIAuthCheckDoesNotAutoAuthenticateWithoutUsers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/auth/check", nil)
	w := httptest.NewRecorder()

	handleAPIAuthCheck(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for auth check, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			Authenticated bool   `json:"authenticated"`
			Role          string `json:"role"`
			Username      string `json:"username"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Data.Authenticated {
		t.Fatalf("expected unauthenticated response when no admin users exist, got %+v", resp.Data)
	}
	if resp.Data.Role != "" || resp.Data.Username != "" {
		t.Fatalf("expected empty auth identity when no admin users exist, got %+v", resp.Data)
	}
}

func TestBootstrapAuth(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// No users — bootstrap should seed from env
	t.Setenv("ADMIN_USER", "testadmin")
	t.Setenv("ADMIN_PASSWORD", "testpass")

	if fixtureErr1198 := bootstrapAuth(); fixtureErr1198 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1198)
	}

	if !hasUsers.Load() {
		t.Error("expected hasUsers to be true after bootstrap")
	}

	users, fixtureErr1204 := listAdminUsers()
	if fixtureErr1204 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1204)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user after bootstrap, got %d", len(users))
	}
	if users[0].Username != "testadmin" {
		t.Errorf("expected username 'testadmin', got %q", users[0].Username)
	}

	// Second call should NOT create duplicate
	if fixtureErr1213 := bootstrapAuth(); fixtureErr1213 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1213)
	}
	var fixtureErr1214 error
	users, fixtureErr1214 = listAdminUsers()
	if fixtureErr1214 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1214)
	}
	if len(users) != 1 {
		t.Errorf("expected still 1 user after second bootstrap, got %d", len(users))
	}
}

// TestBootstrapAuthSkipsSeedingWithoutExplicitPassword verifies that an unset
// ADMIN_PASSWORD no longer silently activates a well-known admin/svart
// credential. bootstrapAuth must skip seeding entirely and leave the
// deployment fail-closed (authEnabled()==false) until an operator explicitly
// sets ADMIN_PASSWORD.
func TestBootstrapAuthSkipsSeedingWithoutExplicitPassword(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	t.Setenv("ADMIN_USER", "admin")
	if orig, ok := os.LookupEnv("ADMIN_PASSWORD"); ok {
		if fixtureErr1231 := os.Unsetenv("ADMIN_PASSWORD"); fixtureErr1231 != nil {
			t.Fatalf("fixture operation failed: %v", fixtureErr1231)
		}
		t.Cleanup(func() {
			if fixtureErr1232 := os.Setenv("ADMIN_PASSWORD", orig); fixtureErr1232 != nil {
				t.Fatalf("fixture operation failed: %v", fixtureErr1232)
			}
		})
	}

	if fixtureErr1235 := bootstrapAuth(); fixtureErr1235 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1235)
	}

	if hasUsers.Load() {
		t.Error("expected hasUsers to remain false when ADMIN_PASSWORD is unset")
	}

	users, fixtureErr1241 := listAdminUsers()
	if fixtureErr1241 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1241)
	}
	if len(users) != 0 {
		t.Fatalf("expected no admin_users row seeded when ADMIN_PASSWORD is unset, got %d", len(users))
	}

	if authEnabled() {
		t.Error("expected authEnabled() to be false (fail closed) without a seeded admin")
	}
}

// TestBootstrapAuthSkipsSeedingWithEmptyPassword verifies that explicitly
// setting ADMIN_PASSWORD="" behaves the same as leaving it unset — no
// account is seeded.
func TestBootstrapAuthSkipsSeedingWithEmptyPassword(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "")

	if fixtureErr1261 := bootstrapAuth(); fixtureErr1261 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1261)
	}

	users, fixtureErr1263 := listAdminUsers()
	if fixtureErr1263 != nil {
		t.Fatalf("fixture operation failed: %v", fixtureErr1263)
	}
	if len(users) != 0 {
		t.Fatalf("expected no admin_users row seeded for empty ADMIN_PASSWORD, got %d", len(users))
	}
}
