package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Verification opens an existing journal read-only and never initializes,
// repairs, expires, checkpoints or creates storage. Run against a stopped
// instance or a consistent backup so file retirement cannot race the scan.
func verifyRawArchives(journalPath, root string, out io.Writer) error {
	u := url.URL{Scheme: "file", Path: journalPath}
	source, err := sql.Open("sqlite3", u.String()+"?mode=ro&_query_only=1")
	if err != nil {
		return err
	}
	defer closeReadResource(source)
	var integrity string
	if err = source.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("journal integrity: %s", integrity)
	}
	var identity string
	if err = source.QueryRow("SELECT value FROM journal_meta WHERE key='identity'").Scan(&identity); err != nil {
		return err
	}
	if identity == "" || identity == "." || identity == ".." || filepath.Base(identity) != identity {
		return errors.New("invalid journal identity")
	}
	a := &rawArchiver{journal: &durableJournal{db: source, identity: identity}, dir: filepath.Join(root, "raw-v1", identity)}
	var archived, previous int64
	if err = a.verify(func(r rawArchiveRow) error {
		if r.Sequence <= previous {
			return errors.New("overlapping archive sequence identities")
		}
		previous = r.Sequence
		archived++
		return nil
	}); err != nil {
		return err
	}
	var originals, quarantined, pending, rewrites int64
	for query, target := range map[string]*int64{
		"SELECT COUNT(*) FROM journal_records":                         &originals,
		"SELECT COUNT(*) FROM journal_quarantine":                      &quarantined,
		"SELECT COUNT(*) FROM raw_archive_files WHERE state='pending'": &pending,
		"SELECT COUNT(*) FROM raw_archive_rewrites":                    &rewrites,
	} {
		if err = source.QueryRow(query).Scan(target); err != nil {
			return err
		}
	}
	// Read every original too; a malformed event is never reported as verified.
	var after int64
	for {
		rows, err := a.journal.batch(after, rawArchiveBatchRows)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			if _, err = rawTimestamp(r.payload); err != nil {
				return fmt.Errorf("raw journal sequence %d: %w", r.id, err)
			}
			after = r.id
		}
	}
	if err = walkRawArchiveFiles(a.dir, func(name string, _ os.FileInfo) error {
		if !strings.HasSuffix(name, ".parquet") {
			return fmt.Errorf("recoverable unfinished/unrecognized raw archive file %s; resume service or investigate preserved source", name)
		}
		var n int
		if err := source.QueryRow("SELECT COUNT(*) FROM raw_archive_files WHERE name=?", name).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("raw archive file %s has no committed ownership manifest; preserved for recovery", name)
		}
		return nil
	}); err != nil {
		return err
	}
	if pending != 0 || rewrites != 0 {
		return fmt.Errorf("raw archive recovery incomplete: pending=%d rewrites=%d; originals preserved, resume service before verifying", pending, rewrites)
	}
	_, err = fmt.Fprintf(out, "verified journal=%s archived_records=%d journal_records=%d quarantine_records=%d\n", identity, archived, originals, quarantined)
	return err
}
