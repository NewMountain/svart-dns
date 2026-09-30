package svart

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArchiveDay(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Set archive path to temp dir
	tmpDir, err := os.MkdirTemp("", "svart-archive-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	origPath := archivePath
	archivePath = tmpDir
	defer func() { archivePath = origPath }()

	// Insert rows spanning 2 days
	day1 := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2023, 6, 16, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 100; i++ {
		ts := day1.Add(time.Duration(i) * time.Minute)
		if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
			ts.Format("2006-01-02 15:04:05"), "10.42.1.42", "example.com.", "A", "NOERROR", false, 5); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}
	for i := 0; i < 50; i++ {
		ts := day2.Add(time.Duration(i) * time.Minute)
		if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
			ts.Format("2006-01-02 15:04:05"), "10.42.1.42", "example.com.", "A", "NOERROR", false, 5); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}

	// Archive June 15
	count, err := archiveDay(day1)
	if err != nil {
		t.Fatalf("archiveDay failed: %v", err)
	}
	if count != 100 {
		t.Errorf("expected 100 archived rows, got %d", count)
	}

	// Verify Parquet file exists with daily name
	expectedFile := filepath.Join(tmpDir, "svart-dns-2023-06-15.parquet")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Error("expected Parquet file to exist")
	}

	// Verify day1 rows deleted from SQLite
	var day1Count int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs WHERE timestamp >= '2023-06-15' AND timestamp < '2023-06-16'").Scan(&day1Count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if day1Count != 0 {
		t.Errorf("expected 0 day1 rows after archive, got %d", day1Count)
	}

	// Verify day2 rows still present
	var day2Count int
	if err := db.QueryRow("SELECT COUNT(*) FROM query_logs WHERE timestamp >= '2023-06-16' AND timestamp < '2023-06-17'").Scan(&day2Count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if day2Count != 50 {
		t.Errorf("expected 50 day2 rows, got %d", day2Count)
	}
}

func TestArchiveIdempotent(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tmpDir, err := os.MkdirTemp("", "svart-archive-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	origPath := archivePath
	archivePath = tmpDir
	defer func() { archivePath = origPath }()

	// Insert rows for Jan 10, 2023
	day := time.Date(2023, 1, 10, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		ts := day.Add(time.Duration(i) * time.Minute)
		if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
			ts.Format("2006-01-02 15:04:05"), "10.42.1.42", "test.com.", "A", "NOERROR", false, 3); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}

	// First archive
	count1, err := archiveDay(day)
	if err != nil {
		t.Fatalf("first archive failed: %v", err)
	}
	if count1 != 50 {
		t.Errorf("expected 50 rows archived, got %d", count1)
	}

	// Second archive — should be a no-op (file already exists)
	count2, err := archiveDay(day)
	if err != nil {
		t.Fatalf("second archive failed: %v", err)
	}
	if count2 != 0 {
		t.Errorf("expected 0 rows on second archive (idempotent), got %d", count2)
	}
}

func TestCleanupOldParquetFiles(t *testing.T) {
	t.Setenv("LOG_LEVEL", "error")
	initLogging()

	tmpDir := t.TempDir()
	// Create fake parquet files
	if err := os.WriteFile(filepath.Join(tmpDir, "svart-dns-2020-01-31.parquet"), []byte("old"), 0600); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "svart-dns-2025-12-31.parquet"), []byte("recent"), 0600); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "not-a-parquet.txt"), []byte("ignore"), 0600); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	cleanupOldParquetFiles(tmpDir, 365) // 1 year retention

	// Old file should be deleted
	if _, err := os.Stat(filepath.Join(tmpDir, "svart-dns-2020-01-31.parquet")); !os.IsNotExist(err) {
		t.Error("expected old parquet file to be deleted")
	}
	// Recent file should remain
	if _, err := os.Stat(filepath.Join(tmpDir, "svart-dns-2025-12-31.parquet")); err != nil {
		t.Error("expected recent parquet file to remain")
	}
	// Non-parquet file should remain
	if _, err := os.Stat(filepath.Join(tmpDir, "not-a-parquet.txt")); err != nil {
		t.Error("expected non-parquet file to remain")
	}
}

func TestArchiveStatus(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	tmpDir, err := os.MkdirTemp("", "svart-archive-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Errorf("fixture operation failed: %v", err)
		}
	}()
	origPath := archivePath
	archivePath = tmpDir
	defer func() { archivePath = origPath }()

	// Insert some data
	ts := time.Now().Add(-24 * time.Hour)
	if _, err := db.Exec("INSERT INTO query_logs (timestamp, client_ip, query_name, query_type, response_code, blocked, latency_microseconds) VALUES (?, ?, ?, ?, ?, ?, ?)",
		ts.Format("2006-01-02 15:04:05"), "10.42.1.42", "test.com.", "A", "NOERROR", false, 3); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	status, err := getArchiveStatus(t.Context())
	if err != nil {
		t.Fatalf("getArchiveStatus failed: %v", err)
	}

	if status.ArchivePath != tmpDir {
		t.Errorf("expected archive_path %q, got %q", tmpDir, status.ArchivePath)
	}
	if status.LiveRows != 1 {
		t.Errorf("expected 1 live row, got %v", status.LiveRows)
	}
}
