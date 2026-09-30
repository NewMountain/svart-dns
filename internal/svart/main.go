// Package svart implements the DNS filtering service and its management API.
package svart

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	assets "github.com/yeti/svart-dns"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yeti/svart-dns/internal/telemetry"
)

// @title svart-dns API
// @version 1.0
// @description Self-hosted DNS sinkhole with three-tier policy cascade
// @host localhost:3000
// @BasePath /
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name X-Api-Key
// @securityDefinitions.apikey SyncKeyAuth
// @in header
// @name X-Sync-Key

var (
	frontendDist = assets.FrontendDist
	docsHTML     = assets.DocsHTML
	rapidocJS    = assets.RapidocJS
	swaggerJSON  = assets.SwaggerJSON
	apiMarkdown  = assets.APIMarkdown
)

var (
	dnsPort     string
	adminPort   string
	serverStart time.Time

	// dbPathGlobal is set after parsing DB_PATH for use by system stats collector
	dbPathGlobal string

	// TLS config for admin server
	tlsCert string
	tlsKey  string
	tlsCA   string

	// syncTLSConfig is built once at startup for the sync HTTP client
	syncTLSConfig     *tls.Config
	syncTLSConfigErr  error
	syncTLSServerName string

	// externalIP remaps loopback (127.0.0.1/::1) to the host's real IP for logging and policy
	externalIP string
	// externalIPConfigured is true when externalIP came from EXTERNAL_IP rather
	// than interface detection (which, in a container, finds the bridge address).
	externalIPConfigured bool

	// trustProxyHeaders gates whether requestIsHTTPS (auth.go) may trust an
	// X-Forwarded-Proto: https header from a reverse proxy when deciding
	// the session cookie's Secure flag. Default false: an untrusted client
	// could otherwise spoof this header to defeat Secure. Set
	// TRUST_PROXY_HEADERS=true only when the admin server sits behind a
	// proxy that terminates TLS and can be trusted to set this header
	// itself (i.e., the proxy strips/overwrites any client-supplied value).
	trustProxyHeaders bool
)

func loadSyncTLSConfig(caPath string) (*tls.Config, error) {
	if caPath == "" {
		return nil, nil
	}
	// TLS_CA is an operator-selected local trust store, never an HTTP path.
	caCert, err := os.ReadFile(caPath) // #nosec G304 -- deployment configuration intentionally permits any readable CA file.
	if err != nil {
		return nil, fmt.Errorf("read TLS_CA %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("TLS_CA %s contains no valid certificates", caPath)
	}
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
}

// Main starts the DNS service and its management API.
func Main() {
	if len(os.Args) > 1 && os.Args[1] == "verify-raw-archives" {
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: svart-dns verify-raw-archives /absolute/DB_PATH.spool.sqlite /absolute/ARCHIVE_PATH")
			os.Exit(2)
		}
		if err := verifyRawArchives(os.Args[2], os.Args[3], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	initLogging() // defaults before .env

	if err := godotenv.Load(); err != nil {
		slog.Info("no .env file found, using defaults")
	}

	initLogging() // re-read after .env
	telemetryConfig, telemetryErr := telemetry.ParseConfig(os.Getenv)
	if telemetryErr != nil {
		fatal("invalid telemetry configuration", "error", telemetryErr)
	}
	stopTelemetry, telemetryErr := telemetry.Start(context.Background(), telemetryConfig, telemetry.Version())
	if telemetryErr != nil {
		fatal("start telemetry", "error", telemetryErr)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := stopTelemetry(ctx); err != nil {
			slog.Error("stop telemetry", "error", err)
		}
	}()

	updateDNSDebugFlag()
	perClientMetrics, metricsErr := loadPerClientMetricsConfig(os.Getenv)
	if metricsErr != nil {
		fatal("invalid metrics configuration", "error", metricsErr)
	}
	initMetrics(perClientMetrics)
	initLoki()

	serverStart = time.Now()

	// Node identity — reuse NODE_ID from loki.go
	nodeID = getEnv("NODE_ID", "")
	if nodeID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			slog.Warn("cannot determine hostname for node identity", "error", err)
		}
		if hostname == "" {
			hostname = "unknown"
		}
		nodeID = hostname
	}

	startPprof(getEnv("PPROF_ADDR", ""))
	switch v := strings.ToLower(getEnv("LOG_QUERIES", "false")); v {
	case "true", "1", "yes":
		logQueryLines.Store(true)
	case "false", "0", "no", "":
	default:
		fatal("invalid LOG_QUERIES", "value", v, "want", "true or false")
	}
	dnsPort = getEnv("DNS_PORT", "5353")
	adminPort = getEnv("ADMIN_PORT", "3000")
	dbPath := getEnv("DB_PATH", "./svart-dns.db")
	dbPathGlobal = dbPath
	archivePath = getEnv("ARCHIVE_PATH", "./archives")
	trustProxyHeaders = getEnvBool("TRUST_PROXY_HEADERS", false)
	if trustProxyHeaders {
		logAdmin.Warn("TRUST_PROXY_HEADERS enabled — X-Forwarded-Proto is trusted for the session cookie's Secure flag; only enable this behind a proxy that overwrites client-supplied values for this header")
	}

	// Validate list-fetch configuration now; refreshes re-read it, but a typo
	// should stop startup rather than surface hours later as failed refreshes.
	if policy, err := listFetchPolicyFromEnv(); err != nil {
		fatal("invalid list fetch configuration", "error", err)
	} else if policy.allowPrivate {
		logBlocklist.Warn("ALLOW_PRIVATE_LIST_URLS enabled — list URLs may reach loopback, private and link-local addresses; only enable this for list servers on your own network")
	}
	guard, guardErr := loadDNSGuardConfig(os.Getenv)
	if guardErr != nil {
		fatal("invalid DNS listener configuration", "error", guardErr)
	}
	dnsGuard.Store(guard)
	cache.setMaxBytes(guard.cacheMaxBytes)
	policyCache.setMaxBytes(guard.policyCacheMaxBytes)
	logDNS.Info("DNS listener configuration", "allowed_clients", fmt.Sprint(guard.allowedClients),
		"rate_limit_qps", guard.rateLimitQPS(), "tcp_max_conns", guard.tcpMaxConns,
		"tcp_max_conns_per_ip", guard.tcpMaxConnsPerIP, "rebind_protection", guard.rebind != nil,
		"cache_max_bytes", guard.cacheMaxBytes, "policy_cache_max_bytes", guard.policyCacheMaxBytes)

	// Auto-detect external IP for loopback remapping
	externalIP = strings.TrimSpace(getEnv("EXTERNAL_IP", ""))
	externalIPConfigured = externalIP != ""
	if externalIPConfigured {
		if net.ParseIP(externalIP) == nil {
			fatal("invalid EXTERNAL_IP", "value", externalIP, "want", "this host's LAN IP address, e.g. 192.168.1.2")
		}
	} else {
		externalIP = detectExternalIP()
	}
	if externalIP != "" {
		slog.Info("loopback remapping enabled", "external_ip", externalIP)
	}

	// TLS + peer config
	tlsCert = getEnv("TLS_CERT", "")
	tlsKey = getEnv("TLS_KEY", "")
	tlsCA = getEnv("TLS_CA", "")
	syncTLSServerName = strings.TrimSpace(getEnv("SYNC_TLS_SERVER_NAME", ""))
	configureSyncIdentityReplication(os.Getenv("SYNC_REPLICATE_IDENTITY"))
	var allowlistErr error
	syncAllowedPeers, allowlistErr = parseSyncPeerAllowlist(getEnv("SYNC_PEER_ALLOWLIST", ""))
	if allowlistErr != nil {
		syncTLSConfigErr = allowlistErr
		logSync.Error("sync peer allowlist unavailable — peer sync disabled; DNS will continue",
			"error", allowlistErr)
	}

	// Build TLS config for sync HTTP client (used by sync worker to connect to peers)
	if tlsCA != "" {
		var caErr error
		syncTLSConfig, caErr = loadSyncTLSConfig(tlsCA)
		if caErr != nil {
			if syncTLSConfigErr == nil {
				syncTLSConfigErr = caErr
			}
			logSync.Error("sync TLS trust configuration unavailable — peer sync disabled; DNS will continue",
				"error", caErr)
		}
	}

	if err := initDatabase(dbPath); err != nil {
		fatal("failed to initialize database", "error", err)
	}
	defer func() {
		if err := closeDatabase(); err != nil {
			slog.Error("close database", "error", err)
		}
	}()

	if err := initDuckDB(dbPath, archivePath); err != nil {
		// DuckDB is optional — investigation queries won't work but DNS continues
		logAdmin.Error("DuckDB initialization failed — investigation queries disabled", "error", err)
	}
	defer closeDuckDB()

	if err := loadUpstreamsFromDB(); err != nil {
		fatal("failed to load upstreams", "error", err)
	}

	if err := reloadPolicyState("startup", reloadListContent); err != nil {
		fatal("failed to load policy", "error", err)
	}

	if err := loadRewritesFromDB(); err != nil {
		fatal("failed to load rewrites", "error", err)
	}

	if err := loadClientAliasCache(); err != nil {
		fatal("failed to load client aliases", "error", err)
	}
	if err := loadLoggingSetting(); err != nil {
		fatal("failed to load logging settings", "error", err)
	}
	if err := bootstrapAuth(); err != nil {
		fatal("failed to initialize authentication", "error", err)
	}
	if err := seedSyncSettings(); err != nil {
		fatal("failed to initialize sync settings", "error", err)
	}
	warnIfDoHWithoutBootstrap()

	go startCacheCleanupWorker()
	startArchiveWorker()
	startRawArchiveWorker()
	startAutoRefreshWorker()
	startSystemStatsCollector()
	initSync()

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dnsAddr := ":" + dnsPort
	adminAddr := ":" + adminPort

	// Fail fast if ports are unavailable
	fatal := make(chan error, 3)

	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := startDNSServer(ctx, "udp", dnsAddr); err != nil {
			fatal <- fmt.Errorf("UDP DNS: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := startDNSServer(ctx, "tcp", dnsAddr); err != nil {
			fatal <- fmt.Errorf("TCP DNS: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := startAdminServer(ctx, adminAddr); err != nil && err != http.ErrServerClosed {
			fatal <- fmt.Errorf("admin server: %w", err)
		}
	}()

	slog.Info("svart-dns started", "dns_port", dnsPort, "admin_port", adminPort)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		slog.Info("shutting down")
	case err := <-fatal:
		slog.Error("fatal startup error", "error", err)
	}

	// Hard deadline: exit no matter what after 5s, or on second signal
	deadline := time.AfterFunc(5*time.Second, func() {
		slog.Warn("shutdown timed out, forcing exit")
		os.Exit(1)
	})
	go func() {
		<-sigChan
		os.Exit(1)
	}()

	closeAutoRefresh()
	closeCertWatcher()
	closeSync()
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	<-done

	deadline.Stop()
	closeRawArchiveWorker()
	closeArchiver()
	closeLogWriter()
	shutdownLoki(context.Background())
	slog.Info("shutdown complete")
}

func newAdminMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Exempt — no auth
	mux.HandleFunc("/health", handleHealth)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/docs", handleDocs)
	mux.HandleFunc("/docs/swagger.json", handleDocsJSON)
	mux.HandleFunc("/api/openapi.json", handleDocsJSON)
	mux.HandleFunc("/openapi.json", handleDocsJSON)
	mux.HandleFunc("/docs.md", handleDocsMarkdown)
	mux.HandleFunc("/static/rapidoc-min.js", handleRapidocJS)
	mux.HandleFunc("/api/auth/login", handleAPIAuthLogin)
	mux.HandleFunc("/api/auth/check", handleAPIAuthCheck)
	mux.HandleFunc("/api/setup", handleAPISetup) // public; guarded by the one-time setup token
	mux.HandleFunc("/api/auth/logout", handleAPIAuthLogout)

	// API: GET=readonly, mutations=admin
	mux.HandleFunc("/api/stats", apiAuth(wrapDashboardStatsEndpoint("summary", false, handleAPIStats)))
	mux.HandleFunc("/api/stats/dashboard", apiAuth(wrapDashboardStatsEndpoint("dashboard", true, handleAPIStatsDashboard)))
	mux.HandleFunc("/api/upstreams", apiAuth(handleAPIUpstreams))
	mux.HandleFunc("/api/upstreams/", apiAuth(handleAPIUpstreamAction))
	mux.HandleFunc("/api/blocklists", apiAuth(handleAPIBlocklistsRouter))
	mux.HandleFunc("/api/blocklists/", apiAuth(handleAPIBlocklistAction))
	mux.HandleFunc("/api/allowlists", apiAuth(handleAPIAllowlistsRouter))
	mux.HandleFunc("/api/allowlists/", apiAuth(handleAPIAllowlistAction))
	mux.HandleFunc("/api/ranges", apiAuth(handleAPIRangesRouter))
	mux.HandleFunc("/api/ranges/", apiAuth(handleAPIRangeAction))
	mux.HandleFunc("/api/settings", apiAuth(handleAPISettingsRouter))
	mux.HandleFunc("/api/settings/", apiAuth(handleAPISetting))
	mux.HandleFunc("/api/bootstrap", apiAuth(handleAPIBootstrapRouter))
	mux.HandleFunc("/api/clients", apiAuth(handleAPIClientsRouter))
	mux.HandleFunc("/api/clients/", apiAuth(handleAPIClient))
	mux.HandleFunc("/api/groups", apiAuth(handleAPIGroupsRouter))
	mux.HandleFunc("/api/groups/", apiAuth(handleAPIGroupAction))
	mux.HandleFunc("/api/rewrites", apiAuth(handleAPIRewritesRouter))
	mux.HandleFunc("/api/rewrites/", apiAuth(handleAPIRewriteAction))
	mux.HandleFunc("/api/rewrites/batch", adminAuth(handleAPIRewritesBatch))
	mux.HandleFunc("/api/policies", apiAuth(handleAPIPoliciesRouter))
	mux.HandleFunc("/api/policies/", apiAuth(handleAPIPolicyAction))
	mux.HandleFunc("/api/query-logs", apiAuth(handleAPIQueryLogs))
	mux.HandleFunc("/api/query-logs/", apiAuth(handleAPIQueryLogDetail))

	// Stats endpoints for dashboard charts
	mux.HandleFunc("/api/stats/timeseries", apiAuth(wrapDashboardStatsEndpoint("timeseries", true, handleAPIStatsTimeseries)))
	mux.HandleFunc("/api/stats/top-clients", apiAuth(wrapDashboardStatsEndpoint("top-clients", true, handleAPIStatsTopClients)))
	mux.HandleFunc("/api/stats/block-sources", apiAuth(wrapDashboardStatsEndpoint("block-sources", true, handleAPIStatsBlockSources)))
	mux.HandleFunc("/api/stats/upstream-usage", apiAuth(wrapDashboardStatsEndpoint("upstream-usage", true, handleAPIStatsUpstreamUsage)))
	mux.HandleFunc("/api/stats/top-domains", apiAuth(wrapDashboardStatsEndpoint("top-domains", true, handleAPIStatsTopDomains)))
	mux.HandleFunc("/api/stats/latency", apiAuth(wrapDashboardStatsEndpoint("latency", true, handleAPIStatsLatency)))
	mux.HandleFunc("/api/stats/servfails", apiAuth(wrapDashboardStatsEndpoint("servfails", true, handleAPIStatsServfails)))
	mux.HandleFunc("/api/stats/system", apiAuth(wrapDashboardStatsEndpoint("system", false, handleAPIStatsSystem)))

	// Rewrite stats
	mux.HandleFunc("/api/rewrites/stats", apiAuth(handleAPIRewritesStats))

	// Blocklist stats
	mux.HandleFunc("/api/blocklists/history", apiAuth(handleAPIBlocklistHistory))
	mux.HandleFunc("/api/blocklists/history/", apiAuth(handleAPIBlocklistCheckpointDomains))
	mux.HandleFunc("/api/blocklists/unique-domains", apiAuth(handleAPIBlocklistsUniqueDomains))

	// API: readonly even for POST (read-only queries)
	mux.HandleFunc("/api/policy/evaluate", readonlyAuth(handleAPIPolicyEvaluate))
	mux.HandleFunc("/api/analysis/matrix", readonlyAuth(handleAPIAnalysisMatrix))
	mux.HandleFunc("/api/analysis/compare", readonlyAuth(handleAPIAnalysisCompare))
	mux.HandleFunc("/api/analysis/simulate", readonlyAuth(handleAPIAnalysisSimulate))
	mux.HandleFunc("/api/analysis/domains", readonlyAuth(handleAPIAnalysisDomains))

	// API: always admin
	mux.HandleFunc("/api/tokens", adminAuth(handleAPITokensRouter))
	mux.HandleFunc("/api/tokens/", adminAuth(handleAPITokenAction))
	mux.HandleFunc("/api/users", adminAuth(handleAPIUsersRouter))
	mux.HandleFunc("/api/users/", adminAuth(handleAPIUserAction))
	mux.HandleFunc("/api/auth/sessions/revoke", adminAuth(handleAPIRevokeSessions))
	mux.HandleFunc("/api/config/export", adminAuth(handleAPIConfigExport))
	mux.HandleFunc("/api/config/import", adminAuth(handleAPIConfigImport))
	mux.HandleFunc("/api/cache/clear", adminAuth(handleAPICacheClear))
	mux.HandleFunc("/api/archive", adminAuth(handleAPIArchive))
	mux.HandleFunc("/api/archive/status", apiAuth(handleAPIArchiveStatus))
	mux.HandleFunc("/api/sync", syncAuth(handleAPISync))
	mux.HandleFunc("/api/sync/pair/complete", handleAPISyncPairComplete) // pairing-code authenticated
	mux.HandleFunc("/api/peers", apiAuth(handleAPIPeers))
	mux.HandleFunc("/api/peers/", adminAuth(handleAPIPeers))
	mux.HandleFunc("/api/peers/pair", adminAuth(handleAPIPeersPair))
	mux.HandleFunc("/api/peers/confirm", adminAuth(handleAPIPeersConfirm))

	// Investigation (DuckDB)
	mux.HandleFunc("/api/investigate", adminAuth(handleAPIInvestigate))
	mux.HandleFunc("/api/investigate/schema", adminAuth(handleAPIInvestigateSchema))

	// React SPA catch-all — serves static files or index.html for client-side routing
	spaHandler := newSPAHandler()
	mux.Handle("/", spaHandler)

	return mux
}

func startAdminServer(ctx context.Context, addr string) error {
	mux := newAdminMux()
	allowedHosts, err := parseAdminAllowedHosts(os.Getenv("ADMIN_ALLOWED_HOSTS"))
	if err != nil {
		return err
	}
	if allowedHosts != nil {
		logAdmin.Info("admin Host allowlist enabled; other Host headers get 421", "entries", len(allowedHosts))
	}
	server := newAdminHTTPServer(addr, newAdminHandler(mux, allowedHosts))
	ctx, cancelServer := context.WithCancel(ctx)
	shutdownDone := make(chan struct{})
	defer func() {
		cancelServer()
		<-shutdownDone
	}()

	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		// Shutdown must drain requests after the serving context is cancelled.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logAdmin.Error("admin server graceful shutdown failed", "error", err)
		}
	}()

	if tlsCert != "" && tlsKey != "" {
		cert, err := loadCertificate(tlsCert, tlsKey)
		if err != nil {
			return fmt.Errorf("failed to load TLS cert: %w", err)
		}
		currentCert.Store(cert)
		server.TLSConfig = newAdminTLSConfig()
		startCertWatcher(tlsCert, tlsKey)
		logAdmin.Info("starting admin server (TLS, auto-reload)", "addr", addr)
		return server.ListenAndServeTLS("", "")
	}
	logAdmin.Info("starting admin server", "addr", addr)
	return server.ListenAndServe()
}

// newAdminTLSConfig builds the tls.Config for the admin HTTPS server.
// MinVersion is pinned to TLS 1.2 explicitly — Go's crypto/tls default
// minimum has been TLS 1.2 since Go 1.22, but pinning it here makes the
// invariant explicit and independent of that stdlib default ever changing.
func newAdminTLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: getCertificate,
		MinVersion:     tls.VersionTLS12,
	}
}

func newAdminHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// newSPAHandler returns an http.Handler that serves the React SPA from the embedded frontend/dist.
// It tries to serve exact file matches first; if no file is found, it serves index.html
// so that React Router can handle client-side routing.
func newSPAHandler() http.Handler {
	distFS, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		slog.Error("failed to create sub filesystem for frontend/dist", "error", err)
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(distFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if the file exists in the embedded filesystem
		f, err := distFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			if err := f.Close(); err != nil {
				logAdmin.Error("close embedded frontend file", "error", err)
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// File not found — serve index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool parses a boolean env var (accepts the same forms as
// strconv.ParseBool: "1"/"t"/"T"/"true"/"TRUE"/"True" and their false
// equivalents). Empty or unparseable values fall back to defaultValue.
func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return b
}
