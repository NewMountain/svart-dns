// Package assets embeds the application UI, API documentation, and local DuckDB extension.
package assets

import "embed"

// FrontendDist contains the built web UI.
//
//go:embed all:frontend/dist
var FrontendDist embed.FS

// DocsHTML is the self-hosted API reference page.
//
//go:embed web/static/docs.html
var DocsHTML string

// RapidocJS is the bundled API reference viewer.
//
//go:embed web/static/rapidoc-min.js
var RapidocJS string

// SwaggerJSON contains the generated OpenAPI contract.
//
//go:embed docs/swagger.json
var SwaggerJSON string

// APIMarkdown contains the generated API reference.
//
//go:embed docs/api-reference.md
var APIMarkdown string

// SQLiteScannerExtension is the pinned local DuckDB SQLite scanner.
//
//go:embed web/duckdb-extensions/sqlite_scanner.duckdb_extension
var SQLiteScannerExtension []byte

// InvestigationTemplates contains the UI queries validated by application tests.
//
//go:embed frontend/src/pages/investigation-templates.json
var InvestigationTemplates []byte
