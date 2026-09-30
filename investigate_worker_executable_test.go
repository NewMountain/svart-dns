package main

import "os"

// Package verification can point the test parent at the actual installed
// executable, so UID/container checks exercise its worker entrypoint too.
func init() {
	if executable := os.Getenv("SVART_TEST_INVESTIGATE_WORKER"); executable != "" {
		investigateExecutable = func() (string, error) { return executable, nil }
	}
}
