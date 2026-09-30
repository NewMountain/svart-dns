package main

import (
	"context"
	"crypto/hmac"
	cryptoRandPkg "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/yeti/svart-dns/internal/telemetry"
)

var cryptoRand = cryptoRandPkg.Reader

var syncAllowedPeers map[string]struct{}

const (
	defaultSyncInterval  = 2 * time.Second
	minSyncInterval      = time.Second
	maxSyncInterval      = time.Hour
	maxPairingBodyBytes  = 64 * 1024
	maxSyncResponseBytes = 16 * 1024 * 1024

	// maxSyncClockSkew is how far ahead of the local clock a peer's LWW
	// timestamp may be. The greatest timestamp wins forever and a tombstone only
	// deletes rows stamped at or before it, so an unbounded future value makes a
	// row permanent. Five minutes absorbs drift between NTP-disciplined nodes
	// while bounding how long a forged row can outrank a genuine edit.
	maxSyncClockSkew = 5 * time.Minute
)

func isNodeLocalSyncSetting(key string) bool {
	return key == "node_name" || key == "sync_peers" || key == "deleted_peers" || isNeverExposeSetting(key)
}

// --- Sync auth middleware ---

var syncSecret string

// syncAuth requires the peer-only X-Sync-Key. Admin credentials intentionally
// do not grant access to replication payloads, which include credential hashes.
func syncAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			writeError(w, http.StatusUpgradeRequired, "TLS is required for peer sync")
			return
		}
		syncMu.Lock()
		secret := syncSecret
		syncMu.Unlock()

		provided := r.Header.Get("X-Sync-Key")
		if secret != "" && len(provided) == len(secret) &&
			subtle.ConstantTimeCompare([]byte(provided), []byte(secret)) == 1 {
			next(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "valid sync key required")
	}
}

// handleAPISync godoc
// @Summary Get sync changes since timestamp
// @Description Returns all config changes and tombstones since the given timestamp for peer-to-peer sync
// @Tags Sync
// @Security SyncKeyAuth
// @Produce json
// @Param since query string false "RFC3339Nano timestamp to get changes since (default: all)"
// @Success 200 {object} SyncResponse
// @Failure 401 {object} apiResponse
// @Failure 503 {object} apiResponse
// @Router /api/sync [get]
func handleAPISync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	sinceStr := r.URL.Query().Get("since")
	var since time.Time
	if sinceStr != "" {
		var err error
		since, err = time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid since timestamp: %v", err))
			return
		}
	}

	resp, err := buildSyncResponse(since)
	if err != nil {
		writeDBError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// --- Peer state ---

type peerState struct {
	URL               string    `json:"url"`
	MetricLabel       string    `json:"-"`
	NodeName          string    `json:"node_name,omitempty"`
	LastSyncAt        time.Time `json:"last_sync_at"`
	LastSyncRows      int       `json:"last_sync_rows"`
	ConsecutiveErrors int       `json:"consecutive_errors"`
	Healthy           bool      `json:"healthy"`
	LastError         string    `json:"last_error,omitempty"`
	// Rolling 24h change counter: each entry is (timestamp, rowCount)
	recentChanges []syncChange
	// Rejection classes already logged for this peer (see reportSyncRejections).
	warnedRejections map[syncRejection]struct{}
	mu               sync.Mutex
}

// firstRejection reports whether this class of refused rows is new for the
// peer, recording it so later polls stay quiet.
func (ps *peerState) firstRejection(r syncRejection) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, seen := ps.warnedRejections[r]; seen {
		return false
	}
	if ps.warnedRejections == nil {
		ps.warnedRejections = make(map[syncRejection]struct{})
	}
	ps.warnedRejections[r] = struct{}{}
	return true
}

func (ps *peerState) metricLabel() string {
	if ps.MetricLabel != "" {
		return ps.MetricLabel
	}
	return "peer-unknown"
}

type syncChange struct {
	at    time.Time
	count int
}

// addChange records a non-zero sync and prunes entries older than 24h.
func (ps *peerState) addChange(count int) {
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	// Prune old entries
	i := 0
	for i < len(ps.recentChanges) && ps.recentChanges[i].at.Before(cutoff) {
		i++
	}
	ps.recentChanges = append(ps.recentChanges[i:], syncChange{at: now, count: count})
}

// changeCount24h returns the sum of synced rows in the last 24 hours.
func (ps *peerState) changeCount24h() int {
	cutoff := time.Now().Add(-24 * time.Hour)
	total := 0
	for _, c := range ps.recentChanges {
		if c.at.After(cutoff) {
			total += c.count
		}
	}
	return total
}

var (
	peerStates   []*peerState
	syncInterval time.Duration
	syncDone     chan struct{}
	syncMu       sync.Mutex
)

// seedSyncSettings seeds sync_peers, sync_secret, sync_interval from env vars
// into the settings table if the DB values are empty. One-time bootstrap — after
// first run, DB owns the config.
func seedSyncSettings() error {
	envMap := map[string]string{
		"PEERS":         "sync_peers",
		"SYNC_SECRET":   "sync_secret",
		"SYNC_INTERVAL": "sync_interval",
	}
	for envKey, settingKey := range envMap {
		envVal := os.Getenv(envKey)
		if envVal == "" {
			continue
		}
		if settingKey == "sync_interval" {
			if _, err := parseSyncInterval(envVal); err != nil {
				logSync.Error("invalid SYNC_INTERVAL ignored; using the safe runtime default")
				continue
			}
		}
		if settingKey == "sync_secret" {
			if err := validateSyncSecretStrength(envVal); err != nil {
				logSync.Error("weak SYNC_SECRET ignored; peer sync remains disabled")
				continue
			}
		}
		var dbVal string
		if err := db.QueryRow("SELECT value FROM settings WHERE key = ?", settingKey).Scan(&dbVal); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return err
		}
		if dbVal == "" {
			ts, nid := syncNow()
			result, err := db.Exec("UPDATE settings SET value = ?, updated_at = ?, node_id = ? WHERE key = ?", envVal, ts, nid, settingKey)
			if err := checkListWrite(result, err, 1); err != nil {
				return err
			}
			logSync.Info("seeded sync setting from env", "key", settingKey, "env", envKey)
		}
	}
	return nil
}

// readSyncSettings preserves absence as an empty lookup and propagates every storage failure.
func readSyncSettings() (map[string]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database not open")
	}
	return readSyncSettingsFrom(db)
}

func readSyncSettingsFrom(q queryer) (map[string]string, error) {
	settings := make(map[string]string)
	err := scanRows(q, "SELECT key,value FROM settings", func(rows *sql.Rows) error {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return err
		}
		settings[key] = value
		return nil
	})
	return settings, err
}

func parseSyncInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultSyncInterval, nil
	}
	interval, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("sync interval must be a valid duration")
	}
	if interval < minSyncInterval || interval > maxSyncInterval {
		return 0, fmt.Errorf("sync interval must be between 1s and 1h")
	}
	return interval, nil
}

// syncLWWColumns lists every replicated LWW timestamp column, with an SQL
// expression that identifies the row in repair logs.
var syncLWWColumns = []struct{ table, column, key string }{
	{"upstreams", "updated_at", "upstream"},
	{"blocklists", "updated_at", "alias"},
	{"allowlists", "updated_at", "alias"},
	{"rewrites", "updated_at", "domain"},
	{"settings", "updated_at", "key"},
	{"bootstrap_servers", "updated_at", "server"},
	{"client_aliases", "updated_at", "ip_address"},
	{"api_tokens", "updated_at", "token_prefix"},
	{"admin_users", "updated_at", "username"},
	{"client_groups", "updated_at", "name"},
	{"ip_ranges", "updated_at", "cidr"},
	{"client_group_members", "updated_at", "client_ip || '|' || group_id"},
	{"client_blocklists", "updated_at", "client_ip || '|' || blocklist_id"},
	{"client_allowlists", "updated_at", "client_ip || '|' || allowlist_id"},
	{"group_blocklists", "updated_at", "group_id || '|' || blocklist_id"},
	{"group_allowlists", "updated_at", "group_id || '|' || allowlist_id"},
	{"range_blocklists", "updated_at", "range_id || '|' || blocklist_id"},
	{"range_allowlists", "updated_at", "range_id || '|' || allowlist_id"},
	{"blocked_domains", "updated_at", "blocklist_id || '|' || domain"},
	{"allowed_domains", "updated_at", "allowlist_id || '|' || domain"},
	{"policies", "updated_at", "name"},
	{"policy_blocklists", "updated_at", "policy_id || '|' || blocklist_id"},
	{"policy_allowlists", "updated_at", "policy_id || '|' || allowlist_id"},
	{"client_policies", "updated_at", "client_ip"},
	{"sync_tombstones", "deleted_at", "table_name || '|' || natural_key"},
}

// repairPoisonedSyncTimestamps fixes LWW timestamps that releases before merge
// admission accepted from peers: values beyond the skew allowance, and values
// that do not parse but sorted high enough to win byte comparison (e.g. "~").
// Such a row outranks every genuine edit and survives every tombstone, and its
// sender resends it on every poll. The row's content is kept; only the
// timestamp moves to now, so the next real edit or delete applies again. Each
// repair is logged at error level with the original value.
func repairPoisonedSyncTimestamps(now time.Time) {
	limit := now.Add(maxSyncClockSkew)
	limitText := formatSyncTimestamp(limit)
	repaired := formatSyncTimestamp(now)
	for _, c := range syncLWWColumns {
		type poisonedRow struct {
			rowID         int64
			key, original string
		}
		var poisoned []poisonedRow
		// Byte order is only a cheap prefilter over large tables: a far-future
		// or high-sorting junk value sorts after limitText. A non-UTC offset can
		// hide an instant at most a day ahead, which expires on its own. CAST
		// keeps values stored with numeric affinity (e.g. 99999999) comparable.
		rows, err := db.Query(fmt.Sprintf("SELECT rowid, CAST(%s AS TEXT), CAST(%s AS TEXT) FROM %s WHERE CAST(%s AS TEXT) > ?",
			c.key, c.column, c.table, c.column), limitText)
		if err != nil {
			logSync.Error("sync timestamp repair scan failed", "table", c.table, "error", err)
			continue
		}
		for rows.Next() {
			var row poisonedRow
			var key sql.NullString
			if err := rows.Scan(&row.rowID, &key, &row.original); err != nil {
				logSync.Error("sync timestamp repair scan failed", "table", c.table, "error", err)
				continue
			}
			row.key = key.String
			if t, err := parseSyncTimestamp(row.original); err == nil && !t.After(limit) {
				continue
			}
			poisoned = append(poisoned, row)
		}
		closeQueryRows(rows)

		for _, row := range poisoned {
			if _, err := db.Exec(fmt.Sprintf("UPDATE %s SET %s = ? WHERE rowid = ?", c.table, c.column), repaired, row.rowID); err != nil {
				logSync.Error("sync timestamp repair failed", "table", c.table, "key", row.key, "error", err)
				continue
			}
			logSync.Error("repaired poisoned sync timestamp: row content kept, timestamp moved to now so edits and tombstones apply again",
				"table", c.table, "key", row.key, "column", c.column, "original", row.original, "repaired", repaired)
		}
	}
}

// initSync reads sync config from DB and starts sync workers.
func initSync() {
	// Once per process and before any worker merges, so rows poisoned under an
	// older release stop outranking the writes this release admits.
	repairPoisonedSyncTimestamps(time.Now())

	syncMu.Lock()
	defer syncMu.Unlock()
	initSyncLocked()
}

// initSyncLocked does the actual init. Caller must hold syncMu.
func initSyncLocked() {
	settings, settingsErr := readSyncSettings()
	if settingsErr != nil {
		logSync.Error("sync settings unavailable", "error", settingsErr)
		return
	}
	initSyncSettingsLocked(settings)
}

// initSyncSettingsLocked consumes a complete, validated settings snapshot.
// Caller must hold syncMu.
func initSyncSettingsLocked(settings map[string]string) {
	storedSyncSecret := settings["sync_secret"]
	syncSecretErr := validateSyncSecretStrength(storedSyncSecret)
	if syncSecretErr == nil {
		syncSecret = storedSyncSecret
	} else {
		// Never activate a legacy weak key for either inbound or outbound sync.
		syncSecret = ""
	}
	intervalStr := settings["sync_interval"]
	if intervalStr == "" {
		intervalStr = defaultSyncInterval.String()
	}
	var err error
	syncInterval, err = parseSyncInterval(intervalStr)
	if err != nil {
		logSync.Warn("invalid sync_interval, using safe default", "error", err)
		syncInterval = defaultSyncInterval
	}

	peersStr := settings["sync_peers"]
	if peersStr == "" {
		return
	}
	if syncSecretErr != nil {
		logSync.Error("sync peers configured without a strong sync secret — sync disabled")
		return
	}

	// Validate TLS when peers are configured
	if tlsCert == "" || tlsKey == "" {
		logSync.Error("sync_peers configured but TLS_CERT/TLS_KEY not set — sync disabled")
		return
	}

	done := make(chan struct{})
	syncDone = done
	secret := syncSecret
	interval := syncInterval

	for _, p := range strings.Split(peersStr, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "https://") {
			logSync.Warn("skipping non-HTTPS peer", "peer", p)
			continue
		}
		ps := &peerState{
			URL:         p,
			MetricLabel: fmt.Sprintf("peer-%d", len(peerStates)+1),
			Healthy:     true,
		}
		peerStates = append(peerStates, ps)
		go syncWorker(ps, secret, interval, done)
	}

	if len(peerStates) > 0 {
		logSync.Info("sync enabled", "peers", len(peerStates), "interval", syncInterval)
		startTombstoneGCLocked()
	}
}

// restartSync stops existing workers and re-initializes from DB settings.
func restartSync() {
	// Serialize the read and publication with local settings transactions.
	policyReloadMu.Lock()
	defer policyReloadMu.Unlock()
	settings, err := readSyncSettings()
	if err != nil {
		logSync.Error("sync settings unavailable", "error", err)
		return
	}
	restartSyncWithSettings(settings)
}

func restartSyncWithSettings(settings map[string]string) {
	syncMu.Lock()
	defer syncMu.Unlock()

	// Stop old workers + tombstone GC
	if syncDone != nil {
		close(syncDone)
		syncDone = nil
	}
	peerStates = nil

	initSyncSettingsLocked(settings)
}

// closeSync stops all sync workers.
func closeSync() {
	syncMu.Lock()
	defer syncMu.Unlock()
	if syncDone != nil {
		close(syncDone)
		syncDone = nil
	}
}

// syncWorker polls a single peer for changes.
func syncWorker(peer *peerState, secret string, interval time.Duration, done <-chan struct{}) {
	client, err := newSyncHTTPClient(peer.URL)
	if err != nil {
		syncError(peer, err)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			syncOnce(client, peer, secret)
		}
	}
}

func newSyncHTTPClient(peerURL string) (*http.Client, error) {
	tlsCfg, err := newSyncTLSConfig(peerURL)
	if err != nil {
		return nil, err
	}

	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("default HTTP transport is not configurable for peer TLS")
	}
	transport := base.Clone()
	transport.TLSClientConfig = tlsCfg
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: telemetry.Transport{Base: transport, Operation: telemetry.Sync},
		// Never follow peer-controlled redirects. Go may copy custom headers to
		// redirect targets, which could disclose X-Sync-Key outside the verified
		// peer trust boundary.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

func canonicalPeerURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid sync peer URL %q: %w", raw, err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return "", fmt.Errorf("sync peer URL must use https: %s", raw)
	}
	if parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("sync peer URL must contain only an HTTPS host and port: %s", raw)
	}

	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return "https://" + net.JoinHostPort(hostname, port), nil
}

func parseSyncPeerAllowlist(raw string) (map[string]struct{}, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	allowed := make(map[string]struct{})
	for _, entry := range strings.Split(raw, ",") {
		canonical, err := canonicalPeerURL(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid SYNC_PEER_ALLOWLIST entry: %w", err)
		}
		allowed[canonical] = struct{}{}
	}
	return allowed, nil
}

func newSyncTLSConfig(peerURL string) (*tls.Config, error) {
	if syncTLSConfigErr != nil {
		return nil, fmt.Errorf("sync TLS trust configuration unavailable: %w", syncTLSConfigErr)
	}
	canonical, err := canonicalPeerURL(peerURL)
	if err != nil {
		return nil, err
	}
	if len(syncAllowedPeers) == 0 {
		return nil, fmt.Errorf("SYNC_PEER_ALLOWLIST is required for peer sync")
	}
	if _, allowed := syncAllowedPeers[canonical]; !allowed {
		return nil, fmt.Errorf("sync peer %s is not present in SYNC_PEER_ALLOWLIST", canonical)
	}
	parsed, err := url.Parse(canonical)
	if err != nil {
		return nil, err
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if syncTLSConfig != nil {
		tlsCfg = syncTLSConfig.Clone()
		tlsCfg.MinVersion = tls.VersionTLS12
	}
	tlsCfg.InsecureSkipVerify = false
	if net.ParseIP(parsed.Hostname()) != nil {
		if syncTLSServerName == "" {
			return nil, fmt.Errorf("SYNC_TLS_SERVER_NAME is required for IP peer %s", peerURL)
		}
		tlsCfg.ServerName = syncTLSServerName
	}
	return tlsCfg, nil
}

func makePairingProof(secret, role, code, peerURL string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	for _, field := range []string{"svart-dns-pairing-v1", role, code, peerURL} {
		_, _ = mac.Write([]byte(field))
		_, _ = mac.Write([]byte{0})
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func verifyPairingProof(secret, provided, role, code, peerURL string) bool {
	expected := makePairingProof(secret, role, code, peerURL)
	return len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func validateSyncSecretStrength(secret string) error {
	if len([]byte(secret)) < 32 {
		return fmt.Errorf("sync secret must be at least 32 bytes")
	}
	if strings.TrimSpace(secret) != secret {
		return fmt.Errorf("sync secret must not have surrounding whitespace")
	}
	for i := 0; i < len(secret); i++ {
		if secret[i] < 0x20 || secret[i] == 0x7f {
			return fmt.Errorf("sync secret contains characters that are unsafe in an HTTP header")
		}
	}
	return nil
}

func syncPayloadContainsSecret(resp *SyncResponse, secret string) bool {
	if resp == nil || secret == "" {
		return false
	}
	return valueContainsSecret(reflect.ValueOf(resp), secret)
}

func stripExactLegacySyncSecretSetting(resp *SyncResponse, secret string) bool {
	if resp == nil {
		return false
	}
	filtered := make([]SyncSetting, 0, len(resp.Changes.Settings))
	found := false
	for _, setting := range resp.Changes.Settings {
		if setting.Key != "sync_secret" {
			filtered = append(filtered, setting)
			continue
		}
		// The immediately preceding release replicated this one field. Permit
		// exactly one copy only when it is byte-for-byte the key we already hold,
		// then discard the whole row before reflection scanning or merge. A
		// mismatch or duplicate is not legitimate rolling-upgrade traffic.
		if found || secret == "" || len(setting.Value) != len(secret) ||
			subtle.ConstantTimeCompare([]byte(setting.Value), []byte(secret)) != 1 {
			return false
		}
		found = true
	}
	resp.Changes.Settings = filtered
	return true
}

func valueContainsSecret(value reflect.Value, secret string) bool {
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.Interface, reflect.Pointer:
		if value.IsNil() {
			return false
		}
		return valueContainsSecret(value.Elem(), secret)
	case reflect.String:
		return strings.Contains(value.String(), secret)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if valueContainsSecret(value.Field(i), secret) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if valueContainsSecret(value.Index(i), secret) {
				return true
			}
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			if valueContainsSecret(iter.Key(), secret) || valueContainsSecret(iter.Value(), secret) {
				return true
			}
		}
	}
	return false
}

func syncOnce(client *http.Client, peer *peerState, secret string) {
	peer.mu.Lock()
	since := peer.LastSyncAt
	peer.mu.Unlock()
	syncOnceSince(client, peer, secret, since)
}

// A missing parent can predate the delta cursor. Retry one complete snapshot
// without changing that cursor until the full payload actually commits.
func syncOnceSince(client *http.Client, peer *peerState, secret string, since time.Time) {
	ctx, complete := telemetry.Begin(context.Background(), telemetry.Sync)
	outcome := errors.New("sync did not complete")
	defer func() { complete(outcome) }()
	url := peer.URL + "/api/sync?since=" + since.UTC().Format(time.RFC3339Nano)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		syncError(peer, err)
		return
	}
	if secret != "" {
		req.Header.Set("X-Sync-Key", secret)
	}

	resp, err := client.Do(req)
	if err != nil {
		// Transport errors can include peer-controlled protocol text after the
		// request (and X-Sync-Key) has been sent. Keep retained state generic.
		syncError(peer, fmt.Errorf("peer request failed"))
		return
	}
	defer closeReadResource(resp.Body)

	if resp.StatusCode != http.StatusOK {
		syncError(peer, fmt.Errorf("peer returned HTTP %d", resp.StatusCode))
		return
	}

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxSyncResponseBytes+1))
	if err != nil {
		syncError(peer, fmt.Errorf("failed to read peer response"))
		return
	}
	if len(responseBody) > maxSyncResponseBytes {
		syncError(peer, fmt.Errorf("peer response exceeded the size limit"))
		return
	}

	// Parse the envelope: {"data": <SyncResponse>, "error": null}
	var envelope struct {
		Data  *SyncResponse `json:"data"`
		Error *string       `json:"error"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		syncError(peer, fmt.Errorf("decode: %w", err))
		return
	}
	if envelope.Error != nil {
		syncError(peer, fmt.Errorf("peer returned an application error"))
		return
	}
	if envelope.Data == nil {
		syncError(peer, fmt.Errorf("nil data in response"))
		return
	}
	if !stripExactLegacySyncSecretSetting(envelope.Data, secret) || syncPayloadContainsSecret(envelope.Data, secret) {
		syncError(peer, fmt.Errorf("peer response contained prohibited authentication material"))
		return
	}

	syncResp := envelope.Data
	// Parse the server time to use as next since
	serverTime, err := time.Parse(time.RFC3339Nano, syncResp.ServerTime)
	if err != nil || serverTime.After(time.Now().Add(maxSyncClockSkew)) {
		syncError(peer, fmt.Errorf("peer returned an invalid or future server_time; correct the peer clock"))
		return
	}

	syncResp.source = peer
	rowCount := countSyncRows(syncResp)

	syncPollsTotal.WithLabelValues(peer.metricLabel()).Inc()

	if rowCount > 0 {
		_, finishStorage := telemetry.Begin(ctx, telemetry.StorageWrite)
		mergeErr := mergeSyncResponse(syncResp)
		finishStorage(mergeErr)
		if err := mergeErr; err != nil {
			var missing *syncMissingReferenceError
			if !since.IsZero() && errors.As(err, &missing) {
				logSync.Info("retrying full sync after missing reference", "peer", peer.metricLabel(), "table", missing.table)
				syncOnceSince(client, peer, secret, time.Time{})
				return
			}
			syncError(peer, fmt.Errorf("merge: %w", err))
			return
		}
		syncRowsReceivedTotal.WithLabelValues(peer.metricLabel()).Add(float64(rowCount))
		logSync.Info("synced from peer", "peer", peer.URL, "rows", rowCount)
	}

	peer.mu.Lock()
	peer.LastSyncAt = serverTime
	peer.LastSyncRows = rowCount
	peer.ConsecutiveErrors = 0
	peer.Healthy = true
	peer.LastError = ""
	if rowCount > 0 {
		peer.addChange(rowCount)
	}
	if syncResp.NodeName != "" {
		peer.NodeName = syncResp.NodeName
	} else if syncResp.NodeID != "" {
		peer.NodeName = syncResp.NodeID
	}
	peer.mu.Unlock()

	syncLastSuccessTimestamp.WithLabelValues(peer.metricLabel()).Set(float64(time.Now().Unix()))
	syncPeerHealthy.WithLabelValues(peer.metricLabel()).Set(1)

	outcome = nil
	// Gossip: discover new peers from the remote's known peer list + self URL
	discoverPeers(syncResp)
}

// peerDownThreshold is the number of consecutive sync errors before emitting
// a critical "peer down" alert. At the default 2s sync interval, 150 errors = 5 minutes.
const peerDownThreshold = 150

func syncError(peer *peerState, err error) {
	peer.mu.Lock()
	peer.ConsecutiveErrors++
	peer.LastError = err.Error()
	peer.Healthy = false
	consec := peer.ConsecutiveErrors
	peer.mu.Unlock()

	syncErrorsTotal.WithLabelValues(peer.metricLabel()).Inc()
	syncPeerHealthy.WithLabelValues(peer.metricLabel()).Set(0)

	// Escalate: emit a critical alert once at the threshold, then every 150 errors (every ~5 min)
	if consec == peerDownThreshold || (consec > peerDownThreshold && consec%peerDownThreshold == 0) {
		logSync.Error("PEER DOWN — node unreachable for extended period",
			"peer", peer.URL,
			"consecutive_errors", consec,
			"estimated_downtime_minutes", consec*2/60,
			"node_name", peer.NodeName)
	} else {
		logSync.Warn("sync error", "peer", peer.URL, "error", err, "consecutive", consec)
	}
}

func countSyncRows(resp *SyncResponse) int {
	c := &resp.Changes
	return len(c.Upstreams) + len(c.Blocklists) + len(c.Allowlists) + len(c.Rewrites) +
		len(c.Settings) + len(c.BootstrapSvrs) + len(c.ClientAliases) + len(c.APITokens) + len(c.AdminUsers) +
		len(c.Groups) + len(c.Ranges) + len(c.GroupMembers) +
		len(c.ClientBlocklists) + len(c.ClientAllowlists) +
		len(c.GroupBlocklists) + len(c.GroupAllowlists) +
		len(c.RangeBlocklists) + len(c.RangeAllowlists) +
		len(c.BlockedDomains) + len(c.AllowedDomains) +
		len(c.Policies) + len(c.PolicyBlocklists) + len(c.PolicyAllowlists) + len(c.ClientPolicies) +
		len(resp.Tombstones)
}

// --- Tombstone GC ---

// startTombstoneGCLocked starts the GC goroutine. Caller must hold syncMu.
func startTombstoneGCLocked() {
	done := syncDone // capture for goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				gcTombstones()
			}
		}
	}()
}

func gcTombstones() {
	cutoff := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)

	// Snapshot peerStates under lock
	syncMu.Lock()
	peers := peerStates
	syncMu.Unlock()

	// Only GC tombstones older than 24h AND older than the oldest peer's last sync
	if len(peers) > 0 {
		var oldestSync time.Time
		for _, ps := range peers {
			ps.mu.Lock()
			if oldestSync.IsZero() || ps.LastSyncAt.Before(oldestSync) {
				oldestSync = ps.LastSyncAt
			}
			ps.mu.Unlock()
		}
		// Only GC if all peers have synced past the tombstone
		if !oldestSync.IsZero() {
			oldestStr := oldestSync.UTC().Format(time.RFC3339Nano)
			if oldestStr < cutoff {
				cutoff = oldestStr
			}
		}
	}

	result, err := db.Exec("DELETE FROM sync_tombstones WHERE deleted_at < ?", cutoff)
	if err != nil {
		logSync.Warn("tombstone GC error", "error", err)
		return
	}
	rows, err := result.RowsAffected()
	if err != nil {
		logSync.Warn("tombstone GC row count unavailable", "error", err)
		return
	}
	if rows > 0 {
		logSync.Info("tombstone GC", "deleted", rows)
	}
}
