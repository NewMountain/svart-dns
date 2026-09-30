package main

import (
	"fmt"
	"strings"

	"github.com/yeti/svart-dns/internal/listparse"
)

// Search and page ordering use source text; reconstruction and row identity
// continue to use the stored representation. Identifiers are fixed, not input.
var storedRuleTextSQL = fmt.Sprintf(
	"CASE WHEN substr(domain,1,%d)='%s' THEN json_extract(substr(domain,%d),'$.text') ELSE domain END",
	len(listparse.StoredPrefix), listparse.StoredPrefix, len(listparse.StoredPrefix)+1,
)

func reservedStoredRule(rule string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rule)), "!svart-rule-")
}

func storedRuleDisplayText(stored string) (string, error) {
	if !reservedStoredRule(stored) {
		return stored, nil
	}
	rule, err := listparse.DecodeStored(stored)
	if err != nil {
		return "", fmt.Errorf("read stored rule for display: %w", err)
	}
	return rule.Text, nil
}
