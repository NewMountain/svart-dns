package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"
)

func handleAdminCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css")
	fmt.Fprint(w, adminCSS)
}

func handleAdminHome(w http.ResponseWriter, r *http.Request) {
	totalQueries, blockedQueries, avgLatency := getDashboardStats()
	uptime := time.Since(serverStart)
	uptimeStr := formatDuration(uptime)
	cacheStats := getCacheStats()

	recentQueries, _ := getRecentQueries(50)
	topDomains, _ := getTopDomains(10)
	topClients, _ := getTopClients(10)

	data := map[string]interface{}{
		"DNSPort":        dnsPort,
		"AdminPort":      adminPort,
		"Uptime":         uptimeStr,
		"TotalQueries":   totalQueries,
		"BlockedQueries": blockedQueries,
		"AvgLatency":     avgLatency,
		"CacheHits":      cacheStats.Hits,
		"CacheMisses":    cacheStats.Misses,
		"CacheHitRate":   cacheStats.HitRate,
		"RecentQueries":  recentQueries,
		"TopDomains":     topDomains,
		"TopClients":     topClients,
	}
	renderPage(w, "Dashboard", "", dashboardHTML, data)
}

func handleRewrites(w http.ResponseWriter, r *http.Request) {
	rewrites, _ := getRewrites()
	data := map[string]interface{}{
		"Rewrites": rewrites,
	}
	renderPage(w, "Rewrites", "rewrites", rewritesHTML, data)
}

func handleWhatIf(w http.ResponseWriter, r *http.Request) {
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
	clients, _ := getAllClients()
	groups, _ := getAllGroups()

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

	alias, totalQueries, avgLatency, firstSeen, lastSeen := getClientDetail(ip)
	queries, _ := getClientQueries(ip, 100)
	groups, _ := getClientGroups(ip)
	allBlocklists, _ := getClientBlocklists(ip)
	activeBlocklists, _ := getClientActiveBlocklists(ip)

	firstSeenFormatted := ""
	lastSeenFormatted := ""
	if t, err := time.Parse("2006-01-02 15:04:05", firstSeen); err == nil {
		firstSeenFormatted = t.Format("Jan 2 15:04:05")
	}
	if t, err := time.Parse("2006-01-02 15:04:05", lastSeen); err == nil {
		lastSeenFormatted = t.Format("Jan 2 15:04:05")
	}

	data := map[string]interface{}{
		"ClientIP":         ip,
		"Alias":            alias,
		"TotalQueries":     totalQueries,
		"AvgLatency":       avgLatency,
		"FirstSeen":        firstSeenFormatted,
		"LastSeen":         lastSeenFormatted,
		"Queries":          queries,
		"Groups":           groups,
		"AllGroups":        groups,
		"AllBlocklists":    allBlocklists,
		"ActiveBlocklists": activeBlocklists,
		"BlocklistCount":   len(activeBlocklists),
	}
	renderPage(w, "Client Detail", "clients", clientDetailHTML, data)
}

func handleGroupDetail(w http.ResponseWriter, r *http.Request) {
	groupIDStr := strings.TrimPrefix(r.URL.Path, "/groups/")
	if groupIDStr == "" {
		http.Redirect(w, r, "/clients", http.StatusSeeOther)
		return
	}

	var groupID int
	fmt.Sscanf(groupIDStr, "%d", &groupID)

	groupName, err := getGroupName(groupID)
	if err != nil {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}

	members, _ := getGroupMembers(groupID)
	allClients, _ := getAllClientsForGroup(groupID)
	allBlocklists, _ := getGroupBlocklists(groupID)
	activeBlocklists, _ := getGroupActiveBlocklists(groupID)

	data := map[string]interface{}{
		"GroupID":          groupID,
		"GroupName":        groupName,
		"MemberCount":      len(members),
		"Members":          members,
		"AllClients":       allClients,
		"AllBlocklists":    allBlocklists,
		"ActiveBlocklists": activeBlocklists,
		"BlocklistCount":   len(activeBlocklists),
	}
	renderPage(w, "Group Detail", "clients", groupDetailHTML, data)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	cacheTTL, bootstrapTTL, logRetention := getSettings()
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

		if err := setClientAlias(ip, req.Alias); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	if action == "groups" && len(parts) == 3 {
		groupID := parts[2]

		switch r.Method {
		case "POST":
			if err := addClientToGroup(ip, groupID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			if err := removeClientFromGroup(ip, groupID); err != nil {
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
			if err := addClientBlocklist(ip, blocklistID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			if err := removeClientBlocklist(ip, blocklistID); err != nil {
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

	id, err := createGroup(req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

			if err := updateGroupName(groupID, req.Name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			if err := deleteGroup(groupID); err != nil {
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
			if err := addClientToGroup(clientIP, fmt.Sprintf("%d", groupID)); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			if err := removeClientFromGroup(clientIP, fmt.Sprintf("%d", groupID)); err != nil {
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
			if err := addGroupBlocklist(groupID, blocklistID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			loadBlocklistsFromDB()
			json.NewEncoder(w).Encode(map[string]bool{"success": true})

		case "DELETE":
			if err := removeGroupBlocklist(groupID, blocklistID); err != nil {
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

	id, err := createRewrite(req.Domain, req.IPAddresses, req.Enabled)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	loadRewritesFromDB()

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

		var err error
		if req.Enabled != nil {
			err = updateRewriteEnabled(rewriteID, *req.Enabled)
		} else if req.Domain != "" {
			err = updateRewriteDomain(rewriteID, req.Domain)
		} else if req.IPAddresses != "" {
			err = updateRewriteIPs(rewriteID, req.IPAddresses)
		}

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		loadRewritesFromDB()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	case "DELETE":
		if err := deleteRewrite(rewriteID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		loadRewritesFromDB()
		json.NewEncoder(w).Encode(map[string]bool{"success": true})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
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
