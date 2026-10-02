# syntax=docker/dockerfile:1

# Every base image is pinned by digest as well as tag. The tag says which
# release line we follow; the digest makes the build reproducible and means a
# moved or tampered tag can't change what ships. Dependabot (docker ecosystem)
# proposes bumps of tag and digest together.

# ---- frontend ----
FROM node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
# --ignore-scripts: no dependency's install script runs in the build. None of
# the dependencies needs one on Linux (the only one that declares any is
# fsevents, macOS-only), so this costs nothing.
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./internal/webassets/dist
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/carlosalbertorg/groundtruth/internal/buildinfo.Version=${VERSION} \
      -X github.com/carlosalbertorg/groundtruth/internal/buildinfo.Commit=${COMMIT} \
      -X github.com/carlosalbertorg/groundtruth/internal/buildinfo.Date=${DATE}" \
    -o /out/groundtruth ./cmd/groundtruth

# ---- terraform/tofu binaries, signature- and checksum-verified ----
# Pinned, not auto-installed at runtime: a smaller, more auditable supply
# chain than a dynamic installer. Bump these deliberately, not silently.
#
# A checksum fetched from the same place as the archive only proves the
# download wasn't corrupted. What proves it came from the publisher is the
# signature on that checksum file, so each is verified before it is trusted:
#   - Terraform: HashiCorp signs terraform_<version>_SHA256SUMS with its PGP
#     key. The key is downloaded, but a signature only counts if it was made by
#     the key with the fingerprint below (the one HashiCorp publishes at
#     https://www.hashicorp.com/trust/security).
#   - OpenTofu: signs tofu_<version>_SHA256SUMS keylessly with cosign, from its
#     release workflow (https://opentofu.org/docs/intro/install/standalone/).
FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8 AS tools
RUN apk add --no-cache curl unzip gnupg cosign
WORKDIR /tools

ARG TERRAFORM_VERSION=1.16.4
ARG TOFU_VERSION=1.13.0
ARG TARGETARCH
ARG HASHICORP_PGP_FINGERPRINT=C874011F0AB405110D02105534365D9472D7468F

RUN set -eux; \
    fetch() { curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o "$2" "$1"; }; \
    base="https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}"; \
    tf_zip="terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip"; \
    sums="terraform_${TERRAFORM_VERSION}_SHA256SUMS"; \
    fetch "$base/$tf_zip" "$tf_zip"; \
    fetch "$base/$sums" "$sums"; \
    fetch "$base/$sums.sig" "$sums.sig"; \
    fetch https://www.hashicorp.com/.well-known/pgp-key.txt hashicorp.asc; \
    export GNUPGHOME="$(mktemp -d)"; \
    gpg --batch --import hashicorp.asc; \
    status="$(gpg --batch --status-fd 1 --verify "$sums.sig" "$sums")"; \
    echo "$status"; \
    echo "$status" | grep -Eq "^\[GNUPG:\] VALIDSIG [0-9A-F]+ .* ${HASHICORP_PGP_FINGERPRINT}\$"; \
    test -s "$tf_zip"; \
    grep " $tf_zip\$" "$sums" | sha256sum -c -; \
    unzip "$tf_zip" -d /tools; \
    rm "$tf_zip" "$sums" "$sums.sig" hashicorp.asc

RUN set -eux; \
    fetch() { curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o "$2" "$1"; }; \
    base="https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}"; \
    tofu_zip="tofu_${TOFU_VERSION}_linux_${TARGETARCH}.zip"; \
    sums="tofu_${TOFU_VERSION}_SHA256SUMS"; \
    fetch "$base/$tofu_zip" "$tofu_zip"; \
    fetch "$base/$sums" "$sums"; \
    fetch "$base/$sums.sig" "$sums.sig"; \
    fetch "$base/$sums.pem" "$sums.pem"; \
    cosign verify-blob "$sums" \
      --signature "$sums.sig" \
      --certificate "$sums.pem" \
      --certificate-identity "https://github.com/opentofu/opentofu/.github/workflows/release.yml@refs/heads/v${TOFU_VERSION%.*}" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com; \
    test -s "$tofu_zip"; \
    grep " $tofu_zip\$" "$sums" | sha256sum -c -; \
    unzip "$tofu_zip" -d /tools; \
    rm "$tofu_zip" "$sums" "$sums.sig" "$sums.pem"

# ---- final image ----
FROM alpine:3.22@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -u 10000 groundtruth && \
    mkdir -p /data && chown groundtruth:groundtruth /data
COPY --from=tools /tools/terraform /usr/local/bin/terraform
COPY --from=tools /tools/tofu /usr/local/bin/tofu
COPY --from=backend /out/groundtruth /usr/local/bin/groundtruth

USER groundtruth
ENV GROUNDTRUTH_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s CMD ["wget", "-q", "-O-", "http://127.0.0.1:8080/healthz"]

ENTRYPOINT ["groundtruth"]
