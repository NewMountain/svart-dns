package svart

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// --- Identity replication gate (DD-029) ---

// syncReplicateIdentity says whether admin_users and api_tokens, and their
// tombstones, cross the sync boundary. It is off by default: LWW has no notion
// of who may write identity rows, so with it on any peer holding SYNC_SECRET
// and a trusted certificate can create, overwrite or delete admins on every
// node.
var syncReplicateIdentity atomic.Bool

func isIdentitySyncTable(table string) bool {
	return table == "admin_users" || table == "api_tokens"
}

// configureSyncIdentityReplication applies SYNC_REPLICATE_IDENTITY. A value
// that is not a boolean fails closed: identity stays node-local.
func configureSyncIdentityReplication(raw string) {
	enabled, err := parseSyncReplicateIdentity(raw)
	if err != nil {
		logSync.Error("invalid SYNC_REPLICATE_IDENTITY; admin_users and api_tokens will not replicate", "error", err)
	}
	syncReplicateIdentity.Store(enabled)
	if enabled {
		logSync.Warn("SYNC_REPLICATE_IDENTITY enabled: admin_users and api_tokens replicate, so one compromised peer can create or remove admins fleet-wide (DD-029)")
	}
}

func parseSyncReplicateIdentity(raw string) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("SYNC_REPLICATE_IDENTITY must be true or false, got %q", raw)
	}
	return enabled, nil
}

// --- LWW timestamps ---

// legacySQLiteTimestampLayout is the UTC form written by SQLite's
// CURRENT_TIMESTAMP and datetime('now') (the settings/client_aliases column
// defaults and the migrateSchema junction backfill). Those values are still in
// real databases and older peers send them verbatim; syncNow() writes
// RFC3339Nano. The optional fraction also covers SQLite's subsecond variant.
const legacySQLiteTimestampLayout = "2006-01-02 15:04:05.999999999"

func parseSyncTimestamp(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(legacySQLiteTimestampLayout, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("sync timestamp is neither RFC3339 nor SQLite UTC")
}

func formatSyncTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// storedSyncTimestamp orders a value already in the local database. Empty
// values (rows from before sync tracking) and values this node cannot parse
// order before every real timestamp, so an admitted write or tombstone
// supersedes them instead of being blocked by metadata nobody can interpret.
func storedSyncTimestamp(raw string) time.Time {
	t, err := parseSyncTimestamp(raw)
	if err != nil {
		return time.Time{}
	}
	return t
}

// syncWriteWins is the LWW rule: an admitted incoming write replaces the local
// row only when it is strictly later. Ties keep the local row.
func syncWriteWins(incoming, stored string) bool {
	return storedSyncTimestamp(incoming).After(storedSyncTimestamp(stored))
}

// --- Merge admission ---

const (
	rejectIdentityDisabled = "identity_replication_disabled"
	rejectInvalidTimestamp = "invalid_timestamp"
	rejectFutureTimestamp  = "future_timestamp"
	rejectInvalidRow       = "invalid_row"
)

// syncRejection is one class of refused rows. Table is always one of this
// file's constant table names, never peer-supplied text, so the metric's label
// set stays bounded.
type syncRejection struct {
	Table  string
	Reason string
}

var syncRowsRejectedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "svart_dns_sync_rows_rejected_total",
	Help: "Replicated rows and tombstones refused at the sync merge boundary, by peer, table and reason.",
}, []string{"peer", "table", "reason"})

// Registered beside the merge boundary that increments it: initMetrics only
// runs from main, and every process that merges peer data must count refusals.
func init() {
	prometheus.MustRegister(syncRowsRejectedTotal)
}

// admitSyncTimestamp parses a peer-supplied LWW timestamp and returns its
// canonical RFC3339Nano UTC form, or the reason it is refused.
func admitSyncTimestamp(raw string, now time.Time) (string, string) {
	t, err := parseSyncTimestamp(raw)
	if err != nil {
		return "", rejectInvalidTimestamp
	}
	if t.After(now.Add(maxSyncClockSkew)) {
		return "", rejectFutureTimestamp
	}
	return formatSyncTimestamp(t), ""
}

// admitSyncRows keeps the rows whose timestamp and content are acceptable,
// rewriting each kept timestamp to canonical form. The input is not modified.
func admitSyncRows[T any](rows []T, table string, stamp func(*T) *string, valid func(*T) bool, now time.Time, rejected *[]syncRejection) []T {
	var admitted []T
	for _, row := range rows {
		canonical, reason := admitSyncTimestamp(*stamp(&row), now)
		if reason == "" && valid != nil && !valid(&row) {
			reason = rejectInvalidRow
		}
		if reason != "" {
			*rejected = append(*rejected, syncRejection{Table: table, Reason: reason})
			continue
		}
		*stamp(&row) = canonical
		admitted = append(admitted, row)
	}
	return admitted
}

func refuseSyncRows(table string, count int, rejected *[]syncRejection) {
	for i := 0; i < count; i++ {
		*rejected = append(*rejected, syncRejection{Table: table, Reason: rejectIdentityDisabled})
	}
}

func admitSyncTombstones(tombstones []Tombstone, now time.Time, replicateIdentity bool, rejected *[]syncRejection) []Tombstone {
	var admitted []Tombstone
	for _, t := range tombstones {
		reason := ""
		canonical := ""
		if _, known := syncDeletionPredicates[t.TableName]; !known {
			reason = rejectInvalidRow
		} else if !validSyncTombstoneKey(t.TableName, t.NaturalKey) {
			reason = rejectInvalidRow
		} else if isIdentitySyncTable(t.TableName) && !replicateIdentity {
			reason = rejectIdentityDisabled
		} else {
			canonical, reason = admitSyncTimestamp(t.DeletedAt, now)
		}
		if reason != "" {
			*rejected = append(*rejected, syncRejection{Table: "sync_tombstones", Reason: reason})
			continue
		}
		t.DeletedAt = canonical
		admitted = append(admitted, t)
	}
	return admitted
}

// The validators below mirror what the HTTP API enforces before it writes the
// same rows, so replication cannot store what an administrator could not.

func validSyncRange(r *SyncRange) bool {
	_, _, err := net.ParseCIDR(r.CIDR)
	return err == nil && r.Name != ""
}

func validSyncPolicy(p *SyncPolicy) bool {
	return strings.TrimSpace(p.Name) != ""
}

func validSyncRole(role string) bool {
	return role == RoleAdmin || role == RoleReadonly
}

func validSyncAdminUser(u *SyncAdminUser) bool {
	return u.Username != "" && u.PasswordHash != "" && validSyncRole(u.Role)
}

func validSyncAPIToken(t *SyncAPIToken) bool {
	return t.TokenPrefix != "" && t.TokenHash != "" && validSyncRole(t.Role)
}

// Encoded downloaded rules are node-local generations, never manual sync rows.
func validSyncManualDomain(row *SyncManualDomain) bool {
	return !reservedStoredRule(row.Domain)
}

// admitSyncResponse is the parse step of the merge boundary: it returns a copy
// of resp holding only rows that may be applied, with canonical timestamps,
// plus one rejection per refused row. It performs no I/O.
func admitSyncResponse(resp *SyncResponse, now time.Time, replicateIdentity bool) (*SyncResponse, []syncRejection) {
	out := *resp
	var rejected []syncRejection
	in, c := &resp.Changes, &out.Changes

	c.Upstreams = admitSyncRows(in.Upstreams, "upstreams", func(r *SyncUpstream) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Blocklists = admitSyncRows(in.Blocklists, "blocklists", func(r *SyncBlocklist) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Allowlists = admitSyncRows(in.Allowlists, "allowlists", func(r *SyncAllowlist) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Rewrites = admitSyncRows(in.Rewrites, "rewrites", func(r *SyncRewrite) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Settings = admitSyncRows(in.Settings, "settings", func(r *SyncSetting) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.BootstrapSvrs = admitSyncRows(in.BootstrapSvrs, "bootstrap_servers", func(r *SyncBootstrap) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.ClientAliases = admitSyncRows(in.ClientAliases, "client_aliases", func(r *SyncClientAlias) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Groups = admitSyncRows(in.Groups, "client_groups", func(r *SyncGroup) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.Ranges = admitSyncRows(in.Ranges, "ip_ranges", func(r *SyncRange) *string { return &r.UpdatedAt }, validSyncRange, now, &rejected)
	c.GroupMembers = admitSyncRows(in.GroupMembers, "client_group_members", func(r *SyncGroupMember) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.ClientBlocklists = admitSyncRows(in.ClientBlocklists, "client_blocklists", func(r *SyncClientList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.ClientAllowlists = admitSyncRows(in.ClientAllowlists, "client_allowlists", func(r *SyncClientList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.GroupBlocklists = admitSyncRows(in.GroupBlocklists, "group_blocklists", func(r *SyncGroupList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.GroupAllowlists = admitSyncRows(in.GroupAllowlists, "group_allowlists", func(r *SyncGroupList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.RangeBlocklists = admitSyncRows(in.RangeBlocklists, "range_blocklists", func(r *SyncRangeList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.RangeAllowlists = admitSyncRows(in.RangeAllowlists, "range_allowlists", func(r *SyncRangeList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.BlockedDomains = admitSyncRows(in.BlockedDomains, "blocked_domains", func(r *SyncManualDomain) *string { return &r.UpdatedAt }, validSyncManualDomain, now, &rejected)
	c.AllowedDomains = admitSyncRows(in.AllowedDomains, "allowed_domains", func(r *SyncManualDomain) *string { return &r.UpdatedAt }, validSyncManualDomain, now, &rejected)
	c.Policies = admitSyncRows(in.Policies, "policies", func(r *SyncPolicy) *string { return &r.UpdatedAt }, validSyncPolicy, now, &rejected)
	c.PolicyBlocklists = admitSyncRows(in.PolicyBlocklists, "policy_blocklists", func(r *SyncPolicyList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.PolicyAllowlists = admitSyncRows(in.PolicyAllowlists, "policy_allowlists", func(r *SyncPolicyList) *string { return &r.UpdatedAt }, nil, now, &rejected)
	c.ClientPolicies = admitSyncRows(in.ClientPolicies, "client_policies", func(r *SyncClientPolicy) *string { return &r.UpdatedAt }, nil, now, &rejected)

	if replicateIdentity {
		c.APITokens = admitSyncRows(in.APITokens, "api_tokens", func(r *SyncAPIToken) *string { return &r.UpdatedAt }, validSyncAPIToken, now, &rejected)
		c.AdminUsers = admitSyncRows(in.AdminUsers, "admin_users", func(r *SyncAdminUser) *string { return &r.UpdatedAt }, validSyncAdminUser, now, &rejected)
	} else {
		refuseSyncRows("api_tokens", len(in.APITokens), &rejected)
		refuseSyncRows("admin_users", len(in.AdminUsers), &rejected)
		c.APITokens, c.AdminUsers = nil, nil
	}

	out.Tombstones = admitSyncTombstones(resp.Tombstones, now, replicateIdentity, &rejected)
	return &out, rejected
}

// reportSyncRejections counts every refused row and logs each class once per
// peer: a peer resends the same bad rows on every poll, and a warning every
// two seconds would bury everything else.
func reportSyncRejections(peer *peerState, rejected []syncRejection) {
	counts := make(map[syncRejection]int)
	for _, r := range rejected {
		counts[r]++
	}
	label, peerURL := "peer-unknown", "unknown"
	if peer != nil {
		label, peerURL = peer.metricLabel(), peer.URL
	}
	for r, n := range counts {
		syncRowsRejectedTotal.WithLabelValues(label, r.Table, r.Reason).Add(float64(n))
		if peer != nil && !peer.firstRejection(r) {
			continue
		}
		msg := "rejected sync rows from peer"
		if r.Reason == rejectIdentityDisabled {
			msg = "ignoring identity rows from peer because SYNC_REPLICATE_IDENTITY is disabled"
		}
		logSync.Warn(msg, "peer", peerURL, "table", r.Table, "reason", r.Reason, "rows", n)
	}
}

// --- Merge logic (Commit 3) ---

// mergeSyncResponse is the trust boundary for replicated data: it admits the
// payload (identity gate, timestamp and row validation), reports what it
// refused, and applies the rest with LWW.
func mergeSyncResponse(resp *SyncResponse) error {
	admitted, rejected := admitSyncResponse(resp, time.Now(), syncReplicateIdentity.Load())
	reportSyncRejections(resp.source, rejected)
	return applySyncResponse(admitted)
}
