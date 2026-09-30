# Measurement decisions

- Use the existing disposable, loopback-only benchmark guest; never send load to production DNS. Keep identical disjoint server/generator/stub CPU sets across products.
- Preserve previous raw results and failed attempts. Do not use the old RA=false upstream or REFUSED-as-blocking runs for performance percentage claims.
- Count REFUSED/SERVFAIL as errors. Check every sampled response against exact-name expected blocking; report timeouts separately from successful-response percentiles.
- Fix the local stub to advertise recursion availability after Pi-hole logs exposed repeated recursion-refused warnings. Regression test fails before the fix.
- Build final application code from 4f20de8 with Go 1.27.1 and the cached Debian Bookworm C/C++ compiler. A workstation-native build required a newer glibc than the isolated guest and never served benchmark traffic. Keep that failed launch log.
- Report a single-run four-way comparison, with configuration differences and shared physical-host limitations. Do not infer Raspberry Pi throughput from x86 measurements.
- Use the same corrected footprint harness for the final and retained prior binaries. Identify compiler differences and incomplete historical provenance; report measured RSS/live heap, not a code-only causal attribution.
