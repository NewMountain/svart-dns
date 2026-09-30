package main

type QueryLog struct {
	Timestamp           string
	ClientIP            string
	ClientIPRaw         string
	QueryName           string
	QueryType           string
	ResponseCode        string
	LatencyMicroseconds int64
}

type TopItem struct {
	Domain      string
	ClientIP    string
	ClientIPRaw string
	Count       int64
}

type Client struct {
	IPAddress  string `json:"ip_address"`
	Alias      string `json:"alias"`
	QueryCount int64  `json:"query_count"`
	LastSeen   string `json:"last_seen"`
	IsMember   bool   `json:"is_member"`
}

type Group struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	MemberCount    int    `json:"member_count"`
	BlocklistCount int    `json:"blocklist_count"`
	IsMember       bool   `json:"is_member"`
}

type Member struct {
	IPAddress  string `json:"ip_address"`
	Alias      string `json:"alias"`
	QueryCount int64  `json:"query_count"`
	LastSeen   string `json:"last_seen"`
}

type BlocklistAssignment struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
	Source      string `json:"source"`
}

type BlocklistResult struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	Enabled     bool   `json:"enabled"`
	IsBlocked   bool   `json:"is_blocked"`
	MatchedRule string `json:"matched_rule"`
}

type RewriteView struct {
	ID          int    `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}
