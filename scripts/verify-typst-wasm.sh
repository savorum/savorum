#!/usr/bin/env bash
# Confirms that the embedded Typst compiler is byte-identical to the file in the
# upstream Go module release it was taken from. The module proxy serves the
# release immutably and the checksum database verifies it.
set -euo pipefail

version="v0.4.0"
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
embedded="$repository_root/internal/render/typst/compiler/typst_compiler.wasm"

upstream="$(cd "$(mktemp -d)" && go mod download -json "github.com/dseif0x/typst-go-wasm@$version" | sed -n 's/.*"Dir": "\(.*\)",/\1/p')"

cmp "$embedded" "$upstream/typst_compiler.wasm"
echo "typst_compiler.wasm matches typst-go-wasm $version"
