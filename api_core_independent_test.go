package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestIndependentBootstrapIgnoredInsertCannotSucceed(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	localSQL(t, `CREATE TRIGGER independent_bootstrap_ignore BEFORE INSERT ON bootstrap_servers BEGIN SELECT RAISE(IGNORE); END`)
	before := localStoredState(t)
	server := httptest.NewServer(http.HandlerFunc(handleAPIBootstrap))
	defer server.Close()
	response, err := server.Client().Post(server.URL+"/api/bootstrap", "application/json", strings.NewReader(`{"server":"192.0.2.53:53"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { checkTestClose(t, response.Body) }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, localStoredState(t)) {
		t.Fatal("ignored insert unexpectedly changed durable state")
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("ignored bootstrap insert reported HTTP %d, body=%s; complete database is unchanged", response.StatusCode, body)
	}
}

func TestIndependentBootstrapReplacementIgnoredWritesRollback(t *testing.T) {
	for _, fault := range []struct{ name, table, event, when string }{
		{"removal metadata", "sync_tombstones", "INSERT", ""},
		{"old server deletion", "bootstrap_servers", "DELETE", ""},
		{"new server insertion", "bootstrap_servers", "INSERT", ""},
		{"retained tombstone deletion", "sync_tombstones", "DELETE", " WHEN OLD.natural_key='192.0.2.54:53'"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			t.Cleanup(setupTestDB(t))
			useBootstrap(t, "192.0.2.53:53", "192.0.2.54:53")
			localSQL(t, fmt.Sprintf("CREATE TRIGGER independent_bootstrap_ignore BEFORE %s ON %s%s BEGIN SELECT RAISE(IGNORE); END", fault.event, fault.table, fault.when))
			before := localStoredState(t)
			server := httptest.NewServer(http.HandlerFunc(handleAPIBootstrap))
			defer server.Close()
			bodyJSON := `{"servers":["192.0.2.54:53","192.0.2.55:53"]}`
			if fault.event == "DELETE" && fault.table == "bootstrap_servers" {
				bodyJSON = `{"servers":["192.0.2.55:53"]}`
			}
			request, err := http.NewRequest("PUT", server.URL+"/api/bootstrap", strings.NewReader(bodyJSON))
			if err != nil {
				t.Fatal(err)
			}
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { checkTestClose(t, response.Body) }()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			unchanged := reflect.DeepEqual(before, localStoredState(t))
			if response.StatusCode != 503 || !unchanged {
				t.Fatalf("ignored replacement write: HTTP %d body=%s unchanged_complete_database=%t", response.StatusCode, body, unchanged)
			}
		})
	}
}
