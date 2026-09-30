package policycore

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestImmutableUpdatesMatchFullRebuild(t *testing.T) {
	lists := []List{{ID: 10, Name: "Ads"}, {ID: 20, Name: "Tracking"}, {ID: 10, Name: "Allowed", Allow: true}}
	rules := [][]string{{"ads.example", "@@safe.ads.example", "*.tracking.example", "ad*.complex.example"}, {"ads.example", "other.example"}, {"safe.ads.example"}}
	feed := func(add func(int, string)) error {
		for slot, entries := range rules {
			for _, rule := range entries {
				add(slot, rule)
			}
		}
		return nil
	}
	previous, err := BuildIndex(lists, feed)
	if err != nil {
		t.Fatal(err)
	}
	corpus := []string{"ads.example", "safe.ads.example", "cdn.ads.example", "a.tracking.example", "tracking.example", "ad42.complex.example", "other.example", "new.example", "nothing.example"}
	for step := 0; step < 5; step++ {
		oldLists := append([]List(nil), previous.Lists...)
		oldRule := previous.Contains(previous.SlotOf(10, false), "ads.example")
		var changed []ListKey
		switch step {
		case 0:
			rules[0] = []string{"new.example", "@@safe.new.example"}
			changed = []ListKey{{10, false}}
		case 1:
			rules[0] = []string{"ads.example", "@@ads.example", "*.tracking.example", "ad*.complex.example", "extra.example"}
			changed = []ListKey{{10, false}}
		case 2:
			lists = append(lists, List{ID: 1, Name: "Small manual", Manual: true})
			rules = append(rules, []string{"ads.example"})
			changed = []ListKey{{1, false}}
		case 3:
			lists = append(lists[:1], lists[2:]...)
			rules = append(rules[:1], rules[2:]...)
			changed = []ListKey{{20, false}}
		case 4:
			rules[1] = []string{"ads.example", "a.tracking.example"}
			changed = []ListKey{{10, true}}
		}
		incremental, err := previous.UpdateIndex(lists, changed, func(add func(int, string)) error {
			for slot, list := range lists {
				for _, key := range changed {
					if list.ID == key.ID && list.Allow == key.Allow {
						for _, r := range rules[slot] {
							add(slot, r)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		full, err := BuildIndex(lists, feed)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(incremental.Lists, full.Lists) {
			t.Fatalf("step%d attribution order=%v want%v", step, incremental.Lists, full.Lists)
		}
		for _, domain := range corpus {
			var a, b Probe
			incremental.ProbeDomain(&a, domain, "", 1)
			full.ProbeDomain(&b, domain, "", 1)
			for _, allow := range []bool{false, true} {
				ids := []int{1, 10, 20}
				got, gok := incremental.FirstMatch(&a, incremental.SetFor(ids, allow), !allow)
				want, wok := full.FirstMatch(&b, full.SetFor(ids, allow), !allow)
				if gok != wok || gok && (got.Rule() != want.Rule() || incremental.Lists[got.Slot] != full.Lists[want.Slot]) {
					t.Fatalf("step%d domain%s allow%t match=%+v,%t want%+v,%t", step, domain, allow, got, gok, want, wok)
				}
			}
		}
		if !reflect.DeepEqual(previous.Lists, oldLists) || previous.Contains(previous.SlotOf(10, false), "ads.example") != oldRule {
			t.Fatal("update changed the old published index")
		}
		previous = incremental
	}
}

func TestImmutableUpdateAcrossWordBoundaryAndFailure(t *testing.T) {
	lists := make([]List, 64)
	for i := range lists {
		lists[i] = List{ID: i + 1, Name: fmt.Sprintf("List%d", i)}
	}
	original, err := BuildIndex(lists, func(add func(int, string)) error {
		for i := range lists {
			add(i, "ads.example")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lists = append(lists, List{ID: 65, Name: "New allow", Allow: true})
	updated, err := original.UpdateIndex(lists, []ListKey{{65, true}}, func(add func(int, string)) error { add(64, "safe.example"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if updated.Words != 2 || original.Words != 1 {
		t.Fatalf("word counts old=%d new=%d", original.Words, updated.Words)
	}
	for i := 1; i <= 64; i++ {
		if !updated.Contains(updated.SlotOf(i, false), "ads.example") {
			t.Fatalf("lost list%d", i)
		}
	}
	if !updated.Contains(updated.SlotOf(65, true), "safe.example") {
		t.Fatal("new allowlist missing")
	}
	failure := errors.New("rule source unavailable")
	failed, err := updated.UpdateIndex(lists, []ListKey{{65, true}}, func(add func(int, string)) error { add(64, "other.example"); return failure })
	if failed != nil || !errors.Is(err, failure) {
		t.Fatalf("failed update=%v,%v", failed, err)
	}
	if !updated.Contains(updated.SlotOf(65, true), "safe.example") || updated.Contains(updated.SlotOf(65, true), "other.example") {
		t.Fatal("failed update mutated prior index")
	}
}

func TestImmutableUpdateSharesUnaffectedPartitions(t *testing.T) {
	lists := []List{{ID: 1, Name: "Published"}, {ID: 2, Name: "Manual", Manual: true}}
	old, err := BuildIndex(lists, func(add func(int, string)) error {
		for i := 0; i < 10000; i++ {
			add(0, fmt.Sprintf("ads%d.example", i))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := old.UpdateIndex(lists, []ListKey{{2, false}}, func(add func(int, string)) error { add(1, "manual.example"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	shared := 0
	for i := range old.Exact.Shards {
		a, b := &old.Exact.Shards[i], &updated.Exact.Shards[i]
		if &a.Slots[0] == &b.Slots[0] {
			shared++
		}
	}
	if shared != domainShardCount-1 {
		t.Fatalf("shared partitions=%d, want%d", shared, domainShardCount-1)
	}
	if old.Contains(old.SlotOf(2, false), "manual.example") {
		t.Fatal("new rule visible in old snapshot")
	}
}
