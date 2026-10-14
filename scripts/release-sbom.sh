#!/usr/bin/env bash
# Writes the SPDX SBOM attached to a GitHub release: the Go modules compiled
# into the savorum binary and the production packages bundled into its web UI.
# The container images carry their own SBOM attestations, which add the base
# image's packages.
#
#     release-sbom.sh 1.2.3 savorum-1.2.3.spdx.json
#
# Needs go, pnpm, jq and syft (or SYFT naming it).
set -euo pipefail

version="${1:?usage: release-sbom.sh VERSION OUTPUT}"
output="${2:?usage: release-sbom.sh VERSION OUTPUT}"
syft="${SYFT:-syft}"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The Go module list comes from the build info of a real build, so test-only
# modules in go.mod stay out. The web UI is not needed to read it.
CGO_ENABLED=0 go build -trimpath -tags nodynamic -o "$work/savorum" ./cmd/savorum
SYFT_CHECK_FOR_APP_UPDATE=false "$syft" scan "file:$work/savorum" \
    --source-name savorum --source-version "$version" \
    --output "spdx-json=$work/go.spdx.json" --quiet

# Syft reads every package in pnpm-lock.yaml, development ones included, so
# pnpm lists the web UI's production dependencies itself.
pnpm --dir web sbom --sbom-format spdx --lockfile-only --prod --sbom-type application \
    > "$work/web.spdx.json"

jq --slurp --arg version "$version" '
    .[0] as $go | .[1] as $web
    | ($go.relationships[] | select(.relationshipType == "DESCRIBES") | .relatedSpdxElement) as $root
    | ($web.relationships[] | select(.relationshipType == "DESCRIBES") | .relatedSpdxElement) as $webRoot
    | $go
    | .name = "savorum-\($version)"
    | .creationInfo.creators += $web.creationInfo.creators
    | .packages += [$web.packages[] | if .SPDXID == $webRoot then .versionInfo = $version else . end]
    | .relationships += [$web.relationships[] | select(.relationshipType != "DESCRIBES")]
    | .relationships += [{spdxElementId: $root, relatedSpdxElement: $webRoot, relationshipType: "CONTAINS"}]
' "$work/go.spdx.json" "$work/web.spdx.json" > "$output"
