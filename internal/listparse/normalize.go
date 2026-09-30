package listparse

import (
	"net/netip"
	"strings"
	"unicode/utf8"
)

// NormalizeEntry puts a blocklist/allowlist entry in the form queries
// arrive in (D18): no trailing dot (except on wildcard patterns), lowercase, IDN labels in punycode (the
// wire form, e.g. "bücher.example" -> "xn--bcher-kva.example") and IP
// literals in canonical form ("2001:DB8:0::BAD" -> "2001:db8::bad", the form
// response addresses are compared in). Entries that differed only in these
// ways used to never match.
//
// IDN handling is lowercase plus RFC 3492 punycode per label, not full
// UTS 46 mapping: that would need golang.org/x/text (a new module) for NFC
// and compatibility mappings, which list entries practically never need.
func NormalizeEntry(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "*") {
		// On a wildcard pattern a trailing dot is part of what it matches
		// ("*shop." = contains "shop."), so only plain names lose it.
		s = strings.TrimSuffix(s, ".")
	}
	if s == "" {
		return ""
	}
	if couldBeIP(s) {
		if a, err := netip.ParseAddr(s); err == nil {
			return a.Unmap().String()
		}
	}
	s = strings.ToLower(s)
	if isASCII(s) {
		return s
	}
	labels := strings.Split(s, ".")
	for i, l := range labels {
		// A label with a wildcard cannot be punycoded meaningfully; leave it.
		if isASCII(l) || strings.Contains(l, "*") {
			continue
		}
		enc, ok := punycodeEncode(l)
		if !ok {
			return s
		}
		labels[i] = "xn--" + enc
	}
	return strings.Join(labels, ".")
}

// couldBeIP is false for strings netip.ParseAddr always rejects (an IPv4
// address is digits and dots, an IPv6 one contains a colon). Skipping the
// parse for names avoids allocating its error for every row a list rebuild
// normalizes (a third of the rebuild's allocations on real lists).
func couldBeIP(s string) bool {
	if strings.IndexByte(s, ':') >= 0 {
		return true
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c != '.' && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// RFC 3492 section 5 parameters.
const (
	punyBase        = 36
	punyTMin        = 1
	punyTMax        = 26
	punySkew        = 38
	punyDamp        = 700
	punyInitialBias = 72
	punyInitialN    = 128
)

// punycodeEncode is the RFC 3492 section 6.3 encoder for one label. It
// reports false for invalid UTF-8.
func punycodeEncode(label string) (string, bool) {
	if !utf8.ValidString(label) {
		return "", false
	}
	runes := []rune(label)
	out := make([]byte, 0, len(label)+8)
	for _, r := range runes {
		if r < utf8.RuneSelf {
			out = append(out, byte(r))
		}
	}
	basic := len(out)
	handled := basic
	if basic > 0 {
		out = append(out, '-')
	}
	n, delta, bias := punyInitialN, 0, punyInitialBias
	for handled < len(runes) {
		m := int(^uint32(0) >> 1)
		for _, r := range runes {
			if int(r) >= n && int(r) < m {
				m = int(r)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, r := range runes {
			if int(r) < n {
				delta++
				continue
			}
			if int(r) > n {
				continue
			}
			q := delta
			for k := punyBase; ; k += punyBase {
				t := k - bias
				if t < punyTMin {
					t = punyTMin
				} else if t > punyTMax {
					t = punyTMax
				}
				if q < t {
					break
				}
				out = append(out, punyDigit(t+(q-t)%(punyBase-t)))
				q = (q - t) / (punyBase - t)
			}
			out = append(out, punyDigit(q))
			bias = punyAdapt(delta, handled+1, handled == basic)
			delta = 0
			handled++
		}
		delta++
		n++
	}
	return string(out), true
}

func punyDigit(d int) byte {
	const digits = "abcdefghijklmnopqrstuvwxyz0123456789"
	return digits[d]
}

func punyAdapt(delta, numPoints int, first bool) int {
	if first {
		delta /= punyDamp
	} else {
		delta /= 2
	}
	delta += delta / numPoints
	k := 0
	for delta > ((punyBase-punyTMin)*punyTMax)/2 {
		delta /= punyBase - punyTMin
		k += punyBase
	}
	return k + (punyBase-punyTMin+1)*delta/(delta+punySkew)
}
