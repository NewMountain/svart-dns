`incremental-churn.json` preserves the complete deterministic corpus from the
original independent incremental-index oracle test. It records all 100 steps,
three `(list index, variant index)` selections per step, and every Fisher-Yates
swap (including self-swaps), using Go's `math/rand` legacy source seed 9272026.
Even steps use 64 lists, odd steps 65, and there are six rule variants. The test
still compares each update with a full rebuild and verifies the old snapshot.
The stored corpus removes runtime PRNG dependence without changing any input.
