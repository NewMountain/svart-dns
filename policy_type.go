package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/miekg/dns"
)

// Existing analysis callers omitted the type because policy was name-only.
// A is the explicit default; ANY remains a real question, not a wildcard.
func parsePolicyRecordType(value string) (uint16, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return dns.TypeA, nil
	}
	if code, ok := dns.StringToType[value]; ok && code != 0 {
		return code, nil
	}
	if strings.HasPrefix(value, "TYPE") {
		code, err := strconv.ParseUint(strings.TrimPrefix(value, "TYPE"), 10, 16)
		if err == nil && code != 0 {
			return uint16(code), nil
		}
	}
	return 0, fmt.Errorf("invalid DNS record type %q; use a type name such as A, AAAA, TXT or TYPE1 through TYPE65535", value)
}
