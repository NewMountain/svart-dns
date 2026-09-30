package policycore

import (
	"hash/maphash"
	"math"
	"math/bits"
	"slices"
	"strings"
)

// Index is the single in-memory store for every enabled blocklist and
// allowlist. Each distinct rule string is stored once, however many lists
// contain it, together with the set of lists ("slots") that contain it.
//
// Why it looks like this: the previous representation kept one
// map[string]bool per list (~64 B per domain per list, all GC-scanned) and
// probed each list separately at each suffix level, allocating a joined
// string per probe. Here the arena and tables hold no Go pointers, so the GC
// never scans them, and a query costs one probe per label for all lists at
// once. Measurements are in design/BENCHMARKS.md (listindex entries).
//
// Slots are assigned in ascending list-size order (ID breaks ties), so the
// lowest set bit of any hit set is the smallest matching list: the project's
// attribution rule ("the most specific list gets credit").
//
// A built index is immutable and safe for concurrent readers.
type Index struct {
	Seed  maphash.Seed
	Words int // uint64 words per list set

	Exact DomainTable // "example.com": the name and all its subdomains
	Wild  DomainTable // "*.example.com" stored as "example.com": subdomains only

	// members holds every distinct list set, words-wide, back to back.
	// A table entry refers to its set by index.
	Members []uint64

	Qualified []QualifiedRule
	Complex   []ComplexRule
	Lists     []List

	// Legacy exceptions a list makes to its own rules (ABP @@ rules
	// stored with listparse.ExceptionPrefix). Same slots and member sets as
	// the rule tables; evaluated once for the query probe.
	ExExact, ExWild DomainTable
	ExComplex       []ComplexRule
}

// List describes the list occupying one slot.
type List struct {
	ID     int
	Name   string
	Allow  bool // allowlist (true) or blocklist (false)
	Manual bool // custom rules entered in the UI rather than a published list
	Count  int
}

// ComplexRule is a wildcard pattern pre-split for allocation-free matching.
type ComplexRule struct {
	Pattern string
	Parts   []string // pattern split on '*', precomputed so matching allocates nothing
	Slot    int
}

// NewComplexRule copies and compiles a pattern for the given list slot.
func NewComplexRule(pattern string, slot int) ComplexRule {
	pattern = strings.Clone(pattern)
	return ComplexRule{Pattern: pattern, Parts: strings.Split(pattern, "*"), Slot: slot}
}

// Matches is matchWildcard with the split hoisted to build time; it keeps
// matchWildcard's exact semantics (TestDomainIndexMatchesOldEngine pins them).
func (r *ComplexRule) Matches(domain string) bool {
	parts := r.Parts
	if len(parts) < 2 {
		return false
	}
	if parts[0] != "" && !strings.HasPrefix(domain, parts[0]) {
		return false
	}
	// See matchWildcard: a pattern ending in "." is not end-anchored.
	if last := parts[len(parts)-1]; last != "" && !strings.HasSuffix(last, ".") && !strings.HasSuffix(domain, last) {
		return false
	}
	pos := len(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		idx := strings.Index(domain[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	return true
}

// DomainShard is an open-addressing hash set of strings stored in a byte
// arena. slots[i] packs the arena offset (high 32 bits) and the member-set id
// (low 32 bits); tags[i] is 0 for an empty slot, otherwise a hash fragment
// that filters most mismatches before touching the arena.
type DomainShard struct {
	Arena []byte // [len byte][bytes]..., offset 0 is a sentinel
	Slots []uint64
	Tags  []uint8
	Count int
	Mask  uint64
}

// KeyAt reads the length-prefixed key at an arena offset.
func (t *DomainShard) KeyAt(off uint32) string {
	n := uint32(t.Arena[off])
	return string(t.Arena[off+1 : off+1+n])
}

// Compare the arena directly so Go can eliminate the temporary string copy,
// including names longer than its small stack conversion buffer.
func (t *DomainShard) keyEquals(off uint32, value string) bool {
	n := uint32(t.Arena[off])
	return string(t.Arena[off+1:off+1+n]) == value
}

// TagOf extracts a nonzero hash fragment for fast mismatch rejection.
func TagOf(h uint64) uint8 { return uint8(h>>56) | 1 }

// Lookup returns the member-set id for s, or -1.
func (t *DomainShard) Lookup(h uint64, s string) int32 {
	if t.Count == 0 {
		return -1
	}
	tag := TagOf(h)
	for i := h & t.Mask; ; i = (i + 1) & t.Mask {
		switch t.Tags[i] {
		case 0:
			return -1
		case tag:
			v := t.Slots[i]
			if t.keyEquals(uint32(v>>32), s) {
				return memberID(v)
			}
		}
	}
}

// ListSet is a words-wide bitset over list slots.
type ListSet []uint64

// Has reports whether slot is present in this list bitset.
func (s ListSet) Has(slot int) bool { return s[slot>>6]&(1<<(uint(slot)&63)) != 0 }

// LowestCommon returns the lowest slot set in both a and b, or -1.
func LowestCommon(a, b []uint64) int {
	for w := range a {
		if x := a[w] & b[w]; x != 0 {
			return w<<6 + bits.TrailingZeros64(x)
		}
	}
	return -1
}

// SetOf returns the list set for a member id.
func (ix *Index) SetOf(id int) []uint64 {
	return ix.Members[id*ix.Words : (id+1)*ix.Words]
}

// MatchKind says how a rule matched, in the precedence order the policy
// engine has always used for a single list.
type MatchKind uint8

// Match kinds follow exact, reverse-address, parent, wildcard and complex precedence.
const (
	KindNone MatchKind = iota
	KindExact
	KindARPA
	KindParent
	KindWildcard
	KindComplex
)

// IndexMatch is one list's match: which list, how, and the rule text. For
// exact/parent/wildcard/arpa the rule is a substring of the queried name (or
// of the ARPA-derived IP), so building it allocates nothing.
type IndexMatch struct {
	Slot int
	Kind MatchKind
	Text string
}

// Rule renders the matched rule the way it appears in lists and logs.
func (m IndexMatch) Rule() string {
	if m.Kind == KindWildcard {
		return "*." + m.Text
	}
	return m.Text
}

// Probe collects, for one queried name, every table hit at every suffix
// level. It is computed once per query and reused for every entity, so
// evaluating many ranges/groups costs a few word ANDs each, not more lookups.
// Most names hit nothing, so hits are stored sparsely.
//
// A probe lives on the caller's stack: it never holds a pointer into itself
// (a slice over its own inline array would make every probe escape to the
// heap). Hits beyond the inline array spill to
// the heap, which only very deep names reach.
type Probe struct {
	Domain          string
	ArpaIP          string
	RecordType      uint16
	arpaException   int32
	candidateN      int
	candidateInline [8]candidateHit
	candidateSpill  []candidateHit
	exempt          [MaxProbeWords]uint64
	N               int // hits held in inline
	Inline          [8]ProbeHit
	Spill           []ProbeHit // every hit, once more than len(inline) were found
	// any is the union of every set hit, so an entity whose lists all miss is
	// rejected with one AND per word.
	Any [MaxProbeWords]uint64
	// complex has the slots with a complex wildcard rule matching domain,
	// evaluated once here rather than once per entity.
	Complex [MaxProbeWords]uint64
}

// ProbeHit identifies a member set matched at one domain suffix.
type ProbeHit struct {
	Kind  MatchKind // kindExact (level 0), kindParent, kindWildcard, kindARPA
	ID    int32     // member-set id
	Level uint8     // byte offset of the suffix within domain (or 0 for ARPA)
}

const (
	// MaxProbeWords provides 64 lists per word; beyond this capacity the index refuses to
	// build rather than silently ignoring lists. Every client with custom
	// rules has its own manual list, so the limit is sized for thousands of
	// clients; probes only touch the words an index actually uses.
	MaxProbeWords = 32
	// MaxIndexLists is the total number of list slots supported by a probe.
	MaxIndexLists = MaxProbeWords * 64
)

// ProbeDomain evaluates domain (lowercased, no trailing dot, at most 253
// bytes as DNS guarantees) against the index. arpaIP is the IP a
// reverse-Lookup name encodes, or "". Compact table probes are bounded by
// the label count (at most 127). Qualified patterns run once per probe.
func (ix *Index) ProbeDomain(p *Probe, domain, arpaIP string, rrtype uint16) {
	p.reset(ix, domain, arpaIP, rrtype)
	if len(domain) > 255 {
		return
	}
	for i := range ix.Complex {
		if r := &ix.Complex[i]; r.Matches(domain) {
			p.Complex[r.Slot>>6] |= 1 << (uint(r.Slot) & 63)
			p.addCandidate(candidateHit{index: -i - 1, kind: KindComplex})
		}
	}
	if len(ix.Qualified) != 0 {
		ix.probeQualified(p, false)
	}
	if ix.ExExact.Count != 0 || ix.ExWild.Count != 0 || len(ix.ExComplex) != 0 {
		ix.probeExceptions(p, false)
	}
	off := 0
	for {
		s := domain[off:]
		if id := ix.LookupExact(s); id >= 0 {
			kind := KindParent
			if off == 0 {
				kind = KindExact
			}
			p.Add(ix, ProbeHit{Kind: kind, ID: id, Level: uint8(off)})
		}
		if off > 0 {
			if id := ix.LookupWild(s); id >= 0 {
				p.Add(ix, ProbeHit{Kind: KindWildcard, ID: id, Level: uint8(off)})
			}
		}
		dot := strings.IndexByte(s, '.')
		if dot < 0 {
			break
		}
		off += dot + 1
	}
	if arpaIP != "" {
		if id := ix.LookupExact(arpaIP); id >= 0 {
			p.Add(ix, ProbeHit{Kind: KindARPA, ID: id})
		}
	}
}

// Add merges a member-set hit into the query probe.
func (p *Probe) Add(ix *Index, h ProbeHit) {
	active := false
	for w, x := range ix.SetOf(int(h.ID)) {
		p.Any[w] |= x
		active = active || x != 0
	}
	if !active {
		return
	}

	switch {
	case p.Spill != nil:
		p.Spill = append(p.Spill, h)
	case p.N < len(p.Inline):
		p.Inline[p.N] = h
		p.N++
	default:
		p.Spill = append(append(make([]ProbeHit, 0, 4*len(p.Inline)), p.Inline[:]...), h)
	}
}

// Hits are the table hits in level order.
func (p *Probe) Hits() []ProbeHit {
	if p.Spill != nil {
		return p.Spill
	}
	return p.Inline[:p.N]
}

// LookupExact returns the exact table member-set ID, or -1.
func (ix *Index) LookupExact(s string) int32 {
	return ix.Exact.Lookup(maphash.String(ix.Seed, s), s)
}

// LookupWild returns the wildcard table member-set ID, or -1.
func (ix *Index) LookupWild(s string) int32 {
	return ix.Wild.Lookup(maphash.String(ix.Seed, s), s)
}

// FirstMatch returns the smallest list in lists that Matches the probe, with
// the rule that matched. checkARPA mirrors the historical rule that reverse
// lookups are checked against blocklists only.
func (ix *Index) FirstMatch(p *Probe, lists ListSet, checkARPA bool) (IndexMatch, bool) {
	if len(lists) == 0 {
		return IndexMatch{}, false
	}
	// Lists in slot (attribution) order; within one list an exact, ARPA,
	// parent or wildcard rule outranks a complex pattern. A list whose own
	// exceptions cover the name does not match it.
	for w := range lists {
		cand := (p.Any[w] | p.Complex[w]) & lists[w]
		for cand != 0 {
			slot := w<<6 + bits.TrailingZeros64(cand)
			cand &= cand - 1
			if m, ok := ix.matchSlot(p, slot, checkARPA); ok {
				return m, true
			}
		}
	}
	return IndexMatch{}, false
}

// MatchInSlot reports how slot matched, in single-list precedence order:
// exact name, reverse-ARPA IP, nearest parent, nearest wildcard suffix.
func (ix *Index) MatchInSlot(p *Probe, slot int, checkARPA bool) (IndexMatch, bool) {
	in := func(h ProbeHit) bool { return ListSet(ix.SetOf(int(h.ID))).Has(slot) }
	for _, kind := range [...]MatchKind{KindExact, KindARPA, KindParent, KindWildcard} {
		if kind == KindARPA && !checkARPA {
			continue
		}
		for _, h := range p.Hits() {
			if h.Kind != kind || !in(h) {
				continue
			}
			rule := p.Domain[h.Level:]
			if kind == KindARPA {
				rule = p.ArpaIP
			}
			return IndexMatch{Slot: slot, Kind: kind, Text: rule}, true
		}
	}
	return IndexMatch{}, false
}

// WithLists returns an index with the same rules for lists, the enabled lists
// of a new configuration, or nil when the set of lists differs and the index
// has to be rebuilt. Names and the manual flag come from lists; they do not
// affect the rules, so a rename reuses the rule tables as they are.
func (ix *Index) WithLists(lists []List) *Index {
	if len(lists) != len(ix.Lists) {
		return nil
	}
	renamed := slices.Clone(ix.Lists)
	changed := false
	for _, l := range lists {
		slot := ix.SlotOf(l.ID, l.Allow)
		if slot < 0 {
			return nil
		}
		if renamed[slot].Name != l.Name || renamed[slot].Manual != l.Manual {
			renamed[slot].Name, renamed[slot].Manual = l.Name, l.Manual
			changed = true
		}
	}
	if !changed {
		return ix
	}
	copied := *ix
	copied.Lists = renamed
	return &copied
}

// SlotOf returns the slot for a list ID and kind, or -1.
func (ix *Index) SlotOf(id int, allow bool) int {
	for i, l := range ix.Lists {
		if l.ID == id && l.Allow == allow {
			return i
		}
	}
	return -1
}

// SetFor builds the list set for the given list IDs of one kind. Unknown IDs
// (disabled lists, lists still loading) are simply absent.
func (ix *Index) SetFor(ids []int, allow bool) ListSet {
	s := make(ListSet, ix.Words)
	for _, id := range ids {
		if slot := ix.SlotOf(id, allow); slot >= 0 {
			s[slot>>6] |= 1 << (uint(slot) & 63)
		}
	}
	return s
}

// Contains reports whether the list in slot contains the exact rule string.
func (ix *Index) Contains(slot int, rule string) bool {
	for _, r := range ix.Qualified {
		if r.Slot == slot && !r.Rule.Exception && (r.Stored == rule || r.Rule.Text == rule) {
			return true
		}
	}
	table, key := &ix.Exact, rule
	if strings.HasPrefix(rule, "*.") && !strings.Contains(rule[2:], "*") {
		table, key = &ix.Wild, rule[2:]
	} else if strings.Contains(rule, "*") {
		for _, r := range ix.Complex {
			if r.Slot == slot && r.Pattern == rule {
				return true
			}
		}
		return false
	}
	id := table.Lookup(maphash.String(ix.Seed, key), key)
	return id >= 0 && ListSet(ix.SetOf(int(id))).Has(slot)
}

// Rules calls fn for every rule in slot, in no particular order.
func (ix *Index) Rules(slot int, fn func(rule string)) {
	each := func(table *DomainTable, prefix string) {
		for si := range table.Shards {
			t := &table.Shards[si]
			for i, tag := range t.Tags {
				if tag == 0 {
					continue
				}
				v := t.Slots[i]
				if ListSet(ix.SetOf(int(memberID(v)))).Has(slot) {
					fn(prefix + t.KeyAt(uint32(v>>32)))
				}
			}
		}
	}

	for _, r := range ix.Qualified {
		if r.Slot == slot && !r.Rule.Exception {
			fn(r.Rule.Text)
		}
	}
	each(&ix.Exact, "")
	each(&ix.Wild, "*.")
	for _, r := range ix.Complex {
		if r.Slot == slot {
			fn(r.Pattern)
		}
	}
}

// ApproxBytes is the index's heap footprint, for the memory gauges.
func (ix *Index) ApproxBytes() int64 {
	t := func(d *DomainTable) int64 {
		var bytes int64
		for i := range d.Shards {
			s := &d.Shards[i]
			bytes += int64(cap(s.Arena) + cap(s.Slots)*8 + cap(s.Tags))
		}
		return bytes
	}

	n := t(&ix.Exact) + t(&ix.Wild) + t(&ix.ExExact) + t(&ix.ExWild) + int64(cap(ix.Members)*8)
	for _, rules := range [][]ComplexRule{ix.Complex, ix.ExComplex} {
		for _, r := range rules {
			n += int64(len(r.Pattern)) + 24
		}
	}
	for _, r := range ix.Qualified {
		n += int64(144 + len(r.requiredLiteral) + len(r.Stored) + len(r.Rule.Text) + len(r.Rule.Pattern) + 2*cap(r.Rule.DNSTypes) + 16*cap(r.Rule.DenyAllow))
		for _, domain := range r.Rule.DenyAllow {
			n += int64(len(domain))
		}
		// Regex internals are intentionally opaque; include an estimate for
		// the compiled program as well as the retained source.
		if r.compiled != nil {
			n += int64(256 + 32*len(r.compiled.String()))
		}
	}
	return n
}

// Packed member IDs are nonnegative int32 values. Reject corrupt internal state
// rather than silently wrapping a packed ID or an overflowing lookup result.
func memberID(packed uint64) int32 {
	id := packed & math.MaxUint32
	if id > math.MaxInt32 {
		panic("policy index member ID exceeds int32")
	}
	return int32(id)
}

// RuleIn returns the rule of the list in slot that Matches the probe (exact,
// parent, wildcard, then complex; reverse-ARPA is not considered), or "".
func (ix *Index) RuleIn(p *Probe, slot int) string {
	if m, ok := ix.matchSlot(p, slot, false); ok {
		return m.Rule()
	}
	return ""
}

// ComplexCount is the number of rules outside compact domain tables in slot.
func (ix *Index) ComplexCount(slot int) int {
	n := 0
	for _, r := range ix.Qualified {
		if r.Slot == slot && !r.Rule.Exception {
			n++
		}
	}
	for i := range ix.Complex {
		if ix.Complex[i].Slot == slot {
			n++
		}
	}
	return n
}

// RuleSpread is one combination of lists and how many distinct rules exactly
// that combination holds.
type RuleSpread struct {
	Positions []int // indices into the slots passed to spreadOf, ascending
	Rules     int
}

// SpreadOf groups every distinct rule held by any of slots by the subset of
// slots holding it. Overlap and unique-domain analyses are sums over the
// result, so no list is ever materialized.
func (ix *Index) SpreadOf(slots []int) []RuleSpread {
	perSet := make([]int, len(ix.Members)/ix.Words)
	for _, table := range [...]*DomainTable{&ix.Exact, &ix.Wild} {
		for si := range table.Shards {
			t := &table.Shards[si]
			for i, tag := range t.Tags {
				if tag != 0 {
					perSet[memberID(t.Slots[i])]++
				}
			}
		}
	}

	combos := map[string]*RuleSpread{}
	key := make([]byte, (len(slots)+7)/8)
	add := func(n int, holds func(slot int) bool) {
		clear(key)
		var positions []int
		for pos, slot := range slots {
			if holds(slot) {
				key[pos>>3] |= 1 << (pos & 7)
				positions = append(positions, pos)
			}
		}
		if len(positions) == 0 {
			return
		}
		if c := combos[string(key)]; c != nil {
			c.Rules += n
			return
		}
		combos[string(key)] = &RuleSpread{Positions: positions, Rules: n}
	}
	for id, n := range perSet {
		if n > 0 {
			add(n, ListSet(ix.SetOf(int(id))).Has)
		}
	}
	patterns := map[string]ListSet{}
	for i := range ix.Complex {
		r := &ix.Complex[i]
		if patterns[r.Pattern] == nil {
			patterns[r.Pattern] = make(ListSet, ix.Words)
		}
		patterns[r.Pattern][r.Slot>>6] |= 1 << (uint(r.Slot) & 63)
	}
	for _, r := range ix.Qualified {
		if r.Rule.Exception {
			continue
		}
		if patterns[r.Stored] == nil {
			patterns[r.Stored] = make(ListSet, ix.Words)
		}
		patterns[r.Stored][r.Slot>>6] |= 1 << (uint(r.Slot) & 63)
	}
	for _, set := range patterns {
		add(1, set.Has)
	}
	out := make([]RuleSpread, 0, len(combos))
	for _, c := range combos {
		out = append(out, *c)
	}
	return out
}
