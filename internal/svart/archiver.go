package svart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// QueryLogRow is the Parquet schema for archived query logs.
type QueryLogRow struct {
	Timestamp           int64  `parquet:"timestamp,snappy"`
	ClientIP            string `parquet:"client_ip,snappy,dict"`
	QueryName           string `parquet:"query_name,snappy"`
	QueryType           string `parquet:"query_type,snappy,dict"`
	ResponseCode        string `parquet:"response_code,snappy,dict"`
	Blocked             bool   `parquet:"blocked"`
	Upstream            string `parquet:"upstream,snappy,dict"`
	LatencyMicroseconds int64  `parquet:"latency_microseconds"`
	BlockTier           string `parquet:"block_tier,snappy,dict"`
	BlockRule           string `parquet:"block_rule,snappy"`
	BlockSource         string `parquet:"block_source,snappy,dict"`
	BlockListID         int64  `parquet:"block_list_id"`
	BlockListName       string `parquet:"block_list_name,snappy,dict"`
	// Rich policy result fields (DD-019)
	Result            string `parquet:"result,snappy,dict"`
	ResultReason      string `parquet:"result_reason,snappy,dict"`
	ClientName        string `parquet:"client_name,snappy,dict"`
	PolicyJSON        string `parquet:"policy_json,snappy"`
	ResultTier        string `parquet:"result_tier,snappy,dict"`
	ResultEntity      string `parquet:"result_entity,snappy,dict"`
	ResultIsPublished bool   `parquet:"result_is_published"`
	ResultRule        string `parquet:"result_rule,snappy"`
	ResultListID      int64  `parquet:"result_list_id"`
	ResultListName    string `parquet:"result_list_name,snappy,dict"`
	RangeResult       string `parquet:"range_result,snappy,dict"`
	RangeEntity       string `parquet:"range_entity,snappy,dict"`
	RangeIsPublished  bool   `parquet:"range_is_published"`
	RangeRule         string `parquet:"range_rule,snappy"`
	RangeListID       int64  `parquet:"range_list_id"`
	RangeListName     string `parquet:"range_list_name,snappy,dict"`
	GroupResult       string `parquet:"group_result,snappy,dict"`
	GroupEntity       string `parquet:"group_entity,snappy,dict"`
	GroupIsPublished  bool   `parquet:"group_is_published"`
	GroupRule         string `parquet:"group_rule,snappy"`
	GroupListID       int64  `parquet:"group_list_id"`
	GroupListName     string `parquet:"group_list_name,snappy,dict"`
	IPResult          string `parquet:"ip_result,snappy,dict"`
	IPEntity          string `parquet:"ip_entity,snappy,dict"`
	IPIsPublished     bool   `parquet:"ip_is_published"`
	IPRule            string `parquet:"ip_rule,snappy"`
	IPListID          int64  `parquet:"ip_list_id"`
	IPListName        string `parquet:"ip_list_name,snappy,dict"`
	CoalescedCount    int64  `parquet:"coalesced_count"`
}

var (
	archivePath string
	archiveDone chan struct{}
	archiveStop chan struct{}
	archiveOnce sync.Once
)

const archiveAgeDays = 8 // archive data older than 8 days (7-day dashboard + 1 day buffer)

// startArchiveWorker launches the daily background archiver goroutine.
// getArchiveTimezone returns the timezone for scheduling archive runs.
// Priority: user-configured timezone → server local time → UTC.
func getArchiveTimezone() *time.Location {
	if readDB != nil {
		var tz string
		if err := readDB.QueryRow("SELECT value FROM settings WHERE key = 'timezone'").Scan(&tz); err != nil && !errors.Is(err, sql.ErrNoRows) {
			logDB.Warn("archive timezone unavailable; using local timezone", "error", err)
		}
		if tz != "" {
			if loc, err := time.LoadLocation(tz); err == nil {
				return loc
			}
		}
	}
	return time.Now().Location() // server local time (falls back to UTC if not set)
}

// nextMidnight returns the next midnight in the given timezone.
func nextMidnight(loc *time.Location) time.Time {
	now := time.Now().In(loc)
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	return next
}

func startArchiveWorker() {
	archiveDone = make(chan struct{})
	archiveStop = make(chan struct{})

	go func() {
		defer close(archiveDone)
		runArchiveCycle()
		for {
			loc := getArchiveTimezone()
			untilMidnight := time.Until(nextMidnight(loc))
			timer := time.NewTimer(untilMidnight)

			select {
			case <-archiveStop:
				timer.Stop()
				return
			case <-timer.C:
				runArchiveCycle()
				// VACUUM after archiving to reclaim disk space from deleted rows
				if _, err := db.Exec("VACUUM"); err != nil {
					logArchiver.Error("VACUUM failed", "error", err)
				} else {
					logArchiver.Info("VACUUM completed")
				}
			}
		}
	}()
}

// closeArchiver stops the archive worker gracefully.
func closeArchiver() {
	archiveOnce.Do(func() {
		if archiveStop != nil {
			close(archiveStop)
		}
		if archiveDone != nil {
			<-archiveDone
		}
	})
}

// runArchiveCycle finds all complete days older than archiveAgeDays and archives them.
func runArchiveCycle() {
	retentionDays, err := readLogRetentionDays(readDB)
	if err != nil {
		logArchiver.Error("skipping archive cycle", "error", err)
		return
	}
	defer cleanupOldParquetFiles(archivePath, retentionDays)
	cutoff := time.Now().UTC().AddDate(0, 0, -archiveAgeDays)
	cutoff = time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, time.UTC) // truncate to day boundary

	// Find the oldest timestamp in query_logs
	var oldestStr string
	err = readDB.QueryRow("SELECT MIN(timestamp) FROM query_logs").Scan(&oldestStr)
	if err != nil || oldestStr == "" {
		return // no data
	}

	oldest, err := parseTimestamp(oldestStr)
	if err != nil {
		logArchiver.Error("failed to parse oldest timestamp", "timestamp", oldestStr, "error", err)
		return
	}

	if oldest.After(cutoff) {
		return // nothing old enough to archive
	}

	// Iterate over complete days from oldest to cutoff
	current := time.Date(oldest.Year(), oldest.Month(), oldest.Day(), 0, 0, 0, 0, time.UTC)
	for current.Before(cutoff) {
		if summaryStopping() != nil {
			return
		}
		count, err := archiveDay(current)
		if err != nil {
			logArchiver.Error("failed to archive day", "date", current.Format("2006-01-02"), "error", err)
		} else if count > 0 {
			logArchiver.Info("archived query logs", "rows", count, "date", current.Format("2006-01-02"), "file", archiveDayFileName(current))
		}

		current = current.AddDate(0, 0, 1)
	}

}

// cleanupOldParquetFiles deletes Parquet archive files older than retentionDays.
func cleanupOldParquetFiles(archiveDir string, retentionDays int) {
	summaryArchiveMu.Lock()
	defer summaryArchiveMu.Unlock()
	if err := lockInvestigation(context.Background(), archiveStop); err != nil {
		return
	}
	defer investigateMu.Unlock()
	if retentionDays < 1 || retentionDays > maxLogRetentionDays {
		logArchiver.Error("skipping archive retention cleanup: invalid log_retention_days", "retention_days", retentionDays)
		return
	}
	if err := expireSummaryFiles(archiveDir, retentionDays); err != nil {
		logArchiver.Error("archive retention unavailable; originals retained", "error", err)
	}
}

// archiveDay exports all query_logs for the given day to Parquet, then deletes them.
// Returns the number of rows archived, or 0 if none/already done.
func archiveDay(day time.Time) (int64, error) {
	summaryArchiveMu.Lock()
	defer summaryArchiveMu.Unlock()
	store, err := openSummaryStore()
	if err != nil {
		return 0, err
	}
	defer closeReadResource(store)
	if err := recoverSummaryArchives(store); err != nil {
		return 0, err
	}
	if err := checkExpectedSummaryFiles(summaryReadContext{stop: archiveStop}, store, archivePath); err != nil {
		return 0, err
	}
	start := day.UTC().Format("2006-01-02") + " 00:00:00"
	end := day.UTC().AddDate(0, 0, 1).Format("2006-01-02") + " 00:00:00"
	var upper int64
	if err := readDB.QueryRow("SELECT COALESCE(MAX(id),0) FROM query_logs WHERE timestamp>=? AND timestamp<?", start, end).Scan(&upper); err != nil {
		return 0, err
	}
	var total int64
	for upper > 0 {
		n, err := archiveSummarySegment(store, day, start, end, upper)
		total += n
		if err != nil || n == 0 {
			return total, err
		}
	}
	return total, nil
}

// archiveDayFileName returns the Parquet file name for a given day.
func archiveDayFileName(day time.Time) string {
	return fmt.Sprintf("svart-dns-%s.parquet", day.Format("2006-01-02"))
}

// getArchiveStatus returns info about the archive state.
func getArchiveStatus(ctx context.Context) (ArchiveStatusView, error) {
	ctx, cancel := context.WithTimeout(ctx, summaryMetadataReadTimeout)
	defer cancel()
	if err := lockInvestigation(ctx, nil); err != nil {
		return ArchiveStatusView{}, err
	}
	defer investigateMu.Unlock()
	databasePath, err := summaryDatabasePath(ctx)
	if err != nil {
		return ArchiveStatusView{}, err
	}
	status := ArchiveStatusView{ArchivePath: archivePath, ArchiveAgeDays: archiveAgeDays, ArchiveFiles: []ArchiveFileView{}}
	var oldest string
	if err := readDB.QueryRowContext(ctx, "SELECT COALESCE(MIN(timestamp),''),COUNT(*) FROM query_logs").Scan(&oldest, &status.LiveRows); err != nil {
		return status, err
	}
	if oldest != "" {
		ts, err := parseTimestamp(oldest)
		if err != nil {
			return status, err
		}
		status.OldestDataDays = int(time.Since(ts).Hours() / 24)
	}
	entries, err := os.ReadDir(archivePath)
	if err != nil && !os.IsNotExist(err) {
		return status, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".parquet" {
			info, err := entry.Info()
			if err != nil {
				return status, err
			}
			status.ArchiveFiles = append(status.ArchiveFiles, ArchiveFileView{Name: entry.Name(), SizeMB: float64(info.Size()) / (1024 * 1024), Modified: info.ModTime().Format(time.RFC3339)})
		}
	}
	if err := checkSummaryAvailability(ctx, databasePath, archivePath); err != nil {
		return ArchiveStatusView{}, err
	}
	return status, nil
}

// parseTimestamp handles the common SQLite timestamp formats.
func parseTimestamp(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", s)
}
