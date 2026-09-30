package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

// --- Peer pairing (initiate/confirm handshake) ---
//
// Flow:
// 1. Admin on node A: POST /api/peers/pair → generates a one-time pairing code, displayed in clear text.
//    Node A stores the code + its own URL. Code expires in 10 minutes.
// 2. Admin on node B: POST /api/peers/confirm → pastes node A's URL + the pairing code.
//    Node B calls node A's /api/sync/pair/complete with a domain-separated HMAC proof.
//    Node A validates the code and proof, then returns a distinct proof of its own.
//    Neither node sends or returns the raw shared secret during pairing.
// 3. Both sides have each other + matching secrets → sync starts.

var (
	pairingMu     sync.Mutex
	pairingCode   string    // one-time code
	pairingExpiry time.Time // when it expires
)

// generatePairingCode creates a random 6-word code using hex.
func generatePairingCode() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(cryptoRand, b); err != nil {
		// Fallback
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// handleAPIPeersPair initiates a pairing. Returns a one-time code valid for 10 minutes.
// Admin calls this on the node they're sitting at, then takes the code to the remote node.
func handleAPIPeersPair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	self := selfURL()
	if self == "" {
		writeError(w, http.StatusBadRequest, "cannot determine this node's external URL (no external IP or TLS not configured)")
		return
	}
	if _, err := newSyncHTTPClient(self); err != nil {
		writeError(w, http.StatusServiceUnavailable, "sync TLS configuration error: "+err.Error())
		return
	}

	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		writeDBError(w, settingsErr)
		return
	}

	secret := settings["sync_secret"]
	if err := validateSyncSecretStrength(secret); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pairingMu.Lock()
	pairingCode = generatePairingCode()
	pairingExpiry = time.Now().Add(10 * time.Minute)
	code := pairingCode
	pairingMu.Unlock()

	logSync.Info("pairing code generated", "expires_in", "10m")
	writeJSON(w, http.StatusOK, PairingCodeResponse{PairingCode: code, SelfURL: self, NodeID: nodeID, ExpiresIn: "10m"})
}

// handleAPIPeersConfirm is called on the remote node. Admin pastes the code + initiator URL.
// This node calls the initiator's /api/sync/pair/complete to validate.
func handleAPIPeersConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req ConfirmPeerRequest
	if !decodeJSONBody(w, r, maxPairingBodyBytes, &req) {
		return
	}
	canonicalPeer, err := canonicalPeerURL(req.PeerURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.PeerURL = canonicalPeer
	req.PairingCode = strings.TrimSpace(req.PairingCode)
	if req.PairingCode == "" {
		writeError(w, http.StatusBadRequest, "pairing code is required")
		return
	}

	self := selfURL()
	if self == "" {
		writeError(w, http.StatusBadRequest, "cannot determine this node's external URL")
		return
	}
	self, err = canonicalPeerURL(self)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "local peer URL is invalid")
		return
	}

	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		writeDBError(w, settingsErr)
		return
	}

	localSecret := settings["sync_secret"]
	if err := validateSyncSecretStrength(localSecret); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Call the initiator to complete the handshake
	body, marshalErr := json.Marshal(map[string]string{
		"pairing_code": req.PairingCode,
		"peer_url":     self,
		"proof":        makePairingProof(localSecret, "confirmer", req.PairingCode, self),
	})
	if marshalErr != nil {
		panic(marshalErr)
	} // A string-only payload is always JSON encodable.
	client, err := newSyncHTTPClient(req.PeerURL)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "sync TLS configuration error: "+err.Error())
		return
	}
	resp, err := client.Post(req.PeerURL+"/api/sync/pair/complete", "application/json", strings.NewReader(string(body)))
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to reach peer")
		return
	}
	defer closeReadResource(resp.Body)

	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("peer rejected pairing (HTTP %d)", resp.StatusCode))
		return
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxPairingBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to read peer response")
		return
	}
	if len(respBody) > maxPairingBodyBytes {
		writeError(w, http.StatusBadGateway, "peer pairing response exceeded the size limit")
		return
	}

	// Parse response. The initiator already verified that the supplied secret
	// matches; it must never echo that secret back in an API response.
	var completeResp struct {
		Data struct {
			Success bool   `json:"success"`
			NodeID  string `json:"node_id"`
			Proof   string `json:"proof"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &completeResp); err != nil {
		writeError(w, http.StatusBadGateway, "invalid response from peer")
		return
	}
	if valueContainsSecret(reflect.ValueOf(completeResp.Data), localSecret) {
		writeError(w, http.StatusBadGateway, "peer pairing response contained prohibited authentication material")
		return
	}

	if !completeResp.Data.Success {
		writeError(w, http.StatusBadGateway, "peer returned an unsuccessful pairing response")
		return
	}
	if !verifyPairingProof(localSecret, completeResp.Data.Proof, "initiator", req.PairingCode, self) {
		writeError(w, http.StatusConflict, "peer pairing proof did not match")
		return
	}

	// Success — add the initiator as our peer (clear any previous deletion)
	if _, err := addPeer(req.PeerURL, true); err != nil {
		writeDBError(w, err)
		return
	}
	restartSync()

	logSync.Info("pairing completed (confirmer side)", "peer", req.PeerURL, "remote_node", completeResp.Data.NodeID)
	writeJSON(w, http.StatusOK, ConfirmPeerResponse{Success: true, PeerURL: req.PeerURL, RemoteNode: completeResp.Data.NodeID})
}

// handleAPISyncPairComplete is the callback endpoint on the initiator.
// The confirmer calls this with the pairing code, its own URL, and an HMAC proof.
// If the code and proof are valid, the initiator adds the confirmer as a peer.
func handleAPISyncPairComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if r.TLS == nil {
		writeError(w, http.StatusUpgradeRequired, "TLS is required for peer pairing")
		return
	}

	var req SyncPairCompleteRequest
	if !decodeJSONBody(w, r, maxPairingBodyBytes, &req) {
		return
	}

	// Validate pairing code
	pairingMu.Lock()
	validCode := pairingCode
	expiry := pairingExpiry
	pairingMu.Unlock()

	if validCode == "" {
		writeError(w, http.StatusForbidden, "no pairing in progress")
		return
	}
	if time.Now().After(expiry) {
		pairingMu.Lock()
		pairingCode = ""
		pairingMu.Unlock()
		writeError(w, http.StatusForbidden, "pairing code expired")
		return
	}
	if req.PairingCode != validCode {
		writeError(w, http.StatusForbidden, "invalid pairing code")
		return
	}

	// Validate peer URL
	canonicalPeer, err := canonicalPeerURL(req.PeerURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.PeerURL = canonicalPeer
	if _, err := newSyncHTTPClient(req.PeerURL); err != nil {
		writeError(w, http.StatusServiceUnavailable, "sync TLS configuration error: "+err.Error())
		return
	}
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		writeDBError(w, settingsErr)
		return
	}

	localSecret := settings["sync_secret"]
	if err := validateSyncSecretStrength(localSecret); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !verifyPairingProof(localSecret, req.Proof, "confirmer", validCode, req.PeerURL) {
		writeError(w, http.StatusConflict, "pairing proof did not match")
		return
	}

	// Hold the one-time code until the local peer transaction is durable.
	pairingMu.Lock()
	if pairingCode != validCode {
		pairingMu.Unlock()
		writeError(w, http.StatusForbidden, "pairing code already used")
		return
	}
	if _, err := addPeer(req.PeerURL, true); err != nil {
		pairingMu.Unlock()
		writeDBError(w, err)
		return
	}
	pairingCode = ""
	pairingMu.Unlock()

	restartSync()

	logSync.Info("pairing completed (initiator side)", "peer", req.PeerURL)
	writeJSON(w, http.StatusOK, CompletePairingResponse{Success: true, NodeID: nodeID, Proof: makePairingProof(localSecret, "initiator", validCode, req.PeerURL)})
}
