package listparse

import (
	"strings"
	"testing"
)

func TestNormalizeStoredPreservesLegacyLoadContract(t *testing.T) {
	// Prior versions could store manually supplied names outside current parser
	// validation. A new reader must not reject an otherwise usable whole snapshot.
	raw := " Legacy-" + strings.Repeat("X", 70) + ".EXAMPLE. "
	got, err := NormalizeStored(raw)
	if err != nil || got != NormalizeEntry(raw) {
		t.Fatalf("got %q err=%v, want legacy normalization %q", got, err, NormalizeEntry(raw))
	}
	fresh, err := Parse(strings.NewReader(raw), 4096, 10)
	if err != nil || fresh.Invalid != 1 || len(fresh.Domains) != 0 || len(fresh.Rules) != 0 {
		t.Fatalf("fresh download should still reject invalid hostname: %+v err=%v", fresh, err)
	}
	if _, err := NormalizeStored("!svart-rule-v2:{}"); err == nil {
		t.Fatal("unknown encoded version accepted")
	}
}
