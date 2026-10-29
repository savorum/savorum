#!/bin/sh
set -eu

tag="${RELEASE_TAG:?container publication requires a release tag}"
image="${IMAGE:?container registry image is required}"
revision="${REVISION:?release revision is required}"
version="${tag#v}"
# An empty TARGET publishes the default, minimal image. VARIANT names any other
# target in its tags: v1.2.3-documents, 1.2.3-documents, 1.2-documents, and
# documents in place of latest.
target="${TARGET:-}"
variant="${VARIANT:-}"
suffix="${variant:+-$variant}"

for name in "$tag$suffix" "$version$suffix"; do
  if docker manifest inspect "$image:$name" >/dev/null 2>&1; then
    echo "immutable image tag $image:$name already exists" >&2
    exit 1
  fi
done

set -- \
  --tag "$image:$tag$suffix" \
  --tag "$image:$version$suffix"

case "$version" in
  *-*) ;;
  *)
    minor="${version%.*}"
    set -- "$@" --tag "$image:$minor$suffix" --tag "$image:${variant:-latest}"
    ;;
esac

if [ -n "$target" ]; then
  set -- "$@" --target "$target"
fi

# METADATA_FILE receives the build metadata, including the pushed digest.
if [ -n "${METADATA_FILE:-}" ]; then
  set -- "$@" --metadata-file "$METADATA_FILE"
fi

if [ -n "${WEB_BUILD_CONTEXT:-}" ]; then
  if ! printf '%s\n' "${WEB_BUNDLE_SHA256:-}" | grep -Eq '^[0-9a-f]{64}$'; then
    echo "a verified web bundle digest is required with WEB_BUILD_CONTEXT" >&2
    exit 1
  fi
  test -d "$WEB_BUILD_CONTEXT/src/internal/webui/dist"
  set -- "$@" --build-context "web=$WEB_BUILD_CONTEXT" \
    --build-arg "WEB_BUNDLE_SHA256=$WEB_BUNDLE_SHA256"
fi

docker buildx build \
  --pull \
  --platform linux/amd64,linux/arm64 \
  --sbom=true \
  --provenance=mode=max \
  --build-arg "VERSION=$version" \
  --build-arg "REVISION=$revision" \
  "$@" \
  --push .
