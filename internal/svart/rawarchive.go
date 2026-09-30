package svart

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/parquet-go/parquet-go"
)

// Raw archives deliberately live below the summary glob and preserve opaque
// payload bytes, including unknown JSON fields and legacy trailing newlines.
type rawArchiveRow struct {
	Journal     string `parquet:"journal,snappy,dict"`
	Sequence    int64  `parquet:"sequence"`
	TimestampNS int64  `parquet:"timestamp_ns"`
	Payload     []byte `parquet:"payload,snappy"`
}

const rawArchiveBatchRows = 512
const rawArchiveBatchBytes = 4 << 20

const rawArchiveSchema = `CREATE TABLE IF NOT EXISTS raw_archive_files (
 name TEXT PRIMARY KEY, first_id INTEGER NOT NULL, last_id INTEGER NOT NULL,
 count INTEGER NOT NULL, digest TEXT NOT NULL, state TEXT NOT NULL, min_ns INTEGER NOT NULL, max_ns INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS raw_archive_rewrites(parent TEXT PRIMARY KEY, cutoff_ns INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS raw_archive_sequence ON raw_archive_files(state,first_id);
 CREATE INDEX IF NOT EXISTS raw_archive_state_name ON raw_archive_files(state,name);
 CREATE INDEX IF NOT EXISTS raw_archive_expiry ON raw_archive_files(state,min_ns);
 CREATE UNIQUE INDEX IF NOT EXISTS raw_archive_one_pending ON raw_archive_files(state) WHERE state='pending';`

type rawArchiveManifest struct {
	name          string
	first, last   int64
	count         int
	digest, state string
	minNS, maxNS  int64
}
type rawArchiver struct {
	journal *durableJournal
	source  *sql.DB
	dir     string
	// Fault injection at actual persistence boundaries; nil in production.
	deferSmall bool
	checkpoint func(string) error
	createTemp func(string) (*os.File, error)
	stop       <-chan struct{}
}

func newRawArchiver(j *durableJournal, source *sql.DB, root string) (*rawArchiver, error) {
	if j.identity == "" || (j.identity == "." || j.identity == "..") || filepath.Base(j.identity) != j.identity {
		return nil, errors.New("invalid raw journal identity")
	}
	if _, err := j.db.Exec(rawArchiveSchema); err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "raw-v1", j.identity)
	// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	// Persist each newly created directory entry before retiring its source.
	for _, p := range []string{dir, filepath.Dir(dir), root, filepath.Dir(root)} {
		if err := syncDirectory(p); err != nil {
			return nil, err
		}
	}
	return &rawArchiver{journal: j, source: source, dir: dir}, nil
}

type rawArchiveExecutor interface {
	Exec(string, ...any) (sql.Result, error)
}

func rawArchiveChange(executor rawArchiveExecutor, query string, args ...any) error {
	result, err := executor.Exec(query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("raw archive ownership mutation affected %d rows, expected 1; transaction must not retire source", n)
	}
	return nil
}
func (a *rawArchiver) step(stage string) error {
	if a.checkpoint != nil {
		return a.checkpoint(stage)
	}
	return nil
}
func rawTimestamp(payload []byte) (int64, error) {
	var header struct {
		TS *int64 `json:"ts"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return 0, err
	}
	if header.TS == nil {
		return 0, errors.New("raw event has no integer ts; preserve journal and repair source")
	}
	return *header.TS, nil
}
func validRawArchiveName(name string) bool {
	return filepath.IsLocal(name) && filepath.Clean(name) == name && filepath.Ext(name) == ".parquet"
}
func scanRawManifest(scanner interface{ Scan(...any) error }) (rawArchiveManifest, error) {
	var m rawArchiveManifest
	err := scanner.Scan(&m.name, &m.first, &m.last, &m.count, &m.digest, &m.state, &m.minNS, &m.maxNS)
	if err == nil && (!validRawArchiveName(m.name) || m.count < 1 || m.count > rawArchiveSegmentRows || m.first < 1 || m.last < m.first) {
		err = errors.New("invalid raw archive manifest; preserve sources and repair manifest")
	}
	return m, err
}
func (a *rawArchiver) nextManifest(after string, state string) (rawArchiveManifest, error) {
	return scanRawManifest(a.journal.db.QueryRow("SELECT name,first_id,last_id,count,digest,state,min_ns,max_ns FROM raw_archive_files WHERE name>? AND state=? ORDER BY name LIMIT 1", after, state))
}
func (a *rawArchiver) originalRows(first, last int64) ([]rawArchiveRow, error) {
	rs, err := a.journal.db.Query("SELECT id,payload FROM journal_records WHERE id>=? AND id<=? ORDER BY id LIMIT ?", first, last, rawArchiveBatchRows)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rs)
	var rows []rawArchiveRow
	size := 0
	for rs.Next() {
		r := rawArchiveRow{Journal: a.journal.identity}
		if err = rs.Scan(&r.Sequence, &r.Payload); err != nil {
			return nil, err
		}
		r.TimestampNS, err = rawTimestamp(r.Payload)
		if err != nil {
			return nil, fmt.Errorf("raw sequence %d: %w", r.Sequence, err)
		}
		rows = append(rows, r)
		size += len(r.Payload)
		if size >= rawArchiveBatchBytes {
			break
		}
	}
	return rows, rs.Err()
}
func (a *rawArchiver) plan(processed int64, now time.Time) (result rawArchiveManifest, resultErr error) {
	m, err := a.nextManifest("", "pending")
	if err == nil {
		return m, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return m, err
	}
	// Low-volume traffic flushes hourly; high-volume traffic uses full segments.
	// Tests/explicit one-shot callers default to immediate flushing.
	if a.deferSmall {
		var lastFlush int64
		err = a.journal.db.QueryRow("SELECT value FROM journal_meta WHERE key='raw_archive_flush_ns'").Scan(&lastFlush)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return m, err
		}
		if now.Before(time.Unix(0, lastFlush).Add(time.Hour)) {
			var id int64
			err = a.journal.db.QueryRow("SELECT id FROM journal_records WHERE id<=? ORDER BY id LIMIT 1 OFFSET ?", processed, rawArchiveSegmentRows-1).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				return m, sql.ErrNoRows
			}
			if err != nil {
				return m, err
			}
		}
	}
	var digest rawArchiveDigest
	input, err := a.journalSource(1, processed)()
	if err != nil {
		return m, err
	}
	defer func() { resultErr = errors.Join(resultErr, input.close()) }()
	for digest.count < rawArchiveSegmentRows && digest.bytes < rawArchiveSegmentBytes {
		rows, e := input.next()
		if e != nil && !errors.Is(e, io.EOF) {
			return m, e
		}
		for _, row := range rows {
			digest.add([]rawArchiveRow{row})
			if digest.count >= rawArchiveSegmentRows || digest.bytes >= rawArchiveSegmentBytes {
				break
			}
		}
		if errors.Is(e, io.EOF) {
			break
		}
	}
	if digest.count == 0 {
		return m, sql.ErrNoRows
	}
	m = digest.manifest(now.UTC().Format("2006/01/02"))
	err = rawArchiveChange(a.journal.db, "INSERT INTO raw_archive_files VALUES(?,?,?,?,?,?,?,?)", m.name, m.first, m.last, m.count, m.digest, m.state, m.minNS, m.maxNS)
	if err != nil {
		return m, err
	}
	return m, a.step("planned")
}
func equalRawRows(want, got []rawArchiveRow) error {
	if len(want) != len(got) {
		return errors.New("raw archive equality count mismatch")
	}
	for i, r := range want {
		g := got[i]
		if r.Journal != g.Journal || r.Sequence != g.Sequence || r.TimestampNS != g.TimestampNS || !bytes.Equal(r.Payload, g.Payload) {
			return fmt.Errorf("raw archive equality failed at sequence %d", r.Sequence)
		}
	}
	return nil
}
func (a *rawArchiver) publish(m rawArchiveManifest, source rawArchiveSource) error {
	digest, err := digestRawArchive(source)
	if err != nil {
		return err
	}
	if err = digest.verify(m); err != nil {
		return err
	}
	parent := filepath.Dir(filepath.Join(a.dir, m.name))
	// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	for p := parent; p != a.dir; p = filepath.Dir(p) {
		if err = syncDirectory(p); err != nil {
			return err
		}
	}
	if err = syncDirectory(a.dir); err != nil {
		return err
	}
	path := filepath.Join(a.dir, m.name)
	// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
	if _, err := os.Stat(path); err == nil {
		if err = equalRawArchiveSources(source, a.fileSource(m)); err != nil {
			return err
		}
		// A prior process may have died immediately after rename, before directory sync.
		if err = syncDirectory(parent); err != nil {
			return err
		}
		return a.step("verified")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Complete originals have already been checked against the durable manifest.
	// An interrupted derived temp is replaceable; it is never the sole owner.
	temp := path + ".partial"
	// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
	if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	create := a.createTemp
	if create == nil {
		// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
		create = func(path string) (*os.File, error) { return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600) }
	}
	f, err := create(temp)
	if err != nil {
		return err
	}
	writer := parquet.NewGenericWriter[rawArchiveRow](f, parquet.Compression(&parquet.Snappy), parquet.MaxRowsPerRowGroup(rawArchiveBatchRows))
	err = walkRawArchive(source, func(rows []rawArchiveRow) error {
		_, e := writer.Write(rows)
		if e != nil {
			return e
		}
		return writer.Flush()
	})
	if err == nil {
		err = writer.Close()
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = a.step("temp-synced"); err != nil {
		return err
	}
	// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
	if err = os.Rename(temp, path); err != nil {
		return err
	}
	if err = a.step("renamed"); err != nil {
		return err
	}
	if err = syncDirectory(parent); err != nil {
		return err
	}
	if err = a.step("published"); err != nil {
		return err
	}
	if err = equalRawArchiveSources(source, a.fileSource(m)); err != nil {
		return err
	}
	return a.step("verified")
}
func (a *rawArchiver) retire(m rawArchiveManifest, processed int64, now time.Time) error {
	if m.last > processed {
		return errors.New("raw archive awaits durable presentation cursor")
	}
	if err := a.step("before-retire"); err != nil {
		return err
	}
	tx, err := a.journal.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	// Source rows are immutable; a bounded segment range was fully compared
	// immediately before this transaction. The single range delete is atomic.
	result, err := tx.Exec("DELETE FROM journal_records WHERE id>=? AND id<=?", m.first, m.last)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != int64(m.count) {
		return errors.New("raw source count changed during retirement; rollback")
	}
	if err = a.step("retire-deleted"); err != nil {
		return err
	}
	if err = rawArchiveChange(tx, "UPDATE raw_archive_files SET state='ready' WHERE name=? AND state='pending'", m.name); err != nil {
		return err
	}
	if err = rawArchiveChange(tx, "INSERT INTO journal_meta(key,value) VALUES('raw_archived_through',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", m.last); err != nil {
		return err
	}
	if err = rawArchiveChange(tx, "INSERT INTO journal_meta(key,value) VALUES('raw_archive_flush_ns',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", now.UnixNano()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return a.step("retired")
}
func (a *rawArchiver) cycle(now time.Time) error {
	days, err := readLogRetentionDays(a.source)
	if err != nil {
		return err
	}
	var processed int64
	err = a.source.QueryRow("SELECT sequence FROM query_journal_progress WHERE source=?", a.journal.identity).Scan(&processed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	// Snapshot the cursor. New events are handled next cycle, so continuous DNS
	// traffic cannot make this cycle infinite or postpone shutdown indefinitely.
	for {
		if a.stopping() {
			return nil
		}
		m, err := a.plan(processed, now)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if m.last > processed {
			return errors.New("pending raw archive exceeds presentation cursor; retain source")
		}
		if err = a.publish(m, a.journalSource(m.first, m.last)); err != nil {
			return err
		}
		if err = a.retire(m, processed, now); err != nil {
			return err
		}
	}
	return a.expire(now.UTC().AddDate(0, 0, -days))
}
func rawCutoffNS(cutoff time.Time) int64 {
	if cutoff.Before(time.Unix(0, math.MinInt64)) {
		return math.MinInt64
	}
	if cutoff.After(time.Unix(0, math.MaxInt64)) {
		return math.MaxInt64
	}
	return cutoff.UnixNano()
}
func (a *rawArchiver) expire(cutoff time.Time) error {
	threshold := rawCutoffNS(cutoff)
	for {
		if a.stopping() {
			return nil
		}
		// A durable rewrite intent names its source and original expiry policy.
		// Restart completes the same replacement even if the clock advances.
		var parent string
		var plannedCutoff int64
		err := a.journal.db.QueryRow("SELECT parent,cutoff_ns FROM raw_archive_rewrites ORDER BY parent LIMIT 1").Scan(&parent, &plannedCutoff)
		if errors.Is(err, sql.ErrNoRows) {
			m, e := scanRawManifest(a.journal.db.QueryRow("SELECT name,first_id,last_id,count,digest,state,min_ns,max_ns FROM raw_archive_files WHERE state='ready' AND min_ns<? ORDER BY min_ns LIMIT 1", threshold))
			if errors.Is(e, sql.ErrNoRows) {
				break
			}
			if e != nil {
				return e
			}
			if e = rawArchiveChange(a.journal.db, "INSERT INTO raw_archive_rewrites VALUES(?,?)", m.name, threshold); e != nil {
				return e
			}
			parent = m.name
			plannedCutoff = threshold
		} else if err != nil {
			return err
		}
		m, err := scanRawManifest(a.journal.db.QueryRow("SELECT name,first_id,last_id,count,digest,state,min_ns,max_ns FROM raw_archive_files WHERE name=? AND state='ready'", parent))
		if err != nil {
			return err
		}
		kept := filterRawArchive(a.fileSource(m), plannedCutoff)
		digest, err := digestRawArchive(kept)
		if err != nil {
			return err
		}
		var replacement rawArchiveManifest
		if digest.count > 0 {
			replacement = digest.manifest(filepath.Dir(m.name))
			if err = a.publish(replacement, kept); err != nil {
				return err
			}
		}
		tx, err := a.journal.db.Begin()
		if err != nil {
			return err
		}
		if digest.count > 0 {
			err = rawArchiveChange(tx, "INSERT INTO raw_archive_files VALUES(?,?,?,?,?,'ready',?,?)", replacement.name, replacement.first, replacement.last, replacement.count, replacement.digest, replacement.minNS, replacement.maxNS)
		}
		if err == nil {
			err = rawArchiveChange(tx, "UPDATE raw_archive_files SET state='obsolete' WHERE name=? AND state='ready'", m.name)
		}
		if err == nil {
			err = rawArchiveChange(tx, "DELETE FROM raw_archive_rewrites WHERE parent=?", parent)
		}
		if err == nil {
			err = a.step("before-expire-commit")
		}
		if err != nil {
			rollbackTransaction(tx)
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if err = a.step("expired"); err != nil {
			return err
		}
	}
	// Obsolete means every retained event has a verified published replacement;
	// expired events were explicitly authorized by a valid retention snapshot.
	for {
		m, err := a.nextManifest("", "obsolete")
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
		if err = os.Remove(filepath.Join(a.dir, m.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err = syncDirectory(filepath.Dir(filepath.Join(a.dir, m.name))); err != nil {
			return err
		}
		if err = a.step("obsolete-removed"); err != nil {
			return err
		}
		if err = rawArchiveChange(a.journal.db, "DELETE FROM raw_archive_files WHERE name=? AND state='obsolete'", m.name); err != nil {
			return err
		}
	}
}

// verify streams one bounded segment at a time, checking all identities, ns
// timestamps, opaque payload bytes and complete manifest digests.
func (a *rawArchiver) verify(visit func(rawArchiveRow) error) error {
	var after int64
	var afterName string
	for {
		m, err := scanRawManifest(a.journal.db.QueryRow("SELECT name,first_id,last_id,count,digest,state,min_ns,max_ns FROM raw_archive_files WHERE state='ready' AND (first_id>? OR (first_id=? AND name>?)) ORDER BY first_id,name LIMIT 1", after, after, afterName))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		after = m.first
		afterName = m.name
		if err = walkRawArchive(a.fileSource(m), func(rows []rawArchiveRow) error {
			for _, r := range rows {
				if e := visit(r); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
}
