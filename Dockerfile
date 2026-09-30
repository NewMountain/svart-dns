ARG NODE_IMAGE=node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1
ARG GO_IMAGE=golang:1.26-trixie@sha256:bdca99a00bc16590cb1a0bb4e698f5fc5d6a64e4d5eef13d9f18a0ee08e5fa65
ARG RUNTIME_IMAGE=gcr.io/distroless/base-nossl-debian13:nonroot@sha256:8c563c1fb5e120606f0d85733049775faed6192e2bd2223ef283a5393eec22b9
ARG FORMATTER_IMAGE=node:24-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe

# The Go contract tests run the pinned frontend formatter. Use a glibc Node
# executable for the Debian builder; the Alpine frontend executable uses musl.
FROM ${FORMATTER_IMAGE} AS formatter

# Stage 1: Build frontend
FROM ${NODE_IMAGE} AS frontend

WORKDIR /app/frontend
RUN npm install --global --ignore-scripts --audit=false npm@12.0.2 && \
    test "$(npm --version)" = "12.0.2"
COPY frontend/package*.json ./
RUN npm ci --ignore-scripts --audit=false
RUN npm audit signatures
COPY frontend/ .
COPY docs/swagger.json /app/docs/swagger.json
COPY scripts/npm-audit.mjs /usr/local/lib/npm-audit.mjs
COPY scripts/check-licenses.mjs /usr/local/lib/check-licenses.mjs
RUN node /usr/local/lib/npm-audit.mjs
RUN node /usr/local/lib/check-licenses.mjs package-lock.json
RUN npm run lint
RUN npm run test:run
RUN npm run build

# Stage 2: Build Go binary
FROM ${GO_IMAGE} AS builder

# Bound compilation and test concurrency on shared build hosts.
ENV GOMAXPROCS=4 GOFLAGS=-p=4

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends gcc g++ libc6-dev && \
    mkdir -p /runtime-libs/var/lib/dpkg/status.d /tmp/runtime-debs && \
    cd /tmp/runtime-debs && \
    apt-get download gcc-14-base libgcc-s1 libstdc++6 && \
    for deb in *.deb; do \
      package_name="$(dpkg-deb --field "$deb" Package)"; \
      dpkg --ctrl-tarfile "$deb" | tar -Oxf - ./control > "/runtime-libs/var/lib/dpkg/status.d/${package_name}"; \
      dpkg --extract "$deb" /runtime-libs; \
    done && \
    rm -rf /tmp/runtime-debs && \
    rm -rf /var/lib/apt/lists/*
RUN GOBIN=/usr/local/bin go install golang.org/x/vuln/cmd/govulncheck@v1.6.0

WORKDIR /src
# CI supplies its exact immutable checkout revision. Local builds remain development.
ARG SOURCE_REVISION=development
# Reader-first rollout: this independently qualified image keeps legacy writes
# until a later active build persists the typed-rule capability on the node.
ARG LIST_WRITER_ROLE=bridge
RUN case "$LIST_WRITER_ROLE" in bridge|active) ;; *) exit 1 ;; esac
RUN if [ "$SOURCE_REVISION" != development ]; then \
      test "${#SOURCE_REVISION}" -eq 40 && \
      case "$SOURCE_REVISION" in *[!0-9a-f]*) exit 1 ;; esac; \
    fi
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN scripts/check-extension.sh
COPY --from=frontend /app/frontend/dist /src/frontend/dist
COPY --from=frontend /app/frontend/node_modules/prettier /src/frontend/node_modules/prettier
COPY --from=formatter /usr/local/bin/node /usr/local/bin/node

RUN CGO_ENABLED=1 go vet ./...
RUN CGO_ENABLED=1 go test ./...
RUN CGO_ENABLED=1 govulncheck -test ./...
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w -X github.com/yeti/svart-dns/internal/telemetry.buildRevision=${SOURCE_REVISION} -X github.com/yeti/svart-dns/internal/svart.listWriterRole=${LIST_WRITER_ROLE}" -o /svart-dns ./cmd/svart-dns
# Go provides TLS itself. DuckDB requires the standard GCC/C++ DSOs; prove those
# are the only extra dynamic dependencies and reject OpenSSL or unresolved DSOs.
RUN runtime_links="$(ldd /svart-dns)" && \
    printf '%s\n' "$runtime_links" && \
    printf '%s\n' "$runtime_links" | grep -F 'libstdc++.so.6' && \
    printf '%s\n' "$runtime_links" | grep -F 'libgcc_s.so.1' && \
    ! printf '%s\n' "$runtime_links" | grep -Eq 'libssl|libcrypto|not found'
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /svart-healthcheck ./cmd/healthcheck
RUN mkdir -p /runtime-data /runtime-archives

# Stage 3: Minimal OpenSSL-free Debian 13 runtime. Preserve the existing
# production UID/GID while omitting shells, package managers, Node, npm, Go,
# wget, Perl, and OpenSSL.
FROM ${RUNTIME_IMAGE}

COPY --from=builder /runtime-libs/ /
COPY --from=builder --chown=999:999 /runtime-data/ /data/
COPY --from=builder --chown=999:999 /runtime-archives/ /archives/
COPY --from=builder /svart-dns /usr/local/bin/svart-dns
COPY --from=builder /svart-healthcheck /usr/local/bin/svart-healthcheck

EXPOSE 5353/udp 5353/tcp 3000/tcp

ENV DNS_PORT=5353
ENV ADMIN_PORT=3000
ENV DB_PATH=/data/svart-dns.db
ENV ARCHIVE_PATH=/archives

VOLUME ["/data", "/archives"]

USER 999:999

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD ["/usr/local/bin/svart-healthcheck"]

CMD ["svart-dns"]
