package svart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/yeti/svart-dns/internal/telemetry"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yeti/svart-dns/internal/listparse"
)

// Published list fetching (A8/S4, design/SECURITY-REVIEW-2026-09.md). List
// URLs come from admins, config imports and sync peers, so every fetch is
// treated as an untrusted request: http/https only, no private or special
// destinations unless the operator opts in, bounded size, and content that
// must parse as hostnames or it is counted and rejected.
const (
	listRefreshRequestTimeout        = 5 * time.Minute
	listRefreshResponseHeaderTimeout = 15 * time.Second
	listRefreshDialTimeout           = 5 * time.Second
	listRefreshTLSHandshakeTimeout   = 5 * time.Second
	listRefreshMaxRedirects          = 5

	// Defaults sized for the largest mainstream lists (hosts-format
	// "ultimate" tiers are ~60 MB / ~2M lines) with headroom.
	defaultListMaxDownloadBytes = 256 * 1024 * 1024
	defaultListMaxLines         = 5_000_000
)

// listFetchPolicy is the operator configuration for list fetches.
type listFetchPolicy struct {
	allowPrivate bool
	maxBytes     int64
	maxLines     int
}

// listFetchPolicyFromEnv parses ALLOW_PRIVATE_LIST_URLS,
// LIST_MAX_DOWNLOAD_BYTES and LIST_MAX_LINES. It is read on every refresh
// (refreshes are rare) and validated at startup so a bad value stops the
// server instead of silently falling back.
func listFetchPolicyFromEnv() (listFetchPolicy, error) {
	p := listFetchPolicy{maxBytes: defaultListMaxDownloadBytes, maxLines: defaultListMaxLines}
	if raw := os.Getenv("ALLOW_PRIVATE_LIST_URLS"); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return p, fmt.Errorf("ALLOW_PRIVATE_LIST_URLS must be true or false, got %q", raw)
		}
		p.allowPrivate = v
	}
	if raw := os.Getenv("LIST_MAX_DOWNLOAD_BYTES"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v <= 0 {
			return p, fmt.Errorf("LIST_MAX_DOWNLOAD_BYTES must be a positive number of bytes, got %q", raw)
		}
		p.maxBytes = v
	}
	if raw := os.Getenv("LIST_MAX_LINES"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v <= 0 {
			return p, fmt.Errorf("LIST_MAX_LINES must be a positive number of lines, got %q", raw)
		}
		p.maxLines = v
	}
	return p, nil
}

var (
	cgnatPrefix     = netip.MustParsePrefix("100.64.0.0/10") // carrier-grade NAT; also cloud metadata (100.100.100.200)
	thisNetPrefix   = netip.MustParsePrefix("0.0.0.0/8")     // "this network"
	reservedPrefix  = netip.MustParsePrefix("240.0.0.0/4")   // reserved, includes 255.255.255.255 broadcast
	ietfPrefix      = netip.MustParsePrefix("192.0.0.0/24")  // IETF protocol assignments
	benchmarkPrefix = netip.MustParsePrefix("198.18.0.0/15") // benchmarking
	nat64Prefix     = netip.MustParsePrefix("64:ff9b::/96")  // NAT64: judge the embedded IPv4
	sixToFourPrefix = netip.MustParsePrefix("2002::/16")     // 6to4: judge the embedded IPv4
	teredoPrefix    = netip.MustParsePrefix("2001::/32")     // Teredo tunnels to arbitrary IPv4
	docsV6Prefix    = netip.MustParsePrefix("2001:db8::/32") // documentation
	discardV6Prefix = netip.MustParsePrefix("100::/64")      // discard-only
	docsV4Prefixes  = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24")}
)

// privateDestinationReason names why ip is not a public unicast address,
// or returns "" for an address list fetches may reach by default.
func privateDestinationReason(ip netip.Addr) string {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid():
		return "an invalid address"
	case ip.IsLoopback():
		return "a loopback address"
	case ip.IsUnspecified():
		return "the unspecified address"
	case ip.IsLinkLocalUnicast():
		return "a link-local address (includes cloud metadata at 169.254.169.254)"
	case ip.IsMulticast(), ip.IsLinkLocalMulticast(), ip.IsInterfaceLocalMulticast():
		return "a multicast address"
	case ip.IsPrivate():
		return "a private-network address (RFC 1918 / IPv6 ULA)"
	case cgnatPrefix.Contains(ip):
		return "a carrier-grade NAT address (100.64.0.0/10)"
	case thisNetPrefix.Contains(ip), reservedPrefix.Contains(ip), ietfPrefix.Contains(ip),
		benchmarkPrefix.Contains(ip), docsV6Prefix.Contains(ip), discardV6Prefix.Contains(ip), teredoPrefix.Contains(ip):
		return "a reserved or special-purpose address"
	case nat64Prefix.Contains(ip), sixToFourPrefix.Contains(ip):
		if embedded, ok := embeddedIPv4(ip); ok {
			if reason := privateDestinationReason(embedded); reason != "" {
				return reason + " embedded in an IPv6 translation address"
			}
		}
	}
	for _, p := range docsV4Prefixes {
		if p.Contains(ip) {
			return "a reserved or special-purpose address"
		}
	}
	return ""
}

func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	b := ip.As16()
	switch {
	case nat64Prefix.Contains(ip):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFourPrefix.Contains(ip):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

func privateDestinationError(what string, ip netip.Addr, reason string) error {
	return fmt.Errorf("%s %s is %s; refusing to fetch lists from private or special destinations "+
		"(set ALLOW_PRIVATE_LIST_URLS=true to allow list servers on your own network)", what, ip.Unmap(), reason)
}

// validateListURL checks a list URL before it is stored or fetched. Hostnames
// are only judged at dial time (below), when their resolved address is known.
func validateListURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("list URL is not a valid URL: %v", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("list URL must use http or https, got scheme %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("list URL has no host")
	}
	if allowPrivate {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if reason := privateDestinationReason(ip); reason != "" {
			return privateDestinationError("list URL host", ip, reason)
		}
	}
	if lower := strings.ToLower(strings.TrimSuffix(host, ".")); lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("list URL host %q is a loopback name; refusing to fetch lists from private destinations "+
			"(set ALLOW_PRIVATE_LIST_URLS=true to allow list servers on your own network)", host)
	}
	return nil
}

// validateListURLForAPI rejects a list URL at the create/update/import API
// with 400. An empty URL means "no URL" (a manual list, or an unchanged URL
// on update) and is accepted.
func validateListURLForAPI(w http.ResponseWriter, raw string) bool {
	if raw == "" {
		return true
	}
	policy, err := listFetchPolicyFromEnv()
	if err == nil {
		err = validateListURL(raw, policy.allowPrivate)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

type listFetchPolicyKey struct{}

// guardedListDialContext checks the address actually being connected to,
// after DNS resolution, for every connection including redirect hops. Doing
// it in net.Dialer.Control rather than before the request means a name
// that resolves to a public address during a check and a private one at
// connect time (DNS rebinding) is still refused.
func guardedListDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	policy, ok := ctx.Value(listFetchPolicyKey{}).(listFetchPolicy)
	if !ok {
		policy = listFetchPolicy{}
	}
	d := &net.Dialer{Timeout: listRefreshDialTimeout, KeepAlive: 30 * time.Second}
	if !policy.allowPrivate {
		d.Control = guardListDestination
	}
	return d.DialContext(ctx, network, addr)
}

func guardListDestination(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("list fetch dial address %q: %v", address, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("list fetch dial address %q is not an IP", address)
	}
	if reason := privateDestinationReason(ip); reason != "" {
		return privateDestinationError("list URL resolves to", ip, reason)
	}
	return nil
}

func checkListRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= listRefreshMaxRedirects {
		return fmt.Errorf("list URL redirected more than %d times", listRefreshMaxRedirects)
	}
	if scheme := strings.ToLower(req.URL.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("list URL redirected to scheme %q; only http and https are fetched", req.URL.Scheme)
	}
	return nil
}

// listRefreshHTTPClient fetches published lists. It ignores HTTP(S)_PROXY:
// through a proxy the dial guard would only see the proxy's address, not
// the list server's. Keep-alives are off so every fetch dials (and is
// checked) afresh; refreshes are too rare for connection reuse to matter.
var listRefreshHTTPClient = &http.Client{
	Timeout:       listRefreshRequestTimeout,
	CheckRedirect: checkListRedirect,
	Transport: telemetry.Transport{Operation: telemetry.ListDownload, Base: &http.Transport{
		Proxy:                 nil,
		DialContext:           guardedListDialContext,
		ForceAttemptHTTP2:     true,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   listRefreshTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: listRefreshResponseHeaderTimeout,
	}},
}

// replaceListRules replaces every stored row of one list inside tx with rows
// (rules, plus exceptions for a blocklist) and records the refresh on the list
// row with domainCount, the number of rows that are rules. Rows go through one
// prepared statement (a refresh can store millions), and the first failure
// is returned so the caller rolls back and the previous version stays.
func replaceListRules(tx *sql.Tx, listTable, ruleTable, idColumn string, id int, rows []string, domainCount int) error {
	var previousRows int64
	if err := tx.QueryRow("SELECT count(*) FROM "+ruleTable+" WHERE "+idColumn+" = ?", id).Scan(&previousRows); err != nil {
		return fmt.Errorf("count stored rules: %w", err)
	}
	// #nosec G202 G701 -- Identifiers come only from blockListStore/allowListStore; downloaded domains and IDs are bound parameters.
	result, err := tx.Exec("DELETE FROM "+ruleTable+" WHERE "+idColumn+" = ?", id)
	if err := checkListWrite(result, err, previousRows); err != nil {
		return fmt.Errorf("clear stored rules: %w", err)
	}
	// #nosec G202 G701 -- Identifiers come only from blockListStore/allowListStore; downloaded domains and IDs are bound parameters.
	stmt, err := tx.Prepare("INSERT INTO " + ruleTable + " (" + idColumn + ", domain) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer closeReadResource(stmt)
	for _, d := range rows {
		result, err := stmt.Exec(id, d)
		if err := checkListWrite(result, err, 1); err != nil {
			return fmt.Errorf("store %q: %w", d, err)
		}
	}
	ts, nid := syncNow()
	// #nosec G202 G701 -- Identifiers come only from blockListStore/allowListStore; downloaded domains and IDs are bound parameters.
	result, err = tx.Exec("UPDATE "+listTable+" SET domain_count = ?, last_updated = CURRENT_TIMESTAMP, updated_at = ?, node_id = ? WHERE id = ?", domainCount, ts, nid, id)
	if err := checkListWrite(result, err, 1); err != nil {
		return fmt.Errorf("record refresh: %w", err)
	}
	return nil
}

// fetchList downloads and parses rawURL under the current policy. Any
// failure (policy, network, size, line count, read error) returns an error
// and no partial list, so the caller keeps the previous version.
func fetchList(ctx context.Context, rawURL string) (downloadedList, error) {
	policy, err := listFetchPolicyFromEnv()
	if err != nil {
		return downloadedList{}, err
	}
	if err := validateListURL(rawURL, policy.allowPrivate); err != nil {
		return downloadedList{}, err
	}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, listFetchPolicyKey{}, policy), http.MethodGet, rawURL, nil)
	if err != nil {
		return downloadedList{}, fmt.Errorf("build request: %v", err)
	}
	resp, err := listRefreshHTTPClient.Do(req)
	if err != nil {
		// url.Error embeds the full URL, which may carry credentials.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return downloadedList{}, fmt.Errorf("fetch %s: %w", redactURL(rawURL), err)
	}
	defer closeReadResource(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return downloadedList{}, fmt.Errorf("fetch %s: HTTP %d", redactURL(rawURL), resp.StatusCode)
	}
	if resp.ContentLength > policy.maxBytes {
		return downloadedList{}, listparse.TooLargeError(policy.maxBytes)
	}
	return parseDownloadedList(resp.Body, policy.maxBytes, policy.maxLines)
}

// logListRefreshRejections records every line a refresh did not apply.
func logListRefreshRejections(logger interface {
	Warn(msg string, args ...any)
}, kind string, id int, p listparse.Result) {
	if p.Unsupported == 0 && p.Invalid == 0 && p.ModifiersIgnored == 0 {
		return
	}
	logger.Warn(kind+" refresh skipped lines",
		"id", id, "lines", p.Lines, "applied", len(p.Domains),
		"unsupported_rules", p.Unsupported, "modifiers_ignored", p.ModifiersIgnored, "invalid_lines", p.Invalid, "first_invalid_line_numbers", p.InvalidAt)
}
