package policycore

import (
	"fmt"
	"hash/maphash"
	"net/netip"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/yeti/svart-dns/internal/listparse"
)

// QualifiedRule retains restrictions and original attribution without expanding
// ordinary domain entries into GC-scanned objects. Built rules are immutable.
type QualifiedRule struct {
	Rule            listparse.Rule
	Stored          string
	Slot            int
	compiled        *regexp.Regexp
	requiredLiteral string
}

type qualifiedKey struct {
	slot int
	row  string
}

func (b *IndexBuilder) addQualified(slot int, row string) {
	if b.qualifiedSeen == nil {
		b.qualifiedSeen = make(map[qualifiedKey]struct{}, len(b.Ix.Qualified))
		for _, r := range b.Ix.Qualified {
			b.qualifiedSeen[qualifiedKey{r.Slot, r.Stored}] = struct{}{}
		}
	}
	key := qualifiedKey{slot, row}
	if _, exists := b.qualifiedSeen[key]; exists {
		return
	}
	rule, err := listparse.DecodeStored(row)
	if err != nil {
		b.Err = fmt.Errorf("list %d: invalid stored rule: %w", b.Ix.Lists[slot].ID, err)
		return
	}
	var compiled *regexp.Regexp
	var literal string
	if rule.Kind == listparse.KindMask || rule.Kind == listparse.KindRegex {
		compiled, err = listparse.CompilePattern(rule)
		if err != nil {
			b.Err = fmt.Errorf("list %d: compile rule %q: %w", b.Ix.Lists[slot].ID, rule.Text, err)
			return
		}
	}
	if compiled != nil {
		tree, err := syntax.Parse(compiled.String(), syntax.Perl)
		if err != nil {
			b.Err = fmt.Errorf("list %d: inspect compiled rule: %w", b.Ix.Lists[slot].ID, err)
			return
		}
		literal = necessaryLiteral(tree)
	}
	b.qualifiedSeen[key] = struct{}{}
	b.Ix.Qualified = append(b.Ix.Qualified, QualifiedRule{Rule: rule, Stored: strings.Clone(row), Slot: slot, compiled: compiled, requiredLiteral: literal})
	if !rule.Exception {
		b.Counts[slot]++
	}
}

func beneath(name, base string) bool {
	return name == base || len(name) > len(base) && strings.HasSuffix(name, base) && name[len(name)-len(base)-1] == '.'
}

func (r *QualifiedRule) matches(name string, rrtype uint16, literalSafe bool) bool {
	if len(r.Rule.DNSTypes) > 0 && slices.Contains(r.Rule.DNSTypes, rrtype) == r.Rule.ExcludeTypes {
		return false
	}
	if literalSafe && r.requiredLiteral != "" && !strings.Contains(name, r.requiredLiteral) {
		return false
	}
	matched := false
	switch r.Rule.Kind {
	case listparse.KindSubtree:
		matched = beneath(name, r.Rule.Pattern)
	case listparse.KindExact, listparse.KindAddress:
		matched = name == r.Rule.Pattern
	default:
		matched = r.compiled.MatchString(name)
	}
	if !matched {
		return false
	}
	for _, excluded := range r.Rule.DenyAllow {
		if beneath(name, excluded) {
			return false
		}
	}
	return true
}

// necessaryLiteral chooses one literal that every matching path must contain.
// Optional and alternative branches contribute nothing: rejecting based on one
// such branch would silently remove valid matches. This is only a prefilter;
// the authoritative compiled regexp still makes every positive decision.
func necessaryLiteral(re *syntax.Regexp) string {
	switch re.Op {
	case syntax.OpLiteral:
		for _, r := range re.Rune {
			if r > 127 {
				return ""
			}
		}
		return strings.ToLower(string(re.Rune))
	case syntax.OpCapture, syntax.OpPlus:
		return necessaryLiteral(re.Sub[0])
	case syntax.OpRepeat:
		if re.Min > 0 {
			return necessaryLiteral(re.Sub[0])
		}
	case syntax.OpConcat:
		best := ""
		for _, child := range re.Sub {
			if s := necessaryLiteral(child); len(s) > len(best) {
				best = s
			}
		}
		return best
	}
	return ""
}

func lowerASCII(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] >= 128 || name[i] >= 'A' && name[i] <= 'Z' {
			return false
		}
	}
	return true
}

// A nonnegative index addresses Qualified; negative indices address Complex.
// Caching the successful candidate once prevents matching regexes per entity.
type candidateHit struct {
	index int
	kind  MatchKind
}

func (p *Probe) addCandidate(hit candidateHit) {
	if p.candidateSpill != nil {
		p.candidateSpill = append(p.candidateSpill, hit)
		return
	}
	if p.candidateN < len(p.candidateInline) {
		p.candidateInline[p.candidateN] = hit
		p.candidateN++
		return
	}
	p.candidateSpill = append(append(make([]candidateHit, 0, 2*len(p.candidateInline)), p.candidateInline[:]...), hit)
}
func (p *Probe) candidates() []candidateHit {
	if p.candidateSpill != nil {
		return p.candidateSpill
	}
	return p.candidateInline[:p.candidateN]
}
func (p *Probe) reset(ix *Index, domain, arpaIP string, rrtype uint16) {
	p.Domain, p.ArpaIP, p.RecordType = domain, arpaIP, rrtype
	p.N, p.Spill = 0, nil
	p.arpaException = -1
	p.candidateN, p.candidateSpill = 0, nil
	clear(p.Any[:ix.Words])
	clear(p.Complex[:ix.Words])
	clear(p.exempt[:ix.Words])
}

func (ix *Index) probeQualified(p *Probe, addressOnly bool) {
	// Non-ASCII and uppercase inputs use the full regexp, preserving Unicode
	// case folding even outside ProbeDomain's normalized DNS-name contract.
	literalSafe := lowerASCII(p.Domain)
	for i := range ix.Qualified {
		r := &ix.Qualified[i]
		if addressOnly && r.Rule.Kind != listparse.KindAddress {
			continue
		}
		name, kind := p.Domain, KindComplex
		if r.Rule.Kind == listparse.KindAddress && p.ArpaIP != "" {
			name, kind = p.ArpaIP, KindARPA
		}
		if !r.matches(name, p.RecordType, literalSafe) {
			continue
		}
		p.addCandidate(candidateHit{index: i, kind: kind})
		if !r.Rule.Exception {
			p.Complex[r.Slot>>6] |= 1 << (uint(r.Slot) & 63)
		}
	}
}

func (ix *Index) probeExceptions(p *Probe, addressOnly bool) {
	if p.ArpaIP != "" {
		p.arpaException = ix.ExExact.Lookup(maphash.String(ix.Seed, p.ArpaIP), p.ArpaIP)
	}
	if ix.ExExact.Count == 0 && ix.ExWild.Count == 0 && len(ix.ExComplex) == 0 {
		return
	}
	add := func(id int32) {
		if id >= 0 {
			for w, set := range ix.SetOf(int(id)) {
				p.exempt[w] |= set
			}
		}
	}
	for off := 0; ; {
		name := p.Domain[off:]
		add(ix.ExExact.Lookup(maphash.String(ix.Seed, name), name))
		if off > 0 {
			add(ix.ExWild.Lookup(maphash.String(ix.Seed, name), name))
		}
		dot := strings.IndexByte(name, '.')
		if addressOnly || dot < 0 {
			break
		}
		off += dot + 1
	}
	if addressOnly {
		return
	}
	for i := range ix.ExComplex {
		r := &ix.ExComplex[i]
		if r.Matches(p.Domain) {
			p.exempt[r.Slot>>6] |= 1 << (uint(r.Slot) & 63)
		}
	}
}

// matchSlot applies priority only inside this list. Its result remains subject
// to Assignment precedence and explicit allow lists in EntityResult.
func (ix *Index) matchSlot(p *Probe, slot int, checkARPA bool) (IndexMatch, bool) {
	match, ok := ix.MatchInSlot(p, slot, checkARPA)
	rank := 0
	if ok {
		rank = 1
	}
	if ListSet(p.exempt[:ix.Words]).Has(slot) || checkARPA && p.arpaException >= 0 && ListSet(ix.SetOf(int(p.arpaException))).Has(slot) {
		rank = 2
		ok = false
	}
	for _, hit := range p.candidates() {
		if hit.kind == KindARPA && !checkARPA {
			continue
		}
		if hit.index < 0 {
			rule := &ix.Complex[-hit.index-1]
			if rule.Slot == slot && rank < 1 {
				match = IndexMatch{Slot: slot, Kind: KindComplex, Text: rule.Pattern}
				rank = 1
				ok = true
			}
			continue
		}
		r := &ix.Qualified[hit.index]
		if r.Slot != slot {
			continue
		}
		priority := 1
		if r.Rule.Important {
			priority += 2
		}
		if r.Rule.Exception {
			priority++
		}
		if priority <= rank {
			continue
		}
		rank = priority
		ok = !r.Rule.Exception
		if ok {
			match = IndexMatch{Slot: slot, Kind: hit.kind, Text: r.Rule.Text}
		}
	}
	return match, ok
}

// probeAddress never applies domain masks or parent suffixes to response IPs.
func (ix *Index) probeAddress(p *Probe, addr netip.Addr) {
	addr = addr.Unmap()
	rrtype := uint16(28)
	if addr.Is4() {
		rrtype = 1
	}
	name := addr.String()
	p.reset(ix, name, "", rrtype)
	if id := ix.LookupExact(name); id >= 0 {
		p.Add(ix, ProbeHit{Kind: KindExact, ID: id})
	}
	ix.probeExceptions(p, true)
	ix.probeQualified(p, true)
}
