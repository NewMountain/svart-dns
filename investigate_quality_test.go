package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestInvestigationRejectsMalformedStructuralFields(t *testing.T) {
	for _, field := range []string{"cte_map", "cte_entries", "table_catalog", "table_schema", "function_catalog", "function_schema"} {
		t.Run(field, func(t *testing.T) {
			table := map[string]any{"type": "BASE_TABLE", "table_name": "query_logs"}
			function := map[string]any{"class": "FUNCTION", "function_name": "count"}
			node := map[string]any{"type": "SELECT_NODE", "from_table": table, "select_list": []any{function}}
			switch field {
			case "cte_map":
				node["cte_map"] = true
			case "cte_entries":
				node["cte_map"] = map[string]any{"map": 42}
			case "table_catalog":
				table["catalog_name"] = 42
			case "table_schema":
				table["schema_name"] = []any{"private"}
			case "function_catalog":
				function["catalog"] = 42
			case "function_schema":
				function["schema"] = []any{"private"}
			}
			raw, err := json.Marshal(map[string]any{"error": false, "statements": []any{map[string]any{"node": node}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := validateInvestigateAST(raw); !errors.Is(err, errInvalidInvestigateQuery) {
				t.Fatalf("malformed %s silently accepted: %v", field, err)
			}
		})
	}
}

type qualityTransportCloseFailure struct{ net.Conn }

func (c qualityTransportCloseFailure) Close() error {
	return errors.Join(c.Conn.Close(), errors.New("fixture close error for private-query.example from192.0.2.1"))
}

func TestUpstreamPoolCloseFailuresAreObservedPrivately(t *testing.T) {
	for _, operation := range []string{"stale", "full", "retire"} {
		t.Run(operation, func(t *testing.T) {
			server, client := net.Pipe()
			t.Cleanup(func() {
				if err := server.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
				if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
					t.Error(err)
				}
			})
			pc := pooledConn{conn: &dns.Conn{Conn: qualityTransportCloseFailure{Conn: server}}, addr: "192.0.2.53:53", idleSince: time.Now().Add(-2 * streamIdleTimeout)}
			p := &streamPool{}
			var output bytes.Buffer
			oldLogger, oldQueries := logResolver, logQueryLines.Load()
			logResolver = slog.New(slog.NewJSONHandler(&output, nil))
			logQueryLines.Store(false)
			t.Cleanup(func() { logResolver = oldLogger; logQueryLines.Store(oldQueries) })
			switch operation {
			case "stale":
				p.idle = []pooledConn{pc}
				if got, reused := p.take(pc.addr, time.Now()); got != nil || reused {
					t.Fatal("stale connection reused")
				}
			case "full":
				p.closed = true
				p.give(pc)
			case "retire":
				p.idle = []pooledConn{pc}
				p.close()
			}
			var one [1]byte
			if _, err := client.Read(one[:]); !errors.Is(err, io.EOF) {
				t.Fatalf("retired connection remained live: %v", err)
			}
			logs := output.String()
			if !strings.Contains(logs, "upstream transport cleanup failed") || !strings.Contains(logs, "error_class") || strings.Contains(logs, "private-query.example") || strings.Contains(logs, "192.0.2.1") {
				t.Fatalf("cleanup failure missing or leaked query detail: %q", logs)
			}
		})
	}
}
