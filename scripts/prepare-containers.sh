#!/usr/bin/env bash
set -euo pipefail

output="${1:?usage: prepare-containers.sh OUTPUT_DIRECTORY}"
tag="${RELEASE_TAG:?release tag is required}"
revision="${REVISION:?release revision is required}"
context="${WEB_BUILD_CONTEXT:?verified web build context is required}"
web_sha256="${WEB_BUNDLE_SHA256:?verified web bundle digest is required}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]
[[ "$web_sha256" =~ ^[0-9a-f]{64}$ ]]
test -f "$context/src/internal/webui/dist/index.html"
mkdir -p "$output"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

build=(docker buildx build --pull
  --build-context "web=$context"
  --build-arg "WEB_BUNDLE_SHA256=$web_sha256"
  --build-arg "VERSION=${tag#v}"
  --build-arg "REVISION=$revision")

for variant in minimal documents; do
  image="savorum:release-smoke-$variant-$revision"
  "${build[@]}" --load --target "$variant" --tag "$image" .
  ./scripts/container-smoke-test.sh "$image"
  if [[ "$variant" == documents ]]; then
    docker run --rm --entrypoint sh "$image" -c \
      'for tool in pdfinfo pdftotext pdftoppm tesseract; do command -v "$tool" || exit 1; done'
    docker run --rm --entrypoint tesseract "$image" --list-langs | grep -qx eng
  fi
done

for variant in minimal documents; do
  "${build[@]}" --target "$variant" --platform linux/amd64,linux/arm64 \
    --sbom=true --provenance=mode=max \
    --metadata-file "$work/$variant.json" \
    --output "type=oci,dest=$output/$variant.oci.tar" .
  digest="$(jq -er '."containerimage.digest" | select(test("^sha256:[0-9a-f]{64}$"))' "$work/$variant.json")"
  sha256="$(sha256sum "$output/$variant.oci.tar" | cut -d ' ' -f 1)"
  jq -n --arg digest "$digest" --arg sha256 "$sha256" \
    '{digest: $digest, sha256: $sha256}' > "$work/$variant-image.json"
done

jq -n --arg revision "$revision" --arg version "${tag#v}" --arg web_sha256 "$web_sha256" \
  --slurpfile minimal "$work/minimal-image.json" --slurpfile documents "$work/documents-image.json" \
  '{revision: $revision, version: $version, web_sha256: $web_sha256,
    images: {minimal: $minimal[0], documents: $documents[0]}}' > "$output/images.json"
