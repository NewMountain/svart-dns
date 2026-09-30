package svart

// QueryLog records a DNS request, its outcome, and filtering attribution.
type QueryLog struct {
	Timestamp           string
	ClientIP            string
	ClientIPRaw         string
	QueryName           string
	QueryType           string
	ResponseCode        string
	LatencyMicroseconds int64
}

// TopItem holds a query count grouped by domain or client.
type TopItem struct {
	Domain      string
	ClientIP    string
	ClientIPRaw string
	Count       int64
}

// Client presents a client identity with activity and membership information.
type Client struct {
	IPAddress  string `json:"ip_address"`
	Alias      string `json:"alias"`
	QueryCount int64  `json:"query_count"`
	LastSeen   string `json:"last_seen"`
	IsMember   bool   `json:"is_member"`
}

// Group presents a group with membership and list counts.
type Group struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	MemberCount    int    `json:"member_count"`
	BlocklistCount int    `json:"blocklist_count"`
	IsMember       bool   `json:"is_member"`
}

// Member presents the identity and activity of a group member.
type Member struct {
	IPAddress  string `json:"ip_address"`
	Alias      string `json:"alias"`
	QueryCount int64  `json:"query_count"`
	LastSeen   string `json:"last_seen"`
}

// BlocklistAssignment describes a list assignment and its policy source.
type BlocklistAssignment struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	DomainCount int    `json:"domain_count"`
	IsAssigned  bool   `json:"is_assigned"`
	Source      string `json:"source"`
}

// BlocklistResult reports a list match and the rule responsible.
type BlocklistResult struct {
	ID          int    `json:"id"`
	Alias       string `json:"alias"`
	Enabled     bool   `json:"enabled"`
	IsBlocked   bool   `json:"is_blocked"`
	MatchedRule string `json:"matched_rule"`
}

// RewriteView presents rewrite data in the API.
type RewriteView struct {
	ID          int    `json:"id"`
	Domain      string `json:"domain"`
	IPAddresses string `json:"ip_addresses"`
	Enabled     bool   `json:"enabled"`
}
