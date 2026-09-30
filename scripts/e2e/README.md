# Disposable browser and DNS end-to-end gate

Run `make e2e` from a clean committed checkout with Docker and Git. It builds the actual embedded
frontend and Go application, then runs the browser, two Svart nodes, and controlled
DNS/list fixtures in one fresh container with `--network none`. The container has
only loopback interfaces, no published ports, and a four-CPU limit. All services
must become ready; missing tools, failed readiness and assertions fail the gate.
The build may fetch pinned toolchains, locked dependencies and Debian packages;
the test phase cannot contact the internet or the host network.

`make e2e-build` builds separately. `bash scripts/e2e/run.sh` runs an existing
image without any dependency downloads. Its revision label must match the checkout. Set `SVART_E2E_IMAGE` to choose the image
and `SVART_E2E_OUTPUT` to an empty evidence directory. Otherwise an evidence
directory is created under `TMPDIR` (or `/tmp`). Existing evidence is never reused
or removed. Only the uniquely named container created by this invocation is
removed after its complete evidence is copied out with `docker cp`. Local and remote
Docker daemons are supported; there is no host bind mount. If copying fails, the
container is retained and its recovery name is printed. No Docker socket or
production files enter the test container.

## Assertions and evidence

- Fresh root setup, rejected setup token, real account creation, rejected login,
  authenticated session and resolver configuration through the shipped UI.
- Actual UDP/TCP client DNS and UDP/TCP/DoH/DoT upstream transports; local list
  downloads, wildcard/exception handling, network assignment, list toggling and
  manual-rule mutations, checked against DNS replies and durable records.
- Two actual nodes with a generated local TLS certificate, authenticated peer
  sync, replicated group/list metadata, local list refresh and peer DNS policy.
- Desktop/mobile user creation, password/role editing, readonly authorization,
  token generation/copy/use/revocation, and actual UI peer pairing (rejected and
  valid codes), manual addition and removal.
- Desktop/mobile settings edits/readback, all resolver strategies, logging and
  upstream toggles; delayed real GET/PUT responses verify drafts survive refresh
  and in-flight saves. Group membership and bundle assignment change actual DNS;
  network/group/bundle creation, manual rules and deletion run through the UI.
- Desktop/mobile list rename/cancel, every refresh interval including Off,
  changed downloaded content with exact added/removed history, historical
  checkpoint selection/search, deletion, and bootstrap server append/removal order.
- Desktop/mobile simulator/matrix and history-source controls, dashboard ranges,
  log filters, policy expansion/copy/block/allow actions, rewrite create/edit/delete
  and exact changed DNS answers.
- Browser configuration download/upload, exact export round trip including
  disabled assignments and manual rules; malformed import preserves configuration.
- Restart preserves exact history rows. Real archival moves a deliberately cold
  summary fixture into Parquet. All four shipped Investigation templates execute
  over hot and cold data, with exact coalesced counts and cold date/month buckets.
  Keyboard execution, mobile execution and failed-query draft preservation run.
- Literal injection text remains text in the live DOM. Injected read/save errors
  expose errors and preserve drafts; retry uses the actual application handlers.
- Navigation, tabs, entity details, create forms and old-link query parameters;
  full-page screenshots and complete DOM/control inventories at 1440, 390 and
  3840 pixels. Inventory is not a claim that every control combination has a dedicated
  assertion: current mutation coverage is the flows listed above.
- Every successful DNS response is paired with a durable original journal
  payload. Final checks account for every original across live records and raw
  archives, independently reproduce archive digests from original bytes, and run
  the application's full raw-Parquet verifier. No journal-only count can pass
  after ownership transfers to archives.

The output preserves application logs, SQLite databases, journals, archives,
accepted DNS responses, TLS fixture files, raw byte/digest proofs, browser console
and network logs, a full embedded-content HAR, screenshots and inventories. These
contain only disposable test credentials, but include session cookies and the
fixture private key; handle the complete evidence as test-private. `result.json`
is written only after all browser, transport and raw-ownership checks pass.
Container/image metadata plus checkout status/diff record provenance.

`world.ts` owns service lifecycle and fixtures, `browser.ts` owns account/policy/
backup/navigation flows, `checks.ts` owns persistence/peer/security assertions,
`controls.ts` covers desktop/mobile control mutations, `config-controls.ts` covers
configuration concurrency, and `list-controls.ts` covers real list changes and history,
and `main.ts` orchestrates. They use the application's generated API parsers and
existing locked Playwright dependency. `npm --prefix scripts run verify` covers
formatting, strict ESLint/TypeScript and the supporting-tools unit gate; ShellCheck
and shfmt cover the runner. E2E scripts are test code, not production coverage.

A final integrated-source run is still required after other changes land. This
harness does not validate production routing, deployment recovery or physical-host
reboots; those require their separately owned rollout gates.
