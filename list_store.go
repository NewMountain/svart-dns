package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yeti/svart-dns/internal/listparse"
	"github.com/yeti/svart-dns/internal/telemetry"

	"github.com/yeti/svart-dns/internal/policycore"
)

type listStore struct {
	kind, lists, rules, idColumn string
	block                        bool
}

var (
	blockListStore = listStore{"blocklist", "blocklists", "blocked_domains", "blocklist_id", true}
	allowListStore = listStore{"allowlist", "allowlists", "allowed_domains", "allowlist_id", false}
)

func refreshListByID(store listStore, id int) (refreshErr error) {
	ctx, complete := telemetry.Begin(context.Background(), telemetry.ListDownload)
	defer func() { complete(refreshErr) }()
	_, finishRead := telemetry.Begin(ctx, telemetry.StorageRead)
	var url string
	err := readDB.QueryRow("SELECT url FROM "+store.lists+" WHERE id = ?", id).Scan(&url)
	finishRead(err)
	if err != nil {
		return err
	}
	if !store.block && url == "" {
		return nil
	}
	list, err := fetchList(ctx, url)
	if err != nil {
		return fmt.Errorf("refresh %s %d: %w", store.kind, id, err)
	}
	logger := logBlocklist
	if !store.block {
		logger = logAllowlist
	}
	logListRefreshRejections(logger, store.kind, id, list.Result)
	rows, count := listparse.StoredRows(list.Result, store.block)
	_, finishWrite := telemetry.Begin(ctx, telemetry.StorageWrite)
	report := &ListCompatibilityReport{Version: 1, AssessedAt: time.Now().UTC().Format(time.RFC3339Nano), Lines: list.Lines, Applied: count, Unsupported: list.Unsupported, Invalid: list.Invalid, Diagnostics: list.Diagnostics}
	if !list.typed {
		report = nil // The bridge preserves legacy semantics; typed compatibility is unassessed.
	}
	err = storeListGeneration(store, id, rows, count, url, report, list.typed)
	finishWrite(err)
	if err != nil {
		return fmt.Errorf("refresh %s %d: %w; the previous version stays active", store.kind, id, err)
	}
	telemetry.Logger(ctx).InfoContext(ctx, "refreshed list", "type", store.kind, "id", id, "domains", count)
	return reloadPolicyState(fmt.Sprintf("%s %d refreshed", store.kind, id), reloadListContent, policycore.ListKey{ID: id, Allow: !store.block})
}

// The write transaction serializes the old version, replacement and complete
// history. A reader therefore sees exactly one committed list generation.
func storeListGeneration(store listStore, id int, rules []string, count int, sourceURL string, report *ListCompatibilityReport, parsedTyped ...bool) error {
	reportJSON := ""
	if report != nil {
		encoded, err := json.Marshal(report)
		if err != nil {
			return fmt.Errorf("encode compatibility report: %w", err)
		}
		reportJSON = string(encoded)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	if err := validateListWriterGeneration(tx, rules, parsedTyped); err != nil {
		return err
	}
	var currentURL string
	if err := tx.QueryRow("SELECT url FROM "+store.lists+" WHERE id=?", id).Scan(&currentURL); err != nil {
		return fmt.Errorf("read current list source: %w", err)
	}
	if currentURL != sourceURL {
		return fmt.Errorf("list source changed during download; retry the refresh")
	}
	if store.block {
		if err := recordListHistory(tx, id, rules, count); err != nil {
			return err
		}
	}
	if err := replaceListRules(tx, store.lists, store.rules, store.idColumn, id, rules, count); err != nil {
		return err
	}
	// #nosec G202 G701 -- The listStore identifiers are private fixed blocklist/allowlist schema specifications; external values are bound.
	result, err := tx.Exec("INSERT INTO local_"+store.kind+"_generations(list_id,url,compatibility_report) VALUES(?,?,?) ON CONFLICT(list_id) DO UPDATE SET url=excluded.url,compatibility_report=excluded.compatibility_report", id, sourceURL, reportJSON)
	if err := checkListWrite(result, err, 1); err != nil {
		return fmt.Errorf("record local list generation: %w", err)
	}
	return tx.Commit()
}

func recordListHistory(tx *sql.Tx, id int, rules []string, count int) error {
	typed, err := typedListWriterEnabled(tx)
	if err != nil {
		return err
	}
	old := make(map[string]bool)
	if err := scanRows(tx, "SELECT domain FROM blocked_domains WHERE blocklist_id = ?", func(rows *sql.Rows) error {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return err
		}
		old[domain] = true
		return nil
	}, id); err != nil {
		return fmt.Errorf("read prior rules: %w", err)
	}
	previousCount := len(old)
	var priorHistory bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM blocklist_history WHERE blocklist_id = ?)", id).Scan(&priorHistory); err != nil {
		return fmt.Errorf("read prior history: %w", err)
	}
	var added, removed []string
	for _, domain := range rules {
		if !old[domain] {
			added = append(added, domain)
		} else {
			delete(old, domain)
		}
	}
	for domain := range old {
		removed = append(removed, domain)
	}
	slices.Sort(added)
	slices.Sort(removed)
	addedSample, err := historySample(added, typed)
	if err != nil {
		return err
	}
	removedSample, err := historySample(removed, typed)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT INTO blocklist_history (blocklist_id, previous_count, new_count, added_count, removed_count, sample_added, sample_removed)
 VALUES (?, ?, ?, ?, ?, ?, ?)`, id, previousCount, count, len(added), len(removed), addedSample, removedSample)
	if err := checkListWrite(result, err, 1); err != nil {
		return fmt.Errorf("store refresh history: %w", err)
	}
	historyID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("read refresh history ID: %w", err)
	}
	// There is no earlier checkpoint to reconstruct for the first population.
	// An empty later generation still needs its full diff for earlier checkpoints.
	if previousCount == 0 && !priorHistory {
		return nil
	}
	stmt, err := tx.Prepare("INSERT INTO blocklist_changelog (history_id, domain, action) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("prepare refresh changelog: %w", err)
	}
	defer closeReadResource(stmt)
	for _, change := range []struct {
		action  string
		domains []string
	}{{"added", added}, {"removed", removed}} {
		for _, domain := range change.domains {
			result, err := stmt.Exec(historyID, domain, change.action)
			if err := checkListWrite(result, err, 1); err != nil {
				return fmt.Errorf("store refresh changelog: %w", err)
			}
		}
	}
	return nil
}

// Samples are display metadata only; every change remains in the changelog.
func historySample(domains []string, typed bool) (string, error) {
	if !typed {
		// The frozen recovery reader splits comma text. Before activation all
		// stored rules use legacy syntax, so retain its exact sample format too.
		return strings.Join(domains[:min(len(domains), 10)], ","), nil
	}
	sample := make([]string, 0, min(len(domains), 10))
	for _, stored := range domains[:min(len(domains), 10)] {
		if strings.HasPrefix(stored, "!svart-rule-") {
			rule, err := listparse.DecodeStored(stored)
			if err != nil {
				return "", fmt.Errorf("read history rule: %w", err)
			}
			stored = rule.Text
		}
		sample = append(sample, stored)
	}
	encoded, err := json.Marshal(sample)
	if err != nil {
		return "", fmt.Errorf("encode history sample: %w", err)
	}
	return historySamplePrefix + string(encoded), nil
}

const historySamplePrefix = "!svart-sample-v1:"

func decodeHistorySample(sample string) ([]string, error) {
	if strings.HasPrefix(sample, historySamplePrefix) {
		var rules []string
		if err := json.Unmarshal([]byte(strings.TrimPrefix(sample, historySamplePrefix)), &rules); err != nil {
			return nil, fmt.Errorf("read history sample: %w", err)
		}
		return rules, nil
	}
	if strings.HasPrefix(sample, "!svart-sample-") {
		return nil, fmt.Errorf("unsupported history sample version")
	}
	return splitDomains(sample), nil
}

// SQLite triggers may suppress a mutation without returning an Exec error.
// Treat the effect as part of the write contract before trusting row IDs.
func checkListWrite(result sql.Result, err error, want int64) error {
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != want {
		return fmt.Errorf("changed %d rows, expected %d", affected, want)
	}
	return nil
}
