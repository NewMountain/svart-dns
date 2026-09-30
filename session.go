package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Browser sessions are random server-side records (A4,
// design/SECURITY-REVIEW-2026-09.md). The cookie carries a 256-bit random
// token; the sessions table stores only its SHA-256, so a database read does
// not yield usable cookies. Rows are node-local: sync.go replicates an
// explicit list of tables that does not include sessions, so a login on one
// node is never valid on another and a compromised peer cannot mint sessions.
//
// Session tokens are deliberately not UUIDv7: an identifier that must be
// unguessable cannot embed a timestamp or any structure.
const (
	sessionCookieName = "svart_session"
	// sessionIdleTimeout ends a session nobody has used for a week.
	sessionIdleTimeout = 7 * 24 * time.Hour
	// sessionAbsoluteLifetime ends every session 30 days after login, the
	// lifetime the old HMAC cookie advertised.
	sessionAbsoluteLifetime = 30 * 24 * time.Hour
	// sessionTouchInterval throttles last_seen_at writes to one per minute.
	sessionTouchInterval = time.Minute
	// maxSessionsPerUser bounds rows one account can accumulate; the oldest
	// sessions beyond it are revoked at login.
	maxSessionsPerUser = 50
	// sessionWriteTimeout bounds session bookkeeping writes so a request never
	// waits long behind the single-connection write pool (e.g. during a large
	// list refresh); a skipped touch only shortens the idle window.
	sessionWriteTimeout = 2 * time.Second
	// sessionTimeLayout is fixed-width UTC so stored timestamps sort and
	// compare correctly as text.
	sessionTimeLayout = "2006-01-02T15:04:05.000000Z"
)

// sessionNow is the session clock; tests replace it to exercise expiry.
var sessionNow = time.Now

var errSessionUnavailable = errors.New("session store unavailable")

// newSessionToken returns a cookie token and the hash stored for it.
func newSessionToken() (token, idHash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashSessionToken(token), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// credentialFingerprint ties a session to the password hash it was issued
// under, so a password change from any source (API, config, or a peer's
// sync merge) invalidates the user's sessions without touching sync.go.
func credentialFingerprint(passwordHash string) string {
	sum := sha256.Sum256([]byte("svart-session-credential:" + passwordHash))
	return hex.EncodeToString(sum[:])
}

func formatSessionTime(t time.Time) string { return t.UTC().Format(sessionTimeLayout) }

// createSession stores a new session for username and returns its cookie
// token and absolute expiry.
func createSession(username, passwordHash string) (token string, expires time.Time, err error) {
	token, idHash, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: generate token: %v", errSessionUnavailable, err)
	}
	now := sessionNow()
	expires = now.Add(sessionAbsoluteLifetime)
	ctx, cancel := context.WithTimeout(context.Background(), sessionWriteTimeout)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: %v", errSessionUnavailable, err)
	}
	defer rollbackTransaction(tx)
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM sessions WHERE expires_at < ? OR last_seen_at < ?",
		formatSessionTime(now), formatSessionTime(now.Add(-sessionIdleTimeout))); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: prune: %v", errSessionUnavailable, err)
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO sessions (id_hash, username, credential_fp, created_at, last_seen_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
		idHash, username, credentialFingerprint(passwordHash),
		formatSessionTime(now), formatSessionTime(now), formatSessionTime(expires)); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: insert: %v", errSessionUnavailable, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE username = ? AND id_hash NOT IN (
			SELECT id_hash FROM sessions WHERE username = ? ORDER BY created_at DESC LIMIT ?)`,
		username, username, maxSessionsPerUser); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: cap: %v", errSessionUnavailable, err)
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, fmt.Errorf("%w: commit: %v", errSessionUnavailable, err)
	}
	return token, expires, nil
}

// lookupSession returns the user a session token belongs to, or nil when the
// token is unknown, expired, idle too long, or was issued under a password
// the user no longer has. Failures to read the store also yield nil: an
// unverifiable session is not a session.
func lookupSession(token string) *AdminUser {
	if token == "" {
		return nil
	}
	idHash := hashSessionToken(token)
	queryDB := readDB
	if queryDB == nil {
		queryDB = db
	}
	var fp, lastSeenRaw, expiresRaw, passwordHash string
	var user AdminUser
	err := queryDB.QueryRow(`SELECT s.credential_fp, s.last_seen_at, s.expires_at,
			u.id, u.username, u.password_hash, u.role, u.created_at
		FROM sessions s JOIN admin_users u ON u.username = s.username
		WHERE s.id_hash = ?`, idHash).
		Scan(&fp, &lastSeenRaw, &expiresRaw, &user.ID, &user.Username, &passwordHash, &user.Role, &user.CreatedAt)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			logAuth.Error("session lookup failed; treating request as unauthenticated", "error", err)
		}
		return nil
	}
	lastSeen, errSeen := time.Parse(sessionTimeLayout, lastSeenRaw)
	expires, errExp := time.Parse(sessionTimeLayout, expiresRaw)
	now := sessionNow()
	switch {
	case errSeen != nil || errExp != nil:
		logAuth.Error("session row has unparseable timestamps; revoking it", "last_seen_at", lastSeenRaw, "expires_at", expiresRaw)
	case !now.Before(expires), now.Sub(lastSeen) >= sessionIdleTimeout:
	case fp != credentialFingerprint(passwordHash):
	default:
		if now.Sub(lastSeen) >= sessionTouchInterval {
			execSessionWrite("UPDATE sessions SET last_seen_at = ? WHERE id_hash = ?", formatSessionTime(now), idHash)
		}
		return &user
	}
	execSessionWrite("DELETE FROM sessions WHERE id_hash = ?", idHash)
	return nil
}

// execSessionWrite runs session bookkeeping with a short timeout, logging
// rather than failing the request when the write pool is busy.
func execSessionWrite(query string, args ...interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), sessionWriteTimeout)
	defer cancel()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		logAuth.Warn("session bookkeeping write failed", "error", err)
	}
}

// deleteSession ends the session behind token (logout).
func deleteSession(token string) error {
	if token == "" {
		return nil
	}
	_, err := db.Exec("DELETE FROM sessions WHERE id_hash = ?", hashSessionToken(token))
	return err
}

// revokeAllSessions ends every browser session on this node.
func revokeAllSessions() (int64, error) {
	res, err := db.Exec("DELETE FROM sessions")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// requestSession returns the user behind the request's session cookie.
func requestSession(r *http.Request) *AdminUser {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}
	return lookupSession(cookie.Value)
}

// setSessionCookie issues the session cookie. SameSite=Strict is safe for
// the SPA: its API calls are same-origin fetches, which carry Strict cookies;
// only a navigation arriving from another site loads the (public) SPA shell
// without the cookie, and the shell's own fetches then carry it.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure follows verified TLS or explicitly trusted proxy headers; plain HTTP admin deployments remain supported.
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(expires.Sub(sessionNow()).Seconds()),
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure follows verified TLS or explicitly trusted proxy headers; plain HTTP admin deployments remain supported.
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}
