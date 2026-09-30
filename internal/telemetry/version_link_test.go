package telemetry

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestVersionFromReleaseLinkerWithoutVCS(t *testing.T) {
	const revision = "0123456789abcdef0123456789abcdef01234567"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "-p=1", "-buildvcs=false", "-ldflags=-X github.com/yeti/svart-dns/internal/telemetry.buildRevision="+revision, "./testdata/version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build/run release fixture: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != revision {
		t.Fatalf("release identity=%q want %q", got, revision)
	}
}
