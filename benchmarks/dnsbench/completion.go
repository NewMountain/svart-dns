package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CompletionSnapshot retains an observation after the DNS workers finish. The
// timestamp interval exposes observer lag; it is not an atomic server snapshot.
type CompletionSnapshot struct {
	RequestStartedUnixNS    int64  `json:"request_started_unix_ns"`
	ResponseCompletedUnixNS int64  `json:"response_completed_unix_ns"`
	StatusCode              int    `json:"status_code"`
	Body                    string `json:"body"`
	Error                   string `json:"error,omitempty"`
}

func captureCompletion(url string) (*CompletionSnapshot, error) {
	if url == "" {
		return nil, nil
	}
	snapshot := &CompletionSnapshot{RequestStartedUnixNS: time.Now().UnixNano()}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url) // #nosec G107 -- Optional operator-selected benchmark metrics endpoint.
	if err == nil {
		snapshot.StatusCode = response.StatusCode
		body, readErr := io.ReadAll(response.Body)
		snapshot.Body = string(body)
		err = errors.Join(readErr, response.Body.Close())
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			err = errors.Join(err, fmt.Errorf("completion snapshot HTTP %d", response.StatusCode))
		}
	}
	snapshot.ResponseCompletedUnixNS = time.Now().UnixNano()
	if err != nil {
		snapshot.Error = err.Error()
	}
	return snapshot, err
}
