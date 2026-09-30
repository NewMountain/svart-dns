package svart

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
)

// This additive catalog is committed before publication. Files without catalog
// provenance are legacy originals and are never overwritten or blindly retired.
const summaryArchiveSchema = `CREATE TABLE IF NOT EXISTS summary_archive_files (
 name TEXT PRIMARY KEY, day TEXT NOT NULL, count INTEGER NOT NULL,
 digest TEXT NOT NULL, sources_digest TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','ready','expiring','expired')));
 CREATE INDEX IF NOT EXISTS summary_archive_state_day ON summary_archive_files(state,day)`
const summarySegmentRows = 65536
const summarySegmentBytes = 64 << 20
const summaryRowGroupRows = 512

// A test hook at real persistence boundaries; production leaves it nil.
var summaryArchiveMu sync.Mutex
var summaryArchiveCheckpoint func(string) error

func summaryStep(stage string) error {
	if summaryArchiveCheckpoint != nil {
		return summaryArchiveCheckpoint(stage)
	}
	return nil
}
func summaryStopping() error {
	select {
	case <-archiveStop:
		return errors.New("summary archive stopped; source retained")
	default:
		return nil
	}
}

// Capture the worker stop channel for long integrity reads without creating a
// goroutine or tying HTTP availability to the archiver's lifetime.
type summaryReadContext struct{ stop <-chan struct{} }

func (c summaryReadContext) Done() <-chan struct{}       { return c.stop }
func (c summaryReadContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c summaryReadContext) Value(any) any               { return nil }
func (c summaryReadContext) Err() error {
	select {
	case <-c.stop:
		return context.Canceled
	default:
		return nil
	}
}

type summarySource struct {
	ID                  int64
	Timestamp, Identity string
	Row                 QueryLogRow
}
type summaryManifest struct {
	Name, Day                    string
	Count                        int64
	Digest, SourcesDigest, State string
}

func summaryIdentitySQL() string {
	cols := []string{"id", "CAST(timestamp AS TEXT)"}
	t := reflect.TypeOf(QueryLogRow{})
	for i := 1; i < t.NumField(); i++ {
		cols = append(cols, strings.Split(t.Field(i).Tag.Get("parquet"), ",")[0])
	}
	return "json_array(" + strings.Join(cols, ",") + ")"
}

func summaryFileDigest(path string) (string, error) {
	return summaryFileDigestContext(context.Background(), path)
}

const summaryIntegrityBufferBytes = 64 << 10
const summaryMetadataReadTimeout = 30 * time.Second

// Read all bytes on every verification. Metadata is only a concurrent-change
// guard, never proof of content or a cache key authorizing an existing file.
func summaryContentDigest(ctx context.Context, source io.Reader) (string, error) {
	hash := sha256.New()
	buffer := make([]byte, summaryIntegrityBufferBytes)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := source.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return "", err
			}
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func summaryFileDigestContext(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	before, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() {
		return "", fmt.Errorf("expected summary file is not regular: %s", path)
	}
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer closeReadResource(f)
	digest, err := summaryContentDigest(ctx, f)
	if err != nil {
		return "", err
	}
	after, err := f.Stat()
	if err != nil {
		return "", err
	}
	named, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !os.SameFile(before, after) || !os.SameFile(after, named) || before.Size() != after.Size() || after.Size() != named.Size() || !before.ModTime().Equal(after.ModTime()) || !after.ModTime().Equal(named.ModTime()) {
		return "", fmt.Errorf("summary file changed during integrity read: %s", path)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return digest, nil
}
func finishSummaryFile(f *os.File) error {
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}
func verifySummaryFile(path, digest string) error {
	return verifySummaryFileContext(context.Background(), path, digest)
}

func verifySummaryFileContext(ctx context.Context, path, digest string) error {
	actual, err := summaryFileDigestContext(ctx, path)
	if err != nil {
		return err
	}
	if actual != digest {
		return fmt.Errorf("summary archive content mismatch: %s", path)
	}
	return nil
}

func archiveSummarySegment(store *sql.DB, day time.Time, start, end string, upper int64) (int64, error) {
	if err := lockInvestigation(context.Background(), archiveStop); err != nil {
		return 0, err
	}
	defer investigateMu.Unlock()
	if err := summaryStopping(); err != nil {
		return 0, err
	}
	name := archiveDayFileName(day)
	var known int
	if err := store.QueryRow("SELECT COUNT(*) FROM summary_archive_files WHERE name=?", name).Scan(&known); err != nil {
		return 0, err
	}
	if _, err := os.Stat(filepath.Join(archivePath, name)); err == nil {
		if known == 0 {
			return 0, errors.New("legacy summary overlap has no source ownership proof; retain hot rows and reconcile originals")
		}
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	if known > 0 {
		name = strings.TrimSuffix(name, ".parquet") + "-" + journalToken() + ".parquet"
	}
	rows, err := readDB.Query(fmt.Sprintf(summarySelect, summaryIdentitySQL())+` FROM query_logs WHERE timestamp>=? AND timestamp<? AND id<=? ORDER BY id LIMIT ?`, start, end, upper, summarySegmentRows)
	if err != nil {
		return 0, err
	}
	defer closeQueryRows(rows)
	if err := os.MkdirAll(archivePath, 0750); err != nil {
		return 0, err
	}
	if err := syncDirectory(filepath.Dir(archivePath)); err != nil {
		return 0, err
	}
	path := filepath.Join(archivePath, name)
	// Unique staging files cannot overwrite an earlier recoverable intent.
	for _, suffix := range []string{".pending", ".sources"} {
		orphan := path + suffix
		if _, err := os.Stat(orphan); err == nil {
			if err := os.Rename(orphan, orphan+".orphan-"+journalToken()); err != nil {
				return 0, err
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
	}
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	f, err := os.OpenFile(path+".pending", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer closeReadResource(f)
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	sourceFile, err := os.OpenFile(path+".sources", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, err
	}
	defer closeReadResource(sourceFile)
	// Before a durable intent exists, unsuccessful staging is disposable because
	// every original still belongs to SQLite. Never remove registered sources.
	registered := false
	defer func() {
		if !registered {
			if err := os.Remove(path + ".pending"); err != nil && !errors.Is(err, os.ErrNotExist) {
				logDB.Error("remove unregistered summary staging file", "error", err)
			}
			if err := os.Remove(path + ".sources"); err != nil && !errors.Is(err, os.ErrNotExist) {
				logDB.Error("remove unregistered summary staging file", "error", err)
			}
		}
	}()
	writer := parquet.NewGenericWriter[QueryLogRow](f, parquet.Compression(&parquet.Snappy), parquet.MaxRowsPerRowGroup(summaryRowGroupRows))
	enc := json.NewEncoder(sourceFile)
	var count, sourceBytes int64
	for rows.Next() {
		if err := summaryStopping(); err != nil {
			return 0, err
		}
		var row QueryLogRow
		var source summarySource
		if err := scanSummarySource(rows, &source, &row); err != nil {
			return 0, err
		}
		ts, err := parseTimestamp(source.Timestamp)
		if err != nil {
			return 0, err
		}
		row.Timestamp = ts.UnixMilli()
		source.Row = row
		n, err := writer.Write([]QueryLogRow{row})
		if err != nil {
			return 0, err
		}
		if n != 1 {
			return 0, io.ErrShortWrite
		}
		if err := enc.Encode(source); err != nil {
			return 0, err
		}
		count++
		sourceBytes += int64(len(source.Identity))
		if sourceBytes >= summarySegmentBytes {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, nil
	}
	if err := finishSummaryFile(f); err != nil {
		return 0, err
	}
	if err := finishSummaryFile(sourceFile); err != nil {
		return 0, err
	}
	if err := syncDirectory(archivePath); err != nil {
		return 0, err
	}
	if err := summaryStep("staged"); err != nil {
		return 0, err
	}
	digest, err := summaryFileDigest(path + ".pending")
	if err != nil {
		return 0, err
	}
	sourcesDigest, err := summaryFileDigest(path + ".sources")
	if err != nil {
		return 0, err
	}
	m := summaryManifest{name, day.UTC().Format("2006-01-02"), count, digest, sourcesDigest, "pending"}
	if err := rawArchiveChange(store, "INSERT INTO summary_archive_files(name,day,count,digest,sources_digest,state) VALUES(?,?,?,?,?,?)", m.Name, m.Day, m.Count, m.Digest, m.SourcesDigest, m.State); err != nil {
		return 0, err
	}
	registered = true
	if err := summaryStep("intent"); err != nil {
		return count, err
	}
	return count, completeSummaryArchive(store, m)
}

func recoverSummaryArchives(store *sql.DB) error {
	for {
		var m summaryManifest
		err := store.QueryRow("SELECT name,day,count,digest,sources_digest,state FROM summary_archive_files WHERE state='pending' ORDER BY name LIMIT 1").Scan(&m.Name, &m.Day, &m.Count, &m.Digest, &m.SourcesDigest, &m.State)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := lockInvestigation(context.Background(), archiveStop); err != nil {
			return err
		}
		err = completeSummaryArchive(store, m)
		investigateMu.Unlock()
		if err != nil {
			return err
		}
	}
}

func completeSummaryArchive(store *sql.DB, m summaryManifest) error {
	path, err := summaryArchiveFilePath(archivePath, m.Name)
	if err != nil {
		return err
	}
	if err := verifySummaryFile(path+".sources", m.SourcesDigest); err != nil {
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := verifySummaryFile(path+".pending", m.Digest); err != nil {
			return err
		}
		if err := os.Rename(path+".pending", path); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := verifySummaryFile(path, m.Digest); err != nil {
		return err
	}
	if err := summaryStep("renamed"); err != nil {
		return err
	}
	if err := syncDirectory(archivePath); err != nil {
		return err
	}
	if err := verifySummaryContents(path, m.Count); err != nil {
		return err
	}
	if err := summaryStep("published"); err != nil {
		return err
	}
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	sources, err := os.Open(path + ".sources")
	if err != nil {
		return err
	}
	defer closeReadResource(sources)
	tx, err := store.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	// Acquire the writer before comparison so concurrent mutation cannot change
	// any source after it has been proved. The segment bounds transaction work.
	if err := rawArchiveChange(tx, "UPDATE summary_archive_files SET state=state WHERE name=? AND state='pending'", m.Name); err != nil {
		return err
	}
	dec := json.NewDecoder(sources)
	// #nosec G202 G701 -- The SQL column expression is derived only from compiled QueryLogRow tags; row identity is bound.
	compare, err := tx.Prepare("SELECT " + summaryIdentitySQL() + " FROM query_logs WHERE id=?")
	if err != nil {
		return err
	}
	defer closeReadResource(compare)
	retire, err := tx.Prepare("DELETE FROM query_logs WHERE id=?")
	if err != nil {
		return err
	}
	defer closeReadResource(retire)
	var count int64
	for {
		if err := summaryStopping(); err != nil {
			return err
		}
		var source summarySource
		err := dec.Decode(&source)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		var actual string
		if err := compare.QueryRow(source.ID).Scan(&actual); err != nil {
			return err
		}
		if actual != source.Identity {
			return fmt.Errorf("summary source %d changed; retain originals", source.ID)
		}
		result, err := retire.Exec(source.ID)
		if err != nil {
			return err
		}
		if err := requireOneMutation(result); err != nil {
			return err
		}
		count++
	}
	if err := compare.Close(); err != nil {
		return err
	}
	if err := retire.Close(); err != nil {
		return err
	}
	if err := sources.Close(); err != nil {
		return err
	}
	if count != m.Count {
		return errors.New("summary source count mismatch; retain originals")
	}
	if err := summaryStep("deleted"); err != nil {
		return err
	}
	if err := rawArchiveChange(tx, "UPDATE summary_archive_files SET state='ready' WHERE name=? AND state='pending'", m.Name); err != nil {
		return err
	}
	if err := summaryStep("ready"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return summaryStep("retired")
}

const summarySelect = `SELECT id, CAST(timestamp AS TEXT), %s, client_ip, query_name, query_type,
				COALESCE(response_code, ''), blocked,
				COALESCE(upstream, ''), COALESCE(latency_microseconds, 0),
				COALESCE(block_tier, ''), COALESCE(block_rule, ''),
				COALESCE(block_source, ''), COALESCE(block_list_id, 0),
				COALESCE(block_list_name, ''),
				COALESCE(result, ''), COALESCE(result_reason, ''),
				COALESCE(client_name, ''), COALESCE(policy_json, ''),
				COALESCE(result_tier, ''), COALESCE(result_entity, ''),
				COALESCE(result_is_published, 0), COALESCE(result_rule, ''),
				COALESCE(result_list_id, 0), COALESCE(result_list_name, ''),
				COALESCE(range_result, ''), COALESCE(range_entity, ''),
				COALESCE(range_is_published, 0), COALESCE(range_rule, ''),
				COALESCE(range_list_id, 0), COALESCE(range_list_name, ''),
				COALESCE(group_result, ''), COALESCE(group_entity, ''),
				COALESCE(group_is_published, 0), COALESCE(group_rule, ''),
				COALESCE(group_list_id, 0), COALESCE(group_list_name, ''),
				COALESCE(ip_result, ''), COALESCE(ip_entity, ''),
				COALESCE(ip_is_published, 0), COALESCE(ip_rule, ''),
				COALESCE(ip_list_id, 0), COALESCE(ip_list_name, ''),
				COALESCE(coalesced_count, 1)
			`

func scanSummarySource(rows *sql.Rows, source *summarySource, row *QueryLogRow) error {
	err := rows.Scan(&source.ID, &source.Timestamp, &source.Identity, &row.ClientIP, &row.QueryName, &row.QueryType,
		&row.ResponseCode, &row.Blocked, &row.Upstream, &row.LatencyMicroseconds,
		&row.BlockTier, &row.BlockRule, &row.BlockSource,
		&row.BlockListID, &row.BlockListName,
		&row.Result, &row.ResultReason, &row.ClientName, &row.PolicyJSON,
		&row.ResultTier, &row.ResultEntity, &row.ResultIsPublished,
		&row.ResultRule, &row.ResultListID, &row.ResultListName,
		&row.RangeResult, &row.RangeEntity, &row.RangeIsPublished,
		&row.RangeRule, &row.RangeListID, &row.RangeListName,
		&row.GroupResult, &row.GroupEntity, &row.GroupIsPublished,
		&row.GroupRule, &row.GroupListID, &row.GroupListName,
		&row.IPResult, &row.IPEntity, &row.IPIsPublished,
		&row.IPRule, &row.IPListID, &row.IPListName,
		&row.CoalescedCount)
	return err
}

// Archive metadata must survive power loss independently of the main writer's
// batching policy. This private connection changes no DNS admission behavior.
func summaryDatabasePath(ctx context.Context) (string, error) {
	var seq int
	var name, path string
	if err := db.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		return "", err
	}
	return path, nil
}
func openSummaryStore() (*sql.DB, error) {
	path, err := summaryDatabasePath(context.Background())
	if err != nil {
		return nil, err
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	store, err := sql.Open("sqlite3", uri+"?mode=rw&_synchronous=FULL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, err
	}
	store.SetMaxOpenConns(1)
	if _, err := store.Exec(summaryArchiveSchema); err != nil {
		return nil, errors.Join(err, store.Close())
	}
	return store, nil
}

func verifySummaryContents(path string, expected int64) error {
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer closeReadResource(file)
	reader := parquet.NewGenericReader[QueryLogRow](file)
	defer closeReadResource(reader)
	// #nosec G304 G703 -- Archive paths are generated basenames or validated by summaryArchiveFilePath; this local integrity helper also supports trusted operator paths.
	sources, err := os.Open(path + ".sources")
	if err != nil {
		return err
	}
	defer closeReadResource(sources)
	dec := json.NewDecoder(sources)
	var count int64
	for {
		if err := summaryStopping(); err != nil {
			return err
		}
		var source summarySource
		err := dec.Decode(&source)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		var row [1]QueryLogRow
		n, err := reader.Read(row[:])
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n != 1 || row[0] != source.Row {
			return errors.New("summary parquet differs from exact source projection; retain originals")
		}
		count++
	}
	var extra [1]QueryLogRow
	n, err := reader.Read(extra[:])
	if n != 0 || !errors.Is(err, io.EOF) {
		return errors.New("summary parquet has unexpected trailing records")
	}
	if count != expected {
		return errors.New("summary parquet source count mismatch")
	}
	if err := reader.Close(); err != nil {
		return err
	}
	if err := sources.Close(); err != nil {
		return err
	}
	return file.Close()
}
