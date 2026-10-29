#!/usr/bin/env bash
set -euo pipefail
# The binary and browser suite consume the same versioned contract.
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
output="output/scale"
if [[ $# -gt 0 && "$1" != --* ]]; then output="$1"; shift; fi
browse_only=false
requested_size=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --browse-only) browse_only=true; shift ;;
    --recipes) requested_size="${2:?--recipes requires a count}"; shift 2 ;;
    *) echo "Unknown scale-check option: $1" >&2; exit 2 ;;
  esac
done
mkdir -p "$output"
output="$(cd "$output" && pwd)"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/savorum-scale.XXXXXX")"
trap 'rm -rf "$fixture_root"' EXIT
sizes="$(node -p "JSON.parse(require('fs').readFileSync('internal/scalefixture/contract.json','utf8')).sizes.join(' ')")"
if [[ -n "$requested_size" ]]; then
  if [[ " $sizes " != *" $requested_size "* ]]; then
    echo "Recipe count must be one of: $sizes" >&2
    exit 2
  fi
  sizes="$requested_size"
fi
for count in $sizes; do
  fixture="$fixture_root/$count"
  status=0
  bin/scalecheck --recipes "$count" --directory "$fixture" --browse-only="$browse_only" > "$output/$count.log" || status=$?
  if [[ -f "$fixture/report.json" ]]; then cp "$fixture/report.json" "$output/$count.json"; fi
  if [[ "$status" -ne 0 ]]; then exit "$status"; fi
  database="$fixture/restored.db"
  assets="$fixture/restored-assets"
  if $browse_only; then database="$fixture/library.db"; assets="$fixture/assets"; fi
  SAVORUM_E2E_DATABASE="$database" SAVORUM_E2E_ASSETS="$assets" \
    SAVORUM_SCALE_REPORT="$output/$count.json" pnpm --dir web exec playwright test performance.spec.mjs
done
