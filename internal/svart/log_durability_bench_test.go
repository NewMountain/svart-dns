package svart

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// Run on baseline and repair with -benchtime=262144x -count=3.
// Actual loopback UDP workload exceeds queue capacity four times; diagnostics
// distinguish reply throughput from subsequent durable drain.
func BenchmarkDurableDNSUDP(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		b.Run(fmt.Sprintf("logging=%t", enabled), func(b *testing.B) {
			cleanup := setupTestDBTB(b)
			defer cleanup()
			previous := loggingEnabled.Load()
			loggingEnabled.Store(enabled)
			defer loggingEnabled.Store(previous)
			var calls atomic.Int32
			secdnsSetUpstream(b, secdnsARecordUpstream(b, &calls))
			addr, _ := secdnsServe(b)
			warm := new(dns.Msg)
			warm.SetQuestion("www.example.org.", dns.TypeA)
			secdnsExchange(b, "udp", addr, warm)
			samples := make([]int64, b.N)
			var next atomic.Int64
			var group sync.WaitGroup
			b.ResetTimer()
			for worker := 0; worker < 64; worker++ {
				group.Add(1)
				go func() {
					defer group.Done()
					client := &dns.Client{Net: "udp", Timeout: 10 * time.Second}
					conn, err := client.Dial(addr)
					if err != nil {
						b.Error(err)
						return
					}
					defer func() { checkTestClose(b, conn) }()
					for {
						i := next.Add(1) - 1
						if i >= int64(b.N) {
							return
						}
						request := new(dns.Msg)
						request.SetQuestion("www.example.org.", dns.TypeA)
						start := time.Now()
						reply, _, err := client.ExchangeWithConn(request, conn)
						samples[i] = time.Since(start).Nanoseconds()
						if err != nil {
							b.Error(err)
							return
						}
						if reply.Rcode != dns.RcodeSuccess {
							b.Errorf("rcode=%d, want 0", reply.Rcode)
							return
						}
					}
				}()
			}
			group.Wait()
			b.StopTimer()
			sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
			if len(samples) > 0 {
				b.ReportMetric(float64(samples[len(samples)/2])/1000, "p50_us")
				b.ReportMetric(float64(samples[(len(samples)-1)*99/100])/1000, "p99_us")
			}
			b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "replies/s")
			if enabled {
				b.ReportMetric(float64(queryLogWriter.queued.Load()), "queued_at_end")
				b.ReportMetric(float64(queryLogWriter.highWater.Load()), "peak_queued")
				journal := queryLogWriter.spool.journal
				b.ReportMetric(float64(journal.committedRecords.Load()), "durable_at_end")
				drained := time.Now()
				for queryLogWriter.queued.Load() != 0 {
					time.Sleep(time.Millisecond)
				}
				b.ReportMetric(time.Since(drained).Seconds(), "drain_s")
				var count int
				if err := journal.db.QueryRow("SELECT COUNT(*) FROM journal_records").Scan(&count); err != nil {
					b.Fatal(err)
				}
				if count != b.N+1 {
					b.Fatalf("raw events=%d want %d", count, b.N+1)
				}
				b.ReportMetric(float64(count), "raw_records")
				b.ReportMetric(float64(journal.committedRecords.Load())/float64(journal.commits.Load()), "events/commit")
				b.ReportMetric(float64(journal.commitNanos.Load())/float64(journal.commits.Load())/1e6, "commit_ms")
				for bucket := range journal.batchSizes {
					if count := journal.batchSizes[bucket].Load(); count > 0 {
						b.Logf("commit batch <=%d events: %d", 1<<bucket, count)
					}
				}
			}

			status, err := os.ReadFile("/proc/self/status")
			if err != nil {
				b.Fatal(err)
			}
			for _, line := range strings.Split(string(status), "\n") {
				if strings.HasPrefix(line, "VmHWM:") {
					var kib int64
					if _, err = fmt.Sscanf(line, "VmHWM: %d kB", &kib); err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(kib), "peak_RSS_KiB")
				}
			}
		})
	}
}
