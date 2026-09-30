package policycore

import (
	"fmt"
	"strings"
	"testing"
)

func benchmarkIndex(b testing.TB) *Index {
	b.Helper()
	index, err := BuildIndex([]List{{ID: 1, Name: "fixture"}}, func(add func(int, string)) error {
		for i := 0; i < 10000; i++ {
			add(0, fmt.Sprintf("ads-%d.example.com", i))
		}
		add(0, strings.Repeat("a", 63)+".example.com")
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return index
}

func TestIndexLookupDoesNotAllocate(t *testing.T) {
	index := benchmarkIndex(t)
	for _, domain := range []string{"ads-0.example.com", "ads-9999.example.com", "missing.example.com", strings.Repeat("a", 63) + ".example.com"} {
		allocations := testing.AllocsPerRun(1000, func() { index.LookupExact(domain) })
		if allocations != 0 {
			t.Fatalf("%s: got %g allocations, want zero", domain, allocations)
		}
	}
}

func BenchmarkIndexLookup(b *testing.B) {
	index := benchmarkIndex(b)
	for _, domain := range []string{"ads-0.example.com", "ads-9999.example.com", "missing.example.com", strings.Repeat("a", 63) + ".example.com"} {
		b.Run(domain, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				index.LookupExact(domain)
			}
		})
	}
}

func TestInternCopiesCallerOwnedWords(t *testing.T) {
	builder, err := NewIndexBuilder([]List{{ID: 1}, {ID: 2}})
	if err != nil {
		t.Fatal(err)
	}
	first := []uint64{1}
	firstID := builder.Intern(first)
	first[0] = 2 // A caller reuses its scratch words for a different set.
	secondID := builder.Intern(first)
	if firstID == secondID {
		t.Fatal("distinct membership sets shared an ID")
	}
	if got := builder.Intern([]uint64{1}); got != firstID {
		t.Fatalf("original set ID changed from %d to %d after scratch reuse", firstID, got)
	}
	if got := builder.Intern([]uint64{2}); got != secondID {
		t.Fatalf("second set ID changed from %d to %d", secondID, got)
	}
	if got := builder.Ix.SetOf(int(firstID)); len(got) != 1 || got[0] != 1 {
		t.Fatalf("stored original set changed: %v", got)
	}
	if got := builder.Ix.SetOf(int(secondID)); len(got) != 1 || got[0] != 2 {
		t.Fatalf("stored second set changed: %v", got)
	}
}

func BenchmarkIndexBuild(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkIndex(b)
	}
}
