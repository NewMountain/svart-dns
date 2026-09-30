package svart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSummaryRetentionRejectsEscapingManifestWithoutRemovingSources(t *testing.T) {
	for _, state := range []string{"ready", "expiring", "expired"} {
		t.Run(state, func(t *testing.T) {
			t.Cleanup(setupTestDB(t))
			if _, err := db.Exec(summaryArchiveSchema); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			originalArchive := archivePath
			archivePath = filepath.Join(root, "archives")
			t.Cleanup(func() { archivePath = originalArchive })
			if err := os.Mkdir(archivePath, 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"retained.parquet", "retained.parquet.sources"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("sole recoverable source"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO summary_archive_files(name,day,count,digest,sources_digest,state) VALUES('../retained.parquet','2000-01-01',1,'digest','sources',?)`, state); err != nil {
				t.Fatal(err)
			}
			if err := expireSummaryFiles(archivePath, 1); err == nil {
				t.Error("escaping summary name accepted")
			}
			for _, name := range []string{"retained.parquet", "retained.parquet.sources"} {
				// #nosec G304 -- This test checks its own sibling files inside t.TempDir after a deliberately malformed manifest.
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(data) != "sole recoverable source" {
					t.Errorf("source %s changed: %q error=%v", name, data, err)
				}
			}
			var retainedState string
			if err := db.QueryRow(`SELECT state FROM summary_archive_files WHERE name='../retained.parquet'`).Scan(&retainedState); err != nil {
				t.Fatal(err)
			}
			if retainedState != state {
				t.Errorf("invalid manifest advanced from %q to %q", state, retainedState)
			}
		})
	}
}

// A dot is local according to filepath.IsLocal, but identifies the archive
// directory itself and makes the sidecar resolve outside that directory.
func TestSummaryRetentionRejectsDirectoryManifestWithoutRemovingSources(t *testing.T) {
	for _, state := range []string{"ready", "expiring", "expired"} {
		t.Run(state, func(t *testing.T) {
			t.Cleanup(setupTestDB(t))
			if _, err := db.Exec(summaryArchiveSchema); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			originalArchive := archivePath
			archivePath = filepath.Join(root, "archives")
			t.Cleanup(func() { archivePath = originalArchive })
			if err := os.Mkdir(archivePath, 0700); err != nil {
				t.Fatal(err)
			}
			sidecar := archivePath + ".sources"
			if err := os.WriteFile(sidecar, []byte("recoverable sibling original"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO summary_archive_files(name,day,count,digest,sources_digest,state) VALUES('.','2000-01-01',1,'digest','sources',?)`, state); err != nil {
				t.Fatal(err)
			}
			if err := expireSummaryFiles(archivePath, 1); err == nil {
				t.Error("directory manifest accepted")
			}
			if info, err := os.Stat(archivePath); err != nil || !info.IsDir() {
				t.Errorf("archive directory removed: %v", err)
			}
			// #nosec G304 -- The intentionally adjacent original lives inside this test's own temporary directory.
			if data, err := os.ReadFile(sidecar); err != nil || string(data) != "recoverable sibling original" {
				t.Errorf("sibling original changed: %q error=%v", data, err)
			}
			var retainedState string
			if err := db.QueryRow(`SELECT state FROM summary_archive_files WHERE name='.'`).Scan(&retainedState); err != nil {
				t.Fatal(err)
			}
			if retainedState != state {
				t.Errorf("invalid manifest advanced from %q to %q", state, retainedState)
			}
		})
	}
}
