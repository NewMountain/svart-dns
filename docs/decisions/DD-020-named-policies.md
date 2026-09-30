# DD-020: Named Policies

**Decision**: Introduce reusable Named Policies that bundle published blocklist/allowlist selections and custom rules. A policy can be assigned to any range, group, or client IP (one policy per entity, nullable `policy_id`). Policy lists and rules are merged with the entity's own assignments at load time — the DNS hot path is unchanged.

**Why**: Configuring the same set of blocklists, allowlists, and custom rules for every range/group/client is repetitive and error-prone. Named Policies solve this with a single template that propagates instantly to all assigned entities. Editing a policy immediately reloads all in-memory stores, so every entity using that policy picks up the change.

**Design principles**:
1. **Merged at load time**: building the policy snapshot (DD-035) adds each entity's policy lists to its own list sets. DNS hot path sees no extra indirection.
2. **Tier custom rules win**: If a policy blocks `example.com` but the entity allows it, the entity's allow wins (existing `buildEntityResult` semantics — allow beats block within an entity).
3. **Strictly additive**: Entities cannot unselect a policy's published lists. They can only add more on top. In the UI, policy-sourced lists appear locked with a purple "Policy" badge.
4. **One policy per entity**: Nullable `policy_id` column on `ip_ranges`, `client_groups`, and a separate `client_policies` junction table for per-IP assignment. No many-to-many — keeps the merge logic simple and predictable.
5. **Immediate propagation**: Every policy mutation triggers the same `reloadPolicyState` as any list/rule mutation.

**Schema**: `policies(id, name UNIQUE, description, created_at, updated_at, node_id)`, `policy_blocklists(policy_id, blocklist_id)`, `policy_allowlists(policy_id, allowlist_id)`, `client_policies(client_ip PK, policy_id)`. Policy custom rules reuse existing `blocked_domains`/`allowed_domains` tables via manual lists (same pattern as `getOrCreateGroupManualBlocklist`).

**Sync**: New sync types `SyncPolicy`, `SyncPolicyList`, `SyncClientPolicy` using natural keys (policy name, policy_name|list_alias, client_ip). `SyncRange`/`SyncGroup` extended with `PolicyName` resolved to local `policy_id` on merge. Tombstone support for all policy tables.

**Config export/import**: `PolicyExport{Name, Description, Blocklists[]alias, Allowlists[]alias}`. Policies imported before groups/ranges/clients (dependency order). Entity exports include policy name reference.

**Frontend**: Policies tab (first tab) in the Tiers page with full CRUD, list toggle, and custom rule management. Entity detail views gain a policy selector dropdown and display policy-sourced lists as locked with disabled toggles.

**Revisit if**: Need for multiple policies per entity (would require junction table), or policy inheritance/layering (policy A extends policy B).
