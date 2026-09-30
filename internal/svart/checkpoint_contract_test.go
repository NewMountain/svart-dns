package svart

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func seedCheckpointContract(t *testing.T) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO blocklists(id,url,alias) VALUES(900,'https://example.com/rules','Checkpoint fixture')`,
		`INSERT INTO blocklist_history(id,blocklist_id,refreshed_at) VALUES(901,900,'2026-09-26 12:00:00'),(902,900,'2026-09-26 12:00:00'),(903,900,'2026-09-26 12:01:00')`,
		`INSERT INTO blocked_domains(blocklist_id,domain) VALUES(900,'keep.example'),(900,'readded.example'),(900,'new.example')`,
		`INSERT INTO blocklist_changelog(history_id,domain,action) VALUES(902,'removed.example','removed'),(902,'readded.example','removed'),(902,'new.example','added'),(903,'readded.example','added')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckpointSameTimestampReference(t *testing.T) {
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	rr := httptest.NewRecorder()
	handleAPIBlocklistCheckpointDomains(rr, httptest.NewRequest(http.MethodGet, "/api/blocklists/history/901/domains?limit=2&offset=1", nil))
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	got := decodeAPIData[struct {
		Domains []string `json:"domains"`
		Total   int      `json:"total"`
	}](t, rr)
	if got.Total != 3 || !reflect.DeepEqual(got.Domains, []string{"readded.example", "removed.example"}) {
		t.Fatalf("checkpoint differs from independent reference: %+v", got)
	}
}

func TestCheckpointMissingChangelogUnavailable(t *testing.T) {
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	if _, err := db.Exec(`DROP TABLE blocklist_changelog`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handleAPIBlocklistCheckpointDomains(rr, httptest.NewRequest(http.MethodGet, "/api/blocklists/history/901/domains", nil))
	if rr.Code < 500 {
		t.Fatalf("missing history reported success: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestCheckpointPageSemanticsAndCancellation(t *testing.T) {
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	for _, tc := range []struct {
		search string
		offset int
		want   []string
		total  int
	}{
		{"", 99, []string{}, 3}, {"removed", 0, []string{"removed.example"}, 1}, {"%", 0, []string{}, 0}, {"EXAMPLE", 0, []string{}, 0},
	} {
		page, err := queryCheckpointPage(context.Background(), 901, tc.search, 10, tc.offset)
		if err != nil || page.Total != tc.total || !reflect.DeepEqual(page.Domains, tc.want) {
			t.Fatalf("search=%q page=%+v err=%v", tc.search, page, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queryCheckpointPage(ctx, 901, "", 10, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	// A canceled request must not leave scratch state on a pooled connection.
	if _, err := queryCheckpointPage(context.Background(), 901, "", 10, 0); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointIterationFailureUnavailable(t *testing.T) {
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	for _, q := range []string{`ALTER TABLE blocklist_changelog RENAME TO saved_changelog`,
		`CREATE VIEW blocklist_changelog AS SELECT id,history_id,CASE WHEN id=2 THEN json_extract('invalid','$') ELSE domain END AS domain,action FROM saved_changelog`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rr := httptest.NewRecorder()
	handleAPIBlocklistCheckpointDomains(rr, httptest.NewRequest(http.MethodGet, "/api/blocklists/history/901/domains", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("iteration error: %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "invalid") || strings.Contains(rr.Body.String(), "domains") {
		t.Fatalf("leaked partial result or SQL: %s", rr.Body.String())
	}
}

func TestCheckpointMillionRulesBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("million-rule SQLite fixture")
	}
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	for _, q := range []string{
		`DELETE FROM blocked_domains WHERE blocklist_id=900`,
		`DELETE FROM blocklist_changelog`,
		`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<999999) INSERT INTO blocked_domains(blocklist_id,domain) SELECT 900,printf('rule-%07d.example',x) FROM n`,
		`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<499999) INSERT INTO blocklist_changelog(history_id,domain,action) SELECT 902,printf('rule-%07d.example',x),'added' FROM n`,
		`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<499999) INSERT INTO blocklist_changelog(history_id,domain,action) SELECT 902,printf('past-%07d.example',x),'removed' FROM n`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	before := checkpointRSS(t)
	peak := before
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if rss := checkpointRSS(t); rss > peak {
					peak = rss
				}
			}
		}
	}()
	page, err := queryCheckpointPage(context.Background(), 901, "", 3, 499999)
	close(stop)
	<-stopped
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1000000 || !reflect.DeepEqual(page.Domains, []string{"past-0499999.example", "rule-0500000.example", "rule-0500001.example"}) {
		t.Fatalf("million-rule page=%+v", page)
	}
	delta := peak - before
	t.Logf("million current rules + million changes: baseline RSS=%d peak RSS=%d delta=%d bytes; page=%v total=%d", before, peak, delta, page.Domains, page.Total)
	if delta > 64*1024*1024 {
		t.Fatalf("checkpoint exceeded 64 MiB RSS growth: %d", delta)
	}
}

func checkpointRSS(t *testing.T) int64 {
	t.Helper()
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(b))
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return pages * int64(os.Getpagesize())
}

func TestCheckpointCorruptHistoryUnavailable(t *testing.T) {
	for _, field := range []string{"NULL AS domain, action", "domain, NULL AS action", "domain, 'unexpected' AS action"} {
		t.Run(field, func(t *testing.T) {
			defer setupTestDB(t)()
			seedCheckpointContract(t)
			for _, q := range []string{`ALTER TABLE blocklist_changelog RENAME TO saved_changelog`, `CREATE VIEW blocklist_changelog AS SELECT id,history_id,` + field + ` FROM saved_changelog`} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := queryCheckpointPage(context.Background(), 901, "", 10, 0); err == nil {
				t.Fatal("corrupt changelog returned a plausible page")
			}
		})
	}
}

func TestCheckpointRestoresConnectionSettings(t *testing.T) {
	defer setupTestDB(t)()
	seedCheckpointContract(t)
	readDB.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA temp_store=FILE", "PRAGMA temp.cache_size=-3072"} {
		if _, err := readDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := queryCheckpointPage(context.Background(), 901, "", 10, 0); err != nil {
		t.Fatal(err)
	}
	var size int
	if err := readDB.QueryRow("PRAGMA temp.cache_size").Scan(&size); err != nil {
		t.Fatal(err)
	}
	if size != -3072 {
		t.Fatalf("pooled connection cache changed: %d", size)
	}
	var count int
	if err := readDB.QueryRow("SELECT count(*) FROM sqlite_temp_master WHERE name LIKE 'checkpoint_%'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("scratch table leaked: %d", count)
	}
}
