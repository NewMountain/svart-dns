.PHONY: check-api build run run-tls clean docs docker docker-run docker-down bench-lists bench-compare bench-footprint bench-throughput build-frontend dev-frontend lint-frontend test-frontend security security-frontend security-go fmt-go check-fmt test-go test-hermetic test-go-race test-benchmarks test-public-check test-export verify

# Frontend
build-frontend:
	cd frontend && npm ci --ignore-scripts --audit=false && npm run build

dev-frontend:
	cd frontend && npm run dev

lint-frontend:
	cd frontend && npm ci --ignore-scripts --audit=false && npm run lint

test-frontend:
	cd frontend && npm ci --ignore-scripts --audit=false && npm run test:run

security: security-frontend security-go

security-frontend:
	cd frontend && npm ci --ignore-scripts --audit=false && npm audit signatures && node ../scripts/npm-audit.mjs && node ../scripts/check-licenses.mjs package-lock.json

security-go:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 -test . ./internal/... ./internal/telemetry/testdata/version ./cmd/...
	cd benchmarks && go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 -test ./...

fmt-go:
	gofmt -w $$(git ls-files '*.go')

check-fmt:
	@files="$$(git ls-files '*.go')"; \
	unformatted="$$(gofmt -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "$$unformatted"; \
		exit 1; \
	fi

test-go: build-frontend check-go-packages
	go vet . ./internal/... ./internal/telemetry/testdata/version ./cmd/...
	go test -timeout=30m . ./internal/... ./internal/telemetry/testdata/version ./cmd/...

# Proves unit tests are hermetic: runs them in a network namespace with only
# loopback (Linux, unprivileged user namespaces).
test-hermetic: build-frontend check-go-packages
	go test -timeout=30m -count=1 -exec $(CURDIR)/scripts/run-hermetic-test.sh . ./internal/... ./internal/telemetry/testdata/version ./cmd/...
	cd benchmarks && go test -timeout=30m -count=1 -exec $(CURDIR)/scripts/run-hermetic-test.sh ./...

test-go-race: build-frontend check-go-packages
	go test -timeout=30m -race . ./internal/... ./internal/telemetry/testdata/version ./cmd/...

test-benchmarks:
	cd benchmarks && go vet ./... && go test ./...

test-public-check:
	scripts/check-public-test.sh

test-export:
	scripts/check-public-test.sh
	scripts/export-public-test.sh

.PHONY: verify-python
verify-python:
	uv sync --locked
	uv run --locked ruff format --check .
	uv run --locked ruff check .
	uv run --locked mypy
	uv run --locked coverage run -m unittest discover -s tests/python -v
	uv run --locked coverage report
	uv run --locked coverage json -o coverage-python.json

.PHONY: verify-shell
verify-shell:
	bash scripts/check-shell.sh

.PHONY: lint-go test-go-coverage check-go-packages
check-go-packages:
	bash scripts/check-go-packages.sh

lint-go: build-frontend check-go-packages
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run . ./internal/... ./internal/telemetry/testdata/version ./cmd/...
	cd benchmarks && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --config ../.golangci.yml ./...

test-go-coverage: build-frontend check-go-packages
	bash scripts/test-go-coverage.sh

.PHONY: verify-tools
verify-tools:
	npm --prefix scripts ci --ignore-scripts --audit=false
	npm --prefix scripts run verify

.PHONY: test-api-clients
test-api-clients: build-frontend
	@set -eu; fixture_dir="$$(mktemp -d)"; \
	trap 'rm -rf "$$fixture_dir"' EXIT; \
	frontend/node_modules/.bin/esbuild testdata/api-client-contract.ts --bundle --platform=node --format=esm --outfile="$$fixture_dir/client.mjs" && \
	SVART_REVIEW_CLIENT_SCRIPT="$$fixture_dir/client.mjs" go test -race . -run '^TestIndependentGeneratedClientsHTTPAndDNS$$' -count=1

.PHONY: verify-frontend verify-public
verify-frontend: build-frontend
	cd frontend && npm run verify

verify-public:
	bash scripts/check-public.sh

verify: test-container-security check-api check-fmt verify-frontend lint-go test-go test-go-race test-go-coverage test-benchmarks test-export verify-public verify-python verify-shell verify-tools test-api-clients security

# Go binary (depends on frontend build)
build: build-frontend
	go build -o svart-dns

run: build
	DB_PATH=$(CURDIR)/svart-dns.db ARCHIVE_PATH=$(CURDIR)/archives DNS_PORT=53 sudo -E ./svart-dns

run-tls: build
	DB_PATH=$(CURDIR)/svart-dns.db ARCHIVE_PATH=$(CURDIR)/archives DNS_PORT=53 ADMIN_PORT=443 \
	TLS_CERT=$(CURDIR)/certs/fullchain.pem TLS_KEY=$(CURDIR)/certs/privkey.pem \
	sudo -E ./svart-dns

clean:
	rm -f svart-dns
	rm -rf frontend/dist

# Docker
docker:
	docker build -t svart-dns .

docker-run:
	docker compose up -d --build

docker-down:
	docker compose down

# The generator is checked in and uses the repository Go dependency versions.
# Go embeds frontend/dist, so fresh checkouts need the real frontend build.
docs: build-frontend
	go test . -run '^TestGeneratedAPIContract$$' -update-api

check-api: build-frontend
	go test . -run '^Test(APIContract|GeneratedAPIContract|PublicAPIDocumentation)'

# Benchmarks (see benchmarks/README.md). Lists are downloaded once; runs are offline.
BENCH_LISTS ?= benchmarks/lists

bench-lists:
	./benchmarks/fetch-lists.sh $(BENCH_LISTS)

# Head-to-head vs Pi-hole, AdGuard Home and Technitium (requires Docker).
bench-compare: build bench-lists
	SVART_BIN=$(CURDIR)/svart-dns ./benchmarks/compare.sh $(BENCH_LISTS)

bench-footprint: build bench-lists
	./benchmarks/footprint.sh ./svart-dns $(BENCH_LISTS)

bench-throughput: build bench-lists
	./benchmarks/throughput.sh ./svart-dns $(BENCH_LISTS)

# Build dependencies with network access, then execute the same isolated gate locally/CI.
.PHONY: e2e-build e2e
e2e-build:
	bash scripts/e2e/run.sh build

e2e: e2e-build
	bash scripts/e2e/run.sh

.PHONY: test-container-security
test-container-security:
	bash scripts/container-security-test.sh
