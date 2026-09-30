# Build tooling is separate from the minimal production runtime.
FROM golang:1.26-trixie@sha256:bdca99a00bc16590cb1a0bb4e698f5fc5d6a64e4d5eef13d9f18a0ee08e5fa65 AS go
FROM node:24-trixie-slim@sha256:8ec5d7557396cfe32d21c3f9c13072355ceab22b584578ca4bb28af31120cffe
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOMAXPROCS=4 GOFLAGS=-p=4 GOTOOLCHAIN=local
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl gcc g++ git libc6-dev make python3 python3-pip python3-venv xz-utils && \
    rm -rf /var/lib/apt/lists/*
RUN npm install --global --ignore-scripts --audit=false npm@12.0.2 && \
    python3 -m pip install --break-system-packages --no-cache-dir uv==0.12.18
# SHA-256 is published in the official GitHub release asset metadata.
RUN curl --fail --location --retry 3 --output /tmp/shellcheck.tar.xz \
      https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz && \
    echo '8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198  /tmp/shellcheck.tar.xz' | sha256sum --check && \
    tar -xJf /tmp/shellcheck.tar.xz --directory /tmp && \
    install /tmp/shellcheck-v0.11.0/shellcheck /usr/local/bin/shellcheck && \
    rm -rf /tmp/shellcheck.tar.xz /tmp/shellcheck-v0.11.0
RUN GOBIN=/usr/local/bin go install mvdan.cc/sh/v3/cmd/shfmt@v3.14.1 && \
    GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 && \
    GOBIN=/usr/local/bin go install golang.org/x/vuln/cmd/govulncheck@v1.6.0 && \
    GOBIN=/usr/local/bin go install github.com/zricethezav/gitleaks/v8@v8.30.1
# CI copies an exact clean checkout owned by the invoking runner into this path.
# Trust only that explicit workspace; disposable export repositories keep normal checks.
RUN git config --global --add safe.directory /workspace && mkdir -p /evidence/tmp
ENV TMPDIR=/evidence/tmp GOTMPDIR=/evidence/tmp
WORKDIR /workspace
CMD ["make", "verify"]
