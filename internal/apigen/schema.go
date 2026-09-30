// Package apigen generates the checked-in API contract from Go wire types.
package apigen

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Schema represents the JSON Schema subset supported by API code generation.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Format               string             `json:"format,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Description          string             `json:"description,omitempty"`
}

// Types tracks reflected Go wire types and their reusable schema definitions.
type Types struct {
	Schemas map[string]*Schema
	names   map[string]reflect.Type
}

// NewTypes creates an empty schema registry.
func NewTypes() *Types {
	return &Types{Schemas: map[string]*Schema{}, names: map[string]reflect.Type{}}
}
func exported(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
func ref(name string) *Schema    { return &Schema{Ref: "#/components/schemas/" + name} }
func nullable(s *Schema) *Schema { return &Schema{AnyOf: []*Schema{s, {Type: "null"}}} }

// Add registers a Go wire type and returns its schema or reusable reference.
func (g *Types) Add(t reflect.Type) *Schema {
	if t == reflect.TypeFor[json.RawMessage]() {
		if _, ok := g.Schemas["JSONValue"]; !ok {
			g.Schemas["JSONValue"] = &Schema{AnyOf: []*Schema{{Type: "null"}, {Type: "boolean"}, {Type: "number"}, {Type: "string"}, {Type: "array", Items: ref("JSONValue")}, {Type: "object", AdditionalProperties: ref("JSONValue")}}}
		}
		return ref("JSONValue")
	}
	if t == reflect.TypeFor[time.Time]() {
		return &Schema{Type: "string", Format: "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		return nullable(g.Add(t.Elem()))
	}
	if t.Name() != "" && t.PkgPath() != "" {
		name := exported(t.Name())
		if previous, ok := g.names[name]; ok && previous != t {
			panic(fmt.Sprintf("API schema name collision: %s and %s", previous, t))
		}
		g.names[name] = t
		if _, ok := g.Schemas[name]; !ok {
			g.Schemas[name] = nil
			g.Schemas[name] = g.shape(t)
		}
		return ref(name)
	}
	return g.shape(t)
}
func (g *Types) shape(t reflect.Type) *Schema {
	switch t.Kind() {
	case reflect.Struct:
		s := &Schema{Type: "object", Properties: map[string]*Schema{}}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			if f.Anonymous && tag[0] == "" {
				panic(fmt.Sprintf("explicit JSON tag required on embedded field %s.%s", t, f.Name))
			}
			s.Properties[name] = g.Add(f.Type)
			if t.Name() == "InvestigationView" && f.Name == "Rows" {
				s.Properties[name] = nullable(&Schema{Type: "array", Items: nullable(&Schema{Type: "array", Items: ref("JSONValue")})})
			}
			optional := false
			for _, part := range tag[1:] {
				optional = optional || part == "omitempty" || part == "omitzero"
			}
			if optional && (f.Type.Kind() == reflect.Pointer || f.Type.Kind() == reflect.Slice || f.Type.Kind() == reflect.Map) {
				shape := s.Properties[name]
				if len(shape.AnyOf) == 2 && shape.AnyOf[1].Type == "null" {
					s.Properties[name] = shape.AnyOf[0]
				}
			}
			if !optional {
				s.Required = append(s.Required, name)
			}
		}
		sort.Strings(s.Required)
		return s
	case reflect.String:
		return &Schema{Type: "string"}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &Schema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &Schema{Type: "number"}
	case reflect.Slice:
		return nullable(&Schema{Type: "array", Items: g.Add(t.Elem())})
	case reflect.Array:
		return &Schema{Type: "array", Items: g.Add(t.Elem())}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("unsupported JSON map key %s", t))
		}
		return nullable(&Schema{Type: "object", AdditionalProperties: g.Add(t.Elem())})
	default:
		panic(fmt.Sprintf("untyped or unsupported API value %s", t))
	}
}

func names[V any](values map[string]V) []string {
	result := make([]string, 0, len(values))
	for k := range values {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
func schemaName(s *Schema) string { return strings.TrimPrefix(s.Ref, "#/components/schemas/") }
func required(s *Schema, name string) bool {
	for _, n := range s.Required {
		if n == name {
			return true
		}
	}
	return false
}
