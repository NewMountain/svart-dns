package main

import (
	"database/sql"
	"fmt"
	dto "github.com/prometheus/client_model/go"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRawArchiveStreamingFullSegmentsAndHourlyTail(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	a.deferSmall = true
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	// Two full segments and a small tail, supplied in normal journal batches.
	const total = 131089
	for start := 0; start < total; start += 512 {
		end := start + 512
		if end > total {
			end = total
		}
		payloads := make([][]byte, 0, end-start)
		for i := start; i < end; i++ {
			payloads = append(payloads, []byte(fmt.Sprintf(`{"ts":%d,"query_name":"host-%d.example.","query_type":"AAAA","client_ip":"192.0.2.10","latency_us":17,"unknown":%q}`, now.UnixNano()+int64(i), i, strings.Repeat("original", 20))))
		}
		if _, err := j.appendBatch(payloads); err != nil {
			t.Fatal(err)
		}
	}
	rawArchiveProgress(t, source, j, total)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	rawArchiveCount(t, j, 17)
	var files, count, maxCount int
	if err := j.db.QueryRow("SELECT COUNT(*),SUM(count),MAX(count) FROM raw_archive_files WHERE state='ready'").Scan(&files, &count, &maxCount); err != nil {
		t.Fatal(err)
	}
	if files != 2 || count != 131072 || maxCount != 65536 {
		t.Fatalf("files=%d records=%d max=%d want2/131072/65536", files, count, maxCount)
	}
	for _, at := range []time.Time{now.Add(time.Minute), now.Add(59 * time.Minute)} {
		if err := a.cycle(at); err != nil {
			t.Fatal(err)
		}
		rawArchiveCount(t, j, 17)
	}
	if err := a.cycle(now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	var n int64
	if err := a.verify(func(r rawArchiveRow) error {
		n++
		if r.Sequence != n {
			return fmt.Errorf("sequence=%d want%d", r.Sequence, n)
		}
		expected := []byte(fmt.Sprintf(`{"ts":%d,"query_name":"host-%d.example.","query_type":"AAAA","client_ip":"192.0.2.10","latency_us":17,"unknown":%q}`, now.UnixNano()+n-1, n-1, strings.Repeat("original", 20)))
		if string(r.Payload) != string(expected) {
			return fmt.Errorf("payload differs at%d", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 131089 {
		t.Fatalf("verified=%d want131089", n)
	}
	var physical int
	var bytes int64
	if err := walkRawArchiveFiles(a.dir, func(path string, info os.FileInfo) error {
		physical++
		bytes += info.Size()
		if filepath.Dir(path) != "2026/09/28" {
			return fmt.Errorf("partition=%s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if physical != 3 {
		t.Fatalf("physical files=%d want3", physical)
	}
	t.Logf("events=%d files=%d archive_bytes=%d archive_and_verify_duration=%s cumulative_alloc_bytes=%d heap_after_cycle_bytes=%d heap_before_bytes=%d", total, physical, bytes, time.Since(started), after.TotalAlloc-before.TotalAlloc, after.HeapAlloc, before.HeapAlloc)
}
func TestRawArchivePartitionTraversalAndManifestOrdering(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	rawArchiveAppend(t, j, now.UnixNano(), "first")
	rawArchiveProgress(t, source, j, 1)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveAppend(t, j, now.UnixNano(), "late after clock correction")
	rawArchiveProgress(t, source, j, 2)
	if err := a.cycle(now.AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	n := int64(0)
	if err := a.verify(func(r rawArchiveRow) error {
		n++
		if r.Sequence != n {
			return fmt.Errorf("sequence=%d want%d", r.Sequence, n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("verified=%d want2", n)
	}
	// 4,096 files span64 day directories; the traversal receives exact counts
	// without a whole directory listing or following external symlinks.
	root := t.TempDir()
	for day := 0; day < 64; day++ {
		dir := filepath.Join(root, "2026", fmt.Sprintf("%02d", day/28+1), fmt.Sprintf("%02d", day%28+1))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 64; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%03d", i)), []byte("archive fixture"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	size, err := rawArchiveDirectoryBytes(root)
	if err != nil {
		t.Fatal(err)
	}
	if size != 61440 {
		t.Fatalf("directory bytes=%d want61440", size)
	}
	if err = os.Symlink(t.TempDir(), filepath.Join(root, "external")); err != nil {
		t.Fatal(err)
	}
	if _, err = rawArchiveDirectoryBytes(root); err == nil {
		t.Fatal("expected nonregular entry rejection")
	}
}
func TestRawArchiveRetirementFailureRollsBackOwnedRows(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "original")
	rawArchiveProgress(t, source, j, 1)
	if _, err := j.db.Exec(`CREATE TRIGGER refuse_retirement BEFORE UPDATE ON raw_archive_files WHEN NEW.state='ready' BEGIN SELECT RAISE(ABORT,'retirement storage failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err == nil || !strings.Contains(err.Error(), "retirement storage failure") {
		t.Fatalf("error=%v", err)
	}
	rawArchiveCount(t, j, 1)
	var value string
	if err := j.db.QueryRow("SELECT value FROM journal_meta WHERE key='raw_archived_through'").Scan(&value); err != sql.ErrNoRows {
		t.Fatalf("progress=%q err=%v want absent", value, err)
	}
	if _, err := j.db.Exec("DROP TRIGGER refuse_retirement"); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
}

func TestRawArchivePayloadByteTargetStreamsOversizedBatches(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	// 257 payloads exceed64MiB; only16 fit the4MiB batch target because one
	// complete event can cross the target. No partial event is ever archived.
	payload := []byte(fmt.Sprintf(`{"ts":%d,"query_name":"large-policy.example.","policy":%q}`, now.UnixNano(), strings.Repeat("p", 256*1024)))
	for start := 0; start < 257; start += 16 {
		n := 16
		if start+n > 257 {
			n = 257 - start
		}
		batch := make([][]byte, n)
		for i := range batch {
			batch[i] = payload
		}
		if _, err := j.appendBatch(batch); err != nil {
			t.Fatal(err)
		}
	}
	rawArchiveProgress(t, source, j, 257)
	rows, err := a.originalRows(1, 257)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 16 {
		t.Fatalf("read batch=%d want16", len(rows))
	}
	if err = a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
	var files, maxCount, total int
	if err = j.db.QueryRow("SELECT COUNT(*),MAX(count),SUM(count) FROM raw_archive_files WHERE state='ready'").Scan(&files, &maxCount, &total); err != nil {
		t.Fatal(err)
	}
	if files != 2 || maxCount != 256 || total != 257 {
		t.Fatalf("files=%d max=%d total=%d want2/256/257", files, maxCount, total)
	}
	n := 0
	if err = a.verify(func(r rawArchiveRow) error {
		n++
		if string(r.Payload) != string(payload) {
			return fmt.Errorf("large payload differs at%d", r.Sequence)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 257 {
		t.Fatalf("verified=%d want257", n)
	}
}

func TestRawArchiveVerifierRejectsDuplicateSequenceManifests(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "duplicate fixture")
	rawArchiveProgress(t, source, j, 1)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	m, err := a.nextManifest("", "ready")
	if err != nil {
		t.Fatal(err)
	}
	copyName := filepath.Join(filepath.Dir(m.name), "duplicate.parquet")
	data, err := os.ReadFile(filepath.Join(a.dir, m.name))
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- Fixture mutation stays in disposable test storage or the explicitly selected evidence directory.
	if err = os.WriteFile(filepath.Join(a.dir, copyName), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = j.db.Exec("INSERT INTO raw_archive_files SELECT ?,first_id,last_id,count,digest,state,min_ns,max_ns FROM raw_archive_files WHERE name=?", copyName, m.name); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = verifyRawArchives(j.path, filepath.Join(filepath.Dir(j.path), "archives"), &out)
	if err == nil || err.Error() != "overlapping archive sequence identities" {
		t.Fatalf("verification=%v want overlap rejection", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected success=%q", out.String())
	}
}

func TestRawArchiveIgnoredRetentionMutationsPreserveOwner(t *testing.T) {
	for name, trigger := range map[string]string{
		"replacement insert": `CREATE TRIGGER ignored BEFORE INSERT ON raw_archive_files WHEN NEW.state='ready' BEGIN SELECT RAISE(IGNORE); END`,
		"obsolete update":    `CREATE TRIGGER ignored BEFORE UPDATE ON raw_archive_files WHEN NEW.state='obsolete' BEGIN SELECT RAISE(IGNORE); END`,
		"rewrite delete":     `CREATE TRIGGER ignored BEFORE DELETE ON raw_archive_rewrites BEGIN SELECT RAISE(IGNORE); END`,
		"obsolete delete":    `CREATE TRIGGER ignored BEFORE DELETE ON raw_archive_files WHEN OLD.state='obsolete' BEGIN SELECT RAISE(IGNORE); END`,
	} {
		t.Run(name, func(t *testing.T) {
			a, j, source := rawArchiveFixture(t)
			now := time.Now()
			rawArchiveAppend(t, j, now.AddDate(0, 0, -2).UnixNano(), "expired")
			keep := rawArchiveAppend(t, j, now.UnixNano(), "retained original")
			rawArchiveProgress(t, source, j, 2)
			if err := a.cycle(now); err != nil {
				t.Fatal(err)
			}
			old, err := a.nextManifest("", "ready")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = source.Exec("UPDATE settings SET value='1'"); err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			err = a.cycle(now)
			if err == nil || !strings.Contains(err.Error(), "affected 0 rows, expected 1") {
				t.Fatalf("cycle error=%v want affected-row failure", err)
			}
			var retained bool
			if err = a.verify(func(r rawArchiveRow) error {
				if r.Sequence == 2 && string(r.Payload) == string(keep) {
					retained = true
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !retained {
				t.Fatal("sole retained raw payload lost")
			}
			if name != "obsolete delete" {
				if _, err = os.Stat(filepath.Join(a.dir, old.name)); err != nil {
					t.Fatalf("original file retired before successful ownership mutation: %v", err)
				}
			}
			if _, err = j.db.Exec("DROP TRIGGER ignored"); err != nil {
				t.Fatal(err)
			}
			if err = a.cycle(now); err != nil {
				t.Fatal(err)
			}
			n := 0
			if err = a.verify(func(r rawArchiveRow) error {
				n++
				if r.Sequence != 2 || string(r.Payload) != string(keep) {
					return fmt.Errorf("unexpected replacement row %d", r.Sequence)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("retained rows=%d want1", n)
			}
		})
	}
}
func TestRawArchiveIgnoredRetirementMutationRollsBack(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "retained")
	rawArchiveProgress(t, source, j, 1)
	if _, err := j.db.Exec(`CREATE TRIGGER ignored BEFORE UPDATE ON raw_archive_files WHEN NEW.state='ready' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err == nil || !strings.Contains(err.Error(), "affected 0 rows") {
		t.Fatalf("error=%v want mutation failure", err)
	}
	rawArchiveCount(t, j, 1)
	if _, err := j.db.Exec("DROP TRIGGER ignored"); err != nil {
		t.Fatal(err)
	}
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	rawArchiveCount(t, j, 0)
}

func TestRawArchiveByteInventorySamplingAndUnavailable(t *testing.T) {
	a, j, source := rawArchiveFixture(t)
	now := time.Now()
	rawArchiveAppend(t, j, now.UnixNano(), "metrics")
	rawArchiveProgress(t, source, j, 1)
	if err := a.cycle(now); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(a.dir, "recoverable.partial")
	if err := os.WriteFile(partial, []byte("retained partial"), 0600); err != nil {
		t.Fatal(err)
	}
	expected, err := rawArchiveDirectoryBytes(a.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.updateMetrics(true)
	read := func() float64 {
		var metric dto.Metric
		if err := rawArchiveBytes.Write(&metric); err != nil {
			t.Fatal(err)
		}
		return metric.GetGauge().GetValue()
	}
	if got := read(); got != float64(expected) {
		t.Fatalf("inventory=%v want%d including16partialbytes", got, expected)
	}
	saved := a.dir
	a.dir = filepath.Join(saved, "missing-directory")
	a.updateMetrics(false)
	if got := read(); got != float64(expected) {
		t.Fatalf("non-inventory cycle changed sampled bytes=%v want%d", got, expected)
	}
	a.updateMetrics(true)
	if got := read(); !math.IsNaN(got) {
		t.Fatalf("failed inventory=%v wantNaN", got)
	}
	a.dir = saved
}
