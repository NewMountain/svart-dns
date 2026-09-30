package svart

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestAdminShutdownWaitsForAcceptedMutation(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	token, _, err := createAPIToken(t.Name(), RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	checkTestClose(t, listener)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- startAdminServer(ctx, address) }()
	t.Cleanup(cancel)
	var conn net.Conn
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err = net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("admin listener not ready: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() { checkTestClose(t, conn) })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"Accepted before shutdown"}`
	if _, err := fmt.Fprintf(conn, "POST /api/groups HTTP/1.1\r\nHost: %s\r\nX-Api-Key: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nExpect: 100-continue\r\n\r\n", address, token, len(body)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, response.Body)
	if response.StatusCode != http.StatusContinue {
		t.Fatalf("request did not reach body reader: %d", response.StatusCode)
	}
	cancel()
	returnedEarly := false
	select {
	case err := <-done:
		returnedEarly = true
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("server error: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
	}
	// Finish the already accepted request even on the failing baseline, so the
	// test does not abandon its handler or race database teardown.
	if _, err := fmt.Fprint(conn, body); err != nil {
		t.Fatal(err)
	}
	response, err = http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkTestClose(t, response.Body)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("accepted mutation failed during drain: %d", response.StatusCode)
	}
	if !returnedEarly {
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("server did not finish draining")
		}
	}
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_groups WHERE name='Accepted before shutdown'`, 1)
	if returnedEarly {
		t.Fatal("admin server returned before its accepted mutation drained; main could close the database underneath it")
	}
}
