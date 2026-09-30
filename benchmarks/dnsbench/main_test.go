package main

import (
	"github.com/miekg/dns"
	"net"
	"testing"
	"time"
)

func TestRefusedIsNotBlocking(t *testing.T) {
	if isBlocked(&dns.Msg{MsgHdr: dns.MsgHdr{Rcode: dns.RcodeRefused}}) {
		t.Fatal("REFUSED is an access or rate-limit failure, not proof of list blocking")
	}
}

func TestReadReplyRejectsDNSFailures(t *testing.T) {
	for _, rcode := range []int{dns.RcodeRefused, dns.RcodeServerFailure} {
		t.Run(dns.RcodeToString[rcode], func(t *testing.T) {
			client, server := net.Pipe()
			defer func() {
				if err := client.Close(); err != nil {
					t.Error(err)
				}
			}()
			defer func() {
				if err := server.Close(); err != nil {
					t.Error(err)
				}
			}()
			reply := &dns.Msg{MsgHdr: dns.MsgHdr{Id: 42, Response: true, Rcode: rcode}}
			wire, err := reply.Pack()
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				if _, err := server.Write(wire); err != nil {
					t.Error(err)
				}
			}()
			got := readReply(client, make([]byte, 4096), 42, time.Now())
			if got.outcome != outcomeError {
				t.Fatalf("got outcome %v, want error", got.outcome)
			}
		})
	}
}
