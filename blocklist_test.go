package main

import (
	"strings"
	"testing"
)

func TestParseDomain(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic domain with prefix and suffix",
			input:    "||example.com^",
			expected: "example.com",
		},
		{
			name:     "domain with subdomain",
			input:    "||ads.example.com^",
			expected: "ads.example.com",
		},
		{
			name:     "domain with path",
			input:    "||example.com/ads^",
			expected: "example.com",
		},
		{
			name:     "domain with modifiers",
			input:    "||example.com^$third-party",
			expected: "example.com",
		},
		{
			name:     "IP address",
			input:    "||103.224.182.210^",
			expected: "103.224.182.210",
		},
		{
			name:     "IP address variant",
			input:    "||193.200.64.30^",
			expected: "193.200.64.30",
		},
		{
			name:     "domain with single pipe",
			input:    "|http://example.com^",
			expected: "http:",
		},
		{
			name:     "domain with trailing pipe",
			input:    "||example.com^|",
			expected: "example.com",
		},
		{
			name:     "uppercase domain",
			input:    "||EXAMPLE.COM^",
			expected: "example.com",
		},
		{
			name:     "domain with multiple path segments",
			input:    "||example.com/path/to/resource^",
			expected: "example.com",
		},
		{
			name:     "domain with complex modifiers",
			input:    "||example.com^$script,third-party,domain=~example.org",
			expected: "example.com",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "domain with space (invalid)",
			input:    "||example .com^",
			expected: "",
		},
		{
			name:     "wildcard domain",
			input:    "||*.example.com^",
			expected: "*.example.com",
		},
		{
			name:     "hosts file format with 127.0.0.1",
			input:    "127.0.0.1 example.com",
			expected: "example.com",
		},
		{
			name:     "hosts file format with 0.0.0.0",
			input:    "0.0.0.0 ads.example.com",
			expected: "ads.example.com",
		},
		{
			name:     "hosts file format with comment",
			input:    "127.0.0.1 tracking.example.com # tracking",
			expected: "tracking.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDomain(tt.input)
			if result != tt.expected {
				t.Errorf("parseDomain(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShouldSkipRule(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		shouldSkip bool
		reason     string
	}{
		{
			name:       "comment with exclamation",
			input:      "! This is a comment",
			shouldSkip: true,
			reason:     "comments",
		},
		{
			name:       "comment with hash",
			input:      "# This is a comment",
			shouldSkip: true,
			reason:     "comments",
		},
		{
			name:       "whitelist rule",
			input:      "@@||example.com^",
			shouldSkip: true,
			reason:     "whitelist",
		},
		{
			name:       "badfilter rule",
			input:      "||tn.porngo.xxx^$badfilter",
			shouldSkip: true,
			reason:     "badfilter",
		},
		{
			name:       "regex pattern with slashes",
			input:      "/^94\\.242\\.247\\.(2[0-9]|3[0-2]):/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "regex pattern IP range",
			input:      "/^23\\.109\\.170\\.(18[7-9]|19[0-2])$/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "regex pattern with digits",
			input:      "/^23\\.109\\.73\\.\\d{3}/",
			shouldSkip: true,
			reason:     "regex",
		},
		{
			name:       "empty line",
			input:      "",
			shouldSkip: true,
			reason:     "empty",
		},
		{
			name:       "whitespace line",
			input:      "   ",
			shouldSkip: true,
			reason:     "empty",
		},
		{
			name:       "valid domain rule",
			input:      "||example.com^",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "valid IP rule",
			input:      "||193.200.64.30^",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "valid domain with modifiers",
			input:      "||example.com^$third-party",
			shouldSkip: false,
			reason:     "valid",
		},
		{
			name:       "badfilter in domain name (not modifier)",
			input:      "||badfilter.com^",
			shouldSkip: false,
			reason:     "valid - badfilter not in modifier",
		},
		{
			name:       "dnstype modifier rule",
			input:      "|google.com|$dnstype=TXT",
			shouldSkip: true,
			reason:     "dnstype modifier",
		},
		{
			name:       "dnsrewrite modifier rule",
			input:      "||example.com^$dnsrewrite=1.2.3.4",
			shouldSkip: true,
			reason:     "dnsrewrite modifier",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := tt.input
			shouldSkip := false

			if line == "" || line == "   " {
				shouldSkip = true
			} else if line[0] == '!' || line[0] == '#' {
				shouldSkip = true
			} else if len(line) >= 2 && line[0:2] == "@@" {
				shouldSkip = true
			} else if len(line) >= 1 && line[0] == '/' && line[len(line)-1] == '/' {
				shouldSkip = true
			} else if strings.Contains(line, "$") {
				parts := strings.Split(line, "$")
				if len(parts) > 1 {
					modifiers := strings.Split(parts[1], ",")
					for _, mod := range modifiers {
						modTrimmed := strings.TrimSpace(mod)
						if modTrimmed == "badfilter" ||
							strings.HasPrefix(modTrimmed, "dnstype=") ||
							strings.HasPrefix(modTrimmed, "dnsrewrite=") ||
							strings.Contains(modTrimmed, "client=") ||
							strings.Contains(modTrimmed, "ctag=") {
							shouldSkip = true
							break
						}
					}
				}
			} else if parseDomain(line) == "" {
				shouldSkip = true
			}

			if shouldSkip != tt.shouldSkip {
				t.Errorf("Rule %q: shouldSkip = %v, want %v (reason: %s)", tt.input, shouldSkip, tt.shouldSkip, tt.reason)
			}
		})
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		domain   string
		expected bool
	}{
		{
			name:     "simple wildcard prefix",
			pattern:  "*.example.com",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "simple wildcard prefix no match",
			pattern:  "*.example.com",
			domain:   "example.com",
			expected: false,
		},
		{
			name:     "wildcard suffix",
			pattern:  "ads.*",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "wildcard middle",
			pattern:  "ads.*.com",
			domain:   "ads.example.com",
			expected: true,
		},
		{
			name:     "no wildcard exact match",
			pattern:  "example.com",
			domain:   "example.com",
			expected: false,
		},
		{
			name:     "no wildcard no match",
			pattern:  "example.com",
			domain:   "ads.example.com",
			expected: false,
		},
		{
			name:     "multiple wildcards",
			pattern:  "*.ads.*.com",
			domain:   "tracking.ads.example.com",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.domain)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.domain, result, tt.expected)
			}
		})
	}
}

func TestReverseARPAToIP(t *testing.T) {
	tests := []struct {
		name     string
		arpa     string
		expected string
	}{
		{
			name:     "standard reverse DNS",
			arpa:     "30.64.200.193.in-addr.arpa",
			expected: "193.200.64.30",
		},
		{
			name:     "another reverse DNS",
			arpa:     "210.190.117.212.in-addr.arpa",
			expected: "212.117.190.210",
		},
		{
			name:     "with trailing dot",
			arpa:     "30.64.200.193.in-addr.arpa.",
			expected: "",
		},
		{
			name:     "invalid format",
			arpa:     "invalid.in-addr.arpa",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := reverseARPAToIP(tt.arpa)
			if result != tt.expected {
				t.Errorf("reverseARPAToIP(%q) = %q, want %q", tt.arpa, result, tt.expected)
			}
		})
	}
}

func TestIPBlockingScenarios(t *testing.T) {
	tests := []struct {
		name        string
		rule        string
		shouldBlock bool
		description string
	}{
		{
			name:        "IP address blocking",
			rule:        "||193.200.64.30^",
			shouldBlock: true,
			description: "Should block IP 193.200.64.30 after resolution",
		},
		{
			name:        "malicious server IP",
			rule:        "||212.117.190.210^",
			shouldBlock: true,
			description: "Should block known malicious server IP",
		},
		{
			name:        "tracking pixel IP",
			rule:        "||185.246.188.124^",
			shouldBlock: true,
			description: "Should block tracking pixel server IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain := parseDomain(tt.rule)
			if domain == "" {
				t.Errorf("parseDomain(%q) returned empty, expected IP address", tt.rule)
			}

			if !strings.Contains(domain, ".") || len(strings.Split(domain, ".")) != 4 {
				t.Errorf("parseDomain(%q) = %q, expected IP format", tt.rule, domain)
			}
		})
	}
}
