#!/usr/bin/env bash
set -euo pipefail
# The binary and browser suite consume the same versioned contract.
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
output="${1:-output/scale}"
mkdir -p "$output"
output="$(cd "$output" && pwd)"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/savorum-scale.XXXXXX")"
trap 'rm -rf "$fixture_root"' EXIT
sizes="$(node -p "JSON.parse(require('fs').readFileSync('internal/scalefixture/contract.json','utf8')).sizes.join(' ')")"
for count in $sizes; do
  fixture="$fixture_root/$count"
  status=0
  bin/scalecheck --recipes "$count" --directory "$fixture" > "$output/$count.log" || status=$?
  if [[ -f "$fixture/report.json" ]]; then cp "$fixture/report.json" "$output/$count.json"; fi
  if [[ "$status" -ne 0 ]]; then exit "$status"; fi
  SAVORUM_E2E_DATABASE="$fixture/restored.db" SAVORUM_E2E_ASSETS="$fixture/restored-assets" \
    SAVORUM_SCALE_REPORT="$output/$count.json" pnpm --dir web exec playwright test performance.spec.mjs
 done
