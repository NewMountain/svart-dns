# Overview

Svart is a DNS forwarding sinkhole with a web interface for investigating network
queries and adjusting filtering policy. It began as a personal project by a
long-time Pi-hole and AdGuard Home user who wanted to compare lists against real
traffic, apply exceptions to particular devices, and query longer histories.
The current feature comparison is in the [README](../README.md#how-it-compares).

## The workflow

1. **Set up resolvers and a baseline.** The first-run wizard creates an admin,
   chooses upstream resolvers, and assigns a filter list to network-wide ranges.
2. **Observe traffic.** Dashboard and Logs show the traffic reaching this node,
   outcomes, upstream use, and policy reasons. Aliases make client IPs readable.
3. **Compare before changing.** Analysis offers a list matrix and policy
   simulation against observed queries. It helps answer whether a stricter list
   would affect traffic you already use; it cannot predict every future request.
4. **Apply the policy where it belongs.** Filter Lists manages source content.
   Assignments connects lists and custom rules to ranges, groups, and client IPs.
   A device-specific exception can leave the rest of the network's policy intact.
5. **Investigate history.** Administrators can run restricted read-only SQL over
   recent SQLite query logs and archived Parquet data through Investigation.

The range, group, and client tiers are evaluated separately. Within an entity,
allow rules override blocks; across entities in the same tier, a block wins.
The narrowest tier with a decision wins: client, then group, then range. With no
matching decision the request is allowed. Rewrites take precedence over this
filtering cascade. See [Architecture](architecture.md) for the DNS path.

## Scope and tradeoffs

Svart serves UDP/TCP DNS and forwards queries over UDP, TCP, DoT, or DoH.
It has no recursive resolver, authoritative zone service, DHCP server, or
encrypted DNS listener for clients. Upstream distribution lets you choose how
queries are spread across resolvers; it is not an anonymity guarantee.

Filtering identity is the source IP seen by Svart. A router that forwards all
queries under its own address hides individual devices. Traffic sent to another
DNS resolver is outside Svart's visibility. Filtering syntax compatibility is
partial; the [limitations](../README.md#known-limitations) matter when migrating
existing AdGuard lists.

One process serves the Go backend and embedded React UI. SQLite holds
configuration and recent query logs. Parquet and embedded DuckDB support older
history without a separate database server. Heavy query loops appear as counted
summary rows in the UI and Investigation. A separate durable journal and raw
archive retain the original logged events under the configured retention policy;
these are query records, not DNS packet captures. Replies stay asynchronous, so
an abrupt crash can lose the newest events still in memory. See
[logging durability](logging-durability.md) and [raw archives](raw-archives.md).
Choose retention and access controls appropriate to the DNS history you store.

Multiple nodes can synchronize configuration while each continues resolving
independently. Query logs, answer caches, and downloaded list contents remain
local. Client failover depends on the router and devices; sync itself does not
provide a virtual IP or force clients to choose a healthy server.

## Development direction

Current implementation decisions are recorded in [decisions](decisions/README.md).
The [roadmap](roadmap.md) describes planned work and is not a list of shipped
features. The code remains a single Go package with feature files; see
[Code layout](code-layout.md). Measured performance belongs in
[Benchmarks](benchmarks.md), alongside the commands and workload needed to
reproduce it.
