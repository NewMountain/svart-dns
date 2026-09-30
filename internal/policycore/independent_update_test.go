package policycore

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// Repeated churn combines identity collisions, exceptions, slot reordering and
// both directions across 64 bits; every incremental result has a full oracle.
func TestIndependentIncrementalChurnMatchesFullOracle(t *testing.T) {
	// Preserve every original seeded selection and swap as a stable regression
	// corpus. Its generator and seed are recorded alongside the fixture.
	var steps []struct {
		Changes [][2]int
		Swaps   [][2]int
	}
	fixture, err := os.ReadFile("testdata/incremental-churn.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &steps); err != nil {
		t.Fatal(err)
	}
	if len(steps) != 100 {
		t.Fatalf("got %d churn steps, want 100", len(steps))
	}
	lists := []List{}
	rules := map[ListKey][]string{}
	corpus := []string{"ads.example", "safe.ads.example", "cdn.ads.example", "x.track.example", "track.example", "ad42.complex.example", "safe42.complex.example", "unrelated.example"}
	variants := [][]string{{"ads.example", "@@safe.ads.example"}, {"*.track.example"}, {"ad*.complex.example", "@@safe*.complex.example"}, {"ads.example", "*.track.example"}, {"safe.ads.example"}, {}}
	for i := 0; i < 65; i++ {
		list := List{ID: i/2 + 1, Allow: i%2 == 1, Name: fmt.Sprintf("independent-%d", i), Manual: i%3 == 0}
		lists = append(lists, list)
		rules[ListKey{list.ID, list.Allow}] = variants[i%len(variants)]
	}
	feed := func(selected map[ListKey]bool) func(func(int, string)) error {
		return func(add func(int, string)) error {
			for slot, list := range lists {
				key := ListKey{list.ID, list.Allow}
				if selected != nil && !selected[key] {
					continue
				}
				for _, rule := range rules[key] {
					add(slot, rule)
				}
			}
			return nil
		}
	}
	previous, err := BuildIndex(lists, feed(nil))
	if err != nil {
		t.Fatal(err)
	}
	signature := func(ix *Index) []string {
		out := []string{}
		for _, domain := range corpus {
			var probe Probe
			ix.ProbeDomain(&probe, domain, "", 1)
			for _, list := range ix.Lists {
				match, found := ix.FirstMatch(&probe, ix.SetFor([]int{list.ID}, list.Allow), !list.Allow)
				value := "none"
				if found {
					value = fmt.Sprintf("%d/%t:%s", ix.Lists[match.Slot].ID, ix.Lists[match.Slot].Allow, match.Rule())
				}
				out = append(out, fmt.Sprintf("%s:%d/%t=%s", domain, list.ID, list.Allow, value))
			}
		}
		return out
	}
	for step := 0; step < 100; step++ {
		if len(steps[step].Changes) != 3 || len(steps[step].Swaps) != 63+step%2 {
			t.Fatalf("step %d has incomplete selections or swaps", step)
		}
		oldSignature := signature(previous)
		changed := map[ListKey]bool{}
		if step%2 == 0 {
			removed := lists[len(lists)-1]
			lists = lists[:len(lists)-1]
			changed[ListKey{removed.ID, removed.Allow}] = true
		} else {
			added := List{ID: 1000 + step, Allow: step%4 == 1, Name: fmt.Sprintf("new-%d", step)}
			lists = append(lists, added)
			key := ListKey{added.ID, added.Allow}
			rules[key] = variants[step%len(variants)]
			changed[key] = true
		}
		for _, change := range steps[step].Changes {
			list := lists[change[0]]
			key := ListKey{list.ID, list.Allow}
			rules[key] = variants[change[1]]
			changed[key] = true
		}
		for _, swap := range steps[step].Swaps {
			lists[swap[0]], lists[swap[1]] = lists[swap[1]], lists[swap[0]]
		}
		keys := []ListKey{}
		for key := range changed {
			keys = append(keys, key)
		}
		updated, err := previous.UpdateIndex(lists, keys, feed(changed))
		if err != nil {
			t.Fatal(err)
		}
		full, err := BuildIndex(lists, feed(nil))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(updated.Lists, full.Lists) || !reflect.DeepEqual(signature(updated), signature(full)) {
			t.Fatalf("step %d differs from full oracle", step)
		}
		previous.ApproxBytes()
		updated.ApproxBytes()
		if !reflect.DeepEqual(oldSignature, signature(previous)) {
			t.Fatalf("step %d changed old snapshot", step)
		}
		previous = updated
	}
}
