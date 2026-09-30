# DD-023: Rewrite NODATA response prevents internal hostname leaks

**Decision**: When a DNS query hits a rewrite but the query type doesn't match available IPs (e.g., AAAA query for an IPv4-only rewrite), return NODATA (empty answer section, NOERROR rcode) instead of passing the query to upstream resolvers.

**Why**: Modern DNS clients send A and AAAA queries simultaneously. If a domain like `opnsense-private.example.lan` has an IPv4 rewrite (A → 10.42.1.1), the A query gets rewritten locally but the AAAA query was previously forwarded to upstream (e.g., Mullvad). This leaked internal hostnames to third-party resolvers — a privacy violation. NODATA is the RFC-compliant response: it tells the client "this domain exists but has no records of the requested type," which is semantically correct for a rewritten domain with only IPv4 addresses.

**Implementation**: `checkRewrite()` in rewrites.go now always returns a DNS response message for rewritten domains. If matching IPs exist for the query type, they're included in the answer section. If not, the message is returned with an empty answer section (NODATA). The calling code in `handleDNSRequest` sees a non-nil response and returns it without consulting upstream.

**Revisit if**: Dual-stack rewrites become common (IPv4 + IPv6 for the same domain), making the NODATA case rare. Or if NODATA causes unexpected client behavior (shouldn't — it's standard DNS semantics).
