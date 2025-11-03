package main

import (
	"context"
	_ "embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"
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

var (
	dnsPort     string
	adminPort   string
	serverStart time.Time
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
