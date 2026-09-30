package svart

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	// RoleReadonly permits authenticated inspection without administrative changes.
	RoleReadonly = "readonly"
	// RoleAdmin permits configuration and account administration.
	RoleAdmin = "admin"
)

var (
	errDeleteLastAdmin = errors.New("cannot delete the last admin user")
	errUserNotFound    = errors.New("user not found")
	errDemoteLastAdmin = errors.New("cannot demote the last admin user: promote another user to admin first")
)

type authContextKey string

const requestAuthContextKey authContextKey = "requestAuth"

// APIToken represents a stored API token (never includes the hash in JSON).
type APIToken struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Token       string `json:"token,omitempty"`
	TokenPrefix string `json:"token_prefix"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at,omitempty"`
}

const tokenPrefix = "sv_"

// AdminUser represents a stored admin user (password hash never in JSON).
type AdminUser struct {
	ID        int    `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// Cached auth state — avoids DB query and os.Getenv per request.
var (
	hasTokens atomic.Bool
	hasUsers  atomic.Bool

	// Token validation cache: sha256(apiKey) -> *validatedToken
	tokenAuthMu         sync.Mutex
	tokenAuthGeneration uint64
	tokenCache          sync.Map
	tokenCacheTTL       = 5 * time.Minute
	lastUsedUpdate      sync.Map // tokenID -> time.Time, throttles last_used_at writes
)

type validatedToken struct {
	token    *APIToken
	cachedAt time.Time
}

func withRequestAuth(r *http.Request, token *APIToken) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestAuthContextKey, token))
}

func requestAuth(r *http.Request) *APIToken {
	if r == nil {
		return nil
	}
	if token, ok := r.Context().Value(requestAuthContextKey).(*APIToken); ok {
		return token
	}
	return nil
}

// principal names the caller for audit logs.
func (t *APIToken) principal() string {
	if t == nil {
		return "unknown"
	}
	return t.Name
}

func requestIsAdmin(r *http.Request) bool {
	token := requestAuth(r)
	return token != nil && token.Role == RoleAdmin
}

// generateToken creates a cryptographically random token prefixed with "sv_".
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return tokenPrefix + hex.EncodeToString(b), nil
}

// --- Admin user management ---

// createAdminUser creates a new admin user with bcrypt-hashed password.
func createAdminUser(username, password, role string) (*AdminUser, error) {
	if role != RoleReadonly && role != RoleAdmin {
		return nil, fmt.Errorf("role must be '%s' or '%s'", RoleReadonly, RoleAdmin)
	}
	if username == "" {
		return nil, fmt.Errorf("username required")
	}
	if password == "" {
		return nil, fmt.Errorf("password required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var id int64
	err = localMutation(snapshotAuth, func(tx *sql.Tx) error {
		ts, nid := syncNow()
		result, err := tx.Exec(
			"INSERT INTO admin_users (username, password_hash, role, updated_at, node_id) VALUES (?, ?, ?, ?, ?)",
			username, string(hash), role, ts, nid,
		)
		if err := checkListWrite(result, err, 1); err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, fmt.Errorf("username '%s' already exists", username)
		}
		return nil, err
	}

	return &AdminUser{
		ID:        int(id),
		Username:  username,
		Role:      role,
		CreatedAt: time.Now().Format(time.RFC3339),
	}, nil
}

// listAdminUsers returns all admin users without password hashes.
func listAdminUsers() ([]AdminUser, error) {
	rows, err := db.Query("SELECT id, username, role, created_at FROM admin_users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if users == nil {
		users = []AdminUser{}
	}
	return users, nil
}

// updateAdminUser updates a user's role and optionally their password.
func updateAdminUser(id int, role, password string) error {
	if role != "" && role != RoleReadonly && role != RoleAdmin {
		return fmt.Errorf("role must be '%s' or '%s'", RoleReadonly, RoleAdmin)
	}

	var sets []string
	var args []interface{}
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		sets, args = append(sets, "password_hash = ?"), append(args, string(hash))
	}
	if role != "" {
		sets, args = append(sets, "role = ?"), append(args, role)
	}
	if len(sets) == 0 {
		return nil
	}
	return localMutation(snapshotAuth, func(tx *sql.Tx) error {
		var username, currentRole string
		if err := tx.QueryRow("SELECT username, role FROM admin_users WHERE id = ?", id).Scan(&username, &currentRole); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errUserNotFound
			}
			return err
		}
		ts, nid := syncNow()
		sets, args = append(sets, "updated_at = ?", "node_id = ?"), append(args, ts, nid, id)
		query := "UPDATE admin_users SET " + strings.Join(sets, ", ") + " WHERE id = ?" // #nosec G202 -- Assignment columns are fixed literals above; all values use SQL parameters.
		demotion := currentRole == RoleAdmin && role == RoleReadonly
		if demotion {
			// The admin count is checked in the same statement as the update, so
			// two concurrent demotions cannot both succeed and leave no admin.
			query += " AND (SELECT COUNT(*) FROM admin_users WHERE role = 'admin') > 1"
		}
		res, err := tx.Exec(query, args...)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			if demotion {
				return errDemoteLastAdmin
			}
			return errUserNotFound
		}

		// A credential or privilege change ends the user's existing sessions so a
		// stolen or stale browser session cannot outlive it.
		if _, err := tx.Exec("DELETE FROM sessions WHERE username=?", username); err != nil {
			return err
		}
		var remaining int
		if err := tx.QueryRow("SELECT COUNT(*) FROM sessions WHERE username=?", username).Scan(&remaining); err != nil {
			return err
		}
		if remaining != 0 {
			return fmt.Errorf("sessions not revoked")
		}
		return nil
	})
}

// deleteAdminUser deletes a user by ID and records a tombstone.
func deleteAdminUser(id int) error {
	return localMutation(snapshotAuth, func(tx *sql.Tx) error {
		var username, role string
		if err := tx.QueryRow("SELECT username,role FROM admin_users WHERE id=?", id).Scan(&username, &role); err != nil {
			return err
		}
		if role == RoleAdmin {
			var count int
			if err := tx.QueryRow("SELECT COUNT(*) FROM admin_users WHERE role=?", RoleAdmin).Scan(&count); err != nil {
				return err
			}
			if count <= 1 {
				return errDeleteLastAdmin
			}
		}
		if err := deleteEntityTx(tx, "admin_users", id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM sessions WHERE username=?", username); err != nil {
			return err
		}
		var remaining int
		if err := tx.QueryRow("SELECT COUNT(*) FROM sessions WHERE username=?", username).Scan(&remaining); err != nil {
			return err
		}
		if remaining != 0 {
			return fmt.Errorf("sessions not revoked")
		}
		return nil
	})
}

// getAdminUserByUsername returns a user by username, including the password hash (for auth).
func getAdminUserByUsername(username string) (id int, passwordHash, role, createdAt string, found bool, err error) {
	err = db.QueryRow("SELECT id, password_hash, role, created_at FROM admin_users WHERE username = ?", username).
		Scan(&id, &passwordHash, &role, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return id, passwordHash, role, createdAt, false, nil
	}
	return id, passwordHash, role, createdAt, err == nil, err
}

// --- API token management ---

// createAPIToken generates a new sv_-prefixed token, bcrypt-hashes it, and stores
// only the prefix and hash. Returns the plaintext token once alongside metadata.
func createAPIToken(name, role string) (string, *APIToken, error) {
	if role != RoleReadonly && role != RoleAdmin {
		return "", nil, fmt.Errorf("role must be '%s' or '%s'", RoleReadonly, RoleAdmin)
	}

	plaintext, err := generateToken()
	if err != nil {
		return "", nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", nil, err
	}

	prefix := plaintext[:11] // "sv_" + first 8 hex chars

	var id int64
	err = localMutation(snapshotAuth, func(tx *sql.Tx) error {
		ts, nid := syncNow()
		result, err := tx.Exec(
			"INSERT INTO api_tokens (name, token_prefix, token_hash, token, role, updated_at, node_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
			name, prefix, string(hash), "", role, ts, nid,
		)
		if err := checkListWrite(result, err, 1); err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return "", nil, err
	}

	token := &APIToken{
		ID:          int(id),
		Name:        name,
		TokenPrefix: prefix,
		Role:        role,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}

	return plaintext, token, nil
}

// validateAPIKey checks a plaintext API key against stored tokens.
// Uses an in-memory SHA256 cache to avoid bcrypt (~80ms) on every request.
// Supports both sv_-prefixed and legacy (8-char hex prefix) tokens.
func validateAPIKey(ctx context.Context, apiKey string) (*APIToken, error) {
	if len(apiKey) < 8 {
		return nil, nil
	}
	cacheKey := sha256.Sum256([]byte(apiKey))
	tokenAuthMu.Lock()
	generation := tokenAuthGeneration
	if cached, ok := tokenCache.Load(cacheKey); ok {
		vt := internalValue[*validatedToken](cached)
		if time.Since(vt.cachedAt) < tokenCacheTTL {
			tokenAuthMu.Unlock()
			throttledUpdateLastUsed(vt.token.ID)
			return vt.token, nil
		}
		tokenCache.Delete(cacheKey)
	}
	tokenAuthMu.Unlock()
	// Admission is immediate: malicious cache misses must not queue goroutines
	// or compete with browser logins for more than the shared bcrypt budget.
	release, ok := acquireTokenBcryptSlot(time.Now())
	if !ok {
		return nil, errTokenVerificationBusy
	}
	defer release()
	prefixLen := 8
	if strings.HasPrefix(apiKey, tokenPrefix) && len(apiKey) >= 11 {
		prefixLen = 11
	}
	rows, err := db.QueryContext(ctx,
		"SELECT id, name, token_prefix, token_hash, role, created_at, COALESCE(last_used_at, '') FROM api_tokens WHERE token_prefix = ?", apiKey[:prefixLen])
	if err != nil {
		return nil, err
	}
	type candidate struct {
		token APIToken
		hash  string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.token.ID, &c.token.Name, &c.token.TokenPrefix, &c.hash, &c.token.Role, &c.token.CreatedAt, &c.token.LastUsedAt); err != nil {
			closeQueryRows(rows)
			return nil, err
		}
		candidates = append(candidates, c)
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// SQLite's sole writer connection is free before any expensive comparison.
	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(apiKey)) != nil {
			continue
		}
		tokenAuthMu.Lock()
		if generation != tokenAuthGeneration {
			tokenAuthMu.Unlock()
			return nil, nil
		}
		token := c.token
		tokenCache.Store(cacheKey, &validatedToken{token: &token, cachedAt: time.Now()})
		tokenAuthMu.Unlock()
		throttledUpdateLastUsed(token.ID)
		return &token, nil
	}
	return nil, nil
}

// throttledUpdateLastUsed updates last_used_at at most once per minute per token.
func throttledUpdateLastUsed(tokenID int) {
	now := time.Now()
	if last, ok := lastUsedUpdate.Load(tokenID); ok {
		if now.Sub(internalValue[time.Time](last)) < time.Minute {
			return
		}
	}
	lastUsedUpdate.Store(tokenID, now)
	go func(database *sql.DB) {
		if _, err := database.Exec("UPDATE api_tokens SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", tokenID); err != nil {
			logAuth.Error("update token last-used time failed", "error", err)
		}
	}(db)
}

// listAPITokens returns token metadata only. Plaintext tokens are never returned.
func listAPITokens() ([]APIToken, error) {
	rows, err := db.Query("SELECT id, name, token_prefix, role, created_at, COALESCE(last_used_at, '') FROM api_tokens ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var tokens []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Name, &t.TokenPrefix, &t.Role, &t.CreatedAt, &t.LastUsedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if tokens == nil {
		tokens = []APIToken{}
	}
	return tokens, nil
}

// revokeAPIToken deletes a token by ID and invalidates caches.
func revokeAPIToken(id int) error {
	return localMutation(snapshotAuth, func(tx *sql.Tx) error {
		var prefix string
		if err := tx.QueryRow("SELECT token_prefix FROM api_tokens WHERE id=?", id).Scan(&prefix); err != nil {
			return err
		}
		return deleteEntityTx(tx, "api_tokens", id)
	})
}

// requestIsHTTPS reports whether r should be treated as arriving over HTTPS,
// for deciding the session cookie's Secure flag. True for a direct TLS
// connection (r.TLS != nil), or — only when the operator has opted in via
// TRUST_PROXY_HEADERS (see main.go) — when a reverse proxy set
// X-Forwarded-Proto: https. The header is untrusted by default because any
// client can set it; without the opt-in flag, a TLS-terminating proxy in
// front of svart-dns would otherwise cause the Secure flag to be silently
// dropped on every cookie.
func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return trustProxyHeaders && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// --- Auth middleware ---

// authEnabled returns true if there are admin users in the database.
// API tokens work independently (middleware validates X-Api-Key when present)
// but their existence doesn't gate browser/SPA access.
func authEnabled() bool {
	return hasUsers.Load()
}

// requireAuth validates the API key or session cookie and writes 401 if invalid.
// Returns the authenticated principal and whether the caller should proceed.
// API keys are always validated when present, regardless of authEnabled().
func requireAuth(w http.ResponseWriter, r *http.Request) (*APIToken, bool) {
	// Always validate API key when provided — tokens work independently of admin users
	if apiKey := r.Header.Get("X-Api-Key"); apiKey != "" {
		token, err := validateAPIKey(r.Context(), apiKey)
		if errors.Is(err, errTokenVerificationBusy) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, errTokenVerificationBusy.Error())
			return nil, false
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "API token verification unavailable; retry later")
			return nil, false
		}
		if token != nil {
			return token, true
		}
		writeError(w, http.StatusUnauthorized, "invalid API key")
		return nil, false
	}

	// No API key — browser/session auth requires at least one admin user.
	if !authEnabled() {
		writeError(w, http.StatusUnauthorized, "valid API key or session required")
		return nil, false
	}

	// Fall back to the session cookie (SPA uses this)
	if user := requestSession(r); user != nil {
		return &APIToken{Role: user.Role, Name: user.Username}, true
	}

	writeError(w, http.StatusUnauthorized, "valid API key or session required")
	return nil, false
}

// apiAuth wraps API handlers. GET/HEAD=readonly, POST/PUT/DELETE=admin.
func apiAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := requireAuth(w, r)
		if !ok {
			return
		}
		r = withRequestAuth(r, token)

		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next(w, r)
			return
		}

		if token != nil && token.Role != RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required for mutations")
			return
		}

		next(w, r)
	}
}

// readonlyAuth wraps handlers where even POST is readonly (analysis, policy).
func readonlyAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := requireAuth(w, r)
		if !ok {
			return
		}
		r = withRequestAuth(r, token)
		next(w, r)
	}
}

// adminAuth wraps handlers that always require admin role.
func adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := requireAuth(w, r)
		if !ok {
			return
		}
		r = withRequestAuth(r, token)

		if token != nil && token.Role != RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		next(w, r)
	}
}

// --- Session cookies ---

// handleAPIRevokeSessions godoc
// @Summary Revoke all browser sessions
// @Description Deletes every browser session on this node (sessions are node-local); every browser must log in again. API tokens are unaffected.
// @Tags Auth
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/auth/sessions/revoke [post]
func handleAPIRevokeSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	revoked, err := revokeAllSessions()
	if err != nil {
		logAuth.Error("revoking all sessions failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to revoke sessions; retry, and check the database if it keeps failing")
		return
	}
	logAuth.Warn("all browser sessions revoked", "sessions", revoked, "by", requestAuth(r).principal())

	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

// --- Token management API ---

// handleAPITokensRouter godoc
// @Summary List or create API tokens
// @Description GET returns API token metadata only. POST creates a new API token and returns the plaintext token (shown once).
// @Tags Auth
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object false "Token creation request (POST only) with 'name' (required) and 'role' (readonly or admin, default readonly)"
// @Success 200 {object} apiResponse "Token list (GET)"
// @Success 201 {object} apiResponse "Created token with plaintext key (POST)"
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/tokens [get]
// @Router /api/tokens [post]
func handleAPITokensRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		tokens, err := listAPITokens()
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tokens)

	case http.MethodPost:
		var req CreateAPITokenRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name required")
			return
		}
		if req.Role == "" {
			req.Role = RoleReadonly
		}

		plaintext, token, err := createAPIToken(req.Name, req.Role)
		if err != nil {
			writeDBError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, CreatedAPITokenResponse{Token: plaintext, ID: token.ID, Name: token.Name, Role: token.Role})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPITokenAction godoc
// @Summary Revoke an API token
// @Description Deletes an API token by its ID, permanently revoking access
// @Tags Auth
// @Security ApiKeyAuth
// @Produce json
// @Param id path int true "Token ID"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 404 {object} apiResponse
// @Router /api/tokens/{id} [delete]
func handleAPITokenAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/tokens/")
	id, valid := parsePathID(w, idStr)
	if !valid {
		return
	}

	if err := revokeAPIToken(id); err != nil {
		writeDBLookupError(w, err, "token not found")
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

// --- User management API ---

// handleAPIUsersRouter godoc
// @Summary List or create admin users
// @Description GET returns all admin users (without hashes). POST creates a new admin user.
// @Tags Auth
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body object false "User creation request (POST only) with 'username', 'password', 'role' (default admin)"
// @Success 200 {object} apiResponse "User list (GET)"
// @Success 201 {object} apiResponse "Created user (POST)"
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 409 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/users [get]
// @Router /api/users [post]
func handleAPIUsersRouter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := listAdminUsers()
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, users)

	case http.MethodPost:
		var req CreateAdminUserRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if req.Username == "" {
			writeError(w, http.StatusBadRequest, "username required")
			return
		}
		if req.Password == "" {
			writeError(w, http.StatusBadRequest, "password required")
			return
		}
		if req.Role == "" {
			req.Role = RoleAdmin
		}

		user, err := createAdminUser(req.Username, req.Password, req.Role)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeDBError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, user)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleAPIUserAction godoc
// @Summary Update or delete an admin user
// @Description PUT updates role and/or password. DELETE removes the user (self-deletion prevented).
// @Tags Auth
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param id path int true "User ID"
// @Param request body object false "Update request with optional 'role' and 'password'"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 404 {object} apiResponse
// @Failure 409 {object} apiResponse
// @Router /api/users/{id} [put]
// @Router /api/users/{id} [delete]
func handleAPIUserAction(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/api/users/")
	id, valid := parsePathID(w, idStr)
	if !valid {
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req UpdateAdminUserRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}

		if req.Role == "" && req.Password == "" {
			writeError(w, http.StatusBadRequest, "role or password required")
			return
		}

		if err := updateAdminUser(id, req.Role, req.Password); err != nil {
			switch {
			case errors.Is(err, errUserNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, errDemoteLastAdmin):
				writeError(w, http.StatusConflict, err.Error())
			case strings.HasPrefix(err.Error(), "role must be"):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				writeDBError(w, err)
			}
			return
		}

		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	case http.MethodDelete:
		// Prevent self-deletion: identify the requesting user from the session cookie
		if user := requestSession(r); user != nil && user.ID == id {
			writeError(w, http.StatusConflict, "cannot delete your own account")
			return
		}

		if err := deleteAdminUser(id); err != nil {
			if errors.Is(err, errDeleteLastAdmin) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeDBLookupError(w, err, "user not found")
			return
		}

		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// bootstrapAuth seeds the admin_users table from env vars if empty, and initializes caches.
//
// ADMIN_PASSWORD must be explicitly set to seed an account — there is no
// built-in default password. os.LookupEnv (not getEnv) is used deliberately
// so an unset ADMIN_PASSWORD is distinguishable from one explicitly set to
// an empty string; either way, an empty/unset value skips seeding rather
// than falling back to a well-known credential. This fails closed: with no
// admin user seeded, authEnabled() stays false and requireAuth locks out
// every protected endpoint (see auth.go requireAuth) until an operator sets
// ADMIN_PASSWORD and restarts, or otherwise creates an admin user.
func bootstrapAuth() error {
	userCount, tokenCount, err := refreshAuthCaches()
	if err != nil {
		return err
	}

	// Seed from ADMIN_USER/ADMIN_PASSWORD env vars if no users exist
	if userCount == 0 {
		envUser := getEnv("ADMIN_USER", "admin")
		envPass, passSet := os.LookupEnv("ADMIN_PASSWORD")
		switch {
		case !passSet || envPass == "":
			token, err := startFirstRunSetup()
			if err != nil {
				logAuth.Error("first-run setup unavailable; set ADMIN_PASSWORD and restart", "error", err)
				return err
			}
			url := "http://<this-host>:" + adminPort + "/setup"
			if runningInContainer() {
				if externalIPConfigured {
					url = "http://" + net.JoinHostPort(externalIP, adminPort) + "/setup"
				}
			} else if addrs := dnsServerAddresses(); len(addrs) > 0 {
				url = "http://" + net.JoinHostPort(addrs[0], adminPort) + "/setup"
			}
			// The token is printed on purpose: reading this log is the proof
			// of control over the host. It only works until an administrator
			// exists and is replaced on every start while setup is pending.
			logAuth.Warn("first-run setup: open the admin UI and enter this one-time setup token to create the administrator",
				"setup_url", url, "setup_token", token)
		default:
			if envPass == "svart" {
				logAuth.Warn("ADMIN_PASSWORD is set to the well-known default value " +
					"'svart' — change this immediately; anyone who knows this default " +
					"can log in as admin")
			}
			if _, err := createAdminUser(envUser, envPass, RoleAdmin); err != nil {
				return err
			}
			userCount++
			logAuth.Info("seeded admin user from environment", "username", envUser)
		}
	}

	if hasUsers.Load() || tokenCount > 0 {
		logAuth.Info("auth enabled", "admin_users", userCount, "api_tokens", tokenCount)
	} else {
		logAuth.Warn("auth locked — no admin users and no API tokens are configured")
	}
	return nil
}

// refreshAuthCaches updates derived authentication state after admin users or
// API tokens change (locally or through a sync merge).
type authCounter interface{ QueryRow(string, ...any) *sql.Row }

func readAuthCounts(q authCounter) (users, tokens int, err error) {
	if err = q.QueryRow("SELECT COUNT(*) FROM admin_users").Scan(&users); err != nil {
		return
	}
	err = q.QueryRow("SELECT COUNT(*) FROM api_tokens").Scan(&tokens)
	return
}
func publishAuthCounts(users, tokens int) {
	tokenAuthMu.Lock()
	defer tokenAuthMu.Unlock()
	hasUsers.Store(users > 0)
	hasTokens.Store(tokens > 0)
	tokenAuthGeneration++
	tokenCache.Range(func(key, _ interface{}) bool { tokenCache.Delete(key); return true })
}
func refreshAuthCaches() (userCount, tokenCount int, err error) {
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer rollbackTransaction(tx)
	userCount, tokenCount, err = readAuthCounts(tx)
	if err != nil {
		return
	}
	if err = tx.Commit(); err != nil {
		return
	}
	publishAuthCounts(userCount, tokenCount)
	return
}
