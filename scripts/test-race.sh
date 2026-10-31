#!/usr/bin/env bash
set -euo pipefail

# Selected packages run normally and with race detection. Typst/WASM corpus
# tests use the normal pass because the detector cannot instrument WebAssembly.
# Package enumeration excludes dependency fixtures outside cmd and internal.
group="${1:-all}"
if [[ $# -gt 0 ]]; then shift; fi
list_only=false
if [[ "$group" == --list ]]; then
	group=all
	list_only=true
elif [[ "${1:-}" == --list ]]; then
	list_only=true
	shift
fi
case "$group" in
	all|storage|administration|application) ;;
	*) echo "Unknown Go verification group: $group" >&2; exit 2 ;;
esac
if [[ $# -ne 0 ]]; then
	echo "usage: $0 [all|storage|administration|application] [--list]" >&2
	exit 2
fi

packages=()
# Capture enumeration first so a go list failure stops the check. A process
# substitution's exit status is not propagated by set -e or pipefail.
package_list=$(go list ./cmd/... ./internal/...)
while IFS= read -r package; do
	if [[ -z "$package" ]]; then continue; fi
	case "$package" in
		*/internal/storage/sqlite) package_group=storage ;;
		*/internal/administration) package_group=administration ;;
		*) package_group=application ;;
	esac
	if [[ "$group" == all || "$group" == "$package_group" ]]; then
		packages+=("$package")
	fi
done <<< "$package_list"
if [[ ${#packages[@]} -eq 0 ]]; then
	echo "Go verification group has no packages: $group" >&2
	exit 2
fi
if $list_only; then
	printf '%s\n' "${packages[@]}"
	exit 0
fi
# Recorded contract coverage is written by running tests, so cached results would leave it incomplete.
uncached=()
if [[ -n "${SAVORUM_CONTRACT_COVERAGE:-}" ]]; then uncached=(-count=1); fi
go test ${uncached[@]+"${uncached[@]}"} "${packages[@]}"
go test ${uncached[@]+"${uncached[@]}"} -race -timeout=30m "${packages[@]}"
