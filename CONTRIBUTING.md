# Contributing

Thanks for helping. Bug reports, list-compatibility reports, benchmark
results from your hardware and pull requests are all welcome.

## Before you start

- Read [AGENTS.md](AGENTS.md): it is short and lists the rules every change
  follows (the DNS path is measured, data is never dropped, input is validated
  at the boundary, defaults fail closed).
- For anything larger than a bug fix, open an issue first so we can agree on
  the approach.

## Making a change

1. `make verify` and `make e2e` must pass. The verification target includes
   the Go race detector; E2E runs the real browser and DNS service in an
   isolated container with no external network.
2. Add tests with realistic data. A bug fix comes with a regression test that
   fails without the fix.
3. Changes on the DNS path include before/after numbers from the benchmarks
   (`go test -bench …` or `make bench-throughput`).
4. Update the matching page under `docs/` in the same pull request. Significant
   design decisions get a record in `docs/decisions/`.
5. Keep commits small and use conventional messages (`fix(dns): …`,
   `feat(ui): …`, `docs: …`).

The [verification tool image](scripts/verify/Dockerfile) pins the production
Go 1.26 toolchain, Node/npm, uv, ShellCheck, shfmt, Go analyzers and Gitleaks.
CI copies an exact clean checkout into this image, runs `make verify`, and
retains coverage profiles and disposable export fixtures. The separate
[E2E image](scripts/e2e/Dockerfile) adds Chromium, DNS and TLS fixture tools;
`make e2e` builds it from the current clean commit. Its [guide](scripts/e2e/README.md)
lists assertions, retained evidence and the deployment checks it cannot replace.

Go coverage includes every tracked production package and counts inactive
platform-specific statements as uncovered. JavaScript/Python helper inventories
also fail if a new tracked helper escapes their configured checks. Frontend
coverage includes all production TS/TSX files. Coverage supplements behavioral
proof; new tests must exercise a meaningful success, failure or recovery case.

Python helper validation requires uv 0.12.18 and Python 3.11 or later.
`make verify-python` uses the locked Ruff, mypy and coverage versions to check
formatting, lint, strict types, and at least 80% coverage of all owned Python
helpers. Its tests exercise real disposable Git repositories, gzip members,
child processes, complete output, and existing evidence preservation. The
coverage report includes helpers that no test executes; a repository check
rejects a tracked Python helper outside the configured coverage roots.
This target is also part of `make verify`.

`make test-api-clients` bundles the tracked TypeScript fixture with the locked
frontend tools, starts disposable HTTP and DNS fixtures, then exercises the
same generated clients as the UI. It verifies real mutations, persisted policy
over UDP/TCP, and complete anonymous documentation responses. It runs under
the Go race detector and is included in `make verify`.

## Reporting bugs

Include the Svart version (`/health` or the image tag), how you run it
(Docker, binary, number of nodes), the relevant log lines (`LOG_LEVEL=debug`
if you can), and for DNS behavior the exact query (`dig @svart name type`).

Security issues: please report privately, see [SECURITY.md](SECURITY.md).

By contributing you agree that your contributions are licensed under the
[MIT License](LICENSE). Participation is governed by the
[Code of Conduct](CODE_OF_CONDUCT.md).
