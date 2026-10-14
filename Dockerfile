# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.27.2
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

# The documents target adds Poppler and Tesseract for PDF and photo import:
#   docker build --target documents -t savorum:documents .
# Savorum Cloud runs this target. The default target below stays minimal.
FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 AS documents
ARG VERSION=development
ARG REVISION=
ARG DEBIAN_SNAPSHOT=20261003T000000Z
RUN printf 'Types: deb\nURIs: http://snapshot.debian.org/archive/debian/%s/\nSuites: bookworm bookworm-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\nCheck-Valid-Until: no\n\nTypes: deb\nURIs: http://snapshot.debian.org/archive/debian-security/%s/\nSuites: bookworm-security\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\nCheck-Valid-Until: no\n' "$DEBIAN_SNAPSHOT" "$DEBIAN_SNAPSHOT" > /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=0 -o Acquire::http::Timeout=30 update \
    && apt-get install -y --no-install-recommends \
       dash=0.5.12-2 \
       poppler-utils=22.12.0-2+deb12u3 \
       tesseract-ocr=5.3.0-2 \
       tesseract-ocr-eng=1:4.1.0-2 \
       fonts-dejavu-core=2.37-6 \
       ca-certificates=20250419~deb12u1 \
    && rm -rf /var/lib/apt/lists/*

LABEL org.opencontainers.image.title="Savorum" \
      org.opencontainers.image.description="Self-hosted recipe manager with PDF and photo import" \
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
    SAVORUM_LISTEN=0.0.0.0:8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/savorum", "healthcheck"]
ENTRYPOINT ["/savorum"]
CMD ["serve"]

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG VERSION=development
ARG REVISION=

LABEL org.opencontainers.image.title="Savorum" \
      org.opencontainers.image.description="Self-hosted recipe manager" \
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
    SAVORUM_LISTEN=0.0.0.0:8080
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/savorum", "healthcheck"]
ENTRYPOINT ["/savorum"]
CMD ["serve"]
