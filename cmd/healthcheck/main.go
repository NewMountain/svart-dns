// Command healthcheck probes the local administrative health endpoint.
package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func runHealthcheck(getenv func(string) string, client *http.Client, diagnostics io.Writer) int {
	port := getenv("ADMIN_PORT")
	if port == "" {
		port = "3000"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		if _, err := fmt.Fprintln(diagnostics, "healthcheck ADMIN_PORT is invalid"); err != nil {
			return 1
		}
		return 1
	}

	for _, scheme := range []string{"https", "http"} {
		response, err := client.Get(scheme + "://127.0.0.1:" + strconv.Itoa(portNumber) + "/health")
		if err != nil {
			if _, err := fmt.Fprintf(diagnostics, "healthcheck %s request failed\n", scheme); err != nil {
				return 1
			}
			continue
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return 1
		}
		if response.StatusCode == http.StatusOK {
			return 0
		}
		if _, err := fmt.Fprintf(diagnostics, "healthcheck %s returned status %d\n", scheme, response.StatusCode); err != nil {
			return 1
		}
	}
	return 1
}

func main() {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("default HTTP transport cannot configure local health TLS")
	}
	transport := base.Clone()
	transport.TLSClientConfig = &tls.Config{
		// This process connects only to the numeric loopback address inside its
		// own container; production's wildcard certificate cannot match that
		// name. External release verification still performs strict TLS checks.
		InsecureSkipVerify: true, //nolint:gosec
		MinVersion:         tls.VersionTLS12,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	os.Exit(runHealthcheck(os.Getenv, client, os.Stderr))
}
