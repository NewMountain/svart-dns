package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/miekg/dns"
)

func legacyReadStringSetting(key string) string {
	var value string
	if err := db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value); err != nil {
		panic(err)
	}
	return value
}

func legacyReadIntSetting(key string) int {
	return parseRuntimeIntSetting(legacyReadStringSetting(key))
}

func legacyReadDeniedTTL() uint32 {
	return parseDeniedTTLValue(legacyReadStringSetting("denied_ttl"))
}

func clearBootstrapCacheForBench() {
	bootstrapHosts.Range(func(key, _ any) bool {
		bootstrapHosts.Delete(key)
		return true
	})
}

func startBenchmarkDNSServer(tb testing.TB, handler dns.HandlerFunc) (string, func()) {
	tb.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen udp: %v", err)
	}

	server := &dns.Server{
		PacketConn: pc,
		Handler:    handler,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ActivateAndServe()
	}()

	return pc.LocalAddr().String(), func() {
		fixtureErr1013 := server.Shutdown()
		if fixtureErr1013 != nil {
			tb.Errorf("fixture operation failed: %v", fixtureErr1013)
		}
		checkTestClose(tb, pc)
		if err := <-errCh; err != nil {
			tb.Errorf("DNS benchmark server shutdown: %v", err)
		}
	}
}

func benchmarkStaticAResponse(ip string) dns.HandlerFunc {
	parsedIP := net.ParseIP(ip)
	return func(w dns.ResponseWriter, r *dns.Msg) {
		msg := new(dns.Msg)
		msg.SetReply(r)
		if len(r.Question) > 0 && r.Question[0].Qtype == dns.TypeA {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: parsedIP,
			})
		}
		if err := w.WriteMsg(msg); err != nil {
			panic(err)
		}
	}
}

func BenchmarkRuntimeSettingsReads(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()

	b.ReportAllocs()

	b.Run("strategy/snapshot", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = getResolutionStrategy()
		}
	})

	b.Run("strategy/sqlite", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = legacyReadStringSetting("strategy")
		}
	})

	b.Run("cache_ttl/snapshot", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = getCacheTTL()
		}
	})

	b.Run("cache_ttl/sqlite", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = legacyReadIntSetting("cache_ttl")
		}
	})

	b.Run("bootstrap_ttl/snapshot", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = getBootstrapTTL()
		}
	})

	b.Run("bootstrap_ttl/sqlite", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = legacyReadIntSetting("bootstrap_ttl")
		}
	})

	b.Run("denied_ttl/snapshot", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = getDeniedTTL()
		}
	})

	b.Run("denied_ttl/sqlite", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = legacyReadDeniedTTL()
		}
	})
}

func BenchmarkResolveQuery_CacheMissLocalUDP(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()

	upstreamAddr, stopServer := startBenchmarkDNSServer(b, benchmarkStaticAResponse("1.1.1.1"))
	defer stopServer()

	if _, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, 1)", upstreamAddr); err != nil {
		b.Fatalf("insert upstream: %v", err)
	}
	if err := loadUpstreamsFromDB(); err != nil {
		b.Fatalf("load upstreams: %v", err)
	}

	reqs := make([]*dns.Msg, b.N)
	for i := 0; i < b.N; i++ {
		req := new(dns.Msg)
		req.SetQuestion(fmt.Sprintf("bench-%d.example.com.", i), dns.TypeA)
		reqs[i] = req
	}

	cache.clear()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := resolveQuery(context.Background(), reqs[i]); err != nil {
			b.Fatalf("resolveQuery: %v", err)
		}
	}
}

func BenchmarkResolveUpstreamHostname_BootstrapFill(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()

	bootstrapAddr, stopServer := startBenchmarkDNSServer(b, benchmarkStaticAResponse("127.0.0.1"))
	defer stopServer()

	if _, err := db.Exec("INSERT INTO bootstrap_servers (server) VALUES (?)", bootstrapAddr); err != nil {
		b.Fatalf("insert bootstrap server: %v", err)
	}

	names := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		names[i] = "resolver-" + strconv.Itoa(i) + ".bench.test:53"
	}

	clearBootstrapCacheForBench()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := resolveUpstreamHostname(context.Background(), names[i]); err != nil {
			b.Fatalf("resolveUpstreamHostname: %v", err)
		}
	}
}

func BenchmarkHandleDNSRequest_BlockedCachedPolicy(b *testing.B) {
	cleanup := setupTestDBTB(b)
	defer cleanup()

	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES ('', 'Bench Blocklist', 1)")
	if err != nil {
		b.Fatalf("insert blocklist: %v", err)
	}
	blID, fixtureErr4768 := result.LastInsertId()
	if fixtureErr4768 != nil {
		b.Errorf("fixture operation failed: %v", fixtureErr4768)
	}

	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "blocked.bench.example.com"); err != nil {
		b.Fatalf("insert blocked domain: %v", err)
	}
	if _, err := db.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", "10.42.1.42", blID); err != nil {
		b.Fatalf("assign blocklist: %v", err)
	}
	mustReloadPolicy(b)

	oldLogging := loggingEnabled.Load()
	loggingEnabled.Store(false)
	defer loggingEnabled.Store(oldLogging)

	req := new(dns.Msg)
	req.SetQuestion("blocked.bench.example.com.", dns.TypeA)

	// Prime the policy cache so this benchmark is mostly the blocked-response path.
	_ = evaluatePolicy("10.42.1.42", "blocked.bench.example.com.", 1)

	writer := newMockWriter("10.42.1.42")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		writer.written = nil
		handleDNSRequest(writer, req)
		if writer.written == nil || writer.written.Rcode != dns.RcodeNameError {
			b.Fatalf("expected NXDOMAIN blocked response, got %#v", writer.written)
		}
	}
}
