package svart

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetricsScrapePublishesResourceStateAndHonorsPrivacyOptIn(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	oldRegisterer, oldGatherer := prometheus.DefaultRegisterer, prometheus.DefaultGatherer
	oldLabels := clientLabels.Load()
	t.Cleanup(func() {
		prometheus.DefaultRegisterer = oldRegisterer
		prometheus.DefaultGatherer = oldGatherer
		clientLabels.Store(oldLabels)
		secmetricsResetPerClient()
	})
	restoreSamples := withSystemStatsSamples(t, []SystemSample{{Timestamp: time.Now(), CPUPercent: 12.5, RSSBytes: 12345, HeapAlloc: 6789, DBSizeBytes: 4567}})
	defer restoreSamples()
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "default-private", true: "explicit-client-labels"}[enabled], func(t *testing.T) {
			registry := prometheus.NewRegistry()
			prometheus.DefaultRegisterer = registry
			prometheus.DefaultGatherer = registry
			var labeler *clientLabeler
			if enabled {
				labeler = newClientLabeler(10)
			}
			initMetrics(labeler)
			dnsClientQueries.WithLabelValues("192.0.2.231", "block").Add(7)
			server := httptest.NewServer(promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
			defer server.Close()
			response, err := server.Client().Get(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			checkTestClose(t, response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("scrape status=%d body=%s", response.StatusCode, body)
			}
			text := string(body)
			for _, line := range []string{"svart_dns_cpu_percent 12.5\n", "svart_dns_memory_rss_bytes 12345\n", "svart_dns_memory_heap_alloc_bytes 6789\n", "svart_dns_disk_db_size_bytes 4567\n", "svart_dns_cache_entries 0\n", "svart_dns_policy_cache_entries 0\n", "svart_dns_log_writer_dropped_total 0\n", "svart_dns_log_writer_queue_length 0\n"} {
				if !strings.Contains(text, line) {
					t.Errorf("scrape missing %q", line)
				}
			}
			exposed := strings.Contains(text, `client_ip="192.0.2.231"`)
			if exposed != enabled {
				t.Errorf("client series exposed=%v opted-in=%v", exposed, enabled)
			}
			for _, name := range []string{"svart_dns_cache_max_bytes", "svart_dns_policy_cache_max_bytes", "svart_dns_uptime_seconds", "svart_dns_list_index_bytes", "svart_dns_dashboard_stats_cache_approx_bytes", "svart_dns_log_writer_queue_high_watermark"} {
				if !strings.Contains(text, "# TYPE "+name+" gauge\n") {
					t.Errorf("missing gauge metadata %s", name)
				}
			}
		})
	}
}
