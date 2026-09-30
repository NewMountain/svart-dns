package apigen

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Examples contain invented documentation values, never environment data.
func (g *Types) example(s *Schema, field string) any {
	if s.Ref != "" {
		if schemaName(s) == "JSONValue" {
			return nil
		}
		return g.example(g.Schemas[schemaName(s)], field)
	}
	if len(s.Enum) > 0 {
		return s.Enum[0]
	}
	if len(s.AnyOf) > 0 {
		return g.example(s.AnyOf[0], field)
	}
	switch s.Type {
	case "null":
		return nil
	case "boolean":
		return true
	case "integer", "number":
		return 1
	case "array":
		return []any{}
	case "object":
		result := map[string]any{}
		for _, name := range names(s.Properties) {
			if required(s, name) {
				result[name] = g.example(s.Properties[name], name)
			}
		}
		return result
	case "string":
		if s.Format == "date-time" || strings.Contains(field, "timestamp") {
			return "2026-01-01T00:00:00Z"
		}
		switch field {
		case "domain", "query_name":
			return "example.com"
		case "ip", "client_ip", "ip_addresses":
			return "192.0.2.10"
		case "cidr":
			return "192.0.2.0/24"
		case "url", "peer_url", "self_url":
			return "https://example.com/list.txt"
		case "upstream", "server":
			return "192.0.2.53"
		case "password":
			return "example-password-not-a-secret"
		case "token", "pairing_code", "proof":
			return "EXAMPLE-NOT-A-VALID-CREDENTIAL"
		case "username":
			return "operator"
		case "sql":
			return "SELECT COUNT(*) AS queries FROM query_logs"
		case "role":
			return "readonly"
		case "status":
			return "ok"
		case "name", "alias":
			return "Example"
		case "error":
			return "Data unavailable; retry the request"
		default:
			return ""
		}
	default:
		panic("missing example type")
	}
}
func (g *Types) addExamples(doc *Document, codes map[int]string) error {
	for _, methods := range doc.Paths {
		for key, endpoint := range methods {
			if endpoint.RequestBody != nil {
				for kind, content := range endpoint.RequestBody.Content {
					encoded, err := json.Marshal(g.requestExample(content.Schema))
					if err != nil {
						return err
					}
					content.Example = encoded
					endpoint.RequestBody.Content[kind] = content
				}
			}
			for status, response := range endpoint.Responses {
				for kind, content := range response.Content {
					value := g.example(content.Schema, "")
					if schemaName(content.Schema) == "APIError" {
						n, err := strconv.Atoi(status)
						if err != nil {
							return err
						}
						code, ok := codes[n]
						if !ok {
							code = codes[0]
						}
						value = struct {
							Data  *struct{} `json:"data"`
							Error string    `json:"error"`
							Code  string    `json:"error_code"`
						}{Error: response.Description, Code: code}
					}
					encoded, err := json.Marshal(value)
					if err != nil {
						return err
					}
					content.Example = encoded
					response.Content[kind] = content
				}
				endpoint.Responses[status] = response
			}
			methods[key] = endpoint
		}
	}
	return nil
}

func (g *Types) requestExample(s *Schema) any {
	value := g.example(s, "")
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	for key := range object {
		switch key {
		case "domains":
			object[key] = []string{"example.com"}
		case "blocklist_ids":
			object[key] = []int{1, 2}
		case "client_ips":
			object[key] = []string{"192.0.2.10"}
		case "servers":
			object[key] = []string{"192.0.2.53"}
		case "rewrites":
			object[key] = []map[string]any{{"domain": "router.example", "ip_addresses": "192.0.2.1", "enabled": true}}
		}
	}
	if len(object) == 0 {
		shape := s
		if shape.Ref != "" {
			shape = g.Schemas[schemaName(shape)]
		}
		for _, name := range []string{"name", "alias", "domain", "value"} {
			if field, ok := shape.Properties[name]; ok {
				object[name] = g.example(field, name)
				break
			}
		}
	}
	return object
}
