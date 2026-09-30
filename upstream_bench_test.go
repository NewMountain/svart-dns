package main

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

// BenchmarkUpstreamExchange measures one queryUpstream round trip per
// transport against local stubs: message build, exchange, validation and
// client mapping. allocs/op includes the in-process stub server's own
// allocations, so compare runs of this benchmark with each other, not with
// numbers from a remote upstream.
func BenchmarkUpstreamExchange(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()

	udp := startUpstreamStub(b, "udp", answerA("192.0.2.1"))
	udp.discard.Store(true)
	tcp := startUpstreamStub(b, "tcp", answerA("192.0.2.1"))
	tcp.discard.Store(true)
	dot := startDoTStub(b, nil, answerA("192.0.2.1"))
	dot.rec.discard.Store(true)
	doh := startDoHStub(b, dohAnswerA(b, "192.0.2.1"))

	for _, bc := range []struct{ name, upstream string }{
		{"UDP", udp.addr},
		{"TCP", "tcp://" + tcp.addr},
		{"DoT", "tls://" + dot.addr},
		{"DoH", doh.srv.URL + "/dns-query"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			req := clientQuery("www.example.com.", dns.TypeA, 0x2a2a, true, false)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := queryUpstream(context.Background(), req, bc.upstream); err != nil {
					b.Fatalf("queryUpstream(%s): %v", bc.upstream, err)
				}
			}
		})
	}
}
