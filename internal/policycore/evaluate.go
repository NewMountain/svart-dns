package policycore

import (
	"math/bits"
	"net/netip"
	"strings"
)

// PolicyResult represents the final policy decision for a client+domain query.
type PolicyResult struct {
	ClientIP        string          `json:"client_ip"`
	Domain          string          `json:"domain"`
	RecordType      uint16          `json:"record_type"`
	Result          string          `json:"result"`                     // "block", "allow", ""
	ResultSource    *EntityResult   `json:"result_source,omitempty"`    // entity that made the final call
	RangeEvaluation *TierEvaluation `json:"range_evaluation,omitempty"` // nil when no ranges matched
	GroupEvaluation *TierEvaluation `json:"group_evaluation,omitempty"` // nil when no groups assigned
	IPEvaluation    *TierEvaluation `json:"ip_evaluation,omitempty"`    // nil when no IP-level data
}

// TierEvaluation captures the decision at a single policy tier.
type TierEvaluation struct {
	Entities     []EntityResult `json:"entities"`
	Result       string         `json:"result"`                  // "block", "allow", ""
	ResultSource *EntityResult  `json:"result_source,omitempty"` // the entity that decided
}

// EntityResult captures one entity's decision within a tier.
type EntityResult struct {
	Tier          string        `json:"tier"` // "range", "group", "ip"
	Name          string        `json:"name"`
	Result        string        `json:"result"`                   // "block", "allow", ""
	PublishedList *PublishedHit `json:"published_list,omitempty"` // which published list matched
	CustomRule    *CustomHit    `json:"custom_rule,omitempty"`    // which custom rule applied
}

// PublishedHit records a match from a published blocklist/allowlist.
type PublishedHit struct {
	Action   string `json:"action"` // "block" or "allow"
	Rule     string `json:"rule"`
	ListID   int    `json:"list_id"`
	ListName string `json:"list_name"`
}

// CustomHit records a match from a manual custom rule (no published list).
type CustomHit struct {
	Action string `json:"action"` // "block" or "allow"
	Rule   string `json:"rule"`
}

// Evaluate is the three-tier cascade (DD-002) over one snapshot. The query
// name is probed against the index once; every entity of every tier then
// costs a few word operations on that probe.
func (s *Snapshot) Evaluate(clientIP string, addr netip.Addr, domain string, rrtype uint16) *PolicyResult {
	var p Probe
	arpaIP := ""
	if strings.HasSuffix(domain, ".arpa") {
		arpaIP = ReverseARPAToIP(domain)
	}
	s.Index.ProbeDomain(&p, domain, arpaIP, rrtype)
	client := addr.Unmap()
	groups := s.Memberships[clientIP]
	direct := s.Clients[clientIP]
	ranges := 0
	for i := range s.Ranges {
		if s.Ranges[i].Prefix.Contains(client) {
			ranges++
		}
	}
	entities := ranges + len(groups)
	if direct != nil {
		entities++
	}

	// One allocation for the result and its three tier evaluations, one for
	// every entity of every tier (a nil-safe empty slice when there are none,
	// so tiers without entities still serialize as []).
	block := new(struct {
		Result PolicyResult
		Tiers  [3]TierEvaluation
	})
	all := make([]EntityResult, 0, entities)
	result := &block.Result
	*result = PolicyResult{ClientIP: clientIP, Domain: domain, RecordType: rrtype,
		RangeEvaluation: &block.Tiers[0], GroupEvaluation: &block.Tiers[1], IPEvaluation: &block.Tiers[2]}

	rangeEval := result.RangeEvaluation
	rangeEval.Entities = all[:0:ranges]
	for i := range s.Ranges {
		if r := &s.Ranges[i]; r.Prefix.Contains(client) {
			rangeEval.Entities = append(rangeEval.Entities, s.EntityResult(&p, "range", r.Name, &r.Lists, false))
		}
	}
	rangeEval.DecideAcrossEntities()

	groupEval := result.GroupEvaluation
	groupEval.Entities = all[ranges : ranges : ranges+len(groups)]
	for _, g := range groups {
		groupEval.Entities = append(groupEval.Entities, s.EntityResult(&p, "group", g.Name, &g.Lists, false))
	}
	groupEval.DecideAcrossEntities()

	ipEval := result.IPEvaluation
	ipEval.Entities = all[ranges+len(groups) : ranges+len(groups) : entities]
	if direct != nil {
		ipEval.Entities = append(ipEval.Entities, s.EntityResult(&p, "ip", clientIP, direct, true))
		if er := &ipEval.Entities[0]; er.Result != "" {
			ipEval.Result = er.Result
			ipEval.ResultSource = er
		}
	}

	// Narrowest tier with a decision wins (IP > Group > Range).
	for _, t := range [...]*TierEvaluation{ipEval, groupEval, rangeEval} {
		if t.Result != "" {
			result.Result = t.Result
			result.ResultSource = t.ResultSource
			return result
		}
	}
	result.Result = "allow"
	result.ResultSource = DefaultAllowSource
	return result
}

// DefaultAllowSource is the source of every default-allow decision. Policy
// results are shared read-only (the cache hands the same one to every
// caller), so one value serves them all.
var DefaultAllowSource = &EntityResult{Tier: "default", Name: "no matching rule", Result: "allow"}

// DecideAcrossEntities sets the tier's decision: a blocking entity wins over
// allowing ones, and the first entity in order with that result is the source.
func (t *TierEvaluation) DecideAcrossEntities() {
	for _, want := range [...]string{"block", "allow"} {
		for i := range t.Entities {
			if t.Entities[i].Result == want {
				t.Result = want
				t.ResultSource = &t.Entities[i]
				return
			}
		}
	}
}

// EntityResult evaluates one entity (a range, a group or the client itself)
// against its block and allow lists. Both sides are always checked so the
// evaluation shows the full picture; within the entity allow wins.
// manualIsCustom reports a hit in a manual list as a custom rule. Only the IP
// tier does that: group and range manual lists have always been attributed
// as published lists.
func (s *Snapshot) EntityResult(p *Probe, tier, name string, lists *EntityLists, manualIsCustom bool) EntityResult {
	er := EntityResult{Tier: tier, Name: name}
	ix := s.Index
	bm, blockMatched := ix.FirstMatch(p, lists.Block, true)
	am, allowMatched := ix.FirstMatch(p, lists.Allow, false)
	published := func(m IndexMatch) bool { return !manualIsCustom || !ix.Lists[m.Slot].Manual }

	if blockMatched {
		if l := &ix.Lists[bm.Slot]; published(bm) {
			er.PublishedList = &PublishedHit{Action: "block", Rule: bm.Rule(), ListID: l.ID, ListName: l.Name}
		} else {
			er.CustomRule = &CustomHit{Action: "block", Rule: bm.Rule()}
		}
	}
	if allowMatched {
		allowRule := am.Rule()
		switch {
		case published(am):
			if er.PublishedList == nil {
				l := &ix.Lists[am.Slot]
				er.PublishedList = &PublishedHit{Action: "allow", Rule: allowRule, ListID: l.ID, ListName: l.Name}
			}
			// A published allow over a published block: the block stays the
			// published hit and the allow is reported as the override.
			if blockMatched && er.PublishedList.Action == "block" {
				er.CustomRule = &CustomHit{Action: "allow", Rule: allowRule}
			}
		case er.CustomRule == nil || er.CustomRule.Action == "block":
			er.CustomRule = &CustomHit{Action: "allow", Rule: allowRule}
		}
	}
	switch {
	case allowMatched:
		er.Result = "allow"
	case blockMatched:
		er.Result = "block"
	}
	return er
}

// ResponseIPsBlocked reports whether an answer's A/AAAA addresses hit an
// IP-literal rule, with Evaluate's cascade: the narrowest tier with an
// opinion wins, block beats allow across the entities of a tier, and allow
// beats block within an entity. Each IP is matched as A or AAAA, including
// typed IP-literal rules and their list-local exceptions.
func (s *Snapshot) ResponseIPsBlocked(clientIP string, ips []string) bool {
	ix := s.Index
	var matched [MaxProbeWords]uint64
	var probe Probe
	for _, value := range ips {
		addr, err := netip.ParseAddr(value)
		if err != nil {
			continue
		}
		ix.probeAddress(&probe, addr)
		for w := 0; w < ix.Words; w++ {
			candidates := probe.Any[w] | probe.Complex[w]
			for candidates != 0 {
				slot := w*64 + bits.TrailingZeros64(candidates)
				candidates &= candidates - 1
				if _, ok := ix.matchSlot(&probe, slot, false); ok {
					matched[w] |= 1 << (uint(slot) & 63)
				}
			}
		}
	}
	hits := func(l ListSet) bool { return len(l) != 0 && LowestCommon(matched[:ix.Words], l) >= 0 }
	entity := func(l *EntityLists) string {
		switch {
		case hits(l.Allow):
			return "allow"
		case hits(l.Block):
			return "block"
		}
		return ""
	}

	if l := s.Clients[clientIP]; l != nil {
		if r := entity(l); r != "" {
			return r == "block"
		}
	}
	var groupOpinions []string
	for _, g := range s.Memberships[clientIP] {
		groupOpinions = append(groupOpinions, entity(&g.Lists))
	}
	if r := TierOpinion(groupOpinions); r != "" {
		return r == "block"
	}
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	var rangeOpinions []string
	for i := range s.Ranges {
		if s.Ranges[i].Prefix.Contains(addr) {
			rangeOpinions = append(rangeOpinions, entity(&s.Ranges[i].Lists))
		}
	}
	return TierOpinion(rangeOpinions) == "block"
}

// TierOpinion folds the entities of one tier: block wins the tie (DD-002).
func TierOpinion(opinions []string) string {
	decided := ""
	for _, r := range opinions {
		if r == "block" {
			return r
		}
		if r == "allow" {
			decided = r
		}
	}
	return decided
}
