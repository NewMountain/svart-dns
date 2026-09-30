# Pure core extraction characterization

Same local machine, fixture and `go version go1.27.1-X:nodwarf5 linux/amd64`,
`GOMAXPROCS=4`, `-p 4`, default natural GC. The before source is `0cfd095`;
the after source is the pure matcher/evaluator extraction committed with this
record. No unrelated processes were stopped. Shared workstation scheduling
remains a timing confound; these are not dedicated-host capacity measurements.

```sh
go test -p 4 -run '^$' -bench 'Benchmark(PolicyMatch|EvaluatePolicy_Uncached)$' -benchmem -benchtime=1s -count=3 .
```

Raw runs are retained in `before.log` and `after.log`. Index match allocations
remain exactly 0 B/op and 0 allocs/op. Uncached policy evaluation remains
3 allocs/op (633–634 B before, 633 B after). Median index matching time is
139.4 ns before and 138.8 ns after. Median uncached evaluation is 1134 ns before
and 601.6 ns after, but the before values vary from 934.2 to 1495 ns. Timing
improvement is **INCONCLUSIVE** on this shared host; allocation preservation is
**VERIFIED**. No additional DNS query allocation was introduced by extraction.

The unchanged policy golden suite and index/parser/refresh tests pass under the
race detector after extraction. The standalone core additionally verifies exact
block/allow/custom attribution, failure without a partial index, caller-owned
configuration preservation, allocation-free matching, and concurrent evaluation.
