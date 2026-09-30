// Package telemetry instruments control-plane operations, never individual DNS queries.
package telemetry

import (
	"fmt"
	"net/url"
)

// Config contains optional self-hosted collector endpoints.
type Config struct{ TraceEndpoint, ProfileEndpoint string }

// ParseConfig validates explicit collector endpoints without disclosing their contents.
func ParseConfig(getenv func(string) string) (Config, error) {
	c := Config{TraceEndpoint: getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), ProfileEndpoint: getenv("PYROSCOPE_SERVER_ADDRESS")}
	for _, item := range []struct{ name, value string }{{"OTEL_EXPORTER_OTLP_ENDPOINT", c.TraceEndpoint}, {"PYROSCOPE_SERVER_ADDRESS", c.ProfileEndpoint}} {
		if item.value == "" {
			continue
		}
		u, err := url.Parse(item.value)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return Config{}, fmt.Errorf("%s must be an absolute http(s) URL without credentials, query, fragment or path", item.name)
		}
	}
	return c, nil
}
