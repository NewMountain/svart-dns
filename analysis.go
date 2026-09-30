package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/yeti/svart-dns/internal/policycore"
)

// --- Request budgets ---

// Analysis endpoints are readonly-accessible and run in the process that
// serves DNS, so each request's CPU and memory are bounded before any work
// starts (A1, design/SECURITY-REVIEW-2026-09.md). A request over budget is
// refused with its numbers; it is never silently cut down to a subset.
const (
	// maxDNSNameLength is the RFC 1035 limit on a presentation-format name
	// without the trailing dot.
	maxDNSNameLength   = 253
	maxAnalysisLists   = 64
	maxAnalysisDomains = 10000
	// maxMatrixWorkUnits bounds domains × (lists + their complex wildcard
	// patterns). Each unit is one list lookup on a probed name, so the worst
	// accepted matrix costs well under a second of one core. 10,000 ranked
	// domains against 25 plain lists fits.
	maxMatrixWorkUnits = 250_000
	// maxCompareLoadedDomains bounds the raw domain sets loaded from SQLite
	// for one comparison (~100 bytes each in memory, so ~400 MB worst case).
	maxCompareLoadedDomains = 4_000_000
	// maxComparePairWork bounds the pairwise overlap scan: the sum over all
	// list pairs of the smaller list's size.
	maxComparePairWork = 40_000_000
	// analysisConcurrency caps simultaneous heavy analysis requests; the
	// budgets above are per request, so this bounds their sum.
	analysisConcurrency = 2
)

var analysisSlots = make(chan struct{}, analysisConcurrency)

// acquireAnalysisSlot claims one of the analysisConcurrency slots without
// waiting. When all are busy it writes 429 and returns ok=false.
func acquireAnalysisSlot(w http.ResponseWriter) (release func(), ok bool) {
	select {
	case analysisSlots <- struct{}{}:
		return func() { <-analysisSlots }, true
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf(
			"analysis capacity busy: %d analysis requests are already running; retry in a second", analysisConcurrency))
		return nil, false
	}
}

// resolveAnalysisLists removes duplicate ids (keeping first-seen order) and
// rejects ids that are not enabled blocklists in ix or more than
// maxAnalysisLists distinct lists.
func resolveAnalysisLists(ids []int, ix *policycore.Index) ([]int, error) {
	seen := make(map[int]struct{}, len(ids))
	unique := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if ix.SlotOf(id, false) < 0 {
			return nil, fmt.Errorf("unknown blocklist_id: %d (not an enabled blocklist)", id)
		}
		unique = append(unique, id)
	}
	if len(unique) > maxAnalysisLists {
		return nil, fmt.Errorf("too many blocklists: %d distinct lists requested, at most %d per request", len(unique), maxAnalysisLists)
	}
	return unique, nil
}

// normalizeAnalysisDomains lowercases and trims each domain and rejects
// empty or over-long names instead of evaluating them.
func normalizeAnalysisDomains(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("domains list required")
	}
	if len(raw) > maxAnalysisDomains {
		return nil, fmt.Errorf("too many domains: %d requested, maximum %d per request", len(raw), maxAnalysisDomains)
	}
	domains := make([]string, len(raw))
	for i, d := range raw {
		d = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(d), "."))
		if d == "" {
			return nil, fmt.Errorf("domains[%d] is empty", i)
		}
		if len(d) > maxDNSNameLength {
			return nil, fmt.Errorf("domains[%d] is %d bytes; DNS names are at most %d", i, len(d), maxDNSNameLength)
		}
		domains[i] = d
	}
	return domains, nil
}

// matrixWorkUnits is domains × Σ(1 + complex wildcard patterns) over lists,
// given each list's number of complex patterns.
func matrixWorkUnits(domainCount int, complexPerList []int) int {
	perDomain := 0
	for _, n := range complexPerList {
		perDomain += 1 + n
	}
	return domainCount * perDomain
}

// compareWork returns the domains a comparison loads and its pairwise scan
// cost, from the in-memory list sizes, before anything is loaded.
func compareWork(sizes []int) (loaded, pairWork int) {
	for i, a := range sizes {
		loaded += a
		for _, b := range sizes[i+1:] {
			pairWork += min(a, b)
		}
	}
	return loaded, pairWork
}

func checkCompareBudget(ids []int, ix *policycore.Index) error {
	sizes := make([]int, len(ids))
	for i, id := range ids {
		sizes[i] = ix.Lists[ix.SlotOf(id, false)].Count
	}
	domains, pairWork := compareWork(sizes)
	if domains > maxCompareLoadedDomains {
		return fmt.Errorf("analysis budget exceeded: the selected lists hold %d domains, limit %d per request; compare fewer or smaller lists", domains, maxCompareLoadedDomains)
	}
	if pairWork > maxComparePairWork {
		return fmt.Errorf("analysis budget exceeded: pairwise overlap needs %d membership checks, limit %d per request; compare fewer lists", pairWork, maxComparePairWork)
	}
	return nil
}

// --- 3.2: Blocklist comparison ---

type compareRequest struct {
	BlocklistIDs []int `json:"blocklist_ids"`
}

type compareListInfo struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
}

type compareResponse struct {
	Lists       []compareListInfo `json:"lists"`
	Overlap     [][]int           `json:"overlap"`
	UniqueCount []int             `json:"unique_count"`
	UnionSize   int               `json:"union_size"`
}

// handleAPIAnalysisCompare godoc
// @Summary Compare blocklists pairwise
// @Description Computes pairwise domain overlap between 2 to 64 distinct enabled blocklists (duplicate IDs are ignored), returning overlap matrix, unique counts, and union size. Requests whose lists exceed the per-request work budget are rejected with 400; 429 when the analysis capacity is busy.
// @Tags Analysis
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body compareRequest true "List of blocklist IDs to compare (minimum 2 distinct)"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 413 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Router /api/analysis/compare [post]
func handleAPIAnalysisCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req compareRequest
	if !decodeJSONBody(w, r, maxAnalysisRequestBodyBytes, &req) {
		return
	}

	ix := policyState.Load().Index
	ids, err := resolveAnalysisLists(req.BlocklistIDs, ix)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(ids) < 2 {
		writeError(w, http.StatusBadRequest, "at least 2 distinct blocklist_ids required")
		return
	}
	if err := checkCompareBudget(ids, ix); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	release, ok := acquireAnalysisSlot(w)
	if !ok {
		return
	}
	defer release()

	n := len(ids)
	slots := make([]int, n)
	lists := make([]compareListInfo, n)
	for i, id := range ids {
		slots[i] = ix.SlotOf(id, false)
		l := ix.Lists[slots[i]]
		lists[i] = compareListInfo{ID: id, Alias: l.Name, DomainCount: l.Count}
	}
	overlap := make([][]int, n)
	for i := range overlap {
		overlap[i] = make([]int, n)
	}
	uniqueCount := make([]int, n)
	union := 0
	for _, c := range ix.SpreadOf(slots) {
		union += c.Rules
		if len(c.Positions) == 1 {
			uniqueCount[c.Positions[0]] += c.Rules
		}
		for _, a := range c.Positions {
			for _, b := range c.Positions {
				overlap[a][b] += c.Rules
			}
		}
	}

	writeJSON(w, http.StatusOK, compareResponse{
		Lists:       lists,
		Overlap:     overlap,
		UniqueCount: uniqueCount,
		UnionSize:   union,
	})
}

// --- 3.1: Domain × List matrix ---

type matrixRequest struct {
	Type         string   `json:"type,omitempty"`
	Domains      []string `json:"domains"`
	BlocklistIDs []int    `json:"blocklist_ids"` // optional — defaults to all enabled
}

type matrixListInfo struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
}

type matrixResponse struct {
	RecordType   uint16           `json:"record_type"`
	Lists        []matrixListInfo `json:"lists"`
	Domains      []string         `json:"domains"`
	Matrix       [][]bool         `json:"matrix"`
	MatchedRules [][]*string      `json:"matched_rules"`
}

// handleAPIAnalysisMatrix godoc
// @Summary Domain x blocklist membership matrix
// @Description Returns a matrix showing which domains are blocked by which blocklists, with matched rule details. At most 10,000 domains and 64 distinct enabled lists per request, and at most 250,000 work units (domains x (lists + their complex wildcard patterns)); larger requests are rejected with 400, never truncated. 429 when the analysis capacity is busy.
// @Tags Analysis
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body matrixRequest true "Domains to check and optional blocklist IDs (defaults to all enabled)"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 413 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Router /api/analysis/matrix [post]
func handleAPIAnalysisMatrix(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req matrixRequest
	if !decodeJSONBody(w, r, maxAnalysisRequestBodyBytes, &req) {
		return
	}

	rrtype, err := parsePolicyRecordType(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domains, err := normalizeAnalysisDomains(req.Domains)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ix := policyState.Load().Index
	requested := req.BlocklistIDs
	if len(requested) == 0 {
		for _, l := range ix.Lists {
			if !l.Allow {
				requested = append(requested, l.ID)
			}
		}
	}
	listIDs, err := resolveAnalysisLists(requested, ix)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Smallest list first (slot order), so the matrix reads most-specific
	// to broadest.
	slots := make([]int, len(listIDs))
	for i, id := range listIDs {
		slots[i] = ix.SlotOf(id, false)
	}
	slices.Sort(slots)
	lists := make([]matrixListInfo, len(slots))
	complexPerList := make([]int, len(slots))
	for i, slot := range slots {
		l := ix.Lists[slot]
		lists[i] = matrixListInfo{ID: l.ID, Alias: l.Name, DomainCount: l.Count}
		complexPerList[i] = ix.ComplexCount(slot)
	}

	if units := matrixWorkUnits(len(domains), complexPerList); units > maxMatrixWorkUnits {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"analysis budget exceeded: %d domains x %d lists = %d work units (complex wildcard patterns count extra), limit %d per request; send fewer domains or lists",
			len(domains), len(slots), units, maxMatrixWorkUnits))
		return
	}

	release, ok := acquireAnalysisSlot(w)
	if !ok {
		return
	}
	defer release()

	ctx := r.Context()
	matrix := make([][]bool, len(domains))
	matchedRules := make([][]*string, len(domains))
	var p policycore.Probe
	for i, domain := range domains {
		if ctx.Err() != nil {
			return
		}
		matrix[i] = make([]bool, len(slots))
		matchedRules[i] = make([]*string, len(slots))
		ix.ProbeDomain(&p, domain, "", rrtype)
		for j, slot := range slots {
			if rule := ix.RuleIn(&p, slot); rule != "" {
				matrix[i][j] = true
				matchedRules[i][j] = &rule
			}
		}
	}

	writeJSON(w, http.StatusOK, matrixResponse{
		RecordType:   rrtype,
		Lists:        lists,
		Domains:      domains,
		Matrix:       matrix,
		MatchedRules: matchedRules,
	})
}

// --- 3.3: Policy simulator ---

type simulateRequest struct {
	Type     string   `json:"type,omitempty"`
	ClientIP string   `json:"client_ip"`
	Domains  []string `json:"domains"`
}

type simulateResponse struct {
	ClientIP string                     `json:"client_ip"`
	Results  []*policycore.PolicyResult `json:"results"`
}

// handleAPIAnalysisSimulate godoc
// @Summary Simulate policy evaluation for a batch of domains
// @Description Runs the full three-tier policy evaluation (Range, Group, IP) for each domain against a specific client IP. Maximum 10,000 domains per request; 429 when the analysis capacity is busy.
// @Tags Analysis
// @Security ApiKeyAuth
// @Accept json
// @Produce json
// @Param request body simulateRequest true "Client IP and list of domains to simulate"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 413 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Router /api/analysis/simulate [post]
func handleAPIAnalysisSimulate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req simulateRequest
	if !decodeJSONBody(w, r, maxAnalysisRequestBodyBytes, &req) {
		return
	}

	if req.ClientIP == "" {
		writeError(w, http.StatusBadRequest, "client_ip required")
		return
	}
	rrtype, err := parsePolicyRecordType(req.Type)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	domains, err := normalizeAnalysisDomains(req.Domains)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	release, ok := acquireAnalysisSlot(w)
	if !ok {
		return
	}
	defer release()

	ctx := r.Context()
	results := make([]*policycore.PolicyResult, len(domains))
	for i, domain := range domains {
		if ctx.Err() != nil {
			return
		}
		// Use evaluatePolicyFull directly — bypass cache for fresh state
		results[i] = evaluatePolicyFull(req.ClientIP, domain, rrtype)
	}

	writeJSON(w, http.StatusOK, simulateResponse{
		ClientIP: req.ClientIP,
		Results:  results,
	})
}

// --- 3.4: Domain prefill sources ---

type domainsResponse struct {
	Source  string   `json:"source"`
	Count   int      `json:"count"`
	Domains []string `json:"domains"`
}

// handleAPIAnalysisDomains godoc
// @Summary Get domain prefill sources
// @Description Returns domain lists from various sources for use in analysis tools. Sources: top-sites (operator-provided ranking CSV), top-queried, top-blocked, top-allowed (from query logs, last 7 days).
// @Tags Analysis
// @Security ApiKeyAuth
// @Produce json
// @Param source query string true "Domain source: top-sites, top-queried, top-blocked, or top-allowed"
// @Param limit query int false "Maximum number of domains to return (1-10000, default 1000)"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 429 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Failure 503 {object} apiResponse
// @Router /api/analysis/domains [get]
func handleAPIAnalysisDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	source := r.URL.Query().Get("source")
	if source == "" {
		writeError(w, http.StatusBadRequest, "source parameter required (top-sites, top-queried, top-blocked, top-allowed)")
		return
	}

	limit, err := intQueryParam(r.URL.Query(), "limit", 1000, 1, 10000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var domains []string

	switch source {
	case "top-sites":
		release, ok := acquireAnalysisSlot(w)
		if !ok {
			return
		}
		defer release()
		domains, err = getTopSites(os.Getenv("TOP_SITES_PATH"), limit)
		if err != nil {
			slog.Warn("analysis top-sites unavailable", "error", err)
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}

	case "top-queried":
		domains, err = getTopQueriedDomains(limit)
		if err != nil {
			writeDBError(w, err)
			return
		}

	case "top-blocked":
		domains, err = getTopBlockedDomains(limit)
		if err != nil {
			writeDBError(w, err)
			return
		}

	case "top-allowed":
		domains, err = getTopAllowedDomains(limit)
		if err != nil {
			writeDBError(w, err)
			return
		}

	case "client-history":
		clientIP := r.URL.Query().Get("client_ip")
		if clientIP == "" {
			writeError(w, http.StatusBadRequest, "client_ip required for client-history source")
			return
		}
		window := r.URL.Query().Get("window")
		domains, err = getClientHistoryDomains(clientIP, window, limit)
		if err != nil {
			writeDBError(w, err)
			return
		}

	default:
		writeError(w, http.StatusBadRequest, "invalid source: must be top-sites, top-queried, top-blocked, top-allowed, or client-history")
		return
	}

	writeJSON(w, http.StatusOK, domainsResponse{
		Source:  source,
		Count:   len(domains),
		Domains: domains,
	})
}

func getTopQueriedDomains(limit int) ([]string, error) {
	rows, err := readDB.Query(`
		SELECT query_name, SUM(coalesced_count) as cnt FROM query_logs
		WHERE timestamp >= datetime('now', '-7 days')
		GROUP BY query_name ORDER BY cnt DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var name string
		var cnt int
		if err := rows.Scan(&name, &cnt); err != nil {
			return nil, err
		}
		// Strip trailing dot from query_name
		domains = append(domains, strings.TrimSuffix(name, "."))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

func getTopBlockedDomains(limit int) ([]string, error) {
	rows, err := readDB.Query(`
		SELECT query_name, SUM(coalesced_count) as cnt FROM query_logs
		WHERE timestamp >= datetime('now', '-7 days') AND blocked = 1
		GROUP BY query_name ORDER BY cnt DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var name string
		var cnt int
		if err := rows.Scan(&name, &cnt); err != nil {
			return nil, err
		}
		domains = append(domains, strings.TrimSuffix(name, "."))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

func getTopAllowedDomains(limit int) ([]string, error) {
	rows, err := readDB.Query(`
		SELECT query_name, SUM(coalesced_count) as cnt FROM query_logs
		WHERE timestamp >= datetime('now', '-7 days') AND blocked = 0
		GROUP BY query_name ORDER BY cnt DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var name string
		var cnt int
		if err := rows.Scan(&name, &cnt); err != nil {
			return nil, err
		}
		domains = append(domains, strings.TrimSuffix(name, "."))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

func getClientHistoryDomains(clientIP, window string, limit int) ([]string, error) {
	if window == "30d" {
		// Validate clientIP to prevent SQL injection (DuckDB path uses string interpolation)
		if net.ParseIP(clientIP) == nil {
			return nil, fmt.Errorf("invalid client IP: %s", clientIP)
		}
		sql := fmt.Sprintf(
			"SELECT RTRIM(query_name, '.') as domain, SUM(coalesced_count) as cnt FROM query_logs WHERE client_ip = '%s' AND timestamp >= NOW() - INTERVAL 30 DAY GROUP BY 1 ORDER BY cnt DESC LIMIT %d",
			clientIP, limit)
		cols, rows, _, err := investigateQuery(sql, 30)
		if err != nil {
			return nil, err
		}
		_ = cols
		var domains []string
		for _, row := range rows {
			if len(row) == 0 {
				return nil, fmt.Errorf("history query returned an empty row")
			}
			name, ok := row[0].(string)
			if !ok {
				return nil, fmt.Errorf("history query returned a non-string domain")
			}
			domains = append(domains, name)
		}
		return domains, nil
	}

	var windowSQL string
	switch window {
	case "1h":
		windowSQL = "-1 hours"
	case "24h":
		windowSQL = "-1 days"
	default: // "7d" or unrecognized
		windowSQL = "-7 days"
	}

	rows, err := readDB.Query(`
		SELECT RTRIM(query_name, '.'), SUM(coalesced_count) as cnt FROM query_logs
		WHERE client_ip = ? AND timestamp >= datetime('now', ?)
		GROUP BY 1 ORDER BY cnt DESC LIMIT ?
	`, clientIP, windowSQL, limit)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)

	var domains []string
	for rows.Next() {
		var name string
		var cnt int
		if err := rows.Scan(&name, &cnt); err != nil {
			return nil, err
		}
		domains = append(domains, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return domains, nil
}

// intToStr converts an int to string without importing strconv.
func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
