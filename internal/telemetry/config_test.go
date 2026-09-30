package telemetry

import "testing"

func TestConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want Config
		err  string
	}{
		{"disabled", nil, Config{}, ""},
		{"enabled", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://alloy:4318", "PYROSCOPE_SERVER_ADDRESS": "http://alloy:9999"}, Config{TraceEndpoint: "http://alloy:4318", ProfileEndpoint: "http://alloy:9999"}, ""},
		// #nosec G101 -- Deliberately invalid fixture credentials test rejection, not authentication.
		{"credentials", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://secret:password@alloy:4318"}, Config{}, "OTEL_EXPORTER_OTLP_ENDPOINT must be an absolute http(s) URL without credentials, query, fragment or path"},
		{"query", map[string]string{"PYROSCOPE_SERVER_ADDRESS": "http://alloy:9999?token=private"}, Config{}, "PYROSCOPE_SERVER_ADDRESS must be an absolute http(s) URL without credentials, query, fragment or path"},
		{"relative", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "alloy:4318"}, Config{}, "OTEL_EXPORTER_OTLP_ENDPOINT must be an absolute http(s) URL without credentials, query, fragment or path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseConfig(func(k string) string { return tc.env[k] })
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("error=%v want=%s", err, tc.err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}
