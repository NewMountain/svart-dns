package apigen

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Operation binds one HTTP route to its handler and reflected wire types.
type Operation struct {
	Method, Path, Handler string
	Request               reflect.Type
	Status                int
	Response              []reflect.Type
}

// Document is the generated OpenAPI 3.1 contract.
type Document struct {
	OpenAPI    string                         `json:"openapi"`
	Info       Info                           `json:"info"`
	Paths      map[string]map[string]Endpoint `json:"paths"`
	Components Components                     `json:"components"`
}

// Info describes the API in an OpenAPI document.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Components contains reusable schemas and authentication schemes.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes"`
}

// SecurityScheme describes an accepted API credential location and format.
type SecurityScheme struct {
	Type   string `json:"type"`
	In     string `json:"in,omitempty"`
	Name   string `json:"name,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

// Endpoint describes one HTTP operation and its request and response contracts.
type Endpoint struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Parameters  []Parameter           `json:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty"`
	Responses   map[string]Response   `json:"responses"`
	Security    []map[string][]string `json:"security"`
}

// Parameter describes a path or query argument.
type Parameter struct {
	Name     string  `json:"name"`
	In       string  `json:"in"`
	Required bool    `json:"required"`
	Schema   *Schema `json:"schema"`
}

// RequestBody describes the required request media and schema.
type RequestBody struct {
	Required bool             `json:"required"`
	Content  map[string]Media `json:"content"`
}

// Response describes one HTTP status and its response media.
type Response struct {
	Description string           `json:"description"`
	Content     map[string]Media `json:"content"`
}

// Media pairs a schema with an optional JSON example.
type Media struct {
	Schema  *Schema         `json:"schema"`
	Example json.RawMessage `json:"example,omitempty"`
}

// ID returns the stable exported operation name derived from its method and path.
func ID(op Operation) string {
	pieces := strings.FieldsFunc(strings.ToLower(op.Method)+" "+op.Path, func(r rune) bool { return r == '/' || r == '-' || r == '{' || r == '}' || r == ' ' })
	for i, p := range pieces {
		pieces[i] = exported(p)
	}
	return strings.Join(pieces, "")
}
func media(s *Schema) map[string]Media { return map[string]Media{"application/json": {Schema: s}} }

// Build derives the OpenAPI document and schema registry from routes and handler metadata.
func Build(ops []Operation, src Source) (*Document, *Types, error) {
	g := NewTypes()
	codes := []string{}
	seenCodes := map[string]bool{}
	for _, code := range src["apiErrorCode"].Codes {
		if !seenCodes[code] {
			codes = append(codes, code)
			seenCodes[code] = true
		}
	}
	sort.Strings(codes)
	g.Schemas["APIError"] = &Schema{Type: "object", Properties: map[string]*Schema{"data": {Type: "null"}, "error": {Type: "string"}, "error_code": {Type: "string", Enum: codes}}, Required: []string{"data", "error", "error_code"}}

	doc := &Document{OpenAPI: "3.1.0", Info: Info{Title: "Svart DNS API", Version: "1.0.0", Description: "Self-hosted administration, DNS policy, query history, and peer synchronization. Success responses contain data and a null error. Failures contain no partial data and a stable error_code."}, Paths: map[string]map[string]Endpoint{}, Components: Components{Schemas: g.Schemas, SecuritySchemes: map[string]SecurityScheme{"apiKeyAuth": {Type: "apiKey", In: "header", Name: "X-Api-Key"}, "syncKeyAuth": {Type: "apiKey", In: "header", Name: "X-Sync-Key"}, "sessionAuth": {Type: "apiKey", In: "cookie", Name: "svart_session"}}}}
	for _, op := range ops {
		if _, ok := src[op.Handler]; !ok {
			return nil, nil, fmt.Errorf("unknown handler %s", op.Handler)
		}
		meta := src.Reachable(op.Handler)
		id := ID(op)
		if doc.Paths[op.Path] == nil {
			doc.Paths[op.Path] = map[string]Endpoint{}
		}
		method := strings.ToLower(op.Method)
		if _, ok := doc.Paths[op.Path][method]; ok {
			return nil, nil, fmt.Errorf("duplicate %s %s", op.Method, op.Path)
		}
		endpoint := Endpoint{OperationID: id, Summary: meta.Summary, Description: meta.Description, Responses: map[string]Response{}, Security: []map[string][]string{{"apiKeyAuth": {}}, {"sessionAuth": {}}}}
		if endpoint.Summary == "" {
			endpoint.Summary = op.Method + " " + op.Path
		}
		if endpoint.Description == "" {
			endpoint.Description = endpoint.Summary + "."
		}
		public := op.Path == "/health" || op.Path == "/api/setup" || strings.HasPrefix(op.Path, "/api/auth/") && op.Path != "/api/auth/sessions/revoke" || op.Path == "/api/sync/pair/complete"
		if op.Path == "/api/sync" {
			endpoint.Security = []map[string][]string{{"syncKeyAuth": {}}}
		}
		if public {
			endpoint.Security = []map[string][]string{}
		}
		seen := map[string]bool{}
		for _, part := range strings.Split(op.Path, "/") {
			if strings.HasPrefix(part, "{") {
				name := strings.Trim(part, "{}")
				endpoint.Parameters = append(endpoint.Parameters, Parameter{Name: name, In: "path", Required: true, Schema: &Schema{Type: "string"}})
				seen[name] = true
			}
		}
		sort.Strings(meta.Queries)
		for _, name := range meta.Queries {
			if !seen[name] {
				endpoint.Parameters = append(endpoint.Parameters, Parameter{Name: name, In: "query", Schema: &Schema{Type: "string"}})
				seen[name] = true
			}
		}
		if op.Request != nil {
			request := g.request(id+"Body", op.Request)
			endpoint.RequestBody = &RequestBody{Required: true, Content: media(request)}
		}
		alternatives := []*Schema{}
		for i, r := range op.Response {
			shape := g.Add(r)
			alternatives = append(alternatives, shape)
			g.Schemas[fmt.Sprintf("%sVariant%dData", id, i+1)] = shape
		}
		if len(alternatives) == 0 {
			return nil, nil, fmt.Errorf("no response for %s", id)
		}
		result := alternatives[0]
		if len(alternatives) > 1 {
			result = &Schema{AnyOf: alternatives}
		}
		g.Schemas[id+"Data"] = result
		envelope := &Schema{Type: "object", Properties: map[string]*Schema{"data": ref(id + "Data"), "error": {Type: "null"}}, Required: []string{"data", "error"}}
		g.Schemas[id+"Response"] = envelope
		endpoint.Responses[strconv.Itoa(op.Status)] = Response{Description: http.StatusText(op.Status), Content: media(ref(id + "Response"))}
		statuses := append(meta.Statuses, 421)
		if !public {
			if op.Path == "/api/sync" {
				statuses = append(statuses, src.Reachable("syncAuth").Statuses...)
			} else {
				statuses = append(statuses, src.Reachable("apiAuth").Statuses...)
			}
		}
		if op.Request != nil {
			statuses = append(statuses, 400, 413)
		}
		// These worker errors are part of the same contract even when generation
		// runs in a checkout preceding the separately integrated worker repair.
		if op.Path == "/api/investigate" {
			statuses = append(statuses, 408, 422, 503)
		}
		if op.Path == "/api/investigate/schema" {
			statuses = append(statuses, 503)
		}
		for _, status := range statuses {
			endpoint.Responses[strconv.Itoa(status)] = Response{Description: http.StatusText(status), Content: media(ref("APIError"))}
		}
		doc.Paths[op.Path][method] = endpoint
	}
	if err := g.addExamples(doc, src["apiErrorCode"].Codes); err != nil {
		return nil, nil, err
	}
	g.addDocumentation(doc)
	return doc, g, nil
}

// JSON serializes the contract with deterministic indentation and a final newline.
func (d *Document) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	return append(b, '\n'), err
}

// Markdown renders the generated operation and schema reference.
func (d *Document) Markdown() []byte {
	var out strings.Builder
	out.WriteString("# Svart API reference\n\nGenerated from the Go request and response types. Run `make docs`; do not edit.\n\nThe interactive reference is at `/docs`; the OpenAPI 3.1 schema is at `/api/openapi.json`. Documentation is public. Protected API calls require a session cookie or X-Api-Key header; mutations require administrator privileges.\n\nRequest object schemas permit unknown properties, matching the server decoder. Omitted request fields use Go zero values unless marked required by handler validation.\n\nEach success envelope has `data` and a null `error`. Every error envelope has `data: null`, a safe human-readable `error`, and a stable `error_code`. A 503 means data is unavailable; it never represents an empty or partially read success.\n\n")
	for _, path := range names(d.Paths) {
		for _, method := range names(d.Paths[path]) {
			op := d.Paths[path][method]
			fmt.Fprintf(&out, "## %s %s\n\n%s\n\n%s\n\n", strings.ToUpper(method), path, op.Summary, op.Description)
			if len(op.Security) == 0 {
				out.WriteString("Authentication: public endpoint (setup and pairing enforce their own one-time proof).\n\n")
			}
			if len(op.Parameters) > 0 {
				out.WriteString("| Parameter | Location | Required |\n| --- | --- | --- |\n")
				for _, p := range op.Parameters {
					fmt.Fprintf(&out, "| `%s` | %s | %t |\n", p.Name, p.In, p.Required)
				}
				out.WriteString("\n")
			}
			if op.RequestBody != nil {
				fmt.Fprintf(&out, "Request: `%s`.\n\n```json\n%s\n```\n\n", schemaName(op.RequestBody.Content["application/json"].Schema), op.RequestBody.Content["application/json"].Example)
			}
			out.WriteString("| Status | Body | Meaning |\n| --- | --- | --- |\n")
			for _, status := range names(op.Responses) {
				response := op.Responses[status]
				fmt.Fprintf(&out, "| %s | `%s` | %s |\n", status, mediaName(response.Content), response.Description)
			}
			out.WriteString("\n")
			for _, status := range names(op.Responses) {
				if strings.HasPrefix(status, "2") {
					if example := op.Responses[status].Content["application/json"].Example; len(example) > 0 {
						fmt.Fprintf(&out, "Example response (%s):\n\n```json\n%s\n```\n\n", status, example)
					}
				}
			}
		}
	}
	out.WriteString("## Schemas\n\n")
	for _, name := range names(d.Components.Schemas) {
		s := d.Components.Schemas[name]
		fmt.Fprintf(&out, "### %s\n\n```typescript\n%s\n```\n\n", name, tsType(s))
	}
	return []byte(out.String())
}

func (g *Types) addDocumentation(doc *Document) {
	documentSchema := g.Add(reflect.TypeFor[Document]())
	for _, item := range []struct {
		path, content, description string
		schema                     *Schema
	}{
		{"/docs", "text/html", "Self-hosted interactive API reference", &Schema{Type: "string"}},
		{"/docs.md", "text/markdown", "Generated Markdown API reference", &Schema{Type: "string"}},
		{"/api/openapi.json", "application/json", "Generated OpenAPI 3.1 specification", documentSchema},
		{"/openapi.json", "application/json", "OpenAPI specification alias", documentSchema},
		{"/docs/swagger.json", "application/json", "Legacy OpenAPI specification URL", documentSchema},
		{"/static/rapidoc-min.js", "application/javascript", "Self-hosted documentation renderer", &Schema{Type: "string"}},
		{"/metrics", "text/plain", "Prometheus metrics exposition", &Schema{Type: "string"}},
	} {
		doc.Paths[item.path] = map[string]Endpoint{"get": {OperationID: ID(Operation{Method: "GET", Path: item.path}), Summary: item.description, Description: item.description + ". No authentication required.", Security: []map[string][]string{}, Responses: map[string]Response{"200": {Description: "OK", Content: map[string]Media{item.content: {Schema: item.schema}}}, "421": {Description: "Misdirected Request", Content: media(ref("APIError"))}}}}
	}
}

func mediaName(content map[string]Media) string {
	kinds := names(content)
	if len(kinds) == 0 {
		return ""
	}
	kind := kinds[0]
	s := content[kind].Schema
	if s.Ref != "" {
		return schemaName(s)
	}
	return kind + " (" + s.Type + ")"
}
