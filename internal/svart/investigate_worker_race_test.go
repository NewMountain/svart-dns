//go:build race

package svart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Keep the HTTP/DNS parent race-instrumented while exercising the exact service
// worker. TSAN's terabytes of writable shadow mappings are incompatible with a
// fixed RLIMIT_DATA; increasing the production budget for tests would test a
// different resource boundary. This build uses only the already-resolved module
// cache and embedded local assets; there is no network dependency.
func TestMain(m *testing.M) {
	if err := setTestRepositoryRoot(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "svart-investigate-race-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary := filepath.Join(dir, "svart-dns")
	cmd := exec.Command("go", "build", "-race=false", "-p=4", "-o", binary, "./cmd/svart-dns")
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOMAXPROCS=4")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build investigation worker:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	investigateExecutable = func() (string, error) { return binary, nil }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
