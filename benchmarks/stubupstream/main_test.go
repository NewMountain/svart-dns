package main

import (
	"github.com/miekg/dns"
	"testing"
)

type captureReply struct {
	dns.ResponseWriter
	msg *dns.Msg
}

func (w *captureReply) WriteMsg(m *dns.Msg) error { w.msg = m; return nil }

func TestRecursiveUpstreamReply(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("q123.bench-miss.example.", dns.TypeA)
	w := new(captureReply)
	answer(w, req)
	if !w.msg.RecursionAvailable {
		t.Fatal("forwarding resolvers require RA from their recursive upstream")
	}
	if !w.msg.RecursionDesired || w.msg.Rcode != dns.RcodeSuccess || len(w.msg.Answer) != 1 {
		t.Fatalf("unexpected reply: %s", w.msg)
	}
}
