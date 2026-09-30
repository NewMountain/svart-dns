// stubupstream is a deterministic local DNS upstream for benchmarks. It answers
// every A query with an address derived from the name and every other type with
// NODATA, so benchmark runs never touch the internet and measure the DNS server
// under test rather than a public resolver's latency.
package main

import (
	"flag"
	"hash/fnv"
	"log"
	"net"

	"github.com/miekg/dns"
)

func answer(w dns.ResponseWriter, req *dns.Msg) {
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Authoritative = true
	resp.RecursionAvailable = true
	if len(req.Question) == 1 && req.Question[0].Qtype == dns.TypeA {
		q := req.Question[0]
		h := fnv.New32a()
		_, _ = h.Write([]byte(q.Name))
		sum := h.Sum32()
		resp.Answer = append(resp.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
			// 198.18.0.0/15 is reserved for benchmarking (RFC 2544).
			A: net.IPv4(198, 18|byte(sum>>24&1), byte((sum>>8)&255), byte(sum&255)),
		})
	}
	if err := w.WriteMsg(resp); err != nil {
		log.Printf("stub upstream response failed: %v", err)
	}
}

func main() {
	addr := flag.String("addr", "127.0.0.1:15300", "listen address (UDP and TCP)")
	flag.Parse()
	dns.HandleFunc(".", answer)
	errs := make(chan error, 2)
	for _, network := range []string{"udp", "tcp"} {
		srv := &dns.Server{Addr: *addr, Net: network}
		go func() { errs <- srv.ListenAndServe() }()
	}
	log.Printf("stub upstream listening on %s", *addr)
	log.Fatal(<-errs)
}
