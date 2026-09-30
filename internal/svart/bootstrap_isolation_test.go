package svart

import (
	"context"
	"errors"
	"github.com/miekg/dns"
	"sync/atomic"
	"testing"
	"time"
)

func TestBootstrapShortDeadlineKeepsLongCallerAndWarmCache(t *testing.T) {
	defer setupTestDB(t)()
	invalidateBootstrapCache()
	defer invalidateBootstrapCache()
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	stub := startUpstreamStub(t, "udp", func(q *dns.Msg) *dns.Msg {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return answerA("192.0.2.10")(q)
	})
	useBootstrap(t, stub.addr)
	short, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	shortDone := make(chan error, 1)
	go func() { _, err := bootstrapLookup(short, "short-long.example"); shortDone <- err }()
	<-entered
	long, cancelLong := context.WithTimeout(context.Background(), time.Second)
	defer cancelLong()
	observed := &independentObservedContext{Context: long, observed: make(chan struct{})}
	longDone := make(chan bootstrapEntry, 1)
	go func() {
		ip, err := bootstrapLookup(observed, "short-long.example")
		longDone <- bootstrapEntry{ip: ip, err: err}
	}()
	<-observed.observed
	err := <-shortDone
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("short caller=%v, want deadline exceeded", err)
	}
	result := <-longDone
	if result.err != nil || result.ip != "192.0.2.10" {
		t.Fatalf("long caller=%q %v", result.ip, result.err)
	}
	ip, err := bootstrapLookup(context.Background(), "short-long.example")
	if ip != "192.0.2.10" || err != nil {
		t.Fatalf("warm cache=%q %v", ip, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("shared+warm DNS queries=%d, want 1", got)
	}
}
