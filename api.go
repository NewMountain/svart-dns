package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// handleDocs godoc
// @Summary Serve API documentation viewer
// @Description Serves the RapiDoc interactive API documentation page
// @Tags Documentation
// @Produce html
// @Success 200 {string} string "HTML page"
// @Router /docs [get]
func handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if _, err := fmt.Fprint(w, docsHTML); err != nil {
		logAdmin.Error("API documentation response write failed", "error", err)
	}
}

// handleDocsJSON godoc
// @Summary Serve OpenAPI specification
// @Description Returns the OpenAPI/Swagger JSON specification
// @Tags Documentation
// @Produce json
// @Success 200 {object} object
// @Router /docs/swagger.json [get]
func handleDocsJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := fmt.Fprint(w, swaggerJSON); err != nil {
		logAdmin.Error("API documentation response write failed", "error", err)
	}
}

func handleDocsMarkdown(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	if _, err := fmt.Fprint(w, apiMarkdown); err != nil {
		logAdmin.Error("API documentation response write failed", "error", err)
	}
}

// handleRapidocJS serves the self-hosted RapiDoc JS bundle.
func handleRapidocJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	if _, err := fmt.Fprint(w, rapidocJS); err != nil {
		logAdmin.Error("API documentation response write failed", "error", err)
	}
}

// APIResponse is the typed success envelope. Errors have their own concrete
// body so an error can never carry a partially read success payload.
type APIResponse[T any] struct {
	Data  T       `json:"data"`
	Error *string `json:"error"`
}
type APIErrorResponse struct {
	Data      *struct{} `json:"data"`
	Error     string    `json:"error"`
	ErrorCode string    `json:"error_code"`
}

func writeJSON[T any](w http.ResponseWriter, status int, data T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(APIResponse[T]{Data: data}); err != nil {
		logAdmin.Error("API response encoding or write failed", "error", err)
	}
}
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(APIErrorResponse{Error: msg, ErrorCode: apiErrorCode(status)}); err != nil {
		logAdmin.Error("API error response write failed", "error", err)
	}
}

// apiErrorCode is stable across wording changes; the legacy error string
// remains available to existing consumers.
func apiErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusRequestTimeout:
		return "request_canceled"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusUnprocessableEntity:
		return "resource_limit"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "unavailable"
	case http.StatusGatewayTimeout:
		return "timeout"
	default:
		return "internal_error"
	}
}

func writeDBLookupError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, message)
		return
	}
	writeDBError(w, err)
}

func writeDBError(w http.ResponseWriter, err error) {
	logAdmin.Error("API data operation unavailable", "error", err)
	writeError(w, http.StatusServiceUnavailable, "Data unavailable; retry the request")
}

// maxRequestBodyBytes is the ceiling every admin request body is held to by
// limitRequestBodies before any handler runs. It equals the largest
// legitimate body (a config import), so a handler that reads r.Body without
// its own tighter cap still cannot be fed an unbounded stream.
const maxRequestBodyBytes = maxConfigImportBodyBytes

// maxAnalysisRequestBodyBytes fits the largest accepted analysis request:
// 10,000 domains of up to 253 bytes each plus JSON punctuation (~2.6 MB).
const maxAnalysisRequestBodyBytes = 4 * 1024 * 1024

// limitRequestBodies wraps every request body in http.MaxBytesReader so the
// admin server never buffers or parses more than maxRequestBodyBytes.
func limitRequestBodies(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// decodeJSONBody decodes exactly one JSON value of at most limit bytes from
// r.Body into dst. On failure it writes the error response (413 when the
// body exceeds limit, 400 otherwise) and returns false, so every JSON
// handler caps and reports bodies the same way. Unknown object fields are
// accepted for compatibility with clients that send newer fields.
func decodeJSONBody[T any](w http.ResponseWriter, r *http.Request, limit int64, dst *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	var decoded *T
	if err := dec.Decode(&decoded); err != nil {
		writeJSONDecodeError(w, err)
		return false
	}
	if decoded == nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request body: expected a non-null value")
		return false
	}
	*dst = *decoded
	switch err := dec.Decode(&struct{}{}); err {
	case io.EOF:
		return true
	case nil:
		writeError(w, http.StatusBadRequest, "invalid JSON request body: expected exactly one JSON value, found more")
	default:
		writeJSONDecodeError(w, err)
	}
	return false
}

func writeJSONDecodeError(w http.ResponseWriter, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("request body exceeds the %d-byte limit for this endpoint; split the request into smaller ones", mbe.Limit))
		return
	}
	if message, ok := jsonTypeErrorMessage(err); ok {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON request body: "+err.Error())
}

// maxPaginationOffset bounds offset parameters to what SQLite and slice
// indexing both handle without overflow.
const maxPaginationOffset = math.MaxInt32

// intQueryParam parses the optional integer query parameter name, returning
// def when it is absent. Anything else outside [min, max] is an error rather
// than a silent fallback: a caller asking for offset=-5 or limit=99999 gets a
// 400 that says what is allowed instead of a page it did not ask for.
func intQueryParam(q url.Values, name string, def, minimum, maximum int) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer, got %q", name, raw)
	}
	if v < minimum || v > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d, got %d", name, minimum, maximum, v)
	}
	return v, nil
}

// pageParams parses the limit/offset pair shared by paginated list endpoints.
func pageParams(q url.Values, defLimit, maxLimit int) (limit, offset int, err error) {
	if limit, err = intQueryParam(q, "limit", defLimit, 1, maxLimit); err != nil {
		return 0, 0, err
	}
	if offset, err = intQueryParam(q, "offset", 0, 0, maxPaginationOffset); err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// redactedURLPart replaces URL components that commonly carry credentials.
const redactedURLPart = "REDACTED"

// redactURL hides the userinfo, query string and fragment of a list or
// upstream URL (A11): list subscriptions and DoH endpoints routinely carry
// tokens there. Scheme, host and path stay visible so the entry is still
// recognisable. A value that cannot be parsed but contains one of those
// markers is withheld entirely rather than shown half-redacted.
func redactURL(raw string) string {
	if !strings.ContainsAny(raw, "@?#") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted: unparseable URL containing credentials or a query]"
	}
	if u.User != nil {
		u.User = url.User(redactedURLPart)
	}
	if u.RawQuery != "" || u.ForceQuery {
		u.RawQuery, u.ForceQuery = redactedURLPart, false
	}
	if u.Fragment != "" {
		u.Fragment, u.RawFragment = redactedURLPart, ""
	}
	return u.String()
}

// urlForViewer shows admins the full URL and everyone else redactURL's view.
func urlForViewer(raw string, isAdmin bool) string {
	if isAdmin {
		return raw
	}
	return redactURL(raw)
}

// Router functions that dispatch GET to new handlers, other methods to existing handlers.

func handleAPIBlocklistsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetBlocklists(w, r)
	} else {
		handleAPIBlocklists(w, r)
	}
}

func handleAPIRewritesRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetRewrites(w, r)
	} else {
		handleAPIRewrites(w, r)
	}
}

func handleAPIGroupsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetGroups(w, r)
	} else {
		handleAPIGroups(w, r)
	}
}

func handleAPIClientsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetClients(w, r)
	} else {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleAPIBootstrapRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetBootstrap(w, r)
	} else {
		handleAPIBootstrap(w, r)
	}
}

func handleAPISettingsRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		handleAPIGetSettings(w, r)
	} else {
		writeError(w, http.StatusMethodNotAllowed, "Use /api/settings/{key} for updates")
	}
}

var neverExposeSettings = map[string]struct{}{
	"session_secret": {},
	"sync_secret":    {},
}

var adminOnlySettings = map[string]struct{}{
	"deleted_peers": {},
	"sync_peers":    {},
}

func isSensitiveSettingKey(key string) bool {
	if isNeverExposeSetting(key) {
		return true
	}
	if _, ok := adminOnlySettings[key]; ok {
		return true
	}

	lower := strings.ToLower(key)
	return strings.Contains(lower, "secret") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "token") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "apikey")
}

func isNeverExposeSetting(key string) bool {
	_, ok := neverExposeSettings[key]
	return ok
}

func shouldExposeSetting(key string, isAdmin bool) bool {
	if isNeverExposeSetting(key) {
		return false
	}
	if isSensitiveSettingKey(key) && !isAdmin {
		return false
	}
	return true
}

// handleHealth godoc
// @Summary Health check
// @Description Returns only public liveness status. All node and operational details require authentication.
// @Tags System
// @Produce json
// @Success 200 {object} apiResponse
// @Router /health [get]
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, HealthResponse{Status: "ok"})
}

// handleAPIStats godoc
// @Summary Get dashboard statistics
// @Description Returns total queries, blocked queries, average latency, uptime, and cache statistics
// @Tags Stats
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/stats [get]
func handleAPIStats(w http.ResponseWriter, _ *http.Request) {
	totalQueries, blockedQueries, avgLatency, err := getDashboardStats()
	if err != nil {
		writeDBError(w, err)
		return
	}
	cacheStats := getCacheStats()

	writeJSON(w, http.StatusOK, DNSStatsResponse{TotalQueries: totalQueries, BlockedQueries: blockedQueries, AvgLatencyMicroseconds: avgLatency, UptimeSeconds: int(time.Since(serverStart).Seconds()), Cache: DNSCacheStats{Hits: cacheStats.Hits, Misses: cacheStats.Misses, HitRate: cacheStats.HitRate, Entries: cacheStats.Entries, EstimatedBytes: cacheStats.EstimatedBytes, PeakEntries: cacheStats.PeakEntries, PeakBytes: cacheStats.PeakBytes}})
}

// handleAPIGetBlocklists godoc
// @Summary List all blocklists
// @Description Returns all configured blocklists with their ID, URL, alias, enabled status, domain count, and last updated timestamp
// @Tags Blocklists
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/blocklists [get]
func handleAPIGetBlocklists(w http.ResponseWriter, r *http.Request) {
	rows, err := readListViewRows(readDB, "blocklist")
	if err != nil {
		writeDBError(w, err)
		return
	}
	isAdmin := requestIsAdmin(r)
	blocklists := make([]BlocklistView, 0, len(rows))
	for _, row := range rows {
		row.URL = urlForViewer(row.URL, isAdmin)
		blocklists = append(blocklists, BlocklistView(row))
	}
	writeJSON(w, http.StatusOK, blocklists)
}

// handleAPIGetRewrites godoc
// @Summary List all rewrites
// @Description Returns all DNS rewrite rules with domain, IP addresses, and enabled status
// @Tags Rewrites
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/rewrites [get]
func handleAPIGetRewrites(w http.ResponseWriter, _ *http.Request) {
	rewrites, err := getRewrites()
	if err != nil {
		writeDBError(w, err)
		return
	}
	if rewrites == nil {
		rewrites = []RewriteView{}
	}
	writeJSON(w, http.StatusOK, rewrites)
}

// handleAPIGetGroups godoc
// @Summary List all groups
// @Description Returns all client groups with their members and assigned lists
// @Tags Groups
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/groups [get]
func handleAPIGetGroups(w http.ResponseWriter, _ *http.Request) {
	groups, err := getAllGroups()
	if err != nil {
		writeDBError(w, err)
		return
	}
	if groups == nil {
		groups = []Group{}
	}
	writeJSON(w, http.StatusOK, groups)
}

// handleAPIGetClients godoc
// @Summary List all known clients
// @Description Returns all known DNS clients with their IP, alias, group memberships, and list assignments
// @Tags Clients
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/clients [get]
func handleAPIGetClients(w http.ResponseWriter, _ *http.Request) {
	clients, err := getAllClients()
	if err != nil {
		writeDBError(w, err)
		return
	}
	if clients == nil {
		clients = []Client{}
	}
	writeJSON(w, http.StatusOK, clients)
}

// handleAPIGetBootstrap godoc
// @Summary List bootstrap DNS servers
// @Description Returns all configured bootstrap DNS servers used for resolving upstream hostnames
// @Tags Bootstrap
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/bootstrap [get]
func handleAPIGetBootstrap(w http.ResponseWriter, _ *http.Request) {
	rows, err := readDB.Query("SELECT id, server FROM bootstrap_servers ORDER BY id")
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	var servers []BootstrapServerView
	for rows.Next() {
		var id int
		var server string
		if err := rows.Scan(&id, &server); err != nil {
			writeDBError(w, err)
			return
		}
		servers = append(servers, BootstrapServerView{ID: id, Server: server})
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	if servers == nil {
		servers = []BootstrapServerView{}
	}
	writeJSON(w, http.StatusOK, servers)
}

// handleAPIGetSettings godoc
// @Summary List all settings
// @Description Returns all configuration settings as key-value pairs
// @Tags Settings
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/settings [get]
func handleAPIGetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := readDB.Query("SELECT key, value FROM settings ORDER BY key")
	if err != nil {
		writeDBError(w, err)
		return
	}
	defer closeQueryRows(rows)

	settings := make(map[string]string)
	isAdmin := requestIsAdmin(r)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			writeDBError(w, err)
			return
		}
		if !shouldExposeSetting(key, isAdmin) {
			continue
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

// handleAPIPolicyEvaluate godoc
// @Summary Evaluate policy for a client and domain
// @Description Debug endpoint that shows the full three-tier policy evaluation chain (Range, Group, IP) without caching
// @Tags Policy
// @Security ApiKeyAuth
// @Produce json
// @Param client_ip query string true "Client IP address to evaluate"
// @Param domain query string true "Domain name to evaluate"
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/policy/evaluate [get]
func handleAPIPolicyEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	clientIP := r.URL.Query().Get("client_ip")
	domain := r.URL.Query().Get("domain")

	if clientIP == "" || domain == "" {
		writeError(w, http.StatusBadRequest, "client_ip and domain query parameters are required")
		return
	}

	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimSuffix(domain, ".")
	if len(domain) > maxDNSNameLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("domain is %d bytes; DNS names are at most %d", len(domain), maxDNSNameLength))
		return
	}

	rrtype, err := parsePolicyRecordType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result := evaluatePolicyFull(clientIP, domain, rrtype)
	writeJSON(w, http.StatusOK, result)
}

// handleAPIArchive godoc
// @Summary Trigger manual archive
// @Description Manually triggers an archive cycle to export old query logs to Parquet files and returns the archive status
// @Tags Archive
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/archive [post]
func handleAPIArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	runArchiveCycle()

	// Return list of archive files created
	status, err := getArchiveStatus(r.Context())
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// handleAPIArchiveStatus godoc
// @Summary Get archive status
// @Description Returns archive status including list of Parquet archive files and their metadata
// @Tags Archive
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Failure 500 {object} apiResponse
// @Router /api/archive/status [get]
func handleAPIArchiveStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	status, err := getArchiveStatus(r.Context())
	if err != nil {
		writeDBError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
