# Getting started

Svart runs as one process: a DNS server (UDP and TCP) and a web UI/API on a
second port. State lives in the main SQLite database, durable journal/sidecar files, and
a directory of Parquet archives. There is nothing else to install or run next to it.

## Requirements

- Linux (amd64). Other platforms build from source but are not tested.
- 512 MB of RAM for typical home use with one or two large blocklists; see
  [Resource use](operations.md#resource-use) for measured numbers.
- For Docker: Docker Engine 24+ with Compose v2.

## Option 1: Docker Compose (recommended)

```bash
git clone <this repository> svart && cd svart
docker compose up -d
docker compose logs svart | grep "first-run setup"
```

The last command prints a one-time **setup token** and the setup URL. Open
`http://<this-host>:3000`, paste the token, and the setup page walks you
through:

1. creating the administrator account,
2. choosing encrypted upstream resolvers (a privacy mix of Quad9, Mullvad and
   Cloudflare over DNS-over-HTTPS is the default),
3. choosing blocklists (Hagezi Pro is the default) and applying them to every
   device on the network,
4. the address to put in your router's DHCP settings as the DNS server.

The compose file publishes port 53 on the host to Svart's unprivileged
listener (5353 inside the container) and port 3000 for the UI. If port 53 is
already taken on the host (for example by `systemd-resolved`'s stub
listener), free it or change the published port.

Until you configure an upstream, Svart resolves through Quad9 over
DNS-over-HTTPS so the network keeps working during setup.

### Set the host's address

Inside a container Svart can only see its container address, so the setup
page asks you to use the host's LAN address. To have it shown there (and used
for `127.0.0.1` remapping in the query log), set `EXTERNAL_IP` in
`compose.yaml`:

```yaml
    environment:
      DNS_PUBLISHED_PORT: "53"
      EXTERNAL_IP: "192.168.1.2"
```

## Option 2: a binary

Build it once (see below) or download a release, then:

```bash
sudo setcap cap_net_bind_service=+ep ./svart-dns   # allow port 53 without root
DNS_PORT=53 ./svart-dns
```

The first-run setup token is printed to the terminal. The database is created
as `./svart-dns.db` unless `DB_PATH` says otherwise. Every setting is listed
in [configuration.md](configuration.md).

## Option 3: from source

Requirements: Go 1.26+ with CGO (a C/C++ toolchain; SQLite and DuckDB are
linked in), Node.js 24+ for the web UI.

```bash
make build        # builds the web UI, then the svart-dns binary
./svart-dns
```

## Pointing your network at Svart

The usual way is your router's DHCP settings: set the DNS server handed to
clients to Svart's address, and remove any secondary public resolver (devices
would otherwise bypass Svart whenever they feel like it). For redundancy, run
two Svart nodes and hand out both; see
[High availability](operations.md#high-availability).

Only clients on private networks are answered by default (RFC 1918, CGNAT,
loopback, link-local and IPv6 ULA). If you serve other clients, a VPN range
for example, list them in `ALLOWED_CLIENTS`.

## Development

The [verification image](../scripts/verify/Dockerfile) records pinned tool versions.
Local verification also needs those Go analyzers, ShellCheck/shfmt, Gitleaks,
uv 0.12.18 and Python 3.11 or later; Python dependencies come from `uv.lock`.
The browser E2E target requires Docker and builds its own tools.

| Command | What it does |
|---|---|
| `make verify` | all language formatting, strict lint/types, tests/race/coverage, API/export and dependency/license checks |
| `make e2e` | build and run the real browser, two-node DNS and persistence fixtures with no external network |
| `make verify-python` | locked Ruff formatting/lint, strict mypy, real helper tests and at least 80% coverage |
| `make test-go` | Go vet and unit/integration tests |
| `make test-go-race` | the same under the race detector |
| `make test-hermetic` | Go tests with loopback only; external networking is unavailable |
| `make dev-frontend` | Vite dev server with hot reload, proxying `/api` to a running backend on `:3006` |
| `make bench-throughput` | throughput and latency of the built binary against a local stub upstream |
| `make bench-footprint` | memory use as lists, entities and cached names grow |
| `make bench-compare` | head-to-head with Pi-hole, AdGuard Home and Technitium (Docker) |

Benchmarks download their blocklists once into `benchmarks/lists/`
(`make bench-lists`) and never touch the internet afterwards; see
[benchmarks.md](benchmarks.md).

Where things live in the code is described in [code-layout.md](code-layout.md);
how the pieces fit together in [architecture.md](architecture.md).
