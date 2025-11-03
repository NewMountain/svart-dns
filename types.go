package main

type QueryLog struct {
	Timestamp    string
	ClientIP     string
	ClientIPRaw  string
	QueryName    string
	QueryType    string
	ResponseCode string
	LatencyMs    int64
}

type TopItem struct {
	Domain      string
	ClientIP    string
	ClientIPRaw string
	Count       int64
}

type Client struct {
	IPAddress  string
	Alias      string
	QueryCount int64
	LastSeen   string
	IsMember   bool
}

type Group struct {
	ID             int
	Name           string
	MemberCount    int
	BlocklistCount int
	IsMember       bool
}

type Member struct {
	IPAddress  string
	Alias      string
	QueryCount int64
	LastSeen   string
}

type BlocklistAssignment struct {
	ID          int
	Alias       string
	DomainCount int
	IsAssigned  bool
	Source      string
}

type BlocklistResult struct {
	ID          int
	Alias       string
	Enabled     bool
	IsBlocked   bool
	MatchedRule string
}

type RewriteView struct {
	ID          int
	Domain      string
	IPAddresses string
	Enabled     bool
}
