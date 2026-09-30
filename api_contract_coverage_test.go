package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/yeti/svart-dns/internal/apigen"
)

func TestAPIContractRegisteredRouteCoverage(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ops := apiOperations()
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "HandleFunc" {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok {
			return true
		}
		path, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(path, "/api/") || path == "/api/openapi.json" {
			return true
		}
		for _, op := range ops {
			if op.Path == path || strings.HasSuffix(path, "/") && strings.HasPrefix(op.Path, path) {
				return true
			}
		}
		t.Errorf("registered API route absent from generated contract: %s", path)
		return true
	})
}

func TestAPIContractTypedSchemasAndErrors(t *testing.T) {
	source, err := apigen.ReadSource(".")
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := apigen.Build(apiOperations(), source)
	if err != nil {
		t.Fatal(err)
	}
	codes := doc.Components.Schemas["APIError"].Properties["error_code"].Enum
	for status := 400; status <= 599; status++ {
		found := false
		for _, code := range codes {
			found = found || code == apiErrorCode(status)
		}
		if !found {
			t.Fatalf("status %d error code missing", status)
		}
	}
	required := map[string][]string{"/api/setup": {"get", "post"}, "/api/stats/dashboard": {"get"}, "/api/blocklists/history/{historyId}/domains": {"get"}, "/api/peers/pair": {"post"}, "/api/peers/confirm": {"post"}, "/api/sync/pair/complete": {"post"}}
	for path, methods := range required {
		for _, method := range methods {
			endpoint, ok := doc.Paths[path][method]
			if !ok {
				t.Fatalf("missing %s %s", method, path)
			}
			for code, response := range endpoint.Responses {
				schema := response.Content["application/json"].Schema
				if schema == nil || schema.Ref == "" {
					t.Fatalf("untyped %s %s %s", method, path, code)
				}
			}
		}
	}
	for _, path := range []string{"/api/investigate", "/api/investigate/schema"} {
		method := "get"
		if path == "/api/investigate" {
			method = "post"
		}
		if _, ok := doc.Paths[path][method].Responses["503"]; !ok {
			t.Fatalf("missing archive unavailable schema for %s", path)
		}
	}
	for _, status := range []string{"408", "422"} {
		if _, ok := doc.Paths["/api/investigate"]["post"].Responses[status]; !ok {
			t.Fatalf("missing investigation %s", status)
		}
	}
}

func TestPublicAPIDocumentation(t *testing.T) {
	hasUsers.Store(true)
	defer hasUsers.Store(false)
	mux := newAdminMux()
	for _, tc := range []struct{ path, contentType, contains string }{{"/docs", "text/html", "/api/openapi.json"}, {"/api/openapi.json", "application/json", `"openapi": "3.1.0"`}, {"/openapi.json", "application/json", `"openapi": "3.1.0"`}, {"/docs/swagger.json", "application/json", `"openapi": "3.1.0"`}, {"/docs.md", "text/markdown", "# Svart API reference"}, {"/static/rapidoc-min.js", "application/javascript", "RapiDoc"}} {
		t.Run(tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), tc.contentType) || !strings.Contains(rr.Body.String(), tc.contains) {
				t.Fatalf("public docs: status=%d type=%s expected marker=%q", rr.Code, rr.Header().Get("Content-Type"), tc.contains)
			}
		})
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if rr.Code != 401 {
		t.Fatalf("public docs opened protected API: %d", rr.Code)
	}
	raw, err := os.ReadFile("docs/swagger.json")
	if err != nil {
		t.Fatal(err)
	}
	var document apigen.Document
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Paths) < 70 {
		t.Fatalf("unexpected route count %d", len(document.Paths))
	}
}

func TestAPIContractRequestUnknownFieldsRemainAllowed(t *testing.T) {
	source, err := apigen.ReadSource(".")
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := apigen.Build(apiOperations(), source)
	if err != nil {
		t.Fatal(err)
	}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			if op.RequestBody == nil {
				continue
			}
			name := strings.TrimPrefix(op.RequestBody.Content["application/json"].Schema.Ref, "#/components/schemas/")
			schema := doc.Components.Schemas[name]
			// An absent additionalProperties permits unknown fields in OpenAPI 3.1.
			if schema.Type != "object" || schema.AdditionalProperties != nil {
				t.Errorf("%s %s has incompatible unknown-field policy", method, path)
			}
		}
	}
}
