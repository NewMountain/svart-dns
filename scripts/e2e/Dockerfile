# Dependencies are fetched only while building; the test container has no network.
ARG GO_IMAGE=golang:1.26-trixie@sha256:bdca99a00bc16590cb1a0bb4e698f5fc5d6a64e4d5eef13d9f18a0ee08e5fa65
ARG NODE_IMAGE=node:24-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe
FROM ${GO_IMAGE} AS go
FROM ${NODE_IMAGE}
ENV GOMAXPROCS=4 GOFLAGS=-p=4 CGO_ENABLED=1 GOTOOLCHAIN=local
COPY --from=go /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
RUN apt-get update && apt-get install -y --no-install-recommends gcc g++ libc6-dev chromium openssl sqlite3 dnsutils ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY frontend/package.json frontend/package-lock.json frontend/
COPY scripts/package.json scripts/package-lock.json scripts/
RUN npm --prefix frontend ci --ignore-scripts --audit=false && npm --prefix scripts ci --ignore-scripts --audit=false
COPY . .
ARG SOURCE_REVISION=development
ENV SVART_E2E_REVISION=${SOURCE_REVISION}
LABEL org.opencontainers.image.revision=${SOURCE_REVISION}
RUN mkdir /evidence && chmod 1777 /evidence
RUN --mount=type=cache,target=/root/.cache/go-build npm --prefix frontend run build && go build -o /usr/local/bin/svart-dns ./cmd/svart-dns
RUN frontend/node_modules/.bin/esbuild scripts/e2e/main.ts --bundle --platform=node --format=esm --packages=external --outfile=scripts/e2e/main.mjs
ENV SVART_E2E_BINARY=/usr/local/bin/svart-dns SVART_E2E_CHROMIUM=/usr/bin/chromium SVART_E2E_OUTPUT=/evidence
CMD ["node", "scripts/e2e/main.mjs"]
