# Keep application code and tests together outside the project root

The project root contained 97 application Go files and 166 same-package test
files. That made the public repository hard to navigate. Moving tests alone
would break their access to package-private application behavior.

The application and all its tests now live together in `internal/svart`.
`cmd/svart-dns/main.go` calls the application entrypoint. Root `assets.go` embeds
the existing frontend, documentation, and DuckDB extension without moving or
duplicating those assets. Parser, policy-core, telemetry, and API-generator
packages retain their existing boundaries.

Build commands target `./cmd/svart-dns`. API generation reads the application
package, and test setup retains the repository-root working directory used by
existing fixtures. The reader-bridge linker flag targets the application package.
Filtering behavior, wire contracts, data formats, and deployment mode are unchanged.

This is source organization, not further decomposition of the application or
a change to the runtime architecture.
