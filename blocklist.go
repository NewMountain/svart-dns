package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type Blocklist struct {
	ID          int    `json:"id"`
	URL         string `json:"url"`
	Alias       string `json:"alias"`
	Enabled     bool   `json:"enabled"`
	DomainCount int    `json:"domain_count"`
	LastUpdated string `json:"last_updated"`
}

type blocklistStore struct {
	clientDomains atomic.Value
	blockCache    sync.Map
}

var blocklistStorage = &blocklistStore{}

func init() {
	blocklistStorage.clientDomains.Store(make(map[string]map[string]bool))
}

type blocklistDomainsStore struct {
	domains atomic.Value
}

var allBlocklistDomains = &blocklistDomainsStore{}

func init() {
	allBlocklistDomains.domains.Store(make(map[int]map[string]bool))
}

func loadBlocklistsFromDB() error {
	clientDomains := make(map[string]map[string]bool)
	blocklistDomains := make(map[int]map[string]bool)

	blRows, err := db.Query("SELECT id FROM blocklists WHERE enabled = 1")
	if err != nil {
		return err
	}
	var enabledBlocklists []int
	for blRows.Next() {
		var id int
		blRows.Scan(&id)
		enabledBlocklists = append(enabledBlocklists, id)
	}
	blRows.Close()

	for _, blID := range enabledBlocklists {
		domains := make(map[string]bool)
		domainRows, err := db.Query("SELECT domain FROM blocked_domains WHERE blocklist_id = ?", blID)
		if err != nil {
			continue
		}
		for domainRows.Next() {
			var domain string
			domainRows.Scan(&domain)
			domains[domain] = true
		}
		domainRows.Close()
		blocklistDomains[blID] = domains
	}

	rows, err := db.Query(`
		SELECT DISTINCT ql.client_ip 
		FROM query_logs ql
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var clients []string
	for rows.Next() {
		var ip string
		rows.Scan(&ip)
		clients = append(clients, ip)
	}

	for _, clientIP := range clients {
		domains := make(map[string]bool)

		blocklistIDs, err := getBlocklistsForClient(clientIP)
		if err != nil {
			continue
		}

		for _, blocklistID := range blocklistIDs {
			for domain := range blocklistDomains[blocklistID] {
				domains[domain] = true
			}
		}

		if len(domains) > 0 {
			clientDomains[clientIP] = domains
		}
	}

	allBlocklistDomains.domains.Store(blocklistDomains)
	blocklistStorage.clientDomains.Store(clientDomains)
	blocklistStorage.blockCache.Range(func(key, value interface{}) bool {
		blocklistStorage.blockCache.Delete(key)
		return true
	})
	log.Printf("Loaded blocklists for %d clients, %d blocklists (cache cleared)", len(clientDomains), len(blocklistDomains))
	return nil
}

func getBlocklistsForClient(clientIP string) ([]int, error) {
	var blocklistIDs []int

	rows, err := db.Query("SELECT blocklist_id FROM client_blocklists WHERE client_ip = ?", clientIP)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		rows.Scan(&id)
		blocklistIDs = append(blocklistIDs, id)
	}

	groupRows, err := db.Query(`
		SELECT gb.blocklist_id 
		FROM group_blocklists gb
		JOIN client_group_members cgm ON gb.group_id = cgm.group_id
		WHERE cgm.client_ip = ?
		AND gb.blocklist_id NOT IN (SELECT blocklist_id FROM client_blocklists WHERE client_ip = ?)
	`, clientIP, clientIP)
	if err != nil {
		return blocklistIDs, nil
	}
	defer groupRows.Close()

	for groupRows.Next() {
		var id int
		groupRows.Scan(&id)
		blocklistIDs = append(blocklistIDs, id)
	}

	return blocklistIDs, nil
}

func isBlockedForClient(clientIP, domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	cacheKey := clientIP + ":" + domain

	if cached, ok := blocklistStorage.blockCache.Load(cacheKey); ok {
		return cached.(bool)
	}

	allClientDomains := blocklistStorage.clientDomains.Load().(map[string]map[string]bool)
	domains, exists := allClientDomains[clientIP]

	if !exists || len(domains) == 0 {
		blocklistStorage.blockCache.Store(cacheKey, false)
		return false
	}

	if domains[domain] {
		blocklistStorage.blockCache.Store(cacheKey, true)
		return true
	}

	if strings.HasSuffix(domain, ".in-addr.arpa") {
		ip := reverseARPAToIP(domain)
		if ip != "" && domains[ip] {
			blocklistStorage.blockCache.Store(cacheKey, true)
			return true
		}
	}

	parts := strings.Split(domain, ".")
	for i := range parts {
		subdomain := strings.Join(parts[i:], ".")
		if domains[subdomain] {
			blocklistStorage.blockCache.Store(cacheKey, true)
			return true
		}
	}

	for pattern := range domains {
		if matchWildcard(pattern, domain) {
			blocklistStorage.blockCache.Store(cacheKey, true)
			return true
		}
	}

	blocklistStorage.blockCache.Store(cacheKey, false)
	return false
}

func reverseARPAToIP(arpa string) string {
	arpa = strings.TrimSuffix(arpa, ".in-addr.arpa")
	parts := strings.Split(arpa, ".")
	if len(parts) != 4 {
		return ""
	}
	return parts[3] + "." + parts[2] + "." + parts[1] + "." + parts[0]
}

func isResponseBlockedByIP(clientIP string, resp *dns.Msg) bool {
	allClientDomains := blocklistStorage.clientDomains.Load().(map[string]map[string]bool)
	domains, exists := allClientDomains[clientIP]

	if !exists || len(domains) == 0 {
		return false
	}

	for _, answer := range resp.Answer {
		switch rr := answer.(type) {
		case *dns.A:
			ip := rr.A.String()
			if domains[ip] {
				return true
			}
		case *dns.AAAA:
			ip := rr.AAAA.String()
			if domains[ip] {
				return true
			}
		}
	}

	return false
}

func matchWildcard(pattern, domain string) bool {
	if !strings.Contains(pattern, "*") {
		return false
	}

	patternParts := strings.Split(pattern, "*")

	if len(patternParts) == 0 {
		return true
	}

	if patternParts[0] != "" && !strings.HasPrefix(domain, patternParts[0]) {
		return false
	}

	if patternParts[len(patternParts)-1] != "" && !strings.HasSuffix(domain, patternParts[len(patternParts)-1]) {
		return false
	}

	currentPos := 0
	for i, part := range patternParts {
		if part == "" {
			continue
		}

		if i == 0 {
			currentPos = len(part)
			continue
		}

		idx := strings.Index(domain[currentPos:], part)
		if idx == -1 {
			return false
		}
		currentPos += idx + len(part)
	}

	return true
}

func handleBlocklists(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, url, alias, enabled, domain_count, last_updated FROM blocklists ORDER BY id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var blocklists []Blocklist
	for rows.Next() {
		var b Blocklist
		var lastUpdated sql.NullString
		rows.Scan(&b.ID, &b.URL, &b.Alias, &b.Enabled, &b.DomainCount, &lastUpdated)

		if lastUpdated.Valid && lastUpdated.String != "" {
			t, err := time.Parse("2006-01-02 15:04:05", lastUpdated.String)
			if err != nil {
				t, err = time.Parse(time.RFC3339, lastUpdated.String)
			}
			if err == nil {
				b.LastUpdated = t.Format("Jan 2 15:04")
			} else {
				b.LastUpdated = "Never"
			}
		} else {
			b.LastUpdated = "Never"
		}

		blocklists = append(blocklists, b)
	}

	data := map[string]interface{}{
		"Blocklists": blocklists,
	}
	renderPage(w, "Blocklists", "blocklists", blocklistsHTML, data)
}

func handleAPIBlocklists(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Method not allowed"})
		return
	}

	var req struct {
		URL     string `json:"url"`
		Alias   string `json:"alias"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	result, err := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)", req.URL, req.Alias, req.Enabled)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	id, _ := result.LastInsertId()

	go func() {
		if err := refreshBlocklistByID(int(id)); err != nil {
			log.Printf("Failed to fetch blocklist %d: %v", id, err)
		}
	}()

	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "id": id})
}

func handleAPIBlocklistAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path[len("/api/blocklists/"):]
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	var id int
	fmt.Sscanf(parts[0], "%d", &id)

	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case "DELETE":
		_, err := db.Exec("DELETE FROM blocklists WHERE id = ?", id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		loadBlocklistsFromDB()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "POST":
		if action == "toggle" {
			_, err := db.Exec("UPDATE blocklists SET enabled = NOT enabled WHERE id = ?", id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else if action == "refresh" {
			go func() {
				if err := refreshBlocklistByID(id); err != nil {
					log.Printf("Failed to refresh blocklist %d: %v", id, err)
				}
			}()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else {
			http.Error(w, "Invalid action", http.StatusBadRequest)
		}

	case "PUT":
		var req struct {
			URL   string `json:"url"`
			Alias string `json:"alias"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.URL != "" {
			db.Exec("UPDATE blocklists SET url = ? WHERE id = ?", req.URL, id)
		}
		if req.Alias != "" {
			db.Exec("UPDATE blocklists SET alias = ? WHERE id = ?", req.Alias, id)
		}

		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func refreshBlocklistByID(id int) error {
	var url string
	err := db.QueryRow("SELECT url FROM blocklists WHERE id = ?", id).Scan(&url)
	if err != nil {
		return err
	}

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM blocked_domains WHERE blocklist_id = ?", id)

	scanner := bufio.NewScanner(resp.Body)
	count := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "@@") {
			continue
		}

		if strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/") {
			continue
		}

		if strings.Contains(line, "$") {
			continue
		}

		domain := parseDomain(line)
		if domain == "" {
			continue
		}

		tx.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", id, domain)
		count++
	}

	tx.Exec("UPDATE blocklists SET domain_count = ?, last_updated = CURRENT_TIMESTAMP WHERE id = ?", count, id)

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Printf("Refreshed blocklist %d: %d domains", id, count)

	loadBlocklistsFromDB()

	return nil
}

func parseDomain(line string) string {
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "127.0.0.1") || strings.HasPrefix(line, "0.0.0.0") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			line = parts[1]
		} else {
			return ""
		}
	}

	if strings.Contains(line, "$") {
		line = strings.Split(line, "$")[0]
	}

	if strings.HasPrefix(line, "||") {
		line = strings.TrimPrefix(line, "||")
		line = strings.TrimSuffix(line, "|")
		line = strings.TrimSuffix(line, "^")
	} else if strings.HasPrefix(line, "|") {
		line = strings.TrimPrefix(line, "|")
		line = strings.TrimSuffix(line, "|")
		line = strings.TrimSuffix(line, "^")
	}

	if strings.Contains(line, "/") {
		line = strings.Split(line, "/")[0]
	}

	line = strings.TrimSpace(line)
	line = strings.ToLower(line)

	if line == "" || strings.Contains(line, " ") {
		return ""
	}

	return line
}
