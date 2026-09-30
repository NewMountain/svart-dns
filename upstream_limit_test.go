package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// TestUpstreamExchangesFailFastWhenSaturated is the D11 (upstream side)
// regression test. Every cache miss holds a UDP socket for up to the
// request deadline; against a black-holed upstream a query flood used to
// grow sockets and goroutines without bound until fds or ports ran out.
// Past the in-flight cap a miss must fail immediately with SERVFAIL, count
// in a metric, and not be blamed on the upstream.
func TestUpstreamExchangesFailFastWhenSaturated(t *testing.T) {
	defer setupTestDB(t)()
	blackhole := startUpstreamStub(t, "udp", silent)
	useUpstreams(t, blackhole.addr)

	// Leave exactly two free slots so the test does not need thousands of
	// sockets to reach the cap.
	const free = 2
	reserved := cap(upstreamExchangeSlots) - len(upstreamExchangeSlots) - free
	for i := 0; i < reserved; i++ {
		upstreamExchangeSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < reserved; i++ {
			<-upstreamExchangeSlots
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < free; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var fixtureErr1235 error
			_, fixtureErr1235 = queryUpstream(ctx, clientQuery("hang.example.com.", dns.TypeA, 1, false, false), blackhole.addr)
			if fixtureErr1235 == nil {
				t.Error("blackhole exchange unexpectedly succeeded after cancellation")
			}
		}()
	}
	waitFor(t, "both slots to be taken by hanging exchanges", func() bool {
		return len(upstreamExchangeSlots) == cap(upstreamExchangeSlots)
	})

	saturated := counterValue(t, dnsUpstreamSaturated)
	start := time.Now()
	_, _, err := resolveQuery(context.Background(), clientQuery("www.example.org.", dns.TypeA, 2, false, false))
	if !errors.Is(err, errUpstreamSaturated) {
		t.Fatalf("resolveQuery at the in-flight cap: err=%v, want errUpstreamSaturated", err)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("saturated resolveQuery took %v, want an immediate failure", elapsed)
	}
	if got := counterValue(t, dnsUpstreamSaturated) - saturated; got != 1 {
		t.Errorf("svart_dns_upstream_saturated_total rose by %v, want 1", got)
	}
	if val, ok := upstreamStatsMap.Load(blackhole.addr); ok && requireFixtureType[*upstreamStats](t, val).failures.Load() != 0 {
		t.Errorf("saturation was recorded as an upstream failure")
	}

	w := newMockWriter("10.42.1.42")
	handleDNSRequest(w, clientQuery("www.example.net.", dns.TypeA, 3, false, false))
	if w.written == nil || w.written.Rcode != dns.RcodeServerFailure {
		t.Errorf("client reply at the in-flight cap = %v, want SERVFAIL", w.written)
	}

	cancel()
	wg.Wait()
	if got := len(upstreamExchangeSlots); got != reserved {
		t.Errorf("%d slots held after the hanging exchanges ended, want %d (slots leaked)", got, reserved)
	}
}
