# Supporting tool quality

`make verify-tools` installs the exact versions in `package-lock.json`, then runs
Prettier, ESLint's strict type-checked rules, TypeScript with strict JavaScript
checking, and real process/file/Git behavioral tests under c8. No runtime
packages are added to the tools; Node runs the existing `.mjs` entrypoints.

The JavaScript production denominator is every maintained executable tool in
this directory: `check-doc-links.mjs`, `check-licenses.mjs`,
`check-public-secrets.mjs`, and `npm-audit.mjs`. c8's `--all` includes unexecuted
files. An inventory assertion rejects tracked JS/TS tooling outside this
measured set, so additions require explicitly updating the gate. Tests and the
ESLint configuration are development infrastructure, not executable product
or release tools. Frontend TypeScript and JavaScript have their separate gate.
Complete V8 and JSON/LCOV reports are available in `scripts/coverage`.

The threshold is 80% for statements, lines, branches, and functions. The
fixtures drive the actual CLI entrypoints against isolated lockfiles, Git
repositories, secret reports and npm child-process responses. They assert
failure for malformed metadata and unsuccessful child processes. Secret
fixtures verify exact-match exceptions and ensure rejected values do not
appear in diagnostics. No live npm audit or external network is needed.

`make verify-shell` requires ShellCheck 0.11.0 and shfmt v3.14.1 and checks every
tracked shell script. ShellCheck follows sources relative to their scripts;
missing tools and different versions fail.

The portable generated-client integration fixture
`testdata/api-client-contract.ts` is also formatted, linted and strictly typed by
this gate. It remains test infrastructure and is executed by
`make test-api-clients` against the real disposable Go HTTP and DNS services;
it is not counted as production JavaScript coverage.
