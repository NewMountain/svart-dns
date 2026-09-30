package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// First-run setup. A fresh install has no administrator; rather than ship a
// default password (anyone on the LAN could take the box) or require an env
// var before the UI is usable, Svart prints a one-time setup token to its log
// and lets whoever can read that log create the first administrator in the
// browser. The token lives only in memory, is regenerated on each start
// while setup is pending, and stops working once an administrator exists.
// Everything after the account (upstreams, lists, who is protected) is done
// by the setup UI through the ordinary API, so it gets the same validation as
// any other client.

const minAdminPasswordLen = 12

var firstRun struct {
	mu        sync.Mutex
	active    bool
	tokenHash [sha256.Size]byte
}

// setupTokenEncoding is standard base32 (A-Z, 2-7) without padding: it has
// no 0/1/8/9 to confuse with letters, and it is accepted in any case.
var setupTokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// startFirstRunSetup arms setup with a fresh token and returns it. Called by
// bootstrapAuth only when no administrator exists and ADMIN_PASSWORD is unset.
func startFirstRunSetup() (string, error) {
	raw := make([]byte, 20) // 160 bits
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate setup token: %w", err)
	}
	enc := setupTokenEncoding.EncodeToString(raw) // 32 characters
	var groups []string
	for i := 0; i < len(enc); i += 4 {
		groups = append(groups, enc[i:i+4])
	}
	token := strings.Join(groups, "-")
	firstRun.mu.Lock()
	firstRun.active = true
	firstRun.tokenHash = sha256.Sum256([]byte(normalizeSetupToken(token)))
	firstRun.mu.Unlock()
	return token, nil
}

// normalizeSetupToken accepts the token with or without dashes, spaces or
// lowercase, the ways people copy it out of a terminal.
func normalizeSetupToken(s string) string {
	return strings.ToUpper(strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.TrimSpace(s)))
}

func setupRequired() bool {
	firstRun.mu.Lock()
	defer firstRun.mu.Unlock()
	return firstRun.active && !hasUsers.Load()
}

// setupStatus is GET /api/setup. While setup is pending it also lists the
// addresses devices should use as their DNS server; afterwards it says only
// that setup is done, so an unauthenticated caller learns nothing new.
type setupStatus struct {
	Required    bool     `json:"required"`
	DNSPort     string   `json:"dns_port,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
	InContainer bool     `json:"in_container,omitempty"`
	MinLength   int      `json:"min_password_length,omitempty"`
}

type setupRequest struct {
	Token    string `json:"token"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAPISetup godoc
// @Summary First-run setup
// @Description GET reports whether first-run setup is pending (and, while it is, the addresses devices should use as their DNS server). POST creates the first administrator using the one-time setup token printed in the server log, and signs that administrator in. Only available until an administrator exists.
// @Tags Auth
// @Accept json
// @Produce json
// @Param setup body setupRequest false "token, username and password (POST)"
// @Success 200 {object} apiResponse
// @Success 201 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 409 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Router /api/setup [get]
// @Router /api/setup [post]
func handleAPISetup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !setupRequired() {
			writeJSON(w, http.StatusOK, setupStatus{Required: false})
			return
		}
		st := setupStatus{
			Required: true,
			// DNS_PUBLISHED_PORT is the port devices use when a container maps
			// host 53 to Svart's unprivileged listener.
			DNSPort:     getEnv("DNS_PUBLISHED_PORT", dnsPort),
			InContainer: runningInContainer(),
			MinLength:   minAdminPasswordLen,
		}
		if st.InContainer {
			// A container only sees its own bridge address, which no device
			// can reach; only an operator-supplied EXTERNAL_IP is worth showing.
			if externalIPConfigured {
				st.Addresses = []string{externalIP}
			}
		} else {
			st.Addresses = dnsServerAddresses()
		}
		writeJSON(w, http.StatusOK, st)
	case http.MethodPost:
		completeSetup(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func completeSetup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !decodeJSONBody(w, r, maxAuthRequestBodyBytes, &req) {
		return
	}
	req.Username = strings.TrimSpace(req.Username)

	clientIP := loginClientIP(r)
	clientKey := loginClientKey(clientIP)
	const throttleKey = "\x00setup" // cannot collide with a real username
	if wait := logins.retryAfter(throttleKey, clientKey, time.Now()); wait > 0 {
		seconds := int(math.Ceil(wait.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many wrong setup tokens; retry in %d seconds", seconds))
		return
	}

	firstRun.mu.Lock()
	defer firstRun.mu.Unlock()
	if !firstRun.active || hasUsers.Load() {
		writeError(w, http.StatusConflict, "setup is already complete; sign in instead")
		return
	}
	got := sha256.Sum256([]byte(normalizeSetupToken(req.Token)))
	if subtle.ConstantTimeCompare(got[:], firstRun.tokenHash[:]) != 1 {
		logAuth.Warn("first-run setup rejected: wrong setup token", "client_ip", clientIP)
		if err := logins.recordFailure(throttleKey, clientKey, time.Now()); err != nil {
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many wrong setup tokens from too many clients; retry in a minute")
			return
		}
		writeError(w, http.StatusUnauthorized, "wrong setup token: copy it from the server log line that starts with \"first-run setup\"")
		return
	}
	if err := validateNewAdmin(req.Username, req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := createAdminUser(req.Username, req.Password, RoleAdmin); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create the administrator: "+err.Error())
		return
	}
	firstRun.active = false
	firstRun.tokenHash = [sha256.Size]byte{}
	logins.recordSuccess(throttleKey)
	logAuth.Info("first-run setup complete", "username", req.Username, "client_ip", clientIP)

	_, passwordHash, role, _, found, err := getAdminUserByUsername(req.Username)
	if err != nil {
		writeDBError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusInternalServerError, "administrator created but could not be read back; sign in manually")
		return
	}
	token, expires, err := createSession(req.Username, passwordHash)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "administrator created, but the session could not be stored; sign in manually")
		return
	}
	setSessionCookie(w, r, token, expires)
	writeJSON(w, http.StatusCreated, AuthenticatedIdentity{Authenticated: true, Role: role, Username: req.Username})
}

func validateNewAdmin(username, password string) error {
	switch {
	case username == "":
		return fmt.Errorf("username is required")
	case len(username) > 64 || strings.ContainsAny(username, " \t\r\n:"):
		return fmt.Errorf("username must be 1-64 characters without spaces or colons")
	case len([]rune(password)) < minAdminPasswordLen:
		return fmt.Errorf("password must be at least %d characters", minAdminPasswordLen)
	case len(password) > 72:
		// bcrypt ignores everything past 72 bytes; refusing is more honest
		// than silently truncating the password.
		return fmt.Errorf("password must be at most 72 bytes")
	}
	return nil
}

// virtualInterfacePrefixes name container and VM bridges: their addresses
// are only reachable from this host, so suggesting them to users is noise.
var virtualInterfacePrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "kube", "podman", "lxcbr", "lxdbr", "vmnet", "vboxnet"}

// dnsServerAddresses lists the unicast addresses of this host that devices
// could use as their DNS server, most useful first: EXTERNAL_IP, then
// private IPv4, then other IPv4, then global IPv6. Loopback, link-local and
// container-bridge addresses are left out because no other device can use them.
func dnsServerAddresses() []string {
	var first, private, public, v6 []string
	if externalIPConfigured {
		first = append(first, externalIP)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return first
	}
	seen := map[string]bool{externalIP: true}
	for ip := range temporaryIPv6Addresses() {
		seen[ip] = true // privacy addresses rotate; a router set to one breaks later
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isVirtualInterface(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			switch {
			case ip.To4() != nil && ip.IsPrivate():
				private = append(private, ip.String())
			case ip.To4() != nil:
				public = append(public, ip.String())
			case ip.IsGlobalUnicast():
				v6 = append(v6, ip.String())
			}
		}
	}
	out := append(first, private...)
	out = append(out, public...)
	return append(out, v6...)
}

// temporaryIPv6Addresses returns the RFC 4941 privacy addresses Linux reports
// in /proc/net/if_inet6 (flag IFA_F_TEMPORARY). Elsewhere it returns none.
func temporaryIPv6Addresses() map[string]bool {
	raw, err := os.ReadFile("/proc/net/if_inet6")
	if err != nil {
		return nil
	}
	return parseTemporaryIPv6(string(raw))
}

func parseTemporaryIPv6(procIfInet6 string) map[string]bool {
	const ifaFTemporary = 0x01
	out := map[string]bool{}
	for _, line := range strings.Split(procIfInet6, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || len(f[0]) != 32 {
			continue
		}
		flags, err := strconv.ParseUint(f[4], 16, 32)
		if err != nil || flags&ifaFTemporary == 0 {
			continue
		}
		b, err := hex.DecodeString(f[0])
		if err != nil {
			continue
		}
		out[net.IP(b).String()] = true
	}
	return out
}

func runningInContainer() bool {
	return fileExists("/.dockerenv") || fileExists("/run/.containerenv")
}

func isVirtualInterface(name string) bool {
	for _, p := range virtualInterfacePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
