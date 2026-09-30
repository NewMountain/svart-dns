package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// decodeAPIResponse decodes the standard {data, error} envelope.
func decodeAPIResponse(t *testing.T, body *bytes.Buffer) (interface{}, *string) {
	t.Helper()
	var resp apiResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode API response: %v", err)
	}
	return resp.Data, resp.Error
}

func TestHandleHealthReturnsOnlyPublicLivenessStatus(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	nodeID = "test-node-id"
	if _, err := db.Exec("UPDATE settings SET value = 'test-node-name' WHERE key = 'node_name'"); err != nil {
		t.Fatalf("set node name: %v", err)
	}
	syncMu.Lock()
	savedPeerStates := peerStates
	peerStates = []*peerState{{URL: "https://10.42.1.7:443", Healthy: true}}
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		peerStates = savedPeerStates
		syncMu.Unlock()
	}()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("unexpected error: %s", *apiErr)
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal health response: %v", err)
	}
	const expected = `{"status":"ok"}`
	if string(encoded) != expected {
		t.Fatalf("health response = %s, want %s", encoded, expected)
	}
}

func TestHandleAPIStats(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	serverStart = time.Now().Add(-10 * time.Minute)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	w := httptest.NewRecorder()

	handleAPIStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)

	if requireFixtureType[float64](t, m["total_queries"]) < 1 {
		t.Error("expected total_queries > 0")
	}
	if requireFixtureType[float64](t, m["blocked_queries"]) < 1 {
		t.Error("expected blocked_queries > 0")
	}
}

func TestHandleAPIGetBlocklists(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/blocklists", nil)
	w := httptest.NewRecorder()

	handleAPIGetBlocklists(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	lists := requireFixtureType[[]interface{}](t, data)
	if len(lists) != 3 {
		t.Errorf("expected 3 blocklists, got %d", len(lists))
	}
}

func TestHandleAPIGetRewrites(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/rewrites", nil)
	w := httptest.NewRecorder()

	handleAPIGetRewrites(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	rewrites := requireFixtureType[[]interface{}](t, data)
	if len(rewrites) != 5 {
		t.Errorf("expected 5 rewrites, got %d", len(rewrites))
	}
}

func TestHandleAPIGetGroups(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/groups", nil)
	w := httptest.NewRecorder()

	handleAPIGetGroups(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	groups := requireFixtureType[[]interface{}](t, data)
	if len(groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(groups))
	}
}

func TestHandleAPIGetClients(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/clients", nil)
	w := httptest.NewRecorder()

	handleAPIGetClients(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	clients := requireFixtureType[[]interface{}](t, data)
	if len(clients) < 5 {
		t.Errorf("expected at least 5 clients, got %d", len(clients))
	}
}

func TestHandleAPIGetBootstrap(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/bootstrap", nil)
	w := httptest.NewRecorder()

	handleAPIGetBootstrap(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	servers := requireFixtureType[[]interface{}](t, data)
	if len(servers) < 1 {
		t.Error("expected at least 1 bootstrap server")
	}
}

func TestHandleAPIGetSettings(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/settings", nil)
	w := httptest.NewRecorder()

	handleAPIGetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	settings := requireFixtureType[map[string]interface{}](t, data)
	if settings["strategy"] != "weighted" {
		t.Errorf("expected strategy 'weighted', got %v", settings["strategy"])
	}
	if settings["cache_ttl"] != "3600" {
		t.Errorf("expected cache_ttl '3600', got %v", settings["cache_ttl"])
	}
}

func TestHandleAPIGetSettingsRedactsSensitiveValuesForReadonly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'https://peer-a:443' WHERE key = 'sync_peers'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'sync-secret-value' WHERE key = 'sync_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'session-secret-value' WHERE key = 'session_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestAuthContextKey, &APIToken{Role: RoleReadonly, Name: "viewer"}))
	w := httptest.NewRecorder()

	handleAPIGetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	settings := requireFixtureType[map[string]interface{}](t, data)

	if settings["strategy"] != "weighted" {
		t.Errorf("expected strategy 'weighted', got %v", settings["strategy"])
	}
	if _, ok := settings["sync_secret"]; ok {
		t.Fatalf("readonly response should redact sync_secret, got %v", settings["sync_secret"])
	}
	if _, ok := settings["session_secret"]; ok {
		t.Fatalf("readonly response should redact session_secret, got %v", settings["session_secret"])
	}
	if _, ok := settings["sync_peers"]; ok {
		t.Fatalf("readonly response should redact sync_peers, got %v", settings["sync_peers"])
	}
}

func TestHandleAPIGetSettingsRedactsSecretsForAdmin(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'https://peer-a:443' WHERE key = 'sync_peers'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'sync-secret-value' WHERE key = 'sync_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("UPDATE settings SET value = 'session-secret-value' WHERE key = 'session_secret'"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestAuthContextKey, &APIToken{Role: RoleAdmin, Name: "admin"}))
	w := httptest.NewRecorder()

	handleAPIGetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	settings := requireFixtureType[map[string]interface{}](t, data)

	if _, ok := settings["sync_secret"]; ok {
		t.Fatalf("admin response should redact sync_secret, got %v", settings["sync_secret"])
	}
	if settings["sync_peers"] != "https://peer-a:443" {
		t.Fatalf("expected admin to see sync_peers, got %v", settings["sync_peers"])
	}
	if _, ok := settings["session_secret"]; ok {
		t.Fatalf("session_secret should never be exposed, got %v", settings["session_secret"])
	}
}

func TestHandleAPIQueryLogs(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	// Basic request
	req := httptest.NewRequest("GET", "/api/query-logs", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)
	logs := requireFixtureType[[]interface{}](t, m["logs"])
	total := int(requireFixtureType[float64](t, m["total"]))

	if len(logs) < 1 {
		t.Error("expected at least 1 log entry")
	}
	if total < 1 {
		t.Error("expected total > 0")
	}
}

func TestHandleAPIQueryLogsFilterByClient(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/query-logs?client_ip=10.42.1.42", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)
	logs := requireFixtureType[[]interface{}](t, m["logs"])

	for _, l := range logs {
		entry := requireFixtureType[map[string]interface{}](t, l)
		if entry["client_ip"] != "10.42.1.42" {
			t.Errorf("expected client_ip '10.42.1.42', got %v", entry["client_ip"])
		}
	}
}

func TestHandleAPIQueryLogsFilterByBlocked(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/query-logs?blocked=true", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)
	logs := requireFixtureType[[]interface{}](t, m["logs"])

	if len(logs) < 1 {
		t.Fatal("expected at least 1 blocked log entry")
	}
	for _, l := range logs {
		entry := requireFixtureType[map[string]interface{}](t, l)
		if entry["blocked"] != true {
			t.Errorf("expected blocked=true, got %v", entry["blocked"])
		}
	}
}

func TestHandleAPIQueryLogsFilterByResultReasonCountsMatch(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	rows := []struct {
		clientIP     string
		queryName    string
		resultReason string
		coalesced    int
	}{
		{"10.42.1.42", "safe.example.", "allowlist", 3},
		{"10.42.1.42", "also-safe.example.", "allowlist", 2},
		{"10.42.1.43", "other.example.", "cache_hit", 7},
	}
	for _, row := range rows {
		if _, err := db.Exec(`INSERT INTO query_logs
			(client_ip, query_name, query_type, response_code, blocked, latency_microseconds, result_reason, coalesced_count)
			VALUES (?, ?, 'A', 'NOERROR', 0, 1, ?, ?)`,
			row.clientIP, row.queryName, row.resultReason, row.coalesced); err != nil {
			t.Fatalf("failed to seed query log: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/query-logs?result_reason=allowlist", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)
	logs := requireFixtureType[[]interface{}](t, m["logs"])
	total := int(requireFixtureType[float64](t, m["total"]))

	if len(logs) != 2 {
		t.Fatalf("expected 2 filtered logs, got %d", len(logs))
	}
	if total != 5 {
		t.Fatalf("expected filtered total 5, got %d", total)
	}
	for _, l := range logs {
		entry := requireFixtureType[map[string]interface{}](t, l)
		if entry["result_reason"] != "allowlist" {
			t.Fatalf("expected result_reason allowlist, got %v", entry["result_reason"])
		}
	}
}

func TestHandleAPIQueryLogDetailSynthesizesPolicyWithoutJSON(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	result, err := db.Exec(`INSERT INTO query_logs
		(client_ip, query_name, query_type, response_code, blocked, latency_microseconds, result, result_reason, result_tier, group_entity)
		VALUES ('10.42.1.42', 'safe.example.', 'A', 'NOERROR', 0, 1, 'allow', 'default_allow', 'default', 'Kids')`)
	if err != nil {
		t.Fatalf("failed to seed query log: %v", err)
	}
	id, fixtureErr11662 := result.LastInsertId()
	if fixtureErr11662 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr11662)
	}

	req := httptest.NewRequest("GET", "/api/query-logs/"+strconv.FormatInt(id, 10), nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogDetail(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, _ := decodeAPIResponse(t, w.Body)
	entry := requireFixtureType[map[string]interface{}](t, data)
	policy := requireFixtureType[map[string]interface{}](t, entry["policy"])
	if policy["result"] != "allow" {
		t.Fatalf("expected synthesized allow policy, got %v", policy["result"])
	}

	resultSource := requireFixtureType[map[string]interface{}](t, policy["result_source"])
	if resultSource["tier"] != "default" {
		t.Fatalf("expected default result source, got %v", resultSource["tier"])
	}

	groupEval := requireFixtureType[map[string]interface{}](t, policy["group_evaluation"])
	entities := requireFixtureType[[]interface{}](t, groupEval["entities"])
	if len(entities) != 1 {
		t.Fatalf("expected one synthesized group entity, got %d", len(entities))
	}
	groupEntity := requireFixtureType[map[string]interface{}](t, entities[0])
	if groupEntity["name"] != "Kids" {
		t.Fatalf("expected synthesized group entity Kids, got %v", groupEntity["name"])
	}
}

func TestHandleAPIQueryLogsPagination(t *testing.T) {
	cleanup := setupFullTestEnv(t)
	defer cleanup()

	// Request with limit=5
	req := httptest.NewRequest("GET", "/api/query-logs?limit=5", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	data, _ := decodeAPIResponse(t, w.Body)
	m := requireFixtureType[map[string]interface{}](t, data)
	logs := requireFixtureType[[]interface{}](t, m["logs"])
	limit := int(requireFixtureType[float64](t, m["limit"]))

	if len(logs) != 5 {
		t.Errorf("expected 5 logs with limit=5, got %d", len(logs))
	}
	if limit != 5 {
		t.Errorf("expected limit=5 in response, got %d", limit)
	}
}

func TestHandleAPIQueryLogsMethodNotAllowed(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/api/query-logs", nil)
	w := httptest.NewRecorder()

	handleAPIQueryLogs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleAPIUpstreamsCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// GET — empty list
	req := httptest.NewRequest("GET", "/api/upstreams", nil)
	w := httptest.NewRecorder()
	handleAPIUpstreams(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET, got %d", w.Code)
	}

	// POST — create upstream
	body := bytes.NewBufferString(`{"upstream":"9.9.9.9:53","enabled":true}`)
	req = httptest.NewRequest("POST", "/api/upstreams", body)
	w = httptest.NewRecorder()
	handleAPIUpstreams(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on POST, got %d", w.Code)
	}

	// GET — should have 1 upstream now
	req = httptest.NewRequest("GET", "/api/upstreams", nil)
	w = httptest.NewRecorder()
	handleAPIUpstreams(w, req)

	var resp apiResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	dataSlice, ok := resp.Data.([]interface{})
	if !ok {
		t.Fatalf("expected data to be an array, got %T", resp.Data)
	}
	if len(dataSlice) != 1 {
		t.Errorf("expected 1 upstream after POST, got %d", len(dataSlice))
	}
	first := requireFixtureType[map[string]interface{}](t, dataSlice[0])
	upstreamID := int(requireFixtureType[float64](t, first["id"]))

	// PUT — update
	body = bytes.NewBufferString(`{"upstream":"tls://dns.quad9.net"}`)
	req = httptest.NewRequest("PUT", "/api/upstreams/"+itoa(upstreamID), body)
	w = httptest.NewRecorder()
	handleAPIUpstreamAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT, got %d", w.Code)
	}

	// DELETE
	req = httptest.NewRequest("DELETE", "/api/upstreams/"+itoa(upstreamID), nil)
	w = httptest.NewRecorder()
	handleAPIUpstreamAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", w.Code)
	}

	// Verify deletion
	all := upstreamStorage.getAll()
	if len(all) != 0 {
		t.Errorf("expected 0 upstreams after DELETE, got %d", len(all))
	}
}

func TestHandleAPIUpstreamsInvalidMethod(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("DELETE", "/api/upstreams", nil)
	w := httptest.NewRecorder()
	handleAPIUpstreams(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestHandleAPIRewritesCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// GET — empty
	req := httptest.NewRequest("GET", "/api/rewrites", nil)
	w := httptest.NewRecorder()
	handleAPIRewritesRouter(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	data, _ := decodeAPIResponse(t, w.Body)
	rewrites := requireFixtureType[[]interface{}](t, data)
	if len(rewrites) != 0 {
		t.Errorf("expected 0 rewrites, got %d", len(rewrites))
	}

	// POST — create
	body := bytes.NewBufferString(`{"domain":"grafana.example.lan","ip_addresses":"10.42.1.5","enabled":true}`)
	req = httptest.NewRequest("POST", "/api/rewrites", body)
	w = httptest.NewRecorder()
	handleAPIRewritesRouter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on POST, got %d", w.Code)
	}

	// GET — should have 1 now
	req = httptest.NewRequest("GET", "/api/rewrites", nil)
	w = httptest.NewRecorder()
	handleAPIRewritesRouter(w, req)

	data, _ = decodeAPIResponse(t, w.Body)
	rewrites = requireFixtureType[[]interface{}](t, data)
	if len(rewrites) != 1 {
		t.Errorf("expected 1 rewrite after POST, got %d", len(rewrites))
	}
}

func TestHandleAPISettingUpdate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Update cache_ttl
	body := bytes.NewBufferString(`{"value":"7200"}`)
	req := httptest.NewRequest("PUT", "/api/settings/cache_ttl", body)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify via GET
	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	handleAPIGetSettings(w, req)

	data, _ := decodeAPIResponse(t, w.Body)
	settings := requireFixtureType[map[string]interface{}](t, data)
	if settings["cache_ttl"] != "7200" {
		t.Errorf("expected cache_ttl '7200', got %v", settings["cache_ttl"])
	}
	if ttl := getCacheTTL(); ttl != 7200 {
		t.Errorf("expected runtime cache_ttl 7200, got %d", ttl)
	}
}

func TestHandleAPISettingTreatsSyncSecretAsWriteOnly(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	defer closeSync()

	const replacement = "0123456789abcdef0123456789abcdef"
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/settings/sync_secret",
		bytes.NewBufferString(`{"value":"`+replacement+`"}`),
	)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), replacement) {
		t.Fatal("sync secret was echoed in setting update response")
	}
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q, want nil", *apiErr)
	}
	response := requireFixtureType[map[string]interface{}](t, data)
	if _, ok := response["value"]; ok {
		t.Fatalf("write-only response contains value: %v", response["value"])
	}
	if response["updated"] != true {
		t.Fatalf("updated = %v, want true", response["updated"])
	}
	if got := getSyncSetting("sync_secret"); got != replacement {
		t.Fatalf("stored sync_secret = %q, want replacement", got)
	}
}

func TestHandleAPISettingRejectsEmptySyncSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'existing-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Fatalf("seed sync secret: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/settings/sync_secret",
		bytes.NewBufferString(`{"value":"   "}`),
	)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if got := getSyncSetting("sync_secret"); got != "existing-secret" {
		t.Fatalf("stored sync_secret = %q, want unchanged", got)
	}
}

func TestHandleAPISettingRejectsShortSyncSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'existing-secret' WHERE key = 'sync_secret'"); err != nil {
		t.Fatalf("seed sync secret: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/settings/sync_secret",
		bytes.NewBufferString(`{"value":"too-short"}`),
	)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if got := getSyncSetting("sync_secret"); got != "existing-secret" {
		t.Fatalf("stored sync_secret = %q, want unchanged", got)
	}
}

func TestHandleAPISettingRejectsSessionSecretUpdate(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = 'existing-session-secret' WHERE key = 'session_secret'"); err != nil {
		t.Fatalf("seed session secret: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/settings/session_secret",
		bytes.NewBufferString(`{"value":"attacker-controlled"}`),
	)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if got := getSyncSetting("session_secret"); got != "existing-session-secret" {
		t.Fatalf("stored session_secret = %q, want unchanged", got)
	}
}

func TestHandleAPISettingWrongMethod(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/settings/cache_ttl", nil)
	w := httptest.NewRecorder()
	handleAPISetting(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET on /api/settings/{key}, got %d", w.Code)
	}
}

func TestHandleAPIGroupsCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// POST — create
	body := bytes.NewBufferString(`{"name":"Test Group"}`)
	req := httptest.NewRequest("POST", "/api/groups", body)
	w := httptest.NewRecorder()
	handleAPIGroupsRouter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	// GET — should have 1
	req = httptest.NewRequest("GET", "/api/groups", nil)
	w = httptest.NewRecorder()
	handleAPIGroupsRouter(w, req)

	data, _ := decodeAPIResponse(t, w.Body)
	groups := requireFixtureType[[]interface{}](t, data)
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
	}
}

func TestHandleAPICacheClear(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Add something to cache
	msg := newTestDNSMsg("test.com", 1)
	cache.set(testCacheKey("test.com", 1), msg, 3600)

	stats := getCacheStats()
	if stats.Entries != 1 {
		t.Fatalf("expected 1 cache entry, got %d", stats.Entries)
	}

	// Clear via API
	req := httptest.NewRequest("POST", "/api/cache/clear", nil)
	w := httptest.NewRecorder()
	handleAPICacheClear(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	stats = getCacheStats()
	if stats.Entries != 0 {
		t.Errorf("expected 0 cache entries after clear, got %d", stats.Entries)
	}
}

func TestHandleAPICacheClearWrongMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/cache/clear", nil)
	w := httptest.NewRecorder()
	handleAPICacheClear(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", w.Code)
	}
}

func TestWriteErrorFormat(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}

	_, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr == nil {
		t.Fatal("expected error in response")
	}
	if *apiErr != "invalid input" {
		t.Errorf("expected error 'invalid input', got %q", *apiErr)
	}
}

func TestMalformedJSONReturns400(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	garbage := bytes.NewBufferString(`{this is not json`)

	tests := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"POST /api/upstreams", "POST", "/api/upstreams", handleAPIUpstreams},
		{"PUT /api/upstreams/1", "PUT", "/api/upstreams/1", handleAPIUpstreamAction},
		{"PUT /api/settings/cache_ttl", "PUT", "/api/settings/cache_ttl", handleAPISetting},
		{"POST /api/groups", "POST", "/api/groups", handleAPIGroupsRouter},
		{"POST /api/rewrites", "POST", "/api/rewrites", handleAPIRewritesRouter},
		{"POST /api/blocklists", "POST", "/api/blocklists", handleAPIBlocklists},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(garbage.String())
			req := httptest.NewRequest(tt.method, tt.path, body)
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
			}
		})
	}
}

// TestOversizedRequestBodyReturns413 verifies the MaxBytesReader caps added
// to admin mutation endpoints actually bound the request body and are
// reported as 413, distinct from a merely malformed body (400).
func TestOversizedRequestBodyReturns413(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tests := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
		size    int
	}{
		{"PUT /api/settings/cache_ttl", "PUT", "/api/settings/cache_ttl", handleAPISetting, maxSmallAdminRequestBodyBytes},
		{"POST /api/groups", "POST", "/api/groups", handleAPIGroups, maxSmallAdminRequestBodyBytes},
		{"POST /api/rewrites", "POST", "/api/rewrites", handleAPIRewrites, maxSmallAdminRequestBodyBytes},
		{"POST /api/users", "POST", "/api/users", handleAPIUsersRouter, maxSmallAdminRequestBodyBytes},
		{"POST /api/tokens", "POST", "/api/tokens", handleAPITokensRouter, maxSmallAdminRequestBodyBytes},
		{"POST /api/investigate", "POST", "/api/investigate", handleAPIInvestigate, maxInvestigateRequestBodyBytes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Oversized but otherwise well-formed-looking JSON so the only
			// possible rejection reason is the body size cap, not parsing.
			oversized := `{"padding":"` + strings.Repeat("a", tt.size+1024) + `"}`
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(oversized))
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Errorf("expected 413 for oversized body, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// itoa is a test helper to convert int to string for URL building
func itoa(n int) string {
	return strconv.Itoa(n)
}
