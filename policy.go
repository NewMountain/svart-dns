package main

import (
	"net/netip"
	"strings"

	"github.com/yeti/svart-dns/internal/policycore"
)

// policyCache holds evaluated decisions in front of the snapshot. Its byte
// budget is POLICY_CACHE_SIZE_MB (dnsguard.go), applied at startup.
var policyCache = newPolicyLRU(defaultPolicyCacheMaxBytes)

// evaluatePolicy is the policy engine entry point.
//
// Each tier (range, group, IP) can have multiple entities (multiple matching CIDRs,
// multiple groups). Within a single entity: published lists define the baseline blocks,
// custom allows carve out exceptions ("Hagezi blocks youtube but I need it"), and
// custom blocks add domains that no published list covers ("sketchy-tracker.io isn't
// in any list yet"). Allow is checked before block so a custom allow overrides a
// published list block within the same entity.
//
// Across entities within the same tier, block wins the tie.
// Across tiers, the narrowest tier with a decision wins.
// If no tier has a decision, default is allow.
func evaluatePolicy(clientIP, domain string, rrtype uint16) *policycore.PolicyResult {
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		// Not an IP (API callers can pass anything): evaluate uncached
		// rather than share a zero-address cache slot between callers.
		return evaluatePolicyFull(clientIP, domain, rrtype)
	}
	return evaluatePolicyAddr(addr, clientIP, domain, rrtype)
}

// evaluatePolicyAddr is evaluatePolicy for callers that already hold the
// parsed client address (the DNS handler), saving the parse per lookup.
// clientIP must be addr's string form.
func evaluatePolicyAddr(addr netip.Addr, clientIP, domain string, rrtype uint16) *policycore.PolicyResult {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	cacheKey := policyKey{client: addr, domain: domain, rrtype: rrtype}

	if cached, ok := policyCache.Load(cacheKey); ok {
		return cached
	}

	// Capture the generation BEFORE loading the snapshot. A reload publishes
	// its snapshot and then bumps the generation, so a result computed from
	// the previous snapshot carries a stale generation and is never served
	// (policycache.go, A3).
	gen := policyCache.Generation()
	result := policyState.Load().Evaluate(clientIP, addr, domain, rrtype)
	policyCache.StoreGen(cacheKey, result, gen)
	return result
}

// evaluatePolicyFull evaluates without the cache (debug endpoint, simulator).
// A clientIP that is not an address matches no range.
func evaluatePolicyFull(clientIP, domain string, rrtype uint16) *policycore.PolicyResult {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	addr, err := netip.ParseAddr(clientIP)
	if err != nil {
		addr = netip.Addr{}
	}
	return policyState.Load().Evaluate(clientIP, addr, domain, rrtype)
}

// responseIPsBlocked checks a resolved answer's A/AAAA addresses (see
// extractResponseIPs; the cache extracts them once per stored answer)
// against IP-literal list entries (e.g. "193.200.64.30" for a known malware
// host), so that DNS rebinding or CNAME cloaking cannot bypass name blocking
// by resolving to an already-blocked address.
func responseIPsBlocked(clientIP string, ips []string) bool {
	if len(ips) == 0 {
		return false
	}
	return policyState.Load().ResponseIPsBlocked(clientIP, ips)
}
