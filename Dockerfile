# syntax=docker/dockerfile:1

# ---- frontend ----
FROM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- backend ----
FROM golang:1.27-alpine AS backend
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

# ---- terraform/tofu binaries, checksum-verified ----
# Pinned, not auto-installed at runtime: a smaller, more auditable supply
# chain than a dynamic installer. Bump these deliberately, not silently.
FROM alpine:3.22 AS tools
RUN apk add --no-cache curl unzip
WORKDIR /tools

ARG TERRAFORM_VERSION=1.16.4
ARG TOFU_VERSION=1.13.0
ARG TARGETARCH

RUN set -eux; \
    tf_zip="terraform_${TERRAFORM_VERSION}_linux_${TARGETARCH}.zip"; \
    curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o "$tf_zip" "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/${tf_zip}"; \
    curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o terraform_SHA256SUMS "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_SHA256SUMS"; \
    test -s "$tf_zip"; \
    grep "$tf_zip" terraform_SHA256SUMS | sha256sum -c -; \
    unzip "$tf_zip" -d /tools; \
    rm "$tf_zip" terraform_SHA256SUMS

RUN set -eux; \
    tofu_zip="tofu_${TOFU_VERSION}_linux_${TARGETARCH}.zip"; \
    curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o "$tofu_zip" "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/${tofu_zip}"; \
    curl -fsSL --retry 5 --retry-connrefused --retry-all-errors -o tofu_SHA256SUMS "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_SHA256SUMS"; \
    test -s "$tofu_zip"; \
    grep "$tofu_zip" tofu_SHA256SUMS | sha256sum -c -; \
    unzip "$tofu_zip" -d /tools; \
    rm "$tofu_zip" tofu_SHA256SUMS

# ---- final image ----
FROM alpine:3.22
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
