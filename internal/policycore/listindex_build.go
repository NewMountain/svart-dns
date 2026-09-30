package policycore

import (
	"errors"
	"fmt"
	"hash/maphash"
	"math"
	"slices"
	"strings"

	"github.com/yeti/svart-dns/internal/listparse"
)

// IndexBuilder accumulates rules for a fixed set of lists and produces an
// immutable Index. Rules are streamed in (one Add per rule), so building
// never holds a second per-list copy of the data.
type IndexBuilder struct {
	Ix     *Index
	Counts []int
	// setIDs interns list sets: the key is the set's words as raw bytes.
	SetIDs map[string]int32
	// grow memoizes "set id + slot -> set id" so adding a list touches the
	// intern map once per distinct set, not once per rule.
	Grow          map[GrowKey]int32
	Err           error
	sharedShards  map[*DomainShard]bool
	qualifiedSeen map[qualifiedKey]struct{}
}

// GrowKey identifies a memoized member-set extension by one list slot.
type GrowKey struct {
	ID   int32
	Slot int32
}

// ErrTooManyLists reports that enabled lists exceed the probe capacity.
var ErrTooManyLists = errors.New("too many enabled lists")

// NewIndexBuilder prepares a builder for lists; Add refers to lists by their
// position in this slice.
func NewIndexBuilder(lists []List) (*IndexBuilder, error) {
	if len(lists) > MaxIndexLists {
		return nil, fmt.Errorf("%w: %d enabled blocklists and allowlists, the list index holds at most %d; disable or delete lists (each client, group or policy with custom rules has its own manual list)",
			ErrTooManyLists, len(lists), MaxIndexLists)
	}
	words := max(1, (len(lists)+63)/64)
	ix := &Index{
		Seed:  maphash.MakeSeed(),
		Words: words,
		Lists: slices.Clone(lists),
	}
	ix.Exact.Init(1024)
	ix.Wild.Init(64)
	ix.ExExact.Init(16)
	ix.ExWild.Init(16)
	b := &IndexBuilder{
		Ix:     ix,
		Counts: make([]int, len(lists)),
		SetIDs: map[string]int32{},
		Grow:   map[GrowKey]int32{},
	}
	return b, nil
}

// Init reserves a power-of-two table, retaining any existing arena.
func (t *DomainShard) Init(capacity int) {
	if capacity < 1 {
		capacity = 1
	}
	n := uint64(1)
	for n < uint64(capacity) {
		if n > math.MaxInt/2 {
			panic("policy index table capacity exceeds addressable memory")
		}
		n <<= 1
	}
	t.Slots = make([]uint64, n)
	t.Tags = make([]uint8, n)
	t.Mask = n - 1
	if t.Arena == nil {
		t.Arena = []byte{0}
	}
}

// Add records that the list at position slot contains rule. Rules must already
// be normalized (lowercase, no trailing dot). Duplicates are ignored.
func (b *IndexBuilder) Add(slot int, rule string) {
	if b.Err != nil {
		return
	}
	if strings.HasPrefix(rule, "!svart-rule-") {
		b.addQualified(slot, rule)
		return
	}
	if strings.HasPrefix(rule, listparse.ExceptionPrefix) {
		b.AddException(slot, rule[len(listparse.ExceptionPrefix):])
		return
	}
	bucket, key := ClassifyDomain(rule)
	if bucket == "complex" {
		for _, r := range b.Ix.Complex {
			if r.Slot == slot && r.Pattern == key {
				return
			}
		}
		b.Ix.Complex = append(b.Ix.Complex, NewComplexRule(key, slot))
		b.Counts[slot]++
		return
	}
	if key == "" || len(key) > 255 {
		return
	}
	table := &b.Ix.Exact
	if bucket == "suffix" {
		table = &b.Ix.Wild
	}
	if b.Insert(table, key, slot) {
		b.Counts[slot]++
	}
}

// AddException records that the list at slot excepts rule from its own rules.
// Exceptions do not count toward the list's size (attribution order).
func (b *IndexBuilder) AddException(slot int, rule string) {
	bucket, key := ClassifyDomain(rule)
	switch {
	case key == "" || len(key) > 255:
	case bucket == "complex":
		for _, r := range b.Ix.ExComplex {
			if r.Slot == slot && r.Pattern == key {
				return
			}
		}
		b.Ix.ExComplex = append(b.Ix.ExComplex, NewComplexRule(key, slot))
	case bucket == "suffix":
		b.Insert(&b.Ix.ExWild, key, slot)
	default:
		b.Insert(&b.Ix.ExExact, key, slot)
	}
}

// Insert adds slot to key's list set, returning false if it was already there.
func (b *IndexBuilder) Insert(table *DomainTable, key string, slot int) bool {
	hash := maphash.String(b.Ix.Seed, key)
	shard := &table.Shards[domainShardFor(hash)]
	if b.sharedShards[shard] {
		shard.Slots = slices.Clone(shard.Slots)
		shard.Tags = slices.Clone(shard.Tags)
		shard.Arena = slices.Clip(shard.Arena)
		delete(b.sharedShards, shard)
	}
	before := shard.Count
	added := b.insertShard(shard, key, slot, hash)
	table.Count += shard.Count - before
	return added
}

func (b *IndexBuilder) insertShard(t *DomainShard, key string, slot int, h uint64) bool {
	if len(key) == 0 || len(key) > math.MaxUint8 {
		return false
	}
	if (t.Count+1)*10 > len(t.Slots)*7 {
		b.Rehash(t)
	}
	tag := TagOf(h)
	for i := h & t.Mask; ; i = (i + 1) & t.Mask {
		switch t.Tags[i] {
		case 0:
			off := len(t.Arena)
			if off+1+len(key) > 1<<32-1 {
				b.Err = errors.New("list index arena exceeds 4 GiB")
				return false
			}
			id := b.WithSlot(-1, slot)
			if id < 0 {
				return false
			}
			t.Arena = append(t.Arena, keyLength(len(key)))
			t.Arena = append(t.Arena, key...)
			t.Tags[i] = tag
			t.Slots[i] = uint64(off)<<32 | uint64(id)
			t.Count++
			return true
		case tag:
			v := t.Slots[i]
			if !t.keyEquals(uint32(v>>32), key) {
				continue
			}
			id := memberID(v)
			if ListSet(b.Ix.SetOf(int(id))).Has(slot) {
				return false
			}
			nextID := b.WithSlot(id, slot)
			if nextID < 0 {
				return false
			}
			t.Slots[i] = v&^0xffffffff | uint64(nextID)
			return true
		}
	}
}

func keyLength(length int) byte {
	if length < 1 || length > math.MaxUint8 {
		panic("policy index key exceeds length-prefix capacity")
	}
	return byte(length)
}

// Rehash doubles a shard table and preserves every existing rule and member set.
func (b *IndexBuilder) Rehash(t *DomainShard) {
	old := *t
	t.Init(len(old.Slots) * 2)
	t.Arena = old.Arena
	t.Count = old.Count
	for i, tag := range old.Tags {
		if tag == 0 {
			continue
		}
		v := old.Slots[i]
		off := uint32(v >> 32)
		length := uint32(t.Arena[off])
		h := maphash.Bytes(b.Ix.Seed, t.Arena[off+1:off+1+length])
		j := h & t.Mask
		for t.Tags[j] != 0 {
			j = (j + 1) & t.Mask
		}
		t.Tags[j] = TagOf(h)
		t.Slots[j] = v
	}
}

// WithSlot returns the id of set(id) ∪ {slot}; id -1 means the empty set.
func (b *IndexBuilder) WithSlot(id int32, slot int) int32 {
	if slot < 0 || slot >= MaxIndexLists {
		panic("policy index slot exceeds probe capacity")
	}
	gk := GrowKey{ID: id, Slot: int32(slot)}
	if got, ok := b.Grow[gk]; ok {
		return got
	}
	set := make([]uint64, b.Ix.Words)
	if id >= 0 {
		copy(set, b.Ix.SetOf(int(id)))
	}
	set[slot>>6] |= 1 << (uint(slot) & 63)
	nid := b.Intern(set)
	b.Grow[gk] = nid
	return nid
}

// Intern returns the stable ID of an immutable member set.
func (b *IndexBuilder) Intern(set []uint64) int32 {
	key := stringWords(set)
	if id, ok := b.SetIDs[key]; ok {
		return id
	}
	count := len(b.Ix.Members) / b.Ix.Words
	if count < 0 || count > math.MaxInt32 {
		b.Err = errors.New("policy index has too many distinct member sets")
		return -1
	}
	id := int32(count)
	b.Ix.Members = append(b.Ix.Members, set...)
	b.SetIDs[key] = id
	return id
}

// Build finalizes the index: slots are renumbered so that lower slots hold
// smaller lists (attribution order), and builder-only state is dropped.
func (b *IndexBuilder) Build() (*Index, error) {
	if b.Err != nil {
		return nil, b.Err
	}
	ix := b.Ix
	n := len(ix.Lists)
	order := make([]int, n) // order[newSlot] = oldSlot
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, c int) int {
		if b.Counts[a] != b.Counts[c] {
			return b.Counts[a] - b.Counts[c]
		}
		return ix.Lists[a].ID - ix.Lists[c].ID
	})
	newOf := make([]int, n)
	lists := make([]List, n)
	for ns, os := range order {
		newOf[os] = ns
		lists[ns] = ix.Lists[os]
		lists[ns].Count = b.Counts[os]
	}
	ix.Lists = lists
	w := ix.Words
	remapped := make([]uint64, w)
	for off := 0; off < len(ix.Members); off += w {
		clear(remapped)
		for os := 0; os < n; os++ {
			if ListSet(ix.Members[off : off+w]).Has(os) {
				ns := newOf[os]
				remapped[ns>>6] |= 1 << (uint(ns) & 63)
			}
		}
		copy(ix.Members[off:off+w], remapped)
	}
	for i := range ix.Complex {
		ix.Complex[i].Slot = newOf[ix.Complex[i].Slot]
	}
	for i := range ix.ExComplex {
		ix.ExComplex[i].Slot = newOf[ix.ExComplex[i].Slot]
	}
	for i := range ix.Qualified {
		ix.Qualified[i].Slot = newOf[ix.Qualified[i].Slot]
	}
	// Copy the append-grown arrays to their exact size: append leaves up to
	// 25% unused capacity behind the arena, which would otherwise stay
	// allocated for the life of the index.
	for _, table := range [...]*DomainTable{&ix.Exact, &ix.Wild, &ix.ExExact, &ix.ExWild} {
		for i := range table.Shards {
			shard := &table.Shards[i]
			if !b.sharedShards[shard] {
				shard.Arena = slices.Clone(shard.Arena)
			}
		}
	}

	ix.Members = slices.Clone(ix.Members)
	b.Ix = nil
	return ix, nil
}

// BuildIndex builds the index for lists, taking their rules from feed,
// which calls add once per stored rule with the rule's list position.
func BuildIndex(lists []List, feed func(add func(slot int, rule string)) error) (*Index, error) {
	b, err := NewIndexBuilder(lists)
	if err != nil {
		return nil, err
	}
	if err := feed(b.Add); err != nil {
		return nil, err
	}
	return b.Build()
}
