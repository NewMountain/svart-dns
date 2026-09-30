package svart

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/yeti/svart-dns/internal/listparse"
)

// seedRealLists stores every list file in SVART_LISTS_DIR as an enabled
// blocklist (parsed the way refreshes parse downloads) and assigns them all
// to a 0.0.0.0/0 range and one client. It skips when the variable is unset.
func seedRealLists(b *testing.B) {
	b.Helper()
	dir := os.Getenv("SVART_LISTS_DIR")
	if dir == "" {
		b.Skip("SVART_LISTS_DIR not set")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.txt"))
	if err != nil || len(files) == 0 {
		b.Fatalf("no lists in %s: %v", dir, err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO ip_ranges (name, cidr) VALUES ('everyone', '0.0.0.0/0')"); err != nil {
		b.Errorf("fixture operation failed: %v", err)
	}
	stmt, err := tx.Prepare("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)")
	if err != nil {
		b.Fatal(err)
	}
	for _, path := range files {
		// #nosec G304 G703 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		f, err := os.Open(path)
		if err != nil {
			b.Fatal(err)
		}
		list, err := listparse.Parse(f, defaultListMaxDownloadBytes, defaultListMaxLines)
		checkTestClose(b, f)
		if err != nil {
			b.Fatal(err)
		}
		res, err := tx.Exec("INSERT INTO blocklists (url, alias, enabled, domain_count) VALUES (?, ?, 1, ?)",
			"https://lists.example.net/"+filepath.Base(path), filepath.Base(path), len(list.Domains))
		if err != nil {
			b.Fatal(err)
		}
		id, fixtureErr1454 := res.LastInsertId()
		if fixtureErr1454 != nil {
			b.Errorf("fixture operation failed: %v", fixtureErr1454)
		}
		for _, d := range list.Domains {
			if _, err := stmt.Exec(id, d); err != nil {
				b.Fatal(err)
			}
		}
		if _, err := tx.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?)", id); err != nil {
			b.Errorf("fixture operation failed: %v", err)
		}
		if _, err := tx.Exec("INSERT INTO client_blocklists (client_ip, blocklist_id) VALUES ('10.42.1.42', ?)", id); err != nil {
			b.Errorf("fixture operation failed: %v", err)
		}
	}
	checkTestClose(b, stmt)
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkReloadPolicyRealLists is the in-memory part of time-to-load: a
// full policy rebuild over real published lists after one of them changed.
func BenchmarkReloadPolicyRealLists(b *testing.B) {
	defer setupTestDBTB(b)()
	seedRealLists(b)
	b.ReportAllocs()
	// CPU time is reported next to wall time: the benchmark machine is shared
	// and heavily loaded, which inflates ns/op but not the work done.
	cpuStart := processCPU()
	for b.Loop() {
		if err := reloadPolicyState("bench", reloadListContent); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64((processCPU()-cpuStart).Milliseconds())/float64(b.N), "cpu-ms/op")
	b.ReportMetric(float64(policyState.Load().Index.ApproxBytes())/(1<<20), "index-MiB")
}

// processCPU is the process's user plus system CPU time so far.
func processCPU() time.Duration {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		panic(err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}
