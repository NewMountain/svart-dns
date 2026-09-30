// Package listparse interprets DNS filtering list syntax without application state.
package listparse

import (
	"fmt"
	"io"
	"net/netip"
	"strings"
)

const (
	listRefreshScannerBufferSize   = 64 * 1024
	listRefreshScannerMaxTokenSize = 1024 * 1024
	listRejectSampleLines          = 5
	maxDNSNameLength               = 253
)

// Result is a complete list ready for transactional publication.
type Result struct {
	Domains     []string
	Exceptions  []string
	Rules       []Rule
	Diagnostics []Diagnostic
	Lines       int
	Unsupported int
	// ModifiersIgnored remains zero: restrictions are honored or the rule is skipped.
	ModifiersIgnored int
	Invalid          int
	InvalidAt        []int
}

// ExceptionPrefix marks a stored list entry as an exception of that list
// (the ABP "@@" syntax), so exceptions live in the same table as rules.
const ExceptionPrefix = "@@"

// StoredRows is what a refresh writes for list: its rules, then its own
// exceptions (prefixed, blocklists only; in an allowlist a "@@" rule is just
// an allowed name). Duplicates are dropped; the count is of rules only.
func StoredRows(list Result, blocklist bool) (rows []string, domainCount int) {
	seen := make(map[string]bool, len(list.Domains)+len(list.Exceptions))
	add := func(r string) bool {
		if seen[r] {
			return false
		}
		seen[r] = true
		rows = append(rows, r)
		return true
	}
	for _, d := range list.Domains {
		if add(d) {
			domainCount++
		}
	}
	for _, e := range list.Exceptions {
		if blocklist {
			add(ExceptionPrefix + e)
		} else if add(e) {
			domainCount++
		}
	}
	for _, rule := range list.Rules {
		if !blocklist {
			rule.Exception = false
		}
		encoded, err := EncodeStored(rule)
		if err != nil {
			panic("invalid parsed rule: " + err.Error())
		}
		if add(encoded) && !rule.Exception {
			domainCount++
		}
	}
	return rows, domainCount
}

// TooLargeError explains how to raise the download limit without losing the active list.
func TooLargeError(maxBytes int64) error {
	return fmt.Errorf("list is larger than %d bytes (LIST_MAX_DOWNLOAD_BYTES); the previous version stays active — raise the limit if this list is legitimate", maxBytes)
}

// isListHeader matches an ABP header such as "[Adblock Plus 2.0]".
func isListHeader(line string) bool {
	return strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") && !strings.Contains(line, ":")
}

func isURLScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// normalizedListRule turns one plain or hosts-file rule into a stored entry.
func normalizedListRule(rule string) string {
	domain := ParseDomain(rule)
	if !strings.Contains(domain, "*") {
		// A trailing dot on a name is FQDN notation; on a wildcard pattern it
		// is part of the pattern ("*shop." = contains "shop.").
		domain = strings.TrimSuffix(domain, ".")
	}
	return domain
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// isValidListEntry accepts what the blocking engine can match: DNS names
// (labels of letters, digits, '-', '_', 1–63 bytes, 253 total), the same
// with '*' wildcards, and IP literals for response-IP blocking. Anything
// else — markup, JSON, credentials, Unicode that was not punycoded — is
// rejected so fetched content cannot masquerade as domains.
func isValidListEntry(entry string) bool {
	if entry == "" || len(entry) > maxDNSNameLength {
		return false
	}
	if _, err := netip.ParseAddr(entry); err == nil {
		return true
	}
	if strings.Contains(entry, "*") {
		entry = strings.TrimSuffix(entry, ".") // see Parse: part of the pattern
	}
	for _, label := range strings.Split(entry, ".") {
		if label == "" || len(label) > 63 {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '*' {
				return false
			}
		}
	}
	return true
}

// ParseDomain extracts and normalizes one domain from hosts or supported filter syntax.
func ParseDomain(line string) string {
	line = strings.TrimSpace(line)

	if strings.HasPrefix(line, "127.0.0.1") || strings.HasPrefix(line, "0.0.0.0") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			line = parts[1]
		} else {
			return ""
		}
	}

	if strings.Contains(line, "$") {
		line = strings.Split(line, "$")[0]
	}

	if strings.HasPrefix(line, "||") {
		line = strings.TrimPrefix(line, "||")
		line = strings.TrimSuffix(line, "|")
		line = strings.TrimSuffix(line, "^")
	} else if strings.HasPrefix(line, "|") {
		line = strings.TrimPrefix(line, "|")
		line = strings.TrimSuffix(line, "|")
		line = strings.TrimSuffix(line, "^")
	}

	if strings.Contains(line, "/") {
		line = strings.Split(line, "/")[0]
	}

	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(line, " ") {
		return ""
	}
	return NormalizeEntry(line)
}
