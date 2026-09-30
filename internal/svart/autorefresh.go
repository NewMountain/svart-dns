package svart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/yeti/svart-dns/internal/telemetry"
	"math"
	"sync"
	"time"
)

var (
	autoRefreshDone   chan struct{}
	autoRefreshStop   chan struct{}
	autoRefreshOnce   sync.Once
	autoRefreshTicker *time.Ticker
)

// startAutoRefreshWorker launches the background goroutine that checks for stale lists every 60s.
func startAutoRefreshWorker() {
	autoRefreshDone = make(chan struct{})
	autoRefreshStop = make(chan struct{})
	autoRefreshTicker = time.NewTicker(60 * time.Second)

	go func() {
		defer close(autoRefreshDone)
		retries := make(refreshRetries)
		for {
			select {
			case <-autoRefreshStop:
				return
			case <-autoRefreshTicker.C:
				runAutoRefreshCycle(retries, time.Now)
			}
		}
	}()
}

// closeAutoRefresh stops the auto-refresh worker gracefully.
func closeAutoRefresh() {
	autoRefreshOnce.Do(func() {
		if autoRefreshTicker != nil {
			autoRefreshTicker.Stop()
		}
		if autoRefreshStop != nil {
			close(autoRefreshStop)
		}
		if autoRefreshDone != nil {
			<-autoRefreshDone
		}
	})
}

// staleList carries the source identity so changed configuration or a manual
// refresh resets automatic retries without coupling the manual refresh path.
type staleList struct {
	ID          int
	Alias       string
	Type        string
	URL         string
	LastUpdated string
}

func findStaleLists() ([]staleList, error) {
	return readRefreshLists(`SELECT id, alias, 'blocklist', url, coalesce(last_updated,'') FROM blocklists
 WHERE refresh_interval > 0 AND enabled = 1 AND url != ''
 AND (last_updated IS NULL OR (strftime('%s','now') - strftime('%s', last_updated)) >= refresh_interval)
 UNION ALL
 SELECT id, alias, 'allowlist', url, coalesce(last_updated,'') FROM allowlists
 WHERE refresh_interval > 0 AND enabled = 1 AND url != ''
 AND (last_updated IS NULL OR (strftime('%s','now') - strftime('%s', last_updated)) >= refresh_interval)`)
}

// Newly synchronized metadata needs a local download even when its remote
// timestamp is recent or its regular refresh interval is disabled.
func findEmptyLists() ([]staleList, error) {
	return readRefreshLists(`SELECT id, alias, 'blocklist', url, coalesce(last_updated,'') FROM blocklists b
 WHERE enabled=1 AND url!='' AND NOT EXISTS(SELECT 1 FROM blocked_domains WHERE blocklist_id=b.id)
 AND NOT EXISTS(SELECT 1 FROM local_blocklist_generations WHERE list_id=b.id AND url=b.url)
 UNION ALL
 SELECT id, alias, 'allowlist', url, coalesce(last_updated,'') FROM allowlists a
 WHERE enabled=1 AND url!='' AND NOT EXISTS(SELECT 1 FROM allowed_domains WHERE allowlist_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM local_allowlist_generations WHERE list_id=a.id AND url=a.url)`)
}

func readRefreshLists(query string) ([]staleList, error) {
	if readDB == nil {
		return nil, errors.New("list discovery database is not open")
	}
	lists := []staleList{}
	err := scanRows(readDB, query, func(rows *sql.Rows) error {
		var list staleList
		if err := rows.Scan(&list.ID, &list.Alias, &list.Type, &list.URL, &list.LastUpdated); err != nil {
			return err
		}
		lists = append(lists, list)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list discovery unavailable: %w", err)
	}
	return lists, nil
}

func countStaleLists() float64 {
	lists, err := findStaleLists()
	if err != nil {
		logBlocklist.Error("stale-list metric unavailable", "error", err)
		return math.NaN()
	}
	return float64(len(lists))
}

type refreshKey struct {
	kind string
	id   int
}
type refreshRetry struct {
	source staleList
	delay  time.Duration
	next   time.Time
}
type refreshRetries map[refreshKey]refreshRetry

func (retries refreshRetries) ready(list staleList, now time.Time) bool {
	retry, found := retries[refreshKey{list.Type, list.ID}]
	return !found || retry.source.URL != list.URL || retry.source.LastUpdated != list.LastUpdated || !now.Before(retry.next)
}

func (retries refreshRetries) record(list staleList, now time.Time, err error) {
	key := refreshKey{list.Type, list.ID}
	if err == nil {
		delete(retries, key)
		return
	}
	retry := retries[key]
	if retry.source.URL != list.URL || retry.source.LastUpdated != list.LastUpdated {
		retry = refreshRetry{}
	}
	delay := retry.delay * 2
	if delay == 0 {
		delay = time.Minute
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	retries[key] = refreshRetry{source: list, delay: delay, next: now.Add(delay)}
}

func runAutoRefreshCycle(retries refreshRetries, now func() time.Time) {
	ctx, complete := telemetry.Begin(context.Background(), telemetry.AutoRefresh)
	var outcome error
	defer func() { complete(outcome) }()
	_, finishStorage := telemetry.Begin(ctx, telemetry.StorageRead)
	stale, err := findStaleLists()
	finishStorage(err)
	if err != nil {
		outcome = err
		logBlocklist.Error("auto-refresh discovery failed", "error", err)
		return
	}
	empty, err := findEmptyLists()
	if err != nil {
		outcome = err
		logBlocklist.Error("auto-refresh discovery failed", "error", err)
		return
	}
	seen := make(map[refreshKey]bool, len(stale)+len(empty))
	lists := make([]staleList, 0, len(stale)+len(empty))
	for _, candidates := range [][]staleList{stale, empty} {
		for _, list := range candidates {
			key := refreshKey{list.Type, list.ID}
			if !seen[key] {
				lists = append(lists, list)
				seen[key] = true
			}
		}
	}
	for key := range retries {
		if !seen[key] {
			delete(retries, key)
		}
	}
	for _, list := range lists {
		if !retries.ready(list, now()) {
			continue
		}
		var err error
		switch list.Type {
		case "blocklist":
			err = refreshBlocklistByID(list.ID)
		case "allowlist":
			err = refreshAllowlistByID(list.ID)
		}
		retries.record(list, now(), err)
		if err != nil {
			outcome = err
			logBlocklist.Error("auto-refresh failed", "type", list.Type, "id", list.ID, "alias", list.Alias, "error", err)
			autoRefreshErrors.WithLabelValues(list.Type, list.Alias).Inc()
		} else {
			logBlocklist.Info("auto-refresh complete", "type", list.Type, "id", list.ID, "alias", list.Alias)
			autoRefreshSuccesses.WithLabelValues(list.Type, list.Alias).Inc()
		}
	}
}
