package listparse

import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strings"

	"github.com/miekg/dns"
)

type parsedRule struct {
	rule      Rule
	ordinary  []string
	identity  string
	badfilter bool
}

// Parse never returns a partial list on a read, size, or compilation failure.
func Parse(body io.Reader, maxBytes int64, maxLines int) (Result, error) {
	counted := &countingReader{r: io.LimitReader(body, maxBytes+1)}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, listRefreshScannerBufferSize), listRefreshScannerMaxTokenSize)
	var out Result
	var domainIdentity, exceptionIdentity, ruleIdentity []string
	cancelled := map[string]bool{}
	for scanner.Scan() {
		out.Lines++
		if out.Lines > maxLines {
			return Result{}, fmt.Errorf("list has more than %d lines (LIST_MAX_LINES); the previous version stays active — raise the limit if this list is legitimate", maxLines)
		}
		line := strings.TrimSpace(scanner.Text())
		if out.Lines == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") || isListHeader(line) {
			continue
		}
		p, reason, unsupported, fatal := parseTypedRule(line)
		if fatal != nil {
			return Result{}, fmt.Errorf("line %d: %w; previous list stays active", out.Lines, fatal)
		}
		if reason != "" {
			out.Diagnostics = append(out.Diagnostics, Diagnostic{Line: out.Lines, Rule: line, Reason: reason})
			if unsupported {
				out.Unsupported++
			} else {
				out.Invalid++
				if len(out.InvalidAt) < listRejectSampleLines {
					out.InvalidAt = append(out.InvalidAt, out.Lines)
				}
			}
			continue
		}
		if p.badfilter {
			cancelled[p.identity] = true
		} else if p.ordinary != nil {
			for _, ordinary := range p.ordinary {
				if p.rule.Exception {
					out.Exceptions = append(out.Exceptions, ordinary)
					exceptionIdentity = append(exceptionIdentity, p.identity)
				} else {
					out.Domains = append(out.Domains, ordinary)
					domainIdentity = append(domainIdentity, p.identity)
				}
			}
		} else {
			out.Rules = append(out.Rules, p.rule)
			ruleIdentity = append(ruleIdentity, p.identity)
		}
	}
	if counted.n > maxBytes {
		return Result{}, TooLargeError(maxBytes)
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("read list: %w", err)
	}
	if len(cancelled) > 0 {
		keep := func(values, identities []string) []string {
			out := values[:0]
			for i, value := range values {
				if !cancelled[identities[i]] {
					out = append(out, value)
				}
			}
			return out
		}
		out.Domains = keep(out.Domains, domainIdentity)
		out.Exceptions = keep(out.Exceptions, exceptionIdentity)
		kept := out.Rules[:0]
		for i, rule := range out.Rules {
			if !cancelled[ruleIdentity[i]] {
				kept = append(kept, rule)
			}
		}
		out.Rules = kept
	}
	return out, nil
}

func parseTypedRule(text string) (parsedRule, string, bool, error) {
	p := parsedRule{rule: Rule{Version: 1, Text: text}}
	line := text
	if strings.HasPrefix(line, "@@") {
		p.rule.Exception = true
		line = line[2:]
	}
	// Hosts comments are lexical data, not filter modifiers or URL syntax.
	// Keep Text intact for provenance and never apply this to browser/regex rules.
	if separator := strings.IndexAny(line, " \t"); separator >= 0 {
		first := line[:separator]
		if first == "0.0.0.0" || first == "127.0.0.1" {
			host, _, _ := strings.Cut(line[separator+1:], "#")
			host = strings.TrimSpace(host)
			if host == "" {
				return p, "hosts rule requires a hostname", false, nil
			}
			line = first + " " + host
		}
	}
	pattern, mods, err := splitRuleModifiers(line)
	if err != nil {
		return p, err.Error(), false, nil
	}
	qualified := false
	for _, mod := range mods {
		name, value, hasValue := strings.Cut(mod, "=")
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		switch name {
		case "badfilter":
			if hasValue {
				return p, "badfilter does not accept a value", false, nil
			}
			p.badfilter = true
		case "important":
			if hasValue {
				return p, "important does not accept a value", false, nil
			}
			p.rule.Important = true
			qualified = true
		case "dnstype":
			types, exclude, err := parseDNSTypes(value)
			if err != nil {
				return p, err.Error(), false, nil
			}
			if len(p.rule.DNSTypes) > 0 {
				return p, "duplicate dnstype modifier", false, nil
			}
			p.rule.DNSTypes, p.rule.ExcludeTypes = types, exclude
			qualified = true
		case "denyallow":
			if value == "" {
				return p, "denyallow requires domains", false, nil
			}
			for _, part := range strings.Split(value, "|") {
				d := NormalizeEntry(part)
				if !isValidListEntry(d) || strings.Contains(d, "*") {
					return p, "invalid denyallow domain: " + part, false, nil
				}
				p.rule.DenyAllow = append(p.rule.DenyAllow, d)
			}
			slices.Sort(p.rule.DenyAllow)
			p.rule.DenyAllow = slices.Compact(p.rule.DenyAllow)
			qualified = true
		default:
			return p, "unsupported modifier: " + name, true, nil
		}
	}
	// Keep the exact pattern spelling/anchors, but compare supported options
	// by their validated meaning rather than source order or type-value case.
	// Ordinary rows retain their allocation-free pattern identity.
	p.identity = canonicalRuleIdentity(pattern, p.rule)
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") && len(pattern) >= 2 {
		p.rule.Kind, p.rule.Pattern = KindRegex, pattern[1:len(pattern)-1]
		if p.rule.Pattern == "" && len(p.rule.DNSTypes) == 0 && len(p.rule.DenyAllow) == 0 {
			return p, "empty regex requires dnstype or denyallow", false, nil
		}
		if _, err := CompilePattern(p.rule); err != nil {
			return p, "", false, err
		}
		return p, "", false, nil
	}
	if p.badfilter && strings.ContainsAny(pattern, " \t") {
		return p, "badfilter on hosts syntax", true, nil
	}
	kind, normalized, ordinary, reason, unsupported := parseHostnamePattern(pattern)
	if reason != "" {
		return p, reason, unsupported, nil
	}
	p.rule.Kind, p.rule.Pattern = kind, normalized
	if !qualified {
		p.ordinary = ordinary
	}
	return p, "", false, nil
}

func canonicalRuleIdentity(pattern string, rule Rule) string {
	var options []string
	if len(rule.DenyAllow) > 0 {
		options = append(options, "denyallow="+strings.Join(rule.DenyAllow, "|"))
	}
	if len(rule.DNSTypes) > 0 {
		types := make([]string, len(rule.DNSTypes))
		for i, typ := range rule.DNSTypes {
			types[i] = dns.TypeToString[typ]
			if rule.ExcludeTypes {
				types[i] = "~" + types[i]
			}
		}
		options = append(options, "dnstype="+strings.Join(types, "|"))
	}
	if rule.Important {
		options = append(options, "important")
	}
	if len(options) > 0 {
		pattern += "$" + strings.Join(options, ",")
	}
	if rule.Exception {
		pattern = "@@" + pattern
	}
	return pattern
}

func splitRuleModifiers(line string) (string, []string, error) {
	if strings.HasPrefix(line, "/") {
		// Regex dollars belong to the body. Only a slash followed by end-of-line
		// or a modifier delimiter can terminate the regex.
		for i := len(line) - 1; i > 0; i-- {
			if line[i] != '/' || (i+1 < len(line) && line[i+1] != '$') {
				continue
			}
			escapes := 0
			for j := i - 1; j >= 0 && line[j] == '\\'; j-- {
				escapes++
			}
			if escapes%2 != 0 {
				continue
			}
			if i+1 == len(line) {
				return line, nil, nil
			}
			return line[:i+1], strings.Split(line[i+2:], ","), nil
		}
		return "", nil, fmt.Errorf("unterminated regular expression")
	}
	pattern, modifiers, found := strings.Cut(line, "$")
	if !found {
		return pattern, nil, nil
	}
	return pattern, strings.Split(modifiers, ","), nil
}

func parseDNSTypes(value string) ([]uint16, bool, error) {
	if value == "" {
		return nil, false, fmt.Errorf("dnstype requires record types")
	}
	var include, exclude []uint16
	for _, part := range strings.Split(value, "|") {
		part = strings.TrimSpace(part)
		neg := strings.HasPrefix(part, "~")
		name := strings.ToUpper(strings.TrimPrefix(part, "~"))
		typ, ok := dns.StringToType[name]
		if !ok || typ == 0 {
			return nil, false, fmt.Errorf("unknown DNS type: %s", name)
		}
		if neg {
			exclude = append(exclude, typ)
		} else {
			include = append(include, typ)
		}
	}
	types, negative := include, false
	if len(include) == 0 {
		types, negative = exclude, true
	} else {
		types = slices.DeleteFunc(types, func(typ uint16) bool { return slices.Contains(exclude, typ) })
		if len(types) == 0 {
			return nil, false, fmt.Errorf("dnstype includes are all excluded")
		}
	}
	slices.Sort(types)
	return slices.Compact(types), negative, nil
}

func parseHostnamePattern(pattern string) (RuleKind, string, []string, string, bool) {
	if _, err := netip.ParsePrefix(pattern); err == nil {
		return "", "", nil, "address ranges are unsupported", true
	}
	if pattern == "" {
		return KindMask, "*", nil, "", false
	}
	// Plain names and supported hosts rows retain Svart's historical subtree
	// semantics. They deliberately differ from AdGuard's plain-domain syntax.
	if !strings.ContainsAny(pattern, "|^*/") && !strings.HasPrefix(pattern, ".") && !strings.Contains(pattern, "://") {
		d := normalizedListRule(pattern)
		if !isValidListEntry(d) {
			return "", "", nil, "invalid hostname rule", false
		}
		if _, err := netip.ParseAddr(d); err == nil {
			return KindAddress, d, []string{d}, "", false
		}
		return KindSubtree, d, []string{d}, "", false
	}
	host := pattern
	prefix := ""
	schemeSpecific := false
	if strings.HasPrefix(host, "||") {
		prefix = "||"
		host = host[2:]
	} else if strings.HasPrefix(host, "|") {
		prefix = "|"
		host = host[1:]
	}
	if i := strings.Index(host, "://"); i >= 0 && isURLScheme(host[:i]) {
		schemeSpecific = i > 0
		host = host[i+3:]
		prefix = "|"
	}
	if schemeSpecific {
		return "", "", nil, "URL scheme requires browser context", true
	}
	if strings.Contains(host, "/") {
		return "", "", nil, "URL path requires browser context", true
	}
	end := false
	if strings.HasSuffix(host, "^") || strings.HasSuffix(host, "|") {
		end = true
		host = strings.TrimRight(host, "^|")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	trailingDot := strings.HasSuffix(host, ".")
	host = NormalizeEntry(host)
	if trailingDot && !strings.HasSuffix(host, ".") {
		host += "."
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return KindAddress, host, []string{host}, "", false
	}
	if host != "" && strings.Trim(host, "0123456789.*") == "" && host != "*" {
		return "", "", nil, "address ranges are unsupported", true
	}
	if host == "" || !validMask(host) {
		return "", "", nil, "invalid hostname mask", false
	}
	if end && !strings.Contains(host, "*") && !isValidListEntry(host) {
		return "", "", nil, "invalid hostname mask", false
	}
	if !strings.Contains(host, "*") && end && prefix == "||" {
		return KindSubtree, host, []string{host}, "", false
	}
	if !strings.Contains(host, "*") && end && prefix == "|" {
		return KindExact, host, nil, "", false
	}
	normalized := prefix + host
	if end {
		normalized += "^"
	}
	// A suffix wildcard with an explicit domain/end anchor already has an
	// exact compact representation. Everything else retains its mask syntax.
	if end && prefix == "||" && strings.HasPrefix(host, "*.") && !strings.Contains(host[2:], "*") {
		return KindMask, normalized, []string{host}, "", false
	}
	return KindMask, normalized, nil, "", false
}

func validMask(host string) bool {
	if host == "*" {
		return true
	}
	host = strings.TrimPrefix(host, ".")
	host = strings.TrimSuffix(host, ".")
	return isValidListEntry(host)
}
