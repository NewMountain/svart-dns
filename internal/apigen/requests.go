package apigen

import (
	"fmt"
	"reflect"
)

// Go zero values are accepted for omitted optional request fields. Required
// fields below mirror handler validation, rather than response JSON presence.
var requiredRequestFields = map[string][]string{
	"setupRequest":     {"token", "username", "password"},
	"AuthLoginRequest": {"username", "password"},

	"ClientBlockDomainRequest": {"domain"}, "ClientAllowDomainRequest": {"domain"},
	"GroupBlockDomainRequest": {"domain"}, "GroupAllowDomainRequest": {"domain"},
	"PolicyBlockDomainRequest": {"domain"}, "PolicyAllowDomainRequest": {"domain"}, "DomainRuleRequest": {"domain"},
	"CreateAdminUserRequest": {"username", "password"}, "CreateAPITokenRequest": {"name"},

	"CreateGroupRequest": {"name"}, "UpdateGroupRequest": {"name"},
	"CreateRewriteRequest": {"domain", "ip_addresses"},
	"CreatePolicyRequest":  {"name"}, "CreateRangeRequest": {"name", "cidr"},

	"AddBootstrapServerRequest": {"server"},
	"CreateUpstreamRequest":     {"upstream"}, "UpdateUpstreamRequest": {"upstream"},
	"InvestigateRequest": {"sql"}, "AddPeerRequest": {"url"},
	"ConfirmPeerRequest": {"peer_url", "pairing_code"}, "SyncPairCompleteRequest": {"peer_url", "pairing_code", "proof"},

	"compareRequest": {"blocklist_ids"}, "matrixRequest": {"domains"}, "simulateRequest": {"client_ip", "domains"},
}

func (g *Types) request(name string, t reflect.Type) *Schema {
	s := g.shape(t)
	clone := *g.inputProjection(s)
	clone.Required = requiredRequestFields[t.Name()]
	for _, field := range clone.Required {
		if _, ok := clone.Properties[field]; !ok {
			panic(fmt.Sprintf("unknown required request field %s.%s", t, field))
		}
		property := clone.Properties[field]
		if len(property.AnyOf) == 2 && property.AnyOf[1].Type == "null" {
			clone.Properties[field] = property.AnyOf[0]
		}
	}
	g.Schemas[name] = &clone
	return ref(name)
}

// A request's nested records also accept omitted zero-value fields unless a
// handler validates them. Keep that projection separate from response records.
func (g *Types) inputProjection(s *Schema) *Schema {
	if s.Ref != "" {
		name := schemaName(s) + "Input"
		if _, ok := g.Schemas[name]; !ok {
			g.Schemas[name] = nil
			projected := g.inputProjection(g.Schemas[schemaName(s)])
			projected.Required = requiredRequestFields[schemaName(s)]
			for _, field := range projected.Required {
				property := projected.Properties[field]
				if len(property.AnyOf) == 2 && property.AnyOf[1].Type == "null" {
					projected.Properties[field] = property.AnyOf[0]
				}
			}
			g.Schemas[name] = projected
		}
		return ref(name)
	}
	projection := *s
	projection.Required = nil
	if s.Properties != nil {
		projection.Properties = map[string]*Schema{}
		for key, property := range s.Properties {
			projection.Properties[key] = nullableInput(g.inputProjection(property))
		}
	}
	if s.Items != nil {
		projection.Items = g.inputProjection(s.Items)
	}
	if s.AdditionalProperties != nil {
		projection.AdditionalProperties = g.inputProjection(s.AdditionalProperties)
	}
	if s.AnyOf != nil {
		projection.AnyOf = make([]*Schema, len(s.AnyOf))
		for i, part := range s.AnyOf {
			projection.AnyOf[i] = g.inputProjection(part)
		}
	}
	return &projection
}

// encoding/json accepts null for an optional field, preserving its zero value.
func nullableInput(s *Schema) *Schema {
	if s.Type == "null" {
		return s
	}
	for _, option := range s.AnyOf {
		if option.Type == "null" {
			return s
		}
	}
	return nullable(s)
}
