package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestCompletionSnapshotFollowsAllRepliesAndPreservesResponse(t *testing.T) {
	var replies atomic.Int32
	target := benchmarkUpstream(t, func(w dns.ResponseWriter, req *dns.Msg) {
		response := new(dns.Msg)
		response.SetReply(req)
		replies.Add(1)
		if err := w.WriteMsg(response); err != nil {
			t.Error(err)
		}
	})
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		replies.Store(0)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || replies.Load() != 7 {
				t.Errorf("hook before all replies: method=%s replies=%d", r.Method, replies.Load())
			}
			w.WriteHeader(status)
			if _, err := io.WriteString(w, "queue_length 3\nfull response\n"); err != nil {
				t.Error(err)
			}
		}))
		result, err := captureRun(t, config{target: target, clients: 2, queries: 5, warmup: 2, timeout: time.Second, jsonOut: true, completionURL: server.URL})
		server.Close()
		if (err != nil) != (status != http.StatusOK) {
			t.Fatalf("status=%d error=%v", status, err)
		}
		var summary Summary
		if err := json.Unmarshal([]byte(result), &summary); err != nil {
			t.Fatal(err)
		}
		snapshot := summary.Completion
		if snapshot == nil || snapshot.StatusCode != status || snapshot.Body != "queue_length 3\nfull response\n" || summary.Answered != 5 {
			t.Fatalf("lost response or DNS result: %+v %s", summary, result)
		}
		if snapshot.RequestStartedUnixNS < summary.RepliesCompleteUnixNS || snapshot.ResponseCompletedUnixNS < snapshot.RequestStartedUnixNS {
			t.Fatalf("invalid observation timestamps: %+v", snapshot)
		}
		if (snapshot.Error != "") != (status != http.StatusOK) {
			t.Fatalf("snapshot error=%q", snapshot.Error)
		}
	}
}

func TestCompletionFailureRetainsMeasuredSummary(t *testing.T) {
	result, err := captureRun(t, config{clients: 1, queries: 0, timeout: time.Second, jsonOut: true, completionURL: "http://127.0.0.1:0"})
	var summary Summary
	if decodeErr := json.Unmarshal([]byte(result), &summary); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if err == nil || summary.Completion == nil || summary.Completion.Error == "" || summary.Queries != 0 {
		t.Fatalf("result=%s error=%v", result, err)
	}
}

func TestCompletionRequiresJSONBeforeSendingQueries(t *testing.T) {
	output, err := captureRun(t, config{clients: 1, queries: 1, timeout: time.Second, completionURL: "http://127.0.0.1:0"})
	if err == nil || !strings.Contains(err.Error(), "requires -json") || output != "" {
		t.Fatalf("output=%q error=%v", output, err)
	}
}
