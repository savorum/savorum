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

docker buildx build \
  --pull \
  --platform linux/amd64,linux/arm64 \
  --sbom=true \
  --provenance=mode=max \
  --build-arg "VERSION=$version" \
  --build-arg "REVISION=$revision" \
  "$@" \
  --push .
