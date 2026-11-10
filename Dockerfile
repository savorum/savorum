# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.27.3
ARG NODE_VERSION=26.13.0

# Release builds supply the verified frontend through the web build context.
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-bookworm@sha256:a546d3f8acd2af4382668a7e6acd3bc4418e6a27478373e3246f8162f5181509 AS web
WORKDIR /src
COPY scripts/corepack/package.json scripts/corepack/package-lock.json /opt/corepack/
ENV PATH="/opt/corepack/node_modules/.bin:${PATH}"
RUN npm ci --prefix /opt/corepack --ignore-scripts --no-fund --no-audit \
    && /opt/corepack/node_modules/.bin/corepack enable --install-directory /usr/local/bin
COPY web/package.json web/
COPY scripts/bootstrap-pnpm.sh /opt/bootstrap-pnpm.sh
RUN sh /opt/bootstrap-pnpm.sh
COPY api/openapi.json api/
COPY internal/recipetext/vocabulary.json internal/recipetext/vocabulary.json
COPY web/ web/
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm --dir web install --frozen-lockfile && pnpm --dir web run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm@sha256:dc0963f32053e4bc451d24d87e3892d42d01021b61f093e98eb95c0352bb84b0 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=development
ARG REVISION=

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -tags nodynamic \
    -ldflags="-s -w -X github.com/savorum/savorum/internal/buildinfo.Version=${VERSION} -X github.com/savorum/savorum/internal/buildinfo.Revision=${REVISION}" \
    -o /out/savorum ./cmd/savorum
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go run -trimpath -tags nodynamic ./cmd/savorum admin compatibility > /out/savorum-compatibility.json
RUN mkdir -p /out/data /out/tmp && chmod 0750 /out/data && chmod 1777 /out/tmp

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS runtime
ARG VERSION=development
ARG REVISION=
ARG WEB_BUNDLE_SHA256=
RUN apk add --no-cache \
       bash=5.3.9-r1 \
       ca-certificates-bundle=20261020-r0 \
       setpriv=2.42.3-r1 \
       zlib=1.4-r0 \
    && addgroup -g 65532 savorum \
    && adduser -D -H -h /data -s /bin/bash -u 65532 -G savorum savorum \
    && ln -s /savorum /usr/local/bin/savorum

LABEL org.opencontainers.image.title="Savorum" \
      org.opencontainers.image.source="https://github.com/savorum/savorum" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      io.savorum.web-bundle-sha256=$WEB_BUNDLE_SHA256

COPY --from=build /out/savorum /savorum
COPY --from=build /out/savorum-compatibility.json /savorum-compatibility.json
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build /out/tmp /tmp

USER 65532:65532
VOLUME ["/data"]
EXPOSE 8080
ENV SAVORUM_DATABASE=/data/savorum.db \
    SAVORUM_ASSETS=/data/assets \
    SAVORUM_LISTEN=0.0.0.0:8080 \
    TMPDIR=/data
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/savorum", "healthcheck"]
ENTRYPOINT ["/savorum"]
CMD ["serve"]

# The documents target adds Poppler and Tesseract for PDF and photo import:
#   docker build --target documents -t savorum:documents .
FROM runtime AS documents
USER 0:0
RUN apk add --no-cache \
       poppler-utils=25.12.0-r1 \
       tesseract-ocr=5.5.2-r0 \
       tesseract-ocr-data-eng=5.5.2-r0 \
       font-dejavu=2.37-r6

LABEL org.opencontainers.image.description="Self-hosted recipe manager with PDF and photo import"
USER 65532:65532

FROM runtime AS minimal
LABEL org.opencontainers.image.description="Self-hosted recipe manager"
