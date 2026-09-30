# Svart DNS

Svart is self-hosted DNS filtering built around two questions: **which stricter
lists match your network's traffic, and which devices should those rules apply to?**

It started with the frustration of using Pi-hole and wanting to block more,
without knowing where stronger lists would start breaking things. A block
percentage wasn't enough to make that decision. Svart brings list comparison
and targeted policy controls into the same interface so you can make those
choices using your own traffic.

## Why Svart?

- **Choose lists using your own traffic.** Compare downloaded, enabled lists
  against recent network domains, one device's observed domains, or examples
  you paste in. See which lists match each name and the rules responsible before
  deciding what to assign.
- **Give each network, group, or device the right policy.** Set a baseline for
  a source-IP subnet, shared rules for your work machines or family, and
  exceptions for one client. Device decisions override group decisions, which
  override the range. Named policies make repeated assignments manageable.
- **Understand a filtering decision.** Logs record matching rules, lists, and
  policy tiers. The Policy Simulator explains how the *current* range/group/client
  assignments resolve a batch of domains.
- **Investigate beyond a dashboard.** On Linux amd64, run read-only SQL from
  the Investigation UI across recent SQLite logs and older Parquet archives.
  Explore which devices contact a domain and how their query patterns change.
- **Fit DNS into your homelab.** Synchronize configuration across nodes,
  manage local name-to-IP rewrites through the API, and use Prometheus metrics
  and optional Loki query logs.

Svart runs as one Go process with an embedded web UI. Allowed queries forward
to your chosen UDP, TCP, DoT, or DoH upstreams, with weighted, random, or blended
selection and domain-specific resolver routes.

![Svart dashboard with synthetic demo traffic](docs/images/dashboard.png)

*Actual Svart UI with synthetic demo traffic. These values illustrate the UI,
not benchmark results.*

## Quick start

From a checkout of this repository, on a machine with Docker Engine and Compose
v2 installed:

```bash
docker compose up -d --build
docker compose logs svart
```

Find the `first-run setup` log entry, then open `http://<host-address>:3000`.
Paste the one-time token to create your administrator account. The setup wizard
then configures upstreams and filter lists; its initial choices are a
Quad9/Mullvad/Cloudflare DoH mix and Hagezi Pro. Review those choices for your
network. Until upstreams are configured, the built-in resolver is Quad9 DoH.

The supplied [compose.yaml](compose.yaml) builds the image locally, publishes
DNS on host port **53/UDP and 53/TCP**, and publishes the UI on **3000/TCP**.
Those ports must be available. Named volumes preserve the database and archives
across container replacement. Set `EXTERNAL_IP` in the Compose environment to
the host's LAN address if you want the setup page to show it.

Test a query from a permitted client, replacing the address below with your
server's address:

```bash
dig @<host-address> example.com
dig +tcp @<host-address> example.com
```

Then configure your router's DHCP DNS setting, or an individual device, to use
that address. Svart does not provide DHCP. For consistent filtering, every DNS
server advertised to clients should apply your intended policy. The default
DNS access list permits loopback, private, CGNAT, link-local, and IPv6 ULA
addresses; configure `ALLOWED_CLIENTS` for other networks.

For source builds, prerequisites, TLS, and development commands, see
[Getting started](docs/getting-started.md), [Configuration](docs/configuration.md),
and [Operations](docs/operations.md). The running server serves its API reference
at `/docs` and its OpenAPI document at `/docs/swagger.json`.

## Choosing Svart

Choose Svart if you want traffic-based list comparison, an explicit
range/group/device policy cascade, and SQL investigation in one application.
It is especially useful when you want stricter filtering on servers, different
rules for work and family devices, and a way to trace why a particular name
was allowed or blocked.

Pi-hole and AdGuard Home are established alternatives that also offer query
visibility, per-client controls, and APIs. Pi-hole includes optional DHCP;
AdGuard Home also provides encrypted DNS listeners for clients. See their
[client/group policy examples](https://docs.pi-hole.net/group_management/example/)
and [client settings](https://adguard-dns.io/kb/adguard-home/clients/).

Svart uses in-memory policy snapshots and caches; list analysis runs on demand.
[Reproducible benchmarks](docs/benchmarks.md) document throughput, logging,
latency, and memory tradeoffs rather than promising universal performance parity.

## Scope and compatibility

- Clients use **DNS over UDP or TCP**. DoH and DoT are upstream transports;
  Svart does not supply DHCP or recursive resolution.
- DNS visibility covers queries sent to Svart and the source IP it receives.
  Devices behind the same source IP share that identity; traffic sent to another
  resolver requires separate network controls or telemetry.
- Analysis compares list matches against observed domains. The Policy Simulator
  evaluates current assignments; it does not replay a complete hypothetical
  policy change or predict whether every application will still work.
- Filter Lists reports applied, unsupported, and invalid rules. A failed refresh
  keeps the previous generation. See [list syntax](docs/configuration.md#list-syntax)
  for supported AdGuard-style rules and deliberate differences.
- Investigation is supported on Linux amd64. See [resource sizing and other
  platforms](docs/operations.md#resource-use). Query history has configurable
  retention and an explicit [logging durability](docs/logging-durability.md)
  boundary; [raw archives](docs/raw-archives.md) retain original events.

## Documentation and contributing

- [Overview](docs/overview.md) — purpose and common workflows
- [Architecture](docs/architecture.md) — request paths and storage
- [Code layout](docs/code-layout.md) — where to find each subsystem
- [Getting started](docs/getting-started.md) — build and development setup
- [Configuration](docs/configuration.md) and [Operations](docs/operations.md)
- [Design decisions](docs/decisions/README.md) and [Roadmap](docs/roadmap.md)
- [Contributing](CONTRIBUTING.md) and [Security policy](SECURITY.md)

Run `make verify` for formatting, strict lint/type checks, generated API
consistency, Go tests and race detection, all four language coverage gates,
public-export checks, dependency vulnerability checks, and frontend license
checks. Each owned production language must meet at least 80% coverage; the
frontend also gates branches and functions. Run `make e2e` for the isolated
browser, two-node DNS, backup/restart, and archive-ownership checks. The canonical Forgejo CI runs both
against the exact event commit. See [Contributing](CONTRIBUTING.md) for tool
prerequisites and [the E2E guide](scripts/e2e/README.md) for its precise scope.

## License and name

Svart is [MIT licensed](LICENSE); dependencies retain their own licenses, listed
in [Third-party notices](THIRD_PARTY_NOTICES.md). *Svart* means black in Swedish.
