package main

import "encoding/json"

// Concrete wire payloads preserve the existing field names and success shapes.
type CreateAllowlistResponse struct {
	ID    int64  `json:"id"`
	Alias string `json:"alias"`
}
type AllowlistDomainsPage struct {
	AllowlistID int      `json:"allowlist_id"`
	Domains     []string `json:"domains"`
	Total       int      `json:"total"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}
type SuccessResponse struct {
	Success bool `json:"success"`
}
type HealthResponse struct {
	Status string `json:"status"`
}
type DNSCacheStats struct {
	Hits           int64 `json:"hits"`
	Misses         int64 `json:"misses"`
	HitRate        int   `json:"hit_rate"`
	Entries        int   `json:"entries"`
	EstimatedBytes int64 `json:"estimated_bytes"`
	PeakEntries    int64 `json:"peak_entries"`
	PeakBytes      int64 `json:"peak_bytes"`
}
type DNSStatsResponse struct {
	TotalQueries           int64         `json:"total_queries"`
	BlockedQueries         int64         `json:"blocked_queries"`
	AvgLatencyMicroseconds int64         `json:"avg_latency_microseconds"`
	UptimeSeconds          int           `json:"uptime_seconds"`
	Cache                  DNSCacheStats `json:"cache"`
}
type CreatedAPITokenResponse struct {
	Token string `json:"token"`
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
type CreateBlocklistResponse struct {
	ID    int64  `json:"id"`
	Alias string `json:"alias"`
}
type BlocklistDomainsPage struct {
	BlocklistID int      `json:"blocklist_id"`
	Domains     []string `json:"domains"`
	Total       int      `json:"total"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}
type SensitiveSettingUpdateResponse struct {
	Key     string `json:"key"`
	Updated bool   `json:"updated"`
}
type SettingValueResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type CreateGroupResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type AssignGroupBlocklistsResponse struct {
	Assigned int `json:"assigned"`
}
type AddGroupMembersResponse struct {
	Added int `json:"added"`
}
type CreateRewriteResponse struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}
type CreatePolicyResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type CreateRangeResponse struct {
	ID int64 `json:"id"`
}
type AuthenticatedIdentity struct {
	Authenticated bool   `json:"authenticated"`
	Role          string `json:"role"`
	Username      string `json:"username"`
}
type UnauthenticatedIdentity struct {
	Authenticated bool   `json:"authenticated"`
	SetupRequired bool   `json:"setup_required"`
	Role          string `json:"role"`
	Username      string `json:"username"`
}
type PairingCodeResponse struct {
	PairingCode string `json:"pairing_code"`
	SelfURL     string `json:"self_url"`
	NodeID      string `json:"node_id"`
	ExpiresIn   string `json:"expires_in"`
}
type ConfirmPeerResponse struct {
	Success    bool   `json:"success"`
	PeerURL    string `json:"peer_url"`
	RemoteNode string `json:"remote_node"`
}
type CompletePairingResponse struct {
	Success bool   `json:"success"`
	NodeID  string `json:"node_id"`
	Proof   string `json:"proof"`
}
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
type BootstrapServerView struct {
	ID     int    `json:"id"`
	Server string `json:"server"`
}
type CreatedRewriteView struct {
	ID          int64  `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}
type PolicyView struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	BlocklistCount int    `json:"blocklist_count"`
	AllowlistCount int    `json:"allowlist_count"`
	UsageCount     int    `json:"usage_count"`
}
type PolicyBlocklistView struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
}
type PolicyAllowlistView struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
}
type RangeView struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	CreatedAt string `json:"created_at"`
}
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

type PolicyReference struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}
type AssignedRange struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}
type AssignedClient struct {
	IP    string `json:"ip"`
	Alias string `json:"alias"`
}
type AssignedEntities struct {
	Ranges  []AssignedRange   `json:"ranges"`
	Groups  []PolicyReference `json:"groups"`
	Clients []AssignedClient  `json:"clients"`
}
type RewriteTopClient struct {
	IP    string `json:"ip"`
	Count int64  `json:"count"`
}
type RewriteStatsView struct {
	Domain        string             `json:"domain"`
	Hits          int64              `json:"hits"`
	UniqueClients int64              `json:"unique_clients"`
	TopClients    []RewriteTopClient `json:"top_clients,omitempty"`
}

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

type QueryLogPage struct {
	Logs   []QueryLogView `json:"logs"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}
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
type ArchiveFileView struct {
	Name     string  `json:"name"`
	SizeMB   float64 `json:"size_mb"`
	Modified string  `json:"modified"`
}
type ArchiveStatusView struct {
	ArchivePath    string            `json:"archive_path"`
	ArchiveAgeDays int               `json:"archive_age_days"`
	OldestDataDays int               `json:"oldest_data_days"`
	LiveRows       int64             `json:"live_rows"`
	ArchiveFiles   []ArchiveFileView `json:"archive_files"`
}
type InvestigationColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type InvestigationSchemaTable struct {
	Name    string                `json:"name"`
	Type    string                `json:"type"`
	Columns []InvestigationColumn `json:"columns"`
}
type InvestigationSchemaView struct {
	Tables []InvestigationSchemaTable `json:"tables"`
}
type InvestigationView struct {
	Columns    []string        `json:"columns"`
	Rows       json.RawMessage `json:"rows"`
	RowCount   int             `json:"row_count"`
	DurationMS int64           `json:"duration_ms"`
}

type PeerView struct {
	URL               string `json:"url"`
	NodeName          string `json:"node_name,omitempty"`
	Healthy           bool   `json:"healthy"`
	LastSyncAt        string `json:"last_sync_at"`
	Changes24h        int    `json:"changes_24h"`
	ConsecutiveErrors int    `json:"consecutive_errors"`
	LastError         string `json:"last_error,omitempty"`
}
