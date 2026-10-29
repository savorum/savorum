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
regctl="${REGCTL:-regctl}"

for name in "$tag$suffix" "$version$suffix"; do
  exists=false
  if [ -n "${OCI_ARCHIVE:-}" ]; then
    if "$regctl" manifest head "$image:$name" >/dev/null 2>&1; then exists=true; fi
  else
    if docker manifest inspect "$image:$name" >/dev/null 2>&1; then exists=true; fi
  fi
  if "$exists"; then
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

if [ -n "${OCI_ARCHIVE:-}" ]; then
  digest="${IMAGE_DIGEST:?prepared image digest is required}"
  if ! printf '%s\n' "$digest" | grep -Eq '^sha256:[0-9a-f]{64}$'; then
    echo "invalid prepared image digest" >&2
    exit 1
  fi
  archived_digest="$(tar -xOf "$OCI_ARCHIVE" index.json | jq -er \
    'select(.manifests | length == 1) | .manifests[0].digest')"
  if [ "$archived_digest" != "$digest" ]; then
    echo "prepared image digest does not match its archive" >&2
    exit 1
  fi
  "$regctl" image import "$image@$digest" "$OCI_ARCHIVE"
  test "$("$regctl" manifest head "$image@$digest" --require-list)" = "$digest"
  while [ "$#" -gt 0 ]; do
    "$regctl" image copy "$image@$digest" "$2"
    shift 2
  done
  if [ -n "${METADATA_FILE:-}" ]; then
    jq -n --arg digest "$digest" '{"containerimage.digest": $digest}' > "$METADATA_FILE"
  fi
  exit 0
fi

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
