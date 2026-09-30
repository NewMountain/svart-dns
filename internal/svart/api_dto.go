package svart

import "encoding/json"

// CreateAllowlistResponse returns the API result for create allowlist.
type CreateAllowlistResponse struct {
	ID    int64  `json:"id"`
	Alias string `json:"alias"`
}

// AllowlistDomainsPage returns a paginated selection of allowlist domains.
type AllowlistDomainsPage struct {
	AllowlistID int      `json:"allowlist_id"`
	Domains     []string `json:"domains"`
	Total       int      `json:"total"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}

// SuccessResponse acknowledges a completed API operation.
type SuccessResponse struct {
	Success bool `json:"success"`
}

// HealthResponse reports the service health status.
type HealthResponse struct {
	Status string `json:"status"`
}

// DNSCacheStats exposes cache counters and memory estimates in the API.
type DNSCacheStats struct {
	Hits           int64 `json:"hits"`
	Misses         int64 `json:"misses"`
	HitRate        int   `json:"hit_rate"`
	Entries        int   `json:"entries"`
	EstimatedBytes int64 `json:"estimated_bytes"`
	PeakEntries    int64 `json:"peak_entries"`
	PeakBytes      int64 `json:"peak_bytes"`
}

// DNSStatsResponse returns aggregate resolver activity, latency, and cache statistics.
type DNSStatsResponse struct {
	TotalQueries           int64         `json:"total_queries"`
	BlockedQueries         int64         `json:"blocked_queries"`
	AvgLatencyMicroseconds int64         `json:"avg_latency_microseconds"`
	UptimeSeconds          int           `json:"uptime_seconds"`
	Cache                  DNSCacheStats `json:"cache"`
}

// CreatedAPITokenResponse returns a new API token once, together with its identity and role.
type CreatedAPITokenResponse struct {
	Token string `json:"token"`
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// CreateBlocklistResponse returns the API result for create blocklist.
type CreateBlocklistResponse struct {
	ID    int64  `json:"id"`
	Alias string `json:"alias"`
}

// BlocklistDomainsPage returns a paginated selection of blocklist domains.
type BlocklistDomainsPage struct {
	BlocklistID int      `json:"blocklist_id"`
	Domains     []string `json:"domains"`
	Total       int      `json:"total"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}

// SensitiveSettingUpdateResponse returns the API result for sensitive setting update.
type SensitiveSettingUpdateResponse struct {
	Key     string `json:"key"`
	Updated bool   `json:"updated"`
}

// SettingValueResponse returns the API result for setting value.
type SettingValueResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CreateGroupResponse returns the API result for create group.
type CreateGroupResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// AssignGroupBlocklistsResponse returns the API result for assign group blocklists.
type AssignGroupBlocklistsResponse struct {
	Assigned int `json:"assigned"`
}

// AddGroupMembersResponse returns the API result for add group members.
type AddGroupMembersResponse struct {
	Added int `json:"added"`
}

// CreateRewriteResponse returns the API result for create rewrite.
type CreateRewriteResponse struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}

// CreatePolicyResponse returns the API result for create policy.
type CreatePolicyResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// CreateRangeResponse returns the API result for create range.
type CreateRangeResponse struct {
	ID int64 `json:"id"`
}

// AuthenticatedIdentity describes the current authenticated account.
type AuthenticatedIdentity struct {
	Authenticated bool   `json:"authenticated"`
	Role          string `json:"role"`
	Username      string `json:"username"`
}

// UnauthenticatedIdentity reports authentication and initial-setup requirements.
type UnauthenticatedIdentity struct {
	Authenticated bool   `json:"authenticated"`
	SetupRequired bool   `json:"setup_required"`
	Role          string `json:"role"`
	Username      string `json:"username"`
}

// PairingCodeResponse returns the temporary code and identity needed to pair a node.
type PairingCodeResponse struct {
	PairingCode string `json:"pairing_code"`
	SelfURL     string `json:"self_url"`
	NodeID      string `json:"node_id"`
	ExpiresIn   string `json:"expires_in"`
}

// ConfirmPeerResponse acknowledges a confirmed peer connection.
type ConfirmPeerResponse struct {
	Success    bool   `json:"success"`
	PeerURL    string `json:"peer_url"`
	RemoteNode string `json:"remote_node"`
}

// CompletePairingResponse returns the paired node identity and authentication proof.
type CompletePairingResponse struct {
	Success bool   `json:"success"`
	NodeID  string `json:"node_id"`
	Proof   string `json:"proof"`
}

// AllowlistView presents allowlist data in the API.
type AllowlistView struct {
	ID              int                      `json:"id"`
	URL             string                   `json:"url"`
	Alias           string                   `json:"alias"`
	Enabled         bool                     `json:"enabled"`
	DomainCount     int                      `json:"domain_count"`
	LastUpdated     string                   `json:"last_updated"`
	RefreshInterval int                      `json:"refresh_interval"`
	Compatibility   ListCompatibilitySummary `json:"compatibility"`
}

// BlocklistView presents blocklist data in the API.
type BlocklistView struct {
	ID              int                      `json:"id"`
	URL             string                   `json:"url"`
	Alias           string                   `json:"alias"`
	Enabled         bool                     `json:"enabled"`
	DomainCount     int                      `json:"domain_count"`
	LastUpdated     string                   `json:"last_updated"`
	RefreshInterval int                      `json:"refresh_interval"`
	Compatibility   ListCompatibilitySummary `json:"compatibility"`
}

// BootstrapServerView presents bootstrap server data in the API.
type BootstrapServerView struct {
	ID     int    `json:"id"`
	Server string `json:"server"`
}

// CreatedRewriteView presents created rewrite data in the API.
type CreatedRewriteView struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}

// PolicyView presents policy data in the API.
type PolicyView struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	BlocklistCount int    `json:"blocklist_count"`
	AllowlistCount int    `json:"allowlist_count"`
	UsageCount     int    `json:"usage_count"`
}

// PolicyBlocklistView presents policy blocklist data in the API.
type PolicyBlocklistView struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
}

// PolicyAllowlistView presents policy allowlist data in the API.
type PolicyAllowlistView struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
}

// RangeView presents range data in the API.
type RangeView struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	CreatedAt string `json:"created_at"`
}

// BlocklistHistoryView presents blocklist history data in the API.
type BlocklistHistoryView struct {
	ID             int      `json:"id"`
	BlocklistID    int      `json:"blocklist_id"`
	BlocklistAlias string   `json:"blocklist_alias"`
	RefreshedAt    string   `json:"refreshed_at"`
	PreviousCount  int      `json:"previous_count"`
	NewCount       int      `json:"new_count"`
	AddedCount     int      `json:"added_count"`
	RemovedCount   int      `json:"removed_count"`
	SampleAdded    []string `json:"sample_added"`
	SampleRemoved  []string `json:"sample_removed"`
}

// QueryLogView presents query log data in the API.
type QueryLogView struct {
	ID                  int     `json:"id"`
	Timestamp           string  `json:"timestamp"`
	ClientIP            string  `json:"client_ip"`
	QueryName           string  `json:"query_name"`
	QueryType           string  `json:"query_type"`
	ResponseCode        string  `json:"response_code"`
	Blocked             bool    `json:"blocked"`
	Upstream            string  `json:"upstream"`
	LatencyMicroseconds int64   `json:"latency_microseconds"`
	ClientAlias         *string `json:"client_alias,omitempty"`
	BlockTier           *string `json:"block_tier,omitempty"`
	BlockRule           *string `json:"block_rule,omitempty"`
	BlockSource         *string `json:"block_source,omitempty"`
	BlockListID         *int    `json:"block_list_id,omitempty"`
	BlockListName       *string `json:"block_list_name,omitempty"`
	Result              *string `json:"result,omitempty"`
	ResultReason        *string `json:"result_reason,omitempty"`
	ResultTier          *string `json:"result_tier,omitempty"`
	ResultEntity        *string `json:"result_entity,omitempty"`
	ResultIsPublished   *bool   `json:"result_is_published,omitempty"`
	ResultRule          *string `json:"result_rule,omitempty"`
	ResultListID        *int    `json:"result_list_id,omitempty"`
	ResultListName      *string `json:"result_list_name,omitempty"`
	RangeResult         *string `json:"range_result,omitempty"`
	RangeName           *string `json:"range_name,omitempty"`
	GroupResult         *string `json:"group_result,omitempty"`
	GroupName           *string `json:"group_name,omitempty"`
	IPResult            *string `json:"ip_result,omitempty"`
	IPEntity            *string `json:"ip_entity,omitempty"`
}

// QueryLogDetailView presents query log detail data in the API.
type QueryLogDetailView struct {
	ID                  int                     `json:"id"`
	Timestamp           string                  `json:"timestamp"`
	ClientIP            string                  `json:"client_ip"`
	ClientName          string                  `json:"client_name"`
	QueryName           string                  `json:"query_name"`
	QueryType           string                  `json:"query_type"`
	ResponseCode        string                  `json:"response_code"`
	Blocked             bool                    `json:"blocked"`
	Upstream            string                  `json:"upstream"`
	LatencyMicroseconds int64                   `json:"latency_microseconds"`
	Result              string                  `json:"result"`
	ResultReason        string                  `json:"result_reason"`
	ResultTier          string                  `json:"result_tier"`
	ResultEntity        string                  `json:"result_entity"`
	ResultIsPublished   bool                    `json:"result_is_published"`
	ResultRule          string                  `json:"result_rule"`
	ResultListID        int                     `json:"result_list_id"`
	ResultListName      string                  `json:"result_list_name"`
	RangeResult         string                  `json:"range_result"`
	RangeEntity         string                  `json:"range_entity"`
	RangeIsPublished    bool                    `json:"range_is_published"`
	RangeRule           string                  `json:"range_rule"`
	RangeListID         int                     `json:"range_list_id"`
	RangeListName       string                  `json:"range_list_name"`
	GroupResult         string                  `json:"group_result"`
	GroupEntity         string                  `json:"group_entity"`
	GroupIsPublished    bool                    `json:"group_is_published"`
	GroupRule           string                  `json:"group_rule"`
	GroupListID         int                     `json:"group_list_id"`
	GroupListName       string                  `json:"group_list_name"`
	IPResult            string                  `json:"ip_result"`
	IPEntity            string                  `json:"ip_entity"`
	IPIsPublished       bool                    `json:"ip_is_published"`
	IPRule              string                  `json:"ip_rule"`
	IPListID            int                     `json:"ip_list_id"`
	IPListName          string                  `json:"ip_list_name"`
	Policy              *queryLogPolicySnapshot `json:"policy,omitempty"`
}

// RecentLogView presents recent log data in the API.
type RecentLogView struct {
	ID                  int64   `json:"id"`
	Timestamp           string  `json:"timestamp"`
	ClientIP            string  `json:"client_ip"`
	QueryName           string  `json:"query_name"`
	QueryType           string  `json:"query_type"`
	Blocked             bool    `json:"blocked"`
	Upstream            string  `json:"upstream"`
	LatencyMicroseconds int64   `json:"latency_microseconds"`
	ClientAlias         *string `json:"client_alias,omitempty"`
	ResultReason        *string `json:"result_reason,omitempty"`
	ResultTier          *string `json:"result_tier,omitempty"`
	ResultEntity        *string `json:"result_entity,omitempty"`
	ResultListName      *string `json:"result_list_name,omitempty"`
	BlockListName       *string `json:"block_list_name,omitempty"`
	RangeName           *string `json:"range_name,omitempty"`
	GroupName           *string `json:"group_name,omitempty"`
}

func apiPtr[T any](v T) *T { return &v }

// PolicyReference identifies a policy by its ID and name.
type PolicyReference struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// AssignedRange identifies a subnet assigned to a policy.
type AssignedRange struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

// AssignedClient identifies a client assigned to a policy.
type AssignedClient struct {
	IP    string `json:"ip"`
	Alias string `json:"alias"`
}

// AssignedEntities groups the ranges, groups, and clients using a policy.
type AssignedEntities struct {
	Ranges  []AssignedRange   `json:"ranges"`
	Groups  []PolicyReference `json:"groups"`
	Clients []AssignedClient  `json:"clients"`
}

// RewriteTopClient counts rewrite hits for one client.
type RewriteTopClient struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}

// RewriteStatsView presents rewrite stats data in the API.
type RewriteStatsView struct {
	Domain        string             `json:"domain"`
	Hits          int64              `json:"hits"`
	UniqueClients int64              `json:"unique_clients"`
	TopClients    []RewriteTopClient `json:"top_clients,omitempty"`
}

// ClientDetailView presents client detail data in the API.
type ClientDetailView struct {
	IP                     string                `json:"ip"`
	Alias                  string                `json:"alias"`
	TotalQueries           int64                 `json:"total_queries"`
	AvgLatencyMicroseconds int64                 `json:"avg_latency_microseconds"`
	FirstSeen              string                `json:"first_seen"`
	LastSeen               string                `json:"last_seen"`
	Groups                 []Group               `json:"groups"`
	Blocklists             []BlocklistAssignment `json:"blocklists"`
	BlocklistStats         BlocklistUniqueStats  `json:"blocklist_stats"`
	CustomBlocked          []string              `json:"custom_blocked"`
	CustomAllowed          []string              `json:"custom_allowed"`
	Policy                 *PolicyReference      `json:"policy,omitempty"`
}

// GroupDetailView presents group detail data in the API.
type GroupDetailView struct {
	ID                     int                   `json:"id"`
	Name                   string                `json:"name"`
	Members                []Member              `json:"members"`
	Blocklists             []BlocklistAssignment `json:"blocklists"`
	CustomBlocked          []string              `json:"custom_blocked"`
	CustomAllowed          []string              `json:"custom_allowed"`
	BlocklistStats         BlocklistUniqueStats  `json:"blocklist_stats"`
	TotalQueries           int64                 `json:"total_queries"`
	AvgLatencyMicroseconds float64               `json:"avg_latency_microseconds"`
	FirstSeen              string                `json:"first_seen"`
	LastSeen               string                `json:"last_seen"`
	RecentLogs             []RecentLogView       `json:"recent_logs"`
	Policy                 *PolicyReference      `json:"policy,omitempty"`
}

// RangeDetailView presents range detail data in the API.
type RangeDetailView struct {
	CustomBlocked          []string              `json:"custom_blocked"`
	CustomAllowed          []string              `json:"custom_allowed"`
	ID                     int                   `json:"id"`
	Name                   string                `json:"name"`
	CIDR                   string                `json:"cidr"`
	CreatedAt              string                `json:"created_at"`
	Blocklists             []BlocklistAssignment `json:"blocklists"`
	Allowlists             []BlocklistAssignment `json:"allowlists"`
	BlocklistStats         BlocklistUniqueStats  `json:"blocklist_stats"`
	TotalQueries           int64                 `json:"total_queries"`
	AvgLatencyMicroseconds float64               `json:"avg_latency_microseconds"`
	FirstSeen              string                `json:"first_seen"`
	LastSeen               string                `json:"last_seen"`
	RecentLogs             []RecentLogView       `json:"recent_logs"`
	Policy                 *PolicyReference      `json:"policy,omitempty"`
}

// PeersView presents peers data in the API.
type PeersView struct {
	NodeID            string     `json:"node_id"`
	NodeName          string     `json:"node_name"`
	Peers             []PeerView `json:"peers"`
	SyncInterval      string     `json:"sync_interval"`
	HasSecret         bool       `json:"has_secret"`
	TLSConfigured     bool       `json:"tls_configured"`
	ReplicateIDentity bool       `json:"replicate_identity"`
	SyncTLSReady      *bool      `json:"sync_tls_ready,omitempty"`
	SyncTLSError      *string    `json:"sync_tls_error,omitempty"`
}

// QueryLogPage returns paginated query history and the matching total.
type QueryLogPage struct {
	Logs   []QueryLogView `json:"logs"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// PolicyDetailView presents policy detail data in the API.
type PolicyDetailView struct {
	ID             int                   `json:"id"`
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	Blocklists     []PolicyBlocklistView `json:"blocklists"`
	Allowlists     []PolicyAllowlistView `json:"allowlists"`
	CustomBlocked  []string              `json:"custom_blocked"`
	CustomAllowed  []string              `json:"custom_allowed"`
	BlocklistStats BlocklistUniqueStats  `json:"blocklist_stats"`
	AssignedTo     AssignedEntities      `json:"assigned_to"`
}

// ArchiveFileView presents archive file data in the API.
type ArchiveFileView struct {
	Name     string  `json:"name"`
	SizeMB   float64 `json:"size_mb"`
	Modified string  `json:"modified"`
}

// ArchiveStatusView presents archive status data in the API.
type ArchiveStatusView struct {
	ArchivePath    string            `json:"archive_path"`
	ArchiveAgeDays int               `json:"archive_age_days"`
	OldestDataDays int               `json:"oldest_data_days"`
	LiveRows       int64             `json:"live_rows"`
	ArchiveFiles   []ArchiveFileView `json:"archive_files"`
}

// InvestigationColumn describes a SQL result column.
type InvestigationColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// InvestigationSchemaTable describes an available investigation table and its columns.
type InvestigationSchemaTable struct {
	Name    string                `json:"name"`
	Type    string                `json:"type"`
	Columns []InvestigationColumn `json:"columns"`
}

// InvestigationSchemaView lists the tables available to read-only investigation queries.
type InvestigationSchemaView struct {
	Tables []InvestigationSchemaTable `json:"tables"`
}

// InvestigationView returns SQL columns, rows, and execution statistics.
type InvestigationView struct {
	Columns    []string        `json:"columns"`
	Rows       json.RawMessage `json:"rows"`
	RowCount   int             `json:"row_count"`
	DurationMS int64           `json:"duration_ms"`
}

// PeerView presents peer data in the API.
type PeerView struct {
	URL               string `json:"url"`
	NodeName          string `json:"node_name,omitempty"`
	Healthy           bool   `json:"healthy"`
	LastSyncAt        string `json:"last_sync_at"`
	Changes24h        int    `json:"changes_24h"`
	ConsecutiveErrors int    `json:"consecutive_errors"`
	LastError         string `json:"last_error,omitempty"`
}
