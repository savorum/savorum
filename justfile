# AVIF and JPEG XL decode in their bundled WebAssembly. Without this tag
# their packages load a host libavif or libjxl at startup instead.
export GOFLAGS := "-tags=nodynamic"

default:
    @just --list

web-build:
    pnpm --dir web run build

build: web-build
    mkdir -p bin
    go build -o bin/savorum ./cmd/savorum

fmt:
    gofumpt -w cmd internal

lint:
    golangci-lint run

lint-fix:
    golangci-lint run --fix

check-format:
    #!/usr/bin/env bash
    set -euo pipefail
    unformatted="$(gofumpt -l cmd internal)"
    if [[ -n "$unformatted" ]]; then
      echo "The following Go files require gofumpt:"
      echo "$unformatted"
      exit 1
    fi

generate:
    go run ./cmd/openapi
    go run ./cmd/units
    pnpm --dir web run generate

# Renders the print corpus for review; print the results at 100 percent.
print-fixtures output="tmp/print":
    go run ./cmd/printfixtures --output {{ output }}

openapi-check:
    go run ./cmd/openapi --check

# The frontend must convert through the catalog the server validates against.
units-check:
    go run ./cmd/units --check

test:
    go test ./...

web-test:
    pnpm --dir web test

fuzz-smoke:
    ./scripts/fuzz-security.sh smoke

fuzz-campaign:
    ./scripts/fuzz-security.sh campaign

# Drives the built binary in a real browser, against the real renderer.
e2e: build
    pnpm --dir web run test:e2e

# The embedded frontend is built first so its handler tests run against it.
test-race: web-build
    SAVORUM_REQUIRE_WEB_BUILD=1 ./scripts/test-race.sh

# Optional private regression suites; checkout requirements are in CONTRIBUTING.md.
corpus-slice corpus_root="../recipe-corpus":
    RECIPE_CORPUS_ROOT={{ quote(corpus_root) }} go test -count=1 -run 'TestPinnedRecipeCorpusSavorumSlice|TestThePinnedSliceStillCarriesItsRequiredConstructs|TestTheSliceCoverageDeclarationIsHonest' ./internal/exchange/

native-corpus corpus_root="../recipe-corpus":
    SAVORUM_RECIPE_CORPUS={{ quote(corpus_root) }} go test -count=1 -run TestNativeCorpus ./internal/importer/

# What a corpus repin changes, so moving the pin is a reviewed decision rather
# than a hash swap. The revisions are read out of Git, so neither checkout moves.
#
# `before`/`after` are corpus revisions and their target directories; the case
# list is the pinned slice, reported in full while the rest of the corpus is
# summarized. See docs/contribute/corpus-repin.md.
corpus-repin before after before_targets after_targets cases corpus_root="../recipe-corpus":
    go run ./cmd/corpusrepin --corpus "{{ corpus_root }}" \
      --before "{{ before }}" --before-targets "{{ before_targets }}" \
      --after "{{ after }}" --after-targets "{{ after_targets }}" --cases "{{ cases }}"

# Every cross-repository check, with a missing sibling checkout as a failure
# rather than a skip, as CI runs it.
sref-checks sref_root="../sref" reader="../sref-reader" writer="../sref-writer":
    SREF_CHECKOUT="{{ sref_root }}" SREF_READER_CHECKOUT="{{ reader }}" SREF_WRITER_CHECKOUT="{{ writer }}" \
      SREF_REQUIRED=1 go test -count=1 ./internal/sref/... ./internal/units/ ./internal/interop/...

# Reproduce blocking CI from immutable declared inputs without moving the
# sibling working trees. This is part of the canonical full check.
sref-integration sref_source="../sref" reader_source="../sref-reader" writer_source="../sref-writer":
    ./scripts/sref-integration.sh {{ sref_source }} {{ reader_source }} {{ writer_source }}

# Runs the SREF project's own conformance runner against Savorum, through the
# adapter protocol the specification defines. The sibling checkout must be at
# the revision integration/sref.json declares.
sref-conformance sref_root="../sref":
    #!/usr/bin/env bash
    set -euo pipefail
    declared="$(scripts/declared-sref-commit.sh)"
    actual="$(git -C {{ sref_root }} rev-parse HEAD)"
    if [[ "$declared" != "$actual" ]]; then
      echo "{{ sref_root }} is at $actual, and this repository is tested against $declared." >&2
      echo "Check it out, or run: just sref-pin {{ sref_root }}" >&2
      exit 1
    fi
    rm -f bin/srefadapter
    go build -o bin/srefadapter ./cmd/srefadapter
    python3 {{ sref_root }}/conformance/runner.py --adapter "$PWD/bin/srefadapter"

# What SREF revision this repository is tested against, and what the sibling
# checkout has. `--write` moves the pin; the full matrix then runs against it.
sref-pin sref_root="../sref" *flags:
    ./scripts/sref-pin.sh {{ sref_root }} {{ flags }}

# How far the pin is behind upstream, and in which parts: a nonblocking
# maintenance signal, also run by the scheduled CI job.
sref-drift sref_root="../sref":
    git -C {{ sref_root }} fetch origin main
    ./scripts/sref-drift.sh {{ sref_root }}

# Chains Savorum with an independent implementation and compares every hop
# against the original document.
sref-roundtrip sref_root="../sref" reader="../sref-reader" writer="../sref-writer":
    rm -f bin/srefadapter
    go build -o bin/srefadapter ./cmd/srefadapter
    python3 {{ sref_root }}/conformance/roundtrip_audit.py \
      --adapter "python3 {{ writer }}/tools/adapter.py --reader {{ reader }}" \
      --adapter "$PWD/bin/srefadapter"

check: check-format lint test-race web-test openapi-check units-check sref-integration build e2e

check-fast: web-test openapi-check units-check sref-integration build e2e

# The shared capacity/performance contract consumed by release load validation.
scale-check: build
    go build -o bin/scalecheck ./cmd/scalecheck
    ./scripts/scale-check.sh
