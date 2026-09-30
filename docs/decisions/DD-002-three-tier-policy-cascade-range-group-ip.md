# DD-002: Three-tier policy cascade (Range > Group > IP)

### Published lists

We have a UI page / API to add "published lists." When we say "published list" we actually are referring to a link to Hagezi Ultimate, Steven Black gambling site list, Firebog Bot domains, etc. You give us the URL and an optional name by which you refer to it (and we display) and a refresh frequency (default 7 days). If we already have the list in memory and it's newer than the refresh frequency, do nothing. Otherwise, go to the URL, download the contents, parse them, enter them in a DB, timestamp it, hold it in memory, divide it into the necessary structures to handle wildcards, etc. Now our "published list" is ready to go.

In the codebase these are stored as `blocklists` (the table), fetched by `refreshBlocklistByID`, parsed by `parseDomain`, and classified at load time by `classifyDomain` into exact domains, wildcard suffixes, and complex wildcards (see DD-010).

### Three tiers

We have three tiers: **ranges**, **groups**, and **users** (IP addresses).

1. **Range/VLAN** (broadest) — CIDR-based. e.g. "IOT VLAN 10.42.3.0/24"
2. **Group** (middle) — e.g. "Sam's devices"
3. **IP/Device** (narrowest) — e.g. a specific phone at 10.42.1.43

**Tiers are independent, not nested.** A client (identified by IP address) can independently match a range (by CIDR) AND belong to a group (by explicit membership). Ranges don't contain groups. Groups don't belong to ranges. A server at 10.42.5.42 might match the "Server VLAN" range AND be in the "Homelab Infra" group — those are two separate policy tiers evaluated independently. My servers are in a bunch of different VLANs; their group rules should apply and overrule their range rules.

**A user belongs to zero to many groups and zero to many ranges.** You might be in a /24 range and a /8 range that conflict — block wins. You might be in Group "Family" and Group "Parental Controls" that conflict — block wins. You might be in no group and no range — that's fine, those tiers just have no opinion and the default is allow.

### What lives at each tier

The UI allows you to assign zero to many published lists to any tier entity (a range, a group, or a user). **Published lists are blocklists** — the default behavior is block. To be perfectly specific: at any given tier, it is likely you will have several published lists. You union all of them together. Any pattern that matches anything in that union is blocked at that tier.

As we may have weird situations where the blocklist is "mostly right," we give ourselves an escape hatch of **custom allow/deny rules**. These can be wildcards or simple domains. Within a single tier entity, a custom allow overrides a published list block (that's the whole point — the published list grabs a bit too much, so you create a custom allow for youtube.com). A custom deny adds a block even if the published lists don't cover it.

In practice, I would expect the custom rules in each tier to be relatively small — I wouldn't ever expect more than 100, and realistically would expect 0-20.

At any single tier entity, you can have custom allows OR custom denies for a given domain — not both at the same time. This must be enforced at write time. If a group already has a custom allow for `facebook.com` and you try to add a custom deny for `facebook.com`, reject it with 409 Conflict.

**Status**: Implemented. Same-tier conflict prevention enforced at write time via `domainConflicts()`. See wildcard overlap section below.

### Evaluation within a single entity

For a given domain and a single tier entity (one range, one group, or one client):

1. Check **custom allows** (allowlist data assigned to this entity)
2. If match → entity says **allow** (custom override of published list block)
3. If no allow match, check **published lists + custom denies** (blocklist data assigned to this entity)
4. If match → entity says **block**
5. If neither matches → entity has **no opinion**

### Multiple entities within a tier

A user may belong to multiple (sometimes overlapping) ranges or multiple groups. Each entity is evaluated independently per the rules above. If one entity says block and another says allow, **block always wins the tie** at the tier level.

Example: Client is in Group A and Group B. Group A evaluates to "block" (published lists block facebook.com, no custom allow override). Group B evaluates to "allow" (custom allow for facebook.com). Result at the group tier: **block** — block wins across entities within the same tier.

This is intentional. Think of the system as a sieve that nudges toward block. If any entity at a tier says block, the tier blocks.

### Cross-tier cascade

Once each tier is evaluated (each producing block, allow, or no opinion), the narrowest tier with an opinion wins:

1. If **IP tier** has a decision → that's the final answer
2. Else if **Group tier** has a decision → that's the final answer
3. Else if **Range tier** has a decision → that's the final answer
4. Else → **default allow** (domain passes to upstream resolver)

If NO rule at any tier applies, then allow. The system is a sieve: it nudges toward block, but if nothing blocks, allow.

### Worked example

Client `10.42.1.43` is in range "Home VLAN" (10.42.1.0/24), group "Sam's Devices." The domain is `facebook.com`.

**Range evaluation**: The range has Hagezi Ultimate assigned. facebook.com is in Hagezi. No custom allow override. Range says **block**.

**Group evaluation**: The group also has Hagezi assigned, so facebook.com is blocked. But the group has a custom allow for facebook.com. Custom allow overrides published list block. Group says **allow**.

**IP evaluation**: No direct lists or custom rules for this IP. No opinion.

**Cascade**: IP has no opinion. Group says allow. Range says block. Group is narrower than range → group wins → **ALLOWED**.

Now suppose we add a custom deny for facebook.com directly on this IP:

**Cascade**: IP says block (custom deny). Group says allow. Range says block. IP is narrowest → IP wins → **BLOCKED**.

### Multi-entity examples

**Overlapping groups**: Client is in Group "Family" and Group "Parental Controls." Family has Hagezi (blocks facebook.com). Parental Controls has a custom allow for facebook.com. Family evaluates to "block." Parental Controls evaluates to "allow." Block wins across groups → group tier is **block**.

**Overlapping CIDRs**: Client at 10.42.1.50 matches both `10.42.0.0/16` (broad, blocks facebook.com via published list) and `10.42.1.0/24` (narrow, custom allow for facebook.com). Broad range evaluates to "block." Narrow range evaluates to "allow." Block wins across ranges → range tier is **block**.

### Why

This matches how you actually think about network policy. VLANs define broad defaults. Groups give consistent experience across a person's devices. Per-IP rules handle individual exceptions. "Narrower tier wins" prevents the "Facebook works on phone but not laptop" problem. Independence between tiers means servers can be in a "Server VLAN" range AND a "Homelab Infra" group without one nesting inside the other. Block winning the tie within a tier is the safe default — you can always create an exception at a narrower tier.

### Implementation

`evaluatePolicy(clientIP, domain)` in policy.go replaces the old `isBlockedForClient` union logic. Three tiers are evaluated independently (Range → Group → IP), each handling multiple entities with block-wins-tie resolution. The narrowest tier with a decision wins. If no tier has a decision, default is allow (not blocked).

All tiers are evaluated from one immutable policy snapshot (ranges, groups, memberships and direct clients over a shared list index), rebuilt as a whole by `reloadPolicyState` (DD-035). Results are cached in `policyCache`, a byte-budgeted LRU (`POLICY_CACHE_SIZE_MB`). The debug endpoint `GET /api/policy/evaluate?client_ip=X&domain=Y` returns the full decision chain without caching. Query logs include `block_tier` and `block_rule` columns for analytics.

### Wildcard overlap and conflict prevention

At any single tier entity, you cannot have both a custom allow and a custom deny that overlap. `domainConflicts(existingDomains, newDomain)` checks four forms of overlap:

1. **Exact match**: `facebook.com` vs `facebook.com`
2. **Subdomain relationship**: `app.tiktok.com` is a subdomain of `tiktok.com` (either direction)
3. **Wildcard covers exact**: `*.tiktok.com` covers `app.tiktok.com` (and `video.tiktok.com`, etc.)
4. **Exact covered by wildcard**: adding `*.tiktok.com` when `app.tiktok.com` exists

Examples:

- Custom allow `*.tiktok.com` + custom deny `app.tiktok.com` → **conflict** (wildcard covers the exact)
- Custom deny `*.tiktok.com` + custom allow `app.tiktok.com` → **conflict** (wildcard covers the exact)
- Custom allow `tiktok.com` + custom deny `app.tiktok.com` → **NOT a conflict** (different domains; `tiktok.com` does not match `app.tiktok.com` via exact or wildcard)
- Custom allow `facebook.com` + custom deny `facebook.com` → **conflict** (exact match)

This is enforced at write time (409 Conflict). All four domain handlers (`handleAPIClientAllowDomain`, `handleAPIClientBlockDomain`, `handleAPIGroupAllowDomain`, `handleAPIGroupBlockDomain`) check for conflicts before inserting.

### Published list attribution

When multiple published lists contain the same domain, we attribute the match to the **smallest list** (fewest domain entries). Rationale: a match in a tiny, specialized list (e.g., "Gambling Sites — 500 domains") is more informative than the same match in a massive general list (e.g., "Hagezi Ultimate — 267k domains"). This gives the most specific, actionable answer to "why is this blocked?"

**Data flow**: The list index numbers its lists in ascending order of rule count (list ID breaks ties), so the lowest-numbered matching list of an entity is the smallest one. Hits in manual (empty-URL) lists get attribution ID 0 at the IP tier; at the group and range tiers they are reported under the manual list's alias (DD-035). List names come from the snapshot, so a rename takes effect with the reload the edit triggers.

**Evaluation output**: Each `EntityResult` shows both a `published_list` (if a published list matched) and a `custom_rule` (if a manual rule applied). A list_id > 0 is published; list_id == 0 is custom/manual. When a published block is overridden by a custom allow, both are shown — the entity `result` is "allow" but `published_list` still reveals what the list would have done.

**Future**: Show ALL matching lists per entity (not just the attributed one). The index already knows every list that holds a rule; it is just not surfaced in the API yet.

### Decision audit trail

Every DNS query logs full decision metadata via the async batch writer — not just blocks, but also allows and the policy path that produced them. Three columns added to `query_logs`: `block_source` (entity name), `block_list_id` (published list ID, 0 for custom), `block_list_name` (published list alias). The `block_` prefix is inherited from existing columns; `blocked` boolean disambiguates outcome.

Zero hot-path impact: `logQuery` sends to the 10M-entry buffered channel, and the DNS response is sent before the entry is even queued.

This enables attribution analytics: "which list blocks the most?", "which list has false positives?", per-list block rates over time. All queryable via `GET /api/query-logs` with the new fields in the response.

### Analysis endpoint

`GET /api/policy/evaluate?client_ip=X&domain=Y` is the "sister route" to DNS resolution. DNS just returns the IP (or block); this endpoint returns the full analysis. The response includes:

- `PolicyResult` with `result` ("block", "allow", ""), `client_ip`, `domain`, and `result_source` pointing to the winning entity
- Named tier evaluation fields: `range_evaluation`, `group_evaluation`, `ip_evaluation` (null when no assignments at that tier)
- Each `TierEvaluation` contains `entities` (every entity consulted), `result`, and `result_source`
- Each `EntityResult` shows `published_list` (which published list matched) and `custom_rule` (which manual rule applied) — both can be non-null when a custom allow overrides a published block
- `PublishedHit`: action, rule, list_id, list_name
- `CustomHit`: action, rule

This makes the full cascade visible for debugging ("why is this blocked?") and for a future UI that visualizes the policy engine.
