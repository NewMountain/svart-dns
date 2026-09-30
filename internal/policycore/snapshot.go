package policycore

import (
	"net"
	"net/netip"
	"slices"
)

// Snapshot is the complete in-memory policy state: every enabled list
// in one shared Index plus, per entity of every tier, which lists it
// uses. It is built in one piece by reloadPolicyState and never modified
// afterwards, so the DNS hot path reads it through one atomic load and every
// query sees one consistent version. Keeping tiers in separately reloaded
// stores is what let ranges keep serving stale lists (B1).
type Snapshot struct {
	Index *Index
	// ranges are sorted most specific first (longest prefix), ID order among
	// equal lengths.
	Ranges []RangePolicy
	// memberships maps a client address, as stored, to its groups in the
	// order the database returns them.
	Memberships map[string][]*GroupPolicy
	// clients holds the direct (IP tier) lists of every client that has at
	// least one non-empty enabled list; any other client has no IP tier.
	Clients map[string]*EntityLists
}

// EntityLists are the list slots one entity uses, policy lists included.
type EntityLists struct {
	Block ListSet
	Allow ListSet
}

// RangePolicy binds a network prefix to the lists used during evaluation.
type RangePolicy struct {
	Prefix netip.Prefix
	Name   string
	Lists  EntityLists
}

// GroupPolicy binds a named client group to its policy lists.
type GroupPolicy struct {
	Name  string
	Lists EntityLists
}

// Config is the policy configuration as stored: the input of
// BuildSnapshot, read in one database transaction by readPolicyConfig.
// List IDs are blocklist IDs in block fields and allowlist IDs in allow
// fields; IDs of disabled or missing lists are ignored when building.
type Config struct {
	Lists       []List
	Ranges      []RangeConfig // ID order
	Groups      map[int]GroupConfig
	Memberships []GroupMembership // database order
	Clients     map[string]*ListIDs
	Policies    map[int]*ListIDs
}

// ListIDs selects blocklists, allowlists and an optional shared policy.
type ListIDs struct {
	Block, Allow []int
	PolicyID     int
}

// RangeConfig is a stored network range and its selected lists.
type RangeConfig struct {
	ID   int
	Name string
	CIDR string
	ListIDs
}

// GroupConfig is a stored client group and its selected lists.
type GroupConfig struct {
	Name string
	ListIDs
}

// GroupMembership associates a client address with a stored group.
type GroupMembership struct {
	ClientIP string
	GroupID  int
}

// WithPolicy is the entity's own lists plus its named policy's lists.
func (c *ListIDs) WithPolicy(policies map[int]*ListIDs) (block, allow []int) {
	block, allow = c.Block, c.Allow
	if p := policies[c.PolicyID]; c.PolicyID > 0 && p != nil {
		block = append(slices.Clip(block), p.Block...)
		allow = append(slices.Clip(allow), p.Allow...)
	}
	return block, allow
}

// ParseRangeCIDR parses a stored range the way the API validates it
// (net.ParseCIDR) and returns the equivalent netip.Prefix.
func ParseRangeCIDR(cidr string) (netip.Prefix, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return netip.Prefix{}, err
	}
	addr, _ := netip.AddrFromSlice(ipNet.IP)
	ones, _ := ipNet.Mask.Size()
	return netip.PrefixFrom(addr, ones), nil
}

// BuildSnapshot combines a configuration with the index holding its
// lists. It is pure; cfg.lists must be the lists ix was built from.
func BuildSnapshot(cfg Config, ix *Index) *Snapshot {
	s := &Snapshot{
		Index:       ix,
		Memberships: make(map[string][]*GroupPolicy),
		Clients:     make(map[string]*EntityLists),
	}
	listsFor := func(c *ListIDs) EntityLists {
		block, allow := c.WithPolicy(cfg.Policies)
		return EntityLists{Block: ix.SetFor(block, false), Allow: ix.SetFor(allow, true)}
	}

	for _, r := range cfg.Ranges {
		prefix, err := ParseRangeCIDR(r.CIDR)
		if err != nil {
			continue
		}
		s.Ranges = append(s.Ranges, RangePolicy{Prefix: prefix, Name: r.Name, Lists: listsFor(&r.ListIDs)})
	}
	slices.SortStableFunc(s.Ranges, func(a, b RangePolicy) int { return b.Prefix.Bits() - a.Prefix.Bits() })

	groups := make(map[int]*GroupPolicy, len(cfg.Groups))
	for id, g := range cfg.Groups {
		groups[id] = &GroupPolicy{Name: g.Name, Lists: listsFor(&g.ListIDs)}
	}
	for _, m := range cfg.Memberships {
		if g := groups[m.GroupID]; g != nil {
			s.Memberships[m.ClientIP] = append(s.Memberships[m.ClientIP], g)
		}
	}

	// An empty list can never match, so a client whose lists are all empty
	// has no IP tier at all (no entity in ip_evaluation).
	nonEmpty := make(ListSet, ix.Words)
	for slot, l := range ix.Lists {
		if l.Count > 0 {
			nonEmpty[slot>>6] |= 1 << (uint(slot) & 63)
		}
	}
	for ip, c := range cfg.Clients {
		l := listsFor(c)
		if LowestCommon(l.Block, nonEmpty) >= 0 || LowestCommon(l.Allow, nonEmpty) >= 0 {
			s.Clients[ip] = &l
		}
	}
	return s
}
