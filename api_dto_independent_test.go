package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/yeti/svart-dns/internal/apigen"
)

type independentChangedGroupRequest struct {
	Name             string `json:"name"`
	IndependentProbe int    `json:"independent_probe"`
}

func TestIndependentConcreteDTOChangeDriftsTypedArtifacts(t *testing.T) {
	operations := apiOperations()
	changed := false
	for i := range operations {
		if operations[i].Path == "/api/groups" && operations[i].Method == "POST" {
			operations[i].Request = wire[independentChangedGroupRequest]()
			changed = true
		}
	}
	if !changed {
		t.Fatal("real group operation absent")
	}
	wireBytes, err := json.Marshal(independentChangedGroupRequest{Name: "Review", IndependentProbe: 7})
	if err != nil || string(wireBytes) != `{"name":"Review","independent_probe":7}` {
		t.Fatalf("actual Go wire=%s err=%v", wireBytes, err)
	}
	source, err := apigen.ReadSource(".")
	if err != nil {
		t.Fatal(err)
	}
	doc, types, err := apigen.Build(operations, source)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := doc.JSON()
	if err != nil {
		t.Fatal(err)
	}
	for path, generated := range map[string][]byte{"docs/swagger.json": spec, "docs/api-reference.md": doc.Markdown(), "frontend/src/api/generated.ts": types.TypeScript()} {
		// #nosec G304 -- Test-owned fixture path or explicitly selected local evidence input; no HTTP input reaches this operation.
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(original, generated) || !bytes.Contains(generated, []byte("independent_probe")) {
			t.Fatalf("Go DTO change absent from %s", path)
		}
	}
	// Operations reference the generated named body, so a field-only DTO change
	// belongs in its concrete body codec rather than changing method/path wrappers.
	original, err := os.ReadFile("frontend/src/api/operations.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(types.Clients(apiOperations()), types.Clients(operations)) {
		t.Fatal("field-only DTO change altered unformatted method/path clients")
	}
	if !bytes.Equal(original, formatGeneratedTypeScript(t, "frontend/src/api/operations.ts", types.Clients(operations))) {
		t.Fatal("field-only DTO change unexpectedly altered method/path clients")
	}
}
