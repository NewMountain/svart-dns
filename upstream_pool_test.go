package main

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Stream upstreams (TCP and DoT) used to open, handshake and close a new
// connection for every query. These tests pin reuse and its failure modes.

func TestTCPUpstreamReusesConnection(t *testing.T) {
	defer setupTestDB(t)()
	stub := startUpstreamStub(t, "tcp", answerA("192.0.2.1"))

	for i := 0; i < 20; i++ {
		resp, err := queryA(t, "tcp://"+stub.addr, fmt.Sprintf("q%d.example.com.", i))
		if err != nil || len(resp.Answer) != 1 {
			t.Fatalf("query %d: resp=%v err=%v", i, resp, err)
		}
	}
	if got := stub.accepted.Load(); got != 1 {
		t.Errorf("20 sequential TCP queries opened %d connections, want 1", got)
	}
}

func TestDoTUpstreamReusesConnection(t *testing.T) {
	defer setupTestDB(t)()
	stub := startDoTStub(t, nil, answerA("192.0.2.1"))

	for i := 0; i < 20; i++ {
		resp, err := queryA(t, "tls://"+stub.addr, fmt.Sprintf("q%d.example.com.", i))
		if err != nil || len(resp.Answer) != 1 {
			t.Fatalf("query %d: resp=%v err=%v", i, resp, err)
		}
	}
	if got := stub.accepted.Load(); got != 1 {
		t.Errorf("20 sequential DoT queries opened %d TLS connections, want 1", got)
	}
}

// TestStreamUpstreamSurvivesServerClosingConnections: servers close idle
// or used-up connections at will (RFC 7766 6.2.3). A pooled connection the
// server has closed must be replaced transparently, never surface as a
// failed query.
func TestStreamUpstreamSurvivesServerClosingConnections(t *testing.T) {
	defer setupTestDB(t)()
	stub := startUpstreamStubWith(t, "tcp", answerA("192.0.2.1"), func(s *dns.Server) {
		s.MaxTCPQueries = 1 // close after every answer
	})

	for i := 0; i < 5; i++ {
		resp, err := queryA(t, "tcp://"+stub.addr, fmt.Sprintf("q%d.example.com.", i))
		if err != nil || len(resp.Answer) != 1 {
			t.Fatalf("query %d after the server closed the previous connection: resp=%v err=%v", i, resp, err)
		}
	}
	if got := stub.accepted.Load(); got != 5 {
		t.Errorf("server that closes after each answer saw %d connections for 5 queries, want 5", got)
	}
}

// TestStreamUpstreamConcurrentExchangesStayMatched: with connections shared
// through a pool, concurrent exchanges must never read each other's reply.
func TestStreamUpstreamConcurrentExchangesStayMatched(t *testing.T) {
	defer setupTestDB(t)()
	stub := startUpstreamStub(t, "tcp", func(r *dns.Msg) *dns.Msg {
		// #nosec G404 -- This RNG only selects test or benchmark workload data and provides no security decision.
		time.Sleep(time.Duration(rand.Intn(3)) * time.Millisecond)
		return answerA("192.0.2.1")(r)
	})

	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for round := 0; round < 3; round++ {
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				name := fmt.Sprintf("r%d-q%d.example.com.", round, i)
				resp, err := queryA(t, "tcp://"+stub.addr, name)
				switch {
				case err != nil:
					errs <- err
				case resp.Question[0].Name != name || len(resp.Answer) != 1 || resp.Answer[0].Header().Name != name:
					errs <- fmt.Errorf("query %s got reply %v", name, resp)
				}
			}(i)
		}
		wg.Wait()
	}
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
