#!/usr/bin/env bash
set -euo pipefail

# Every test still runs. The real Typst/WASM rendering corpus runs normally:
# the Go race detector cannot instrument the embedded compiler and makes that
# CPU-heavy work roughly an order of magnitude slower. API print tests exercise
# the same renderer. Those test files opt out through the standard `race` build
# tag while their packages and the rest of the repository remain instrumented.
# `./...` traverses every directory whose name does not start with `.` or
# `_`. That includes package-shaped fixtures from JavaScript dependencies such
# as web/node_modules, which are neither part of Savorum nor safe to execute.
packages=()
# Capture enumeration first so a go list failure stops the check. A process
# substitution's exit status is not propagated by set -e or pipefail.
package_list=$(go list ./cmd/... ./internal/...)
while IFS= read -r package; do
	packages+=("$package")
done <<< "$package_list"
go test "${packages[@]}"
go test -race -timeout=30m "${packages[@]}"
