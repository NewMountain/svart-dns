package main

// --- Sync types using natural keys ---

type SyncResponse struct {
	NodeID     string      `json:"node_id"`
	NodeName   string      `json:"node_name,omitempty"`
	SelfURL    string      `json:"self_url,omitempty"`
	KnownPeers []string    `json:"known_peers,omitempty"`
	ServerTime string      `json:"server_time"`
	Changes    SyncChanges `json:"changes"`
	Tombstones []Tombstone `json:"tombstones"`

	// source is the verified peer that delivered this payload (nil when it did
	// not come off the wire), so merge-boundary rejections are attributed to it.
	source *peerState
}

type SyncChanges struct {
	Upstreams        []SyncUpstream     `json:"upstreams,omitempty"`
	Blocklists       []SyncBlocklist    `json:"blocklists,omitempty"`
	Allowlists       []SyncAllowlist    `json:"allowlists,omitempty"`
	Rewrites         []SyncRewrite      `json:"rewrites,omitempty"`
	Settings         []SyncSetting      `json:"settings,omitempty"`
	BootstrapSvrs    []SyncBootstrap    `json:"bootstrap_servers,omitempty"`
	ClientAliases    []SyncClientAlias  `json:"client_aliases,omitempty"`
	APITokens        []SyncAPIToken     `json:"api_tokens,omitempty"`
	AdminUsers       []SyncAdminUser    `json:"admin_users,omitempty"`
	Groups           []SyncGroup        `json:"groups,omitempty"`
	Ranges           []SyncRange        `json:"ranges,omitempty"`
	GroupMembers     []SyncGroupMember  `json:"group_members,omitempty"`
	ClientBlocklists []SyncClientList   `json:"client_blocklists,omitempty"`
	ClientAllowlists []SyncClientList   `json:"client_allowlists,omitempty"`
	GroupBlocklists  []SyncGroupList    `json:"group_blocklists,omitempty"`
	GroupAllowlists  []SyncGroupList    `json:"group_allowlists,omitempty"`
	RangeBlocklists  []SyncRangeList    `json:"range_blocklists,omitempty"`
	RangeAllowlists  []SyncRangeList    `json:"range_allowlists,omitempty"`
	BlockedDomains   []SyncManualDomain `json:"blocked_domains,omitempty"`
	AllowedDomains   []SyncManualDomain `json:"allowed_domains,omitempty"`
	Policies         []SyncPolicy       `json:"policies,omitempty"`
	PolicyBlocklists []SyncPolicyList   `json:"policy_blocklists,omitempty"`
	PolicyAllowlists []SyncPolicyList   `json:"policy_allowlists,omitempty"`
	ClientPolicies   []SyncClientPolicy `json:"client_policies,omitempty"`
}

type SyncUpstream struct {
	Upstream  string `json:"upstream"` // natural key
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncBlocklist struct {
	Alias           string `json:"alias"` // natural key
	URL             string `json:"url"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval int    `json:"refresh_interval"`
	UpdatedAt       string `json:"updated_at"`
	NodeID          string `json:"node_id"`
}

type SyncAllowlist struct {
	Alias           string `json:"alias"` // natural key
	URL             string `json:"url"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval int    `json:"refresh_interval"`
	UpdatedAt       string `json:"updated_at"`
	NodeID          string `json:"node_id"`
}

type SyncRewrite struct {
	Domain      string `json:"domain"` // natural key
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
	UpdatedAt   string `json:"updated_at"`
	NodeID      string `json:"node_id"`
}

type SyncSetting struct {
	Key       string `json:"key"` // natural key
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncBootstrap struct {
	Server    string `json:"server"` // natural key
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncClientAlias struct {
	IPAddress string `json:"ip_address"` // natural key
	Alias     string `json:"alias"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncAPIToken struct {
	TokenPrefix string `json:"token_prefix"` // natural key
	Name        string `json:"name"`
	TokenHash   string `json:"token_hash"`
	Role        string `json:"role"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at"`
	UpdatedAt   string `json:"updated_at"`
	NodeID      string `json:"node_id"`
}

type SyncAdminUser struct {
	Username     string `json:"username"` // natural key
	PasswordHash string `json:"password_hash"`
	Role         string `json:"role"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	NodeID       string `json:"node_id"`
}

type SyncGroup struct {
	Name       string `json:"name"` // natural key
	PolicyName string `json:"policy_name,omitempty"`
	UpdatedAt  string `json:"updated_at"`
	NodeID     string `json:"node_id"`
}

type SyncRange struct {
	CIDR       string `json:"cidr"` // natural key
	Name       string `json:"name"`
	PolicyName string `json:"policy_name,omitempty"`
	UpdatedAt  string `json:"updated_at"`
	NodeID     string `json:"node_id"`
}

type SyncGroupMember struct {
	GroupName string `json:"group_name"` // junction: client_ip|group_name
	ClientIP  string `json:"client_ip"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncClientList struct {
	ClientIP  string `json:"client_ip"` // junction: client_ip|alias
	ListAlias string `json:"list_alias"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncGroupList struct {
	GroupName string `json:"group_name"` // junction: group_name|alias
	ListAlias string `json:"list_alias"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncRangeList struct {
	RangeCIDR string `json:"range_cidr"` // junction: cidr|alias
	ListAlias string `json:"list_alias"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncManualDomain struct {
	ListAlias string `json:"list_alias"` // junction: alias|domain
	Domain    string `json:"domain"`
	UpdatedAt string `json:"updated_at"`
	NodeID    string `json:"node_id"`
}

type SyncPolicy struct {
	Name        string `json:"name"` // natural key
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
	NodeID      string `json:"node_id"`
}

type SyncPolicyList struct {
	PolicyName string `json:"policy_name"` // junction: policy_name|alias
	ListAlias  string `json:"list_alias"`
	UpdatedAt  string `json:"updated_at"`
	NodeID     string `json:"node_id"`
}

type SyncClientPolicy struct {
	ClientIP   string `json:"client_ip"` // natural key: client_ip
	PolicyName string `json:"policy_name"`
	UpdatedAt  string `json:"updated_at"`
	NodeID     string `json:"node_id"`
}

type Tombstone struct {
	TableName  string `json:"table_name"`
	NaturalKey string `json:"natural_key"`
	DeletedAt  string `json:"deleted_at"`
	NodeID     string `json:"node_id"`
}
