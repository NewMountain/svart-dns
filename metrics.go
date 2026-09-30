package main

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/yeti/svart-dns/internal/policycore"
)

// --- Counters (incremented in logwriter flush — background goroutine) ---

var (
	dnsQueriesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_queries_total",
		Help: "Total DNS queries by action (allow, block, rewrite, error).",
	}, []string{"action"})

	dnsQueryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "svart_dns_query_duration_seconds",
		Help:    "DNS query latency distribution.",
		Buckets: []float64{50e-6, 100e-6, 250e-6, 500e-6, 1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 500e-3, 1},
	})

	dnsClientQueries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_client_queries_total",
		Help: "Per-client DNS queries by action.",
	}, []string{"client_ip", "action"})

	dnsBlocksByTier = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_blocks_by_tier_total",
		Help: "Blocked queries by policy tier (range, group, ip).",
	}, []string{"tier"})

	dnsBlocksByList = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_blocks_by_list_total",
		Help: "Blocked queries by published list name.",
	}, []string{"list_name"})

	dnsQueryTypes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_query_types_total",
		Help: "DNS queries by query type (A, AAAA, HTTPS, etc.).",
	}, []string{"type"})

	dnsResponseCodes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_response_codes_total",
		Help: "DNS queries by response code (NOERROR, NXDOMAIN, SERVFAIL, etc.).",
	}, []string{"rcode"})

	dnsResultReasons = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_result_reasons_total",
		Help: "DNS queries by result reason (published_block, custom_block, custom_allow, published_allow, default_allow, rewrite).",
	}, []string{"reason"})

	// Winning decision breakdown: which tier decided, what result, published vs custom
	dnsDecisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_decisions_total",
		Help: "Winning policy decisions by tier, result, and source type.",
	}, []string{"tier", "result", "source"})

	// Published list attribution for winning decisions globally
	dnsListDecisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_list_decisions_total",
		Help: "Winning decisions attributed to a published list, by list name and result (block/allow).",
	}, []string{"list_name", "result"})

	// Per-tier evaluations — every tier that produced a result, not just the winner.
	// Shows "range wanted to block but IP-level allowed."
	dnsTierEvaluations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_tier_evaluations_total",
		Help: "Per-tier evaluation outcomes including non-winning tiers.",
	}, []string{"tier", "result"})

	// Per-tier published list attribution — which list won at which tier
	dnsTierListHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_tier_list_hits_total",
		Help: "Published list hits per tier and result (block/allow).",
	}, []string{"tier", "list_name", "result"})

	// Per-tier custom rule hits
	dnsTierCustomHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_tier_custom_hits_total",
		Help: "Custom rule hits per tier and action (block/allow).",
	}, []string{"tier", "action"})

	// Entity-level decisions — which named ranges/groups are making decisions
	// (IP tier uses the more targeted client_list_hits and client_custom_hits below)
	dnsEntityDecisions = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_entity_decisions_total",
		Help: "Policy decisions per named entity (range name, group name) by tier and result.",
	}, []string{"tier", "entity", "result"})

	// Per-client list attribution — which published list is blocking/allowing each user
	dnsClientListHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_client_list_hits_total",
		Help: "Published list hits per client IP, list name, and result (block/allow).",
	}, []string{"client_ip", "list_name", "result"})

	// Per-client custom rule impact — are custom rules disproportionately hitting one user
	dnsClientCustomHits = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_client_custom_hits_total",
		Help: "Custom rule hits per client IP and action (block/allow).",
	}, []string{"client_ip", "action"})
)

// --- Upstream metrics (resolver.go — off hot path, cache miss only) ---
//
// The vectors are wrapped so every label goes through upstreamMetricLabel:
// /metrics is unauthenticated, and DoH URLs carry account or profile IDs in
// the path (NextDNS, ControlD) and sometimes userinfo or query tokens (A5).

var (
	dnsUpstreamQueries = upstreamCounterVec{prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_upstream_queries_total",
		Help: "Queries sent to each upstream DNS server (label: scheme://host[:port], path and credentials removed).",
	}, []string{"upstream"})}

	dnsUpstreamErrors = upstreamCounterVec{prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_upstream_errors_total",
		Help: "Errors from each upstream DNS server (label: scheme://host[:port], path and credentials removed).",
	}, []string{"upstream"})}

	dnsUpstreamDuration = upstreamHistogramVec{prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "svart_dns_upstream_duration_seconds",
		Help:    "Upstream DNS resolution latency distribution.",
		Buckets: []float64{1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 500e-3, 1, 2.5},
	}, []string{"upstream"})}

	// Nonzero on a UDP upstream means someone is trying to spoof answers;
	// on DoT/DoH it means the upstream itself is broken.
	dnsUpstreamMismatchedReplies = upstreamCounterVec{prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_upstream_mismatched_replies_total",
		Help: "Upstream replies discarded because they did not answer the query sent (wrong ID, opcode or question).",
	}, []string{"upstream"})}

	dnsUpstreamSaturated = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "svart_dns_upstream_saturated_total",
		Help: "Cache misses answered SERVFAIL because the upstream in-flight exchange cap was reached.",
	})
)

type upstreamCounterVec struct{ *prometheus.CounterVec }

func (v upstreamCounterVec) WithLabelValues(upstream string) prometheus.Counter {
	return v.CounterVec.WithLabelValues(upstreamMetricLabel(upstream))
}

type upstreamHistogramVec struct{ *prometheus.HistogramVec }

func (v upstreamHistogramVec) WithLabelValues(upstream string) prometheus.Observer {
	return v.HistogramVec.WithLabelValues(upstreamMetricLabel(upstream))
}

var upstreamLabelMemo sync.Map // configured upstream string -> label

// upstreamMetricLabel reduces an upstream to scheme://host[:port] (or the
// bare host:port): no userinfo, path, query, fragment, or [/domain/]
// routing prefix. Memoized: upstreams are a small configured set.
func upstreamMetricLabel(upstream string) string {
	if v, ok := upstreamLabelMemo.Load(upstream); ok {
		label, valid := v.(string)
		if !valid {
			panic("upstream label cache has an invalid internal type")
		}
		return label
	}
	label := redactUpstream(upstream)
	upstreamLabelMemo.Store(upstream, label)
	return label
}

func redactUpstream(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "[/") {
		if i := strings.Index(u, "]"); i > 0 {
			u = u[i+1:]
		}
	}
	scheme, rest, hasScheme := strings.Cut(u, "://")
	if !hasScheme {
		rest = u
	}
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if hasScheme {
		return scheme + "://" + rest
	}
	return rest
}

// qtypeMetricLabel keeps svart_dns_query_types_total bounded: the query log
// records unknown types precisely ("TYPE65400"), but as a label that would be
// up to 65k client-chosen series, so they share "other".
func qtypeMetricLabel(t string) string {
	if _, known := dns.StringToType[t]; known {
		return t
	}
	return "other"
}

// --- Per-client and per-entity labels (opt-in) ---

// clientLabels is nil unless METRICS_PER_CLIENT=true. Off by default:
// /metrics is unauthenticated, and per-client series both publish who is on
// the network and grow one series per source address, forever (D9: 100k
// addresses cost 50 MiB in one counter). The series gated by it are
// svart_dns_client_queries_total, svart_dns_client_list_hits_total,
// svart_dns_client_custom_hits_total and svart_dns_entity_decisions_total
// (range and group names).
var clientLabels atomic.Pointer[clientLabeler]

const defaultMetricsClientLimit = 256

// clientLabeler caps client_ip cardinality: clients with an alias (a
// bounded, admin-curated set) always get their own label, other addresses
// get one until limit distinct addresses are admitted, and the rest share
// "other". Only the log writer's flush goroutine calls it.
type clientLabeler struct {
	limit    int
	mu       sync.Mutex
	admitted map[string]struct{}
}

func newClientLabeler(limit int) *clientLabeler {
	return &clientLabeler{limit: limit, admitted: make(map[string]struct{})}
}

func (l *clientLabeler) label(ip string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.admitted[ip]; ok {
		return ip
	}
	if getClientAliasCached(ip) != "" || len(l.admitted) < l.limit {
		l.admitted[ip] = struct{}{}
		return ip
	}
	return "other"
}

// loadPerClientMetricsConfig parses METRICS_PER_CLIENT (bool, default
// false) and METRICS_CLIENT_LIMIT (default 256). nil means off.
func loadPerClientMetricsConfig(getenv func(string) string) (*clientLabeler, error) {
	raw := strings.TrimSpace(getenv("METRICS_PER_CLIENT"))
	if raw == "" {
		return nil, nil
	}
	on, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("METRICS_PER_CLIENT: want true or false, got %q", raw)
	}
	if !on {
		return nil, nil
	}
	limit := defaultMetricsClientLimit
	if rawLimit := strings.TrimSpace(getenv("METRICS_CLIENT_LIMIT")); rawLimit != "" {
		n, err := strconv.Atoi(rawLimit)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("METRICS_CLIENT_LIMIT: want a positive integer, got %q", rawLimit)
		}
		limit = n
	}
	return newClientLabeler(limit), nil
}

// recordQueryMetrics increments every per-query counter for one log entry.
// Called from the log writer's flush loop, off the DNS hot path (DD-015).
func recordQueryMetrics(e queryLogEntry, fcc float64) {
	perClient := clientLabels.Load()
	legacyBlock := deriveLegacyBlockFields(e)

	// Derive action
	action := "allow"
	if e.blocked {
		action = "block"
	} else if e.upstream == "rewrite" {
		action = "rewrite"
	} else if e.responseCode == "SERVFAIL" {
		action = "error"
	}

	dnsQueriesTotal.WithLabelValues(action).Add(fcc)
	dnsQueryDuration.Observe(float64(e.latencyMicroseconds) / 1e6) // one latency sample per entry regardless of coalescing
	if perClient != nil {
		dnsClientQueries.WithLabelValues(perClient.label(e.clientIP), action).Add(fcc)
	}
	dnsQueryTypes.WithLabelValues(qtypeMetricLabel(e.queryType)).Add(fcc)
	dnsResponseCodes.WithLabelValues(e.responseCode).Add(fcc)

	if e.blocked {
		if legacyBlock.tier != "" {
			dnsBlocksByTier.WithLabelValues(legacyBlock.tier).Add(fcc)
		}
		if legacyBlock.listName != "" {
			dnsBlocksByList.WithLabelValues(legacyBlock.listName).Add(fcc)
		} else {
			dnsBlocksByList.WithLabelValues("custom").Add(fcc)
		}
	}
	if e.resultReason != "" {
		dnsResultReasons.WithLabelValues(e.resultReason).Add(fcc)
	}

	// Rich policy decision metrics (DD-019)
	if e.resultTier != "" && e.resultTier != "default" {
		source := "custom"
		if e.resultIsPublished {
			source = "published"
		}
		dnsDecisions.WithLabelValues(e.resultTier, e.result, source).Add(fcc)

		// Global list attribution for winning decision
		if e.resultIsPublished && e.resultListName != "" {
			dnsListDecisions.WithLabelValues(e.resultListName, e.result).Add(fcc)
		}

		// Entity-level decision tracking
		if perClient != nil && e.resultEntity != "" {
			dnsEntityDecisions.WithLabelValues(e.resultTier, e.resultEntity, e.result).Add(fcc)
		}
	}

	// Per-tier evaluations — every tier that produced a result, not just the winner
	if e.rangeResult != "" {
		dnsTierEvaluations.WithLabelValues("range", e.rangeResult).Add(fcc)
		if e.rangeIsPublished && e.rangeListName != "" {
			dnsTierListHits.WithLabelValues("range", e.rangeListName, e.rangeResult).Add(fcc)
		} else if !e.rangeIsPublished && e.rangeRule != "" {
			dnsTierCustomHits.WithLabelValues("range", e.rangeResult).Add(fcc)
		}
		if perClient != nil && e.rangeEntity != "" {
			dnsEntityDecisions.WithLabelValues("range", e.rangeEntity, e.rangeResult).Add(fcc)
		}
	}
	if e.groupResult != "" {
		dnsTierEvaluations.WithLabelValues("group", e.groupResult).Add(fcc)
		if e.groupIsPublished && e.groupListName != "" {
			dnsTierListHits.WithLabelValues("group", e.groupListName, e.groupResult).Add(fcc)
		} else if !e.groupIsPublished && e.groupRule != "" {
			dnsTierCustomHits.WithLabelValues("group", e.groupResult).Add(fcc)
		}
		if perClient != nil && e.groupEntity != "" {
			dnsEntityDecisions.WithLabelValues("group", e.groupEntity, e.groupResult).Add(fcc)
		}
	}
	if e.ipResult != "" {
		dnsTierEvaluations.WithLabelValues("ip", e.ipResult).Add(fcc)
		if e.ipIsPublished && e.ipListName != "" {
			dnsTierListHits.WithLabelValues("ip", e.ipListName, e.ipResult).Add(fcc)
		} else if !e.ipIsPublished && e.ipRule != "" {
			dnsTierCustomHits.WithLabelValues("ip", e.ipResult).Add(fcc)
		}
		// IP tier uses targeted per-client metrics instead of dnsEntityDecisions
	}

	// Per-client attribution: which list is hitting this user, and how much custom rules affect them
	if perClient == nil {
		return
	}
	if e.resultIsPublished && e.resultListName != "" {
		dnsClientListHits.WithLabelValues(perClient.label(e.clientIP), e.resultListName, e.result).Add(fcc)
	} else if !e.resultIsPublished && e.resultRule != "" && e.resultTier != "" && e.resultTier != "default" {
		dnsClientCustomHits.WithLabelValues(perClient.label(e.clientIP), e.result).Add(fcc)
	}
}

// --- List inventory gauges (updated on blocklist/allowlist reload) ---

var (
	dnsBlocklistDomains = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "svart_dns_blocklist_domains",
		Help: "Number of domains per blocklist.",
	}, []string{"list_name"})

	dnsAllowlistDomains = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "svart_dns_allowlist_domains",
		Help: "Number of domains per allowlist.",
	}, []string{"list_name"})

	policyReloadErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "svart_dns_policy_reload_errors_total",
		Help: "Policy reloads that failed; the previous policy stayed active.",
	})
)

// updateListMetrics sets the per-list rule count gauges from a new index.
func updateListMetrics(ix *policycore.Index) {
	dnsBlocklistDomains.Reset()
	dnsAllowlistDomains.Reset()
	for _, l := range ix.Lists {
		if l.Name == "" {
			continue
		}
		gauge := dnsBlocklistDomains
		if l.Allow {
			gauge = dnsAllowlistDomains
		}
		gauge.WithLabelValues(l.Name).Set(float64(l.Count))
	}
}

// --- Sync metrics (sync worker — background goroutine) ---

var (
	syncPollsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_sync_polls_total",
		Help: "Total sync polls per peer.",
	}, []string{"peer"})

	syncRowsReceivedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_sync_rows_received_total",
		Help: "Total rows received from sync per peer.",
	}, []string{"peer"})

	syncErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_sync_errors_total",
		Help: "Total sync errors per peer.",
	}, []string{"peer"})

	syncLastSuccessTimestamp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "svart_dns_sync_last_success_timestamp",
		Help: "Unix timestamp of last successful sync per peer.",
	}, []string{"peer"})

	syncPeerHealthy = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "svart_dns_peer_healthy",
		Help: "Whether each peer is healthy (1) or unhealthy (0).",
	}, []string{"peer"})
)

// --- Auto-refresh metrics (autorefresh.go — background goroutine) ---

var (
	autoRefreshSuccesses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_auto_refresh_successes_total",
		Help: "Successful auto-refreshes by list type and name.",
	}, []string{"type", "list_name"})

	autoRefreshErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_auto_refresh_errors_total",
		Help: "Failed auto-refreshes by list type and name.",
	}, []string{"type", "list_name"})
)

// --- Dashboard stats metrics (admin/dashboard read path) ---

var (
	dashboardStatsRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "svart_dns_dashboard_stats_request_duration_seconds",
		Help:    "Dashboard stats endpoint duration by endpoint and cache result.",
		Buckets: []float64{1e-3, 2.5e-3, 5e-3, 10e-3, 25e-3, 50e-3, 100e-3, 250e-3, 500e-3, 1, 2.5, 5, 10, 20, 30},
	}, []string{"endpoint", "cache"})

	dashboardStatsRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "svart_dns_dashboard_stats_requests_total",
		Help: "Dashboard stats requests by endpoint, cache result, and HTTP status.",
	}, []string{"endpoint", "cache", "status"})
)

// initMetrics registers all Prometheus metrics and scrape-time gauge
// functions. perClient enables the per-client and per-entity series (nil =
// off, see clientLabels).
func initMetrics(perClient *clientLabeler) {
	clientLabels.Store(perClient)
	// Counters + histogram (logwriter flush)
	prometheus.MustRegister(dnsQueriesTotal)
	prometheus.MustRegister(dnsQueryDuration)
	prometheus.MustRegister(dnsBlocksByTier)
	prometheus.MustRegister(dnsBlocksByList)
	prometheus.MustRegister(dnsQueryTypes)
	prometheus.MustRegister(dnsResponseCodes)
	prometheus.MustRegister(dnsResultReasons)
	prometheus.MustRegister(dnsDecisions)
	prometheus.MustRegister(dnsListDecisions)
	prometheus.MustRegister(dnsTierEvaluations)
	prometheus.MustRegister(dnsTierListHits)
	prometheus.MustRegister(dnsTierCustomHits)
	if perClient != nil {
		prometheus.MustRegister(dnsClientQueries)
		prometheus.MustRegister(dnsEntityDecisions)
		prometheus.MustRegister(dnsClientListHits)
		prometheus.MustRegister(dnsClientCustomHits)
	}

	// Upstream (resolver.go)
	prometheus.MustRegister(dnsUpstreamQueries)
	prometheus.MustRegister(dnsUpstreamErrors)
	prometheus.MustRegister(dnsUpstreamDuration)
	prometheus.MustRegister(dnsUpstreamMismatchedReplies)
	prometheus.MustRegister(dnsUpstreamSaturated)

	// List inventory
	prometheus.MustRegister(dnsBlocklistDomains)
	prometheus.MustRegister(dnsAllowlistDomains)
	prometheus.MustRegister(policyReloadErrors)
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_list_index_bytes",
		Help: "Bytes held by the shared index of every enabled blocklist and allowlist.",
	}, func() float64 {
		return float64(policyState.Load().Index.ApproxBytes())
	}))
	// Sync metrics
	prometheus.MustRegister(syncPollsTotal)
	prometheus.MustRegister(syncRowsReceivedTotal)
	prometheus.MustRegister(syncErrorsTotal)
	prometheus.MustRegister(syncLastSuccessTimestamp)
	prometheus.MustRegister(syncPeerHealthy)

	// Auto-refresh
	prometheus.MustRegister(autoRefreshSuccesses)
	prometheus.MustRegister(autoRefreshErrors)
	prometheus.MustRegister(dashboardStatsRequestDuration)
	prometheus.MustRegister(dashboardStatsRequestsTotal)
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_auto_refresh_stale_lists",
		Help: "Number of lists currently past their refresh interval.",
	}, countStaleLists))

	// DNS abuse controls (dnsguard.go): hot-path atomics read at scrape time.
	for _, c := range []struct {
		name, help string
		v          *atomic.Int64
	}{
		{"svart_dns_acl_refused_total", "Queries and TCP connections refused because the source is outside ALLOWED_CLIENTS.", &dnsGuardStats.aclRefused},
		{"svart_dns_rate_limited_total", "Queries dropped or refused by the per-client rate limit (DNS_RATE_LIMIT_QPS).", &dnsGuardStats.rateLimited},
		{"svart_dns_tcp_conns_rejected_total", "TCP connections closed at accept because a global or per-IP connection cap was reached.", &dnsGuardStats.tcpRejected},
		{"svart_dns_handler_panics_total", "DNS handler panics recovered and answered SERVFAIL.", &dnsGuardStats.handlerPanics},
		{"svart_dns_overload_shed_total", "Cache misses answered SERVFAIL at once because too many upstream resolutions were pending.", &dnsGuardStats.overloadShed},
	} {
		v := c.v
		prometheus.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: c.name, Help: c.help},
			func() float64 { return float64(v.Load()) }))
	}

	// Scrape-time gauges — zero operational cost
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_entries",
		Help: "Current number of entries in the DNS cache.",
	}, func() float64 {
		return float64(cache.stats().Entries)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_estimated_bytes",
		Help: "Approximate bytes retained by the current DNS cache contents.",
	}, func() float64 {
		return float64(cache.stats().EstimatedBytes)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_peak_entries",
		Help: "Highest observed DNS cache entry count since process start.",
	}, func() float64 {
		return float64(cache.stats().PeakEntries)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_peak_bytes",
		Help: "Highest observed approximate DNS cache bytes since process start.",
	}, func() float64 {
		return float64(cache.stats().PeakBytes)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_hits_total",
		Help: "Total DNS cache hits (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(cache.hits.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_misses_total",
		Help: "Total DNS cache misses (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(cache.misses.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_expired_total",
		Help: "Total DNS cache entries removed after expiry (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(cache.expired.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_evicted_total",
		Help: "Total DNS cache entries evicted to stay within the byte budget (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(cache.evicted.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cache_max_bytes",
		Help: "DNS cache byte budget (DNS_CACHE_SIZE_MB).",
	}, func() float64 {
		return float64(cache.maxBytes.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_policy_cache_max_bytes",
		Help: "Policy decision cache byte budget (POLICY_CACHE_SIZE_MB).",
	}, func() float64 {
		return float64(policyCache.maxBytes.Load())
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_dashboard_stats_cache_entries",
		Help: "Current in-memory dashboard stats cache entries.",
	}, func() float64 {
		entries, _ := dashboardStatsCache.stats()
		return float64(entries)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_dashboard_stats_cache_approx_bytes",
		Help: "Approximate bytes retained by the in-memory dashboard stats cache.",
	}, func() float64 {
		_, approxBytes := dashboardStatsCache.stats()
		return float64(approxBytes)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_policy_cache_entries",
		Help: "Current entries in the policy LRU cache.",
	}, func() float64 {
		return float64(policyCache.Stats().Entries)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_policy_cache_bytes",
		Help: "Bytes used by the policy LRU cache.",
	}, func() float64 {
		return float64(policyCache.Stats().UsedBytes)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_policy_cache_hits_total",
		Help: "Total policy cache hits (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(policyCache.Stats().Hits)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_policy_cache_misses_total",
		Help: "Total policy cache misses (monotonic, use rate() in Grafana).",
	}, func() float64 {
		return float64(policyCache.Stats().Misses)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_uptime_seconds",
		Help: "Seconds since svart-dns started.",
	}, func() float64 {
		return time.Since(serverStart).Seconds()
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_queue_length",
		Help: "Volatile query events awaiting durable journal commit, including buffered and active batches.",
	}, func() float64 {
		if queryLogWriter != nil && queryLogWriter.spool != nil {
			return float64(queryLogWriter.queued.Load())
		}
		return 0
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_dropped_total",
		Help: "Compatibility counter; durable query logging never drops admitted events.",
	}, func() float64 {
		if queryLogWriter != nil {
			return float64(queryLogWriter.dropped.Load())
		}
		return 0
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_queue_high_watermark",
		Help: "Highest number of volatile query events waiting for durable journal commit since process start.",
	}, func() float64 {
		if queryLogWriter != nil && queryLogWriter.spool != nil {
			return float64(queryLogWriter.highWater.Load())
		}
		return 0
	}))

	// System resource gauges (read latest sample from ring buffer at scrape time)
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_cpu_percent",
		Help: "Process CPU usage percentage (averaged over 30s sample interval).",
	}, func() float64 {
		return getLatestSystemSample().CPUPercent
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_memory_rss_bytes",
		Help: "Process resident set size (physical memory) in bytes.",
	}, func() float64 {
		return float64(getLatestSystemSample().RSSBytes)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_memory_heap_alloc_bytes",
		Help: "Go heap bytes currently allocated (live objects).",
	}, func() float64 {
		return float64(getLatestSystemSample().HeapAlloc)
	}))

	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_disk_db_size_bytes",
		Help: "Combined size of SQLite DB + WAL + SHM files.",
	}, func() float64 {
		return float64(getLatestSystemSample().DBSizeBytes)
	}))
}
