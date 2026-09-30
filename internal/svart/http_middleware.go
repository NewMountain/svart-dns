package svart

import (
	"fmt"
	"github.com/yeti/svart-dns/internal/telemetry"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// newAdminHandler wraps the admin mux in the middleware every admin request
// passes through, outermost first:
//
//   - requestLoggingMiddleware records every request, including rejections.
//   - securityHeaders applies to every response, including errors.
//   - allowedHostsGuard (when ADMIN_ALLOWED_HOSTS is set) refuses unknown Host names.
//   - crossOriginGuard refuses cross-origin state changes before auth runs.
//   - limitRequestBodies caps every body before a handler can read it.
func newAdminHandler(mux http.Handler, hosts hostAllowlist) http.Handler {
	routeFor := func(r *http.Request) string {
		if router, ok := mux.(*http.ServeMux); ok {
			_, pattern := router.Handler(r)
			if pattern != "" {
				return pattern
			}
		}
		return "unmatched"
	}
	return telemetry.HTTP(securityHeaders(allowedHostsGuard(hosts, crossOriginGuard(limitRequestBodies(mux)))), routeFor)
}

// hostAllowlist holds the admin server's permitted Host names (A12). A nil
// list allows every Host, which is the default: DNS rebinding against the
// admin UI still needs a valid session or API key, and CSRF, the Origin
// check and login throttling bound what a rebound page can do. Setting
// ADMIN_ALLOWED_HOSTS removes the remaining surface (e.g. reaching
// unauthenticated endpoints through a victim's browser).
type hostAllowlist []allowedHost

type allowedHost struct {
	name string // lowercased, no trailing dot, no brackets
	port string // empty matches any port
}

// parseAdminAllowedHosts parses ADMIN_ALLOWED_HOSTS: comma-separated host
// names or IP literals, each optionally with :port (IPv6 in brackets).
func parseAdminAllowedHosts(raw string) (hostAllowlist, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list hostAllowlist
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.ContainsAny(entry, "/@?# ") {
			return nil, fmt.Errorf("ADMIN_ALLOWED_HOSTS entry %q is not a host[:port]; list bare names like svart.home.arpa or 192.168.1.2:3000", entry)
		}
		name, port := splitHost(entry)
		if name == "" {
			return nil, fmt.Errorf("ADMIN_ALLOWED_HOSTS entry %q has no host name", entry)
		}
		list = append(list, allowedHost{name: name, port: port})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("ADMIN_ALLOWED_HOSTS is set but lists no hosts; unset it to allow every Host")
	}
	return list, nil
}

// splitHost normalises a Host header value or allowlist entry into a
// lowercased name without brackets or trailing dot, and its port if any.
func splitHost(hostport string) (name, port string) {
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		name, port = h, p
	} else {
		name = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	return strings.TrimSuffix(strings.ToLower(name), "."), port
}

func (l hostAllowlist) allows(host string) bool {
	name, port := splitHost(host)
	for _, a := range l {
		if a.name == name && (a.port == "" || a.port == port) {
			return true
		}
	}
	return false
}

// hostCheckExempt paths are reached by liveness probes and peers that address
// the node by whatever IP or name they were configured with; none of them
// honours an ambient browser credential, so a rebound page gains nothing.
var hostCheckExempt = map[string]bool{
	"/health":                 true,
	"/api/sync":               true,
	"/api/sync/pair/complete": true,
}

func allowedHostsGuard(hosts hostAllowlist, next http.Handler) http.Handler {
	if hosts == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hostCheckExempt[r.URL.Path] || hosts.allows(r.Host) {
			next.ServeHTTP(w, r)
			return
		}
		logAdmin.Warn("rejected request for unlisted Host", "host", r.Host, "path", r.URL.Path, "client_ip", remoteIP(r))
		writeError(w, http.StatusMisdirectedRequest, fmt.Sprintf(
			"host %q is not served here: it is not listed in ADMIN_ALLOWED_HOSTS", r.Host))
	})
}

// crossOriginGuard is the CSRF defence (A2, design/SECURITY-REVIEW-2026-09.md).
// Browsers attach the session cookie to requests another site triggers, so
// every state-changing request must prove it came from this origin and, if it
// has a body, declare it as JSON (an HTML form cannot send application/json
// cross-origin without a CORS preflight, which this server never grants).
//
// Requests carrying X-Api-Key or X-Sync-Key are exempt: a cross-origin page
// cannot set custom headers without that same preflight, so those requests
// come from scripts, peers or curl, not from a victim's browser.
func crossOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || r.Header.Get("X-Api-Key") != "" || r.Header.Get("X-Sync-Key") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if err := checkSameOrigin(r); err != nil {
			logAdmin.Warn("blocked cross-origin request",
				"method", r.Method, "path", r.URL.Path, "origin", r.Header.Get("Origin"),
				"sec_fetch_site", r.Header.Get("Sec-Fetch-Site"), "client_ip", remoteIP(r))
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		if r.ContentLength != 0 && !isJSONContentType(r.Header.Get("Content-Type")) {
			writeError(w, http.StatusUnsupportedMediaType, fmt.Sprintf(
				"unsupported Content-Type %q: request bodies must be sent as Content-Type: application/json",
				r.Header.Get("Content-Type")))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// checkSameOrigin trusts Fetch Metadata when the browser sends it and falls
// back to comparing Origin with Host for older browsers. A request with
// neither header is not from a browser page, so there is no ambient cookie
// being abused.
func checkSameOrigin(r *http.Request) error {
	switch site := r.Header.Get("Sec-Fetch-Site"); site {
	case "same-origin", "none":
		return nil
	case "":
	default:
		return fmt.Errorf("cross-origin request blocked: Sec-Fetch-Site is %q; state-changing requests must come from this admin UI "+
			"(scripts should authenticate with an X-Api-Key token instead of a browser session)", site)
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) {
		return nil
	}
	return fmt.Errorf("cross-origin request blocked: Origin %q does not match this server's host %q; "+
		"state-changing requests must come from this admin UI", origin, r.Host)
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

// remoteIP is the TCP peer address without its port.
func remoteIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// adminContentSecurityPolicy mirrors the SPA's <meta> policy and adds the
// directives a meta tag cannot carry. frame-ancestors only works as a header;
// form-action stops injected markup from posting credentials off-site.
// style-src keeps 'unsafe-inline' because ApexCharts, CodeMirror and
// RapiDoc's shadow DOM inject <style> rules; script-src needs no exception.
const adminContentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

// hstsHeaderValue omits includeSubDomains: svart-dns does not own its
// parent domain's other hosts and must not force HTTPS onto them.
const hstsHeaderValue = "max-age=31536000"

// securityHeaders sets the browser hardening headers on every admin response
// (A7, design/SECURITY-REVIEW-2026-09.md). HSTS is only sent over HTTPS
// (direct TLS, or a trusted proxy's X-Forwarded-Proto), because on plain HTTP
// it is ignored at best.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", adminContentSecurityPolicy)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if requestIsHTTPS(r) {
			h.Set("Strict-Transport-Security", hstsHeaderValue)
		}
		next.ServeHTTP(w, r)
	})
}
