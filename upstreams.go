package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

type Upstream struct {
	ID       int    `json:"id"`
	Upstream string `json:"upstream"`
	Enabled  bool   `json:"enabled"`
}

type upstreamStore struct {
	upstreams atomic.Value
}

var upstreamStorage = &upstreamStore{}

func init() {
	upstreamStorage.upstreams.Store([]Upstream{})
}

func loadUpstreamsFromDB() error {
	rows, err := db.Query("SELECT id, upstream, enabled FROM upstreams ORDER BY id")
	if err != nil {
		return err
	}
	defer rows.Close()

	var upstreams []Upstream
	for rows.Next() {
		var u Upstream
		if err := rows.Scan(&u.ID, &u.Upstream, &u.Enabled); err != nil {
			return err
		}
		upstreams = append(upstreams, u)
	}

	upstreamStorage.upstreams.Store(upstreams)
	log.Printf("Loaded %d upstreams from database", len(upstreams))
	return nil
}

func (s *upstreamStore) getAll() []Upstream {
	return s.upstreams.Load().([]Upstream)
}

func (s *upstreamStore) add(upstream string, enabled bool) (Upstream, error) {
	result, err := db.Exec("INSERT INTO upstreams (upstream, enabled) VALUES (?, ?)", upstream, enabled)
	if err != nil {
		return Upstream{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return Upstream{}, err
	}

	u := Upstream{
		ID:       int(id),
		Upstream: upstream,
		Enabled:  enabled,
	}

	current := s.getAll()
	newList := make([]Upstream, len(current)+1)
	copy(newList, current)
	newList[len(current)] = u
	s.upstreams.Store(newList)

	return u, nil
}

func (s *upstreamStore) update(id int, upstream string) error {
	_, err := db.Exec("UPDATE upstreams SET upstream = ? WHERE id = ?", upstream, id)
	if err != nil {
		return err
	}

	current := s.getAll()
	newList := make([]Upstream, len(current))
	copy(newList, current)

	for i := range newList {
		if newList[i].ID == id {
			newList[i].Upstream = upstream
			s.upstreams.Store(newList)
			return nil
		}
	}

	return sql.ErrNoRows
}

func (s *upstreamStore) toggle(id int) error {
	current := s.getAll()
	var newEnabled bool
	found := false

	for _, u := range current {
		if u.ID == id {
			newEnabled = !u.Enabled
			found = true
			break
		}
	}

	if !found {
		return sql.ErrNoRows
	}

	_, err := db.Exec("UPDATE upstreams SET enabled = ? WHERE id = ?", newEnabled, id)
	if err != nil {
		return err
	}

	newList := make([]Upstream, len(current))
	copy(newList, current)

	for i := range newList {
		if newList[i].ID == id {
			newList[i].Enabled = newEnabled
			break
		}
	}
	s.upstreams.Store(newList)

	return nil
}

func (s *upstreamStore) delete(id int) error {
	_, err := db.Exec("DELETE FROM upstreams WHERE id = ?", id)
	if err != nil {
		return err
	}

	current := s.getAll()
	for i, u := range current {
		if u.ID == id {
			newList := make([]Upstream, len(current)-1)
			copy(newList, current[:i])
			copy(newList[i:], current[i+1:])
			s.upstreams.Store(newList)
			return nil
		}
	}

	return nil
}

func handleAPIUpstreams(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	switch r.Method {
	case "GET":
		if err := json.NewEncoder(w).Encode(upstreamStorage.getAll()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		
	case "POST":
		var req struct {
			Upstream string `json:"upstream"`
			Enabled  bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		upstream, err := upstreamStorage.add(req.Upstream, req.Enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(upstream)
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleAPIUpstreamAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	path := r.URL.Path[len("/api/upstreams/"):]
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
		if err := upstreamStorage.delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		
	case "POST":
		if action == "toggle" {
			if err := upstreamStorage.toggle(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			json.NewEncoder(w).Encode(map[string]bool{"success": true})
		} else {
			http.Error(w, "Invalid action", http.StatusBadRequest)
		}
		
	case "PUT":
		var req struct {
			Upstream string `json:"upstream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := upstreamStorage.update(id, req.Upstream); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleUpstreams(w http.ResponseWriter, r *http.Request) {
	var strategy string
	db.QueryRow("SELECT value FROM settings WHERE key = 'strategy'").Scan(&strategy)
	
	rows, _ := db.Query("SELECT server FROM bootstrap_servers ORDER BY id")
	var bootstrapServers []string
	for rows.Next() {
		var server string
		rows.Scan(&server)
		bootstrapServers = append(bootstrapServers, server)
	}
	rows.Close()
	
	data := map[string]interface{}{
		"Upstreams":        upstreamStorage.getAll(),
		"Strategy":         strategy,
		"BootstrapServers": bootstrapServers,
	}
	renderPage(w, "Upstreams", "upstreams", upstreamsHTML, data)
}
