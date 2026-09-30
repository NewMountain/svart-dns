# DD-034: UI names follow the two concepts: filter lists and assignments

**Decision**: The UI names the two things users manage for what they are.

| Before | After |
|---|---|
| Filters (page) | **Filter Lists**: the rules (published lists and their history) |
| Tiers (page) | **Assignments**: who the rules apply to |
| Ranges (VLANs) tab | **Networks** |
| Groups tab | Groups |
| Clients (IPs) tab | **Clients** |
| Policies tab, "Policy" picker | **Bundles**, "Bundle" picker: saved sets of lists and allow/block rules attached to a network, group or client in one step |

The sidebar is ordered by workflow: Dashboard, Logs, Filter Lists,
Assignments, Rewrites, Analysis, Investigation, Config, Admin. Old URLs
(`/filters`, `/tiers?tab=ranges`, `tab=policies`) redirect to the new ones.

**Why**: "Tiers" described the evaluation mechanism, not what a user does on
the page, and "Policies" there collided with the generic sense of "policy".
Users think of filter lists as the rules and the tiers page as where those
rules get applied. No behavior changed: the API, database and evaluation
order (client over group over network) keep their existing names (`ranges`,
`policies`, `tier`), so integrations are unaffected.

**Alternatives considered**: "Devices" (a network or group is not a device),
"Clients" for the page (collides with the Clients tab), "Rules" (collides with
the custom allow/block rules inside each entity).
