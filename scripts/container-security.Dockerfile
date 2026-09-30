# syntax=docker/dockerfile:1

ARG TARGET_IMAGE=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG SCANNER_BASE_IMAGE=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a

FROM ${TARGET_IMAGE} AS target

# Trivy is deliberately installed from a checksum-pinned release archive
# instead of a floating action or image. This stage has no Docker socket,
# source-control token, registry credential, or deployment secret.
FROM ${SCANNER_BASE_IMAGE} AS scanner
ARG TRIVY_VERSION=0.72.0
ARG TRIVY_SHA256=bbb64b9695866ce4a7a8f5c9592002c5961cab378577fa3f8a040df362b9b2ea

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*
RUN wget -qO /tmp/trivy.tar.gz \
      "https://github.com/aquasecurity/trivy/releases/download/v${TRIVY_VERSION}/trivy_${TRIVY_VERSION}_Linux-64bit.tar.gz" && \
    printf '%s  %s\n' "${TRIVY_SHA256}" /tmp/trivy.tar.gz | sha256sum -c - && \
    tar -xzf /tmp/trivy.tar.gz -C /usr/local/bin trivy && \
    rm -f /tmp/trivy.tar.gz && \
    trivy --version

COPY --from=target / /scanroot

# Fail closed on every HIGH/CRITICAL finding, including findings for which an
# upstream vendor has not published a fix. The JSON report retains all levels.
RUN mkdir -p /out && \
    trivy rootfs --scanners vuln --severity HIGH,CRITICAL \
      --exit-code 1 --no-progress /scanroot && \
    trivy rootfs --scanners vuln --format json --output /out/vulnerabilities.json \
      --no-progress /scanroot && \
    trivy rootfs --scanners vuln --format cyclonedx --output /out/sbom.cdx.json \
      --no-progress /scanroot

FROM scratch AS sbom
COPY --from=scanner /out/ /
