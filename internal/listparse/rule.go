package listparse

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"regexp"
	"strings"
)

// StoredPrefix distinguishes qualified rules from legacy hostname rows.
const StoredPrefix = "!svart-rule-v1:"

// RuleKind identifies the matching semantics of a stored rule.
type RuleKind string

// Supported rule kinds separate domain, mask, regex and address matching.
const (
	KindSubtree RuleKind = "subtree"
	KindExact   RuleKind = "exact"
	KindMask    RuleKind = "mask"
	KindRegex   RuleKind = "regex"
	KindAddress RuleKind = "address"
)

// Rule stores syntax that cannot be represented by an unqualified domain row.
// Text is retained for attribution; Pattern alone determines name matching.
type Rule struct {
	Version      uint8    `json:"version"`
	Text         string   `json:"text"`
	Pattern      string   `json:"pattern"`
	Kind         RuleKind `json:"kind"`
	Exception    bool     `json:"exception,omitempty"`
	Important    bool     `json:"important,omitempty"`
	DNSTypes     []uint16 `json:"dns_types,omitempty"`
	ExcludeTypes bool     `json:"exclude_types,omitempty"`
	DenyAllow    []string `json:"deny_allow,omitempty"`
}

// Diagnostic retains one rejected source line and its precise rejection reason.
type Diagnostic struct {
	Line   int    `json:"line"`
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// DecodeStored validates a versioned rule or interprets a legacy domain row.
func DecodeStored(row string) (Rule, error) {
	if strings.HasPrefix(row, "!svart-rule-") {
		if !strings.HasPrefix(row, StoredPrefix) {
			return Rule{}, fmt.Errorf("unsupported stored rule version")
		}
		var r Rule
		dec := json.NewDecoder(strings.NewReader(strings.TrimPrefix(row, StoredPrefix)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			return Rule{}, fmt.Errorf("decode stored rule: %w", err)
		}
		var extra json.RawMessage
		if err := dec.Decode(&extra); err != io.EOF {
			return Rule{}, fmt.Errorf("stored rule has trailing data")
		}
		if err := validateRule(r); err != nil {
			return Rule{}, err
		}
		return r, nil
	}
	trimmed := strings.TrimSpace(row)
	exception := strings.HasPrefix(trimmed, ExceptionPrefix)
	pattern := NormalizeEntry(strings.TrimPrefix(trimmed, ExceptionPrefix))
	r := Rule{Version: 1, Text: row, Pattern: pattern, Kind: KindSubtree, Exception: exception}
	if _, err := netip.ParseAddr(pattern); err == nil {
		r.Kind = KindAddress
	} else if strings.Contains(pattern, "*") {
		// Legacy wildcard rows are start/end anchored, except their historical
		// trailing-dot form. Preserve them when reading an existing database.
		r.Kind = KindMask
		r.Pattern = "|" + pattern
		if !strings.HasSuffix(pattern, ".") {
			r.Pattern += "|"
		}
	}
	if !isValidListEntry(pattern) {
		return Rule{}, fmt.Errorf("invalid stored domain rule %q", row)
	}
	return r, nil
}

// EncodeStored validates and serializes a qualified rule without losing its source text.
func EncodeStored(r Rule) (string, error) {
	if err := validateRule(r); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	return StoredPrefix + string(encoded), nil
}

// NormalizeStored never lowercases regex syntax or serialized provenance.
func NormalizeStored(row string) (string, error) {
	if strings.HasPrefix(row, "!svart-rule-") {
		r, err := DecodeStored(row)
		if err != nil {
			return "", err
		}
		return EncodeStored(r)
	}
	if strings.TrimSpace(row) == "" {
		return "", nil
	}
	// Existing databases were normalized without revalidating legacy rows.
	// Keep that loading contract; fresh downloads validate at Parse instead.
	trimmed := strings.TrimSpace(row)
	prefix := ""
	if strings.HasPrefix(trimmed, ExceptionPrefix) {
		prefix = ExceptionPrefix
	}
	return prefix + NormalizeEntry(strings.TrimPrefix(trimmed, ExceptionPrefix)), nil
}

func validateRule(r Rule) error {
	if r.Version != 1 {
		return fmt.Errorf("unsupported stored rule version %d", r.Version)
	}
	if r.Text == "" || (r.Pattern == "" && r.Kind != KindRegex) {
		return fmt.Errorf("stored rule requires text and pattern")
	}
	if r.ExcludeTypes && len(r.DNSTypes) == 0 {
		return fmt.Errorf("stored exclusion rule requires DNS types")
	}
	for _, typ := range r.DNSTypes {
		if typ == 0 {
			return fmt.Errorf("invalid DNS type 0")
		}
	}
	for _, d := range r.DenyAllow {
		if !isValidListEntry(d) || strings.Contains(d, "*") {
			return fmt.Errorf("invalid denyallow domain %q", d)
		}
	}
	switch r.Kind {
	case KindAddress:
		if _, err := netip.ParseAddr(r.Pattern); err != nil {
			return fmt.Errorf("invalid address pattern: %w", err)
		}
	case KindSubtree, KindExact:
		if !isValidListEntry(r.Pattern) || strings.Contains(r.Pattern, "*") {
			return fmt.Errorf("invalid domain pattern %q", r.Pattern)
		}
	case KindMask:
		host := strings.TrimLeft(r.Pattern, "|")
		host = strings.TrimRight(host, "^|")
		if host != "*" && !validMask(host) {
			return fmt.Errorf("invalid hostname mask %q", r.Pattern)
		}
	case KindRegex:
	default:
		return fmt.Errorf("unsupported stored rule kind %q", r.Kind)
	}
	_, err := CompilePattern(r)
	return err
}

// CompilePattern supplies one authoritative mask translation for refresh
// validation and immutable policy indexes. Query evaluation only matches it.
func CompilePattern(r Rule) (*regexp.Regexp, error) {
	var pattern string
	switch r.Kind {
	case KindRegex:
		pattern = "(?i:" + r.Pattern + ")"
	case KindSubtree:
		pattern = `(?:^|\.)` + regexp.QuoteMeta(r.Pattern) + `$`
	case KindExact, KindAddress:
		pattern = "^" + regexp.QuoteMeta(r.Pattern) + "$"
	case KindMask:
		mask := r.Pattern
		prefix := ""
		if strings.HasPrefix(mask, "||") {
			prefix = `(?:^|\.)`
			mask = mask[2:]
		} else if strings.HasPrefix(mask, "|") {
			prefix = "^"
			mask = mask[1:]
		}
		suffix := ""
		if strings.HasSuffix(mask, "^") || strings.HasSuffix(mask, "|") {
			suffix = "$"
			mask = strings.TrimRight(mask, "^|")
		}
		var out strings.Builder
		out.WriteString(prefix)
		for _, part := range strings.SplitAfter(mask, "*") {
			if strings.HasSuffix(part, "*") {
				out.WriteString(regexp.QuoteMeta(strings.TrimSuffix(part, "*")))
				out.WriteString(".*")
			} else {
				out.WriteString(regexp.QuoteMeta(part))
			}
		}
		out.WriteString(suffix)
		pattern = out.String()
	default:
		return nil, fmt.Errorf("unsupported rule kind %q", r.Kind)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	return re, nil
}
