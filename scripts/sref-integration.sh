#!/usr/bin/env bash
# Run every SREF integration check against the revisions this repository
# declares, without moving or otherwise changing the contributor's sibling
# working trees.
set -euo pipefail

repository_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repository_root"

sref_source="${1:-../sref}"
reader_source="${2:-../sref-reader}"
writer_source="${3:-../sref-writer}"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/savorum-sref-integration.XXXXXX")"
trap 'rm -rf -- "$scratch"' EXIT

stage_declared_checkout() {
    local key="$1"
    local source="$2"
    local destination="$3"
    local commit

    if [ ! -d "$source/.git" ]; then
        echo "no sibling Git checkout at $source" >&2
        return 1
    fi
    commit="$(scripts/declared-sref-commit.sh "$key")"
    if ! git -C "$source" cat-file -e "$commit^{commit}" 2>/dev/null; then
        echo "$source does not contain declared $key revision $commit; fetch it before checking" >&2
        return 1
    fi
    git clone --quiet --no-checkout "$source" "$destination"
    git -C "$destination" checkout --quiet --detach "$commit"
    echo "$key: $commit"
}

stage_declared_checkout sref "$sref_source" "$scratch/sref"
stage_declared_checkout sref_reader "$reader_source" "$scratch/sref-reader"
stage_declared_checkout sref_writer "$writer_source" "$scratch/sref-writer"

if ! python3 -c 'import jsonschema' 2>/dev/null; then
    echo "Python's jsonschema package is required for SREF integration; activate a development virtual environment first" >&2
    exit 1
fi

SREF_CHECKOUT="$scratch/sref" \
SREF_READER_CHECKOUT="$scratch/sref-reader" \
SREF_WRITER_CHECKOUT="$scratch/sref-writer" \
SREF_REQUIRED=1 \
SREF_REGISTRY_PATH="$scratch/sref/registry/units.json" \
SREF_REGISTRY_REQUIRED=1 \
    go test -count=1 ./internal/sref/... ./internal/units/ ./internal/ingredientparser/ ./internal/interop/...

mkdir -p bin
# Removed first, so a failed build cannot leave a stale adapter to be tested.
rm -f bin/srefadapter
go build -o bin/srefadapter ./cmd/srefadapter
python3 "$scratch/sref/conformance/runner.py" \
    --adapter "$repository_root/bin/srefadapter"
