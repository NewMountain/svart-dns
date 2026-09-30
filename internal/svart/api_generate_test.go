package svart

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/apigen"
)

var updateAPI = flag.Bool("update-api", false, "regenerate OpenAPI, frontend types/parsers, and API reference")

func TestGeneratedAPIContract(t *testing.T) {
	source, err := apigen.ReadSource("internal/svart")
	if err != nil {
		t.Fatal(err)
	}
	doc, types, err := apigen.Build(apiOperations(), source)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := doc.JSON()
	if err != nil {
		t.Fatal(err)
	}
	artifacts := map[string][]byte{"docs/swagger.json": spec, "docs/api-reference.md": doc.Markdown(), "frontend/src/api/generated.ts": types.TypeScript(), "frontend/src/api/operations.ts": types.Clients(apiOperations())}
	for path, content := range artifacts {
		t.Run(path, func(t *testing.T) {
			if strings.HasSuffix(path, ".ts") {
				content = formatGeneratedTypeScript(t, path, content)
			}
			if *updateAPI {
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
				return
			}
			// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
			existing, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(existing, content) {
				t.Fatalf("generated artifact drift: %s; run make docs", path)
			}
		})
	}
}
