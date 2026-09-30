// This executable verifies release identity with VCS metadata disabled.
package main

import (
	"os"

	"github.com/yeti/svart-dns/internal/telemetry"
)

func main() {
	if _, err := os.Stdout.WriteString(telemetry.Version() + "\n"); err != nil {
		os.Exit(1)
	}
}
