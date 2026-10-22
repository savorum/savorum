# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.27.3
ARG NODE_VERSION=26.9.0

# The frontend is built here rather than committed. Its output is
# platform-independent, so it is built once on the build platform.
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-bookworm@sha256:347d9d5391493c2fc3f0d161b75a9cab64a9805064b1b01b099f8ee97f98ed47 AS web
WORKDIR /src
COPY scripts/corepack/package.json scripts/corepack/package-lock.json /opt/corepack/
ENV PATH="/opt/corepack/node_modules/.bin:${PATH}"
RUN npm ci --prefix /opt/corepack --ignore-scripts --no-fund --no-audit \
    && /opt/corepack/node_modules/.bin/corepack enable --install-directory /usr/local/bin
COPY web/package.json web/
RUN corepack install --global "$(node -p "require('./web/package.json').packageManager")"
COPY api/openapi.json api/
COPY internal/recipetext/vocabulary.json internal/recipetext/vocabulary.json
COPY web/ web/
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm --dir web install --frozen-lockfile && pnpm --dir web run build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=development
ARG REVISION=

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -tags nodynamic \
    -ldflags="-s -w -X github.com/savorum/savorum/internal/buildinfo.Version=${VERSION} -X github.com/savorum/savorum/internal/buildinfo.Revision=${REVISION}" \
    -o /out/savorum ./cmd/savorum
RUN CGO_ENABLED=0 go run -tags nodynamic ./cmd/savorum admin compatibility > /out/savorum-compatibility.json
RUN mkdir -p /out/data /out/tmp && chmod 0750 /out/data && chmod 1777 /out/tmp

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime
ARG VERSION=development
ARG REVISION=
RUN apk add --no-cache \
       bash=5.3.9-r1 \
       ca-certificates-bundle=20260909-r0 \
       setpriv=2.42.3-r1 \
       zlib=1.3.2-r1 \
    && addgroup -g 65532 savorum \
    && adduser -D -H -h /data -s /bin/bash -u 65532 -G savorum savorum \
    && ln -s /savorum /usr/local/bin/savorum

LABEL org.opencontainers.image.title="Savorum" \
      org.opencontainers.image.source="https://github.com/savorum/savorum" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION

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
