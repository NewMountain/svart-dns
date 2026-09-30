//go:build !race

package svart

import (
	"fmt"
	"os"
	"testing"
)

// Tests use repository-owned fixtures and generator outputs from the repository root.
func TestMain(m *testing.M) {
	if err := setTestRepositoryRoot(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
