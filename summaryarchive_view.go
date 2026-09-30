package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// The caller serializes the complete query with summary publication/retention.
// A separate read-only connection also works in the isolated query subprocess.
func checkSummaryAvailability(ctx context.Context, databasePath, archiveDir string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: databasePath}).String()
	source, err := sql.Open("sqlite3", uri+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer closeReadResource(source)
	var catalog bool
	if err := source.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='summary_archive_files')").Scan(&catalog); err != nil {
		return err
	}
	if catalog {
		if err := checkExpectedSummaryFiles(ctx, source, archiveDir); err != nil {
			return err
		}
	}

	entries, err := os.ReadDir(archiveDir)
	if os.IsNotExist(err) {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".parquet") {
			continue
		}
		var known bool
		if catalog {
			if err := source.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM summary_archive_files WHERE name=?)", name).Scan(&known); err != nil {
				return err
			}
		}
		if known {
			continue
		}
		day, err := time.Parse("2006-01-02", strings.TrimSuffix(strings.TrimPrefix(name, "svart-dns-"), ".parquet"))
		if err != nil {
			continue
		}
		var overlap bool
		if err := source.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM query_logs WHERE timestamp>=? AND timestamp<?)", day.Format("2006-01-02 15:04:05"), day.AddDate(0, 0, 1).Format("2006-01-02 15:04:05")).Scan(&overlap); err != nil {
			return err
		}
		if overlap {
			return fmt.Errorf("legacy summary overlap needs exact source reconciliation: %s", name)
		}
	}
	return ctx.Err()
}

func checkExpectedSummaryFiles(ctx context.Context, source *sql.DB, archiveDir string) error {
	rows, err := source.QueryContext(ctx, "SELECT name,state,digest FROM summary_archive_files")
	if err != nil {
		return err
	}
	defer closeQueryRows(rows)
	for rows.Next() {
		var name, state, digest string
		if err := rows.Scan(&name, &state, &digest); err != nil {
			return err
		}
		path, err := summaryArchiveFilePath(archiveDir, name)
		if err != nil {
			return err
		}
		if state == "expired" {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return err
			}
			return fmt.Errorf("expired summary unexpectedly reappeared: %s", name)
		}
		if state != "ready" {
			return fmt.Errorf("summary ownership transition %s requires recovery", state)
		}
		if err := verifySummaryFileContext(ctx, path, digest); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	return ctx.Err()
}

func summaryPending(source *sql.DB) (bool, error) {
	var exists bool
	if err := source.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name='summary_archive_files')").Scan(&exists); err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	var pending bool
	err := source.QueryRow("SELECT EXISTS(SELECT 1 FROM summary_archive_files WHERE state='pending')").Scan(&pending)
	return pending, err
}
