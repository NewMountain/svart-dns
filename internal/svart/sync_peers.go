package svart

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// --- Peer info API ---

// handleAPIPeers godoc
// @Summary Get peer sync status or manage peers
// @Description GET returns sync status. POST adds a peer. DELETE /api/peers/{url} removes a peer.
// @Tags Sync
// @Security ApiKeyAuth
// @Produce json
// @Success 200 {object} apiResponse
// @Failure 400 {object} apiResponse
// @Failure 401 {object} apiResponse
// @Router /api/peers [get]
func handleAPIPeers(w http.ResponseWriter, r *http.Request) {
	// Check for DELETE /api/peers/{encoded_url}
	if strings.HasPrefix(r.URL.Path, "/api/peers/") && r.Method == http.MethodDelete {
		encoded := r.URL.Path[len("/api/peers/"):]
		peerURL, err := url.QueryUnescape(encoded)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid URL encoding")
			return
		}
		if err := removePeerFromSettings(peerURL); err != nil {
			writeDBError(w, err)
			return
		}
		restartSync()
		writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
		return
	}

	switch r.Method {
	case http.MethodGet:
		handleAPIPeersGet(w, r)
	case http.MethodPost:
		handleAPIPeersPost(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func handleAPIPeersGet(w http.ResponseWriter, _ *http.Request) {

	syncMu.Lock()
	peers := peerStates
	syncMu.Unlock()

	var result []PeerView
	for _, ps := range peers {
		ps.mu.Lock()
		info := PeerView{
			URL:               ps.URL,
			NodeName:          ps.NodeName,
			Healthy:           ps.Healthy,
			Changes24h:        ps.changeCount24h(),
			ConsecutiveErrors: ps.ConsecutiveErrors,
			LastError:         ps.LastError,
		}
		if !ps.LastSyncAt.IsZero() {
			info.LastSyncAt = ps.LastSyncAt.Format(time.RFC3339Nano)
		}
		ps.mu.Unlock()
		result = append(result, info)
	}

	if result == nil {
		result = []PeerView{}
	}

	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		writeDBError(w, settingsErr)
		return
	}

	secret := settings["sync_secret"]
	secretReady := validateSyncSecretStrength(secret) == nil
	nodeName := settings["node_name"]
	if nodeName == "" {
		nodeName = nodeID
	}
	resp := PeersView{
		NodeID:        nodeID,
		NodeName:      nodeName,
		Peers:         result,
		SyncInterval:  settings["sync_interval"],
		HasSecret:     secretReady,
		TLSConfigured: tlsCert != "" && tlsKey != "",
		// Read-only: set by SYNC_REPLICATE_IDENTITY at startup, never by API.
		ReplicateIDentity: syncReplicateIdentity.Load(),
	}
	if ready, tlsErr := syncTLSStatus(peers); ready {
		resp.SyncTLSReady = apiPtr(true)
	} else {
		resp.SyncTLSReady = apiPtr(false)
		resp.SyncTLSError = apiPtr(tlsErr)
	}
	writeJSON(w, http.StatusOK, resp)
}

func syncTLSStatus(peers []*peerState) (bool, string) {
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		return false, "sync settings unavailable"
	}

	if err := validateSyncSecretStrength(settings["sync_secret"]); err != nil {
		return false, err.Error()
	}
	if len(peers) == 0 {
		return false, "no sync peers are configured"
	}
	if tlsCert == "" || tlsKey == "" {
		return false, "TLS_CERT and TLS_KEY are required for peer sync"
	}
	for _, peer := range peers {
		if _, err := newSyncTLSConfig(peer.URL); err != nil {
			return false, err.Error()
		}
		peer.mu.Lock()
		healthy := peer.Healthy
		lastSyncAt := peer.LastSyncAt
		lastError := peer.LastError
		peer.mu.Unlock()
		if !healthy {
			if lastError == "" {
				lastError = "unknown sync failure"
			}
			return false, fmt.Sprintf("peer %s sync is unhealthy: %s", peer.URL, lastError)
		}
		if lastSyncAt.IsZero() {
			return false, fmt.Sprintf("peer %s has not completed a verified sync", peer.URL)
		}
	}
	return true, ""
}

func handleAPIPeersPost(w http.ResponseWriter, r *http.Request) {
	var req AddPeerRequest
	if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
		return
	}
	canonicalURL, err := canonicalPeerURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := newSyncTLSConfig(canonicalURL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.URL = canonicalURL
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		writeDBError(w, settingsErr)
		return
	}

	if settings["node_name"] == "" {
		writeError(w, http.StatusBadRequest, "set a node name before adding peers")
		return
	}

	// Explicit add clears any previous deletion (so re-adding a deleted peer works)
	added, err := addPeer(req.URL, true)
	if err != nil {
		writeDBError(w, err)
		return
	}
	if !added {
		writeError(w, http.StatusConflict, "peer already configured")
		return
	}
	restartSync()
	writeJSON(w, http.StatusCreated, SuccessResponse{Success: true})
}

func removePeerFromSettings(peerURL string) error {
	return localMutation(snapshotNone, func(tx *sql.Tx) error {
		if _, err := changePeerSetting(tx, "sync_peers", peerURL, false); err != nil {
			return err
		}
		_, err := changePeerSetting(tx, "deleted_peers", peerURL, true)
		return err
	})
}

// removeDeletedPeer removes a peer URL from the deleted_peers blocklist (called when explicitly re-adding).
func removeDeletedPeer(peerURL string) error {
	return localMutation(snapshotNone, func(tx *sql.Tx) error { _, err := changePeerSetting(tx, "deleted_peers", peerURL, false); return err })
}

// isDeletedPeer checks whether a peer URL was explicitly deleted and should not be re-added by gossip.
func isDeletedPeer(peerURL string) bool {
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		logSync.Error("deleted peers unavailable", "error", settingsErr)
		return true
	}

	current := settings["deleted_peers"]
	for _, p := range strings.Split(current, ",") {
		if strings.TrimSpace(p) == peerURL {
			return true
		}
	}
	return false
}

// discoverPeers adds any unknown peers from a sync response (gossip protocol).
// It collects the remote's self URL and its known peers, filters out ourselves
// and peers we already have, and adds any new ones.
func discoverPeers(resp *SyncResponse) {
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		logSync.Error("peer discovery settings unavailable", "error", settingsErr)
		return
	}

	self := selfURL()
	if canonical, err := canonicalPeerURL(self); err == nil {
		self = canonical
	}

	// Collect candidate URLs: remote's self URL + all peers it knows about
	candidates := make(map[string]bool)
	if resp.SelfURL != "" {
		candidates[resp.SelfURL] = true
	}
	for _, p := range resp.KnownPeers {
		candidates[p] = true
	}

	// Get our current peer list
	current := settings["sync_peers"]
	known := make(map[string]bool)
	for _, p := range strings.Split(current, ",") {
		if canonical, err := canonicalPeerURL(p); err == nil {
			known[canonical] = true
		}
	}

	var added []string
	for candidate := range candidates {
		canonical, err := canonicalPeerURL(candidate)
		if err != nil {
			continue
		}
		candidate = canonical
		if _, err := newSyncTLSConfig(candidate); err != nil {
			logSync.Warn("rejected discovered peer that is outside the configured TLS trust boundary")
			continue
		}
		// Skip ourselves
		if self != "" && candidate == self {
			continue
		}
		// Skip peers we already have
		if known[candidate] {
			continue
		}
		// Skip peers that were explicitly deleted
		if isDeletedPeer(candidate) {
			continue
		}
		addedPeer, err := addPeerToSettings(candidate)
		if err != nil {
			logSync.Error("peer discovery unavailable", "error", err)
			return
		}
		if addedPeer {
			added = append(added, candidate)
		}
	}

	if len(added) > 0 {
		logSync.Info("discovered new peers via gossip", "peers", added)
		restartSync()
	}
}

// addPeerToSettings adds a peer URL to sync_peers if not already present. Returns true if added.
func addPeerToSettings(peerURL string) (bool, error) { return addPeer(peerURL, false) }

// selfURL constructs this node's reachable HTTPS URL from external IP and admin port.
func selfURL() string {
	if externalIP == "" {
		return ""
	}
	if tlsCert == "" || tlsKey == "" {
		return ""
	}
	return fmt.Sprintf("https://%s:%s", externalIP, adminPort)
}
