package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func statsResponse[T any](t *testing.T, server *httptest.Server, path string) T {
	t.Helper()
	response, err := server.Client().Get(server.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTestClose(t, response.Body)
	var envelope struct {
		Data  T       `json:"data"`
		Error *string `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || envelope.Error != nil {
		t.Fatalf("%s: status=%d error=%v", path, response.StatusCode, envelope.Error)
	}
	return envelope.Data
}

func TestStatsHTTPPreservesWeightedRewriteAndSourceHistory(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	// A single read connection makes nested-query cursor leaks observable.
	readDB.SetMaxOpenConns(1)
	mux := http.NewServeMux()
	mux.HandleFunc("/rewrites", handleAPIRewritesStats)
	mux.HandleFunc("/sources", handleAPIStatsBlockSources)
	mux.HandleFunc("/upstreams", handleAPIStatsUpstreamUsage)
	mux.HandleFunc("/history", handleAPIBlocklistHistory)
	server := httptest.NewServer(mux)
	defer server.Close()
	if got := statsResponse[[]RewriteStatsView](t, server, "/rewrites"); got == nil || len(got) != 0 {
		t.Fatalf("empty rewrite inventory=%#v", got)
	}
	for _, row := range []struct {
		ip, name, upstream, source string
		blocked                    bool
		count                      int
	}{
		{"192.0.2.1", "nas.example.", "rewrite", "", false, 3},
		{"192.0.2.2", "nas.example.", "rewrite", "", false, 2},
		{"192.0.2.1", "other.example.", "rewrite", "", false, 1},
		{"192.0.2.1", "ads.example.", "", "Published List", true, 6},
		{"192.0.2.2", "other-ads.example.", "", "Other List", true, 2},
		{"192.0.2.1", "public.example.", "192.0.2.53:53", "", false, 2},
		{"192.0.2.1", "cache.example.", "cache", "", false, 20},
	} {
		if _, err := db.Exec(`INSERT INTO query_logs(client_ip,query_name,query_type,upstream,result_list_name,blocked,coalesced_count) VALUES(?,?,'A',?,?,?,?)`, row.ip, row.name, row.upstream, row.source, row.blocked, row.count); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO query_logs(timestamp,client_ip,query_name,query_type,upstream,coalesced_count) VALUES('2000-01-01 00:00:00','192.0.2.1','nas.example.','A','rewrite',100)`); err != nil {
		t.Fatal(err)
	}
	for _, window := range []string{"5m", "1h", "7d", "24h"} {
		got := statsResponse[[]RewriteStatsView](t, server, "/rewrites?window="+window)
		want := []RewriteStatsView{{Domain: "nas.example", Hits: 5, UniqueClients: 2, TopClients: []RewriteTopClient{{IP: "192.0.2.1", Count: 3}, {IP: "192.0.2.2", Count: 2}}}, {Domain: "other.example", Hits: 1, UniqueClients: 1, TopClients: []RewriteTopClient{{IP: "192.0.2.1", Count: 1}}}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s rewrite stats=%#v want=%#v", window, got, want)
		}
	}
	sources := statsResponse[[]DashboardBlockSource](t, server, "/sources")
	if want := []DashboardBlockSource{{ListName: "Published List", Count: 6, Percentage: 75}, {ListName: "Other List", Count: 2, Percentage: 25}}; !reflect.DeepEqual(sources, want) {
		t.Fatalf("sources=%#v want=%#v", sources, want)
	}
	upstreams := statsResponse[[]DashboardUpstreamUsage](t, server, "/upstreams")
	if want := []DashboardUpstreamUsage{{Upstream: "rewrite", Count: 6, Percentage: 75}, {Upstream: "192.0.2.53:53", Count: 2, Percentage: 25}}; !reflect.DeepEqual(upstreams, want) {
		t.Fatalf("upstreams=%#v want=%#v", upstreams, want)
	}
	if _, err := db.Exec(`INSERT INTO blocklists(id,url,alias,enabled) VALUES(91,'https://example.test/list','Readable name',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO blocklist_history(blocklist_id,previous_count,new_count,added_count,removed_count,sample_added,sample_removed) VALUES(91,5,6,3,2,'a.example, b.example, ,c.example',' old.example,other.example ')`); err != nil {
		t.Fatal(err)
	}
	history := statsResponse[[]BlocklistHistoryView](t, server, "/history")
	if len(history) != 1 {
		t.Fatalf("history=%#v", history)
	}
	h := history[0]
	if h.BlocklistID != 91 || h.BlocklistAlias != "Readable name" || h.PreviousCount != 5 || h.NewCount != 6 || h.AddedCount != 3 || h.RemovedCount != 2 || !reflect.DeepEqual(h.SampleAdded, []string{"a.example", "b.example", "c.example"}) || !reflect.DeepEqual(h.SampleRemoved, []string{"old.example", "other.example"}) {
		t.Fatalf("history values=%#v", h)
	}
}

func TestStatsHTTPStorageFailuresRemainFailures(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	mux := http.NewServeMux()
	for path, handler := range map[string]http.HandlerFunc{
		"/timeseries": handleAPIStatsTimeseries, "/clients": handleAPIStatsTopClients, "/sources": handleAPIStatsBlockSources, "/upstreams": handleAPIStatsUpstreamUsage, "/domains": handleAPIStatsTopDomains, "/latency": handleAPIStatsLatency, "/servfails": handleAPIStatsServfails, "/rewrites": handleAPIRewritesStats, "/history": handleAPIBlocklistHistory,
	} {
		mux.HandleFunc(path, handler)
	}
	server := httptest.NewServer(mux)
	defer server.Close()
	server.Client().Timeout = 2 * time.Second
	checkTestClose(t, readDB)
	for _, path := range []string{"/timeseries", "/timeseries?buckets=1", "/clients", "/sources", "/upstreams", "/domains", "/latency", "/servfails", "/rewrites", "/history"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		checkTestClose(t, response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s status=%d body=%s", path, response.StatusCode, body)
		}
		var envelope struct {
			Data  json.RawMessage `json:"data"`
			Error string          `json:"error"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope.Data) != "null" || envelope.Error == "" {
			t.Errorf("%s falsely reported successful data: %s", path, body)
		}
	}
}
