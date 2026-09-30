package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/miekg/dns"
	"io"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeDeniedTTLRejectsWireOverflow(t *testing.T) {
	for _, raw := range []string{"4294967296", "4294967596", "9223372036854775807"} {
		if got := parseDeniedTTLValue(raw); got != 3600 {
			t.Errorf("invalid TTL %q wrapped to %d, want default3600", raw, got)
		}
	}
	for _, tc := range []struct {
		raw  string
		want uint32
	}{{"4294967295", math.MaxUint32}, {" +300 ", 300}, {"-1", 3600}, {"0", 3600}, {"garbage", 3600}} {
		if got := parseDeniedTTLValue(tc.raw); got != tc.want {
			t.Errorf("TTL %q=%d,want%d", tc.raw, got, tc.want)
		}
	}
}

func TestRuntimeTTLOffsetsRejectProtocolSizeOverflow(t *testing.T) {
	wire := make([]byte, 12+11+65520+11)
	binary.BigEndian.PutUint16(wire[6:], 2)
	binary.BigEndian.PutUint16(wire[13:], 65280)
	binary.BigEndian.PutUint16(wire[15:], 1)
	binary.BigEndian.PutUint16(wire[21:], 65520)
	second := 12 + 11 + 65520
	binary.BigEndian.PutUint16(wire[second+1:], 1)
	binary.BigEndian.PutUint16(wire[second+3:], 1)
	if offs, err := ttlOffsets(wire); !errors.Is(err, errCacheWire) {
		t.Fatalf("oversized message offsets=%v err=%v; expected malformed wire", offs, err)
	}
}

func TestRuntimeGuardRejectsOverflowBudgets(t *testing.T) {
	for _, key := range []string{"DNS_CACHE_SIZE_MB", "POLICY_CACHE_SIZE_MB"} {
		t.Run(key, func(t *testing.T) {
			_, err := loadDNSGuardConfig(func(k string) string {
				if k == key {
					return "8796093022208"
				}
				return ""
			})
			if err == nil {
				t.Fatal("accepted memory budget whose byte size overflows int64")
			}
		})
	}
	cfg, err := loadDNSGuardConfig(func(k string) string {
		if k == "DNS_RATE_LIMIT_QPS" {
			return strconv.Itoa(math.MaxInt)
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.rateLimit.allow(netip.MustParseAddr("192.0.2.1"), time.Now().UnixNano()) {
		t.Fatal("positive rate budget overflow denied the first client")
	}
}

func TestRuntimeUsageFailurePreservesPublishedSample(t *testing.T) {
	systemStatsMu.Lock()
	oldBuffer, oldHead, oldCount := systemStatsBuffer, systemStatsHead, systemStatsCount
	oldUser, oldSys, oldTime := lastRusageUser, lastRusageSys, lastSampleTime
	previous := SystemSample{Timestamp: time.Unix(100, 0), CPUPercent: 17, RSSBytes: 123}
	systemStatsBuffer = [systemStatsBufferSize]SystemSample{previous}
	systemStatsHead = 1
	systemStatsCount = 1
	lastRusageUser, lastRusageSys, lastSampleTime = 123, 456, previous.Timestamp
	systemStatsMu.Unlock()
	t.Cleanup(func() {
		systemStatsMu.Lock()
		defer systemStatsMu.Unlock()
		systemStatsBuffer, systemStatsHead, systemStatsCount = oldBuffer, oldHead, oldCount
		lastRusageUser, lastRusageSys, lastSampleTime = oldUser, oldSys, oldTime
	})
	collectSystemSampleWithUsage(func(*syscall.Rusage) error { return syscall.EIO })
	if got := getLatestSystemSample(); got != previous {
		t.Errorf("failed sample replaced valid published state: %+v", got)
	}
	if lastRusageUser != 123 || lastRusageSys != 456 || !lastSampleTime.Equal(previous.Timestamp) {
		t.Fatal("failed sample advanced CPU accounting")
	}
	collectSystemSampleWithUsage(func(ru *syscall.Rusage) error { ru.Utime.Usec = 124; ru.Stime.Usec = 457; return nil })
	got := getLatestSystemSample()
	if !got.Timestamp.After(previous.Timestamp) || math.IsNaN(got.CPUPercent) || got.HeapAlloc == 0 {
		t.Fatalf("collector did not recover with real resources: %+v", got)
	}
}

func TestRuntimeRSSParsingRejectsOverflow(t *testing.T) {
	for _, tc := range []struct {
		stat string
		page int
		want uint64
	}{
		{"100 5 0 0 0", 4096, 20480}, {"100 0", 4096, 0}, {"invalid", 4096, 0},
		{"100 -1", 4096, 0}, {"100 18446744073709551615", 4096, 0}, {"100 1", 0, 0}, {"100 1", -1, 0},
	} {
		if got := rssBytesFromStatm([]byte(tc.stat), tc.page); got != tc.want {
			t.Errorf("RSS(%q,%d)=%d,want%d", tc.stat, tc.page, got, tc.want)
		}
	}
	if got := readRSSBytes(); runtime.GOOS == "linux" && got == 0 {
		t.Fatal("Linux process RSS should reflect the live process")
	}
}

func TestRuntimeRateLimiterRejectsInvalidSource(t *testing.T) {
	limiter := newClientRateLimiter(1, 2)
	if limiter.allow(netip.Addr{}, time.Now().UnixNano()) {
		t.Fatal("invalid source admitted")
	}
	for _, tc := range []struct{ addr, prefix string }{{"192.0.2.17", "192.0.2.0/24"}, {"2001:db8:abcd:1234::7", "2001:db8:abcd:1200::/56"}} {
		if got := rateLimitKey(netip.MustParseAddr(tc.addr)); got != netip.MustParsePrefix(tc.prefix) {
			t.Errorf("rate prefix=%s,want%s", got, tc.prefix)
		}
	}
}

func TestRuntimeCachedAnswerRejectsInvalidLifetime(t *testing.T) {
	req := new(dns.Msg)
	req.SetQuestion("runtime.example.", dns.TypeA)
	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: "runtime.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.IPv4(192, 0, 2, 20)}}
	for _, lifetime := range []int{300, math.MaxInt} {
		entry, err := newCachedAnswer(cacheKeyFor(req), resp, lifetime, time.Now().UnixNano())
		if lifetime == math.MaxInt {
			if err == nil || entry != nil {
				t.Fatalf("overflow cache lifetime accepted: %v %v", entry, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		actual := new(dns.Msg)
		if err := actual.Unpack(entry.appendReply(nil, req, entry.storedAt)); err != nil {
			t.Fatal(err)
		}
		if actual.Answer[0].Header().Ttl != 300 {
			t.Fatalf("valid lifetime changed: %v", actual.Answer)
		}
	}
}

type runtimeCloseFailureConn struct {
	net.Conn
	closed bool
}

func (c *runtimeCloseFailureConn) Close() error {
	c.closed = true
	return errors.Join(c.Conn.Close(), errors.New("close failure 192.0.2.9 private-question.example"))
}

type runtimeSingleConnectionListener struct {
	net.Listener
	conn    net.Conn
	nextErr error
}

func (l *runtimeSingleConnectionListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		c := l.conn
		l.conn = nil
		return c, nil
	}
	return nil, l.nextErr
}

func TestRuntimeRejectedConnectionCloseErrorIsObservedPrivately(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(strconv.FormatBool(limited), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
			})
			client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
			})
			server, err := listener.Accept()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
			})
			conn := &runtimeCloseFailureConn{Conn: server}
			end := errors.New("fixture accept exhausted")
			wrapped := &runtimeSingleConnectionListener{Listener: listener, conn: conn, nextErr: end}
			cfg := &dnsGuardConfig{}
			if limited {
				cfg.allowedClients = defaultAllowedClients
			}
			var output bytes.Buffer
			oldLogger, oldQueries := logDNS, logQueryLines.Load()
			logDNS = slog.New(slog.NewJSONHandler(&output, nil))
			logQueryLines.Store(false)
			t.Cleanup(func() { logDNS = oldLogger; logQueryLines.Store(oldQueries) })
			got, err := newLimitedListener(wrapped, cfg).Accept()
			if !errors.Is(err, end) || got != nil || !conn.closed {
				t.Fatalf("rejection was not closed: conn=%v err=%v closed=%v", got, err, conn.closed)
			}
			if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			var buf [1]byte
			if _, err := client.Read(buf[:]); !errors.Is(err, io.EOF) {
				t.Fatalf("rejected socket remains usable: %v", err)
			}
			logs := output.String()
			if !strings.Contains(logs, "failed to close rejected DNS connection") || !strings.Contains(logs, "error_class") || strings.Contains(logs, "192.0.2.9") || strings.Contains(logs, "private-question.example") {
				t.Fatalf("close error missing or leaked query identity: %s", logs)
			}
		})
	}
}
