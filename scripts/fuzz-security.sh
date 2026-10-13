#!/usr/bin/env bash
set -euo pipefail

mode="${1:-smoke}"
case "$mode" in
	smoke) duration=5s; timeout=3m ;;
	campaign) duration=2m; timeout=10m ;;
	*) echo "usage: $0 [smoke|campaign]" >&2; exit 2 ;;
esac

export GOMAXPROCS=2

while read -r package target; do
	go test -run '^$' -parallel=2 -timeout="$timeout" -fuzz="^${target}$" -fuzztime="$duration" "$package"
done <<'TARGETS'
./internal/importer FuzzImportBytes
./internal/importer FuzzImageHeader
./internal/sref/strictjson FuzzStrictJSON
./internal/ingredientparser FuzzIngredientLine
./internal/recipe FuzzRational
TARGETS
