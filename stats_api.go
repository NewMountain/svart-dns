package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// parseTimeWindow parses a window query param ("1h", "24h", "7d") into a time.Duration.
func parseTimeWindow(r *http.Request) time.Duration {
	switch r.URL.Query().Get("window") {
	case "5m":
		return 5 * time.Minute
	case "1h":
		return time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// windowSince returns the UTC threshold timestamp for a given duration.
// Uses UTC because SQLite CURRENT_TIMESTAMP stores UTC.
func windowSince(d time.Duration) string {
	return time.Now().UTC().Add(-d).Format("2006-01-02 15:04:05")
}

// getConfiguredTimezone returns the *time.Location from the timezone setting, falling back to UTC.
func getConfiguredTimezone() (*time.Location, error) {
	var tz string
	err := readDB.QueryRow("SELECT value FROM settings WHERE key='timezone'").Scan(&tz)
	if errors.Is(err, sql.ErrNoRows) || err == nil && tz == "" {
		return time.UTC, nil
	}
	if err != nil {
		return nil, err
	}
	return time.LoadLocation(tz)
}

// utcToConfiguredTZ converts a UTC timestamp string from SQLite to the configured timezone.
func utcToConfiguredTZ(utcTS string, loc *time.Location) string {
	t, err := time.Parse("2006-01-02 15:04:05", utcTS)
	if err != nil {
		return utcTS
	}
	return t.In(loc).Format("2006-01-02T15:04:05")
}

// handleAPIStatsTimeseries godoc
// @Summary Get query timeseries data
// @Description Returns time-bucketed query counts for charting
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Param buckets query int false "Number of time buckets (default 24)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/timeseries [get]
func handleAPIStatsTimeseries(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	buckets, err := intQueryParam(r.URL.Query(), "buckets", 24, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// If only 1 bucket requested, return aggregate stats (for stat cards).
	// Single scan over the timestamp range instead of 7 separate queries.
	if buckets == 1 {
		summary, err := queryDashboardSummary(since)
		if err != nil {
			writeDBError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	result, err := queryDashboardTimeseries(window, since, buckets)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsTopClients godoc
// @Summary Get top clients by query count
// @Description Returns clients ranked by query volume with allowed/blocked breakdown
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Param limit query int false "Max results (default 10)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/top-clients [get]
func handleAPIStatsTopClients(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	limit, err := intQueryParam(r.URL.Query(), "limit", 10, 1, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := queryDashboardTopClients(since, limit)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsBlockSources godoc
// @Summary Get block sources breakdown
// @Description Returns which blocklists are responsible for blocks
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/block-sources [get]
func handleAPIStatsBlockSources(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	result, err := queryDashboardBlockSources(since)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsUpstreamUsage godoc
// @Summary Get upstream DNS usage breakdown
// @Description Returns which upstream resolvers handled queries
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/upstream-usage [get]
func handleAPIStatsUpstreamUsage(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	result, err := queryDashboardUpstreamUsage(since)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsTopDomains godoc
// @Summary Get top queried domains
// @Description Returns top domains by query count, optionally filtered by blocked status
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Param limit query int false "Max results (default 10)"
// @Param blocked query string false "Filter by blocked status (true/false)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/top-domains [get]
func handleAPIStatsTopDomains(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	limit, err := intQueryParam(r.URL.Query(), "limit", 50, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var blockedFilter *bool
	switch r.URL.Query().Get("blocked") {
	case "true", "1":
		blocked := true
		blockedFilter = &blocked
	case "false", "0":
		blocked := false
		blockedFilter = &blocked
	}

	result, err := queryDashboardTopDomains(since, blockedFilter, limit)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsLatency godoc
// @Summary Get latency timeseries
// @Description Returns time-bucketed avg and max latency for charting
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Param buckets query int false "Number of time buckets (default 24)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/latency [get]
func handleAPIStatsLatency(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	buckets, err := intQueryParam(r.URL.Query(), "buckets", 24, 1, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := queryDashboardLatency(window, since, buckets)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIStatsServfails godoc
// @Summary Get SERVFAIL breakdown by client
// @Description Returns SERVFAIL counts per client with top failed domains
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window: 1h, 24h, 7d (default 24h)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats/servfails [get]
func handleAPIStatsServfails(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	result, err := queryDashboardServfails(since)
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIRewritesStats godoc
// @Summary Get rewrite hit statistics
// @Description Returns hit counts and unique client counts per rewrite domain
// @Tags Rewrites
// @Security ApiKeyAuth
// @Produce json
// @Param window query string false "Time window (default 24h)"
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/rewrites/stats [get]
func handleAPIRewritesStats(w http.ResponseWriter, r *http.Request) {
	window := parseTimeWindow(r)
	since := windowSince(window)

	// Strip trailing dot from query_name so domains match the rewrites table (e.g. "nas.home.arpa" not "nas.home.arpa.")
	rows, err := readDB.Query(`
		SELECT RTRIM(ql.query_name, '.') as domain, SUM(ql.coalesced_count) as hits, COUNT(DISTINCT ql.client_ip) as unique_clients
		FROM query_logs ql
		WHERE ql.upstream = 'rewrite' AND ql.timestamp > ?
		GROUP BY domain
		ORDER BY hits DESC`, since)
	if err != nil {
		writeDBError(w, err)
		return
	}

	defer closeQueryRows(rows)

	// Collect outer results first to avoid nested query deadlock (MaxOpenConns=1)
	type rwDomain struct {
		domain        string
		hits          int64
		uniqueClients int64
	}
	var domains []rwDomain
	for rows.Next() {
		var d rwDomain
		if err := rows.Scan(&d.domain, &d.hits, &d.uniqueClients); err != nil {
			writeDBError(w, err)
			return
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	closeQueryRows(rows)

	var result []RewriteStatsView
	for _, d := range domains {
		entry := RewriteStatsView{Domain: d.domain, Hits: d.hits, UniqueClients: d.uniqueClients}

		// Fetch top clients for this rewrite domain (safe — outer rows closed)
		clientRows, err := readDB.Query(`
			SELECT ql.client_ip, SUM(ql.coalesced_count) as cnt
			FROM query_logs ql
			WHERE ql.upstream = 'rewrite' AND RTRIM(ql.query_name, '.') = ? AND ql.timestamp > ?
			GROUP BY ql.client_ip
			ORDER BY cnt DESC
			LIMIT 5`, d.domain, since)
		if err != nil {
			writeDBError(w, err)
			return
		}
		{
			var clients []RewriteTopClient
			for clientRows.Next() {
				var ip string
				var count int64
				if err := clientRows.Scan(&ip, &count); err != nil {
					closeQueryRows(clientRows)
					writeDBError(w, err)
					return
				}
				clients = append(clients, RewriteTopClient{IP: ip, Count: count})
			}
			if err := clientRows.Err(); err != nil {
				closeQueryRows(clientRows)
				writeDBError(w, err)
				return
			}
			closeQueryRows(clientRows)
			if clients != nil {
				entry.TopClients = clients
			}
		}

		result = append(result, entry)
	}
	if result == nil {
		result = []RewriteStatsView{}
	}
	writeJSON(w, http.StatusOK, result)
}

const maxAuthRequestBodyBytes = 16 * 1024

// handleAPIAuthLogin handles JSON login for the SPA.
func handleAPIAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !authEnabled() {
		writeError(w, http.StatusServiceUnavailable, "no admin users configured")
		return
	}

	var req AuthLoginRequest
	if !decodeJSONBody(w, r, maxAuthRequestBodyBytes, &req) {
		return
	}

	clientIP := loginClientIP(r)
	clientKey := loginClientKey(clientIP)
	if wait := logins.retryAfter(req.Username, clientKey, time.Now()); wait > 0 {
		seconds := int(math.Ceil(wait.Seconds()))
		logAuth.Warn("login throttled", "username", req.Username, "client_ip", clientIP, "retry_after_s", seconds)
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeError(w, http.StatusTooManyRequests, fmt.Sprintf("too many failed logins; retry in %d seconds", seconds))
		return
	}

	release, ok := acquireLoginBcryptSlot(r.Context())
	if !ok {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "login is busy verifying other attempts; retry in a second")
		return
	}
	_, passwordHash, role, _, found, err := getAdminUserByUsername(req.Username)
	if err != nil {
		release()
		writeDBError(w, err)
		return
	}
	compareAgainst := []byte(passwordHash)
	if !found {
		compareAgainst = dummyBcryptHash()
	}
	compareErr := bcrypt.CompareHashAndPassword(compareAgainst, []byte(req.Password))
	release()

	if !found || compareErr != nil {
		reason := "wrong password"
		if !found {
			reason = "unknown username"
		}
		logAuth.Warn("login failed", "username", req.Username, "client_ip", clientIP, "reason", reason)
		if err := logins.recordFailure(req.Username, clientKey, time.Now()); err != nil {
			logAuth.Error("login throttle full; refusing new failing clients", "error", err)
			w.Header().Set("Retry-After", "60")
			writeError(w, http.StatusTooManyRequests, "too many failed logins from too many clients; retry in a minute")
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	logins.recordSuccess(req.Username)

	token, expires, err := createSession(req.Username, passwordHash)
	if err != nil {
		logAuth.Error("login succeeded but the session could not be stored", "username", req.Username, "error", err)
		writeError(w, http.StatusServiceUnavailable, "session authentication unavailable: the session could not be stored; retry, and check the database if it keeps failing")
		return
	}
	// A cookie the browser still holds from an earlier login is replaced, so
	// end that session instead of leaving it valid until it expires.
	if old, err := r.Cookie(sessionCookieName); err == nil {
		if err := deleteSession(old.Value); err != nil {
			logAuth.Warn("could not delete the replaced session", "error", err)
		}
	}
	setSessionCookie(w, r, token, expires)

	writeJSON(w, http.StatusOK, AuthenticatedIdentity{Authenticated: true, Role: role, Username: req.Username})
}

// handleAPIAuthCheck returns the current auth state.
func handleAPIAuthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check API key first
	if apiKey := r.Header.Get("X-Api-Key"); apiKey != "" {
		token, err := validateAPIKey(r.Context(), apiKey)
		if err != nil {
			if errors.Is(err, errTokenVerificationBusy) {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusTooManyRequests, errTokenVerificationBusy.Error())
			} else {
				writeError(w, http.StatusServiceUnavailable, "API token verification unavailable; retry later")
			}
			return
		}
		if token != nil {
			writeJSON(w, http.StatusOK, AuthenticatedIdentity{Authenticated: true, Role: token.Role, Username: token.Name})
			return
		}
	}

	// Check session cookie
	if user := requestSession(r); user != nil {
		writeJSON(w, http.StatusOK, AuthenticatedIdentity{Authenticated: true, Role: user.Role, Username: user.Username})
		return
	}

	writeJSON(w, http.StatusOK, UnauthenticatedIdentity{Authenticated: false, SetupRequired: setupRequired(), Role: "", Username: ""})
}

// handleAPIAuthLogout ends the server-side session and clears the cookie.
func handleAPIAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if err := deleteSession(cookie.Value); err != nil {
			logAuth.Error("logout could not delete the session", "error", err)
			writeError(w, http.StatusInternalServerError, "logout failed: the session is still active; retry")
			return
		}
	}
	clearSessionCookie(w, r)

	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}

// handleAPIBlocklistHistory returns the blocklist refresh history.
func handleAPIBlocklistHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	rows, err := readDB.Query(`
		SELECT bh.id, bh.blocklist_id, COALESCE(bl.alias, '') as alias,
			bh.refreshed_at, bh.previous_count, bh.new_count,
			bh.added_count, bh.removed_count,
			COALESCE(bh.sample_added, ''), COALESCE(bh.sample_removed, '')
		FROM blocklist_history bh
		LEFT JOIN blocklists bl ON bh.blocklist_id = bl.id
		ORDER BY bh.refreshed_at DESC
		LIMIT 50`)
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	var result []BlocklistHistoryView
	for rows.Next() {
		var id, blocklistID, prevCount, newCount, addedCount, removedCount int
		var alias, refreshedAt, sampleAdded, sampleRemoved string
		if err := rows.Scan(&id, &blocklistID, &alias, &refreshedAt, &prevCount, &newCount, &addedCount, &removedCount, &sampleAdded, &sampleRemoved); err != nil {
			writeDBError(w, err)
			return
		}

		addedList, err := decodeHistorySample(sampleAdded)
		if err != nil {
			writeDBError(w, err)
			return
		}
		removedList, err := decodeHistorySample(sampleRemoved)
		if err != nil {
			writeDBError(w, err)
			return
		}
		if addedList == nil {
			addedList = []string{}
		}
		if removedList == nil {
			removedList = []string{}
		}

		result = append(result, BlocklistHistoryView{ID: id, BlocklistID: blocklistID, BlocklistAlias: alias, RefreshedAt: refreshedAt, PreviousCount: prevCount, NewCount: newCount, AddedCount: addedCount, RemovedCount: removedCount, SampleAdded: addedList, SampleRemoved: removedList})
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	if result == nil {
		result = []BlocklistHistoryView{}
	}
	writeJSON(w, http.StatusOK, result)
}

// splitDomains splits a comma-separated domain string.
func splitDomains(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// BlocklistUniqueStats is the result of computing unique domain counts across selected blocklists.
type BlocklistUniqueStats struct {
	UniqueTotal int                    `json:"unique_total"`
	Lists       []BlocklistUniqueEntry `json:"lists"`
}

// BlocklistUniqueEntry holds per-list unique domain info within a selected set.
type BlocklistUniqueEntry struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	Total       int    `json:"total"`
	UniqueToSet int    `json:"unique_to_set"`
}

// computeBlocklistUniqueDomains computes the unique domain count for a set of
// blocklist IDs from the list index (no list is copied or re-read).
// Lists are walked from smallest to largest: each list's "new" count is the
// rules no smaller list in the set holds. This gives cumulative attribution —
// the smallest, most-specific list gets full credit, larger lists show
// marginal value. IDs that are not enabled blocklists count as empty.
func computeBlocklistUniqueDomains(ids []int) BlocklistUniqueStats {
	ix := policyState.Load().Index
	lists := make([]BlocklistUniqueEntry, len(ids))
	var slots []int
	for i, id := range ids {
		lists[i] = BlocklistUniqueEntry{ID: id}
		if slot := ix.SlotOf(id, false); slot >= 0 {
			lists[i].Alias, lists[i].Total = ix.Lists[slot].Name, ix.Lists[slot].Count
			slots = append(slots, slot)
		}
	}
	// Slot order is size order (then ID), so the first position holding a
	// rule is the smallest list that has it.
	slices.Sort(slots)
	slots = slices.Compact(slots)
	newBySlot := make(map[int]int, len(slots))
	unique := 0
	for _, c := range ix.SpreadOf(slots) {
		newBySlot[slots[c.Positions[0]]] += c.Rules
		unique += c.Rules
	}
	for i := range lists {
		if slot := ix.SlotOf(lists[i].ID, false); slot >= 0 {
			lists[i].UniqueToSet = newBySlot[slot]
		}
	}
	return BlocklistUniqueStats{UniqueTotal: unique, Lists: lists}
}

// handleAPIBlocklistsUniqueDomains returns the unique domain count for a set of
// blocklist IDs. Raw domains are loaded on demand to avoid permanent duplicate
// in-memory blocklist copies.
// GET /api/blocklists/unique-domains?ids=1,2,3
func handleAPIBlocklistsUniqueDomains(w http.ResponseWriter, r *http.Request) {
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		writeJSON(w, http.StatusOK, BlocklistUniqueStats{UniqueTotal: 0, Lists: []BlocklistUniqueEntry{}})
		return
	}

	var requested []int
	for _, p := range strings.Split(idsParam, ",") {
		id, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("ids must be comma-separated blocklist IDs, got %q", p))
			return
		}
		requested = append(requested, id)
	}
	ix := policyState.Load().Index
	ids, err := resolveAnalysisLists(requested, ix)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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

	writeJSON(w, http.StatusOK, computeBlocklistUniqueDomains(ids))
}

// handleAPIBlocklistCheckpointDomains reconstructs the domain set at a historical checkpoint.
// GET /api/blocklists/history/{historyId}/domains?search=&limit=&offset=
// Algorithm: start with current domains, walk backwards through newer history entries
// undoing each changelog diff to reconstruct the state at the requested checkpoint.
func handleAPIBlocklistCheckpointDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse /api/blocklists/history/{historyId}/domains
	path := strings.TrimPrefix(r.URL.Path, "/api/blocklists/history/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "domains" {
		writeError(w, http.StatusBadRequest, "Expected /api/blocklists/history/{id}/domains")
		return
	}
	historyID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid history ID")
		return
	}
	limit, offset, err := pageParams(r.URL.Query(), 1000, 10000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	release, ok := acquireAnalysisSlot(w)
	if !ok {
		return
	}
	defer release()
	page, err := queryCheckpointPage(r.Context(), historyID, r.URL.Query().Get("search"), limit, offset)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "History entry not found")
		return
	}
	if err != nil {
		logAdmin.Error("checkpoint reconstruction unavailable", "history_id", historyID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "Checkpoint history unavailable; retry the request")
		return
	}
	writeJSON(w, http.StatusOK, page)
}
