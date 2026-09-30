package main

type CreateAllowlistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval *int   `json:"refresh_interval"`
}

type UpdateAllowlistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	RefreshInterval *int   `json:"refresh_interval"`
}

type ClientAllowDomainRequest struct {
	Domain string `json:"domain"`
}

type GroupAllowDomainRequest struct {
	Domain string `json:"domain"`
}

type CreateAPITokenRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type CreateAdminUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

type UpdateAdminUserRequest struct {
	Role     string `json:"role"`
	Password string `json:"password"`
}

type CreateBlocklistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	Enabled         bool   `json:"enabled"`
	RefreshInterval *int   `json:"refresh_interval"`
}

type UpdateBlocklistRequest struct {
	URL             string `json:"url"`
	Alias           string `json:"alias"`
	RefreshInterval *int   `json:"refresh_interval"`
}

type ClientBlockDomainRequest struct {
	Domain string `json:"domain"`
}

type GroupBlockDomainRequest struct {
	Domain string `json:"domain"`
}

type UpdateSettingRequest struct {
	Value string `json:"value"`
}

type AddBootstrapServerRequest struct {
	Server string `json:"server"`
}

type ReplaceBootstrapServersRequest struct {
	Servers []string `json:"servers"`
}

type UpdateClientAliasRequest struct {
	Alias string `json:"alias"`
}

type CreateGroupRequest struct {
	Name string `json:"name"`
}

type UpdateGroupRequest struct {
	Name string `json:"name"`
}

type AssignGroupBlocklistsRequest struct {
	BlocklistIDs []int `json:"blocklist_ids"`
}

type AddGroupMembersRequest struct {
	ClientIPs []string `json:"client_ips"`
}

type CreateRewriteRequest struct {
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}

type UpdateRewriteRequest struct {
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     *bool  `json:"enabled"`
}

type CreateRewritesBatchRequest struct {
	Rewrites []CreateRewriteRequest `json:"rewrites"`
}

type InvestigateRequest struct {
	SQL     string `json:"sql"`
	Timeout int    `json:"timeout"`
}

type CreatePolicyRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type UpdatePolicyRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type PolicyBlockDomainRequest struct {
	Domain string `json:"domain"`
}

type PolicyAllowDomainRequest struct {
	Domain string `json:"domain"`
}

type CreateRangeRequest struct {
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

type UpdateRangeRequest struct {
	Name string `json:"name"`
	CIDR string `json:"cidr"`
}

type AuthLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AddPeerRequest struct {
	URL string `json:"url"`
}

type ConfirmPeerRequest struct {
	PeerURL     string `json:"peer_url"`
	PairingCode string `json:"pairing_code"`
}

type SyncPairCompleteRequest struct {
	PairingCode string `json:"pairing_code"`
	PeerURL     string `json:"peer_url"`
	Proof       string `json:"proof"`
}

type CreateUpstreamRequest struct {
	Upstream string `json:"upstream"`
	Enabled  bool   `json:"enabled"`
}

type UpdateUpstreamRequest struct {
	Upstream string `json:"upstream"`
}

// DomainRuleRequest is shared by range custom block and allow operations.
type DomainRuleRequest struct {
	Domain string `json:"domain"`
}
