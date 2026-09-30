package policycore

import (
	"encoding/binary"
	"slices"
)

// Shards keep an immutable update proportional to the affected hash partitions.
// A lookup selects its partition from high hash bits, then uses the unchanged
// open-addressed table. Query matching still allocates no memory.
const domainShardCount = 64

// DomainTable partitions rule storage into independently reusable hash shards.
type DomainTable struct {
	Shards [domainShardCount]DomainShard
	Count  int
}

func domainShardFor(hash uint64) int { return int(hash >> 58) }

// Init distributes initial capacity across all shards.
func (t *DomainTable) Init(capacity int) {
	for i := range t.Shards {
		t.Shards[i].Init(max(1, (capacity+domainShardCount-1)/domainShardCount))
	}
}

// Lookup finds the member-set ID of a key, or returns -1.
func (t *DomainTable) Lookup(hash uint64, key string) int32 {
	return t.Shards[domainShardFor(hash)].Lookup(hash, key)
}

// ListKey is node-local list identity; block and allow row IDs are independent.
type ListKey struct {
	ID    int
	Allow bool
}

// UpdateIndex replaces only the supplied lists' rules and reuses immutable
// partitions for other rules. New and deleted IDs are reconciled against lists;
// slots are remapped by identity before size-based attribution is recomputed.
func (previous *Index) UpdateIndex(lists []List, changed []ListKey, feed func(add func(int, string)) error) (*Index, error) {
	builder, err := NewIndexBuilder(lists)
	if err != nil {
		return nil, err
	}
	next := *previous
	next.Lists = slices.Clone(lists)
	next.Words = max(1, (len(lists)+63)/64)
	builder.Ix = &next
	refresh := make(map[ListKey]bool, len(changed))
	for _, key := range changed {
		refresh[key] = true
	}
	positions := make(map[ListKey]int, len(lists))
	for slot, list := range lists {
		positions[ListKey{list.ID, list.Allow}] = slot
	}
	remap := make([]int, len(previous.Lists))
	for old, list := range previous.Lists {
		key := ListKey{list.ID, list.Allow}
		slot, found := positions[key]
		remap[old] = -1
		if found && !refresh[key] {
			remap[old] = slot
			builder.Counts[slot] = list.Count
		}
	}
	countSets := len(previous.Members) / previous.Words
	next.Members = make([]uint64, countSets*next.Words)
	for id := 0; id < countSets; id++ {
		set := next.Members[id*next.Words : (id+1)*next.Words]
		oldSet := ListSet(previous.SetOf(id))
		for old, slot := range remap {
			if slot >= 0 && oldSet.Has(old) {
				set[slot>>6] |= 1 << (uint(slot) & 63)
			}
		}
		builder.SetIDs[stringWords(set)] = int32(id)
	}
	copyComplex := func(input []ComplexRule) []ComplexRule {
		out := make([]ComplexRule, 0, len(input))
		for _, rule := range input {
			if slot := remap[rule.Slot]; slot >= 0 {
				rule.Slot = slot
				out = append(out, rule)
			}
		}
		return out
	}
	next.Complex = copyComplex(previous.Complex)
	next.ExComplex = copyComplex(previous.ExComplex)
	next.Qualified = make([]QualifiedRule, 0, len(previous.Qualified))
	for _, rule := range previous.Qualified {
		if slot := remap[rule.Slot]; slot >= 0 {
			rule.Slot = slot
			next.Qualified = append(next.Qualified, rule)
		}
	}
	builder.sharedShards = make(map[*DomainShard]bool, 4*domainShardCount)
	for _, table := range [...]*DomainTable{&next.Exact, &next.Wild, &next.ExExact, &next.ExWild} {
		for i := range table.Shards {
			builder.sharedShards[&table.Shards[i]] = true
		}
	}
	if err := feed(builder.Add); err != nil {
		return nil, err
	}
	return builder.Build()
}

func stringWords(words []uint64) string {
	encoded := make([]byte, len(words)*8)
	for i, word := range words {
		binary.LittleEndian.PutUint64(encoded[i*8:], word)
	}
	return string(encoded)
}
