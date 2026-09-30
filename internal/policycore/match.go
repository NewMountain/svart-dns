package policycore

import (
	"net/netip"
	"strings"
)

// ClassifyDomain categorizes a domain entry into one of three buckets:
// "exact" (no wildcard), "suffix" (*.something.com), or "complex" (ad*.foo*.com).
func ClassifyDomain(domain string) (bucket string, value string) {
	if !strings.Contains(domain, "*") {
		return "exact", domain
	}
	if strings.HasPrefix(domain, "*.") && !strings.Contains(domain[2:], "*") {
		return "suffix", domain[2:]
	}
	return "complex", domain
}

// ReverseARPAToIP returns the canonical address a full reverse-Lookup name
// (no trailing dot) encodes: "30.64.200.193.in-addr.arpa" -> "193.200.64.30",
// and the 32-nibble ip6.arpa form -> "4321:0:1:2:3:4:567:89ab". Anything else,
// including partial reverse zones, gives "".
func ReverseARPAToIP(arpa string) string {
	switch {
	case strings.HasSuffix(arpa, ".in-addr.arpa"):
		parts := strings.Split(strings.TrimSuffix(arpa, ".in-addr.arpa"), ".")
		if len(parts) != 4 {
			return ""
		}
		a, err := netip.ParseAddr(parts[3] + "." + parts[2] + "." + parts[1] + "." + parts[0])
		if err != nil {
			return ""
		}
		return a.String()
	case strings.HasSuffix(arpa, ".ip6.arpa"):
		nibbles := strings.TrimSuffix(arpa, ".ip6.arpa")
		if len(nibbles) != 63 { // 32 nibbles and 31 dots
			return ""
		}
		var b [16]byte
		for i := 0; i < 32; i++ {
			if i < 31 && nibbles[2*i+1] != '.' {
				return ""
			}
			v, ok := HexNibble(nibbles[2*i])
			if !ok {
				return ""
			}
			pos := 31 - i // least significant nibble comes first
			if pos%2 == 0 {
				b[pos/2] |= v << 4
			} else {
				b[pos/2] |= v
			}
		}
		return netip.AddrFrom16(b).Unmap().String()
	}
	return ""
}

// HexNibble decodes an ASCII hexadecimal digit.
func HexNibble(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
