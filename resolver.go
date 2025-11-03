package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type ResolverConfig struct {
	Timeout time.Duration
}

var resolverConfig = &ResolverConfig{
	Timeout: 5 * time.Second,
}

type upstreamStats struct {
	failures   int64
	totalTime  int64
	queryCount int64
}

var upstreamStatsMap sync.Map

func resolveQuery(ctx context.Context, req *dns.Msg) (*dns.Msg, error) {
	if len(req.Question) == 0 {
		return nil, fmt.Errorf("no question in request")
	}

	q := req.Question[0]

	if cached, ok := cache.get(q.Name, q.Qtype); ok {
		cached.SetReply(req)
		return cached, nil
	}

	upstreams := upstreamStorage.getAll()

	var enabledUpstreams []Upstream
	for _, u := range upstreams {
		if u.Enabled {
			enabledUpstreams = append(enabledUpstreams, u)
		}
	}

	if len(enabledUpstreams) == 0 {
		return nil, fmt.Errorf("no enabled upstreams configured")
	}

	var strategy string
	db.QueryRow("SELECT value FROM settings WHERE key = 'strategy'").Scan(&strategy)

	var resp *dns.Msg
	var err error

	if strategy == "parallel" {
		resp, err = resolveParallel(ctx, req, enabledUpstreams)
	} else {
		resp, err = resolveLoadBalanced(ctx, req, enabledUpstreams)
	}

	if err != nil {
		return nil, err
	}

	var cacheTTL int
	db.QueryRow("SELECT value FROM settings WHERE key = 'cache_ttl'").Scan(&cacheTTL)
	cache.set(q.Name, q.Qtype, resp, cacheTTL)

	return resp, nil
}

func resolveLoadBalanced(ctx context.Context, req *dns.Msg, upstreams []Upstream) (*dns.Msg, error) {
	weights := make([]float64, len(upstreams))
	totalWeight := 0.0

	for i, u := range upstreams {
		weight := calculateWeight(u.Upstream)
		weights[i] = weight
		totalWeight += weight
	}

	for i := 0; i < len(upstreams); i++ {
		selected := weightedRandomSelect(upstreams, weights, totalWeight)

		start := time.Now()
		resp, err := queryUpstream(ctx, req, selected.Upstream)
		latency := time.Since(start).Milliseconds()

		if err != nil {
			recordFailure(selected.Upstream)
			log.Printf("Failed to query upstream %s: %v", selected.Upstream, err)
			continue
		}

		recordSuccess(selected.Upstream, latency)
		return resp, nil
	}

	return nil, fmt.Errorf("all upstreams failed")
}

func resolveParallel(ctx context.Context, req *dns.Msg, upstreams []Upstream) (*dns.Msg, error) {
	type result struct {
		resp *dns.Msg
		err  error
		idx  int
	}

	results := make(chan result, len(upstreams))

	for i, u := range upstreams {
		go func(idx int, upstream Upstream) {
			start := time.Now()
			resp, err := queryUpstream(ctx, req, upstream.Upstream)
			latency := time.Since(start).Milliseconds()

			if err != nil {
				recordFailure(upstream.Upstream)
			} else {
				recordSuccess(upstream.Upstream, latency)
			}

			results <- result{resp: resp, err: err, idx: idx}
		}(i, u)
	}

	for i := 0; i < len(upstreams); i++ {
		res := <-results
		if res.err == nil {
			return res.resp, nil
		}
	}

	return nil, fmt.Errorf("all upstreams failed")
}

func calculateWeight(upstream string) float64 {
	val, ok := upstreamStatsMap.Load(upstream)
	if !ok {
		return 1.0
	}

	stats := val.(*upstreamStats)
	if stats.queryCount == 0 {
		return 1.0
	}

	failureRate := float64(stats.failures) / float64(stats.queryCount)
	avgTime := float64(stats.totalTime) / float64(stats.queryCount)

	weight := 1.0 / (1.0 + failureRate*10 + avgTime/1000)

	if weight < 0.01 {
		weight = 0.01
	}

	return weight
}

func weightedRandomSelect(upstreams []Upstream, weights []float64, totalWeight float64) Upstream {
	r := float64(time.Now().UnixNano()%1000000) / 1000000.0 * totalWeight

	cumulative := 0.0
	for i, w := range weights {
		cumulative += w
		if r <= cumulative {
			return upstreams[i]
		}
	}

	return upstreams[len(upstreams)-1]
}

func recordSuccess(upstream string, latencyMs int64) {
	val, _ := upstreamStatsMap.LoadOrStore(upstream, &upstreamStats{})
	stats := val.(*upstreamStats)

	atomic.AddInt64(&stats.totalTime, latencyMs)
	atomic.AddInt64(&stats.queryCount, 1)
}

func recordFailure(upstream string) {
	val, _ := upstreamStatsMap.LoadOrStore(upstream, &upstreamStats{})
	stats := val.(*upstreamStats)

	atomic.AddInt64(&stats.failures, 1)
	atomic.AddInt64(&stats.queryCount, 1)
}

func queryUpstream(ctx context.Context, req *dns.Msg, upstreamAddr string) (*dns.Msg, error) {
	protocol, address := parseUpstreamAddress(upstreamAddr)

	resolvedAddr, err := resolveUpstreamHostname(address, protocol)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve upstream hostname: %w", err)
	}

	switch protocol {
	case "udp", "":
		return queryUDP(ctx, req, resolvedAddr)
	case "tcp":
		return queryTCP(ctx, req, resolvedAddr)
	case "tls":
		return queryDoT(ctx, req, resolvedAddr)
	case "https":
		return queryDoH(ctx, req, address)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

func parseUpstreamAddress(upstream string) (protocol, address string) {
	upstream = strings.TrimSpace(upstream)

	if strings.HasPrefix(upstream, "#") {
		return "", ""
	}

	if strings.HasPrefix(upstream, "[/") {
		idx := strings.Index(upstream, "]")
		if idx > 0 && idx+1 < len(upstream) {
			upstream = upstream[idx+1:]
		}
	}

	if strings.Contains(upstream, "://") {
		parts := strings.SplitN(upstream, "://", 2)
		protocol = parts[0]
		address = parts[1]
		return
	}

	protocol = "udp"
	address = upstream

	if !strings.Contains(address, ":") {
		address = address + ":53"
	}

	return
}

func queryUDP(ctx context.Context, req *dns.Msg, address string) (*dns.Msg, error) {
	client := &dns.Client{
		Net:     "udp",
		Timeout: resolverConfig.Timeout,
	}

	resp, _, err := client.ExchangeContext(ctx, req, address)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func queryTCP(ctx context.Context, req *dns.Msg, address string) (*dns.Msg, error) {
	if !strings.Contains(address, ":") {
		address = address + ":53"
	}

	client := &dns.Client{
		Net:     "tcp",
		Timeout: resolverConfig.Timeout,
	}

	resp, _, err := client.ExchangeContext(ctx, req, address)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func queryDoT(ctx context.Context, req *dns.Msg, address string) (*dns.Msg, error) {
	if !strings.Contains(address, ":") {
		address = address + ":853"
	}

	client := &dns.Client{
		Net:     "tcp-tls",
		Timeout: resolverConfig.Timeout,
	}

	resp, _, err := client.ExchangeContext(ctx, req, address)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func queryDoH(ctx context.Context, req *dns.Msg, url string) (*dns.Msg, error) {
	if !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	if !strings.Contains(url[8:], "/") {
		url = url + "/dns-query"
	}

	packed, err := req.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/dns-message")
	httpReq.Header.Set("Accept", "application/dns-message")

	client := &http.Client{
		Timeout: resolverConfig.Timeout,
	}

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", httpResp.StatusCode)
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	resp := new(dns.Msg)
	if err := resp.Unpack(body); err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %w", err)
	}

	return resp, nil
}

func resolveUpstreamHostname(address, protocol string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host = address
		port = ""
	}

	if net.ParseIP(host) != nil {
		return address, nil
	}

	if protocol == "https" {
		return address, nil
	}

	if cached, ok := bsCache.get(host); ok {
		if port != "" {
			return net.JoinHostPort(cached, port), nil
		}
		return cached, nil
	}

	ip, err := resolveWithBootstrap(host)
	if err != nil {
		return "", err
	}

	var bootstrapTTL int
	db.QueryRow("SELECT value FROM settings WHERE key = 'bootstrap_ttl'").Scan(&bootstrapTTL)

	msg := new(dns.Msg)
	msg.Answer = append(msg.Answer, &dns.A{
		A: net.ParseIP(ip),
	})
	bsCache.set(host, msg, bootstrapTTL)

	if port != "" {
		return net.JoinHostPort(ip, port), nil
	}
	return ip, nil
}

func resolveWithBootstrap(hostname string) (string, error) {
	rows, err := db.Query("SELECT server FROM bootstrap_servers ORDER BY id")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var bootstrapServers []string
	for rows.Next() {
		var server string
		rows.Scan(&server)
		bootstrapServers = append(bootstrapServers, server)
	}

	if len(bootstrapServers) == 0 {
		return "", fmt.Errorf("no bootstrap servers configured")
	}

	req := new(dns.Msg)
	req.SetQuestion(dns.Fqdn(hostname), dns.TypeA)
	req.RecursionDesired = true

	client := &dns.Client{
		Net:     "udp",
		Timeout: 5 * time.Second,
	}

	for _, server := range bootstrapServers {
		resp, _, err := client.Exchange(req, server)
		if err != nil {
			log.Printf("Bootstrap DNS query to %s failed: %v", server, err)
			continue
		}

		if len(resp.Answer) == 0 {
			continue
		}

		for _, ans := range resp.Answer {
			if a, ok := ans.(*dns.A); ok {
				return a.A.String(), nil
			}
		}
	}

	return "", fmt.Errorf("failed to resolve %s with bootstrap servers", hostname)
}
