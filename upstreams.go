package main

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/miekg/dns"
)

type Upstream struct {
	ID       int    `json:"id"`
	Upstream string `json:"upstream"`
	Enabled  bool   `json:"enabled"`
}

// upstreamStore holds one immutable snapshot of the configured upstreams
// and the routing derived from them, swapped atomically on every change so
// the query path never locks.
type upstreamStore struct {
	snap atomic.Pointer[upstreamSnapshot]
}

// upstreamSnapshot is the routing view of the configured upstreams.
// Conditional forwarding follows dnsmasq/AdGuard Home: an entry written
// "[/lan/home.arpa/]192.168.1.1" answers only for those domains and their
// subdomains, the most specific configured domain wins, and every other
// name goes to the unrestricted entries (see DD-032).
type upstreamSnapshot struct {
	all      []Upstream            // as configured, for the API
	defaults []Upstream            // enabled, valid, unrestricted
	routes   map[string][]Upstream // lowercase FQDN -> enabled upstreams restricted to it
	builtin  bool                  // nothing configured: defaults is builtinUpstreams
}

// builtinUpstreams answer while no upstream is configured at all, so a fresh
// install resolves names before (or without) the setup page. It lives in code
// rather than being seeded into the database so a new node joining a sync
// mesh cannot overwrite the mesh's upstreams with it. Configuring any
// upstream, even a disabled one, replaces it.
var builtinUpstreams = []Upstream{{Upstream: "https://dns.quad9.net/dns-query", Enabled: true}}

var upstreamStorage = &upstreamStore{}

func init() {
	upstreamStorage.snap.Store(buildUpstreamSnapshot(nil))
}

// upstreamSpec is a parsed upstream entry.
type upstreamSpec struct {
	domains  []string // lowercase FQDNs the entry is restricted to; empty = unrestricted
	protocol string   // udp, tcp, tls or https
	address  string   // as returned by parseUpstreamAddress
}

// parseUpstreamSpec validates an upstream entry: an optional
// "[/domain/.../]" prefix followed by host[:port], udp://, tcp://, tls://
// or https:// forms.
func parseUpstreamSpec(raw string) (upstreamSpec, error) {
	s := strings.TrimSpace(raw)
	var spec upstreamSpec
	if strings.HasPrefix(s, "[/") {
		end := strings.Index(s, "/]")
		if end < 0 {
			return upstreamSpec{}, fmt.Errorf("upstream %q: unterminated [/domain/] prefix", raw)
		}
		for _, d := range strings.Split(s[2:end], "/") {
			d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
			if _, ok := dns.IsDomainName(d); d == "" || !ok || strings.ContainsAny(d, " \t") {
				return upstreamSpec{}, fmt.Errorf("upstream %q: invalid domain %q in [/domain/] prefix", raw, d)
			}
			spec.domains = append(spec.domains, dns.Fqdn(d))
		}
		s = strings.TrimSpace(s[end+2:])
	}
	if s == "" || strings.HasPrefix(s, "#") {
		return upstreamSpec{}, fmt.Errorf("upstream %q: no server address", raw)
	}

	spec.protocol, spec.address = parseUpstreamAddress(s)
	switch spec.protocol {
	case "udp", "tcp", "tls":
		if err := validateHostPort(withDefaultPort(spec.address, "53")); err != nil {
			return upstreamSpec{}, fmt.Errorf("upstream %q: %w", raw, err)
		}
	case "https":
		u, err := url.Parse("https://" + spec.address)
		if err != nil {
			return upstreamSpec{}, fmt.Errorf("upstream %q: %w", raw, err)
		}
		if err := validateHostPort(withDefaultPort(u.Host, "443")); err != nil {
			return upstreamSpec{}, fmt.Errorf("upstream %q: %w", raw, err)
		}
	default:
		return upstreamSpec{}, fmt.Errorf("upstream %q: unsupported protocol %q (want udp, tcp, tls or https)", raw, spec.protocol)
	}
	return spec, nil
}

func validateHostPort(hostport string) error {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return err
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port %q", port)
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if _, ok := dns.IsDomainName(host); host == "" || !ok || strings.ContainsAny(host, " \t/") {
		return fmt.Errorf("invalid host %q", host)
	}
	return nil
}

// buildUpstreamSnapshot derives routing from the configured list. Entries
// that do not parse (they can arrive through config import or sync, which
// bypass API validation) are left out of routing rather than tried and
// failed on every query.
func buildUpstreamSnapshot(list []Upstream) *upstreamSnapshot {
	snap := &upstreamSnapshot{all: list}
	for _, u := range list {
		spec, err := parseUpstreamSpec(u.Upstream)
		if err != nil {
			if logResolver != nil {
				logResolver.Warn("ignoring invalid upstream", "upstream", u.Upstream, "error", err)
			}
			continue
		}
		if !u.Enabled {
			continue
		}
		if len(spec.domains) == 0 {
			snap.defaults = append(snap.defaults, u)
			continue
		}
		if snap.routes == nil {
			snap.routes = make(map[string][]Upstream)
		}
		for _, d := range spec.domains {
			snap.routes[d] = append(snap.routes[d], u)
		}
	}
	if len(list) == 0 {
		snap.defaults = builtinUpstreams
		snap.builtin = true
	}
	return snap
}

// usingBuiltinUpstreams reports whether queries go to builtinUpstreams.
func (s *upstreamStore) usingBuiltinUpstreams() bool { return s.snap.Load().builtin }

// forQuery returns the enabled upstreams that may answer name. The slice
// is shared and must not be modified.
func (s *upstreamStore) forQuery(name string) []Upstream {
	snap := s.snap.Load()
	if len(snap.routes) == 0 {
		return snap.defaults
	}
	// Walk the name's suffixes from most to least specific, skipping the
	// root. ToLower does not allocate for the common lowercase name.
	name = strings.ToLower(name)
	for i := 0; i < len(name); {
		if ups, ok := snap.routes[name[i:]]; ok {
			return ups
		}
		dot := strings.IndexByte(name[i:], '.')
		if dot < 0 {
			break
		}
		i += dot + 1
	}
	return snap.defaults
}

func readUpstreams(q queryer) ([]Upstream, error) {
	rows, err := q.Query("SELECT id, upstream, enabled FROM upstreams ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var upstreams []Upstream
	for rows.Next() {
		var u Upstream
		if err := rows.Scan(&u.ID, &u.Upstream, &u.Enabled); err != nil {
			return nil, err
		}
		upstreams = append(upstreams, u)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return upstreams, nil
}

func loadUpstreamsFromDB() error {
	upstreams, err := readUpstreams(db)
	if err != nil {
		return err
	}
	upstreamStorage.publish(upstreams)
	if upstreamStorage.usingBuiltinUpstreams() && logResolver != nil {
		logResolver.Warn("no upstream resolvers configured; answering through the built-in default until one is added",
			"builtin", builtinUpstreams[0].Upstream)
	}
	logDB.Info("loaded upstreams", "count", len(upstreams))
	return nil
}

func (s *upstreamStore) getAll() []Upstream {
	return s.snap.Load().all
}

// publish is the only way the upstream set changes. Pooled upstream
// connections are dropped only on a real change: sync merges reload the
// list on every poll, and resetting then would defeat connection reuse.
func (s *upstreamStore) publish(list []Upstream) {
	old := s.getAll()
	if slices.Equal(old, list) {
		return
	}
	s.snap.Store(buildUpstreamSnapshot(list))
	resetUpstreamTransports()
}

func (s *upstreamStore) add(upstream string, enabled bool) (Upstream, error) {
	ts, nid := syncNow()
	result, err := db.Exec("INSERT INTO upstreams (upstream, enabled, updated_at, node_id) VALUES (?, ?, ?, ?)", upstream, enabled, ts, nid)
	if err != nil {
		return Upstream{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Upstream{}, err
	}

	u := Upstream{
		ID:       int(id),
		Upstream: upstream,
		Enabled:  enabled,
	}

	current := s.getAll()
	newList := make([]Upstream, len(current)+1)
	copy(newList, current)
	newList[len(current)] = u
	s.publish(newList)

	return u, nil
}

func (s *upstreamStore) update(id int, upstream string) error {
	ts, nid := syncNow()
	_, err := db.Exec("UPDATE upstreams SET upstream = ?, updated_at = ?, node_id = ? WHERE id = ?", upstream, ts, nid, id)
	if err != nil {
		return err
	}

	current := s.getAll()
	newList := make([]Upstream, len(current))
	copy(newList, current)

	for i := range newList {
		if newList[i].ID == id {
			newList[i].Upstream = upstream
			s.publish(newList)
			return nil
		}
	}

	return sql.ErrNoRows
}

func (s *upstreamStore) toggle(id int) error {
	current := s.getAll()
	var newEnabled bool
	found := false

	for _, u := range current {
		if u.ID == id {
			newEnabled = !u.Enabled
			found = true
			break
		}
	}

	if !found {
		return sql.ErrNoRows
	}

	ts, nid := syncNow()
	_, err := db.Exec("UPDATE upstreams SET enabled = ?, updated_at = ?, node_id = ? WHERE id = ?", newEnabled, ts, nid, id)
	if err != nil {
		return err
	}

	newList := make([]Upstream, len(current))
	copy(newList, current)

	for i := range newList {
		if newList[i].ID == id {
			newList[i].Enabled = newEnabled
			break
		}
	}
	s.publish(newList)

	return nil
}

func (s *upstreamStore) delete(id int) error {
	if err := deleteLocalEntity("upstreams", id, snapshotNone); err != nil {
		return err
	}

	current := s.getAll()
	for i, u := range current {
		if u.ID == id {
			newList := make([]Upstream, len(current)-1)
			copy(newList, current[:i])
			copy(newList[i:], current[i+1:])
			s.publish(newList)
			return nil
		}
	}

	return nil
}

// handleAPIUpstreams godoc
// @Summary List or create upstream DNS servers
// @Description GET returns all configured upstream DNS servers. POST adds a new upstream server.
// @Tags Upstreams
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param upstream body object false "Upstream server (POST only) with 'upstream' (address) and 'enabled' fields"
// @Success 200 {object} apiResponse "Upstream list (GET)"
// @Success 201 {object} apiResponse "Created upstream (POST)"
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 403 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/upstreams [get]
// @Router /api/upstreams [post]
func handleAPIUpstreams(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		upstreams := upstreamStorage.getAll()
		if !requestIsAdmin(r) {
			redacted := make([]Upstream, len(upstreams))
			for i, u := range upstreams {
				u.Upstream = redactURL(u.Upstream)
				redacted[i] = u
			}
			upstreams = redacted
		}
		writeJSON(w, http.StatusOK, upstreams)

	case "POST":
		var req CreateUpstreamRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}
		if _, err := parseUpstreamSpec(req.Upstream); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		upstream, err := upstreamStorage.add(req.Upstream, req.Enabled)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, upstream)

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleAPIUpstreamAction(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path[len("/api/upstreams/"):]
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		writeError(w, http.StatusBadRequest, "Invalid path")
		return
	}

	id, valid := parsePathID(w, parts[0])
	if !valid {
		return
	}

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case "DELETE":
		if err := upstreamStorage.delete(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	case "POST":
		if action == "toggle" {
			if err := upstreamStorage.toggle(id); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		} else {
			writeError(w, http.StatusBadRequest, "Invalid action")
		}

	case "PUT":
		var req UpdateUpstreamRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}
		if _, err := parseUpstreamSpec(req.Upstream); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := upstreamStorage.update(id, req.Upstream); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})

	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
