package policycore

import (
	"errors"
	"net/netip"
	"reflect"
	"sync"
	"testing"
)

func TestIndependentSnapshotPreservesDecisionsAndAttribution(t *testing.T) {
	lists := []List{{ID: 3, Name: "Ads"}, {ID: 7, Name: "Household allow", Allow: true}, {ID: 1, Name: "Manual block", Manual: true}}
	rules := [][]string{{"ads.example", "@@safe.ads.example"}, {"safe.ads.example"}, {"custom.example"}}
	ix, err := BuildIndex(lists, func(add func(int, string)) error {
		for slot, entries := range rules {
			for _, r := range entries {
				add(slot, r)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	client := "192.0.2.10"
	cfg := Config{Lists: lists, Clients: map[string]*ListIDs{client: {Block: []int{3, 1}, Allow: []int{7}}}}
	snapshot := BuildSnapshot(cfg, ix)
	for _, test := range []struct {
		domain string
		entity EntityResult
	}{
		{"cdn.ads.example", EntityResult{Tier: "ip", Name: client, Result: "block", PublishedList: &PublishedHit{Action: "block", Rule: "ads.example", ListID: 3, ListName: "Ads"}}},
		{"safe.ads.example", EntityResult{Tier: "ip", Name: client, Result: "allow", PublishedList: &PublishedHit{Action: "allow", Rule: "safe.ads.example", ListID: 7, ListName: "Household allow"}}},
		{"custom.example", EntityResult{Tier: "ip", Name: client, Result: "block", CustomRule: &CustomHit{Action: "block", Rule: "custom.example"}}},
	} {
		got := snapshot.Evaluate(client, netip.MustParseAddr(client), test.domain, 1)
		want := &PolicyResult{ClientIP: client, Domain: test.domain, RecordType: 1, Result: test.entity.Result, ResultSource: &test.entity,
			RangeEvaluation: &TierEvaluation{Entities: []EntityResult{}}, GroupEvaluation: &TierEvaluation{Entities: []EntityResult{}},
			IPEvaluation: &TierEvaluation{Entities: []EntityResult{test.entity}, Result: test.entity.Result, ResultSource: &test.entity}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s decision=%+v, want %+v", test.domain, got, want)
		}
	}
	var probe Probe
	if allocs := testing.AllocsPerRun(100, func() {
		ix.ProbeDomain(&probe, "cdn.ads.example", "", 1)
		ix.FirstMatch(&probe, snapshot.Clients[client].Block, true)
	}); allocs != 0 {
		t.Fatalf("matching allocated %f times", allocs)
	}
	// Concurrent evaluations never modify the published snapshot or configuration.
	var wait sync.WaitGroup
	for i := 0; i < 8; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for n := 0; n < 100; n++ {
				snapshot.Evaluate(client, netip.MustParseAddr(client), "safe.ads.example", 1)
			}
		}()
	}
	wait.Wait()
	if !reflect.DeepEqual(cfg.Clients[client], &ListIDs{Block: []int{3, 1}, Allow: []int{7}}) {
		t.Fatal("snapshot construction changed caller configuration")
	}
}

func TestBuildIndexFailureReturnsNoPartialIndex(t *testing.T) {
	failure := errors.New("input unavailable")
	ix, err := BuildIndex([]List{{ID: 1, Name: "Ads"}}, func(add func(int, string)) error { add(0, "ads.example"); return failure })
	if ix != nil || !errors.Is(err, failure) {
		t.Fatalf("failed build returned index=%v error=%v", ix, err)
	}
}

func TestIndexFootprintIsReadOnly(t *testing.T) {
	backing := make([]ComplexRule, 4)
	backing[0].Pattern = "ad*.example"
	backing[1].Pattern = "unchanged backing storage"
	before := append([]ComplexRule(nil), backing...)
	ix := &Index{Complex: backing[:1], ExComplex: []ComplexRule{{Pattern: "safe*.example"}}}
	ix.ApproxBytes()
	if !reflect.DeepEqual(backing, before) {
		t.Fatal("footprint read modified the immutable complex-rule backing array")
	}
}
