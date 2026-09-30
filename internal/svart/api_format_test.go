package svart

import (
	"bytes"
	"os/exec"
	"testing"
)

// Generated contract and mutation-proof comparisons must use the same pinned
// formatter; formatting alone must not masquerade as an operation shape change.
func formatGeneratedTypeScript(t *testing.T, path string, content []byte) []byte {
	t.Helper()
	// #nosec G204 -- Test runner executes the known fixture program directly with separate arguments; no shell is involved.
	formatter := exec.Command("node", "frontend/node_modules/prettier/bin/prettier.cjs", "--stdin-filepath", path)
	formatter.Stdin = bytes.NewReader(content)
	var stderr bytes.Buffer
	formatter.Stderr = &stderr
	formatted, err := formatter.Output()
	if err != nil {
		t.Fatalf("format generated TypeScript (run npm ci in frontend first): %v: %s", err, stderr.String())
	}
	return formatted
}
