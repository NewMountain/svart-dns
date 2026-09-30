package main

import (
	"bytes"

	"crypto/tls"

	"encoding/json"

	"io"
	"log/slog"

	"net/http"
	"net/http/httptest"

	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHandleAPIPeersConfirmRejectsUntrustedTLSBeforeSendingSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server, _, requests, _ := newNamedTLSServer(t, testSyncTLSServerName)
	defer server.Close()
	unrelatedServer, unrelatedRoots, _, _ := newNamedTLSServer(t, "unrelated.example.lan")
	defer unrelatedServer.Close()
	restoreTLS := configureSyncTLSForTest(unrelatedRoots, testSyncTLSServerName, server.URL)
	defer restoreTLS()
	restoreGlobals := configurePairingGlobalsForTest()
	defer restoreGlobals()

	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}
	body, err := json.Marshal(map[string]string{
		"peer_url":     server.URL,
		"pairing_code": "one-time-code",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/peers/confirm", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	handleAPIPeersConfirm(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
}

func TestHandleAPIPeersConfirmSendsOnlyProofAndDoesNotReflectPeerBody(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	capturedBody := &atomic.Value{}
	capturedBody.Store("")
	server, roots, _, _ := newNamedTLSServer(t, testSyncTLSServerName, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, fixtureErr21537 := io.ReadAll(r.Body)
		if fixtureErr21537 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr21537)
		}
		capturedBody.Store(string(body))
		w.WriteHeader(http.StatusBadRequest)
		var fixtureErr21643 error
		_, fixtureErr21643 = w.Write([]byte(`{"reflected":"` + testSyncSecret + `"}`))
		if fixtureErr21643 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr21643)
		}
	}))
	defer server.Close()
	restoreTLS := configureSyncTLSForTest(roots, testSyncTLSServerName, server.URL)
	defer restoreTLS()
	restoreGlobals := configurePairingGlobalsForTest()
	defer restoreGlobals()

	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}
	body, err := json.Marshal(map[string]string{
		"peer_url":     server.URL,
		"pairing_code": "one-time-code",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/peers/confirm", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handleAPIPeersConfirm(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), testSyncSecret) {
		t.Fatal("peer-controlled response reflected the sync secret")
	}
	sent := requireFixtureType[string](t, capturedBody.Load())
	if strings.Contains(sent, testSyncSecret) || strings.Contains(sent, "sync_secret") {
		t.Fatalf("pairing request exposed raw sync secret: %s", sent)
	}
	if !strings.Contains(sent, `"proof"`) {
		t.Fatalf("pairing request did not contain proof: %s", sent)
	}
}

func TestHandleAPIPeersConfirmRejectsSecretInSuccessfulPeerMetadata(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const confirmerURL = "https://10.42.1.6:443"
	proof := makePairingProof(testSyncSecret, "initiator", testPairingCode, confirmerURL)
	server, roots, _, _ := newNamedTLSServer(t, testSyncTLSServerName, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"node_id": "remote-" + testSyncSecret,
			"proof":   proof,
		})
	}))
	defer server.Close()
	restoreTLS := configureSyncTLSForTest(roots, testSyncTLSServerName, server.URL)
	defer restoreTLS()
	restoreGlobals := configurePairingGlobalsForTest()
	defer restoreGlobals()
	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}

	var logOutput bytes.Buffer
	savedLogSync := logSync
	logSync = slog.New(slog.NewTextHandler(&logOutput, &slog.HandlerOptions{Level: slog.LevelDebug}))
	defer func() { logSync = savedLogSync }()

	syncMu.Lock()
	savedPeerStates := peerStates
	savedSyncDone := syncDone
	peerStates = nil
	syncDone = nil
	syncMu.Unlock()
	defer func() {
		closeSync()
		syncMu.Lock()
		peerStates = savedPeerStates
		syncDone = savedSyncDone
		syncMu.Unlock()
	}()

	body, err := json.Marshal(map[string]string{
		"peer_url":     server.URL,
		"pairing_code": testPairingCode,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	handleAPIPeersConfirm(w, httptest.NewRequest(http.MethodPost, "/api/peers/confirm", strings.NewReader(string(body))))

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	responseBody := w.Body.String()
	data, apiErr := decodeAPIResponse(t, bytes.NewBufferString(responseBody))
	if data != nil {
		t.Errorf("response data = %#v, want nil", data)
	}
	if apiErr == nil || *apiErr != testPairingSecretErr {
		t.Errorf("response error = %v, want %q", apiErr, testPairingSecretErr)
	}
	if strings.Contains(responseBody, testSyncSecret) {
		t.Errorf("response reflected sync secret: %s", responseBody)
	}
	if peers := getSyncSetting("sync_peers"); peers != "" {
		t.Errorf("sync_peers = %q, want empty", peers)
	}
	if deletedPeers := getSyncSetting("deleted_peers"); deletedPeers != "" {
		t.Errorf("deleted_peers = %q, want empty", deletedPeers)
	}
	syncMu.Lock()
	peerCount := len(peerStates)
	syncMu.Unlock()
	if peerCount != 0 {
		t.Errorf("peer state count = %d, want 0", peerCount)
	}
	if strings.Contains(logOutput.String(), testSyncSecret) {
		t.Errorf("logs reflected sync secret: %s", logOutput.String())
	}
}

func TestHandleAPIPeersConfirmRejectsOversizedSuccessfulResponse(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	const pairingCode = "one-time-code"
	self := "https://10.42.1.6:443"
	proof := makePairingProof(testSyncSecret, "initiator", pairingCode, self)
	response := `{"data":{"success":true,"node_id":"remote-node","proof":"` + proof + `"},"error":null}`
	server, roots, _, _ := newNamedTLSServer(t, testSyncTLSServerName, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var fixtureErr26214 error
		_, fixtureErr26214 = io.WriteString(w, response)
		checkRejectedResponseWrite(t, fixtureErr26214)
		var fixtureErr26251 error
		_, fixtureErr26251 = io.WriteString(w, strings.Repeat(" ", maxPairingBodyBytes+1))
		checkRejectedResponseWrite(t, fixtureErr26251)
	}))
	defer server.Close()
	restoreTLS := configureSyncTLSForTest(roots, testSyncTLSServerName, server.URL)
	defer restoreTLS()
	restoreGlobals := configurePairingGlobalsForTest()
	defer restoreGlobals()
	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}

	body, err := json.Marshal(map[string]string{"peer_url": server.URL, "pairing_code": pairingCode})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	w := httptest.NewRecorder()
	handleAPIPeersConfirm(w, httptest.NewRequest(http.MethodPost, "/api/peers/confirm", strings.NewReader(string(body))))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
	if peers := getSyncSetting("sync_peers"); peers != "" {
		t.Fatalf("sync_peers = %q, want empty after oversized response", peers)
	}
}

func TestHandleAPIPeersGetReportsMissingTLSIdentity(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}

	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	savedCert := tlsCert
	savedKey := tlsKey
	syncTLSServerName = ""
	syncAllowedPeers = map[string]struct{}{"https://10.42.1.7:443": {}}
	tlsCert = "/test/fullchain.pem"
	tlsKey = "/test/privkey.pem"
	defer func() {
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
		tlsCert = savedCert
		tlsKey = savedKey
	}()
	syncMu.Lock()
	savedPeerStates := peerStates
	peerStates = []*peerState{{URL: "https://10.42.1.7:443", Healthy: false}}
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		peerStates = savedPeerStates
		syncMu.Unlock()
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
	w := httptest.NewRecorder()
	handleAPIPeersGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q, want nil", *apiErr)
	}
	response := requireFixtureType[map[string]interface{}](t, data)
	if response["sync_tls_ready"] != false {
		t.Fatalf("sync_tls_ready = %v, want false", response["sync_tls_ready"])
	}
	const expectedError = "SYNC_TLS_SERVER_NAME is required for IP peer https://10.42.1.7:443"
	if response["sync_tls_error"] != expectedError {
		t.Fatalf("sync_tls_error = %q, want %q", response["sync_tls_error"], expectedError)
	}
}

func TestHandleAPIPeersGetRequiresObservedVerifiedSyncForTLSReady(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}

	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	savedCert := tlsCert
	savedKey := tlsKey
	syncTLSServerName = testSyncTLSServerName
	syncAllowedPeers = map[string]struct{}{"https://10.42.1.7:443": {}}
	tlsCert = "/test/fullchain.pem"
	tlsKey = "/test/privkey.pem"
	defer func() {
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
		tlsCert = savedCert
		tlsKey = savedKey
	}()
	peer := &peerState{URL: "https://10.42.1.7:443", Healthy: true}
	syncMu.Lock()
	savedPeerStates := peerStates
	peerStates = []*peerState{peer}
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		peerStates = savedPeerStates
		syncMu.Unlock()
	}()

	request := func() map[string]interface{} {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/peers", nil)
		w := httptest.NewRecorder()
		handleAPIPeersGet(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		data, apiErr := decodeAPIResponse(t, w.Body)
		if apiErr != nil {
			t.Fatalf("API error = %q, want nil", *apiErr)
		}
		return requireFixtureType[map[string]interface{}](t, data)
	}

	beforeSync := request()
	if beforeSync["sync_tls_ready"] != false {
		t.Fatalf("sync_tls_ready before verified sync = %v, want false", beforeSync["sync_tls_ready"])
	}
	if got := beforeSync["sync_tls_error"]; got != "peer https://10.42.1.7:443 has not completed a verified sync" {
		t.Fatalf("sync_tls_error = %q, want no verified sync diagnostic", got)
	}

	peer.mu.Lock()
	peer.LastSyncAt = time.Now()
	peer.mu.Unlock()
	afterSync := request()
	if afterSync["sync_tls_ready"] != true {
		t.Fatalf("sync_tls_ready after verified sync = %v, want true; error = %v", afterSync["sync_tls_ready"], afterSync["sync_tls_error"])
	}
}

func TestHandleAPIPeersGetDoesNotReportTLSReadyWithoutPeers(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}
	syncMu.Lock()
	savedPeerStates := peerStates
	peerStates = nil
	syncMu.Unlock()
	defer func() {
		syncMu.Lock()
		peerStates = savedPeerStates
		syncMu.Unlock()
	}()

	w := httptest.NewRecorder()
	handleAPIPeersGet(w, httptest.NewRequest(http.MethodGet, "/api/peers", nil))

	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q, want nil", *apiErr)
	}
	response := requireFixtureType[map[string]interface{}](t, data)
	if response["sync_tls_ready"] != false {
		t.Fatalf("sync_tls_ready = %v, want false with no peers", response["sync_tls_ready"])
	}
}

func TestPairCompletePreservesCodeWhenTLSConfigIsInvalid(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}
	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	syncTLSServerName = ""
	syncAllowedPeers = map[string]struct{}{"https://10.42.1.7:443": {}}
	defer func() {
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
	}()
	pairingMu.Lock()
	savedCode := pairingCode
	savedExpiry := pairingExpiry
	pairingCode = "still-valid-code"
	pairingExpiry = time.Now().Add(time.Minute)
	pairingMu.Unlock()
	defer func() {
		pairingMu.Lock()
		pairingCode = savedCode
		pairingExpiry = savedExpiry
		pairingMu.Unlock()
	}()

	body, err := json.Marshal(map[string]string{
		"pairing_code": "still-valid-code",
		"peer_url":     "https://10.42.1.7:443",
		"proof":        "unused-before-tls-validation",
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/pair/complete", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	handleAPISyncPairComplete(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", w.Code, w.Body.String())
	}
	pairingMu.Lock()
	remainingCode := pairingCode
	pairingMu.Unlock()
	if remainingCode != "still-valid-code" {
		t.Fatalf("pairing code = %q, want %q", remainingCode, "still-valid-code")
	}
	if peers := getSyncSetting("sync_peers"); peers != "" {
		t.Fatalf("sync_peers = %q, want empty", peers)
	}
}

func TestPairCompleteRejectsOversizedRequestBody(t *testing.T) {
	body := `{"pairing_code":"` + strings.Repeat("x", maxPairingBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sync/pair/complete", strings.NewReader(body))
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()

	handleAPISyncPairComplete(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"error_code":"request_too_large"`) {
		t.Fatalf("missing size error code: %s", w.Body.String())
	}
}

func TestPairCompleteRejectsPlainHTTP(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/sync/pair/complete", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	handleAPISyncPairComplete(w, req)

	if w.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want 426; body = %s", w.Code, w.Body.String())
	}
}

func TestSyncOnceRejectsOversizedPeerResponse(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var fixtureErr34573 error
		_, fixtureErr34573 = io.WriteString(w, strings.Repeat(" ", maxSyncResponseBytes+1))
		checkRejectedResponseWrite(t, fixtureErr34573)
	}))
	defer server.Close()

	peer := &peerState{URL: server.URL, Healthy: true, MetricLabel: "peer-size-test"}
	syncOnce(server.Client(), peer, testSyncSecret)

	if peer.Healthy {
		t.Fatal("peer remained healthy after oversized response")
	}
	if peer.LastError != "peer response exceeded the size limit" {
		t.Fatalf("LastError = %q, want generic size-limit error", peer.LastError)
	}
}

func TestPairCompleteDoesNotReturnSyncSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server, roots, _, _ := newNamedTLSServer(t, testSyncTLSServerName)
	defer server.Close()
	restoreTLS := configureSyncTLSForTest(roots, testSyncTLSServerName, server.URL)
	defer restoreTLS()
	savedTLSCert := tlsCert
	savedTLSKey := tlsKey
	tlsCert = ""
	tlsKey = ""
	defer func() {
		tlsCert = savedTLSCert
		tlsKey = savedTLSKey
	}()

	if _, err := db.Exec("UPDATE settings SET value = ? WHERE key = 'sync_secret'", testSyncSecret); err != nil {
		t.Fatalf("set sync secret: %v", err)
	}
	pairingMu.Lock()
	savedCode := pairingCode
	savedExpiry := pairingExpiry
	pairingCode = "single-use-code"
	pairingExpiry = time.Now().Add(time.Minute)
	pairingMu.Unlock()
	defer func() {
		pairingMu.Lock()
		pairingCode = savedCode
		pairingExpiry = savedExpiry
		pairingMu.Unlock()
	}()

	canonicalServerURL, err := canonicalPeerURL(server.URL)
	if err != nil {
		t.Fatalf("canonicalize server URL: %v", err)
	}
	body, err := json.Marshal(map[string]string{
		"pairing_code": "single-use-code",
		"peer_url":     canonicalServerURL,
		"proof":        makePairingProof(testSyncSecret, "confirmer", "single-use-code", canonicalServerURL),
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/sync/pair/complete", strings.NewReader(string(body)))
	req.TLS = &tls.ConnectionState{}
	w := httptest.NewRecorder()
	handleAPISyncPairComplete(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), testSyncSecret) {
		t.Fatal("pair-complete response exposes sync secret")
	}
	data, apiErr := decodeAPIResponse(t, w.Body)
	if apiErr != nil {
		t.Fatalf("API error = %q, want nil", *apiErr)
	}
	response := requireFixtureType[map[string]interface{}](t, data)
	if response["success"] != true {
		t.Fatalf("success = %v, want true", response["success"])
	}
	if _, ok := response["sync_secret"]; ok {
		t.Fatalf("pair-complete response contains sync_secret: %v", response["sync_secret"])
	}
}
