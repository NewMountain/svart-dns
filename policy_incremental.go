package main

import "github.com/yeti/svart-dns/internal/policycore"

// Small list changes reuse immutable partitions. Full downloads and unknown
// bulk mutations keep the full rebuild path. Periodic compaction bounds retained
// keys after deletions; no rule or history row is discarded by this choice.
func readIncrementalIndex(q queryer, previous *policycore.Index, lists []policycore.List, changed []policycore.ListKey) (*policycore.Index, bool, error) {
	const maxChangedRules = 50_000
	refresh := make(map[policycore.ListKey]bool, len(changed))
	for _, key := range changed {
		refresh[key] = true
	}
	var selected []policycore.List
	var slots []int
	var size, remaining int
	for slot, list := range lists {
		key := policycore.ListKey{ID: list.ID, Allow: list.Allow}
		old := previous.SlotOf(list.ID, list.Allow)
		if old < 0 {
			refresh[key] = true
		}
		if refresh[key] {
			selected = append(selected, list)
			slots = append(slots, slot)
			size += list.Count
			if old >= 0 {
				size += previous.Lists[old].Count
			}
		} else if old >= 0 {
			remaining += previous.Lists[old].Count
		}
	}
	if size > maxChangedRules {
		return nil, false, nil
	}
	physical := previous.Exact.Count + previous.Wild.Count + previous.ExExact.Count + previous.ExWild.Count
	if physical > 2*max(1024, remaining+size) || len(previous.Members)/previous.Words > max(1024, len(lists)*16) {
		return nil, false, nil
	}
	keys := make([]policycore.ListKey, 0, len(refresh))
	for key := range refresh {
		keys = append(keys, key)
	}
	index, err := previous.UpdateIndex(lists, keys, func(add func(int, string)) error {
		return listRuleFeed(q, selected)(func(slot int, rule string) { add(slots[slot], rule) })
	})
	return index, true, err
}
