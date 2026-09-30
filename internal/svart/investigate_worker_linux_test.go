//go:build linux

package svart

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func investigationChildPIDs(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("/proc/self/task/*/children")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, file := range files {
		// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, pid := range strings.Fields(string(data)) {
			// #nosec G304 G703 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			command, err := os.ReadFile("/proc/" + pid + "/cmdline")
			if err == nil && strings.Contains(string(command), investigateWorkerArgument) {
				found[pid] = true
			}
		}
	}
	pids := []string{}
	for pid := range found {
		pids = append(pids, pid)
	}
	return pids
}

func TestInvestigateWorkerBoundsIntermediateMemoryAndReaps(t *testing.T) {
	setupInvestigateSandbox(t)
	rewritesCache.Load().Store("printer.home.arpa", Rewrite{Domain: "printer.home.arpa", IPAddresses: []string{"192.168.1.40"}})
	udp, tcp := secdnsServe(t)
	var before, previousChildren syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &before); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &previousChildren); err != nil {
		t.Fatal(err)
	}
	// RUSAGE_CHILDREN is cumulative; race TestMain also built the worker.
	// Linux also accounts fork-before-exec inherited parent RSS. The
	// standalone non-race run starts below600MiB and asserts that ceiling.
	childCeiling := max(int64(600*1024), previousChildren.Maxrss)
	queries := []string{
		`SELECT length(repeat(query_name, 30000000)) FROM query_logs LIMIT 1`,
		`SELECT array_length(range(0, 100000000 + length(query_name))) FROM query_logs LIMIT 1`,
		`SELECT length(string_agg(repeat(query_name, 10000000), '')) FROM query_logs`,
		`WITH traffic AS (SELECT query_name FROM query_logs WHERE client_ip LIKE '10.42.1.%') SELECT array_length(list(repeat(a.query_name, 10000))) FROM traffic a, traffic b, traffic c, traffic d, traffic e`,
	}
	for repetition := 0; repetition < 2; repetition++ {
		for index, query := range queries {
			body, err := json.Marshal(map[string]interface{}{"sql": query, "timeout": 5})
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			begin := time.Now()
			go func() {
				response := httptest.NewRecorder()
				handleAPIInvestigate(response, httptest.NewRequest("POST", "/api/investigate", strings.NewReader(string(body))))
				done <- response
			}()
			ticker := time.NewTicker(5 * time.Millisecond)
			seenChild := false
			probes := 0
			var worstDNS time.Duration
			var response *httptest.ResponseRecorder
		waiting:
			for {
				select {
				case response = <-done:
					break waiting
				case <-ticker.C:
					if len(investigationChildPIDs(t)) > 0 {
						seenChild = true
					}
					for _, endpoint := range []struct{ network, address string }{{"udp", udp}, {"tcp", tcp}} {
						question := new(dns.Msg)
						question.SetQuestion("printer.home.arpa.", dns.TypeA)
						start := time.Now()
						answer := secdnsExchange(t, endpoint.network, endpoint.address, question)
						elapsed := time.Since(start)
						if elapsed > worstDNS {
							worstDNS = elapsed
						}
						if answer.Rcode != dns.RcodeSuccess || len(answer.Answer) != 1 || requireFixtureType[*dns.A](t, answer.Answer[0]).A.String() != "192.168.1.40" {
							t.Fatalf("DNS answer=%v", answer)
						}
						probes++
					}
				case <-time.After(8 * time.Second):
					t.Fatal("worker exceeded deadline")
				}
			}
			ticker.Stop()
			if !seenChild || probes < 2 {
				t.Fatalf("worker/DNS overlap not observed: child=%v probes=%d", seenChild, probes)
			}
			if response.Code != 422 {
				t.Fatalf("query %d status=%d body=%s", index, response.Code, response.Body.String())
			}
			var reply apiResponse
			if err := json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
			if reply.Error == nil || *reply.Error != "investigation resource limit exceeded; select fewer columns, aggregate, or page a smaller result" || reply.Data != nil {
				t.Fatalf("unexpected rejection %s", response.Body.String())
			}
			if pids := investigationChildPIDs(t); len(pids) != 0 {
				t.Fatalf("worker not reaped: %v", pids)
			}
			var parent, children syscall.Rusage
			if err := syscall.Getrusage(syscall.RUSAGE_SELF, &parent); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children); err != nil {
				t.Fatal(err)
			}
			if parent.Maxrss-before.Maxrss > 128*1024 {
				t.Fatalf("parent RSS grew %dKiB", parent.Maxrss-before.Maxrss)
			}
			if children.Maxrss > max(childCeiling, parent.Maxrss) {
				t.Fatalf("child peak RSS=%dKiB exceeds600MiB plus inherited process peak=%dKiB", children.Maxrss, max(previousChildren.Maxrss, parent.Maxrss))
			}
			if worstDNS > 200*time.Millisecond {
				t.Fatalf("worst DNS=%v exceeds200ms", worstDNS)
			}
			t.Logf("repetition=%d query=%d HTTP422 duration=%v parent_peak=%dKiB cumulative_child_peak=%dKiB DNS_probes=%d worst_DNS=%v live_children=0", repetition, index, time.Since(begin), parent.Maxrss, children.Maxrss, probes, worstDNS)
		}
	}
	_, rows, _, err := investigateQuery(`SELECT count(*) FROM query_logs WHERE client_ip LIKE '10.42.1.%'`, 5)
	if err != nil || fmt.Sprint(rows) != "[[5]]" {
		t.Fatalf("recovery=%v err=%v", rows, err)
	}
}

func TestInvestigateWorkerCancelsActiveRequestAndReaps(t *testing.T) {
	setupInvestigateSandbox(t)
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<500)
 INSERT INTO query_logs (client_ip,query_name,query_type,coalesced_count)
 SELECT '192.0.2.42','www.example.com','A',i FROM n`); err != nil {
		t.Fatal(err)
	}
	for repetition := 0; repetition < 3; repetition++ {
		ctx, cancel := context.WithCancel(context.Background())
		request := httptest.NewRequest("POST", "/api/investigate", strings.NewReader(`{"sql":"WITH traffic AS (SELECT coalesced_count FROM query_logs) SELECT SUM(sin(a.coalesced_count*b.coalesced_count+c.coalesced_count*d.coalesced_count)) FROM traffic a, traffic b, traffic c, traffic d","timeout":30}`)).WithContext(ctx)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { response := httptest.NewRecorder(); handleAPIInvestigate(response, request); done <- response }()
		ticker := time.NewTicker(5 * time.Millisecond)
		deadline := time.NewTimer(3 * time.Second)
		ready := false
		for !ready {
			select {
			case <-ticker.C:
				for _, pid := range investigationChildPIDs(t) {
					// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
					data, fixtureErr6618 := os.ReadFile("/proc/" + pid + "/limits")
					if fixtureErr6618 != nil {
						if errors.Is(fixtureErr6618, os.ErrNotExist) {
							continue
						}
						t.Fatalf("read live worker limits: %v", fixtureErr6618)
					}
					if strings.Contains(string(data), "402653184") {
						ready = true
					}
				}
			case <-deadline.C:
				cancel()
				t.Fatal("worker did not install kernel limit")
			}
		}
		ticker.Stop()
		deadline.Stop()
		begin := time.Now()
		cancel()
		select {
		case response := <-done:
			if response.Code != http.StatusRequestTimeout {
				t.Fatalf("canceled status=%d body=%s", response.Code, response.Body.String())
			}
			if pids := investigationChildPIDs(t); len(pids) != 0 {
				t.Fatalf("canceled child remains %v", pids)
			}
			t.Logf("cancellation=%v live_children=0", time.Since(begin))
		case <-time.After(time.Second):
			t.Fatal("cancellation did not reap worker within1s")
		}
	}
	_, rows, _, err := investigateQuery(`SELECT count(*) FROM query_logs`, 5)
	if err != nil || fmt.Sprint(rows) != "[[505]]" {
		t.Fatalf("recovery=%v err=%v", rows, err)
	}
}

func TestInvestigateWorkerHTTPDisconnectReaps(t *testing.T) {
	setupInvestigateSandbox(t)
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<500)
 INSERT INTO query_logs (client_ip,query_name,query_type,coalesced_count)
 SELECT '192.0.2.42','www.example.com','A',i FROM n`); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handleAPIInvestigate(w, r); finished <- struct{}{} }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "POST", server.URL, strings.NewReader(`{"sql":"WITH traffic AS (SELECT coalesced_count FROM query_logs) SELECT SUM(sin(a.coalesced_count*b.coalesced_count+c.coalesced_count*d.coalesced_count)) FROM traffic a, traffic b, traffic c, traffic d","timeout":30}`))
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan error, 1)
	go func() {
		response, err := server.Client().Do(request)
		if response != nil {
			checkTestClose(t, response.Body)
		}
		clientDone <- err
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	ready := false
	for !ready {
		select {
		case <-ticker.C:
			for _, pid := range investigationChildPIDs(t) {
				// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
				data, fixtureErr8942 := os.ReadFile("/proc/" + pid + "/limits")
				if fixtureErr8942 != nil {
					if errors.Is(fixtureErr8942, os.ErrNotExist) {
						continue
					}
					t.Fatalf("read live worker limits: %v", fixtureErr8942)
				}
				if strings.Contains(string(data), "402653184") {
					ready = true
				}
			}
		case <-timeout.C:
			t.Fatal("HTTP worker never installed resource limits")
		}
	}
	start := time.Now()
	cancel()
	select {
	case err := <-clientDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP client did not cancel")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("HTTP disconnect did not release worker")
	}
	if investigateBusy.Load() || len(investigationChildPIDs(t)) != 0 {
		t.Fatal("HTTP disconnect retained worker/admission")
	}
	t.Logf("HTTP disconnect released worker and admission in %v", time.Since(start))
}
