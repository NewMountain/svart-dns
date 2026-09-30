package svart

import (
	"crypto/tls"
	"testing"
)

func TestGetEnvBool(t *testing.T) {
	const key = "SVART_TEST_GETENVBOOL"

	tests := []struct {
		name       string
		value      string
		set        bool
		defaultVal bool
		want       bool
	}{
		{name: "unset falls back to default true", set: false, defaultVal: true, want: true},
		{name: "unset falls back to default false", set: false, defaultVal: false, want: false},
		{name: "empty falls back to default", value: "", set: true, defaultVal: true, want: true},
		{name: "true", value: "true", set: true, defaultVal: false, want: true},
		{name: "TRUE", value: "TRUE", set: true, defaultVal: false, want: true},
		{name: "1", value: "1", set: true, defaultVal: false, want: true},
		{name: "false", value: "false", set: true, defaultVal: true, want: false},
		{name: "0", value: "0", set: true, defaultVal: true, want: false},
		{name: "garbage falls back to default", value: "not-a-bool", set: true, defaultVal: true, want: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(key, tc.value)
			}
			got := getEnvBool(key, tc.defaultVal)
			if got != tc.want {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tc.value, tc.defaultVal, got, tc.want)
			}
		})
	}
}

// TestNewAdminTLSConfigPinsMinVersion guards against the admin HTTPS server
// ever silently dropping back to a Go stdlib default (or a future edit
// removing the field): TLS 1.2 must always be the explicit floor.
func TestNewAdminTLSConfigPinsMinVersion(t *testing.T) {
	cfg := newAdminTLSConfig()
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("expected MinVersion = tls.VersionTLS12 (%d), got %d", tls.VersionTLS12, cfg.MinVersion)
	}
	if cfg.GetCertificate == nil {
		t.Fatal("expected GetCertificate callback to be set")
	}
}
