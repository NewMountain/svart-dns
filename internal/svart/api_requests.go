package svart

// CreateAllowlistRequest defines the API inputs for create allowlist.
type CreateAllowlistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval *int   `json:"refresh_interval"`
}

// UpdateAllowlistRequest defines the API inputs for update allowlist.
type UpdateAllowlistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	RefreshInterval *int   `json:"refresh_interval"`
}

// ClientAllowDomainRequest defines the API inputs for client allow domain.
type ClientAllowDomainRequest struct {
	Domain string `json:"domain"`
}

// GroupAllowDomainRequest defines the API inputs for group allow domain.
type GroupAllowDomainRequest struct {
	Domain string `json:"domain"`
}

// CreateAPITokenRequest defines the API inputs for create api token.
type CreateAPITokenRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// CreateAdminUserRequest defines the API inputs for create admin user.
type CreateAdminUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateAdminUserRequest defines the API inputs for update admin user.
type UpdateAdminUserRequest struct {
	Role     string `json:"role"`
	Password string `json:"password"`
}

// CreateBlocklistRequest defines the API inputs for create blocklist.
type CreateBlocklistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval *int   `json:"refresh_interval"`
}

// UpdateBlocklistRequest defines the API inputs for update blocklist.
type UpdateBlocklistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	RefreshInterval *int   `json:"refresh_interval"`
}

// ClientBlockDomainRequest defines the API inputs for client block domain.
type ClientBlockDomainRequest struct {
	Domain string `json:"domain"`
}

// GroupBlockDomainRequest defines the API inputs for group block domain.
type GroupBlockDomainRequest struct {
	Domain string `json:"domain"`
}

// UpdateSettingRequest defines the API inputs for update setting.
type UpdateSettingRequest struct {
	Value string `json:"value"`
}

// AddBootstrapServerRequest defines the API inputs for add bootstrap server.
type AddBootstrapServerRequest struct {
	Server string `json:"server"`
}

// ReplaceBootstrapServersRequest defines the API inputs for replace bootstrap servers.
type ReplaceBootstrapServersRequest struct {
	Servers []string `json:"servers"`
}

// UpdateClientAliasRequest defines the API inputs for update client alias.
type UpdateClientAliasRequest struct {
	Alias string `json:"alias"`
}

// CreateGroupRequest defines the API inputs for create group.
type CreateGroupRequest struct {
	Name string `json:"name"`
}

// UpdateGroupRequest defines the API inputs for update group.
type UpdateGroupRequest struct {
	Name string `json:"name"`
}

// AssignGroupBlocklistsRequest defines the API inputs for assign group blocklists.
type AssignGroupBlocklistsRequest struct {
	BlocklistIDs []int `json:"blocklist_ids"`
}

// AddGroupMembersRequest defines the API inputs for add group members.
type AddGroupMembersRequest struct {
	ClientIPs []string `json:"client_ips"`
}

// CreateRewriteRequest defines the API inputs for create rewrite.
type CreateRewriteRequest struct {
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}

// UpdateRewriteRequest defines the API inputs for update rewrite.
type UpdateRewriteRequest struct {
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     *bool  `json:"enabled"`
}

// CreateRewritesBatchRequest defines the API inputs for create rewrites batch.
type CreateRewritesBatchRequest struct {
	Rewrites []CreateRewriteRequest `json:"rewrites"`
}

// InvestigateRequest defines the API inputs for investigate.
type InvestigateRequest struct {
	SQL     string `json:"sql"`
	Timeout int    `json:"timeout"`
}

// CreatePolicyRequest defines the API inputs for create policy.
type CreatePolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// UpdatePolicyRequest defines the API inputs for update policy.
type UpdatePolicyRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// PolicyBlockDomainRequest defines the API inputs for policy block domain.
type PolicyBlockDomainRequest struct {
	Domain string `json:"domain"`
}

// PolicyAllowDomainRequest defines the API inputs for policy allow domain.
type PolicyAllowDomainRequest struct {
	Domain string `json:"domain"`
}

// CreateRangeRequest defines the API inputs for create range.
type CreateRangeRequest struct {
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

// UpdateRangeRequest defines the API inputs for update range.
type UpdateRangeRequest struct {
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

// AuthLoginRequest defines the API inputs for auth login.
type AuthLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AddPeerRequest defines the API inputs for add peer.
type AddPeerRequest struct {
	URL string `json:"url"`
}

// ConfirmPeerRequest defines the API inputs for confirm peer.
type ConfirmPeerRequest struct {
	PeerURL     string `json:"peer_url"`
	PairingCode string `json:"pairing_code"`
}

// SyncPairCompleteRequest defines the API inputs for sync pair complete.
type SyncPairCompleteRequest struct {
	PairingCode string `json:"pairing_code"`
	PeerURL     string `json:"peer_url"`
	Proof       string `json:"proof"`
}

// CreateUpstreamRequest defines the API inputs for create upstream.
type CreateUpstreamRequest struct {
	Upstream string `json:"upstream"`
	Enabled  bool   `json:"enabled"`
}

// UpdateUpstreamRequest defines the API inputs for update upstream.
type UpdateUpstreamRequest struct {
	Upstream string `json:"upstream"`
}

// DomainRuleRequest is shared by range custom block and allow operations.
type DomainRuleRequest struct {
	Domain string `json:"domain"`
}
