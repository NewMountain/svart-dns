package main

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/miekg/dns"
)

//go:embed web/templates/layout.html
var layoutHTML string

//go:embed web/templates/dashboard.html
var dashboardHTML string

//go:embed web/templates/upstreams.html
var upstreamsHTML string

//go:embed web/templates/blocklists.html
var blocklistsHTML string

//go:embed web/templates/rewrites.html
var rewritesHTML string

//go:embed web/templates/whatif.html
var whatifHTML string

//go:embed web/templates/settings.html
var settingsHTML string

//go:embed web/templates/clients.html
var clientsHTML string

//go:embed web/templates/client_detail.html
var clientDetailHTML string

//go:embed web/templates/group_detail.html
var groupDetailHTML string

//go:embed web/static/css/admin.css
var adminCSS string

type Rewrite struct {
	Domain      string
	IPAddresses []string
}

var (
	dnsPort         string
	adminPort       string
	serverStart     time.Time
	rewritesCache   sync.Map
	rewritesCacheMu sync.RWMutex
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using defaults")
	}

	serverStart = time.Now()

	dnsPort = getEnv("DNS_PORT", "5353")
	adminPort = getEnv("ADMIN_PORT", "3000")
	dbPath := getEnv("DB_PATH", "./svart-dns.db")

	if err := initDatabase(dbPath); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer closeDatabase()

	if err := loadUpstreamsFromDB(); err != nil {
		log.Fatalf("Failed to load upstreams: %v", err)
	}

	if err := loadBlocklistsFromDB(); err != nil {
		log.Fatalf("Failed to load blocklists: %v", err)
	}

	if err := loadRewritesFromDB(); err != nil {
		log.Fatalf("Failed to load rewrites: %v", err)
	}

	go startCacheCleanupWorker()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg.Add(3)

	dnsAddr := ":" + dnsPort
	adminAddr := ":" + adminPort

	go func() {
		defer wg.Done()
		if err := startDNSServer(ctx, "udp", dnsAddr); err != nil {
			log.Printf("UDP DNS server error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := startDNSServer(ctx, "tcp", dnsAddr); err != nil {
			log.Printf("TCP DNS server error: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := startAdminServer(ctx, adminAddr); err != nil {
			log.Printf("Admin server error: %v", err)
		}
	}()

	log.Println("svart-dns started")
	log.Printf("DNS server listening on :%s (UDP/TCP)", dnsPort)
	log.Printf("Admin portal available at http://localhost:%s", adminPort)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	cancel()
	wg.Wait()
	log.Println("Shutdown complete")
}

func startDNSServer(ctx context.Context, network, addr string) error {
	server := &dns.Server{
		Addr:    addr,
		Net:     network,
		Handler: dns.HandlerFunc(handleDNSRequest),
	}

	go func() {
		<-ctx.Done()
		server.Shutdown()
	}()

	log.Printf("Starting DNS server on %s (%s)", addr, network)
	return server.ListenAndServe()
}

func loadRewritesFromDB() error {
	rows, err := db.Query("SELECT domain, ip_addresses FROM rewrites WHERE enabled = 1 AND ip_addresses IS NOT NULL AND ip_addresses != ''")
	if err != nil {
		return err
	}
	defer rows.Close()

	rewritesCacheMu.Lock()
	defer rewritesCacheMu.Unlock()

	rewritesCache = sync.Map{}

	for rows.Next() {
		var domain, ipAddresses string
		if err := rows.Scan(&domain, &ipAddresses); err != nil {
			continue
		}

		domain = strings.ToLower(strings.TrimSpace(domain))
		ips := strings.Fields(ipAddresses)

		if len(ips) > 0 {
			rewritesCache.Store(domain, Rewrite{
				Domain:      domain,
				IPAddresses: ips,
			})
		}
	}

	log.Printf("Loaded rewrites into memory cache")
	return nil
}

func checkRewrite(r *dns.Msg) *dns.Msg {
	if len(r.Question) == 0 {
		return nil
	}

	q := r.Question[0]
	domain := strings.ToLower(strings.TrimSuffix(q.Name, "."))

	val, ok := rewritesCache.Load(domain)
	if !ok {
		return nil
	}

	rewrite := val.(Rewrite)
	ips := rewrite.IPAddresses

	if len(ips) == 0 {
		return nil
	}

	msg := new(dns.Msg)
	msg.SetReply(r)

	if q.Qtype == dns.TypeA {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() != nil {
				rr := &dns.A{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					A: ip.To4(),
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	} else if q.Qtype == dns.TypeAAAA {
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if ip != nil && ip.To4() == nil {
				rr := &dns.AAAA{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeAAAA,
						Class:  dns.ClassINET,
						Ttl:    300,
					},
					AAAA: ip,
				}
				msg.Answer = append(msg.Answer, rr)
			}
		}
	}

	if len(msg.Answer) > 0 {
		return msg
	}

	return nil
}

func handleDNSRequest(w dns.ResponseWriter, r *dns.Msg) {
	startTime := time.Now()
	clientIP := getClientIP(w)

	if len(r.Question) == 0 {
		msg := new(dns.Msg)
		msg.SetReply(r)
		w.WriteMsg(msg)
		return
	}

	q := r.Question[0]
	queryType := dns.TypeToString[q.Qtype]
	log.Printf("DNS Query from %s: %s %s", clientIP, q.Name, queryType)

	if rewriteResp := checkRewrite(r); rewriteResp != nil {
		log.Printf("Rewrite: %s -> %v", q.Name, rewriteResp.Answer)
		w.WriteMsg(rewriteResp)
		logQuery(clientIP, q.Name, queryType, dns.RcodeToString[rewriteResp.Rcode], false, "rewrite", time.Since(startTime).Milliseconds())
		return
	}

	if isBlockedForClient(clientIP, q.Name) {
		log.Printf("Blocked: %s for client %s", q.Name, clientIP)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "NXDOMAIN", true, "", time.Since(startTime).Milliseconds())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := resolveQuery(ctx, r)
	if err != nil {
		log.Printf("Resolution failed for %s: %v", q.Name, err)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeServerFailure)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "SERVFAIL", false, "", time.Since(startTime).Milliseconds())
		return
	}

	if isResponseBlockedByIP(clientIP, resp) {
		log.Printf("Blocked by IP: %s for client %s", q.Name, clientIP)
		msg := new(dns.Msg)
		msg.SetReply(r)
		msg.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(msg)

		logQuery(clientIP, q.Name, queryType, "NXDOMAIN", true, "", time.Since(startTime).Milliseconds())
		return
	}

	w.WriteMsg(resp)

	rcode := dns.RcodeToString[resp.Rcode]
	latency := time.Since(startTime).Milliseconds()
	log.Printf("Resolved %s -> %s (latency: %dms)", q.Name, rcode, latency)

	logQuery(clientIP, q.Name, queryType, rcode, false, "", latency)
}

func logQuery(clientIP, queryName, queryType, responseCode string, blocked bool, upstream string, latencyMs int64) {
	go func() {
		_, err := db.Exec(`
			INSERT INTO query_logs (client_ip, query_name, query_type, response_code, blocked, upstream, latency_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, clientIP, queryName, queryType, responseCode, blocked, upstream, latencyMs)

		if err != nil {
			log.Printf("Failed to log query: %v", err)
		}
	}()
}

func startAdminServer(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", handleAdminHome)
	mux.HandleFunc("/upstreams", handleUpstreams)
	mux.HandleFunc("/blocklists", handleBlocklists)
	mux.HandleFunc("/rewrites", handleRewrites)
	mux.HandleFunc("/whatif", handleWhatIf)
	mux.HandleFunc("/groups/", handleGroupDetail)
	mux.HandleFunc("/clients/", handleClientDetail)
	mux.HandleFunc("/clients", handleClients)
	mux.HandleFunc("/settings", handleSettings)
	mux.HandleFunc("/static/css/admin.css", handleAdminCSS)
	mux.HandleFunc("/api/stats", handleStats)
	mux.HandleFunc("/api/upstreams", handleAPIUpstreams)
	mux.HandleFunc("/api/upstreams/", handleAPIUpstreamAction)
	mux.HandleFunc("/api/blocklists", handleAPIBlocklists)
	mux.HandleFunc("/api/blocklists/", handleAPIBlocklistAction)
	mux.HandleFunc("/api/settings/", handleAPISetting)
	mux.HandleFunc("/api/bootstrap", handleAPIBootstrap)
	mux.HandleFunc("/api/cache/clear", handleAPICacheClear)
	mux.HandleFunc("/api/clients/", handleAPIClient)
	mux.HandleFunc("/api/groups", handleAPIGroups)
	mux.HandleFunc("/api/groups/", handleAPIGroupAction)
	mux.HandleFunc("/api/rewrites", handleAPIRewrites)
	mux.HandleFunc("/api/rewrites/", handleAPIRewriteAction)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Starting admin server on %s", addr)
	return server.ListenAndServe()
}

func handleAdminHome(w http.ResponseWriter, r *http.Request) {
	var totalQueries, blockedQueries int64
	var avgLatency sql.NullFloat64

	db.QueryRow("SELECT COUNT(*) FROM query_logs").Scan(&totalQueries)
	db.QueryRow("SELECT COUNT(*) FROM query_logs WHERE blocked = 1").Scan(&blockedQueries)
	db.QueryRow("SELECT AVG(latency_ms) FROM query_logs WHERE latency_ms > 0").Scan(&avgLatency)

	avgLatencyVal := int64(0)
	if avgLatency.Valid {
		avgLatencyVal = int64(avgLatency.Float64)
	}

	uptime := time.Since(serverStart)
	uptimeStr := formatDuration(uptime)

	cacheStats := getCacheStats()

	type QueryLog struct {
		Timestamp    string
		ClientIP     string
		ClientIPRaw  string
		QueryName    string
		QueryType    string
		ResponseCode string
		LatencyMs    int64
	}

	rows, _ := db.Query(`
		SELECT ql.timestamp, ql.client_ip, COALESCE(ca.alias, '') as alias, ql.query_name, ql.query_type, ql.response_code, ql.latency_ms 
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		ORDER BY ql.timestamp DESC 
		LIMIT 50
	`)
	defer rows.Close()

	var recentQueries []QueryLog
	for rows.Next() {
		var q QueryLog
		var timestamp string
		var alias string
		rows.Scan(&timestamp, &q.ClientIP, &alias, &q.QueryName, &q.QueryType, &q.ResponseCode, &q.LatencyMs)

		var t time.Time
		var err error

		t, err = time.Parse("2006-01-02 15:04:05", timestamp)
		if err != nil {
			t, err = time.Parse(time.RFC3339, timestamp)
		}
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", timestamp)
		}

		if err != nil {
			q.Timestamp = timestamp
		} else {
			q.Timestamp = t.Format("Jan 2 15:04:05")
		}

		q.ClientIPRaw = q.ClientIP
		if alias != "" {
			q.ClientIP = alias + " (" + q.ClientIP + ")"
		}

		recentQueries = append(recentQueries, q)
	}

	type TopItem struct {
		Domain      string
		ClientIP    string
		ClientIPRaw string
		Count       int64
	}

	domainRows, _ := db.Query(`
		SELECT query_name, COUNT(*) as cnt 
		FROM query_logs 
		GROUP BY query_name 
		ORDER BY cnt DESC 
		LIMIT 10
	`)
	defer domainRows.Close()

	var topDomains []TopItem
	for domainRows.Next() {
		var item TopItem
		domainRows.Scan(&item.Domain, &item.Count)
		topDomains = append(topDomains, item)
	}

	clientRows, _ := db.Query(`
		SELECT ql.client_ip, COALESCE(ca.alias, ql.client_ip) as display_name, COUNT(*) as cnt 
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		GROUP BY ql.client_ip 
		ORDER BY cnt DESC 
		LIMIT 10
	`)
	defer clientRows.Close()

	var topClients []TopItem
	for clientRows.Next() {
		var item TopItem
		var displayName string
		clientRows.Scan(&item.ClientIP, &displayName, &item.Count)
		item.ClientIPRaw = item.ClientIP
		if displayName != item.ClientIP {
			item.ClientIP = displayName + " (" + item.ClientIP + ")"
		}
		topClients = append(topClients, item)
	}

	data := map[string]interface{}{
		"DNSPort":        dnsPort,
		"AdminPort":      adminPort,
		"Uptime":         uptimeStr,
		"TotalQueries":   totalQueries,
		"BlockedQueries": blockedQueries,
		"AvgLatency":     avgLatencyVal,
		"CacheHits":      cacheStats.Hits,
		"CacheMisses":    cacheStats.Misses,
		"CacheHitRate":   cacheStats.HitRate,
		"RecentQueries":  recentQueries,
		"TopDomains":     topDomains,
		"TopClients":     topClients,
	}
	renderPage(w, "Dashboard", "", dashboardHTML, data)
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 24 {
		days := hours / 24
		hours = hours % 24
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func handleAdminCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	fmt.Fprint(w, adminCSS)
}

func handleRewrites(w http.ResponseWriter, r *http.Request) {
	type Rewrite struct {
		ID          int
		Domain      string
		IPAddresses string
		Enabled     bool
	}

	rows, _ := db.Query("SELECT id, domain, COALESCE(ip_addresses, ''), enabled FROM rewrites ORDER BY domain")
	defer rows.Close()

	var rewrites []Rewrite
	for rows.Next() {
		var rw Rewrite
		rows.Scan(&rw.ID, &rw.Domain, &rw.IPAddresses, &rw.Enabled)
		rewrites = append(rewrites, rw)
	}

	data := map[string]interface{}{
		"Rewrites": rewrites,
	}
	renderPage(w, "Rewrites", "rewrites", rewritesHTML, data)
}

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
	type BlocklistResult struct {
		ID          int
		Alias       string
		Enabled     bool
		IsBlocked   bool
		MatchedRule string
	}

	domain := r.URL.Query().Get("domain")
	var results []BlocklistResult

	if domain != "" {
		domain = strings.ToLower(strings.TrimSpace(domain))
		domain = strings.TrimSuffix(domain, ".")

		allDomains := allBlocklistDomains.domains.Load().(map[int]map[string]bool)

		rows, _ := db.Query("SELECT id, alias, enabled FROM blocklists ORDER BY alias")
		defer rows.Close()

		for rows.Next() {
			var result BlocklistResult
			rows.Scan(&result.ID, &result.Alias, &result.Enabled)

			if result.Enabled {
				domains := allDomains[result.ID]

				if domains[domain] {
					result.IsBlocked = true
					result.MatchedRule = domain
				} else {
					parts := strings.Split(domain, ".")
					for i := 1; i < len(parts); i++ {
						subdomain := strings.Join(parts[i:], ".")
						if domains[subdomain] {
							result.IsBlocked = true
							result.MatchedRule = subdomain
							break
						}
					}

					if !result.IsBlocked {
						for pattern := range domains {
							if strings.Contains(pattern, "*") && matchWildcard(pattern, domain) {
								result.IsBlocked = true
								result.MatchedRule = pattern
								break
							}
						}
					}
				}
			}

			results = append(results, result)
		}
	}

	data := map[string]interface{}{
		"Domain":  domain,
		"Results": results,
	}
	renderPage(w, "What If", "whatif", whatifHTML, data)
}

func handleClients(w http.ResponseWriter, r *http.Request) {
	type Client struct {
		IPAddress  string
		Alias      string
		QueryCount int64
		LastSeen   string
	}

	type Group struct {
		ID             int
		Name           string
		MemberCount    int
		BlocklistCount int
	}

	rows, _ := db.Query(`
		SELECT 
			ql.client_ip,
			COALESCE(ca.alias, '') as alias,
			COUNT(*) as query_count,
			MAX(ql.timestamp) as last_seen
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		GROUP BY ql.client_ip
		ORDER BY query_count DESC
	`)
	defer rows.Close()

	var clients []Client
	for rows.Next() {
		var c Client
		var lastSeen string
		rows.Scan(&c.IPAddress, &c.Alias, &c.QueryCount, &lastSeen)

		t, _ := time.Parse("2006-01-02 15:04:05", lastSeen)
		c.LastSeen = t.Format("2006-01-02 15:04")

		clients = append(clients, c)
	}

	groupRows, _ := db.Query(`
		SELECT 
			g.id,
			g.name,
			COUNT(DISTINCT cgm.client_ip) as member_count,
			COUNT(DISTINCT gb.blocklist_id) as blocklist_count
		FROM client_groups g
		LEFT JOIN client_group_members cgm ON g.id = cgm.group_id
		LEFT JOIN group_blocklists gb ON g.id = gb.group_id
		GROUP BY g.id
		ORDER BY g.name
	`)
	defer groupRows.Close()

	var groups []Group
	for groupRows.Next() {
		var g Group
		groupRows.Scan(&g.ID, &g.Name, &g.MemberCount, &g.BlocklistCount)
		groups = append(groups, g)
	}

	data := map[string]interface{}{
		"Clients": clients,
		"Groups":  groups,
	}
	renderPage(w, "Clients", "clients", clientsHTML, data)
}

func handleClientDetail(w http.ResponseWriter, r *http.Request) {
	ip := strings.TrimPrefix(r.URL.Path, "/clients/")
	if ip == "" {
		http.Redirect(w, r, "/clients", http.StatusSeeOther)
		return
	}

	var alias string
	var totalQueries int64
	var avgLatency sql.NullFloat64
	var firstSeen, lastSeen string

	db.QueryRow("SELECT COALESCE(alias, '') FROM client_aliases WHERE ip_address = ?", ip).Scan(&alias)
	db.QueryRow("SELECT COUNT(*), AVG(latency_ms), MIN(timestamp), MAX(timestamp) FROM query_logs WHERE client_ip = ?", ip).Scan(&totalQueries, &avgLatency, &firstSeen, &lastSeen)

	avgLatencyVal := int64(0)
	if avgLatency.Valid {
		avgLatencyVal = int64(avgLatency.Float64)
	}

	type QueryLog struct {
		Timestamp    string
		QueryName    string
		QueryType    string
		ResponseCode string
		LatencyMs    int64
	}

	rows, _ := db.Query(`
		SELECT timestamp, query_name, query_type, response_code, latency_ms 
		FROM query_logs 
		WHERE client_ip = ?
		ORDER BY timestamp DESC 
		LIMIT 100
	`, ip)
	defer rows.Close()

	var queries []QueryLog
	for rows.Next() {
		var q QueryLog
		var timestamp string
		rows.Scan(&timestamp, &q.QueryName, &q.QueryType, &q.ResponseCode, &q.LatencyMs)

		var t time.Time
		var err error

		t, err = time.Parse("2006-01-02 15:04:05", timestamp)
		if err != nil {
			t, err = time.Parse(time.RFC3339, timestamp)
		}
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", timestamp)
		}

		if err != nil {
			q.Timestamp = timestamp
		} else {
			q.Timestamp = t.Format("Jan 2 15:04:05")
		}

		queries = append(queries, q)
	}

	firstSeenFormatted := ""
	lastSeenFormatted := ""

	if t, err := time.Parse("2006-01-02 15:04:05", firstSeen); err == nil {
		firstSeenFormatted = t.Format("Jan 2 15:04:05")
	}
	if t, err := time.Parse("2006-01-02 15:04:05", lastSeen); err == nil {
		lastSeenFormatted = t.Format("Jan 2 15:04:05")
	}

	type Group struct {
		ID       int
		Name     string
		IsMember bool
	}

	type BlocklistAssignment struct {
		ID          int
		Alias       string
		DomainCount int
		IsAssigned  bool
		Source      string
	}

	groupRows, _ := db.Query(`
		SELECT cg.id, cg.name, 
			EXISTS(SELECT 1 FROM client_group_members WHERE client_ip = ? AND group_id = cg.id) as is_member
		FROM client_groups cg
		ORDER BY cg.name
	`, ip)
	defer groupRows.Close()

	var groups []Group
	for groupRows.Next() {
		var g Group
		groupRows.Scan(&g.ID, &g.Name, &g.IsMember)
		groups = append(groups, g)
	}

	blocklistRows, _ := db.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM client_blocklists WHERE client_ip = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, ip)
	defer blocklistRows.Close()

	var allBlocklists []BlocklistAssignment
	for blocklistRows.Next() {
		var bl BlocklistAssignment
		blocklistRows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned)
		allBlocklists = append(allBlocklists, bl)
	}

	activeBlocklistRows, _ := db.Query(`
		SELECT DISTINCT b.id, b.alias, b.domain_count,
			CASE 
				WHEN EXISTS(SELECT 1 FROM client_blocklists cb WHERE cb.client_ip = ? AND cb.blocklist_id = b.id) THEN 'direct'
				ELSE 'group'
			END as source
		FROM blocklists b
		WHERE b.enabled = 1 AND (
			EXISTS(SELECT 1 FROM client_blocklists cb WHERE cb.client_ip = ? AND cb.blocklist_id = b.id)
			OR EXISTS(
				SELECT 1 FROM group_blocklists gb 
				JOIN client_group_members cgm ON gb.group_id = cgm.group_id 
				WHERE cgm.client_ip = ? AND gb.blocklist_id = b.id
			)
		)
		ORDER BY b.alias
	`, ip, ip, ip)
	defer activeBlocklistRows.Close()

	var activeBlocklists []BlocklistAssignment
	for activeBlocklistRows.Next() {
		var bl BlocklistAssignment
		activeBlocklistRows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.Source)
		activeBlocklists = append(activeBlocklists, bl)
	}

	var blocklistCount int
	blocklistCount = len(activeBlocklists)

	data := map[string]interface{}{
		"ClientIP":         ip,
		"Alias":            alias,
		"TotalQueries":     totalQueries,
		"AvgLatency":       avgLatencyVal,
		"FirstSeen":        firstSeenFormatted,
		"LastSeen":         lastSeenFormatted,
		"Queries":          queries,
		"Groups":           groups,
		"AllGroups":        groups,
		"AllBlocklists":    allBlocklists,
		"ActiveBlocklists": activeBlocklists,
		"BlocklistCount":   blocklistCount,
	}
	renderPage(w, "Client Detail", "clients", clientDetailHTML, data)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	var cacheTTL, bootstrapTTL, logRetention string
	db.QueryRow("SELECT value FROM settings WHERE key = 'cache_ttl'").Scan(&cacheTTL)
	db.QueryRow("SELECT value FROM settings WHERE key = 'bootstrap_ttl'").Scan(&bootstrapTTL)
	db.QueryRow("SELECT value FROM settings WHERE key = 'log_retention_days'").Scan(&logRetention)

	cacheStats := getCacheStats()

	data := map[string]interface{}{
		"CacheTTL":     cacheTTL,
		"BootstrapTTL": bootstrapTTL,
		"LogRetention": logRetention,
		"CacheHits":    cacheStats.Hits,
		"CacheMisses":  cacheStats.Misses,
		"CacheHitRate": cacheStats.HitRate,
		"CacheEntries": cacheStats.Entries,
	}
	renderPage(w, "Settings", "settings", settingsHTML, data)
}

func renderPage(w http.ResponseWriter, title, activeTab, contentTmpl string, data map[string]interface{}) {
	contentTemplate := template.Must(template.New("content").Parse(contentTmpl))
	var contentBuf bytes.Buffer
	if err := contentTemplate.Execute(&contentBuf, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	layoutData := map[string]interface{}{
		"Title":     title,
		"ActiveTab": activeTab,
		"Content":   template.HTML(contentBuf.String()),
	}

	layoutTemplate := template.Must(template.New("layout").Parse(layoutHTML))
	w.Header().Set("Content-Type", "text/html")
	if err := layoutTemplate.Execute(w, layoutData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"queries":0,"blocked":0,"uptime":"0s"}`)
}

func handleAPISetting(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Path[len("/api/settings/"):]

	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = ?", req.Value, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func handleAPIBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case "POST":
		var req struct {
			Server string `json:"server"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		_, err := db.Exec("INSERT INTO bootstrap_servers (server) VALUES (?)", req.Server)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "PUT":
		var req struct {
			Servers []string `json:"servers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tx.Exec("DELETE FROM bootstrap_servers")
		for _, server := range req.Servers {
			tx.Exec("INSERT INTO bootstrap_servers (server) VALUES (?)", server)
		}

		if err := tx.Commit(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleAPICacheClear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clearDNSCache()

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func handleAPIClient(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path[len("/api/clients/"):]
	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	ip := parts[0]
	action := parts[1]

	if action == "alias" && r.Method == "PUT" {
		var req struct {
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Alias == "" {
			_, err := db.Exec("DELETE FROM client_aliases WHERE ip_address = ?", ip)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			_, err := db.Exec(`
				INSERT INTO client_aliases (ip_address, alias, updated_at) 
				VALUES (?, ?, CURRENT_TIMESTAMP)
				ON CONFLICT(ip_address) DO UPDATE SET alias = ?, updated_at = CURRENT_TIMESTAMP
			`, ip, req.Alias, req.Alias)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	if action == "groups" && len(parts) == 3 {
		groupID := parts[2]

		switch r.Method {
		case "POST":
			_, err := db.Exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id) VALUES (?, ?)", ip, groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			_, err := db.Exec("DELETE FROM client_group_members WHERE client_ip = ? AND group_id = ?", ip, groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if action == "blocklists" && len(parts) == 3 {
		blocklistID := parts[2]

		switch r.Method {
		case "POST":
			_, err := db.Exec("INSERT OR IGNORE INTO client_blocklists (client_ip, blocklist_id) VALUES (?, ?)", ip, blocklistID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			_, err := db.Exec("DELETE FROM client_blocklists WHERE client_ip = ? AND blocklist_id = ?", ip, blocklistID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.Error(w, "Invalid action", http.StatusBadRequest)
}

func handleGroupDetail(w http.ResponseWriter, r *http.Request) {
	groupIDStr := strings.TrimPrefix(r.URL.Path, "/groups/")
	if groupIDStr == "" {
		http.Redirect(w, r, "/clients", http.StatusSeeOther)
		return
	}

	var groupID int
	fmt.Sscanf(groupIDStr, "%d", &groupID)

	var groupName string
	err := db.QueryRow("SELECT name FROM client_groups WHERE id = ?", groupID).Scan(&groupName)
	if err != nil {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}

	type Member struct {
		IPAddress  string
		Alias      string
		QueryCount int64
		LastSeen   string
	}

	type Client struct {
		IPAddress string
		Alias     string
		IsMember  bool
	}

	type BlocklistAssignment struct {
		ID          int
		Alias       string
		DomainCount int
		IsAssigned  bool
		Source      string
	}

	memberRows, _ := db.Query(`
		SELECT 
			cgm.client_ip,
			COALESCE(ca.alias, '') as alias,
			COUNT(ql.id) as query_count,
			MAX(ql.timestamp) as last_seen
		FROM client_group_members cgm
		LEFT JOIN client_aliases ca ON cgm.client_ip = ca.ip_address
		LEFT JOIN query_logs ql ON cgm.client_ip = ql.client_ip
		WHERE cgm.group_id = ?
		GROUP BY cgm.client_ip
		ORDER BY query_count DESC
	`, groupID)
	defer memberRows.Close()

	var members []Member
	for memberRows.Next() {
		var m Member
		var lastSeen sql.NullString
		memberRows.Scan(&m.IPAddress, &m.Alias, &m.QueryCount, &lastSeen)

		if lastSeen.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", lastSeen.String)
			m.LastSeen = t.Format("2006-01-02 15:04")
		} else {
			m.LastSeen = "Never"
		}

		members = append(members, m)
	}

	allClientRows, _ := db.Query(`
		SELECT DISTINCT 
			ql.client_ip,
			COALESCE(ca.alias, '') as alias,
			EXISTS(SELECT 1 FROM client_group_members WHERE client_ip = ql.client_ip AND group_id = ?) as is_member
		FROM query_logs ql
		LEFT JOIN client_aliases ca ON ql.client_ip = ca.ip_address
		ORDER BY ql.client_ip
	`, groupID)
	defer allClientRows.Close()

	var allClients []Client
	for allClientRows.Next() {
		var c Client
		allClientRows.Scan(&c.IPAddress, &c.Alias, &c.IsMember)
		allClients = append(allClients, c)
	}

	blocklistRows, _ := db.Query(`
		SELECT b.id, b.alias, b.domain_count,
			EXISTS(SELECT 1 FROM group_blocklists WHERE group_id = ? AND blocklist_id = b.id) as is_assigned
		FROM blocklists b
		WHERE b.enabled = 1
		ORDER BY b.alias
	`, groupID)
	defer blocklistRows.Close()

	var allBlocklists []BlocklistAssignment
	for blocklistRows.Next() {
		var bl BlocklistAssignment
		blocklistRows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount, &bl.IsAssigned)
		allBlocklists = append(allBlocklists, bl)
	}

	activeBlocklistRows, _ := db.Query(`
		SELECT b.id, b.alias, b.domain_count
		FROM blocklists b
		JOIN group_blocklists gb ON b.id = gb.blocklist_id
		WHERE gb.group_id = ? AND b.enabled = 1
		ORDER BY b.alias
	`, groupID)
	defer activeBlocklistRows.Close()

	var activeBlocklists []BlocklistAssignment
	for activeBlocklistRows.Next() {
		var bl BlocklistAssignment
		activeBlocklistRows.Scan(&bl.ID, &bl.Alias, &bl.DomainCount)
		bl.Source = "group"
		activeBlocklists = append(activeBlocklists, bl)
	}

	var blocklistCount int
	blocklistCount = len(activeBlocklists)

	data := map[string]interface{}{
		"GroupID":          groupID,
		"GroupName":        groupName,
		"MemberCount":      len(members),
		"Members":          members,
		"AllClients":       allClients,
		"AllBlocklists":    allBlocklists,
		"ActiveBlocklists": activeBlocklists,
		"BlocklistCount":   blocklistCount,
	}
	renderPage(w, "Group Detail", "clients", groupDetailHTML, data)
}

func handleAPIGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec("INSERT INTO client_groups (name) VALUES (?)", req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	id, _ := result.LastInsertId()
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id})
}

func handleAPIGroupAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path[len("/api/groups/"):]
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	var groupID int
	fmt.Sscanf(parts[0], "%d", &groupID)

	if len(parts) == 1 {
		switch r.Method {
		case "PUT":
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			_, err := db.Exec("UPDATE client_groups SET name = ? WHERE id = ?", req.Name, groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			_, err := db.Exec("DELETE FROM client_groups WHERE id = ?", groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 3 && parts[1] == "members" {
		clientIP := parts[2]

		switch r.Method {
		case "POST":
			_, err := db.Exec("INSERT OR IGNORE INTO client_group_members (client_ip, group_id) VALUES (?, ?)", clientIP, groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			_, err := db.Exec("DELETE FROM client_group_members WHERE client_ip = ? AND group_id = ?", clientIP, groupID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 3 && parts[1] == "blocklists" {
		var blocklistID int
		fmt.Sscanf(parts[2], "%d", &blocklistID)

		switch r.Method {
		case "POST":
			_, err := db.Exec("INSERT OR IGNORE INTO group_blocklists (group_id, blocklist_id) VALUES (?, ?)", groupID, blocklistID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			_, err := db.Exec("DELETE FROM group_blocklists WHERE group_id = ? AND blocklist_id = ?", groupID, blocklistID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	http.Error(w, "Invalid action", http.StatusBadRequest)
}

func handleAPIRewrites(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Domain      string `json:"domain"`
		IPAddresses string `json:"ip_addresses"`
		Enabled     bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := db.Exec("INSERT INTO rewrites (domain, target, ip_addresses, enabled) VALUES (?, ?, ?, ?)",
		req.Domain, "", req.IPAddresses, req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	loadRewritesFromDB()

	id, _ := result.LastInsertId()
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id})
}

func handleAPIRewriteAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path[len("/api/rewrites/"):]
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	var rewriteID int
	fmt.Sscanf(parts[0], "%d", &rewriteID)

	switch r.Method {
	case "PUT":
		var req struct {
			Domain      string `json:"domain"`
			IPAddresses string `json:"ip_addresses"`
			Enabled     *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Enabled != nil {
			_, err := db.Exec("UPDATE rewrites SET enabled = ? WHERE id = ?", *req.Enabled, rewriteID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.Domain != "" {
			_, err := db.Exec("UPDATE rewrites SET domain = ? WHERE id = ?", req.Domain, rewriteID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		} else if req.IPAddresses != "" {
			_, err := db.Exec("UPDATE rewrites SET ip_addresses = ? WHERE id = ?", req.IPAddresses, rewriteID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		loadRewritesFromDB()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "DELETE":
		_, err := db.Exec("DELETE FROM rewrites WHERE id = ?", rewriteID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		loadRewritesFromDB()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
