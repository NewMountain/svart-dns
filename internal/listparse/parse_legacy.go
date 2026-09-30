package listparse

// Reader-first rollout compatibility parser, frozen from 1de baseline.
// Its legacy lowering intentionally stays unchanged until durable activation.
// All reused helpers in parse.go and normalize.go are byte-identical to that
// baseline; the typed parser lives separately in parse_typed.go.
import (
	"bufio"
	"fmt"
	"io"
	"net/netip"
	"strings"
)

// ParseLegacy preserves the pre-activation parser for the reader-first bridge.
func ParseLegacy(body io.Reader, maxBytes int64, maxLines int) (Result, error) {
	counted := &countingReader{r: io.LimitReader(body, maxBytes+1)}
	scanner := bufio.NewScanner(counted)
	scanner.Buffer(make([]byte, listRefreshScannerBufferSize), listRefreshScannerMaxTokenSize)
	var out Result
	var badfilters map[string]bool // "@@"-prefixed for exceptions
	for scanner.Scan() {
		out.Lines++
		if out.Lines > maxLines {
			return Result{}, fmt.Errorf("list has more than %d lines (LIST_MAX_LINES); the previous version stays active — raise the limit if this list is legitimate", maxLines)
		}
		line := strings.TrimSpace(scanner.Text())
		if out.Lines == 1 {
			line = strings.TrimPrefix(line, "\ufeff") // byte order mark
		}
		if line == "" || strings.HasPrefix(line, "!") || strings.HasPrefix(line, "#") || isListHeader(line) {
			continue
		}
		exception := strings.HasPrefix(line, "@@")
		if exception {
			line = line[2:]
		}
		if len(line) > 1 && strings.HasPrefix(line, "/") && strings.HasSuffix(line, "/") {
			out.Unsupported++ // regular expression
			continue
		}
		var mods legacyRuleModifiers
		if i := strings.IndexByte(line, '$'); i >= 0 {
			mods = parseLegacyRuleModifiers(line[i+1:])
			line = line[:i]
		}
		patterns, supported := legacyRulePatterns(line)
		valid := !supported || patterns != nil
		for _, p := range patterns {
			valid = valid && isValidListEntry(p)
		}
		switch {
		case !valid:
			out.Invalid++
			if len(out.InvalidAt) < listRejectSampleLines {
				out.InvalidAt = append(out.InvalidAt, out.Lines)
			}
			continue
		case !supported, mods.unsupported:
			out.Unsupported++
			continue
		case mods.badfilter:
			if badfilters == nil {
				badfilters = make(map[string]bool)
			}
			for _, p := range patterns {
				if exception {
					p = ExceptionPrefix + p
				}
				badfilters[p] = true
			}
			continue
		}
		if mods.partial {
			out.ModifiersIgnored++
		}
		for _, d := range mods.denyallow {
			if d = normalizedListRule(d); isValidListEntry(d) {
				out.Exceptions = append(out.Exceptions, d)
			}
		}
		if exception {
			out.Exceptions = append(out.Exceptions, patterns...)
		} else {
			out.Domains = append(out.Domains, patterns...)
		}
	}
	if counted.n > maxBytes {
		return Result{}, TooLargeError(maxBytes)
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("read list: %w", err)
	}
	if len(badfilters) > 0 {
		// $badfilter cancels the same rule of this list. (AdGuard applies it
		// across every list; lists that ship one cancel their own rules.)
		out.Domains = legacyDropCancelled(out.Domains, badfilters, "")
		out.Exceptions = legacyDropCancelled(out.Exceptions, badfilters, ExceptionPrefix)
	}
	return out, nil
}

func legacyDropCancelled(rules []string, cancelled map[string]bool, prefix string) []string {
	kept := rules[:0]
	for _, r := range rules {
		if !cancelled[prefix+r] {
			kept = append(kept, r)
		}
	}
	return kept
}

func legacyRulePatterns(rule string) (patterns []string, supported bool) {
	host, anchored, domainAnchor := rule, false, false
	switch {
	case strings.HasPrefix(host, "||"):
		host, anchored, domainAnchor = host[2:], true, true
	case strings.HasPrefix(host, "|"):
		host, anchored = host[1:], true
	}
	if i := strings.Index(host, "://"); i >= 0 && isURLScheme(host[:i]) {
		host, anchored, domainAnchor = host[i+3:], true, false
	}
	if !anchored && !strings.HasPrefix(host, ".") && !strings.Contains(host, "*") {
		// A plain name or a hosts-file line, as every list format writes them.
		if d := normalizedListRule(rule); d != "" {
			return []string{d}, true
		}
		return nil, true
	}
	endAnchored := false
	if i := strings.IndexAny(host, "/?"); i >= 0 {
		host, endAnchored = host[:i], true
	}
	for strings.HasSuffix(host, "^") || strings.HasSuffix(host, "|") {
		host, endAnchored = host[:len(host)-1], true
	}
	host = strings.ToLower(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1] // IPv6 literal
	}
	if host == "" || host == "*" {
		return nil, true
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return []string{NormalizeEntry(host)}, true
	}
	if strings.Trim(host, "0123456789.*") == "" {
		return nil, false // an address range; only exact addresses are matched
	}
	if !anchored { // ".example.com", ".booking.com-id*", "*shop."
		if strings.HasPrefix(host, ".") {
			host = "*" + host
		}
		return []string{NormalizeEntry(host)}, true
	}
	if !endAnchored && strings.HasSuffix(host, ".") {
		host += "*" // "||glthub." continues with more labels
	}
	host = NormalizeEntry(host)
	if domainAnchor && strings.Contains(host, "*") && !strings.HasPrefix(host, "*") {
		// "||" also matches at every subdomain boundary.
		return []string{host, "*." + host}, true
	}
	return []string{host}, true
}

type legacyRuleModifiers struct {
	denyallow   []string // $denyallow=a|b: names the rule must not block
	badfilter   bool     // $badfilter: cancels the same rule
	unsupported bool     // the rule is limited in a way Svart cannot honor, so it is skipped
	// partial: a $dnstype/$client/$ctag that only excludes (dnstype=~CNAME)
	// is not honored for what it excludes; the rule otherwise applies.
	partial bool
}

func parseLegacyRuleModifiers(s string) legacyRuleModifiers {
	var m legacyRuleModifiers
	for _, mod := range strings.Split(s, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(mod), "=")
		switch strings.ToLower(name) {
		case "denyallow":
			for _, d := range strings.Split(value, "|") {
				if d = strings.TrimSpace(d); d != "" {
					m.denyallow = append(m.denyallow, d)
				}
			}
		case "badfilter":
			m.badfilter = true
		case "", "important", "all", "document", "doc", "match-case":
			// A whole-host block already is what these ask for.
		case "dnstype", "client", "ctag":
			for _, v := range strings.Split(value, "|") {
				if !strings.HasPrefix(strings.TrimSpace(v), "~") {
					m.unsupported = true
				}
			}
			m.partial = true
		default: // $dnsrewrite, $third-party, $script, $domain=, $popup, ...
			m.unsupported = true
		}
	}
	m.partial = m.partial && !m.unsupported
	return m
}
