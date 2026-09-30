package svart

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Login throttling (A3, design/SECURITY-REVIEW-2026-09.md). Failed logins are
// counted per username and per client network; past a free allowance each
// further failure doubles a lockout (1 s, 2 s, 4 s … capped at 15 min) during
// which even the correct password is refused with 429 + Retry-After. A
// success clears the username's counter; a client's counter decays after 15
// minutes without failures, so one household member's typos cannot unlock an
// attacker sharing the NAT, and a spray across usernames still trips the
// per-client limit.
const (
	loginUserFreeFailures   = 5
	loginClientFreeFailures = 20
	loginBackoffBase        = time.Second
	loginBackoffMax         = 15 * time.Minute
	loginFailureMemory      = 15 * time.Minute
	// loginMaxTrackedKeys bounds limiter memory. When an attacker fills it
	// with distinct failing keys that are all still live, new keys are
	// refused rather than silently untracked (fail closed).
	loginMaxTrackedKeys = 50000
	// loginBcryptConcurrency caps simultaneous bcrypt comparisons (~70 ms of
	// one core each at DefaultCost) so a login flood cannot occupy every CPU
	// the DNS server needs.
	loginBcryptConcurrency = 4
	loginBcryptWait        = 3 * time.Second
)

var errLoginLimiterFull = errors.New("login throttle is tracking too many failing clients")

type loginFailures struct {
	count     int
	lastFail  time.Time
	lockUntil time.Time
}

type loginLimiter struct {
	mu     sync.Mutex
	users  map[string]*loginFailures
	client map[string]*loginFailures
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{users: map[string]*loginFailures{}, client: map[string]*loginFailures{}}
}

var logins = newLoginLimiter()

// retryAfter is how long username/client must wait before another attempt.
func (l *loginLimiter) retryAfter(username, client string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, f := range []*loginFailures{l.users[username], l.client[client]} {
		if f != nil && f.lockUntil.After(now) {
			wait = max(wait, f.lockUntil.Sub(now))
		}
	}
	return wait
}

// recordFailure counts a failed attempt against both keys.
func (l *loginLimiter) recordFailure(username, client string, now time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := l.bump(l.users, username, loginUserFreeFailures, now); err != nil {
		return err
	}
	return l.bump(l.client, client, loginClientFreeFailures, now)
}

func (l *loginLimiter) bump(m map[string]*loginFailures, key string, free int, now time.Time) error {
	f := m[key]
	if f == nil {
		if len(m) >= loginMaxTrackedKeys {
			pruneLoginFailures(m, now)
			if len(m) >= loginMaxTrackedKeys {
				return errLoginLimiterFull
			}
		}
		f = &loginFailures{}
		m[key] = f
	}
	if now.Sub(f.lastFail) > loginFailureMemory && !f.lockUntil.After(now) {
		f.count = 0
	}
	f.count++
	f.lastFail = now
	if f.count >= free {
		f.lockUntil = now.Add(loginBackoff(f.count - free))
	}
	return nil
}

// loginBackoff is 1 s × 2^n, capped at loginBackoffMax.
func loginBackoff(n int) time.Duration {
	if n >= 20 {
		return loginBackoffMax
	}
	return min(loginBackoffBase*time.Duration(math.Pow(2, float64(n))), loginBackoffMax)
}

func pruneLoginFailures(m map[string]*loginFailures, now time.Time) {
	for k, f := range m {
		if !f.lockUntil.After(now) && now.Sub(f.lastFail) > loginFailureMemory {
			delete(m, k)
		}
	}
}

// recordSuccess clears the username's failures. The client counter is left
// to decay so a valid login cannot reset an ongoing spray from that network.
func (l *loginLimiter) recordSuccess(username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.users, username)
}

// loginClientIP is the address logins are attributed to: the TCP peer, or
// with TRUST_PROXY_HEADERS the right-most X-Forwarded-For entry (the one the
// trusted proxy itself appended).
func loginClientIP(r *http.Request) string {
	if trustProxyHeaders {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	return remoteIP(r)
}

// loginClientKey groups IPv6 clients by /64, the smallest block a single
// host is routinely given, so rotating addresses within it does not reset
// the counter.
func loginClientKey(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() != nil {
		return ip
	}
	return parsed.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

var loginBcryptSlots = make(chan struct{}, loginBcryptConcurrency)

// acquireLoginBcryptSlot waits up to loginBcryptWait for a bcrypt slot.
func acquireLoginBcryptSlot(ctx context.Context) (release func(), ok bool) {
	timer := time.NewTimer(loginBcryptWait)
	defer timer.Stop()
	select {
	case loginBcryptSlots <- struct{}{}:
		return func() { <-loginBcryptSlots }, true
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

var (
	dummyPasswordHashOnce sync.Once
	dummyPasswordHash     []byte
)

// dummyBcryptHash is compared against for unknown usernames so that a login
// for a user who does not exist costs the same bcrypt work as a wrong
// password; otherwise response time reveals which usernames exist.
func dummyBcryptHash() []byte {
	dummyPasswordHashOnce.Do(func() {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic("crypto/rand unavailable: " + err.Error())
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(b)), bcrypt.DefaultCost)
		if err != nil {
			panic("bcrypt dummy hash: " + err.Error())
		}
		dummyPasswordHash = hash
	})
	return dummyPasswordHash
}

// Cold API-token authentication has a process-wide budget as well as the
// shared login concurrency cap. No client-controlled keys are retained.
const tokenBcryptBurst = 4
const tokenBcryptPeriod = time.Second

var errTokenVerificationBusy = errors.New("API token verification busy; retry later")

type tokenAdmission struct {
	mu       sync.Mutex
	started  time.Time
	admitted int
}

var tokenVerifications tokenAdmission

func acquireTokenBcryptSlot(now time.Time) (func(), bool) {
	tokenVerifications.mu.Lock()
	defer tokenVerifications.mu.Unlock()
	if now.Sub(tokenVerifications.started) >= tokenBcryptPeriod {
		tokenVerifications.started = now
		tokenVerifications.admitted = 0
	}
	if tokenVerifications.admitted >= tokenBcryptBurst {
		return nil, false
	}
	select {
	case loginBcryptSlots <- struct{}{}:
		tokenVerifications.admitted++
		return func() { <-loginBcryptSlots }, true
	default:
		return nil, false
	}
}
