package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"

	"fmt"

	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testSyncTLSServerName = "svart-dns.example.lan"
	testSyncSecret        = "test-sync-secret-32-bytes-minimum"
	testSyncServerTime    = "2026-07-10T20:00:00Z"
	testCanonicalBlueURL  = "https://svart-a.example.lan:443"
	testCanonicalRedURL   = "https://svart-b.example.lan:8443"
	testPairingCode       = "one-time-code"
	// #nosec G101 -- Synthetic authentication/redaction fixture or diagnostic text; not a deployable credential.
	testPairingSecretErr = "peer pairing response contained prohibited authentication material"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCanonicalPeerURLNormalizesExactDestination(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "default port and lowercase DNS name",
			raw:  "  https://SVART-A.EXAMPLE.LAN  ",
			want: testCanonicalBlueURL,
		},
		{
			name: "explicit non-default port and root path",
			raw:  "https://SVART-B.EXAMPLE.LAN:8443/",
			want: testCanonicalRedURL,
		},
		{
			name: "IPv4 default port",
			raw:  "https://10.42.1.7",
			want: "https://10.42.1.7:443",
		},
		{
			name: "IPv6 brackets and lowercase",
			raw:  "https://[2001:DB8::7]:9443",
			want: "https://[2001:db8::7]:9443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalPeerURL(tt.raw)

			if err != nil {
				t.Fatalf("canonicalPeerURL(%q) error = %v, want nil", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("canonicalPeerURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalPeerURLRejectsUnsafeOrMalformedDestinations(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "HTTP",
			raw:     "http://svart-a.example.lan:443",
			wantErr: "sync peer URL must use https: http://svart-a.example.lan:443",
		},
		{
			name:    "path",
			raw:     "https://svart-a.example.lan/api/sync",
			wantErr: "sync peer URL must contain only an HTTPS host and port: https://svart-a.example.lan/api/sync",
		},
		{
			name:    "query",
			raw:     "https://svart-a.example.lan?redirect=https://attacker.example",
			wantErr: "sync peer URL must contain only an HTTPS host and port: https://svart-a.example.lan?redirect=https://attacker.example",
		},
		{
			name:    "userinfo",
			raw:     "https://svart-a.example.lan@attacker.example",
			wantErr: "sync peer URL must contain only an HTTPS host and port: https://svart-a.example.lan@attacker.example",
		},
		{
			name:    "fragment",
			raw:     "https://svart-a.example.lan#attacker.example",
			wantErr: "sync peer URL must contain only an HTTPS host and port: https://svart-a.example.lan#attacker.example",
		},
		{
			name:    "missing host",
			raw:     "https://",
			wantErr: "sync peer URL must use https: https://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalPeerURL(tt.raw)

			if got != "" {
				t.Fatalf("canonicalPeerURL(%q) = %q, want empty string", tt.raw, got)
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("canonicalPeerURL(%q) error = %v, want %q", tt.raw, err, tt.wantErr)
			}
		})
	}
}

func TestParseSyncPeerAllowlistCanonicalizesAndDeduplicates(t *testing.T) {
	const raw = " https://SVART-A.EXAMPLE.LAN, https://svart-b.example.lan:8443/, https://svart-a.example.lan:443 "
	want := map[string]struct{}{
		testCanonicalBlueURL: {},
		testCanonicalRedURL:  {},
	}

	got, err := parseSyncPeerAllowlist(raw)

	if err != nil {
		t.Fatalf("parseSyncPeerAllowlist error = %v, want nil", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSyncPeerAllowlist = %#v, want %#v", got, want)
	}
}

func TestParseSyncPeerAllowlistEmptyMeansNotConfigured(t *testing.T) {
	got, err := parseSyncPeerAllowlist(" \t\n ")

	if err != nil {
		t.Fatalf("parseSyncPeerAllowlist error = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("parseSyncPeerAllowlist = %#v, want nil", got)
	}
}

func TestNewSyncHTTPClientRejectsPeerWhenAllowlistIsNotConfigured(t *testing.T) {
	savedAllowedPeers := syncAllowedPeers
	syncAllowedPeers = nil
	defer func() { syncAllowedPeers = savedAllowedPeers }()

	client, err := newSyncHTTPClient(testCanonicalBlueURL)

	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
	if err == nil || err.Error() != "SYNC_PEER_ALLOWLIST is required for peer sync" {
		t.Fatalf("error = %v, want required peer allowlist error", err)
	}
}

func TestParseSyncPeerAllowlistRejectsWholeListWhenAnyEntryIsUnsafe(t *testing.T) {
	const raw = "https://svart-a.example.lan,https://attacker.example/api/sync"
	const wantErr = "invalid SYNC_PEER_ALLOWLIST entry: sync peer URL must contain only an HTTPS host and port: https://attacker.example/api/sync"

	got, err := parseSyncPeerAllowlist(raw)

	if got != nil {
		t.Fatalf("parseSyncPeerAllowlist = %#v, want nil", got)
	}
	if err == nil || err.Error() != wantErr {
		t.Fatalf("parseSyncPeerAllowlist error = %v, want %q", err, wantErr)
	}
}

func TestMalformedSyncPeerAllowlistDisablesSyncWithoutFailingHealth(t *testing.T) {
	const raw = "https://svart-a.example.lan,https://attacker.example/api/sync"
	const wantParseErr = "invalid SYNC_PEER_ALLOWLIST entry: sync peer URL must contain only an HTTPS host and port: https://attacker.example/api/sync"
	const wantTLSErr = "sync TLS trust configuration unavailable: " + wantParseErr

	allowed, parseErr := parseSyncPeerAllowlist(raw)
	if allowed != nil {
		t.Fatalf("parseSyncPeerAllowlist = %#v, want nil", allowed)
	}
	if parseErr == nil || parseErr.Error() != wantParseErr {
		t.Fatalf("parseSyncPeerAllowlist error = %v, want %q", parseErr, wantParseErr)
	}

	savedAllowedPeers := syncAllowedPeers
	savedConfigErr := syncTLSConfigErr
	syncAllowedPeers = allowed
	syncTLSConfigErr = parseErr
	defer func() {
		syncAllowedPeers = savedAllowedPeers
		syncTLSConfigErr = savedConfigErr
	}()

	config, err := newSyncTLSConfig(testCanonicalBlueURL)
	if config != nil {
		t.Fatalf("newSyncTLSConfig = %v, want nil", config)
	}
	if err == nil || err.Error() != wantTLSErr {
		t.Fatalf("newSyncTLSConfig error = %v, want %q", err, wantTLSErr)
	}

	w := httptest.NewRecorder()
	handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestNewSyncHTTPClientRejectsIPPeerWithoutServerName(t *testing.T) {
	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	syncTLSServerName = ""
	syncAllowedPeers = map[string]struct{}{"https://10.42.1.6:443": {}}
	defer func() {
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
	}()

	client, err := newSyncHTTPClient("https://10.42.1.6:443")

	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
	if err == nil || err.Error() != "SYNC_TLS_SERVER_NAME is required for IP peer https://10.42.1.6:443" {
		t.Fatalf("error = %v, want missing TLS server name error", err)
	}
}

func TestNewSyncTLSConfigCannotInheritVerificationBypass(t *testing.T) {
	savedConfig := syncTLSConfig
	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	// #nosec G402 -- Deliberately unsafe input verifies that production transport cloning clears InsecureSkipVerify.
	syncTLSConfig = &tls.Config{InsecureSkipVerify: true} // Regression guard: clones must clear this.
	syncTLSServerName = testSyncTLSServerName
	syncAllowedPeers = map[string]struct{}{"https://10.42.1.6:443": {}}
	defer func() {
		syncTLSConfig = savedConfig
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
	}()

	config, err := newSyncTLSConfig("https://10.42.1.6:443")
	if err != nil {
		t.Fatalf("newSyncTLSConfig: %v", err)
	}
	if config.InsecureSkipVerify {
		t.Fatal("sync TLS config inherited InsecureSkipVerify")
	}
}

func TestNewSyncHTTPClientRejectsDestinationOutsidePeerAllowlist(t *testing.T) {
	savedAllowedPeers := syncAllowedPeers
	savedName := syncTLSServerName
	syncAllowedPeers = map[string]struct{}{
		"https://10.42.1.6:443": {},
	}
	syncTLSServerName = testSyncTLSServerName
	defer func() {
		syncAllowedPeers = savedAllowedPeers
		syncTLSServerName = savedName
	}()

	client, err := newSyncHTTPClient("https://10.42.1.99:443")
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
	if err == nil || !strings.Contains(err.Error(), "not present in SYNC_PEER_ALLOWLIST") {
		t.Fatalf("error = %v, want peer allowlist rejection", err)
	}
}

func TestHandleAPIPeersPostRejectsDestinationOutsidePeerAllowlist(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	savedAllowedPeers := syncAllowedPeers
	savedName := syncTLSServerName
	savedCert := tlsCert
	savedKey := tlsKey
	syncAllowedPeers = map[string]struct{}{
		"https://10.42.1.6:443": {},
	}
	syncTLSServerName = testSyncTLSServerName
	tlsCert = "/test/fullchain.pem"
	tlsKey = "/test/privkey.pem"
	defer func() {
		syncAllowedPeers = savedAllowedPeers
		syncTLSServerName = savedName
		tlsCert = savedCert
		tlsKey = savedKey
	}()
	if _, err := db.Exec("UPDATE settings SET value = 'test-node' WHERE key = 'node_name'"); err != nil {
		t.Fatalf("set node name: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/peers", strings.NewReader(`{"url":"https://10.42.1.99:443"}`))
	w := httptest.NewRecorder()
	handleAPIPeersPost(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if peers := getSyncSetting("sync_peers"); peers != "" {
		t.Fatalf("sync_peers = %q, want empty after allowlist rejection", peers)
	}
}

func TestPairingProofDoesNotRevealSecretAndBindsTranscript(t *testing.T) {
	const (
		secret  = "0123456789abcdef0123456789abcdef"
		code    = "pairing-code"
		peerURL = "https://10.42.1.7:443"
	)

	proof := makePairingProof(secret, "confirmer", code, peerURL)
	if proof == "" || strings.Contains(proof, secret) {
		t.Fatal("pairing proof is empty or contains the raw secret")
	}
	if !verifyPairingProof(secret, proof, "confirmer", code, peerURL) {
		t.Fatal("valid pairing proof was rejected")
	}
	if verifyPairingProof("different-secret-0123456789abcdef", proof, "confirmer", code, peerURL) {
		t.Fatal("pairing proof accepted a different secret")
	}
	if verifyPairingProof(secret, proof, "initiator", code, peerURL) {
		t.Fatal("pairing proof accepted a different role")
	}
	if verifyPairingProof(secret, proof, "confirmer", code, "https://10.42.1.8:443") {
		t.Fatal("pairing proof accepted a different peer URL")
	}
}

func TestLoadSyncTLSConfigRejectsMalformedCA(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "invalid-ca.pem")
	if err := os.WriteFile(caPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("write malformed CA: %v", err)
	}

	config, err := loadSyncTLSConfig(caPath)
	if config != nil {
		t.Fatalf("config = %v, want nil", config)
	}
	if err == nil || !strings.Contains(err.Error(), "contains no valid certificates") {
		t.Fatalf("error = %v, want invalid CA error", err)
	}
}

func TestSyncTLSConfigurationErrorDisablesSyncWithoutFailingHealth(t *testing.T) {
	savedError := syncTLSConfigErr
	syncTLSConfigErr = fmt.Errorf("test CA failure")
	defer func() { syncTLSConfigErr = savedError }()

	client, err := newSyncHTTPClient("https://peer.example.com:443")
	if client != nil {
		t.Fatalf("client = %v, want nil", client)
	}
	if err == nil || !strings.Contains(err.Error(), "test CA failure") {
		t.Fatalf("error = %v, want sync trust configuration error", err)
	}

	w := httptest.NewRecorder()
	handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", w.Code)
	}
}

func TestSyncOnceAcceptsTrustedCertificateForConfiguredServerName(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server, roots, requests, observedSecret := newNamedTLSServer(t, testSyncTLSServerName)
	defer server.Close()
	restore := configureSyncTLSForTest(roots, testSyncTLSServerName, server.URL)
	defer restore()

	client, err := newSyncHTTPClient(server.URL)
	if err != nil {
		t.Fatalf("newSyncHTTPClient: %v", err)
	}
	peer := &peerState{URL: server.URL}

	syncOnce(client, peer, testSyncSecret)

	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
	if observedSecret.Load() != testSyncSecret {
		t.Fatalf("observed secret = %q, want %q", observedSecret.Load(), testSyncSecret)
	}
	if !peer.Healthy {
		t.Fatalf("peer healthy = false, want true; last error = %q", peer.LastError)
	}
	if peer.ConsecutiveErrors != 0 {
		t.Fatalf("consecutive errors = %d, want 0", peer.ConsecutiveErrors)
	}
	if got := peer.LastSyncAt.UTC().Format(time.RFC3339); got != testSyncServerTime {
		t.Fatalf("last sync = %q, want %q", got, testSyncServerTime)
	}
}

func TestSyncOnceRejectsUntrustedCertificateBeforeSendingSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server, _, requests, _ := newNamedTLSServer(t, testSyncTLSServerName)
	defer server.Close()
	unrelatedServer, unrelatedRoots, _, _ := newNamedTLSServer(t, "unrelated.example.lan")
	defer unrelatedServer.Close()
	restore := configureSyncTLSForTest(unrelatedRoots, testSyncTLSServerName, server.URL)
	defer restore()

	client, err := newSyncHTTPClient(server.URL)
	if err != nil {
		t.Fatalf("newSyncHTTPClient: %v", err)
	}
	peer := &peerState{URL: server.URL, Healthy: true}

	syncOnce(client, peer, testSyncSecret)

	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
	if peer.Healthy {
		t.Fatal("peer healthy = true, want false")
	}
	if peer.ConsecutiveErrors != 1 {
		t.Fatalf("consecutive errors = %d, want 1", peer.ConsecutiveErrors)
	}
	if peer.LastError != "peer request failed" {
		t.Fatalf("last error = %q, want generic transport failure", peer.LastError)
	}
}

func TestSyncOnceRejectsWrongServerNameBeforeSendingSecret(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	server, roots, requests, _ := newNamedTLSServer(t, testSyncTLSServerName)
	defer server.Close()
	restore := configureSyncTLSForTest(roots, "wrong-name.example.lan", server.URL)
	defer restore()

	client, err := newSyncHTTPClient(server.URL)
	if err != nil {
		t.Fatalf("newSyncHTTPClient: %v", err)
	}
	peer := &peerState{URL: server.URL, Healthy: true}

	syncOnce(client, peer, testSyncSecret)

	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0", requests.Load())
	}
	if peer.Healthy {
		t.Fatal("peer healthy = true, want false")
	}
	if peer.LastError != "peer request failed" {
		t.Fatalf("last error = %q, want generic transport failure", peer.LastError)
	}
}

func TestSyncOnceDoesNotReflectPeerControlledSecretInErrors(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "HTTP body", statusCode: http.StatusUnauthorized, body: testSyncSecret},
		{name: "API envelope", statusCode: http.StatusOK, body: `{"data":null,"error":"` + testSyncSecret + `"}`},
		{name: "server time", statusCode: http.StatusOK, body: `{"data":{"server_time":"` + testSyncSecret + `","changes":{},"tombstones":[]},"error":null}`},
		{name: "successful metadata", statusCode: http.StatusOK, body: `{"data":{"node_id":"remote-node","node_name":"` + testSyncSecret + `","server_time":"` + testSyncServerTime + `","changes":{},"tombstones":[]},"error":null}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				var fixtureErr15746 error
				_, fixtureErr15746 = w.Write([]byte(tt.body))
				if fixtureErr15746 != nil {
					t.Errorf("fixture operation failed: %v", fixtureErr15746)
				}
			}))
			defer server.Close()

			peer := &peerState{URL: server.URL, Healthy: true, MetricLabel: "peer-reflection-test"}
			syncOnce(server.Client(), peer, testSyncSecret)

			if peer.Healthy {
				t.Fatal("peer remained healthy after rejected response")
			}
			if strings.Contains(peer.LastError, testSyncSecret) {
				t.Fatalf("LastError reflects peer-controlled secret: %q", peer.LastError)
			}
		})
	}
}

func TestSyncOnceAcceptsOneExactLegacySyncSecretSettingAndMergesOtherRows(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	legacyTime := time.Now().UTC().Add(time.Minute).Format(time.RFC3339Nano)
	legacyResponse := SyncResponse{
		NodeID:     "legacy-red",
		NodeName:   "svart-b",
		ServerTime: testSyncServerTime,
		Changes: SyncChanges{
			Settings: []SyncSetting{{
				Key:       "sync_secret",
				Value:     testSyncSecret,
				UpdatedAt: legacyTime,
				NodeID:    "legacy-red",
			}},
			Rewrites: []SyncRewrite{{
				Domain:      "legacy-rollout.example",
				IPAddresses: "203.0.113.55",
				Enabled:     true,
				UpdatedAt:   legacyTime,
				NodeID:      "legacy-red",
			}},
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, legacyResponse)
	}))
	defer server.Close()

	peer := &peerState{URL: server.URL, MetricLabel: "peer-legacy-rollout"}
	syncOnce(server.Client(), peer, testSyncSecret)

	if !peer.Healthy || peer.ConsecutiveErrors != 0 {
		t.Fatalf("legacy peer was not accepted: healthy=%v errors=%d last_error=%q", peer.Healthy, peer.ConsecutiveErrors, peer.LastError)
	}
	if peer.LastSyncRows != 1 {
		t.Fatalf("merged row count = %d, want 1 non-secret row", peer.LastSyncRows)
	}
	var ipAddresses string
	if err := db.QueryRow("SELECT ip_addresses FROM rewrites WHERE domain = ?", "legacy-rollout.example").Scan(&ipAddresses); err != nil {
		t.Fatalf("query merged legacy rewrite: %v", err)
	}
	if ipAddresses != "203.0.113.55" {
		t.Fatalf("merged rewrite IPs = %q, want 203.0.113.55", ipAddresses)
	}
}

func TestSyncOnceRejectsNonExactOrDuplicateLegacySyncSecretSettings(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tests := []struct {
		name     string
		settings []SyncSetting
	}{
		{
			name: "non-exact value",
			settings: []SyncSetting{{
				Key: "sync_secret", Value: testSyncSecret + "-reflected",
			}},
		},
		{
			name: "duplicate exact values",
			settings: []SyncSetting{
				{Key: "sync_secret", Value: testSyncSecret},
				{Key: "sync_secret", Value: testSyncSecret},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacyResponse := SyncResponse{
				NodeID:     "legacy-red",
				ServerTime: testSyncServerTime,
				Changes:    SyncChanges{Settings: tt.settings},
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, legacyResponse)
			}))
			defer server.Close()

			peer := &peerState{URL: server.URL, Healthy: true, MetricLabel: "peer-invalid-legacy-rollout"}
			syncOnce(server.Client(), peer, testSyncSecret)
			if peer.Healthy {
				t.Fatal("invalid legacy secret setting was accepted")
			}
			if peer.LastError != "peer response contained prohibited authentication material" {
				t.Fatalf("LastError = %q, want generic prohibited-material error", peer.LastError)
			}
			if strings.Contains(peer.LastError, testSyncSecret) {
				t.Fatal("invalid legacy response reflected the secret in retained state")
			}
		})
	}
}

func TestSyncOnceDoesNotStorePeerControlledTransportError(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("malformed peer response: %s", testSyncSecret)
	})}
	peer := &peerState{URL: "https://peer.example.com:443", Healthy: true, MetricLabel: "peer-transport-test"}

	syncOnce(client, peer, testSyncSecret)

	if strings.Contains(peer.LastError, testSyncSecret) {
		t.Fatalf("LastError reflected peer-controlled transport text: %q", peer.LastError)
	}
	if peer.LastError != "peer request failed" {
		t.Fatalf("LastError = %q, want generic transport error", peer.LastError)
	}
}

func newNamedTLSServer(t *testing.T, dnsName string, customHandlers ...http.Handler) (*httptest.Server, *x509.CertPool, *atomic.Int32, *atomic.Value) {
	t.Helper()

	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "svart test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}

	requests := &atomic.Int32{}
	observedSecret := &atomic.Value{}
	observedSecret.Store("")
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		observedSecret.Store(r.Header.Get("X-Sync-Key"))
		w.Header().Set("Content-Type", "application/json")
		var fixtureErr39040 error
		_, fixtureErr39040 = w.Write([]byte(`{"data":{"node_id":"remote-node","server_time":"` + testSyncServerTime + `","changes":{},"tombstones":[]},"error":null}`))
		if fixtureErr39040 != nil {
			t.Errorf("fixture operation failed: %v", fixtureErr39040)
		}
	})
	if len(customHandlers) > 0 {
		handler = customHandlers[0]
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{
		Certificate: [][]byte{leafDER, caDER},
		PrivateKey:  leafKey,
		Leaf:        nil,
	}}}
	server.StartTLS()

	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	return server, roots, requests, observedSecret
}

func configureSyncTLSForTest(roots *x509.CertPool, serverName string, peerURLs ...string) func() {
	savedConfig := syncTLSConfig
	savedName := syncTLSServerName
	savedAllowedPeers := syncAllowedPeers
	syncTLSConfig = &tls.Config{RootCAs: roots}
	syncTLSServerName = serverName
	syncAllowedPeers = make(map[string]struct{}, len(peerURLs))
	for _, peerURL := range peerURLs {
		canonical, err := canonicalPeerURL(peerURL)
		if err != nil {
			panic(err)
		}
		syncAllowedPeers[canonical] = struct{}{}
	}
	return func() {
		syncTLSConfig = savedConfig
		syncTLSServerName = savedName
		syncAllowedPeers = savedAllowedPeers
	}
}

func configurePairingGlobalsForTest() func() {
	savedExternalIP := externalIP
	savedAdminPort := adminPort
	savedTLSCert := tlsCert
	savedTLSKey := tlsKey
	externalIP = "10.42.1.6"
	adminPort = "443"
	tlsCert = "/test/fullchain.pem"
	tlsKey = "/test/privkey.pem"
	return func() {
		externalIP = savedExternalIP
		adminPort = savedAdminPort
		tlsCert = savedTLSCert
		tlsKey = savedTLSKey
	}
}
