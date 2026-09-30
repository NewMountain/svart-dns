package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func response(status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader("ok")),
		Header:     make(http.Header),
	}
}

func TestRunHealthcheckPrefersHTTPS(t *testing.T) {
	var urls []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		urls = append(urls, request.URL.String())
		return response(http.StatusOK), nil
	})}

	if code := runHealthcheck(func(string) string { return "443" }, client, io.Discard); code != 0 {
		t.Fatalf("runHealthcheck() = %d, want 0", code)
	}
	if got, want := strings.Join(urls, ","), "https://127.0.0.1:443/health"; got != want {
		t.Fatalf("requested URLs = %q, want %q", got, want)
	}
}

func TestRunHealthcheckFallsBackToHTTP(t *testing.T) {
	var urls []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		urls = append(urls, request.URL.String())
		if request.URL.Scheme == "https" {
			return nil, errors.New("TLS is not configured")
		}
		return response(http.StatusOK), nil
	})}

	if code := runHealthcheck(func(string) string { return "3000" }, client, io.Discard); code != 0 {
		t.Fatalf("runHealthcheck() = %d, want 0", code)
	}
	if got, want := strings.Join(urls, ","), "https://127.0.0.1:3000/health,http://127.0.0.1:3000/health"; got != want {
		t.Fatalf("requested URLs = %q, want %q", got, want)
	}
}

func TestRunHealthcheckRejectsBadPortBeforeRequest(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		called = true
		return response(http.StatusOK), nil
	})}

	if code := runHealthcheck(func(string) string { return "443/steal" }, client, io.Discard); code == 0 {
		t.Fatal("runHealthcheck() accepted an invalid port")
	}
	if called {
		t.Fatal("invalid port triggered a request")
	}
}

func TestRunHealthcheckFailsClosed(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return response(http.StatusServiceUnavailable), nil
	})}

	if code := runHealthcheck(func(string) string { return "" }, client, io.Discard); code == 0 {
		t.Fatal("runHealthcheck() succeeded when both health endpoints failed")
	}
}

func TestRunHealthcheckReportsOnlyCategoricalFailureDetails(t *testing.T) {
	const sensitiveError = "proxy rejected https://user:secret@example.test"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme == "https" {
			return nil, errors.New(sensitiveError)
		}
		return response(http.StatusServiceUnavailable), nil
	})}
	var diagnostics bytes.Buffer

	if code := runHealthcheck(func(string) string { return "443" }, client, &diagnostics); code == 0 {
		t.Fatal("runHealthcheck() succeeded when both health endpoints failed")
	}
	if got, want := diagnostics.String(), "healthcheck https request failed\nhealthcheck http returned status 503\n"; got != want {
		t.Fatalf("diagnostics = %q, want %q", got, want)
	}
	if strings.Contains(diagnostics.String(), sensitiveError) {
		t.Fatal("diagnostics disclosed a raw request error")
	}
}

type failingBody struct{ readErr, closeErr error }

func (b failingBody) Read([]byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return 0, io.EOF
}
func (b failingBody) Close() error { return b.closeErr }
func TestRunHealthcheckRejectsIncompleteResponses(t *testing.T) {
	failure := errors.New("response interrupted")
	for _, body := range []failingBody{{readErr: failure}, {closeErr: failure}} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})}
		if code := runHealthcheck(func(string) string { return "3000" }, client, io.Discard); code != 1 {
			t.Errorf("incomplete response reported healthy: %d", code)
		}
		if calls != 1 {
			t.Errorf("incomplete response attempted %d requests", calls)
		}
	}
}
