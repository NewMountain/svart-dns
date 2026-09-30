# Third-party software

Svart is MIT licensed. It includes or links the following components, all
under permissive licenses. `scripts/check-licenses.mjs` enforces this for the
web UI's production dependencies on every build; Go modules were checked with
`go-licenses` (MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0).

| Component | Use | License |
|---|---|---|
| [miekg/dns](https://github.com/miekg/dns) | DNS wire format and server | BSD-3-Clause |
| [mattn/go-sqlite3](https://github.com/mattn/go-sqlite3) (SQLite) | database | MIT (SQLite: public domain) |
| [marcboeker/go-duckdb](https://github.com/marcboeker/go-duckdb) (DuckDB) | query investigation engine | MIT |
| DuckDB `sqlite_scanner` extension d5d6265, DuckDB v1.1.3, linux/amd64 (embedded binary) | reads the SQLite query log from DuckDB; [provenance and checksum](web/duckdb-extensions/provenance.json), [license](web/duckdb-extensions/LICENSE.sqlite-scanner) | MIT |
| [parquet-go](https://github.com/parquet-go/parquet-go) | query archives | Apache-2.0 |
| [prometheus/client_golang](https://github.com/prometheus/client_golang) | metrics | Apache-2.0 |
| [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) (bcrypt) | password hashing | BSD-3-Clause |
| [React](https://react.dev), [React Router](https://reactrouter.com) | web UI | MIT |
| [ApexCharts](https://apexcharts.com) 4.7.0, react-apexcharts 1.7.0 | dashboard charts (pinned: later wrapper versions are not permissively licensed) | MIT |
| [CodeMirror 6](https://codemirror.net) | SQL editor | MIT |
| [RapiDoc](https://rapidocweb.com) | API reference at `/docs` | MIT |
| [Inter](https://rsms.me/inter/), [JetBrains Mono](https://www.jetbrains.com/lp/mono/) | self-hosted fonts | SIL Open Font License 1.1 |

Blocklists are downloaded by the operator from their publishers at run time
and are not distributed with Svart; each list keeps its own license.

Analysis ranking data is also supplied by the operator and is not distributed
with Svart. Configure a local `TOP_SITES_PATH` CSV under the dataset publisher's
terms; Svart does not download ranking datasets. Earlier private development
revisions embedded a Tranco CSV. It is excluded from this release and from the
fresh-history public export; the software's MIT license does not relicense
operator-provided datasets.
