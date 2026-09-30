package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func expireSummaryFiles(dir string, days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	var store *sql.DB
	if dir == archivePath && db != nil {
		var err error
		store, err = openSummaryStore()
		if err != nil {
			return err
		}
		defer closeReadResource(store)
		for {
			var name, state string
			err := store.QueryRow("SELECT name,state FROM summary_archive_files WHERE state='expiring' OR (state='ready' AND day<?) ORDER BY day LIMIT 1", cutoff.Format("2006-01-02")).Scan(&name, &state)
			if errors.Is(err, sql.ErrNoRows) {
				break
			}
			if err != nil {
				return err
			}
			path, err := summaryArchiveFilePath(dir, name)
			if err != nil {
				return err
			}
			if state == "ready" {
				if err := rawArchiveChange(store, "UPDATE summary_archive_files SET state='expiring' WHERE name=? AND state='ready'", name); err != nil {
					return err
				}
			}
			if err := summaryStep("expire-intent"); err != nil {
				return err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := summaryStep("expire-unlinked"); err != nil {
				return err
			}
			if err := syncDirectory(dir); err != nil {
				return err
			}
			if err := rawArchiveChange(store, "UPDATE summary_archive_files SET state='expired' WHERE name=? AND state='expiring'", name); err != nil {
				return err
			}
			if err := summaryStep("expired"); err != nil {
				return err
			}
			// The sidecar contains presentation identities; it follows the same explicit
			// retention policy after the sole archive owner's retirement is durable.
			if err := os.Remove(path + ".sources"); err != nil && !os.IsNotExist(err) {
				return err
			}
			if err := syncDirectory(dir); err != nil {
				return err
			}
		}
		// Resume removal of derived identity sidecars after a crash immediately
		// following the durable expired transition.
		rows, err := store.Query("SELECT name FROM summary_archive_files WHERE state='expired'")
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				closeQueryRows(rows)
				return err
			}
			path, err := summaryArchiveFilePath(dir, name)
			if err != nil {
				closeQueryRows(rows)
				return err
			}
			if err := os.Remove(path + ".sources"); err != nil && !os.IsNotExist(err) {
				closeQueryRows(rows)
				return err
			}
		}
		if err := rows.Err(); err != nil {
			closeQueryRows(rows)
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		// #nosec G304 G703 -- Directory is operator selected; retained filenames here come directly from os.ReadDir, never catalog or request text.
		if _, err := os.Stat(dir); err == nil {
			if err := syncDirectory(dir); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".parquet") {
			continue
		}
		if store != nil {
			var known bool
			if err := store.QueryRow("SELECT EXISTS(SELECT 1 FROM summary_archive_files WHERE name=?)", entry.Name()).Scan(&known); err != nil {
				return err
			}
			if known {
				continue
			}
		}
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "svart-dns-"), ".parquet"))
		if err != nil || !day.Before(cutoff) {
			continue
		}
		// #nosec G304 G703 -- Directory is operator selected; retained filenames here come directly from os.ReadDir, never catalog or request text.
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
		if err := syncDirectory(dir); err != nil {
			return err
		}
	}
	return nil
}
